//go:build linux

package sysboxcapture

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestFilesystemSnapshotRetirementNative(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_COW_COPY") != "1" {
		t.Skip("requires the owned native copy-on-write fixture")
	}
	root, err := os.MkdirTemp("/var/lib/sysbox/fixed-store/data/store", "snapshot-retirement-")
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
	var mounts []string
	t.Cleanup(func() {
		for index := len(mounts) - 1; index >= 0; index-- {
			if err := unix.Unmount(mounts[index], 0); err != nil {
				t.Error(err)
				return
			}
		}
		for index := len(subvolumes) - 1; index >= 0; index-- {
			path := subvolumes[index]
			if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err := command("subvolume", "delete", "--", path); err != nil {
				t.Error(err)
				return
			}
		}
		if err := os.RemoveAll(root); err != nil {
			t.Error(err)
		}
	})
	sourcePath := root + "/source"
	if err := command("subvolume", "create", "--", sourcePath); err != nil {
		t.Fatal(err)
	}
	subvolumes = append(subvolumes, sourcePath)
	if err := os.Mkdir(sourcePath+"/covered", 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePath+"/marker", []byte("retained-data"), 0600); err != nil {
		t.Fatal(err)
	}
	source, err := openDirectory(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	snapshot := func(name string) subvolumeIdentity {
		t.Helper()
		path := root + "/" + name
		subvolumes = append(subvolumes, path)
		identity, err := cloneSubvolume(context.Background(), source, path, true)
		if err != nil {
			t.Fatal(err)
		}
		return identity
	}
	expected := snapshot("retire")
	sibling := snapshot("sibling")
	parent, err := openDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	pending, inspectErr := subvolumeRetirementPending(parent, expected.ID)
	if err := errors.Join(inspectErr, parent.Close()); err != nil || !pending {
		t.Fatal("kernel root-item query did not find the live snapshot", pending, err)
	}
	pendingContext, pendingCancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	err = waitSubvolumeRetirement(pendingContext, root+"/not-withdrawn", sibling)
	pendingCancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("cleaning observation did not honor cancellation while the exact root item remained", err)
	}
	path := root + "/retire"
	if err := os.RemoveAll(path); !errors.Is(err, unix.EROFS) {
		t.Fatal("fixture did not exercise a genuinely read-only snapshot", err)
	}
	if err := retireSubvolume(context.Background(), path, sibling); err == nil {
		t.Fatal("another snapshot identity authorized deletion")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := retireSubvolume(cancelled, path, expected); !errors.Is(err, context.Canceled) {
		t.Fatal("retirement ignored caller cancellation", err)
	}
	if err := waitSubvolumeRetirement(context.Background(), path, expected); err == nil {
		t.Fatal("cleaning accepted a snapshot that was not withdrawn")
	}
	if err := os.Symlink(root, root+"/alias"); err != nil {
		t.Fatal(err)
	}
	if err := retireSubvolume(context.Background(), root+"/alias/retire", expected); err == nil {
		t.Fatal("aliased ancestors authorized retirement")
	}
	if err := unix.Mount(root+"/sibling", path, "", unix.MS_BIND, ""); err != nil {
		t.Fatal(err)
	}
	mounts = append(mounts, path)
	if err := retireSubvolume(context.Background(), path, expected); err == nil {
		t.Fatal("foreign covering mount authorized retirement")
	}
	if err := unix.Unmount(path, 0); err != nil {
		t.Fatal(err)
	}
	mounts = mounts[:len(mounts)-1]
	if err := unix.Mount(root+"/sibling", path+"/covered", "", unix.MS_BIND, ""); err != nil {
		t.Fatal(err)
	}
	mounts = append(mounts, path+"/covered")
	if err := retireSubvolume(context.Background(), path, expected); err == nil {
		t.Fatal("retirement crossed a foreign descendant mount")
	}
	if err := unix.Unmount(path+"/covered", 0); err != nil {
		t.Fatal(err)
	}
	mounts = mounts[:len(mounts)-1]
	started := time.Now()
	if err := retireSubvolume(context.Background(), path, expected); err != nil {
		t.Fatal(err)
	}
	withdrawn := time.Since(started)
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("withdrawn snapshot path remains", err)
	}
	if err := retireSubvolume(context.Background(), path, expected); err == nil {
		t.Fatal("absent path silently acquired deletion authority")
	}
	deadline, ok := t.Deadline()
	if !ok {
		t.Fatal("native fixture requires its runner deadline")
	}
	ctx, cancel := context.WithDeadline(context.Background(), deadline.Add(-5*time.Second))
	defer cancel()
	started = time.Now()
	if err := waitSubvolumeRetirement(ctx, path, expected); err != nil {
		pending, inspectErr := subvolumeRetirementPending(source, expected.ID)
		deleted, listErr := exec.Command("btrfs", "subvolume", "list", "-d", root).CombinedOutput()
		t.Fatalf("kernel did not confirm cleaning of exact snapshot %d after %s: %v; pending=%t inspect=%v deleted=%s list=%v",
			expected.ID, time.Since(started), err, pending, inspectErr, deleted, listErr)
	}
	cleaned := time.Since(started)
	if err := waitSubvolumeRetirement(cancelled, path, expected); !errors.Is(err, context.Canceled) {
		t.Fatal("cleaning ignored caller cancellation", err)
	}
	// A reused name is not the original snapshot, even with identical bytes.
	replacement := snapshot("retire")
	if replacement.UUID == expected.UUID || replacement.ID == expected.ID {
		t.Fatal("replacement reused the retired snapshot identity")
	}
	if err := retireSubvolume(context.Background(), path, expected); err == nil {
		t.Fatal("old identity authorized deletion of a replacement")
	}
	for _, name := range []string{"source", "sibling", "retire"} {
		data, err := os.ReadFile(filepath.Join(root, name, "marker"))
		if err != nil || string(data) != "retained-data" {
			t.Fatal("retirement changed unrelated or replacement data", name, err)
		}
	}
	if err := verifySubvolume(root+"/sibling", sibling); err != nil {
		t.Fatal("retirement invalidated an unrelated sealed snapshot", err)
	}
	t.Logf("exact read-only snapshot withdrawal=%s kernel cleaning confirmation=%s; foreign mounts, cancellation, aliases and replacement refused",
		withdrawn, cleaned)
}
