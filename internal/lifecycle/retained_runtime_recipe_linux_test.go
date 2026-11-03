//go:build linux

package lifecycle

import (
	"strings"
	"testing"

	"factorybox/internal/storefs"
)

func TestRetainedControllerMetadataUsesHistoricalRuntimeRecipe(t *testing.T) {
	owner, epochID := strings.Repeat("a", 32), strings.Repeat("b", 32)
	store := "/fixed/data/store"
	active := storefs.Active{
		Image: storefs.Image{Schema: 2, Owner: owner, UUID: owner, Path: "/fixed/store.img",
			Device: 31, Inode: 1234, Bytes: 32 << 30},
		Target: "/fixed/data", DirectIO: true, FilesystemMax: 31 << 30, LoopNumber: 1,
		BlockDevice: 12345,
		Mount:       storefs.Object{Device: 12345, Inode: 256, MountID: 20},
	}
	current := Config{
		Root:        store + "/epochs/" + strings.Repeat("f", 32) + "/controller",
		ManagerRoot: store + "/manager",
		Runtime:     "/bin/runtime", RuntimeSHA256: strings.Repeat("c", 64),
		Criu: "/bin/criu", CriuSHA256: strings.Repeat("d", 64), BoundedStore: &active,
	}
	source := store + "/epochs/" + epochID + "/controller"
	header := recoveryStoreEpoch{
		Schema: 1, Store: store, StoreID: owner, Epoch: epochID,
		BootID: "boot", Lifecycle: current,
	}
	header.Lifecycle.Root = source
	for name, recipe := range map[string][2]string{
		"runtime": {strings.Repeat("e", 64), current.CriuSHA256},
		"criu":    {current.RuntimeSHA256, strings.Repeat("e", 64)},
		"both":    {strings.Repeat("e", 64), strings.Repeat("f", 64)},
	} {
		t.Run(name, func(t *testing.T) {
			original := current
			recorded := header
			recorded.Lifecycle.RuntimeSHA256 = recipe[0]
			recorded.Lifecycle.CriuSHA256 = recipe[1]
			metadata, previous, err := retainedControllerConfiguration(current, source, "boot", recorded, true)
			if err != nil {
				t.Fatal("an owned historical recipe blocked metadata-only dependency inventory", err)
			}
			if metadata.Root != source || metadata.RuntimeSHA256 != recipe[0] ||
				metadata.CriuSHA256 != recipe[1] || previous != active || current != original {
				t.Fatal("historical metadata changed current execution or storage authority")
			}
			if _, _, err := retainedControllerConfiguration(current, source, "boot", recorded, false); err == nil {
				t.Fatal("metadata-only compatibility authorized historical execution")
			}
			for _, invalid := range []string{"", "old-runtime", strings.Repeat("e", 63), strings.Repeat("E", 64)} {
				invalidCurrent := current
				invalidCurrent.RuntimeSHA256 = invalid
				if _, _, err := retainedControllerConfiguration(invalidCurrent, source, "boot", recorded, true); err == nil {
					t.Fatal("invalid current runtime identity admitted", invalid)
				}
				changed := recorded
				changed.Lifecycle.RuntimeSHA256 = invalid
				if _, _, err := retainedControllerConfiguration(current, source, "boot", changed, true); err == nil {
					t.Fatal("invalid historical runtime identity admitted", invalid)
				}
				changed = recorded
				changed.Lifecycle.CriuSHA256 = invalid
				if _, _, err := retainedControllerConfiguration(current, source, "boot", changed, true); err == nil {
					t.Fatal("invalid historical CRIU identity admitted", invalid)
				}
			}
		})
	}
}
