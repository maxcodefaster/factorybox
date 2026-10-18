package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func sendFixture(connection *net.UnixConn, message captureMessage) error {
	packet := make([]byte, 40+len(message.spans)*32)
	binary.LittleEndian.PutUint32(packet, captureMagic)
	binary.LittleEndian.PutUint32(packet[4:], message.op)
	for index, field := range []uint64{message.a, message.b, message.c, message.d} {
		binary.LittleEndian.PutUint64(packet[8+8*index:], field)
	}
	for index, span := range message.spans {
		for column, field := range []uint64{span.offset, span.bytes, span.pipeOffset, span.eager} {
			binary.LittleEndian.PutUint64(packet[40+index*32+column*8:], field)
		}
	}
	var control []byte
	if len(message.fds) != 0 {
		control = unix.UnixRights(message.fds...)
	}
	if n, _, err := connection.WriteMsgUnix(packet, control, nil); err != nil || n != len(packet) {
		return fmt.Errorf("fixture send failed: %d %v", n, err)
	}
	reply := make([]byte, 16)
	n, err := connection.Read(reply)
	if err != nil || n != len(reply) || binary.LittleEndian.Uint32(reply) != captureMagic ||
		binary.LittleEndian.Uint32(reply[4:]) != message.op {
		return fmt.Errorf("fixture reply malformed: %d %v", n, err)
	}
	if binary.LittleEndian.Uint64(reply[8:]) != 0 {
		return fmt.Errorf("fixture command rejected")
	}
	return nil
}

func captureFixture(t *testing.T) (*captureStore, *net.UnixConn, <-chan error, *os.File, [2]int, *exec.Cmd) {
	return captureFixtureRoot(t, false)
}

func captureFixtureRoot(t *testing.T, ownTmpfs bool) (*captureStore, *net.UnixConn, <-chan error, *os.File, [2]int, *exec.Cmd) {
	return captureFixtureBackend(t, ownTmpfs, false)
}

func captureFixtureBackend(t *testing.T, ownTmpfs, rawBase bool) (*captureStore, *net.UnixConn, <-chan error, *os.File, [2]int, *exec.Cmd) {
	t.Helper()
	if os.Getenv("PIPE_STORE_TEST_OUTPUT") == "" {
		t.Skip("native owned container only")
	}
	directory := t.TempDir()
	if ownTmpfs {
		if err := unix.Mount("owned-native-raw-image", directory, "tmpfs", unix.MS_NOSUID|unix.MS_NODEV, "size=131072,mode=0700"); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := unix.Unmount(directory, 0); err != nil {
				t.Error("owned native snapshot mount remained", err)
			}
		})
	}
	if err := os.Mkdir(filepath.Join(directory, "images-again"), 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "images-again/pages-7.img")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	data := append(bytes.Repeat([]byte{17}, 4096), bytes.Repeat([]byte{29}, 4096)...)
	if _, err := file.Write(data); err != nil {
		t.Fatal(err)
	}
	var pipe [2]int
	if err := unix.Pipe2(pipe[:], unix.O_CLOEXEC); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, fd := range pipe {
			if fd >= 0 {
				_ = unix.Close(fd)
			}
		}
	})
	if _, err := unix.FcntlInt(uintptr(pipe[1]), unix.F_SETPIPE_SZ, 8192); err != nil {
		t.Fatal(err)
	}
	if n, err := unix.Write(pipe[1], data); err != nil || n != len(data) {
		t.Fatal("native fixture pipe did not fill")
	}
	listener, err := net.ListenUnix("unixpacket", &net.UnixAddr{Name: filepath.Join(directory, "control.sock"), Net: "unixpacket"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	output, err := os.MkdirTemp(os.Getenv("PIPE_STORE_TEST_OUTPUT"), "keeper-")
	if err != nil {
		t.Fatal(err)
	}
	store := &captureStore{imageRoot: directory, output: output, rawBase: rawBase}
	done := make(chan error, 1)
	go func() {
		connection, err := listener.AcceptUnix()
		if err == nil {
			err = store.connection(connection)
		}
		done <- err
	}()
	connection, err := net.DialUnix("unixpacket", nil, listener.Addr().(*net.UnixAddr))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	if err := connection.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	source := exec.Command("/bin/sleep", "20")
	if err := source.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if source.ProcessState == nil {
			_ = source.Process.Kill()
			_ = source.Wait()
		}
	})
	return store, connection, done, file, pipe, source
}

func TestActualDescriptorProtocolCommitExportAndRelease(t *testing.T) {
	store, connection, done, file, pipe, source := captureFixture(t)
	if err := sendFixture(connection, captureMessage{op: 1, a: 7, b: uint64(source.Process.Pid), fds: []int{int(file.Fd())}}); err != nil {
		t.Fatal(err)
	}
	if err := sendFixture(connection, captureMessage{op: 2, b: 8192, c: 2, fds: []int{pipe[0]}, spans: []captureSpan{
		{0, 4096, 0, 0}, {4096, 4096, 4096, 1},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := sendFixture(connection, captureMessage{op: 3, a: 8192}); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteAt(make([]byte, 8192), 0); err != nil {
		t.Fatal(err)
	}
	if err := source.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = source.Wait()
	proof, err := store.finish()
	if err != nil {
		t.Fatal(err)
	}
	export, err := os.ReadFile(filepath.Join(store.output, "capture-000-pages-7.img"))
	expected := append(bytes.Repeat([]byte{17}, 4096), bytes.Repeat([]byte{29}, 4096)...)
	if err != nil || !bytes.Equal(export, expected) {
		t.Fatal("actual descriptor transport/eager seal/export did not preserve bytes")
	}
	proof["status"], proof["kind"] = "passed", "capture-protocol-commit-export-release"
	proof["scope"] = "Native SCM_RIGHTS protocol and ownership regression with copied fixture pipe pages, sealed eager exception and dead source identity. No actual CRIU capture or CoW claim from this transport fixture."
	encoded, _ := json.Marshal(proof)
	fmt.Println(string(encoded))
}

func TestRejectedTransferClosesOnlyReceivedOwnedDescriptors(t *testing.T) {
	for _, failure := range []string{"gap", "writer", "bad-size", "range", "missing-fd", "fallback-private", "double-fallback"} {
		t.Run(failure, func(t *testing.T) {
			store, connection, done, file, pipe, source := captureFixture(t)
			if err := sendFixture(connection, captureMessage{op: 1, a: 7, b: uint64(source.Process.Pid), fds: []int{int(file.Fd())}}); err != nil {
				t.Fatal(err)
			}
			message := captureMessage{op: 2, b: 8192, c: 1, fds: []int{pipe[0]},
				spans: []captureSpan{{0, 8192, 0, 0}}}
			switch failure {
			case "gap":
				message.spans[0].offset = 4096
			case "writer":
				message.fds[0] = pipe[1]
			case "bad-size":
				message.b = 4096
			case "range":
				message.a = 4096
			case "missing-fd":
				message.fds = nil
			case "fallback-private":
				message.spans[0].eager = 8
			case "double-fallback":
				message.spans[0].eager = 25
			}
			if err := sendFixture(connection, message); err == nil {
				t.Fatal("malformed native capture transfer accepted")
			}
			if err := <-done; err == nil {
				t.Fatal("actual receiver did not retain its failure")
			}
			if store.heldBytes != 0 || store.heldFDs != 0 || len(store.images) != 0 {
				t.Fatal("failed transfer retained ownership credits")
			}
			for _, fd := range []int{pipe[0], pipe[1], int(file.Fd())} {
				var stat unix.Stat_t
				if err := unix.Fstat(fd, &stat); err != nil {
					t.Fatal("cleanup closed the sender's descriptor")
				}
			}
		})
	}
}

func TestMismatchRetainsActualPrivatePageEvidence(t *testing.T) {
	store, connection, done, file, pipe, source := captureFixture(t)
	if err := sendFixture(connection, captureMessage{op: 1, a: 7, b: uint64(source.Process.Pid), fds: []int{int(file.Fd())}}); err != nil {
		t.Fatal(err)
	}
	if err := sendFixture(connection, captureMessage{op: 2, b: 8192, c: 2, fds: []int{pipe[0]}, spans: []captureSpan{
		{0, 4096, 0, 0}, {4096, 4096, 4096, 1},
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteAt([]byte{231}, 37); err != nil {
		t.Fatal(err)
	}
	if err := sendFixture(connection, captureMessage{op: 3, a: 8192}); err == nil {
		t.Fatal("actual differing retained/golden byte accepted")
	}
	if err := <-done; err == nil {
		t.Fatal("actual mismatch failure lost")
	}
	if store.heldBytes != 0 || store.heldFDs != 0 || len(store.images) != 0 {
		t.Fatal("mismatching image retained ownership credits")
	}
	original, err := os.ReadFile(filepath.Join(store.output, "mismatch-000-pages-7.img-original.bin"))
	if err != nil {
		t.Fatal(err)
	}
	retained, err := os.ReadFile(filepath.Join(store.output, "mismatch-000-pages-7.img-retained.bin"))
	if err != nil || original[37] != 231 || retained[37] != 17 {
		t.Fatal("actual first differing page bytes were not preserved")
	}
	encoded, err := os.ReadFile(filepath.Join(store.output, "mismatch-000-pages-7.img.json"))
	if err != nil {
		t.Fatal(err)
	}
	var evidence map[string]any
	if err := json.Unmarshal(encoded, &evidence); err != nil {
		t.Fatal(err)
	}
	positions := evidence["changed_byte_offsets_in_page"].([]any)
	if evidence["page_offset"] != float64(0) || len(positions) != 1 || positions[0] != float64(37) ||
		evidence["span"].(map[string]any)["eager"] != false {
		t.Fatal("mismatch position or private-span classification differs")
	}
}

func TestPostCommitVmspliceMutationIsDetected(t *testing.T) {
	store, connection, done, file, pipe, source := captureFixture(t)
	mapped, err := unix.Mmap(-1, 0, 8192, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_PRIVATE|unix.MAP_ANONYMOUS)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Munmap(mapped)
	copy(mapped[:4096], bytes.Repeat([]byte{17}, 4096))
	copy(mapped[4096:], bytes.Repeat([]byte{29}, 4096))
	var drain [8192]byte
	if n, err := unix.Read(pipe[0], drain[:]); err != nil || n != len(drain) {
		t.Fatal("cannot prepare actual unprotected vmsplice control")
	}
	vector := unix.Iovec{Base: &mapped[0]}
	vector.SetLen(len(mapped))
	if n, err := unix.Vmsplice(pipe[1], []unix.Iovec{vector}, unix.SPLICE_F_NONBLOCK); err != nil || n != len(mapped) {
		t.Fatalf("actual vmsplice control failed: %d %v", n, err)
	}
	if err := sendFixture(connection, captureMessage{op: 1, a: 7, b: uint64(source.Process.Pid), fds: []int{int(file.Fd())}}); err != nil {
		t.Fatal(err)
	}
	if err := sendFixture(connection, captureMessage{op: 2, b: 8192, c: 2, fds: []int{pipe[0]}, spans: []captureSpan{
		{0, 4096, 0, 0}, {4096, 4096, 4096, 1},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := sendFixture(connection, captureMessage{op: 3, a: 8192}); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	mapped[37] = 231
	_ = source.Process.Kill()
	_ = source.Wait()
	if _, err := store.finish(); err == nil {
		t.Fatal("actual post-commit source write to vmsplice page was accepted")
	}
	diagnostic := filepath.Join(store.output, "mismatch-001-pages-7.img")
	page, err := os.ReadFile(diagnostic + "-retained.bin")
	if err != nil || page[37] != 231 {
		t.Fatal("actual mutated vmsplice page was not preserved")
	}
	encoded, err := os.ReadFile(diagnostic + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var evidence map[string]any
	if err := json.Unmarshal(encoded, &evidence); err != nil ||
		evidence["after_source_exit"] != true || evidence["page_offset"] != float64(0) {
		t.Fatal("post-source mismatch stage/offset missing")
	}
	store.discard(store.images[0])
}
