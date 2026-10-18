package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestDescendantParentPinPreservesMountedPayload(t *testing.T) {
	source, head, _ := imageGraphFixture(t)
	if err := os.Chown(head, 165536, 165536); err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte{0x79}, 3*4096)
	original := filepath.Join(source, "original-pages.img")
	target := filepath.Join(head, "pages-1.img")
	if err := os.WriteFile(original, payload, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := criuRAMReadonlyBind(original, target); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := unix.Unmount(target, 0); err != nil {
			t.Error("original payload mount cleanup", err)
		}
	})
	mountID, err := epochReplayMountID(unix.AT_FDCWD, target, unix.AT_SYMLINK_NOFOLLOW)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	pins, err := descendantPinSnapshotParents(descendantLayout{Root: source}, head)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := pins.close(); err != nil {
			t.Error("owned parent pin cleanup", err)
		}
	})
	current, statErr := os.Stat(target)
	actual, readErr := os.ReadFile(target)
	if statErr != nil || readErr != nil || !os.SameFile(before, current) || !bytes.Equal(actual, payload) {
		t.Fatalf("parent pin hid the real mounted payload: bytes=%d want=%d stat=%v read=%v",
			len(actual), len(payload), statErr, readErr)
	}
	writable, err := os.OpenFile(target, os.O_WRONLY, 0)
	if writable != nil {
		writable.Close()
	}
	if !errors.Is(err, unix.EROFS) {
		t.Fatal("parent payload lost write protection", err)
	}
	if err := pins.close(); err != nil {
		t.Fatal(err)
	}
	afterMount, err := epochReplayMountID(unix.AT_FDCWD, target, unix.AT_SYMLINK_NOFOLLOW)
	after, statErr := os.Stat(target)
	actual, readErr = os.ReadFile(target)
	if err != nil || statErr != nil || readErr != nil || afterMount != mountID ||
		!os.SameFile(before, after) || !bytes.Equal(actual, payload) {
		t.Fatal("closing parent pins changed the pre-existing payload mount", err, statErr, readErr)
	}
}

func TestDescendantParentPinRejectsWritableMountedPayload(t *testing.T) {
	source, head, _ := imageGraphFixture(t)
	if err := os.Chown(head, 165536, 165536); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(source, "writable.img")
	target := filepath.Join(head, "pages-1.img")
	if err := os.WriteFile(original, []byte("retained"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mount(original, target, "", unix.MS_BIND, ""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := unix.Unmount(target, 0); err != nil {
			t.Error(err)
		}
	})
	before, err := epochReplayMountID(unix.AT_FDCWD, target, unix.AT_SYMLINK_NOFOLLOW)
	if err != nil {
		t.Fatal(err)
	}
	if pins, err := descendantPinSnapshotParents(descendantLayout{Root: source}, head); err == nil {
		pins.close()
		t.Fatal("accepted a writable mounted snapshot payload")
	}
	after, err := epochReplayMountID(unix.AT_FDCWD, target, unix.AT_SYMLINK_NOFOLLOW)
	actual, readErr := os.ReadFile(target)
	if err != nil || readErr != nil || before != after || string(actual) != "retained" {
		t.Fatal("rejected parent changed the original payload mount", err, readErr)
	}
}
