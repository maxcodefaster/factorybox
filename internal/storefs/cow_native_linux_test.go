//go:build linux

package storefs

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestNativeStoreFSCopyOnWrite(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_STOREFS") != "1" {
		t.Skip("requires the owned fixed-filesystem native fixture")
	}
	root, err := os.MkdirTemp("/store", "cow-store-")
	if err != nil {
		t.Fatal(err)
	}
	var owner [16]byte
	if _, err := rand.Read(owner[:]); err != nil {
		t.Fatal(err)
	}
	formatter, err := filepath.EvalSymlinks("/usr/sbin/mkfs.btrfs")
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.ReadFile(formatter)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(executable)
	pin := FormatTool{Path: formatter, SHA256: hex.EncodeToString(hash[:]), Schema: 2}
	image, err := Create(context.Background(), root+"/store.img", 512<<20, hex.EncodeToString(owner[:]), pin)
	if err != nil {
		t.Fatal(err)
	}
	if image.Schema != 2 {
		t.Fatal("new CoW image did not retain its distinct immutable format")
	}
	target := root + "/data"
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	attachment, err := Attach(image, target)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := attachment.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := Verify(attachment.Active); err != nil {
		t.Fatal("independent CoW attachment verification", err)
	}
	if attachment.Active.BlockDevice == unix.Mkdev(7, uint32(attachment.Active.LoopNumber)) {
		t.Fatal("fixture did not exercise the separate virtual filesystem device")
	}
	if _, err := Attach(image, target); err == nil {
		t.Fatal("a second owner adopted the live CoW image")
	}
	payload := make([]byte, 32<<20)
	if _, err := rand.Read(payload); err != nil {
		t.Fatal(err)
	}
	if err := writeDurable(target+"/source", payload); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("cp", "--reflink=always", "--", target+"/source", target+"/fork").CombinedOutput(); err != nil {
		t.Fatalf("fixed-size filesystem does not support a real kernel clone: %v: %s", err, output)
	}
	if err := os.WriteFile(target+"/fork", []byte("private fork"), 0600); err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile(target + "/source")
	if err != nil || sha256.Sum256(source) != sha256.Sum256(payload) {
		t.Fatal("the independently writable clone changed the original", err)
	}
	if output, err := exec.Command("btrfs", "subvolume", "create", target+"/subvolume").CombinedOutput(); err != nil {
		t.Fatalf("owned subvolume preparation: %v: %s", err, output)
	}
	if err := writeDurable(target+"/subvolume/data", payload[:16<<20]); err != nil {
		t.Fatal(err)
	}
	reserve, err := os.OpenFile(target+"/reserve", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer reserve.Close()
	if err := errors.Join(unix.Fallocate(int(reserve.Fd()), 0, 0, 8<<20), reserve.Sync()); err != nil {
		t.Fatal(err)
	}
	pressure, err := os.OpenFile(target+"/pressure", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer pressure.Close()
	var written int64
	full := false
	for written <= image.Bytes {
		n, writeErr := pressure.Write(payload[:1<<20])
		written += int64(n)
		err := errors.Join(writeErr, pressure.Sync())
		if errors.Is(err, unix.ENOSPC) {
			full = true
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if !full || written > image.Bytes {
		t.Fatal("CoW storage did not enforce its actual fixed capacity", written)
	}
	if err := writeDurable(target+"/canary", make([]byte, 4096)); !errors.Is(err, unix.ENOSPC) {
		t.Fatal("the fixed CoW boundary accepted another allocation", err)
	}
	if err := errors.Join(reserve.Truncate(0), reserve.Sync()); err != nil {
		t.Fatal(err)
	}
	if err := writeDurable(target+"/rollback", []byte("owned rollback")); err != nil {
		t.Fatal("the owned reserve did not preserve rollback writes at ENOSPC", err)
	}
	if err := pressure.Close(); err != nil {
		t.Fatal(err)
	}
	if err := reserve.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(target + "/pressure"); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(target + "/subvolume"); err != nil {
		t.Fatal("the original owned subvolume could not be retired", err)
	}
	if err := attachment.Close(); err != nil {
		t.Fatal(err)
	}
	if err := VerifyReleased(image); err != nil {
		t.Fatal("CoW loop association did not release", err)
	}
	again, err := Attach(image, target)
	if err != nil {
		t.Fatal(err)
	}
	retained, err := os.ReadFile(target + "/source")
	fork, forkErr := os.ReadFile(target + "/fork")
	rollback, rollbackErr := os.ReadFile(target + "/rollback")
	if err != nil || forkErr != nil || rollbackErr != nil ||
		sha256.Sum256(retained) != sha256.Sum256(payload) || string(fork) != "private fork" ||
		string(rollback) != "owned rollback" {
		again.Close()
		t.Fatal("remount lost independently retained CoW data", err, forkErr, rollbackErr)
	}
	if err := again.Close(); err != nil {
		t.Fatal(err)
	}
	if err := VerifyReleased(image); err != nil {
		t.Fatal(err)
	}
	saveProof(t, "cow", map[string]any{
		"kernel_reflink": true, "private_clone_mutation": true, "fixed_capacity": true,
		"rollback_after_reserve_release": true, "remount_persistence": true,
		"exact_autoclear_cleanup": true, "independent_verification": true, "second_owner_refused": true,
		"subvolume_inside_capacity": true, "owned_subvolume_cleanup": true,
		"pressure_bytes_written": written,
	})
}
