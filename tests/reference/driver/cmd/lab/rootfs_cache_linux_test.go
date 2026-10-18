package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestRootfsCacheNative(t *testing.T) {
	if os.Getenv("ROOTFS_CACHE_NATIVE") != "1" {
		t.Skip("requires two owned native Btrfs volumes and private mount namespace")
	}
	const cache = "/opt/probe-rootfs-cache"
	source := filepath.Join(cache, "source")
	if err := os.Mkdir(source, 0755); err != nil {
		t.Fatal(err)
	}
	payload := []byte(strings.Repeat("complete-private-rootfs-state", 1024))
	if err := os.WriteFile(filepath.Join(source, "state"), payload, 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(filepath.Join(source, "state"), 12345, 23456); err != nil {
		t.Fatal(err)
	}
	if err := unix.Setxattr(filepath.Join(source, "state"), "user.rootfs-oracle", []byte("retained-xattr"), 0); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(source, "state"), filepath.Join(source, "hardlink")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("state", filepath.Join(source, "symlink")); err != nil {
		t.Fatal(err)
	}
	var saved unix.Stat_t
	if err := unix.Lstat(source, &saved); err != nil {
		t.Fatal(err)
	}
	provenance := rootfsCacheProvenance{
		Version: 1, BaseImageID: "sha256:041140e968e8efdbc6b2f33651f84bc19c13593c6d3f527e90af648f967dd52f",
		Source: "source", Device: uint64(saved.Dev), Inode: saved.Ino, Mode: saved.Mode,
		UID: saved.Uid, GID: saved.Gid, MtimeNS: saved.Mtim.Sec*1e9 + saved.Mtim.Nsec,
		CtimeNS: saved.Ctim.Sec*1e9 + saved.Ctim.Nsec, Entries: 3,
		PayloadBytes: int64(len(payload) * 2), ManifestSHA: strings.Repeat("a", 64),
	}
	data, err := json.Marshal(provenance)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "template.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	t.Setenv("RUNTIME_FORK_ROOTFS_CACHE", cache)
	t.Setenv("RUNTIME_FORK_ROOTFS_CACHE_SHA256", hex.EncodeToString(sum[:]))
	readonly := func(value bool) {
		flags := uintptr(unix.MS_REMOUNT | unix.MS_BIND)
		if value {
			flags |= unix.MS_RDONLY
		}
		if err := unix.Mount("", cache, "", flags, ""); err != nil {
			t.Fatal(err)
		}
	}
	reject := func(t *testing.T, suffix, message string) {
		t.Helper()
		target := filepath.Join("/experiment", suffix)
		err := criuUnpackRoot(target, 3*time.Second)
		if err == nil || !strings.Contains(err.Error(), message) {
			t.Fatalf("unsafe cache was not rejected at the actual startup seam: %v", err)
		}
		entries, readErr := os.ReadDir(target)
		if readErr != nil || len(entries) != 0 {
			t.Fatalf("rejected cache copied data before admission: %v %v", entries, readErr)
		}
	}
	t.Run("writable_source_rejected", func(t *testing.T) {
		reject(t, "writable-rejected", "readonly mount")
	})
	readonly(true)
	defer readonly(false)
	t.Run("wrong_provenance_rejected", func(t *testing.T) {
		t.Setenv("RUNTIME_FORK_ROOTFS_CACHE_SHA256", strings.Repeat("0", 64))
		reject(t, "provenance-rejected", "pinned native build")
	})
	t.Run("complete_cross_volume_clone_and_mutation", func(t *testing.T) {
		target := "/experiment/complete"
		if err := criuUnpackRoot(target, 3*time.Second); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"state", "hardlink", "symlink"} {
			actual, err := os.ReadFile(filepath.Join(target, name))
			if err != nil || string(actual) != string(payload) {
				t.Fatalf("complete rootfs content changed for %s: %v", name, err)
			}
		}
		var original, copy, link unix.Stat_t
		if err := unix.Lstat(filepath.Join(source, "state"), &original); err != nil {
			t.Fatal(err)
		}
		if err := unix.Lstat(filepath.Join(target, "state"), &copy); err != nil {
			t.Fatal(err)
		}
		if err := unix.Lstat(filepath.Join(target, "hardlink"), &link); err != nil {
			t.Fatal(err)
		}
		if original.Dev == copy.Dev && original.Ino == copy.Ino || copy.Ino != link.Ino ||
			original.Mode != copy.Mode || original.Uid != copy.Uid || original.Gid != copy.Gid {
			t.Fatal("clone inode/owner/mode/hardlink relationships changed")
		}
		attribute := make([]byte, 64)
		n, err := unix.Getxattr(filepath.Join(target, "state"), "user.rootfs-oracle", attribute)
		if err != nil || string(attribute[:n]) != "retained-xattr" {
			t.Fatalf("clone lost the original xattr: %v", err)
		}
		if err := os.WriteFile(filepath.Join(target, "state"), []byte("child-diverged"), 0640); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"state", "hardlink"} {
			actual, err := os.ReadFile(filepath.Join(source, name))
			if err != nil || string(actual) != string(payload) {
				t.Fatalf("child mutation changed source %s: %v", name, err)
			}
		}
	})
	t.Run("foreign_successor_rejected", func(t *testing.T) {
		readonly(false)
		if err := os.Rename(source, source+"-retained"); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(source, 0755); err != nil {
			t.Fatal(err)
		}
		readonly(true)
		reject(t, "successor-rejected", "identity changed")
		readonly(false)
		if err := os.Remove(source); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(source+"-retained", source); err != nil {
			t.Fatal(err)
		}
		readonly(true)
	})
	t.Run("unsupported_filesystem_rejected", func(t *testing.T) {
		if err := unix.Mount("tmpfs", source, "tmpfs", 0, "size=1m"); err != nil {
			t.Fatal(err)
		}
		defer unix.Unmount(source, 0)
		if err := unix.Mount("", source, "", unix.MS_REMOUNT|unix.MS_RDONLY, ""); err != nil {
			t.Fatal(err)
		}
		reject(t, "filesystem-rejected", "tested Btrfs readonly mount")
	})
}
