package main

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestDescendantRAMTransportNative(t *testing.T) {
	if os.Getenv("DESCENDANT_RAM_NATIVE") != "1" {
		t.Skip("one isolated privileged existing-worker container only")
	}
	t.Setenv("RUNTIME_FORK_DESCENDANT_COMPLETE_RAM", "1")
	source, _ := descendantOwnedLayout("copy-sysbox")
	target, _ := descendantOwnedLayout("branch-0123456789abcdef0123456789abcdef")
	images := source.Root + "/images-fedcba9876543210fedcba9876543210"
	for _, bad := range []string{source.Root + "/images", source.Root + "/images-1234",
		source.Root + "/../sysbox/images-fedcba9876543210fedcba9876543210",
		"/experiment/unrelated/images-fedcba9876543210fedcba9876543210"} {
		if err := descendantRAMPath(source, bad, true); err == nil {
			t.Fatal("foreign or ambiguous capture accepted", bad)
		}
	}
	for _, layout := range []descendantLayout{source, target} {
		if _, err := os.Lstat(layout.Root); !os.IsNotExist(err) {
			t.Fatal("fixture must own absent roots", layout.Root)
		}
		if err := os.MkdirAll(layout.Root, 0755); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.RemoveAll(layout.Root) })
	}
	state, err := descendantPrepareRAM(source, images, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := state.cleanup(); err != nil {
			t.Error(err)
		}
	})
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
	writer, err := os.OpenFile(images+"/pages-1.img", os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.seal(); !errors.Is(err, unix.EBUSY) {
		writer.Close()
		t.Fatal("live writer did not block publication", err)
	}
	writer.Close()
	if err := state.seal(); err != nil {
		t.Fatal(err)
	}
	if err := state.clone(target, expected); err != nil {
		t.Fatal(err)
	}
	if err := state.clone(target, expected); err == nil {
		t.Fatal("capture accepted a second mutable clone")
	}
	if !reflect.DeepEqual(state.Cloned, expected) {
		t.Fatal("copy-time metadata readback/immutable-page proof omitted original bytes")
	}
	for name := range expected {
		a, err := os.Stat(filepath.Join(images, name))
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.Stat(filepath.Join(target.Root, "images", name))
		if err != nil {
			t.Fatal(err)
		}
		if os.SameFile(a, b) != criuRAMPageName.MatchString(name) {
			t.Fatal("immutable pages/private metadata ownership differs", name)
		}
	}
	for _, path := range []string{images + "/pages-1.img", target.Root + "/images/pages-1.img"} {
		file, err := os.OpenFile(path, os.O_WRONLY, 0)
		if file != nil {
			file.Close()
		}
		if !errors.Is(err, unix.EROFS) {
			t.Fatal("published page alias admitted writes", path, err)
		}
	}
	if err := os.WriteFile(target.Root+"/images/core-1.img", []byte("clone metadata changes independently"), 0600); err != nil {
		t.Fatal("clone metadata was not private/writable", err)
	}
	original, err := os.ReadFile(images + "/core-1.img")
	if err != nil || !bytes.Equal(original, payloads["core-1.img"]) {
		t.Fatal("clone metadata write crossed into original snapshot", err)
	}
	if err := os.WriteFile(target.Root+"/images/core-1.img", payloads["core-1.img"], 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{images, target.Root + "/images"} {
		got, err := descendantSnapshotManifest(path)
		if err != nil || !reflect.DeepEqual(got, expected) {
			t.Fatal("complete original writer/clone hashes differ", path, got, err)
		}
	}
	reader, err := os.Open(images + "/pages-1.img")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reader.Close() })
	if _, err := state.release(expected); err == nil || !strings.Contains(err.Error(), "live snapshot page reader") {
		t.Fatal("open page reader did not reject retirement", err)
	}
	mapping, err := unix.Mmap(int(reader.Fd()), 0, 4096, unix.PROT_READ, unix.MAP_PRIVATE)
	reader.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := state.release(expected); err == nil || !strings.Contains(err.Error(), "live snapshot page reader") {
		unix.Munmap(mapping)
		t.Fatal("page mapping without a descriptor did not reject retirement", err)
	}
	if err := unix.Munmap(mapping); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{images + "-disk", target.Root + "/images-disk"} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatal("reader rejection created backing before admission", path, err)
		}
	}
	// A mount reference alone can retain the retired tmpfs inode. This exact
	// owned alias has no readers and must lose its physical payload too.
	hiddenAlias := filepath.Join(t.TempDir(), "held-page")
	if err := os.WriteFile(hiddenAlias, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := criuRAMReadonlyBind(images+"/pages-1.img", hiddenAlias); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { unix.Unmount(hiddenAlias, 0) })
	released, err := state.release(expected)
	if err != nil || released["released"] != true ||
		released["actual_shmem_released_bytes"].(uint64) < state.Allocated {
		t.Fatal("actual RAM retirement failed", released, err)
	}
	if info, err := os.Stat(hiddenAlias); err != nil || info.Size() != 0 {
		t.Fatal("retired inode retained bytes through another owned mount", info, err)
	}
	for _, path := range []string{images, target.Root + "/images"} {
		got, err := descendantSnapshotManifest(path)
		if err != nil || !reflect.DeepEqual(got, expected) {
			t.Fatal("backed payload changed after real RAM release", path, err)
		}
	}
	a, _ := os.Stat(images + "/pages-1.img")
	b, _ := os.Stat(target.Root + "/images/pages-1.img")
	if os.SameFile(a, b) {
		t.Fatal("retired page payload backing inodes are not private")
	}
	t.Log(asJSON(released))
}
