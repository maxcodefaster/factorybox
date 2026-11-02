//go:build linux

package lifecycle

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"factorybox/internal/storefs"

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

func TestStorageAllocationPreservesFreeFloorAndRecoveryReserve(t *testing.T) {
	for _, row := range []struct {
		available, floor, reserve, allocation uint64
		admitted                              bool
	}{
		{10, 2, 3, 5, true},
		{10, 2, 3, 6, false},
		{1, 2, 3, 0, false},
		{10, 2, math.MaxUint64, 0, false},
		{10, 2, 3, math.MaxUint64, false},
	} {
		if got := storageAllocationAdmitted(row.available, row.floor, row.reserve, row.allocation); got != row.admitted {
			t.Fatal("storage allocation crossed its floor/reserve or overflowed", row, got)
		}
	}
	path := t.TempDir()
	root, err := os.OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	engine := &Engine{root: root, config: Config{Root: path, MinFreeBytes: 1, WriteReserve: 1}}
	report, err := engine.storageCapacity()
	if err != nil || !report.WriteAdmitted || report.AvailableBytes < 3 {
		t.Fatal("fixture has no generic write admission", report, err)
	}
	if err := engine.admitStorageAllocation(1); err != nil {
		t.Fatal("small complete allocation refused", err)
	}
	for _, bytes := range []uint64{report.AvailableBytes, math.MaxUint64} {
		if err := engine.admitStorageAllocation(bytes); err == nil ||
			!strings.Contains(err.Error(), "new storage writes refused") ||
			!strings.Contains(err.Error(), "required allocation=") {
			t.Fatal("known allocation consumed the free floor/recovery reserve or overflowed", bytes, err)
		}
	}
	entries, err := os.ReadDir(path)
	if err != nil || len(entries) != 0 {
		t.Fatal("refused allocation changed backing", entries, err)
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

func TestBoundedStorageRequiresInternalRootsAndAdmission(t *testing.T) {
	active := &storefs.Active{Target: "/private/data"}
	valid := Config{BoundedStore: active, Root: "/private/data/store/controller", ManagerRoot: "/private/data/store/manager", MinFreeBytes: 1, WriteReserve: 1}
	if err := validateStoragePolicy(valid); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Config){
		func(c *Config) { c.Root = "/private/data-other/controller" },
		func(c *Config) { c.Root = "/private/data/../outside" },
		func(c *Config) { c.ManagerRoot = "/private/manager" },
		func(c *Config) { c.MinFreeBytes = 0; c.WriteReserve = 0 },
	} {
		invalid := valid
		change(&invalid)
		if err := validateStoragePolicy(invalid); err == nil {
			t.Fatal("bounded store accepted external roots or disabled admission", invalid)
		}
	}
}
