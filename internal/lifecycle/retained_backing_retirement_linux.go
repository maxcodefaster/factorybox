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

	"factorybox/internal/ownedtree"
	"factorybox/internal/storefs"
)

func retainedBackingObject(saved, current KernelObject, previous, active KernelObject) bool {
	return saved.Inode != 0 && saved.Inode == current.Inode &&
		saved.Device == previous.Device && saved.MountID == previous.MountID &&
		current.Device == active.Device && current.MountID == active.MountID
}

func (e *Engine) backingRetirementAttachment(saved *storefs.Active) (storefs.Active, error) {
	if saved == nil {
		saved = e.retainedStorage
	}
	if saved == nil {
		saved = e.config.BoundedStore
	}
	active := e.config.BoundedStore
	if saved == nil || active == nil || saved.Image != active.Image || saved.Target != active.Target ||
		!saved.DirectIO || !active.DirectIO || saved.FilesystemMax != active.FilesystemMax ||
		saved.Mount.Inode != active.Mount.Inode || storefs.ValidateAttachmentIdentity(*saved) != nil ||
		storefs.ValidateAttachmentIdentity(*active) != nil {
		return storefs.Active{}, fmt.Errorf("private backing retirement changed its persistent attachment")
	}
	return *saved, nil
}

func (e *Engine) backingDirectoryMatches(saved, current KernelObject, attachment *storefs.Active) bool {
	previous, err := e.backingRetirementAttachment(attachment)
	if err != nil {
		return false
	}
	active := e.config.BoundedStore
	return retainedBackingObject(saved, current,
		KernelObject{Device: previous.BlockDevice, MountID: previous.Mount.MountID},
		KernelObject{Device: active.BlockDevice, MountID: active.Mount.MountID})
}

func (e *Engine) rebindBackingRetirementPlan(plan ownedtree.Plan, saved *storefs.Active) (ownedtree.Plan, error) {
	previous, err := e.backingRetirementAttachment(saved)
	if err != nil {
		return ownedtree.Plan{}, err
	}
	// A native mutable subvolume has its own device identity within the
	// unchanged mount. The ordinary owned-tree verifier preserves it exactly.
	if previous == *e.config.BoundedStore {
		return plan, nil
	}
	return rebindEpochRetirementPlan(plan, previous, *e.config.BoundedStore)
}

func (f *epochRetirementForest) backingDependencies(ctx context.Context, store string, paths []string) error {
	for _, controller := range f.controllers {
		if err := controller.sandboxBackingDependencies(ctx, store, paths); err != nil {
			return err
		}
	}
	return nil
}

// RetireRetainedDeletedBacking continues private backing retirement only after
// the original exact Delete and current absence of execution are verified.
// The original lifecycle record and any completed receipt remain unchanged.
// No historical runtime or CRIU recipe executes.
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
	if err := old.readCheckpointJSON(relative+"/retirement-complete.json", &completed); errors.Is(err, os.ErrNotExist) {
		return old.retireDeletedBacking(ctx, request, forest)
	} else if err != nil {
		return completed, err
	}
	if err := old.readCheckpointJSON(relative+"/retirement-intent.json", &intent); err != nil {
		return completed, err
	}
	data, err := json.Marshal(intent)
	retirementDirectory, directoryErr := directoryIdentity(old.config.Root + "/" + relative)
	if err != nil || intent.Version != 1 || intent.Request != request || intent.StartedAt == "" ||
		intent.Bundle != *record.InputDirectory || !digest.MatchString(intent.InventorySHA256) ||
		directoryErr != nil || !old.backingDirectoryMatches(intent.Directory, retirementDirectory, intent.Attachment) ||
		completed.Version != 1 || completed.PayloadKind != "sandbox-backing" ||
		completed.Request != request || completed.IntentSHA256 != sum(data) || completed.CompletedAt == "" ||
		!reflect.DeepEqual(completed.Before, intent.Before) {
		return SandboxBackingRetirement{}, fmt.Errorf("retained Sandbox lacks its exact completed original retirement: %v", err)
	}
	plan, err := old.readRetirementInventory(relative, intent.InventorySHA256)
	if err != nil || !reflect.DeepEqual(plan.Paths, intent.Payloads) {
		return SandboxBackingRetirement{}, errors.Join(err, fmt.Errorf("retained original retirement inventory changed"))
	}
	if err := old.retireDeletedFailedCaptureBacking(ctx, record, request, forest); err != nil {
		return SandboxBackingRetirement{}, err
	}
	if err := old.retireRecoveredCaptureBacking(ctx, record, request, forest); err != nil {
		return SandboxBackingRetirement{}, err
	}
	return completed, nil
}
