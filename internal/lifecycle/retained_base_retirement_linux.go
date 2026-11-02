//go:build linux

package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"

	"factorybox/internal/ownedtree"
	"factorybox/internal/storefs"
	"factorybox/internal/sysboxcapture"
)

type RetainedBaseRequest struct {
	SourceRoot string `json:"source_root"`
	BaseID     string `json:"base_id"`
	BaseSHA256 string `json:"base_sha256"`
}

type baseRetirementIntent struct {
	Version         int                 `json:"version"`
	Request         RetainedBaseRequest `json:"request"`
	Directory       KernelObject        `json:"directory"`
	Attachment      storefs.Active      `json:"attachment"`
	InventorySHA256 string              `json:"inventory_sha256"`
	Reserve         retirementReserve   `json:"reserve"`
	StartedAt       string              `json:"started_at"`
	Before          StorageCapacity     `json:"before"`
}

type BaseRetirement struct {
	Version      int                 `json:"version"`
	Request      RetainedBaseRequest `json:"request"`
	IntentSHA256 string              `json:"intent_sha256"`
	CompletedAt  string              `json:"completed_at"`
	Before       StorageCapacity     `json:"before"`
	After        StorageCapacity     `json:"after"`
}

func baseRetirementPath(id string) string { return "bases/" + id + "/retirement" }

func (e *Engine) baseRetirementIdentity(base Base) (RetainedBaseRequest, error) {
	raw, err := e.root.ReadFile("bases/" + base.ID + "/base.json")
	return RetainedBaseRequest{e.config.Root, base.ID, sum(raw)}, err
}

func (e *Engine) baseRetirementProof(base Base) (baseRetirementIntent, BaseRetirement, error) {
	var intent baseRetirementIntent
	var result BaseRetirement
	request, err := e.baseRetirementIdentity(base)
	if err != nil {
		return intent, result, err
	}
	relative := baseRetirementPath(base.ID)
	if err := e.readCheckpointJSON(relative+"/retirement-intent.json", &intent); err != nil {
		return intent, result, err
	}
	if err := privateDirectory(e.config.Root + "/" + relative); err != nil {
		return intent, result, err
	}
	if err := e.boundedObject(e.config.Root + "/" + relative); err != nil {
		return intent, result, err
	}
	directory, err := directoryIdentity(e.config.Root + "/" + relative)
	active := e.config.BoundedStore
	if err != nil || active == nil || intent.Version != 1 || intent.Request != request ||
		intent.StartedAt == "" || !digest.MatchString(intent.InventorySHA256) ||
		intent.Reserve.Device != intent.Attachment.BlockDevice || intent.Reserve.Inode == 0 ||
		!retainedBackingObject(intent.Directory, directory,
			KernelObject{Device: intent.Attachment.BlockDevice, MountID: intent.Attachment.Mount.MountID},
			KernelObject{Device: active.BlockDevice, MountID: active.Mount.MountID}) {
		return intent, result, fmt.Errorf("base retirement differs from its original identity: %v", err)
	}
	plan, err := e.readRetirementInventory(relative, intent.InventorySHA256)
	if err != nil || !reflect.DeepEqual(plan.Paths, []string{"bases/" + base.ID + "/rootfs"}) {
		return intent, result, fmt.Errorf("base retirement inventory changed: %v", err)
	}
	if _, err := rebindEpochRetirementPlan(plan, intent.Attachment, *active); err != nil {
		return intent, result, err
	}
	if err := e.readCheckpointJSON(relative+"/retirement-complete.json", &result); err != nil {
		return intent, result, err
	}
	data, _ := json.Marshal(intent)
	if result.Version != 1 || result.Request != request || result.IntentSHA256 != sum(data) ||
		result.CompletedAt == "" || !reflect.DeepEqual(result.Before, intent.Before) {
		return intent, result, fmt.Errorf("base retirement has no exact completed receipt")
	}
	if _, err := e.root.Lstat("bases/" + base.ID + "/rootfs"); !errors.Is(err, os.ErrNotExist) {
		return intent, result, fmt.Errorf("retired base has remaining or successor backing: %v", err)
	}
	return intent, result, nil
}

// Retained records can still own usable checkpoints after execution was deleted.
// Only their exact completed checkpoint retirement withdraws that dependency.
func (e *Engine) baseCheckpointDependencies(ctx context.Context, retiring string) error {
	objects, err := e.exitEntries("objects")
	if err != nil {
		return err
	}
	previous := *e.config.BoundedStore
	if e.retainedStorage != nil {
		previous = *e.retainedStorage
	}
	builder := epochInventoryBuilder{engine: e, ctx: ctx, previous: previous, allowed: map[string]uint32{}}
	for _, entry := range objects {
		record, err := e.read(entry.Name())
		if err != nil {
			return err
		}
		if record.Base != nil && record.Base.Immutable != nil &&
			below(record.Base.Immutable.Root, retiring) && record.Phase == "deleted" {
			retired, err := builder.retiredSandboxBacking(record)
			if err != nil || !retired {
				return errors.Join(err, fmt.Errorf("deleted Sandbox has unretired private base references"))
			}
		}
		captures, err := e.exitEntries("objects/" + record.ID + "/captures")
		if errors.Is(err, os.ErrNotExist) && record.Phase == "deleted" {
			continue
		}
		if err != nil {
			return err
		}
		for _, capture := range captures {
			relative, err := checkpointPath(record.ID, capture.Name())
			if err != nil || !capture.IsDir() {
				return fmt.Errorf("unrecognized capture blocks base retirement")
			}
			var saved Checkpoint
			err = e.readCheckpointJSON(relative+"/checkpoint.json", &saved)
			if errors.Is(err, os.ErrNotExist) {
				// An unsuccessful capture with no remaining payload is evidence,
				// not a usable checkpoint. Preserve every diagnostic file.
				if record.Base != nil && record.Base.Immutable != nil &&
					below(record.Base.Immutable.Root, retiring) {
					for _, name := range []string{"images", "filesystem"} {
						if _, err := e.root.Lstat(relative + "/" + name); !errors.Is(err, os.ErrNotExist) {
							return fmt.Errorf("unfinished capture backing protects base: %s", relative)
						}
					}
				}
				continue
			}
			if err != nil {
				return err
			}
			originalBase := record.Base != nil && record.Base.Immutable != nil &&
				below(record.Base.Immutable.Root, retiring)
			if err := epochSnapshotReferences(saved.Filesystem, retiring); err == nil && !originalBase {
				continue
			}
			retired, err := builder.retired(record, relative)
			if err != nil || !retired {
				return errors.Join(err, fmt.Errorf("retained checkpoint protects base: %s", relative))
			}
		}
	}
	return nil
}

func (e *Engine) retainedBaseDependencies(ctx context.Context, forest *epochRetirementForest, base Base) error {
	for _, controller := range forest.controllers {
		if err := controller.retirementPathDependencies(ctx, base.Tree.Saved); err != nil {
			return err
		}
		if err := controller.baseCheckpointDependencies(ctx, base.Tree.Saved); err != nil {
			return err
		}
	}
	return nil
}

// RetireRetainedBase removes one explicitly named, unreferenced base from a
// previous worker. It never changes original imports, records or checkpoints.
// The current worker must be drained; old unresolved records remain protected
// by their actual paths rather than blocking unrelated immutable bases.
func (e *Engine) RetireRetainedBase(ctx context.Context, request RetainedBaseRequest) (BaseRetirement, error) {
	var result BaseRetirement
	if !sandboxID.MatchString(request.BaseID) || !digest.MatchString(request.BaseSHA256) {
		return result, fmt.Errorf("retained base retirement requires its exact ID and metadata hash")
	}
	if err := retainedControllerPath(e.config, request.SourceRoot); err != nil {
		return result, err
	}
	forest, err := e.deletedBackingForest()
	if err != nil {
		return result, err
	}
	defer forest.close()
	objects, err := e.exitEntries("objects")
	if err != nil {
		return result, err
	}
	for _, entry := range objects {
		record, err := e.read(entry.Name())
		if err != nil || record.Phase != "deleted" || epochClosedRecord(record) != nil {
			return result, fmt.Errorf("drain current execution before retained base retirement: %v", err)
		}
		state, err := e.state(ctx, record, forest.locks[0])
		if err != nil || state != nil {
			return result, fmt.Errorf("current deleted execution has remaining runtime state: %v", err)
		}
		if _, err := os.Lstat(cgroupRoot + "/" + record.RuntimeID); !errors.Is(err, os.ErrNotExist) {
			return result, fmt.Errorf("current deleted execution has remaining or successor cgroup: %v", err)
		}
	}
	source := forest.controllers[request.SourceRoot]
	if source == nil || !source.metadataOnly {
		return result, fmt.Errorf("base does not belong to an exact retained worker")
	}
	if err := source.epochRetirementFence(); err != nil {
		return result, err
	}
	base, err := source.readBase(request.BaseID)
	if err != nil {
		return result, err
	}
	actual, err := source.baseRetirementIdentity(base)
	if err != nil || actual != request {
		return result, fmt.Errorf("retained base differs from the requested metadata")
	}
	relative := baseRetirementPath(base.ID)
	var intent baseRetirementIntent
	err = source.readCheckpointJSON(relative+"/retirement-intent.json", &intent)
	if errors.Is(err, os.ErrNotExist) {
		if err := e.retainedBaseDependencies(ctx, forest, base); err != nil {
			return result, err
		}
		verifier := epochInventoryBuilder{engine: source, ctx: ctx, previous: *source.retainedStorage,
			bases: map[string]sysboxcapture.FilesystemTree{base.Tree.Saved: base.Tree}}
		if err := sysboxcapture.VerifyRetainedFilesystemBase(ctx, base.Tree, verifier.translate); err != nil {
			return result, err
		}
		if err := source.root.Mkdir(relative, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return result, err
		}
		if err := source.resetUnacceptedRetirementPreparation(ctx, relative); err != nil {
			return result, err
		}
		directory, err := directoryIdentity(source.config.Root + "/" + relative)
		if err != nil {
			return result, err
		}
		plan, err := ownedtree.Capture(ctx, source.root, []string{"bases/" + base.ID + "/rootfs"})
		if err != nil {
			return result, err
		}
		before, err := e.storageCapacity()
		if err != nil {
			return result, err
		}
		hash, err := source.writeRetirementInventory(relative, plan)
		if err != nil {
			return result, err
		}
		reserve, err := source.prepareRetirementReserve(relative)
		if err != nil {
			return result, err
		}
		intent = baseRetirementIntent{1, request, directory, *e.config.BoundedStore, hash, reserve, now(), before}
		if err := source.publishCheckpoint(relative, "retirement-intent.json", intent); err != nil {
			return result, err
		}
	} else if err != nil {
		return result, err
	}
	_, result, proofErr := source.baseRetirementProof(base)
	if proofErr == nil {
		return result, nil
	}
	if !errors.Is(proofErr, os.ErrNotExist) {
		return result, proofErr
	}
	if err := e.retainedBaseDependencies(ctx, forest, base); err != nil {
		return result, err
	}
	plan, err := source.readRetirementInventory(relative, intent.InventorySHA256)
	if err != nil {
		return result, err
	}
	plan, err = rebindEpochRetirementPlan(plan, intent.Attachment, *e.config.BoundedStore)
	if err != nil {
		return result, err
	}
	if err := ownedtree.RemoveRemaining(ctx, source.root, plan); err != nil {
		return result, err
	}
	reserve := intent.Reserve
	reserve.Device = e.config.BoundedStore.BlockDevice
	if err := source.releaseRetirementReserve(relative, reserve); err != nil {
		return result, err
	}
	after, err := e.storageCapacity()
	if err != nil {
		return result, err
	}
	data, _ := json.Marshal(intent)
	result = BaseRetirement{1, request, sum(data), now(), intent.Before, after}
	if err := source.publishCheckpoint(relative, "retirement-complete.json", result); err != nil {
		return result, err
	}
	_, result, err = source.baseRetirementProof(base)
	return result, err
}
