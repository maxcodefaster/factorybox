//go:build linux

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func committedNativeFixture(t *testing.T) (string, map[string]string) {
	t.Helper()
	if os.Getenv("RUNTIME_FORK_COMMITTED_TEST") != "1" {
		t.Skip("owned native filesystem fixture only")
	}
	t.Setenv("RUNTIME_FORK_COMMITTED_SNAPSHOT", "1")
	t.Setenv("RUNTIME_FORK_RICH_EPOCH_CHAIN", "1")
	t.Setenv("RUNTIME_FORK_EPOCH_REBASE", "0")
	path := "/experiment/sysbox/images-again"
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mount("owned-committed-fixture", path, "tmpfs",
		unix.MS_NOSUID|unix.MS_NODEV|unix.MS_NOEXEC, "size=1m,mode=0700"); err != nil {
		t.Fatal(err)
	}
	old := criuRAMSnapshots
	criuRAMSnapshots = append(criuRAMSnapshots, &criuRAMSnapshot{Images: path, Limit: 1 << 20, Mounted: true})
	t.Cleanup(func() {
		if err := criuCloseCommittedSnapshot(path); err != nil {
			t.Error(err)
		}
		if err := unix.Unmount(path, 0); err != nil {
			t.Error(err)
		}
		os.Remove(path)
		criuRAMSnapshots = old
	})
	expected := map[string]string{}
	for name, data := range map[string][]byte{"pages-1.img": []byte(strings.Repeat("x", 64)), "inventory.img": []byte("ok")} {
		if err := os.WriteFile(filepath.Join(path, name), data, 0600); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(data)
		expected[name] = hex.EncodeToString(digest[:])
	}
	writer, err := os.OpenFile(path+"/pages-1.img", os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	err = criuCommitSnapshot(path)
	writer.Close()
	if !errors.Is(err, unix.EBUSY) || committedSnapshots[path] != nil {
		t.Fatalf("published while writer was still open: %v", err)
	}
	if err := criuCommitSnapshot(path); err != nil {
		t.Fatal(err)
	}
	if err := criuRegisterCommittedParent(path, expected); err != nil {
		t.Fatal(err)
	}
	return path, expected
}

func TestCommittedNativeVerificationReadsMetadataAndRejectsWrongExpectations(t *testing.T) {
	path, expected := committedNativeFixture(t)
	result := checkEpochParentFiles(context.Background(), path, expected, 1<<20)
	if result.Err != nil || result.Files != 2 || result.Bytes != 66 || result.ReadBytes != 2 ||
		result.KernelFiles != 1 || result.KernelBytes != 64 {
		t.Fatalf("wrong actual/certified work: %+v", result)
	}
	for _, name := range []string{"pages-1.img", "inventory.img"} {
		wrong := map[string]string{}
		for key, value := range expected {
			wrong[key] = value
		}
		wrong[name] = strings.Repeat("0", 64)
		if result := checkEpochParentFiles(context.Background(), path, wrong, 1<<20); result.Err == nil {
			t.Fatalf("accepted wrong %s digest expectation", name)
		}
	}
}

func TestCommittedNativeVerificationRejectsPolicyAndDataChanges(t *testing.T) {
	for _, change := range []string{"writable", "same-inode", "replacement"} {
		t.Run(change, func(t *testing.T) {
			path, expected := committedNativeFixture(t)
			if err := unix.Mount("", path, "", unix.MS_REMOUNT|unix.MS_NOSUID|unix.MS_NODEV|unix.MS_NOEXEC, ""); err != nil {
				t.Fatal(err)
			}
			if change != "writable" {
				time.Sleep(2 * time.Millisecond)
				if change == "replacement" {
					if err := os.Remove(path + "/pages-1.img"); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.WriteFile(path+"/pages-1.img", []byte(strings.Repeat("y", 64)), 0600); err != nil {
					t.Fatal(err)
				}
				err := unix.Mount("", path, "", unix.MS_REMOUNT|unix.MS_RDONLY|unix.MS_NOSUID|unix.MS_NODEV|unix.MS_NOEXEC, "")
				if change == "replacement" {
					// The held old inode is now unlinked. The kernel refuses
					// to republish this filesystem while that inode is pinned.
					if !errors.Is(err, unix.EBUSY) {
						t.Fatalf("replaced pinned inode did not block republication: %v", err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
			}
			if result := checkEpochParentFiles(context.Background(), path, expected, 1<<20); result.Err == nil {
				t.Fatalf("accepted %s after publication", change)
			}
		})
	}
}
