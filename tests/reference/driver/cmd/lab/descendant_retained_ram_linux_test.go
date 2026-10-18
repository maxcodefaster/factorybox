package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"

	"golang.org/x/sys/unix"
	"runtime-fork-experiment/pageview"
)

func descendantRAMCapturePacket(t *testing.T, connection *net.UnixConn, op uint32, fields [4]uint64, spans [][4]uint64, fd int) {
	t.Helper()
	data := make([]byte, 40+32*len(spans))
	binary.LittleEndian.PutUint32(data, 0x31535050)
	binary.LittleEndian.PutUint32(data[4:], op)
	for index, value := range fields {
		binary.LittleEndian.PutUint64(data[8+8*index:], value)
	}
	for index, span := range spans {
		for part, value := range span {
			binary.LittleEndian.PutUint64(data[40+32*index+8*part:], value)
		}
	}
	var control []byte
	if fd >= 0 {
		control = unix.UnixRights(fd)
	}
	if n, _, err := connection.WriteMsgUnix(data, control, nil); err != nil || n != len(data) {
		t.Fatal("native retained capture send", n, err)
	}
	reply := make([]byte, 16)
	n, err := connection.Read(reply)
	if err != nil || n != 16 || binary.LittleEndian.Uint32(reply) != 0x31535050 ||
		binary.LittleEndian.Uint32(reply[4:]) != op || binary.LittleEndian.Uint64(reply[8:]) != 0 {
		t.Fatal("native retained capture rejected", op, n, err)
	}
}

func TestDescendantRetainedRAMNative(t *testing.T) {
	if os.Getenv("DESCENDANT_RETAINED_RAM_NATIVE") != "1" {
		t.Skip("one isolated existing-worker keeper/tmpfs/bind test")
	}
	source, _ := descendantOwnedLayout("copy-sysbox")
	target, _ := descendantOwnedLayout("branch-0123456789abcdef0123456789abcdef")
	for _, path := range []string{source.Root, target.Root, source.Cgroup, "/experiment/pipe-store"} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatal("fixture must own absent roots", path, err)
		}
	}
	for _, layout := range []descendantLayout{source, target} {
		if err := os.MkdirAll(layout.Root, 0755); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.RemoveAll(layout.Root) })
	}
	if err := os.Mkdir(source.Cgroup, 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(source.Cgroup) })
	actor := exec.Command("/bin/sleep", "25")
	if err := actor.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { actor.Process.Kill(); actor.Wait() })
	if err := os.WriteFile(source.Cgroup+"/cgroup.procs", []byte(strconv.Itoa(actor.Process.Pid)), 0600); err != nil {
		t.Fatal(err)
	}
	keeper := exec.Command("/pipe-keeper")
	keeper.Env = append(os.Environ(), "RUNTIME_FORK_PIPE_RAW_BASE=1", "RUNTIME_FORK_PIPE_READ=lazy",
		"RUNTIME_FORK_PIPE_COMBINED_DIGEST=1", "GOMAXPROCS=1")
	log, err := os.OpenFile("/experiment/native-keeper.log", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	keeper.Stdout, keeper.Stderr = log, log
	if err := keeper.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { keeper.Process.Kill(); keeper.Wait() })
	end := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat("/experiment/pipe-store/ready.json"); err == nil {
			break
		}
		if time.Now().After(end) {
			t.Fatal("native keeper did not become ready")
		}
		time.Sleep(time.Millisecond)
	}
	images := source.Root + "/images-fedcba9876543210fedcba9876543210"
	state, err := descendantPrepareRAM(source, images, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { state.cleanup() })
	metadata := map[string][]byte{
		"inventory.img": []byte("native complete inventory"), "pstree.img": []byte("native task tree"),
		"runtime-config.json": []byte(`{"root":"owned"}`), "descriptors.json": []byte(`["/dev/null"]`),
		"core-1.img": []byte("independent metadata"),
	}
	expected := map[string]string{}
	for name, data := range metadata {
		path := filepath.Join(images, name)
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chown(path, 165536, 165536); err != nil {
			t.Fatal(err)
		}
		expected[name] = fmt.Sprintf("%x", sha256.Sum256(data))
	}
	payload := append(bytes.Repeat([]byte{17}, 4096), bytes.Repeat([]byte{29}, 4096)...)
	expected["pages-7.img"] = fmt.Sprintf("%x", sha256.Sum256(payload))
	file, err := os.OpenFile(images+"/pages-7.img", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(8192); err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteAt(payload[:4096], 0); err != nil {
		t.Fatal(err)
	}
	if err := file.Chown(165536, 165536); err != nil {
		t.Fatal(err)
	}
	var pipe [2]int
	if err := unix.Pipe2(pipe[:], unix.O_CLOEXEC); err != nil {
		t.Fatal(err)
	}
	defer unix.Close(pipe[0])
	defer unix.Close(pipe[1])
	if _, err := unix.FcntlInt(uintptr(pipe[1]), unix.F_SETPIPE_SZ, 8192); err != nil {
		t.Fatal(err)
	}
	if n, err := unix.Write(pipe[1], payload); err != nil || n != len(payload) {
		t.Fatal("native page pipe incomplete", n, err)
	}
	connection, err := net.DialUnix("unixpacket", nil, &net.UnixAddr{Name: "/experiment/pipe-store/control.sock", Net: "unixpacket"})
	if err != nil {
		t.Fatal(err)
	}
	if err := connection.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	descendantRAMCapturePacket(t, connection, 1, [4]uint64{7, uint64(actor.Process.Pid), 1, 0}, nil, int(file.Fd()))
	descendantRAMCapturePacket(t, connection, 2, [4]uint64{0, 8192, 2, 0},
		[][4]uint64{{0, 4096, 0, 1}, {4096, 4096, 4096, 2}}, pipe[0])
	if err := unix.Fsetxattr(int(file.Fd()), pageview.Attribute, []byte{1}, unix.XATTR_CREATE); err != nil {
		t.Fatal(err)
	}
	descendantRAMCapturePacket(t, connection, 3, [4]uint64{8192}, nil, -1)
	connection.Close()
	raw, err := os.ReadFile(file.Name())
	if err != nil || fmt.Sprintf("%x", sha256.Sum256(raw)) == expected["pages-7.img"] {
		t.Fatal("fixture did not actually omit private raw bytes", err)
	}
	if view, err := pageview.OpenAt("/experiment/missing.sock", file); err == nil || view != nil {
		t.Fatal("marked sparse image fell back to zero-filled raw bytes")
	}
	file.Close()
	if err := os.Chown(images, 165536, 165536); err != nil {
		t.Fatal(err)
	}
	if err := state.seal(); err != nil {
		t.Fatal(err)
	}
	if got, err := descendantSnapshotManifest(images); err != nil || !reflect.DeepEqual(got, expected) {
		t.Fatal("native logical source differs from complete oracle", got, err)
	}
	if err := state.clone(target, expected); err != nil {
		t.Fatal(err)
	}
	if got, err := descendantSnapshotManifest(target.Root + "/images"); err != nil || !reflect.DeepEqual(got, expected) {
		t.Fatal("native shared logical clone differs from complete oracle", got, err)
	}
	released, err := state.release(expected)
	if err != nil {
		t.Fatal("retained logical RAM release failed", released, err)
	}
	for _, path := range []string{images, target.Root + "/images"} {
		got, err := descendantSnapshotManifest(path)
		if err != nil || !reflect.DeepEqual(got, expected) {
			t.Fatal("fully backed bytes changed after actual RAM release", path, err)
		}
	}
	actor.Process.Kill()
	actor.Wait()
	if err := os.WriteFile("/experiment/pipe-store/finish", nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := keeper.Wait(); err != nil {
		t.Fatal("keeper did not retire its exact backing", err)
	}
	var proof map[string]any
	data, err := os.ReadFile("/experiment/pipe-store/result.json")
	if err != nil || json.Unmarshal(data, &proof) != nil || proof["status"] != "passed" ||
		proof["active_capture_credit_bytes"] != float64(0) || proof["active_capture_credit_descriptors"] != float64(0) {
		t.Fatal("retained keeper credits survived RAM backing", string(data), err)
	}
	t.Log(asJSON(released))
}
