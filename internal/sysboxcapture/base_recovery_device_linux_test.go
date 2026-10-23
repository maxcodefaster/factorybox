//go:build linux

package sysboxcapture

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

func TestFilesystemBaseDeviceRecoveryNative(t *testing.T) {
	parent := os.Getenv("FACTORYBOX_DEVICE_RECOVERY_TEST_ROOT")
	if os.Getenv("FACTORYBOX_NATIVE_RETAINED_CAPTURE") != "1" || parent == "" {
		t.Skip("owned bounded ext4 recovery fixture only")
	}
	home, err := os.MkdirTemp(parent, "base-device-recovery-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(home); err != nil {
			t.Error(err)
		}
	})
	for _, name := range []string{"input", "old", "new"} {
		if err := os.Mkdir(home+"/"+name, 0700); err != nil {
			t.Fatal(err)
		}
	}
	payload := []byte("original independently recovered bytes\n")
	if err := os.WriteFile(home+"/input/payload", payload, 0640); err != nil {
		t.Fatal(err)
	}
	if err := unix.Setxattr(home+"/input/payload", "user.factorybox", []byte("original metadata"), 0); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	original, err := PrepareFilesystemBase(ctx, home+"/input", home+"/old/rootfs")
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Unmount(original.Saved, 0); err != nil {
		t.Fatal(err)
	}
	saved := original
	identity := *original.Immutable
	identity.Device++ // Only a trusted caller may translate an old device identity.
	saved.Immutable = &identity
	before, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	if rejected, err := RecoverFilesystemBase(ctx, saved, home+"/new/strict"); err == nil || rejected.Immutable != nil {
		t.Fatal("standalone recovery admitted a changed device without authority", err)
	}
	allow := func(tree FilesystemTree, current uint64) error {
		if tree.Immutable == nil || tree.Immutable.Root != original.Saved || tree.Immutable.Device != identity.Device || current != original.Immutable.Device {
			return fmt.Errorf("unexpected fixture device translation")
		}
		return nil
	}
	if rejected, err := recoverFilesystemBase(ctx, saved, home+"/new/denied", func(FilesystemTree, uint64) error {
		return fmt.Errorf("fixture authority refused")
	}); err == nil || rejected.Immutable != nil {
		t.Fatal("recovery ignored its independent verifier refusal", err)
	}
	recovered, err := recoverFilesystemBase(ctx, saved, home+"/new/translated", allow)
	if err != nil || !sameFilesystemTree(recovered, saved) || recovered.Immutable == nil || recovered.Immutable.Device != original.Immutable.Device {
		t.Fatal("verified translation failed complete independent reimport", recovered, err)
	}
	t.Cleanup(func() {
		if err := VerifyImmutableTree(recovered.Saved, recovered); err != nil {
			t.Error(err)
			return
		}
		if err := unix.Unmount(recovered.Saved, 0); err != nil {
			t.Error(err)
		}
	})
	if err := unix.Mount(original.Saved, original.Saved, "", unix.MS_BIND, ""); err != nil {
		t.Fatal(err)
	}
	called := false
	invalid, rejected := recoverFilesystemBase(ctx, saved, home+"/new/foreign", func(tree FilesystemTree, current uint64) error {
		called = true
		return allow(tree, current)
	})
	unmountErr := unix.Unmount(original.Saved, 0)
	if rejected == nil || invalid.Immutable != nil || called || unmountErr != nil {
		t.Fatal("foreign covering mount bypassed admission or reached device authority", invalid, rejected, called, unmountErr)
	}
	path := original.Saved + "/payload"
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, bytes.Repeat([]byte{'x'}, len(payload)), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if invalid, err := recoverFilesystemBase(ctx, saved, home+"/new/corrupt", allow); err == nil || invalid.Immutable != nil {
		t.Fatal("authorized device translation bypassed complete content verification", invalid, err)
	}
	after, err := json.Marshal(saved)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("translation rewrote its saved source manifest", err)
	}
	if err := os.RemoveAll(original.Saved); err != nil {
		t.Fatal(err)
	}
	if err := VerifyImmutableTree(recovered.Saved, recovered); err != nil {
		t.Fatal("new base depends on original backing", err)
	}
	data, err := os.ReadFile(recovered.Saved + "/payload")
	if err != nil || !bytes.Equal(data, payload) {
		t.Fatal("old backing mutation/deletion changed the new base", err)
	}
}
