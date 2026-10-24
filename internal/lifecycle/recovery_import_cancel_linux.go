//go:build linux

package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"

	"factorybox/internal/ownedtree"
	"factorybox/internal/storefs"
	"golang.org/x/sys/unix"
)

type RecoveryImportInspection struct {
	Version           int                         `json:"version"`
	SourceRoot        string                      `json:"source_root"`
	TargetID          string                      `json:"target_id"`
	Owner             string                      `json:"owner"`
	PreparationSHA256 string                      `json:"preparation_sha256"`
	BootID            string                      `json:"boot_id"`
	Attachment        storefs.Active              `json:"attachment"`
	Inventory         ownedtree.Plan              `json:"inventory"`
	InventorySHA256   string                      `json:"inventory_sha256"`
	DirectoryBirth    string                      `json:"directory_birth"`
	DirectoryChange   string                      `json:"directory_change"`
	InspectedAt       string                      `json:"inspected_at"`
	Cancelled         *RecoveryImportCancellation `json:"cancelled,omitempty"`
	ReservationAbsent bool                        `json:"reservation_absent,omitempty"`
}

type RecoveryImportCancellation struct {
	Version           int    `json:"version"`
	SourceRoot        string `json:"source_root"`
	TargetID          string `json:"target_id"`
	Owner             string `json:"owner"`
	PreparationSHA256 string `json:"preparation_sha256"`
	InspectionSHA256  string `json:"inspection_sha256"`
	CompletedAt       string `json:"completed_at"`
	ReservationAbsent bool   `json:"reservation_absent,omitempty"`
}

func recoveryImportPreparationSHA(p recoveryImportPreparation) string {
	data, _ := json.Marshal(p)
	return sum(data)
}

func (e *Engine) recoveryImportCancellation(operation string, p recoveryImportPreparation) (*RecoveryImportCancellation, error) {
	var record RecoveryImportCancellation
	err := e.readCheckpointJSON(operation+"/import-cancelled.json", &record)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if record.Version != 1 || record.SourceRoot != p.Root || record.TargetID != p.TargetID ||
		record.Owner != p.Owner || record.PreparationSHA256 != recoveryImportPreparationSHA(p) ||
		!digest.MatchString(record.InspectionSHA256) || record.CompletedAt == "" {
		return nil, fmt.Errorf("import cancellation differs from its original reservation")
	}
	if _, err := e.root.Lstat(operation + "/recovery"); !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("cancelled import still owns payload or a replacement: %v", err)
	}
	return &record, nil
}

func (e *Engine) recoveryImportTimes(operation string) (string, string, error) {
	directory, err := e.root.Open(operation)
	if err != nil {
		return "", "", err
	}
	defer directory.Close()
	var stat unix.Statx_t
	if err := unix.Statx(int(directory.Fd()), "", unix.AT_EMPTY_PATH, unix.STATX_BTIME|unix.STATX_CTIME, &stat); err != nil {
		return "", "", err
	}
	if stat.Mask&(unix.STATX_BTIME|unix.STATX_CTIME) != unix.STATX_BTIME|unix.STATX_CTIME || stat.Btime.Sec <= 0 || stat.Ctime.Sec <= 0 {
		return "", "", fmt.Errorf("import directory generation is unavailable")
	}
	return fmt.Sprintf("%d.%09d", stat.Btime.Sec, stat.Btime.Nsec), fmt.Sprintf("%d.%09d", stat.Ctime.Sec, stat.Ctime.Nsec), nil
}

// The caller holds the current and retained controller locks and the live
// bounded-store lease. No old process/mount identity is adopted. A mounted
// partial import refuses inspection until its worker has been fenced and its
// exact store has been attached to a replacement worker.
func (e *Engine) inspectRecoveryImportLocked(ctx context.Context, source *Engine, targetID string) (RecoveryImportInspection, error) {
	var report RecoveryImportInspection
	if e.config.BoundedStore == nil || ctx.Err() != nil {
		return report, fmt.Errorf("import inspection requires a live bounded store: %v", ctx.Err())
	}
	operation := "forks/" + targetID
	if _, err := source.root.Lstat(operation + "/import.json"); errors.Is(err, os.ErrNotExist) {
		return e.inspectRecoveryImportInitialLocked(ctx, source, targetID)
	} else if err != nil {
		return report, err
	}
	p, err := source.readUnpublishedRecoveryImport(operation, targetID)
	if err != nil {
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
	if err != nil || birth != currentBirth || change != currentChange {
		return report, fmt.Errorf("import changed during inspection: %v", err)
	}
	completed, err := source.recoveryImportCancellation(operation, p)
	if err != nil {
		return report, err
	}
	data, err := json.Marshal(plan)
	if err != nil {
		return report, err
	}
	return RecoveryImportInspection{Version: 1, SourceRoot: source.config.Root, TargetID: targetID, Owner: p.Owner,
		PreparationSHA256: recoveryImportPreparationSHA(p), BootID: e.boot, Attachment: *e.config.BoundedStore,
		Inventory: plan, InventorySHA256: sum(data), DirectoryBirth: birth, DirectoryChange: change,
		InspectedAt: now(), Cancelled: completed}, nil
}

func (e *Engine) importCancellationForest(sourceRoot string) (*epochRetirementForest, *Engine, error) {
	if e.config.BoundedStore == nil || sourceRoot != e.config.Root && retainedControllerPath(e.config, sourceRoot) != nil {
		return nil, nil, fmt.Errorf("import cancellation requires an exact controller in the current bounded store")
	}
	forest, err := e.retirementForest()
	if err != nil {
		return nil, nil, err
	}
	source := forest.controllers[sourceRoot]
	if source == nil {
		forest.close()
		return nil, nil, fmt.Errorf("import controller is missing")
	}
	store := epochStoreRoot(e.config.Root)
	if canonical(store) != nil || privateDirectory(store) != nil || e.boundedObject(store) != nil {
		forest.close()
		return nil, nil, fmt.Errorf("import cancellation store identity changed")
	}
	root, err := os.OpenRoot(store)
	if err != nil {
		forest.close()
		return nil, nil, err
	}
	guard := &Engine{root: root}
	err = epochPreparationUnaccepted(source, guard)
	err = errors.Join(err, root.Close())
	if err != nil {
		forest.close()
		return nil, nil, err
	}
	return forest, source, nil
}

// InspectRecoveryImport is read-only and needs no free store allocation.
// Save its full output outside the bounded store before explicit cancellation.
func (e *Engine) InspectRecoveryImport(ctx context.Context, sourceRoot, targetID string) (RecoveryImportInspection, error) {
	forest, source, err := e.importCancellationForest(sourceRoot)
	if err != nil {
		return RecoveryImportInspection{}, err
	}
	defer forest.close()
	return e.inspectRecoveryImportLocked(ctx, source, targetID)
}

// Cancellation only withdraws unpublished file copies. It never removes the
// reservation, publishes a fork, adopts an old runtime, or releases a mount.
// Interrupted deletion requires a fresh complete inspection of the remainder.
func (e *Engine) cancelRecoveryImportLocked(ctx context.Context, source *Engine, inspected RecoveryImportInspection) (RecoveryImportCancellation, error) {
	var report RecoveryImportCancellation
	data, err := json.Marshal(inspected.Inventory)
	if err != nil || e.config.BoundedStore == nil || inspected.Version != 1 || inspected.SourceRoot != source.config.Root ||
		inspected.BootID != e.boot || inspected.InspectedAt == "" || inspected.Cancelled != nil ||
		!digest.MatchString(inspected.InventorySHA256) || sum(data) != inspected.InventorySHA256 {
		return report, fmt.Errorf("import cancellation requires its exact original inspection")
	}
	if inspected.ReservationAbsent {
		return e.cancelRecoveryImportInitialLocked(ctx, source, inspected)
	}
	operation := "forks/" + inspected.TargetID
	p, err := source.readUnpublishedRecoveryImport(operation, inspected.TargetID)
	if err != nil {
		return report, err
	}
	if p.Owner != inspected.Owner || inspected.PreparationSHA256 != recoveryImportPreparationSHA(p) {
		return report, fmt.Errorf("import inspection differs from its original reservation")
	}
	identity, err := json.Marshal(inspected)
	if err != nil {
		return report, err
	}
	completed, err := source.recoveryImportCancellation(operation, p)
	if err != nil {
		return report, err
	}
	if completed != nil {
		if completed.InspectionSHA256 != sum(identity) {
			return report, fmt.Errorf("import was cancelled by a different inspection")
		}
		return *completed, nil
	}
	plan := inspected.Inventory
	if !reflect.DeepEqual(inspected.Attachment, *e.config.BoundedStore) {
		plan, err = rebindEpochRetirementPlan(plan, inspected.Attachment, *e.config.BoundedStore)
		if err != nil {
			return report, err
		}
	}
	current, err := e.inspectRecoveryImportLocked(ctx, source, inspected.TargetID)
	if err != nil {
		return report, err
	}
	if !reflect.DeepEqual(current.Inventory, plan) || current.DirectoryBirth != inspected.DirectoryBirth || current.DirectoryChange != inspected.DirectoryChange {
		return report, fmt.Errorf("import inventory or generation changed; inspect the remaining copy again")
	}
	// Only the exact recovery subtree is disposable. Recompute its inventory
	// under the same locks after proving the caller's entire original stage.
	if _, err := source.root.Lstat(operation + "/recovery"); err == nil {
		payload, err := ownedtree.Capture(ctx, source.root, []string{operation + "/recovery"})
		if err != nil {
			return report, err
		}
		if err := ownedtree.RemoveRemaining(ctx, source.root, payload); err != nil {
			return report, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return report, err
	}
	if _, err := source.root.Lstat(operation + "/recovery"); !errors.Is(err, os.ErrNotExist) {
		return report, fmt.Errorf("cancelled import payload remains or was replaced: %v", err)
	}
	entries, err := source.exitEntries(operation)
	if err != nil {
		return report, err
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), ".checkpoint-") {
			continue
		}
		if err := source.recoveryImportTemporary(operation, entry.Name()); err != nil {
			return report, err
		}
		control, err := ownedtree.Capture(ctx, source.root, []string{operation + "/" + entry.Name()})
		if err != nil {
			return report, err
		}
		if err := ownedtree.RemoveRemaining(ctx, source.root, control); err != nil {
			return report, err
		}
	}
	directory, err := source.root.Open(operation)
	if err != nil {
		return report, err
	}
	if err := errors.Join(unix.Syncfs(int(directory.Fd())), directory.Close()); err != nil {
		return report, err
	}
	report = RecoveryImportCancellation{Version: 1, SourceRoot: source.config.Root, TargetID: p.TargetID,
		Owner: p.Owner, PreparationSHA256: recoveryImportPreparationSHA(p), InspectionSHA256: sum(identity), CompletedAt: now()}
	if err := source.publishCheckpoint(operation, "import-cancelled.json", report); err != nil {
		return RecoveryImportCancellation{}, err
	}
	return report, nil
}

func (e *Engine) CancelRecoveryImport(ctx context.Context, inspected RecoveryImportInspection) (RecoveryImportCancellation, error) {
	forest, source, err := e.importCancellationForest(inspected.SourceRoot)
	if err != nil {
		return RecoveryImportCancellation{}, err
	}
	defer forest.close()
	return e.cancelRecoveryImportLocked(ctx, source, inspected)
}

func (e *Engine) cancelledRecoveryImport(ctx context.Context, targetID string) bool {
	operation := "forks/" + targetID
	p, err := e.readUnpublishedRecoveryImport(operation, targetID)
	if err != nil {
		return false
	}
	completed, err := e.recoveryImportCancellation(operation, p)
	return err == nil && completed != nil && e.inspectUnpublishedRecoveryExit(ctx, operation, targetID) == nil
}
