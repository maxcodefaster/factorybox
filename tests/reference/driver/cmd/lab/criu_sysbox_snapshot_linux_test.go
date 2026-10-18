//go:build linux

package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func ramNativeFixture(t *testing.T, name string) (string, []byte) {
	t.Helper()
	if os.Getenv("CRIU_RAM_NATIVE_TEST") != "1" {
		t.Skip("owned native mount verification only")
	}
	t.Setenv("CRIU_SYSBOX_RAM_CAPTURE", "1")
	oldRoot, oldSnapshots := criuRoot, criuRAMSnapshots
	criuRoot, criuRAMSnapshots = t.TempDir(), nil
	t.Cleanup(func() {
		for _, snapshot := range criuRAMSnapshots {
			for i := len(snapshot.Bindings) - 1; i >= 0; i-- {
				_ = unix.Unmount(snapshot.Bindings[i], 0)
			}
			if snapshot.Mounted {
				_ = unix.Unmount(snapshot.Images, 0)
			}
		}
		criuRoot, criuRAMSnapshots = oldRoot, oldSnapshots
	})
	images := filepath.Join(criuRoot, "sysbox", name)
	if err := criuSysboxPrepareRAMCapture(images); err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, 32<<20)
	for i := range payload {
		payload[i] = byte(i ^ (i >> 12) ^ (i >> 19))
	}
	for name, data := range map[string][]byte{"pages-1.img": payload, "inventory.img": []byte("saved-metadata"), "runtime-config.json": []byte(`{"saved":true}`)} {
		path := filepath.Join(images, name)
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chown(path, 165536, 165536); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chown(images, 165536, 165536); err != nil {
		t.Fatal(err)
	}
	if err := criuSysboxRAMCaptureProof(images); err != nil {
		t.Fatal(err)
	}
	return images, payload
}

func ramAssertPayload(t *testing.T, path string, payload []byte) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, payload) {
		t.Fatalf("saved bytes differ: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	owner := info.Sys().(*syscall.Stat_t)
	if owner.Uid != 165536 || owner.Gid != 165536 || info.Mode().Perm() != 0600 {
		t.Fatalf("private ownership/mode lost: %+v", owner)
	}
}

func TestRAMNativeSuspendBackingAndRelease(t *testing.T) {
	images, payload := ramNativeFixture(t, "images")
	snapshot := criuRAMSnapshotFind(images)
	if snapshot.Allocated < uint64(len(payload)) {
		t.Fatal("snapshot RAM accounting omitted pages")
	}
	if err := criuSysboxReleaseRAMCapture(images); err != nil {
		t.Fatal(err)
	}
	if snapshot.Mounted {
		t.Fatal("snapshot still holds tmpfs")
	}
	var fs unix.Statfs_t
	if err := unix.Statfs(images, &fs); err != nil || fs.Type != snapshot.BackingFS {
		t.Fatalf("backing not published: %v", err)
	}
	ramAssertPayload(t, filepath.Join(images, "pages-1.img"), payload)
}

func TestRAMNativeForkReadonlyPagesPrivateMetadataAndDiskCopies(t *testing.T) {
	images, payload := ramNativeFixture(t, "images-again")
	copyImages := filepath.Join(criuRoot, "sysbox-copy", "images")
	if err := os.MkdirAll(filepath.Dir(copyImages), 0700); err != nil {
		t.Fatal(err)
	}
	handled, err := criuSysboxCloneRAMImages(images, copyImages)
	if err != nil || !handled {
		t.Fatalf("RAM fork: %v %t", err, handled)
	}
	a, err := os.Stat(filepath.Join(images, "pages-1.img"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.Stat(filepath.Join(copyImages, "pages-1.img"))
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(a, b) {
		t.Fatal("fork copied snapshot RAM")
	}
	for _, base := range []string{images, copyImages} {
		file, err := os.OpenFile(filepath.Join(base, "pages-1.img"), os.O_WRONLY, 0)
		if file != nil {
			file.Close()
		}
		if !errors.Is(err, unix.EROFS) {
			t.Fatalf("saved RAM is writable: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(copyImages, "inventory.img"), []byte("private-child-metadata"), 0600); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(filepath.Join(images, "inventory.img"))
	if err != nil || string(original) != "saved-metadata" {
		t.Fatal("fork metadata is shared")
	}
	if err := criuSysboxReleaseRAMCapture(images); err != nil {
		t.Fatal(err)
	}
	for _, base := range []string{images, copyImages} {
		ramAssertPayload(t, filepath.Join(base, "pages-1.img"), payload)
	}
	a, err = os.Stat(filepath.Join(images, "pages-1.img"))
	if err != nil {
		t.Fatal(err)
	}
	b, err = os.Stat(filepath.Join(copyImages, "pages-1.img"))
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(a, b) {
		t.Fatal("disk copies share a writable inode")
	}
	file, err := os.OpenFile(filepath.Join(copyImages, "pages-1.img"), os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = file.WriteAt([]byte{255}, 0)
	file.Close()
	if err != nil {
		t.Fatal(err)
	}
	ramAssertPayload(t, filepath.Join(images, "pages-1.img"), payload)
	child, err := os.ReadFile(filepath.Join(copyImages, "inventory.img"))
	if err != nil || string(child) != "private-child-metadata" {
		t.Fatal("published copy metadata changed")
	}
}

func TestRAMNativeRejectNonRegularAndExistingBacking(t *testing.T) {
	images, payload := ramNativeFixture(t, "images")
	outside := filepath.Join(criuRoot, "outside")
	if err := os.WriteFile(outside, []byte("untouched"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(images, "unexpected.img")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, err := criuRAMExportDirectory(images, images+"-disk", "", nil, false); err == nil {
		t.Fatal("accepted snapshot symlink")
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(images+"-disk", 0700); err != nil {
		t.Fatal(err)
	}
	if err := criuSysboxReleaseRAMCapture(images); err == nil {
		t.Fatal("adopted ambiguous existing backing")
	}
	ramAssertPayload(t, filepath.Join(images, "pages-1.img"), payload)
	data, err := os.ReadFile(outside)
	if err != nil || string(data) != "untouched" {
		t.Fatal("symlink target changed")
	}
	fifo := filepath.Join(criuRoot, "pipe")
	if err := unix.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := criuRAMExportFile(fifo, filepath.Join(criuRoot, "fifo-copy"), "", "", true, nil); err == nil {
		t.Fatal("accepted FIFO")
	}
}

func TestRAMNativeRejectCorruptBackingClone(t *testing.T) {
	if os.Getenv("CRIU_RAM_NATIVE_TEST") != "1" {
		t.Skip("owned native verification only")
	}
	dir := t.TempDir()
	source, clone, target := filepath.Join(dir, "source"), filepath.Join(dir, "bad-clone"), filepath.Join(dir, "target")
	if err := os.WriteFile(source, []byte("saved"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(clone, []byte("wrong"), 0600); err != nil {
		t.Fatal(err)
	}
	hash, err := criuRAMHashFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := criuRAMExportFile(source, target, clone, hash, true, nil); err == nil {
		t.Fatal("accepted corrupt saved bytes")
	}
	data, err := os.ReadFile(source)
	if err != nil || string(data) != "saved" {
		t.Fatal("source changed after rejection")
	}
}

func TestRAMNativeCloneFallbackAcrossFilesystems(t *testing.T) {
	images, payload := ramNativeFixture(t, "images")
	source := filepath.Join(images, "pages-1.img")
	hash, err := criuRAMHashFile(source)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(criuRoot, "fallback.img")
	file, err := criuRAMExportFile(source, target, source, hash, true, nil)
	if err != nil || file.SHA256 != hash {
		t.Fatalf("cross-filesystem fallback: %v", err)
	}
	ramAssertPayload(t, target, payload)
}

func TestRAMNativeRejectReleaseWithRetainedReader(t *testing.T) {
	images, payload := ramNativeFixture(t, "images")
	reader, err := os.Open(filepath.Join(images, "pages-1.img"))
	if err != nil {
		t.Fatal(err)
	}
	err = criuSysboxReleaseRAMCapture(images)
	reader.Close()
	if !errors.Is(err, unix.EBUSY) {
		t.Fatalf("retained RAM reader did not block release: %v", err)
	}
	if criuRAMSnapshotFind(images) == nil {
		t.Fatal("reported RAM release with retained reader")
	}
	ramAssertPayload(t, filepath.Join(images, "pages-1.img"), payload)
}
