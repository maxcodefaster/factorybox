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
)

// Before import.json is atomically published there is no committed target
// reservation. Only an empty private directory or one exact publication
// temporary can exist. Never infer this state from missing record metadata
// alone: any payload, intent, target object or unknown entry refuses it.
func (e *Engine) recoveryImportInitialStage(operation, targetID string) error {
	if !sandboxID.MatchString(targetID) || operation != "forks/"+targetID {
		return fmt.Errorf("initial import metadata escaped its exact target")
	}
	if err := privateDirectory(e.config.Root + "/" + operation); err != nil {
		return err
	}
	for _, path := range []string{operation + "/import.json", "objects/" + targetID} {
		if _, err := e.root.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("initial import already has committed or uncertain ownership: %s: %v", path, err)
		}
	}
	entries, err := e.exitEntries(operation)
	if err != nil {
		return err
	}
	if len(entries) > 1 {
		return fmt.Errorf("initial import metadata contains repeated or unknown publication")
	}
	for _, entry := range entries {
		if err := e.recoveryImportTemporary(operation, entry.Name()); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) inspectRecoveryImportInitialLocked(ctx context.Context, source *Engine, targetID string) (RecoveryImportInspection, error) {
	var report RecoveryImportInspection
	if e.config.BoundedStore == nil || ctx.Err() != nil {
		return report, fmt.Errorf("initial import inspection requires a live bounded store: %v", ctx.Err())
	}
	operation := "forks/" + targetID
	if err := source.recoveryImportInitialStage(operation, targetID); err != nil {
		return report, err
	}
	birth, change, err := source.recoveryImportTimes(operation)
	if err != nil {
		return report, err
	}
	plan, err := ownedtree.Capture(ctx, source.root, []string{operation})
	if err != nil {
		return report, err
	}
	currentBirth, currentChange, err := source.recoveryImportTimes(operation)
	if err != nil || currentBirth != birth || currentChange != change {
		return report, fmt.Errorf("initial import metadata changed during inspection: %v", err)
	}
	data, err := json.Marshal(plan)
	if err != nil {
		return report, err
	}
	return RecoveryImportInspection{Version: 1, SourceRoot: source.config.Root, TargetID: targetID,
		BootID: e.boot, Attachment: *e.config.BoundedStore, Inventory: plan, InventorySHA256: sum(data),
		DirectoryBirth: birth, DirectoryChange: change, InspectedAt: now(), ReservationAbsent: true}, nil
}

// This unaccepted metadata stage grants no durable missing-entry replay or
// target reservation. Remove its complete inspected inventory without needing
// another inode/allocation inside a full store. A lost response requires a
// fresh inspection if anything remains; an absent stage is not adopted again.
func (e *Engine) cancelRecoveryImportInitialLocked(ctx context.Context, source *Engine, inspected RecoveryImportInspection) (RecoveryImportCancellation, error) {
	var report RecoveryImportCancellation
	if inspected.Owner != "" || inspected.PreparationSHA256 != "" || inspected.Cancelled != nil || !inspected.ReservationAbsent {
		return report, fmt.Errorf("initial import inspection must not invent a committed reservation")
	}
	current, err := e.inspectRecoveryImportInitialLocked(ctx, source, inspected.TargetID)
	if err != nil {
		return report, err
	}
	plan := inspected.Inventory
	if !reflect.DeepEqual(inspected.Attachment, *e.config.BoundedStore) {
		plan, err = rebindEpochRetirementPlan(plan, inspected.Attachment, *e.config.BoundedStore)
		if err != nil {
			return report, err
		}
	}
	if !reflect.DeepEqual(current.Inventory, plan) || current.DirectoryBirth != inspected.DirectoryBirth || current.DirectoryChange != inspected.DirectoryChange {
		return report, fmt.Errorf("initial import metadata or generation changed; inspect its remainder again")
	}
	if err := ownedtree.RemoveRemaining(ctx, source.root, plan); err != nil {
		return report, err
	}
	operation := "forks/" + inspected.TargetID
	if _, err := source.root.Lstat(operation); !errors.Is(err, os.ErrNotExist) {
		return report, fmt.Errorf("cancelled initial import metadata remains or was replaced: %v", err)
	}
	parent, err := source.root.Open("forks")
	if err != nil {
		return report, err
	}
	if err := errors.Join(parent.Sync(), parent.Close()); err != nil {
		return report, err
	}
	data, err := json.Marshal(inspected)
	if err != nil {
		return report, err
	}
	return RecoveryImportCancellation{Version: 1, SourceRoot: source.config.Root, TargetID: inspected.TargetID,
		InspectionSHA256: sum(data), CompletedAt: now(), ReservationAbsent: true}, nil
}
