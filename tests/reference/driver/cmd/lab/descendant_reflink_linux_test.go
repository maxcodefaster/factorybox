package main

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func TestDescendantReflinkNative(t *testing.T) {
	if os.Getenv("DESCENDANT_REFLINK_NATIVE") != "1" {
		t.Skip("one isolated existing-worker Btrfs volume")
	}
	source, _ := descendantOwnedLayout("copy-sysbox")
	if _, err := os.Lstat(source.Root); !os.IsNotExist(err) {
		t.Fatal("fixture must own absent source root", err)
	}
	if err := os.MkdirAll(source.Root, 0755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(source.Root) })
	fixture := func(t *testing.T, serial int) (string, descendantLayout, map[string][]byte, map[string]string) {
		t.Helper()
		images := fmt.Sprintf("%s/images-%032x", source.Root, serial)
		target, _ := descendantOwnedLayout(fmt.Sprintf("branch-%032x", serial))
		if err := os.MkdirAll(images, 0700); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Lstat(target.Root); !os.IsNotExist(err) {
			t.Fatal("fixture must own absent target root", err)
		}
		if err := os.MkdirAll(target.Root, 0755); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.RemoveAll(target.Root) })
		payloads := map[string][]byte{
			"inventory.img": []byte("complete inventory"), "pstree.img": []byte("owned tasks"),
			"runtime-config.json": []byte(`{"root":"owned"}`), "descriptors.json": []byte(`["/dev/null"]`),
			"core-1.img": []byte("private task metadata"), "pages-1.img": bytes.Repeat([]byte("native-page-data"), 1<<18),
			"dump.log": []byte("retained diagnostics"), "stats-dump": []byte("native counters"),
		}
		expected := map[string]string{}
		for name, data := range payloads {
			path := filepath.Join(images, name)
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chown(path, 165536, 165536); err != nil {
				t.Fatal(err)
			}
			if filepath.Ext(name) == ".img" || filepath.Ext(name) == ".json" {
				expected[name] = fmt.Sprintf("%x", sha256.Sum256(data))
			}
		}
		if err := os.Chown(images, 165536, 165536); err != nil {
			t.Fatal(err)
		}
		return images, target, payloads, expected
	}
	t.Run("kernel_clone_matches_independent_full_bytes_and_diverges", func(t *testing.T) {
		images, target, payloads, expected := fixture(t, 1)
		protected, captured, err := descendantProtectCapture(source, images)
		if err != nil {
			t.Fatal(err)
		}
		defer protected.close(false)
		if !reflect.DeepEqual(captured, expected) {
			t.Fatal("initial held source hash differs from original writer")
		}
		cloned, proof, err := descendantCloneProtectedCapture(protected, target, captured)
		if err != nil || !reflect.DeepEqual(cloned, expected) {
			t.Fatal("kernel clone verification failed", proof, err)
		}
		if proof["target_page_hash_read_bytes"] != int64(0) || proof["kernel_reflink_page_files"] != 1 {
			t.Fatal("unexpected clone verification path", proof)
		}
		for _, path := range []string{images, target.Root + "/images"} {
			full, err := descendantSnapshotManifest(path)
			if err != nil || !reflect.DeepEqual(full, expected) {
				t.Fatal("independent complete byte oracle differs", path, err)
			}
		}
		for name := range expected {
			a, aErr := os.Stat(filepath.Join(images, name))
			b, bErr := os.Stat(filepath.Join(target.Root, "images", name))
			if aErr != nil || bErr != nil || os.SameFile(a, b) {
				t.Fatal("clone shares mutable inode", name, aErr, bErr)
			}
		}
		for _, name := range []string{"pages-1.img", "core-1.img"} {
			if err := os.WriteFile(filepath.Join(target.Root, "images", name), []byte("independent clone write"), 0600); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(filepath.Join(images, name))
			if err != nil || !bytes.Equal(before, payloads[name]) {
				t.Fatal("clone write changed source", name, err)
			}
		}
		if err := protected.close(true); err != nil {
			t.Fatal(err)
		}
		cloneBefore, err := os.ReadFile(target.Root + "/images/pages-1.img")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(images+"/pages-1.img", []byte("independent source write"), 0600); err != nil {
			t.Fatal(err)
		}
		cloneAfter, err := os.ReadFile(target.Root + "/images/pages-1.img")
		if err != nil || !bytes.Equal(cloneBefore, cloneAfter) {
			t.Fatal("source write changed clone", err)
		}
		if _, _, err := descendantCloneProtectedCapture(protected, target, captured); err == nil {
			t.Fatal("closed source certificate admitted another clone")
		}
		t.Log(asJSON(proof))
	})
	t.Run("existing_writable_fd_rejects_publication", func(t *testing.T) {
		images, _, _, _ := fixture(t, 2)
		writer, err := os.OpenFile(images+"/pages-1.img", os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer writer.Close()
		protected, _, err := descendantProtectCapture(source, images)
		if protected != nil || !errors.Is(err, unix.EAGAIN) {
			t.Fatal("existing writable FD was admitted", err)
		}
	})
	t.Run("write_break_rejects_clone_without_modifying_original", func(t *testing.T) {
		images, target, _, expected := fixture(t, 3)
		alias := images + "/page-alias"
		if err := os.Link(images+"/pages-1.img", alias); err != nil {
			t.Fatal(err)
		}
		protected, captured, err := descendantProtectCapture(source, images)
		if err != nil {
			t.Fatal(err)
		}
		defer protected.close(false)
		fd, err := unix.Open(alias, unix.O_WRONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
		if err == nil {
			unix.Close(fd)
			t.Fatal("writer bypassed source read lease")
		}
		if !errors.Is(err, unix.EAGAIN) {
			t.Fatal("writer rejected for another reason", err)
		}
		if _, _, err := descendantCloneProtectedCapture(protected, target, captured); err == nil {
			t.Fatal("breaking source lease certified clone")
		}
		hash, err := descendantLeaseHash(protected.Pages["pages-1.img"].File)
		if err != nil || hash != expected["pages-1.img"] {
			t.Fatal("blocked writer changed original captured bytes", err)
		}
		if _, err := os.Lstat(target.Root + "/images"); !os.IsNotExist(err) {
			t.Fatal("invalid certificate created target images", err)
		}
	})
	t.Run("cross_filesystem_has_no_copy_fallback", func(t *testing.T) {
		images, _, _, _ := fixture(t, 4)
		protected, _, err := descendantProtectCapture(source, images)
		if err != nil {
			t.Fatal(err)
		}
		defer protected.close(false)
		file, err := os.CreateTemp("/dev/shm", "owned-reflink-test-*")
		if err != nil {
			t.Fatal(err)
		}
		path := file.Name()
		file.Close()
		os.Remove(path)
		t.Cleanup(func() { os.Remove(path) })
		err = descendantCloneLeasedPage(protected.Pages["pages-1.img"], path)
		if !errors.Is(err, syscall.EXDEV) {
			t.Fatal("cross-filesystem clone silently copied or failed differently", err)
		}
	})
}
