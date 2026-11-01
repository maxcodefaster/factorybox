//go:build linux

package fdstore

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func descriptorConnections(t *testing.T) (*net.UnixConn, *net.UnixConn) {
	t.Helper()
	pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	connections := make([]*net.UnixConn, 0, 2)
	for _, fd := range pair {
		file := os.NewFile(uintptr(fd), "owned descriptor transport")
		connection, err := net.FileConn(file)
		file.Close()
		if err != nil {
			t.Fatal(err)
		}
		owned := connection.(*net.UnixConn)
		owned.SetDeadline(time.Now().Add(5 * time.Second))
		connections = append(connections, owned)
		t.Cleanup(func() { owned.Close() })
	}
	return connections[0], connections[1]
}

func TestDescriptorTransportRetainsFileSetBeyondOneRightsPacket(t *testing.T) {
	directory := t.TempDir()
	path := directory + "/project-file"
	if err := os.WriteFile(path, []byte("original open description"), 0600); err != nil {
		t.Fatal(err)
	}
	original, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer original.Close()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("replacement on canonical source"), 0600); err != nil {
		t.Fatal(err)
	}
	const count = 257
	names, retained := make([]string, count), make([]*os.File, count)
	for index := range names {
		names[index], retained[index] = fmt.Sprintf("factorybox-reg:%d", index+1), original
	}
	listener, err := net.ListenUnix("unixpacket", &net.UnixAddr{Name: directory + "/socket", Net: "unixpacket"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan error, 1)
	key := strings.Repeat("a", 32) + ":" + strings.Repeat("b", 32)
	go func() {
		connection, err := listener.AcceptUnix()
		if err != nil {
			done <- err
			return
		}
		defer connection.Close()
		connection.SetDeadline(time.Now().Add(5 * time.Second))
		done <- send(connection, message{Operation: "get", Key: key, Names: names}, retained)
	}()
	client, err := net.DialUnix("unixpacket", nil, &net.UnixAddr{Name: directory + "/socket", Net: "unixpacket"})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	client.SetDeadline(time.Now().Add(5 * time.Second))
	reply, files, receiveErr := receive(client)
	defer closeFiles(files)
	sendErr := <-done
	if sendErr != nil || receiveErr != nil {
		t.Fatalf("retaining %d descriptions failed: send=%v receive=%v", count, sendErr, receiveErr)
	}
	if reply.Operation != "get" || reply.Key != key || len(reply.Names) != count || len(files) != count {
		t.Fatal("large retained set lost its exact request or descriptor cardinality")
	}
	for index, file := range files {
		if reply.Names[index] != names[index] {
			t.Fatal("large retained set changed descriptor order", index)
		}
		contents := make([]byte, len("original open description"))
		if _, err := file.ReadAt(contents, 0); err != nil || string(contents) != "original open description" {
			t.Fatal("transport reopened the replaced canonical source", index, err)
		}
	}
	current, err := os.ReadFile(path)
	if err != nil || string(current) != "replacement on canonical source" {
		t.Fatal("large descriptor transfer changed canonical source", string(current), err)
	}
}

func TestDescriptorTransportSplitsDirectoryMetadataWithoutLosingMappings(t *testing.T) {
	file, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	const count = 17
	value := message{Operation: "put", Key: strings.Repeat("c", 32) + ":" + strings.Repeat("d", 32)}
	retained := make([]*os.File, count)
	for index := range retained {
		name := fmt.Sprintf("factorybox-dir:%d", index+1)
		value.Names = append(value.Names, name)
		value.Directories = append(value.Directories, Directory{
			Name: name, MountPoint: "/workspace/" + strings.Repeat("d", 2500), MountRootRelative: "project",
		})
		retained[index] = file
	}
	sender, receiver := descriptorConnections(t)
	done := make(chan error, 1)
	go func() { done <- send(sender, value, retained) }()
	actual, files, err := receive(receiver)
	defer closeFiles(files)
	sendErr := <-done
	if err != nil || sendErr != nil {
		t.Fatal("large directory mapping transfer", err, sendErr)
	}
	if !reflect.DeepEqual(actual, value) || len(files) != count {
		t.Fatal("packet metadata was lost or reassociated")
	}
}

func TestDescriptorTransportRejectsPartialOrChangedTransfersAndClosesRights(t *testing.T) {
	for _, variant := range []string{"missing-final", "changed-key", "changed-total", "duplicate-offset", "changed-version"} {
		t.Run(variant, func(t *testing.T) {
			original, err := os.Open("/dev/null")
			if err != nil {
				t.Fatal(err)
			}
			defer original.Close()
			sender, receiver := descriptorConnections(t)
			before, err := os.ReadDir("/proc/self/fd")
			if err != nil {
				t.Fatal(err)
			}
			key := strings.Repeat("a", 32) + ":" + strings.Repeat("b", 32)
			first := packet{Version: TransferVersion, Total: 2,
				Message: message{Operation: "put", Key: key, Names: []string{"factorybox-reg:1"}}}
			second := packet{Version: TransferVersion, Offset: 1, Total: 2, Final: true,
				Message: message{Operation: "put", Key: key, Names: []string{"factorybox-reg:2"}}}
			switch variant {
			case "changed-key":
				second.Message.Key = strings.Repeat("c", 32) + ":" + strings.Repeat("d", 32)
			case "changed-total":
				second.Total, second.Final = 3, false
			case "duplicate-offset":
				second.Offset, second.Final = 0, false
			case "changed-version":
				second.Version++
			}
			done := make(chan error, 1)
			go func() {
				data, _ := json.Marshal(first)
				if err := sendPacket(sender, data, []*os.File{original}); err != nil {
					done <- err
					return
				}
				if variant == "missing-final" {
					done <- sender.CloseWrite()
					return
				}
				data, _ = json.Marshal(second)
				done <- sendPacket(sender, data, []*os.File{original})
			}()
			_, files, err := receive(receiver)
			closeFiles(files)
			if sendErr := <-done; sendErr != nil {
				t.Fatal(sendErr)
			}
			if err == nil || len(files) != 0 {
				t.Fatal("accepted a partial or changed descriptor transfer")
			}
			after, err := os.ReadDir("/proc/self/fd")
			if err != nil || len(after) != len(before) {
				t.Fatal("rejected transfer leaked received rights", len(before), len(after), err)
			}
		})
	}
}

func TestRootPID1RetainsAndReleasesLargeGenerationAtomically(t *testing.T) {
	if os.Geteuid() != 0 || os.Getpid() != 1 {
		t.Skip("explicit owned Linux PID 1 component proof")
	}
	path := t.TempDir() + "/retained.sock"
	server, err := Start(path)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	original, err := os.CreateTemp(t.TempDir(), "original")
	if err != nil {
		t.Fatal(err)
	}
	defer original.Close()
	if _, err := original.WriteString("pinned description"); err != nil {
		t.Fatal(err)
	}
	const count = 257
	names, retained := make([]string, count), make([]*os.File, count)
	for index := range names {
		names[index], retained[index] = fmt.Sprintf("factorybox-reg:%d", index+1), original
	}
	key := strings.Repeat("e", 32) + ":" + strings.Repeat("f", 32)
	if err := Put(path, key, names, retained); err != nil {
		t.Fatal("large generation admission failed", err)
	}
	if err := Put(path, key, names, retained); err == nil {
		t.Fatal("replaced an already retained generation")
	}
	actual, files, err := Get(path, key)
	if err != nil {
		t.Fatal(err)
	}
	defer closeFiles(files)
	if !reflect.DeepEqual(actual, names) || len(files) != count {
		t.Fatal("large generation was published incompletely")
	}
	if err := VerifyRestore(path, key); err != nil {
		t.Fatal(err)
	}
	if err := Release(path, key); err != nil {
		t.Fatal(err)
	}
	if _, extra, err := Get(path, key); err == nil {
		closeFiles(extra)
		t.Fatal("released generation remained available")
	}
	contents := make([]byte, len("pinned description"))
	if _, err := files[count-1].ReadAt(contents, 0); err != nil || string(contents) != "pinned description" {
		t.Fatal("release closed the consumer's independently received rights", err)
	}
}
