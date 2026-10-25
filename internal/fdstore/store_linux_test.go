//go:build linux

package fdstore

import (
	"net"
	"os"
	"strings"
	"testing"
)

func TestDescriptorTransportPreservesReplacedInode(t *testing.T) {
	directory := t.TempDir()
	path := directory + "/project-file"
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
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
	if err := os.WriteFile(path, []byte("editor replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.ListenUnix("unixpacket", &net.UnixAddr{Name: directory + "/socket", Net: "unixpacket"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan error, 1)
	go func() {
		connection, err := listener.AcceptUnix()
		if err != nil {
			done <- err
			return
		}
		defer connection.Close()
		done <- send(connection, message{Operation: "get", Key: strings.Repeat("a", 32) + ":" + strings.Repeat("b", 32), Names: []string{"factorybox-reg:5"}}, []*os.File{original})
	}()
	client, err := net.DialUnix("unixpacket", nil, &net.UnixAddr{Name: directory + "/socket", Net: "unixpacket"})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	reply, files, err := receive(client)
	if err != nil {
		t.Fatal(err)
	}
	defer closeFiles(files)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || len(reply.Names) != 1 || reply.Names[0] != "factorybox-reg:5" {
		t.Fatal("descriptor identity was lost", reply)
	}
	contents := make([]byte, 8)
	if _, err := files[0].ReadAt(contents, 0); err != nil || string(contents) != "original" {
		t.Fatal("transport reopened the edited path", string(contents), err)
	}
	current, err := os.ReadFile(path)
	if err != nil || string(current) != "editor replacement" {
		t.Fatal("transport changed canonical project data", string(current), err)
	}
}

func TestClientRejectsEndpointOutsideOriginalRootPID1(t *testing.T) {
	path := t.TempDir() + "/socket"
	listener, err := net.ListenUnix("unixpacket", &net.UnixAddr{Name: path, Net: "unixpacket"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		connection, err := listener.AcceptUnix()
		if err == nil {
			connection.Close()
		}
	}()
	if err := Ping(path); err == nil {
		t.Fatal("accepted a foreign descriptor endpoint")
	}
	<-done
}

func TestRetainedNamesAreBoundedAndUnique(t *testing.T) {
	for _, names := range [][]string{
		{"factorybox-reg:0"}, {"factorybox-reg:1", "factorybox-reg:1"}, {"/project/file"},
		{"factorybox-reg:1\n"}, make([]string, MaximumFiles+1),
	} {
		if validNames(names) {
			t.Fatal("admitted invalid retained-file identities", names)
		}
	}
	if !validNames([]string{"factorybox-reg:1", "factorybox-inotify:2", "factorybox-fanotify:3"}) || !validNames(nil) {
		t.Fatal("refused a valid bounded generation")
	}
}
