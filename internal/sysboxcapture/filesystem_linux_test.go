//go:build linux

package sysboxcapture

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
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

func BenchmarkDeveloperBaseDigestNative(b *testing.B) {
	if os.Getenv("FACTORYBOX_NATIVE_DEVELOPER_DIGEST") != "1" {
		b.Skip("requires the owned archived developer-base fixture")
	}
	var expected struct {
		Entries     int   `json:"entries"`
		FileBytes   int64 `json:"file_bytes"`
		HashedBytes int64 `json:"hashed_bytes"`
	}
	data, err := os.ReadFile("/input.json")
	if err != nil || json.Unmarshal(data, &expected) != nil ||
		expected.Entries < 40000 || expected.HashedBytes < 1<<30 {
		b.Fatal("developer base does not have its pinned complete dataset", err)
	}
	root, err := openDirectory("/var/lib/sysbox/lower")
	if err != nil {
		b.Fatal(err)
	}
	defer root.Close()
	cold := os.Getenv("FACTORYBOX_NATIVE_DEVELOPER_DIGEST_COLD") == "1"
	if cold {
		if err := filepath.WalkDir("/var/lib/sysbox/lower", func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if !entry.Type().IsRegular() {
				return nil
			}
			fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NOATIME|unix.O_CLOEXEC, 0)
			if err != nil {
				return err
			}
			adviceErr := unix.Fadvise(fd, 0, 0, unix.FADV_DONTNEED)
			closeErr := unix.Close(fd)
			if adviceErr != nil {
				return adviceErr
			}
			return closeErr
		}); err != nil {
			b.Fatal("evict only the owned archived dataset's clean file pages", err)
		}
	}
	b.SetBytes(expected.HashedBytes)
	b.ResetTimer()
	for range b.N {
		var before, after unix.Rusage
		var memoryBefore, memoryAfter runtime.MemStats
		if err := unix.Getrusage(unix.RUSAGE_SELF, &before); err != nil {
			b.Fatal(err)
		}
		runtime.ReadMemStats(&memoryBefore)
		started := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		tree, scanErr := fingerprintTree(ctx, root, nil, false)
		cancel()
		elapsed := time.Since(started)
		runtime.ReadMemStats(&memoryAfter)
		allocated := memoryAfter.TotalAlloc - memoryBefore.TotalAlloc
		if err := unix.Getrusage(unix.RUSAGE_SELF, &after); err != nil {
			b.Fatal(err)
		}
		cpu := func(value unix.Timeval) float64 {
			return float64(value.Sec) + float64(value.Usec)/1e6
		}
		observation := map[string]any{
			"seconds": elapsed.Seconds(), "tree": tree, "complete": scanErr == nil,
			"cold_file_data":  cold,
			"allocated_bytes": allocated,
			"user_seconds":    cpu(after.Utime) - cpu(before.Utime),
			"system_seconds":  cpu(after.Stime) - cpu(before.Stime),
			"major_faults":    after.Majflt - before.Majflt,
		}
		if scanErr != nil {
			observation["error"] = scanErr.Error()
		}
		encoded, err := json.MarshalIndent(observation, "", "  ")
		if err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile("/output/digest.json", encoded, 0600); err != nil {
			b.Fatal(err)
		}
		b.Logf("developer base digest=%s entries=%d hashed=%d complete=%t",
			elapsed, tree.Entries, tree.HashedBytes, scanErr == nil)
		if scanErr != nil {
			b.Fatal(scanErr)
		}
		if tree.Entries != expected.Entries || tree.FileBytes != expected.FileBytes ||
			tree.HashedBytes != expected.HashedBytes {
			b.Fatal("developer base digest omitted or duplicated archived data", tree)
		}
		if allocated >= 128<<20 {
			b.Fatal("developer base scan exceeded its 128-MiB allocation regression budget", allocated)
		}
		b.ReportMetric(float64(tree.Entries), "entries/op")
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
	if copied.SourceReadUS <= 0 || copied.CopyUS <= 0 || copied.TargetReadUS <= 0 {
		t.Fatal("complete copy omitted its source, copy or independent-target observation", copied)
	}
	t.Run("known-digest-complete-target", func(t *testing.T) {
		actual, err := cloneFilesystemTreeExpected(context.Background(), root, base+"/known-digest-copy", &copied)
		if err != nil || !sameFilesystemTree(actual, copied) {
			t.Fatal("known complete source did not produce the same verified independent target", err)
		}
		if actual.SourceReadUS != 0 || actual.CopyUS <= 0 || actual.TargetReadUS <= 0 {
			t.Fatal("reused digest reported stale source work or omitted new copy/target work", actual)
		}
	})
	t.Run("known-digest-rejects-changed-content", func(t *testing.T) {
		path := source + "/file"
		original, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, bytes.Repeat([]byte{'x'}, len(original)), 0640); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
			t.Fatal(err)
		}
		_, rejected := cloneFilesystemTreeExpected(context.Background(), root, base+"/changed-known-digest", &copied)
		if err := os.WriteFile(path, original, 0640); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
			t.Fatal(err)
		}
		if rejected == nil {
			t.Fatal("known-digest copy accepted changed content with the original size and mtime")
		}
	})
	t.Run("known-digest-cancellation-before-target", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		destination := base + "/canceled-known-digest"
		if _, err := cloneFilesystemTreeExpected(ctx, root, destination, &copied); err == nil {
			t.Fatal("known-digest copy ignored cancellation")
		}
		if _, err := os.Lstat(destination); !os.IsNotExist(err) {
			t.Fatal("canceled known-digest copy created a target", err)
		}
	})
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
