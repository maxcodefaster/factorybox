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
	"strings"

	"factorybox/internal/ownedtree"
	"factorybox/internal/sysboxcapture"
)

const resumeImagePayload = "resume-images"

func resumeRetirementPause(record Record, request CheckpointRequest) (PauseOperation, error) {
	var selected PauseOperation
	if record.ID != request.SourceID || record.Owner != request.SourceOwner {
		return selected, fmt.Errorf("Resume-image retirement owner differs")
	}
	if record.Phase != "running" && record.Phase != "paused" && record.Phase != "deleted" {
		return selected, fmt.Errorf("unresolved lifecycle blocks Resume-image retirement")
	}
	found := false
	pauses := append([]PauseOperation(nil), record.PauseHistory...)
	if record.Pause != nil {
		pauses = append(pauses, *record.Pause)
	}
	for _, pause := range pauses {
		if pause.SnapshotID != request.SnapshotID {
			continue
		}
		if found {
			return selected, fmt.Errorf("Resume-image retirement has conflicting pause history")
		}
		found, selected = true, pause
	}
	if !found || selected.CheckpointSHA256 != request.CheckpointSHA256 || selected.CompletedAt == "" ||
		selected.Resume == nil || !selected.Resume.Attempted || selected.Resume.CompletedAt == "" || selected.Resume.Error != "" {
		return selected, fmt.Errorf("Resume-image retirement lacks its original completed Resume")
	}
	if record.Phase == "deleted" {
		if record.Delete == nil || !record.Delete.Attempted || record.Delete.CompletedAt == "" || record.Delete.Error != "" {
			return selected, fmt.Errorf("Resume-image retirement requires confirmed Delete")
		}
		return selected, nil
	}
	if record.Pause == nil || record.Pause.SnapshotID == request.SnapshotID {
		return selected, fmt.Errorf("active Resume images remain protected")
	}
	if record.Pause.CompletedAt == "" || !digest.MatchString(record.Pause.CheckpointSHA256) {
		return selected, fmt.Errorf("a later complete Pause must replace the old Resume")
	}
	return selected, nil
}

// Resume files are private restore inputs, not recovery checkpoints. A running
// Sandbox keeps its current inputs; a later committed Pause or Delete makes an
// earlier generation eligible. Ordinary checkpoint selections remain separate.
func (e *Engine) resumeRetirementSafety(ctx context.Context, record Record, request CheckpointRequest, lock *os.File) error {
	if _, err := resumeRetirementPause(record, request); err != nil {
		return err
	}
	switch record.Phase {
	case "deleted":
		state, err := e.state(ctx, record, lock)
		if err != nil || state != nil {
			return errors.Join(err, fmt.Errorf("Resume-image retirement lacks confirmed runtime absence"))
		}
	case "paused":
		if err := e.requireEmptyCgroup(record); err != nil {
			return err
		}
		latest := CheckpointRequest{record.ID, record.Pause.SnapshotID, record.Owner, record.Pause.CheckpointSHA256}
		_, saved, relative, err := e.checkpointRequest(latest)
		if err != nil {
			return err
		}
		published, err := e.publishedCheckpointMetadata(ctx, record, latest.SnapshotID, relative)
		if err != nil || !reflect.DeepEqual(saved, published) || !saved.SourceStopped {
			return errors.Join(err, fmt.Errorf("later Pause is not an independent complete checkpoint"))
		}
		if err := sysboxcapture.VerifyFilesystemSnapshot(ctx, e.config.Root+"/"+relative, saved.Filesystem); err != nil {
			return err
		}
	case "running":
		if _, _, err := e.resumeCompleted(ctx, lock, record); err != nil {
			return fmt.Errorf("later Resume lacks its independent live completion: %w", err)
		}
		relative := "objects/" + record.ID + "/resumes/" + record.Pause.SnapshotID
		var prepared []CheckpointFile
		if err := e.readCheckpointJSON(relative+"/resume-prepared.json", &prepared); err != nil {
			return err
		}
		actual, err := e.checkpointFiles(ctx, relative)
		if err != nil || !checkpointPreparationIntact(prepared, actual) {
			return errors.Join(err, fmt.Errorf("later independent Resume images changed"))
		}
	}
	return e.retirementPathDependencies(ctx, e.config.Root+"/objects/"+record.ID+"/resumes/"+request.SnapshotID+"/images")
}

func (e *Engine) retirementPathDependencies(ctx context.Context, retiring string) error {
	overlaps := func(name string) bool {
		return filepath.IsAbs(name) && (below(name, retiring) || below(retiring, name))
	}
	objects, err := e.exitEntries("objects")
	if err != nil {
		return err
	}
	for _, entry := range objects {
		if err := ctx.Err(); err != nil {
			return err
		}
		record, err := e.read(entry.Name())
		if err != nil {
			return err
		}
		if record.Phase == "deleted" && record.Delete != nil && record.Delete.CompletedAt != "" {
			continue
		}
		if record.Phase != "running" && record.Phase != "paused" {
			return fmt.Errorf("unresolved Sandbox blocks private-backing retirement")
		}
		if overlaps(record.InputBundle) || overlaps(record.Rootfs) ||
			record.Base != nil && record.Base.Immutable != nil && overlaps(record.Base.Immutable.Root) {
			return fmt.Errorf("live Sandbox references retiring private backing")
		}
		var config json.RawMessage
		if err := e.readCheckpointJSON("objects/"+record.ID+"/config.json", &config); err != nil {
			return err
		}
		if sum(config) != record.ConfigSHA256 {
			return fmt.Errorf("saved Sandbox configuration changed before backing retirement")
		}
		var spec struct{ Mounts []struct{ Source string } }
		if err := json.Unmarshal(config, &spec); err != nil {
			return err
		}
		for _, mount := range spec.Mounts {
			if overlaps(mount.Source) {
				return fmt.Errorf("live Sandbox binds retiring private backing")
			}
		}
	}
	forks, err := e.exitEntries("forks")
	if err != nil {
		return err
	}
	for _, entry := range forks {
		relative := "forks/" + entry.Name()
		var intent forkIntent
		if err := e.readCheckpointJSON(relative+"/intent.json", &intent); err != nil {
			return fmt.Errorf("incomplete fork blocks private-backing retirement: %w", err)
		}
		if _, err := e.readForkIntent(relative, intent.Origin.SourceID, intent.Origin.CheckpointID, entry.Name()); err != nil {
			return err
		}
		record, err := e.read(entry.Name())
		if err != nil || record.Start.CompletedAt == "" {
			return errors.Join(err, fmt.Errorf("incomplete descendant blocks private-backing retirement"))
		}
		if record.Phase == "deleted" && record.Delete != nil && record.Delete.CompletedAt != "" {
			continue
		}
		var prepared forkPreparation
		if err := e.readCheckpointJSON(relative+"/preparation.json", &prepared); err != nil {
			return err
		}
		data, _ := json.Marshal(intent)
		if intent.Owner != record.Owner || prepared.Owner != record.Owner || prepared.IntentSHA256 != sum(data) ||
			prepared.PreparedAt == "" || prepared.Filesystem == nil {
			return fmt.Errorf("descendant private backing is not independently prepared")
		}
		actual, err := e.checkpointFiles(ctx, relative+"/materialized")
		if err != nil || !checkpointPreparationIntact(prepared.Files, actual) {
			return errors.Join(err, fmt.Errorf("descendant private images changed"))
		}
		for _, tree := range prepared.Filesystem.Roots {
			if overlaps(tree.Saved) || tree.Immutable != nil && overlaps(tree.Immutable.Root) {
				return fmt.Errorf("descendant references retiring private backing")
			}
		}
	}
	return nil
}

func (e *Engine) RetireResumeImages(ctx context.Context, request CheckpointRequest) (result CheckpointRetirement, resultErr error) {
	lock, err := e.lock()
	if err != nil {
		return result, err
	}
	defer lock.Close()
	if e.config.BoundedStore == nil {
		return result, fmt.Errorf("Resume-image retirement requires the explicit bounded-store profile")
	}
	record, saved, _, err := e.checkpointRequest(request)
	if err != nil {
		return result, err
	}
	pause, err := resumeRetirementPause(record, request)
	if err != nil {
		return result, err
	}
	relative := "objects/" + record.ID + "/resumes/" + request.SnapshotID
	original := record
	original.Pause = &pause
	resume := e.resumeRequest(original)
	var resumed resumeIntent
	if err := e.readCheckpointJSON(relative+"/resume-intent.json", &resumed); err != nil {
		return result, err
	}
	if resumed.Version != 1 || resumed.Request != resume || resumed.SnapshotSHA != request.CheckpointSHA256 ||
		resumed.CriuSHA256 != e.config.CriuSHA256 || resumed.RuntimeSHA256 != e.config.RuntimeSHA256 || !saved.SourceStopped {
		return result, fmt.Errorf("Resume-image retirement differs from its immutable preparation")
	}
	if err := epochRestoreReceipt(e, relative+"/resume", record, resume, pause.Resume.CompletedAt); err != nil {
		return result, err
	}
	directory, err := directoryIdentity(e.config.Root + "/" + relative)
	if err != nil {
		return result, err
	}
	paths := []string{relative + "/images"}
	var intent checkpointRetirementIntent
	err = e.readCheckpointJSON(relative+"/retirement-intent.json", &intent)
	if errors.Is(err, os.ErrNotExist) {
		if err := e.resumeRetirementSafety(ctx, record, request, lock); err != nil {
			return result, err
		}
		var prepared []CheckpointFile
		if err := e.readCheckpointJSON(relative+"/resume-prepared.json", &prepared); err != nil {
			return result, err
		}
		actual, err := e.checkpointFiles(ctx, relative)
		if err != nil || !checkpointPreparationIntact(prepared, actual) {
			return result, errors.Join(err, fmt.Errorf("obsolete private Resume images changed"))
		}
		plan, err := ownedtree.Capture(ctx, e.root, paths)
		if err != nil {
			return result, err
		}
		before, err := e.storageCapacity()
		if err != nil {
			return result, err
		}
		hash, err := e.writeRetirementInventory(relative, plan)
		if err != nil {
			return result, err
		}
		reserve, err := e.prepareRetirementReserve(relative)
		if err != nil {
			return result, err
		}
		intent = checkpointRetirementIntent{Version: 1, PayloadKind: resumeImagePayload, Request: request,
			Directory: directory, InventorySHA256: hash, Payloads: paths, Reserve: reserve, StartedAt: now(), Before: before}
		if err := e.publishCheckpoint(relative, "retirement-intent.json", intent); err != nil {
			return result, err
		}
	} else if err != nil {
		return result, err
	}
	if intent.Version != 1 || intent.PayloadKind != resumeImagePayload || intent.Request != request ||
		intent.Directory != directory || !reflect.DeepEqual(intent.Payloads, paths) || len(intent.Aliases) != 0 ||
		!digest.MatchString(intent.InventorySHA256) || intent.StartedAt == "" {
		return result, fmt.Errorf("Resume-image retirement intent differs from its exact generation")
	}
	identity, err := json.Marshal(intent)
	if err != nil {
		return result, err
	}
	intentHash := sum(identity)
	if err := e.readCheckpointJSON(relative+"/retirement-complete.json", &result); err == nil {
		if result.Version != 1 || result.PayloadKind != resumeImagePayload || result.Request != request ||
			result.IntentSHA256 != intentHash || result.CompletedAt == "" || !reflect.DeepEqual(result.Before, intent.Before) {
			return result, fmt.Errorf("Resume-image retirement completion differs")
		}
		return result, e.retirementAbsent(intent)
	} else if !errors.Is(err, os.ErrNotExist) {
		return result, err
	}
	if err := e.resumeRetirementSafety(ctx, record, request, lock); err != nil {
		return result, err
	}
	plan, err := e.readRetirementInventory(relative, intent.InventorySHA256)
	if err != nil {
		return result, err
	}
	if !reflect.DeepEqual(plan.Paths, paths) {
		return result, fmt.Errorf("Resume-image retirement inventory escaped its exact payload")
	}
	if err := ownedtree.RemoveRemaining(ctx, e.root, plan); err != nil {
		return result, err
	}
	if err := e.retirementAbsent(intent); err != nil {
		return result, err
	}
	if err := e.releaseRetirementReserve(relative, intent.Reserve); err != nil {
		return result, err
	}
	after, err := e.storageCapacity()
	if err != nil {
		return result, err
	}
	result = CheckpointRetirement{Version: 1, PayloadKind: resumeImagePayload, Request: request,
		IntentSHA256: intentHash, CompletedAt: now(), Before: intent.Before, After: after}
	return result, e.publishCheckpoint(relative, "retirement-complete.json", result)
}

// These metadata files share the retirement format with checkpoint payloads,
// but must never authorize a capture generation's deletion inventory.
func resumeRetirementPath(relative, id, snapshot string) bool {
	return relative == "objects/"+id+"/resumes/"+snapshot && !strings.Contains(snapshot, "/")
}
