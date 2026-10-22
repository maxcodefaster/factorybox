//go:build linux

package sysboxcapture

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestFilesystemMaterializationNative(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_FILESYSTEM_PROBE") != "1" && os.Getenv("FACTORYBOX_NATIVE_RETAINED_CAPTURE") != "1" {
		t.Skip("requires an owned native filesystem probe")
	}
	base, err := os.MkdirTemp("/var/lib/sysbox", "materialization-probe-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)
	generation := base + "/generation"
	snapshot := &FilesystemSnapshot{Version: 1}
	for _, name := range []string{"lower", "upper"} {
		path := generation + "/filesystem/" + name
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
		if name == "upper" {
			if err := os.WriteFile(path+"/payload", bytes.Repeat([]byte("complete-payload"), 4096), 0600); err != nil {
				t.Fatal(err)
			}
		}
		root, err := openDirectory(path)
		if err != nil {
			t.Fatal(err)
		}
		tree, readErr := fingerprintTree(context.Background(), root, nil, false)
		closeErr := root.Close()
		if readErr != nil || closeErr != nil {
			t.Fatal(readErr, closeErr)
		}
		tree.Name, tree.Source, tree.Saved = name, path, path
		snapshot.Roots = append(snapshot.Roots, tree)
	}
	payload := generation + "/filesystem/upper/payload"
	watcher, err := unix.InotifyInit1(unix.IN_CLOEXEC | unix.IN_NONBLOCK)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(watcher)
	if _, err := unix.InotifyAddWatch(watcher, payload, unix.IN_OPEN|unix.IN_CLOSE_NOWRITE); err != nil {
		t.Fatal(err)
	}
	materialized, err := MaterializeFilesystemSnapshot(context.Background(), generation, base+"/target", snapshot)
	if err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 4096)
	opens := 0
	for {
		n, err := unix.Read(watcher, buffer)
		if errors.Is(err, unix.EAGAIN) {
			break
		}
		if err != nil || n == 0 {
			t.Fatal("read exact source-open observation", n, err)
		}
		for offset := 0; offset < n; {
			mask := binary.NativeEndian.Uint32(buffer[offset+4:])
			if mask&unix.IN_Q_OVERFLOW != 0 {
				t.Fatal("source-open observation overflowed")
			}
			if mask&unix.IN_OPEN != 0 {
				opens++
			}
			offset += unix.SizeofInotifyEvent + int(binary.NativeEndian.Uint32(buffer[offset+12:]))
		}
	}
	if opens != 1 {
		t.Errorf("materialization opened source payload %d times; require one copy read and complete target verification", opens)
	}
	if err := VerifyFilesystemSnapshot(context.Background(), base+"/target", materialized); err != nil {
		t.Fatal("complete target verification", err)
	}
	original, err := os.ReadFile(payload)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(payload, bytes.Repeat([]byte{'x'}, len(original)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(payload, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if _, err := MaterializeFilesystemSnapshot(context.Background(), generation, base+"/corrupt", snapshot); err == nil {
		t.Fatal("same-size same-mtime source corruption was published")
	}
	if err := os.WriteFile(payload, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(payload, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := unix.Setxattr(payload, "user.factorybox", []byte("corrupted-metadata"), 0); err != nil {
		t.Fatal(err)
	}
	if _, err := MaterializeFilesystemSnapshot(context.Background(), generation, base+"/corrupt-metadata", snapshot); err == nil {
		t.Fatal("source metadata corruption was published")
	}
	if err := unix.Removexattr(payload, "user.factorybox"); err != nil {
		t.Fatal(err)
	}
	t.Run("source-changes-during-copy", func(t *testing.T) {
		// Intercept the real copy helper's open, after topology admission.
		// Keep a write descriptor from before the permission watch so that
		// injecting corruption cannot recursively block on its own open.
		writer, err := os.OpenFile(payload, os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer writer.Close()
		fan, err := unix.FanotifyInit(unix.FAN_CLASS_CONTENT|unix.FAN_CLOEXEC|unix.FAN_NONBLOCK, unix.O_RDONLY|unix.O_CLOEXEC)
		if err != nil {
			t.Fatal("require actual copy-open permission interception", err)
		}
		defer unix.Close(fan)
		if err := unix.FanotifyMark(fan, unix.FAN_MARK_ADD, unix.FAN_OPEN_PERM, unix.AT_FDCWD, payload); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		finished := make(chan error, 1)
		go func() {
			_, err := MaterializeFilesystemSnapshot(ctx, generation, base+"/changed-during-copy", snapshot)
			finished <- err
		}()
		poll := []unix.PollFd{{Fd: int32(fan), Events: unix.POLLIN}}
		if count, err := unix.Poll(poll, 3000); err != nil || count != 1 {
			t.Fatal("real copy never opened the source payload", count, err)
		}
		buffer := make([]byte, 4096)
		n, err := unix.Read(fan, buffer)
		if err != nil || n < unix.FAN_EVENT_METADATA_LEN || buffer[4] != unix.FANOTIFY_METADATA_VERSION {
			t.Fatal("invalid source-open permission event", n, err)
		}
		mask := binary.NativeEndian.Uint64(buffer[8:])
		eventFD := int(int32(binary.NativeEndian.Uint32(buffer[16:])))
		if mask&unix.FAN_OPEN_PERM == 0 || eventFD < 0 {
			t.Fatal("source-open permission observation was incomplete", mask, eventFD)
		}
		defer unix.Close(eventFD)
		if _, err := writer.WriteAt(bytes.Repeat([]byte{'x'}, len(original)), 0); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(payload, info.ModTime(), info.ModTime()); err != nil {
			t.Fatal(err)
		}
		response := make([]byte, 8)
		binary.NativeEndian.PutUint32(response, uint32(eventFD))
		binary.NativeEndian.PutUint32(response[4:], unix.FAN_ALLOW)
		if n, err := unix.Write(fan, response); err != nil || n != len(response) {
			t.Fatal("release exact intercepted copy open", n, err)
		}
		if err := <-finished; err == nil || !strings.Contains(err.Error(), "complete filesystem copy differs") {
			t.Fatal("copy-time corruption escaped complete target verification", err)
		}
		if _, err := writer.WriteAt(original, 0); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(payload, info.ModTime(), info.ModTime()); err != nil {
			t.Fatal(err)
		}
	})
	foreign := base + "/foreign"
	if err := os.WriteFile(foreign, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mount(foreign, payload, "", unix.MS_BIND, ""); err != nil {
		t.Fatal(err)
	}
	_, rejected := MaterializeFilesystemSnapshot(context.Background(), generation, base+"/foreign-target", snapshot)
	unmountErr := unix.Unmount(payload, 0)
	if rejected == nil || !strings.Contains(rejected.Error(), "foreign mount") || unmountErr != nil {
		t.Fatal("foreign source mount was admitted", rejected, unmountErr)
	}
	if _, err := os.Lstat(base + "/foreign-target"); !os.IsNotExist(err) {
		t.Fatal("foreign mount was copied before topology admission", err)
	}
	if err := os.RemoveAll(generation); err != nil {
		t.Fatal(err)
	}
	if err := VerifyFilesystemSnapshot(context.Background(), base+"/target", materialized); err != nil {
		t.Fatal("target depended on removed source", err)
	}
}
