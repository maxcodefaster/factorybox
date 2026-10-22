//go:build linux

package sysboxcapture

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

func TestFilesystemBaseRecoveryNative(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_RETAINED_CAPTURE") != "1" {
		t.Skip("requires an owned native immutable-base fixture")
	}
	home, err := os.MkdirTemp("/var/lib/sysbox", "base-recovery-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	for _, name := range []string{"input", "old", "new"} {
		if err := os.Mkdir(home+"/"+name, 0700); err != nil {
			t.Fatal(err)
		}
	}
	payload := []byte("independent persisted base content\n")
	if err := os.WriteFile(home+"/input/payload", payload, 0640); err != nil {
		t.Fatal(err)
	}
	if err := unix.Setxattr(home+"/input/payload", "user.factorybox", []byte("saved-metadata"), 0); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	saved, err := PrepareFilesystemBase(ctx, home+"/input", home+"/old/rootfs")
	if err != nil {
		t.Fatal(err)
	}
	before, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyImmutableTree(saved.Saved, saved); err != nil {
		t.Fatal(err)
	}
	if err := unix.Unmount(saved.Immutable.Root, 0); err != nil {
		t.Fatal(err)
	}
	if err := VerifyImmutableTree(saved.Saved, saved); err == nil {
		t.Fatal("lost read-only mount was still accepted as immutable authority")
	}
	if err := os.RemoveAll(home + "/input"); err != nil {
		t.Fatal(err)
	}
	recovered, err := RecoverFilesystemBase(ctx, saved, home+"/new/rootfs")
	if err != nil || !sameFilesystemTree(recovered, saved) || recovered.Immutable == nil || recovered.Alias != nil {
		t.Fatal("complete base reimport after lost mount and original source deletion", recovered, err)
	}
	t.Cleanup(func() {
		if err := VerifyImmutableTree(recovered.Saved, recovered); err != nil {
			t.Error("refuse cleanup of changed recovered base", err)
			return
		}
		if err := unix.Unmount(recovered.Saved, 0); err != nil {
			t.Error(err)
		}
	})
	if recovered.Immutable.Device == saved.Immutable.Device && recovered.Immutable.Inode == saved.Immutable.Inode {
		t.Fatal("reimport reused the vanished base's kernel identity")
	}
	after, err := json.Marshal(saved)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("base recovery rewrote its source manifest", err)
	}
	rawPayload := saved.Immutable.Root + "/payload"
	t.Run("foreign-raw-root-mount", func(t *testing.T) {
		if err := unix.Mount(saved.Immutable.Root, saved.Immutable.Root, "", unix.MS_BIND, ""); err != nil {
			t.Fatal(err)
		}
		invalid, rejected := RecoverFilesystemBase(ctx, saved, home+"/new/foreign-root")
		unmountErr := unix.Unmount(saved.Immutable.Root, 0)
		if rejected == nil {
			// Retire only the exact new result this negative control made.
			if err := VerifyImmutableTree(invalid.Saved, invalid); err != nil {
				t.Fatal("refuse cleanup of changed negative-control output", err)
			}
			if err := unix.Unmount(invalid.Saved, 0); err != nil {
				t.Fatal(err)
			}
			t.Error("foreign covering mount was accepted as canonical persistent backing")
		}
		if unmountErr != nil {
			t.Fatal(unmountErr)
		}
	})
	info, err := os.Stat(rawPayload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rawPayload, bytes.Repeat([]byte{'x'}, len(payload)), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(rawPayload, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	invalid, err := RecoverFilesystemBase(ctx, saved, home+"/new/corrupt")
	if err == nil || invalid.Immutable != nil {
		t.Fatal("corrupt canonical backing acquired a fresh immutable identity", invalid, err)
	}
	if err := os.RemoveAll(saved.Immutable.Root); err != nil {
		t.Fatal(err)
	}
	if err := VerifyImmutableTree(recovered.Saved, recovered); err != nil {
		t.Fatal("recovered base depends on old backing", err)
	}
	data, err := os.ReadFile(recovered.Saved + "/payload")
	if err != nil || !bytes.Equal(data, payload) {
		t.Fatal("old backing mutation/deletion changed recovered data", err)
	}
}
