//go:build linux

package lifecycle

import (
	"strings"
	"testing"

	"factorybox/internal/storefs"
	"golang.org/x/sys/unix"
)

func TestRetainedDependencyInventoryFromPreviousBoot(t *testing.T) {
	owner, epochID := strings.Repeat("a", 32), strings.Repeat("b", 32)
	store := "/fixed/data/store"
	active := storefs.Active{
		Image: storefs.Image{Schema: 1, Owner: owner, UUID: owner, Path: "/fixed/store.img",
			Device: 31, Inode: 1234, Bytes: 32 << 30},
		Target: "/fixed/data", DirectIO: true, FilesystemMax: 31 << 30, LoopNumber: 1,
		BlockDevice: uint64(unix.Mkdev(7, 1)),
		Mount:       storefs.Object{Device: uint64(unix.Mkdev(7, 1)), Inode: 2, MountID: 20},
	}
	current := Config{Root: store + "/epochs/" + epochID + "/controller", ManagerRoot: store + "/manager",
		Runtime: "/bin/runtime", RuntimeSHA256: strings.Repeat("c", 64),
		Criu: "/bin/criu", CriuSHA256: strings.Repeat("d", 64), BoundedStore: &active}
	previous := active
	previous.BlockDevice, previous.LoopNumber = uint64(unix.Mkdev(7, 0)), 0
	previous.Mount.Device, previous.Mount.MountID = previous.BlockDevice, 10
	header := recoveryStoreEpoch{Schema: 1, Store: store, StoreID: owner, Epoch: epochID,
		BootID: "previous-boot", Lifecycle: current}
	header.Lifecycle.BoundedStore = &previous
	metadata, recorded, err := retainedControllerConfiguration(current, current.Root, "current-boot", header, true)
	if err != nil || metadata.Root != current.Root || recorded != previous {
		t.Fatal("unchanged retained image from the previous boot blocked read-only dependency inventory", err)
	}
	if _, _, err := retainedControllerConfiguration(current, current.Root, "current-boot", header, false); err == nil {
		t.Fatal("read-only previous-boot inventory authorized lifecycle recovery")
	}
	if _, err := validateRetainedStoreMetadata(current, "current-boot", header); err == nil {
		t.Fatal("strict epoch validator stopped checking the supplied boot identity")
	}
	header.BootID = ""
	if _, _, err := retainedControllerConfiguration(current, current.Root, "current-boot", header, true); err == nil {
		t.Fatal("missing historical boot identity admitted")
	}
}
