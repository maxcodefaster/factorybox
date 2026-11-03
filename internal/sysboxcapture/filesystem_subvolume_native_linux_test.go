//go:build linux

package sysboxcapture

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"factorybox/internal/storefs"
	"golang.org/x/sys/unix"
)

// Establish the actual kernel layout before choosing a snapshot representation.
// In particular, OverlayFS must still be able to rename between work and upper.
func TestFilesystemSnapshotSubvolumeNative(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_COW_COPY") != "1" {
		t.Skip("requires the owned native copy-on-write fixture")
	}
	root, err := os.MkdirTemp("/var/lib/sysbox/fixed-store/data/store", "snapshot-layout-")
	if err != nil {
		t.Fatal(err)
	}
	command := func(arguments ...string) error {
		output, err := exec.Command("btrfs", arguments...).CombinedOutput()
		if err != nil {
			return errors.Join(err, errors.New(string(output)))
		}
		return nil
	}
	var subvolumes []string
	var mounted bool
	t.Cleanup(func() {
		if mounted {
			if err := unix.Unmount(root+"/merged", 0); err != nil {
				t.Error(err)
				return
			}
		}
		for index := len(subvolumes) - 1; index >= 0; index-- {
			if _, err := os.Lstat(subvolumes[index]); errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err := command("subvolume", "delete", "--", subvolumes[index]); err != nil {
				t.Error(err)
				return
			}
		}
		if err := os.RemoveAll(root); err != nil {
			t.Error(err)
		}
	})
	for _, name := range []string{"lower", "work", "merged"} {
		if err := os.Mkdir(root+"/"+name, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(root+"/lower/base", []byte("base"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := command("subvolume", "create", "--", root+"/upper"); err != nil {
		t.Fatal(err)
	}
	subvolumes = append(subvolumes, root+"/upper")
	mountErr := unix.Mount("overlay", root+"/merged", "overlay", 0,
		"index=off,lowerdir="+root+"/lower,upperdir="+root+"/upper,workdir="+root+"/work")
	if mountErr == nil {
		mounted = true
		mountErr = os.WriteFile(root+"/merged/created", []byte("independent-upper"), 0600)
		if mountErr == nil {
			mountErr = os.WriteFile(root+"/merged/base", []byte("copied-up-base"), 0600)
		}
	}
	t.Logf("separate-subvolume upper and work: %v", mountErr)
	if mounted {
		if err := unix.Unmount(root+"/merged", 0); err != nil {
			t.Fatal(err)
		}
		mounted = false
	}
	if err := command("subvolume", "create", "--", root+"/bundle"); err != nil {
		t.Fatal(err)
	}
	subvolumes = append(subvolumes, root+"/bundle")
	status, err := exec.Command("/opt/factorybox/factorybox", "worker-status").Output()
	if err != nil {
		t.Fatal(err)
	}
	var worker struct {
		Lifecycle struct {
			Store *storefs.Active `json:"bounded_store"`
		} `json:"lifecycle"`
	}
	if err := json.Unmarshal(status, &worker); err != nil || worker.Lifecycle.Store == nil {
		t.Fatal("owned fixture has no exact admitted store", err)
	}
	active := *worker.Lifecycle.Store
	var subvolumeStat unix.Stat_t
	if err := unix.Stat(root+"/bundle", &subvolumeStat); err != nil || uint64(subvolumeStat.Dev) == active.BlockDevice {
		t.Fatal("fixture did not exercise the subvolume's distinct virtual device", err)
	}
	if err := storefs.VerifyObjectBacking(active, root+"/bundle"); err != nil {
		t.Fatal("owned subvolume escaped its fixed capacity boundary", err)
	}
	if err := os.Mkdir(root+"/covered", 0700); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mount(root+"/bundle", root+"/covered", "", unix.MS_BIND, ""); err != nil {
		t.Fatal(err)
	}
	coveredErr := storefs.VerifyObjectBacking(active, root+"/covered")
	unmountErr := unix.Unmount(root+"/covered", 0)
	if coveredErr == nil || unmountErr != nil {
		t.Fatal("same-data foreign bind gained subvolume admission", coveredErr, unmountErr)
	}
	for _, name := range []string{"upper", "work"} {
		if err := os.Mkdir(root+"/bundle/"+name, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := unix.Mount("overlay", root+"/merged", "overlay", 0,
		"index=off,lowerdir="+root+"/lower,upperdir="+root+"/bundle/upper,workdir="+root+"/bundle/work"); err != nil {
		t.Fatal("snapshot-capable common upper/work layout", err)
	}
	mounted = true
	if err := os.WriteFile(root+"/merged/marker", []byte("captured"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := unix.Unmount(root+"/merged", 0); err != nil {
		t.Fatal(err)
	}
	mounted = false
	// Exercise substantial file data and metadata. The independent full digest
	// below belongs to this proof, not to the timed kernel snapshot operation.
	payload, err := os.OpenFile(root+"/bundle/upper/payload", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 128<<10)
	value := uint64(0x9e3779b97f4a7c15)
	for index := range buffer {
		value ^= value << 13
		value ^= value >> 7
		value ^= value << 17
		buffer[index] = byte(value)
	}
	for range 1024 {
		if _, err := payload.Write(buffer); err != nil {
			payload.Close()
			t.Fatal(err)
		}
	}
	if err := errors.Join(payload.Sync(), payload.Close()); err != nil {
		t.Fatal(err)
	}
	for directory := range 128 {
		path := root + "/bundle/upper/directory-" + strconv.Itoa(directory)
		if err := os.Mkdir(path, 0750); err != nil {
			t.Fatal(err)
		}
		for file := range 256 {
			if err := os.WriteFile(path+"/file-"+strconv.Itoa(file), []byte("snapshot-payload"), 0640); err != nil {
				t.Fatal(err)
			}
		}
	}
	source, err := openDirectory(root + "/bundle")
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	// UAPI names are NUL-terminated. Reject before issuing the ioctl so an
	// invalid path cannot leave a snapshot under its silently truncated name.
	truncated := root + "/truncated"
	subvolumes = append(subvolumes, truncated)
	if _, err := cloneSubvolume(context.Background(), source, truncated+"\x00suffix", true); err == nil {
		t.Fatal("snapshot accepted a NUL-containing path")
	}
	if _, err := os.Lstat(truncated); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("invalid snapshot path created a truncated destination", err)
	}
	subvolumes = append(subvolumes, root+"/saved")
	started := time.Now()
	identity, err := cloneSubvolume(context.Background(), source, root+"/saved", true)
	kernelTime := time.Since(started)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := cloneSubvolume(ctx, source, root+"/cancelled", true); !errors.Is(err, context.Canceled) {
		t.Fatal("snapshot ignored cancellation", err)
	}
	if _, err := os.Lstat(root + "/cancelled"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("cancelled snapshot created a destination", err)
	}
	if err := verifySubvolume(root+"/saved", identity); err != nil {
		t.Fatal("original sealed kernel snapshot", err)
	}
	if _, err := cloneSubvolume(context.Background(), source, root+"/saved", true); !errors.Is(err, unix.EEXIST) {
		t.Fatal("snapshot creation overwrote an existing destination", err)
	}
	if err := os.WriteFile(root+"/saved/upper/marker", []byte("changed"), 0600); !errors.Is(err, unix.EROFS) {
		t.Fatal("saved subvolume did not enforce read-only data", err)
	}
	saved, err := openDirectory(root + "/saved")
	if err != nil {
		t.Fatal(err)
	}
	defer saved.Close()
	started = time.Now()
	before, err := fingerprintTree(context.Background(), source, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	captured, err := fingerprintTree(context.Background(), saved, source, false)
	if err != nil || !sameFilesystemTree(before, captured) {
		t.Fatal("kernel snapshot differs from the complete source data and metadata", err)
	}
	t.Logf("kernel snapshot=%s independent two-tree content proof=%s entries=%d bytes=%d",
		kernelTime, time.Since(started), before.Entries, before.FileBytes)
	subvolumes = append(subvolumes, root+"/fork")
	fork, err := cloneSubvolume(context.Background(), saved, root+"/fork", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root+"/fork/upper/marker", []byte("fork-only"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"bundle", "saved"} {
		actual, err := os.ReadFile(filepath.Join(root, path, "upper/marker"))
		if err != nil || string(actual) != "captured" {
			t.Fatal("snapshot/fork writes changed their source", path, string(actual), err)
		}
	}
	if err := verifySubvolume(root+"/saved", identity); err != nil {
		t.Error("creating and writing a descendant changed the sealed source", err)
	}
	t.Logf("sealed snapshot=%+v writable fork=%+v", identity, fork)
	if err := command("property", "set", "-t", "s", root+"/saved", "ro", "false"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root+"/saved/upper/marker", []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := command("property", "set", "-t", "s", root+"/saved", "ro", "true"); err != nil {
		t.Fatal(err)
	}
	if err := verifySubvolume(root+"/saved", identity); err == nil {
		t.Fatal("restoring read-only after mutation hid the changed snapshot generation")
	}
	if err := command("subvolume", "create", "--", root+"/bundle/child"); err != nil {
		t.Fatal(err)
	}
	subvolumes = append(subvolumes, root+"/bundle/child")
	if err := os.WriteFile(root+"/bundle/child/important", []byte("must-not-disappear"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := cloneSubvolume(context.Background(), source, root+"/incomplete", true); !errors.Is(err, errNestedSubvolume) {
		t.Fatal("non-recursive snapshot did not reject its omitted child data", err)
	}
	if _, err := os.Lstat(root + "/incomplete"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("nested-subvolume refusal created partial backing", err)
	}
	t.Log("common-subvolume OverlayFS, read-only capture and independent writable snapshot passed")
}
