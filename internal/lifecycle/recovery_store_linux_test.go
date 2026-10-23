//go:build linux

package lifecycle

import (
	"encoding/json"
	"strings"
	"testing"

	"factorybox/internal/storefs"
	"factorybox/internal/sysboxcapture"
	"golang.org/x/sys/unix"
)

func TestRecoveryStoreEpochIdentity(t *testing.T) {
	owner, epochID := strings.Repeat("a", 32), strings.Repeat("b", 32)
	root := "/fixed/data/store/epochs/" + epochID + "/controller"
	current := storefs.Active{Image: storefs.Image{Schema: 1, Owner: owner, UUID: owner, Path: "/fixed/store.img",
		Device: 31, Inode: 1234, Bytes: 32 << 30}, Target: "/fixed/data", DirectIO: true,
		BlockDevice: uint64(unix.Mkdev(7, 1)), LoopNumber: 1, FilesystemMax: 31 << 30,
		Mount: storefs.Object{Device: uint64(unix.Mkdev(7, 1)), Inode: 2, MountID: 20}}
	old := current
	old.BlockDevice, old.LoopNumber = uint64(unix.Mkdev(7, 0)), 0
	old.Mount.Device, old.Mount.MountID = old.BlockDevice, 10
	config := Config{Root: root, ManagerRoot: "/fixed/data/store/manager", Runtime: "/bin/runtime", Criu: "/bin/criu",
		RuntimeSHA256: strings.Repeat("c", 64), CriuSHA256: strings.Repeat("d", 64), BoundedStore: &current}
	source := Record{BootID: "boot", RuntimeSHA256: config.RuntimeSHA256,
		Base: &sysboxcapture.FilesystemTree{Immutable: &sysboxcapture.ImmutableTree{Root: root + "/bases/base/rootfs", Device: old.BlockDevice}}}
	header := recoveryStoreEpoch{Schema: 1, Store: "/fixed/data/store", StoreID: owner, Epoch: epochID, BootID: "boot", Lifecycle: config}
	header.Lifecycle.BoundedStore = &old
	if admitted, err := validateRecoveryStoreEpoch(config, "boot", source, header); err != nil || admitted.BlockDevice != old.BlockDevice || admitted.Image != current.Image {
		t.Fatal("exact retained image with a changed kernel device was refused", admitted, err)
	}
	for name, change := range map[string]func(*recoveryStoreEpoch){
		"schema":         func(h *recoveryStoreEpoch) { h.Schema++ },
		"store":          func(h *recoveryStoreEpoch) { h.Store += "-other" },
		"store-id":       func(h *recoveryStoreEpoch) { h.StoreID = strings.Repeat("e", 32) },
		"epoch":          func(h *recoveryStoreEpoch) { h.Epoch = strings.Repeat("e", 32) },
		"boot":           func(h *recoveryStoreEpoch) { h.BootID += "other" },
		"root":           func(h *recoveryStoreEpoch) { h.Lifecycle.Root += "/other" },
		"manager":        func(h *recoveryStoreEpoch) { h.Lifecycle.ManagerRoot += "other" },
		"runtime-path":   func(h *recoveryStoreEpoch) { h.Lifecycle.Runtime += "other" },
		"runtime-digest": func(h *recoveryStoreEpoch) { h.Lifecycle.RuntimeSHA256 = strings.Repeat("e", 64) },
		"criu-path":      func(h *recoveryStoreEpoch) { h.Lifecycle.Criu += "other" },
		"criu-digest":    func(h *recoveryStoreEpoch) { h.Lifecycle.CriuSHA256 = strings.Repeat("e", 64) },
		"missing-store":  func(h *recoveryStoreEpoch) { h.Lifecycle.BoundedStore = nil },
		"image-schema":   func(h *recoveryStoreEpoch) { h.Lifecycle.BoundedStore.Image.Schema++ },
		"image-path":     func(h *recoveryStoreEpoch) { h.Lifecycle.BoundedStore.Image.Path += "other" },
		"image-owner":    func(h *recoveryStoreEpoch) { h.Lifecycle.BoundedStore.Image.Owner = strings.Repeat("e", 32) },
		"image-uuid":     func(h *recoveryStoreEpoch) { h.Lifecycle.BoundedStore.Image.UUID = strings.Repeat("e", 32) },
		"image-inode":    func(h *recoveryStoreEpoch) { h.Lifecycle.BoundedStore.Image.Inode++ },
		"image-device":   func(h *recoveryStoreEpoch) { h.Lifecycle.BoundedStore.Image.Device++ },
		"image-size":     func(h *recoveryStoreEpoch) { h.Lifecycle.BoundedStore.Image.Bytes-- },
		"target":         func(h *recoveryStoreEpoch) { h.Lifecycle.BoundedStore.Target += "other" },
		"direct-io":      func(h *recoveryStoreEpoch) { h.Lifecycle.BoundedStore.DirectIO = false },
		"capacity":       func(h *recoveryStoreEpoch) { h.Lifecycle.BoundedStore.FilesystemMax-- },
		"loop":           func(h *recoveryStoreEpoch) { h.Lifecycle.BoundedStore.LoopNumber++ },
		"block-device":   func(h *recoveryStoreEpoch) { h.Lifecycle.BoundedStore.BlockDevice++ },
		"mount-device":   func(h *recoveryStoreEpoch) { h.Lifecycle.BoundedStore.Mount.Device++ },
		"mount-id":       func(h *recoveryStoreEpoch) { h.Lifecycle.BoundedStore.Mount.MountID = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			data, err := json.Marshal(header)
			if err != nil {
				t.Fatal(err)
			}
			var changed recoveryStoreEpoch
			if err := json.Unmarshal(data, &changed); err != nil {
				t.Fatal(err)
			}
			change(&changed)
			if _, err := validateRecoveryStoreEpoch(config, "boot", source, changed); err == nil {
				t.Fatal("changed source identity admitted")
			}
		})
	}
}
