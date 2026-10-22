//go:build linux

package sysboxcapture

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strconv"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// This bounded synthetic fixture profiles the real verifier without unpacking
// historical dumps. It is not a substitute for the complete consumer benchmark.
func filesystemProbeFixture(b testing.TB) (string, *os.File) {
	if os.Getenv("FACTORYBOX_NATIVE_FILESYSTEM_PROBE") != "1" {
		b.Skip("requires an owned filesystem probe volume")
	}
	base, err := os.MkdirTemp("/var/lib/sysbox", "digest-probe-")
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { os.RemoveAll(base) })
	stamp := time.Unix(1700000000, 123456789)
	for directory := range 8 {
		path := base + "/directory-" + strconv.Itoa(directory)
		if err := os.Mkdir(path, 0750); err != nil {
			b.Fatal(err)
		}
		for index := range 512 {
			file := path + "/file-" + strconv.Itoa(index)
			data := bytes.Repeat([]byte{byte(index), byte(directory)}, 8192)
			if err := os.WriteFile(file, data, 0640); err != nil {
				b.Fatal(err)
			}
			if index%64 == 0 {
				if err := unix.Setxattr(file, "user.factorybox", []byte("probe-xattr"), 0); err != nil {
					b.Fatal(err)
				}
			}
			if err := os.Chtimes(file, stamp, stamp); err != nil {
				b.Fatal(err)
			}
		}
		if err := os.Link(path+"/file-0", path+"/hardlink"); err != nil {
			b.Fatal(err)
		}
		if err := os.Symlink("file-1", path+"/symlink"); err != nil {
			b.Fatal(err)
		}
		if err := unix.UtimesNanoAt(unix.AT_FDCWD, path+"/symlink", []unix.Timespec{
			unix.NsecToTimespec(stamp.UnixNano()), unix.NsecToTimespec(stamp.UnixNano()),
		}, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			b.Fatal(err)
		}
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			b.Fatal(err)
		}
	}
	if err := os.Chtimes(base, stamp, stamp); err != nil {
		b.Fatal(err)
	}
	root, err := openDirectory(base)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { root.Close() })
	return base, root
}

func BenchmarkFilesystemDigestNative(b *testing.B) {
	_, root := filesystemProbeFixture(b)
	b.SetBytes(64 << 20)
	b.ResetTimer()
	for range b.N {
		started := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		tree, err := fingerprintTree(ctx, root, nil, false)
		cancel()
		elapsed := time.Since(started)
		if err != nil || tree.Entries != 4121 || tree.HashedBytes != 64<<20 || tree.FileBytes != (64<<20)+(8*16384) {
			b.Fatal("probe omitted data, metadata or hardlink topology", tree, err)
		}
		recordFilesystemProbe(b, "digest", elapsed, tree)
		b.ReportMetric(float64(tree.Entries), "entries/op")
	}
}

func BenchmarkFilesystemMaterializeNative(b *testing.B) {
	base, root := filesystemProbeFixture(b)
	home, err := os.MkdirTemp("/var/lib/sysbox", "materialize-benchmark-")
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { os.RemoveAll(home) })
	generation := home + "/generation"
	if err := os.MkdirAll(generation+"/filesystem/lower", 0700); err != nil {
		b.Fatal(err)
	}
	if err := os.Rename(base, generation+"/filesystem/upper"); err != nil {
		b.Fatal(err)
	}
	lower, err := openDirectory(generation + "/filesystem/lower")
	if err != nil {
		b.Fatal(err)
	}
	defer lower.Close()
	snapshot := &FilesystemSnapshot{Version: 1}
	for index, file := range []*os.File{lower, root} {
		tree, err := fingerprintTree(context.Background(), file, nil, false)
		if err != nil {
			b.Fatal(err)
		}
		tree.Name = []string{"lower", "upper"}[index]
		tree.Source = generation + "/filesystem/" + tree.Name
		tree.Saved = tree.Source
		snapshot.Roots = append(snapshot.Roots, tree)
	}
	b.SetBytes(64 << 20)
	b.ResetTimer()
	for index := range b.N {
		started := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		target, err := MaterializeFilesystemSnapshot(ctx, generation, home+"/target-"+strconv.Itoa(index), snapshot)
		cancel()
		elapsed := time.Since(started)
		if err != nil || target == nil || len(target.Roots) != 2 || !sameFilesystemTree(target.Roots[1], snapshot.Roots[1]) {
			b.Fatal("materialization omitted data or metadata", err)
		}
		recordFilesystemProbe(b, "materialize", elapsed, target.Roots[1])
		b.StopTimer()
		if err := os.RemoveAll(home + "/target-" + strconv.Itoa(index)); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
	}
}

func recordFilesystemProbe(b testing.TB, operation string, elapsed time.Duration, tree FilesystemTree) {
	b.Helper()
	observation := map[string]any{"configuration": os.Getenv("FACTORYBOX_PROBE_CONFIGURATION"),
		"operation": operation, "seconds": elapsed.Seconds(), "tree": tree}
	file, err := os.OpenFile("/output/digest.jsonl", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		b.Fatal(err)
	}
	encodeErr := json.NewEncoder(file).Encode(observation)
	closeErr := file.Close()
	if encodeErr != nil || closeErr != nil {
		b.Fatal(encodeErr, closeErr)
	}
}
