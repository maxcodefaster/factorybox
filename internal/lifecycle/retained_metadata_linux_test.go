//go:build linux

package lifecycle

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"factorybox/internal/storefs"
	"golang.org/x/sys/unix"
)

func TestRetainedMetadataKeepsRecoveryRecipeStrict(t *testing.T) {
	owner, epochID := strings.Repeat("a", 32), strings.Repeat("b", 32)
	store := "/fixed/data/store"
	active := storefs.Active{
		Image: storefs.Image{Schema: 1, Owner: owner, UUID: owner, Path: "/fixed/store.img",
			Device: 31, Inode: 1234, Bytes: 32 << 30},
		Target: "/fixed/data", DirectIO: true, FilesystemMax: 31 << 30, LoopNumber: 1,
		BlockDevice: uint64(unix.Mkdev(7, 1)),
		Mount:       storefs.Object{Device: uint64(unix.Mkdev(7, 1)), Inode: 2, MountID: 20},
	}
	config := Config{Root: store + "/epochs/" + epochID + "/controller", ManagerRoot: store + "/manager",
		Runtime: "/bin/runtime", RuntimeSHA256: strings.Repeat("c", 64),
		Criu: "/bin/criu", CriuSHA256: strings.Repeat("d", 64), BoundedStore: &active}
	header := recoveryStoreEpoch{Schema: 1, Store: store, StoreID: owner, Epoch: epochID,
		BootID: "boot", Lifecycle: config}
	header.Lifecycle.CriuSHA256 = strings.Repeat("e", 64)
	if _, err := validateRetainedStoreMetadata(config, "boot", header); err != nil {
		t.Fatal("a different valid recorded CRIU recipe prevents dependency inventory", err)
	}
	if _, err := validateRetainedStoreEpoch(config, "boot", header); err == nil {
		t.Fatal("metadata inventory relaxed recovery recipe admission")
	}
	for _, invalid := range []string{"", "old-criu", strings.Repeat("e", 63), strings.Repeat("E", 64)} {
		header.Lifecycle.CriuSHA256 = invalid
		if _, err := validateRetainedStoreMetadata(config, "boot", header); err == nil {
			t.Fatal("invalid historical CRIU identity admitted", invalid)
		}
	}
	header.Lifecycle.CriuSHA256 = strings.Repeat("e", 64)
	for name, change := range map[string]func(*recoveryStoreEpoch){
		"boot":           func(h *recoveryStoreEpoch) { h.BootID += "other" },
		"store":          func(h *recoveryStoreEpoch) { h.Store += "other" },
		"store-owner":    func(h *recoveryStoreEpoch) { h.StoreID = strings.Repeat("f", 32) },
		"controller":     func(h *recoveryStoreEpoch) { h.Lifecycle.Root += "other" },
		"manager":        func(h *recoveryStoreEpoch) { h.Lifecycle.ManagerRoot += "other" },
		"runtime":        func(h *recoveryStoreEpoch) { h.Lifecycle.Runtime += "other" },
		"runtime-digest": func(h *recoveryStoreEpoch) { h.Lifecycle.RuntimeSHA256 = strings.Repeat("f", 64) },
		"criu-path":      func(h *recoveryStoreEpoch) { h.Lifecycle.Criu += "other" },
		"image":          func(h *recoveryStoreEpoch) { h.Lifecycle.BoundedStore.Image.Inode++ },
		"capacity":       func(h *recoveryStoreEpoch) { h.Lifecycle.BoundedStore.FilesystemMax-- },
		"direct-io":      func(h *recoveryStoreEpoch) { h.Lifecycle.BoundedStore.DirectIO = false },
		"attachment":     func(h *recoveryStoreEpoch) { h.Lifecycle.BoundedStore.Mount.Device++ },
	} {
		t.Run(name, func(t *testing.T) {
			data, _ := json.Marshal(header)
			var changed recoveryStoreEpoch
			if err := json.Unmarshal(data, &changed); err != nil {
				t.Fatal(err)
			}
			change(&changed)
			if _, err := validateRetainedStoreMetadata(config, "boot", changed); err == nil {
				t.Fatal("metadata inventory admitted changed storage/runtime ownership")
			}
		})
	}
}

func TestRetainedMetadataCannotExecuteLifecycle(t *testing.T) {
	// Refuse before touching even an absent root or executable.
	engine := &Engine{metadataOnly: true}
	if _, err := engine.lock(); err == nil || !strings.Contains(err.Error(), "metadata handle") {
		t.Fatal("metadata reader acquired lifecycle authority", err)
	}
	if _, err := engine.commandFiles(context.Background(), nil, nil, "state", "old-peer"); err == nil ||
		!strings.Contains(err.Error(), "metadata handle") {
		t.Fatal("metadata reader attempted to execute a historical runtime", err)
	}
}
