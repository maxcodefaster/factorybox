//go:build linux

package lifecycle

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestStorageAdmissionObservesAnchoredFilesystem(t *testing.T) {
	path := t.TempDir()
	root, err := os.OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	engine := &Engine{root: root, config: Config{Root: path, MinFreeBytes: 1, WriteReserve: 1}}
	report, err := engine.storageCapacity()
	if err != nil {
		t.Fatal(err)
	}
	var actual unix.Statfs_t
	if err := unix.Statfs(path, &actual); err != nil {
		t.Fatal(err)
	}
	if !report.PolicyEnabled || !report.WriteAdmitted || report.Directory.Inode == 0 || report.Directory.MountID == 0 ||
		report.TotalBytes != actual.Blocks*uint64(actual.Bsize) || report.AvailableBytes > report.FreeBytes || report.ObservedAt == "" {
		t.Fatal("capacity did not describe its anchored filesystem", report)
	}
	if err := engine.admitStorageWrite(); err != nil {
		t.Fatal("small admitted write refused on the actual filesystem", err)
	}
	// A floor above the entire actual filesystem must refuse without modifying
	// any data, regardless of concurrent test-host allocation changes.
	engine.config.MinFreeBytes = report.TotalBytes
	if err := engine.admitStorageWrite(); err == nil || !strings.Contains(err.Error(), "new storage writes refused") {
		t.Fatal("impossible capacity request admitted", err)
	}
	entries, err := os.ReadDir(path)
	if err != nil || len(entries) != 0 {
		t.Fatal("capacity observation or refusal created backing", entries, err)
	}
	// Substituting a fresh directory at the old numeric path cannot cause a
	// report for the substitute to be attributed to the anchored controller.
	moved := path + "-moved"
	if err := os.Rename(path, moved); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(moved) })
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.storageCapacity(); err == nil {
		t.Fatal("changed controller identity admitted")
	}
}

func TestStoragePolicyRejectsPartialOrOverflowedLimits(t *testing.T) {
	for _, config := range []Config{
		{MinFreeBytes: 1}, {WriteReserve: 1}, {MinFreeBytes: math.MaxUint64, WriteReserve: 1},
	} {
		if validateStoragePolicy(config) == nil {
			t.Fatal("invalid admission policy accepted", config)
		}
	}
	for _, config := range []Config{{}, {MinFreeBytes: 1, WriteReserve: 1}} {
		if err := validateStoragePolicy(config); err != nil {
			t.Fatal("valid admission policy refused", config, err)
		}
	}
}

func TestStorageAdmissionDoesNotFollowReplacedRoot(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "controller")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	engine := &Engine{root: root, config: Config{Root: path, MinFreeBytes: 1, WriteReserve: 1}}
	if err := os.Rename(path, path+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(base, path); err != nil {
		t.Fatal(err)
	}
	if err := engine.admitStorageWrite(); err == nil {
		t.Fatal("substituted root symlink admitted a write")
	}
}
