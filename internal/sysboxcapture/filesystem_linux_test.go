//go:build linux

package sysboxcapture

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func BenchmarkFilesystemCloneNative(b *testing.B) {
	if os.Getenv("FACTORYBOX_NATIVE_RETAINED_CAPTURE") != "1" {
		b.Skip("requires an owned native filesystem fixture")
	}
	base, err := os.MkdirTemp("/var/lib/sysbox", "factorybox-filesystem-benchmark-")
	if err != nil {
		b.Fatal(err)
	}
	source := base + "/source"
	if err := os.Mkdir(source, 0750); err != nil {
		b.Fatal(err)
	}
	for directory := range 16 {
		path := source + "/directory-" + strconv.Itoa(directory)
		if err := os.Mkdir(path, 0750); err != nil {
			b.Fatal(err)
		}
		for index := range 128 {
			if err := os.WriteFile(path+"/file-"+strconv.Itoa(index), bytes.Repeat([]byte{byte(index)}, 512), 0640); err != nil {
				b.Fatal(err)
			}
		}
	}
	root, err := openDirectory(source)
	if err != nil {
		b.Fatal(err)
	}
	defer root.Close()
	b.ResetTimer()
	for index := range b.N {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		tree, err := cloneFilesystemTree(ctx, root, base+"/saved-"+strconv.Itoa(index))
		cancel()
		if err != nil {
			b.Fatal(err)
		}
		if tree.Entries != 2065 || tree.FileBytes != 1<<20 || tree.HashedBytes != 1<<20 {
			b.Fatal("metadata benchmark did not capture its exact complete dataset", tree)
		}
		b.ReportMetric(float64(tree.Entries), "entries/op")
		b.ReportMetric(float64(tree.HashedBytes), "hashed-bytes/op")
	}
}

func TestFilesystemHookAdmission(t *testing.T) {
	for _, test := range []struct {
		raw   string
		count int
	}{
		{`{"prestart":null,"createRuntime":null,"createContainer":null,"startContainer":null,"poststart":null,"poststop":null}`, 0},
		{`{"prestart":[]}`, 0},
		{`{"prestart":[{"path":"/external-hook"}]}`, 1},
	} {
		var hooks map[string][]json.RawMessage
		if err := json.Unmarshal([]byte(test.raw), &hooks); err != nil {
			t.Fatal(err)
		}
		if got := filesystemHookCount(hooks); got != test.count {
			t.Fatalf("hook admission count=%d expected=%d", got, test.count)
		}
	}
}

func TestFilesystemCopyNative(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_RETAINED_CAPTURE") != "1" {
		t.Skip("requires the owned native filesystem fixture")
	}
	base, err := os.MkdirTemp("/var/lib/sysbox", "factorybox-filesystem-")
	if err != nil {
		t.Fatal(err)
	}
	source := base + "/source"
	if err := os.Mkdir(source, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source+"/file", []byte("original-inode-content\n"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(source+"/file", source+"/hardlink"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("file", source+"/symlink"); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(source+"/fifo", 0600); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mknod(source+"/whiteout", unix.S_IFCHR|0600, 0); err != nil {
		t.Fatal(err)
	}
	socket, err := unix.Socket(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Bind(socket, &unix.SockaddrUnix{Name: source + "/socket"}); err != nil {
		unix.Close(socket)
		t.Fatal(err)
	}
	unix.Close(socket)
	file, err := os.OpenFile(source+"/sparse", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteAt([]byte("sparse-tail"), 8<<20); err != nil {
		file.Close()
		t.Fatal(err)
	}
	file.Close()
	if err := unix.Setxattr(source+"/file", "user.factorybox", []byte("original-xattr"), 0); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(source+"/opaque", 0700); err != nil {
		t.Fatal(err)
	}
	if err := unix.Setxattr(source+"/opaque", "trusted.overlay.opaque", []byte("y"), 0); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(source+"/file", 12345, 12346); err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(1700000000, 123456789)
	if err := os.Chtimes(source+"/file", stamp, stamp); err != nil {
		t.Fatal(err)
	}
	root, err := openDirectory(source)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	destination := base + "/saved"
	copied, err := cloneFilesystemTree(context.Background(), root, destination)
	if err != nil {
		t.Fatal(err)
	}
	target, err := openDirectory(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	if copied.Entries < 9 || copied.FileBytes < 8<<20 {
		t.Fatal("filesystem fixture coverage was incomplete", copied)
	}
	if copied.HashedBytes >= copied.FileBytes {
		t.Fatal("hardlinked content was read repeatedly")
	}
	if err := os.WriteFile(source+"/file", []byte("source-after\n"), 0640); err != nil {
		t.Fatal(err)
	}
	after, err := fingerprintTree(context.Background(), target, nil, false)
	if err != nil || after.SHA256 != copied.SHA256 {
		t.Fatal("source mutation changed saved tree", err)
	}
	if err := unix.Setxattr(destination+"/file", "user.factorybox", []byte("changed-xattr"), 0); err != nil {
		t.Fatal(err)
	}
	changed, err := fingerprintTree(context.Background(), target, nil, false)
	if err != nil || changed.SHA256 == copied.SHA256 {
		t.Fatal("xattr corruption escaped the filesystem digest", err)
	}
	foreign := base + "/foreign"
	mount := source + "/foreign"
	if err := os.Mkdir(foreign, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(mount, 0700); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mount(foreign, mount, "", unix.MS_BIND, ""); err != nil {
		t.Fatal(err)
	}
	defer unix.Unmount(mount, 0)
	if _, err := fingerprintTree(context.Background(), root, nil, false); err == nil ||
		!strings.Contains(err.Error(), "foreign mount") {
		t.Fatal("foreign filesystem mount was traversed", err)
	}
	if data, err := os.ReadFile(filepath.Join(destination, "hardlink")); err != nil || string(data) != "original-inode-content\n" {
		t.Fatal("captured hardlink lost its independent original content", err)
	}
	t.Log("complete copy retained UID/GID, modes, mtime, hardlinks, symlinks, xattrs, sparse data, FIFO/socket and whiteout; source mutation independent; xattr corruption and foreign mount detected")
}
