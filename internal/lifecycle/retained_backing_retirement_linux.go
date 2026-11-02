//go:build linux

package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
)

func retainedBackingObject(saved, current KernelObject, previous, active KernelObject) bool {
	return saved.Inode != 0 && saved.Inode == current.Inode &&
		saved.Device == previous.Device && saved.MountID == previous.MountID &&
		current.Device == active.Device && current.MountID == active.MountID
}

// RetireRetainedDeletedBacking completes only the newly supported capture stage
// of an already completed native retirement in an older, validated store epoch.
// The original receipt and lifecycle record remain unchanged. No historical
// runtime/CRIU recipe executes, and private input/manager cleanup is not replayed.
func (e *Engine) RetireRetainedDeletedBacking(ctx context.Context, sourceRoot string,
	request SandboxBackingRequest) (SandboxBackingRetirement, error) {
	if err := retainedControllerPath(e.config, sourceRoot); err != nil {
		return SandboxBackingRetirement{}, err
	}
	forest, err := e.deletedBackingForest()
	if err != nil {
		return SandboxBackingRetirement{}, err
	}
	defer forest.close()
	old := forest.controllers[sourceRoot]
	if old == nil || !old.metadataOnly || old.retainedStorage == nil {
		return SandboxBackingRetirement{}, fmt.Errorf("retained backing has no validated original attachment")
	}
	if err := old.epochRetirementFence(); err != nil {
		return SandboxBackingRetirement{}, err
	}
	record, err := old.read(request.SandboxID)
	if err != nil {
		return SandboxBackingRetirement{}, err
	}
	if err := sandboxBackingRecord(record, request); err != nil {
		return SandboxBackingRetirement{}, err
	}
	for _, path := range []string{"runtime/" + record.RuntimeID} {
		if _, err := old.root.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			return SandboxBackingRetirement{}, fmt.Errorf("retained deleted Sandbox has remaining or successor runtime metadata: %v", err)
		}
	}
	if _, err := os.Lstat(cgroupRoot + "/" + record.RuntimeID); !errors.Is(err, os.ErrNotExist) {
		return SandboxBackingRetirement{}, fmt.Errorf("retained deleted Sandbox has remaining or successor compute: %v", err)
	}
	current, err := directoryIdentity(record.InputBundle)
	previous := KernelObject{Device: old.retainedStorage.BlockDevice, MountID: old.retainedStorage.Mount.MountID}
	active := KernelObject{Device: old.config.BoundedStore.BlockDevice, MountID: old.config.BoundedStore.Mount.MountID}
	if err != nil || !retainedBackingObject(*record.InputDirectory, current, previous, active) ||
		!withinStore(filepath.Dir(e.config.ManagerRoot), record.InputBundle) {
		return SandboxBackingRetirement{}, fmt.Errorf("retained original bundle differs from its verified store attachment: %v", err)
	}
	relative := "objects/" + record.ID + "/backing-retirement"
	var intent sandboxBackingIntent
	var completed SandboxBackingRetirement
	if err := old.readCheckpointJSON(relative+"/retirement-intent.json", &intent); err != nil {
		return completed, err
	}
	if err := old.readCheckpointJSON(relative+"/retirement-complete.json", &completed); err != nil {
		return completed, err
	}
	data, err := json.Marshal(intent)
	retirementDirectory, directoryErr := directoryIdentity(old.config.Root + "/" + relative)
	if err != nil || intent.Version != 1 || intent.Request != request || intent.StartedAt == "" ||
		intent.Bundle != *record.InputDirectory || !digest.MatchString(intent.InventorySHA256) ||
		directoryErr != nil || !retainedBackingObject(intent.Directory, retirementDirectory, previous, active) ||
		completed.Version != 1 || completed.PayloadKind != "sandbox-backing" ||
		completed.Request != request || completed.IntentSHA256 != sum(data) || completed.CompletedAt == "" ||
		!reflect.DeepEqual(completed.Before, intent.Before) {
		return SandboxBackingRetirement{}, fmt.Errorf("retained Sandbox lacks its exact completed original retirement: %v", err)
	}
	plan, err := old.readRetirementInventory(relative, intent.InventorySHA256)
	if err != nil || !reflect.DeepEqual(plan.Paths, intent.Payloads) {
		return SandboxBackingRetirement{}, errors.Join(err, fmt.Errorf("retained original retirement inventory changed"))
	}
	if err := old.retireRecoveredCaptureBacking(ctx, record, request, forest); err != nil {
		return SandboxBackingRetirement{}, err
	}
	return completed, nil
}
