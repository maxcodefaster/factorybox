//go:build linux

package sysboxcapture

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// This opt-in component proof exercises the actual selected filesystem copier.
// It creates no Sandbox and uses only the owned worker's two fixture roots.
func TestFilesystemCopyCOWNative(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_COW_COPY") != "1" {
		t.Skip("requires the owned native copy-on-write fixture")
	}
	for _, root := range []string{
		"/var/lib/sysbox/cow-copy-host",
		"/var/lib/sysbox/fixed-store/data/store/cow-copy-bounded",
	} {
		t.Run(filepath.Base(root), func(t *testing.T) {
			if err := os.Mkdir(root, 0700); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := os.RemoveAll(root); err != nil {
					t.Error(err)
				}
			})
			source := filepath.Join(root, "source")
			if err := os.Mkdir(source, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(source, "payload")
			file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
			if err != nil {
				t.Fatal(err)
			}
			// Incompressible, allocated data. A sparse or all-zero fixture
			// would incorrectly make an ordinary deep copy look inexpensive.
			buffer := make([]byte, 128<<10)
			value := uint64(0x9e3779b97f4a7c15)
			for index := range buffer {
				value ^= value << 13
				value ^= value >> 7
				value ^= value << 17
				buffer[index] = byte(value)
			}
			for index := 0; index < 256; index++ {
				if _, err := file.Write(buffer); err != nil {
					file.Close()
					t.Fatal(err)
				}
			}
			if err := file.Sync(); err != nil {
				file.Close()
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
			var sourceFS unix.Statfs_t
			if err := unix.Statfs(source, &sourceFS); err != nil {
				t.Fatal(err)
			}
			// Independently ask the kernel whether this exact mount can clone.
			probe := filepath.Join(root, "kernel-reflink")
			probeOutput, probeErr := exec.Command("cp", "--reflink=always", "--", path, probe).CombinedOutput()
			if err := os.Remove(probe); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			input, err := openDirectory(source)
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close()
			var before, after unix.Statfs_t
			if err := unix.Statfs(root, &before); err != nil {
				t.Fatal(err)
			}
			start := time.Now()
			destination := filepath.Join(root, "snapshot")
			tree, err := cloneFilesystemTree(context.Background(), input, destination)
			duration := time.Since(start)
			if err != nil {
				t.Fatal(err)
			}
			if err := unix.Statfs(root, &after); err != nil {
				t.Fatal(err)
			}
			allocated := int64(before.Bavail-after.Bavail) * before.Bsize
			output, err := json.Marshal(map[string]any{
				"root": root, "filesystemMagic": fmt.Sprintf("%x", uint64(sourceFS.Type)),
				"kernelReflink": probeErr == nil, "fileBytes": tree.FileBytes,
				"filesystemAvailableDeltaBytes": allocated, "durationUS": duration.Microseconds(),
				"sourceReadUS": tree.SourceReadUS, "copyUS": tree.CopyUS,
				"targetReadUS": tree.TargetReadUS, "completeContentAndMetadataVerified": true,
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Log(string(output))
			if probeErr != nil {
				t.Fatalf("prepared filesystem cloning fell back to full copying: %s: %v", probeOutput, probeErr)
			}
			// Only the bounded filesystem is exclusively owned by this
			// fixture. Host counters also include unrelated concurrent work.
			if filepath.Base(root) == "cow-copy-bounded" && allocated > tree.FileBytes/4 {
				t.Fatalf("prepared clone allocated %d bytes for %d bytes of already prepared content", allocated, tree.FileBytes)
			}
			// Reflink must not become a hardlink or share mutable contents.
			if err := os.WriteFile(filepath.Join(destination, "payload"), []byte("independent fork"), 0600); err != nil {
				t.Fatal(err)
			}
			original, err := os.ReadFile(path)
			if err != nil || len(original) != 32<<20 || original[0] != buffer[0] {
				t.Fatal("writing the private clone changed its source", err)
			}
		})
	}
}
