//go:build linux

package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"

	"factorybox/internal/ownedtree"
)

type failedCaptureBackingIntent struct {
	Version         int                   `json:"version"`
	Request         SandboxBackingRequest `json:"request"`
	SnapshotID      string                `json:"snapshot_id"`
	AdmissionSHA256 string                `json:"admission_sha256"`
	Controller      string                `json:"controller"`
	Capture         KernelObject          `json:"capture"`
	Directory       KernelObject          `json:"directory"`
	Bundle          KernelObject          `json:"bundle"`
	InventorySHA256 string                `json:"inventory_sha256"`
	Payloads        []string              `json:"payloads"`
	StartedAt       string                `json:"started_at"`
	Before          StorageCapacity       `json:"before"`
}

// A failed, unpublished capture is not a recovery checkpoint. Its exact failed
// Delete admission can release its private images only after confirmed Delete.
// Keep all guard, writer, source-state and request evidence. The separate journal
// also completes this stage when replaying an older overlay-only retirement.
// The caller holds all retained epochs and has verified runtime/bundle absence.
func (e *Engine) retireDeletedFailedCaptureBacking(ctx context.Context, record Record, request SandboxBackingRequest, forest *epochRetirementForest) error {
	pause := record.Pause
	if pause == nil || pause.CompletedAt != "" || pause.RejectedAt != "" || pause.Resume != nil {
		return nil
	}
	proved, err := e.deletedFailedPauseCapture(record, pause.SnapshotID)
	if err != nil || !proved {
		return errors.Join(err, fmt.Errorf("failed capture has no exact confirmed Delete admission"))
	}
	generation, err := checkpointPath(record.ID, pause.SnapshotID)
	if err != nil {
		return err
	}
	if err := e.boundedObject(e.config.Root + "/" + generation); err != nil {
		return err
	}
	if err := privateDirectory(e.config.Root + "/" + generation); err != nil {
		return err
	}
	capture, err := directoryIdentity(e.config.Root + "/" + generation)
	if err != nil {
		return err
	}
	var admission failedCaptureDeleteAdmission
	if err := e.readCheckpointJSON(generation+"/capture-delete-admission.json", &admission); err != nil {
		return err
	}
	encodedAdmission, err := json.Marshal(admission)
	if err != nil {
		return err
	}
	admissionSHA := sum(encodedAdmission)
	allowed := []string{generation + "/images"}
	if admission.Version == 2 {
		allowed = append(allowed, generation+"/filesystem")
	}
	slices.Sort(allowed)
	journal, relative, err := e.privateBackingJournal(record, "failed-capture")
	if err != nil {
		return err
	}
	defer journal.root.Close()
	alias := ""
	if admission.Version == 2 {
		alias, err = journal.privateJournalChild(relative, "alias")
		if err != nil {
			return err
		}
		relative, err = journal.privateJournalChild(relative, "payload")
		if err != nil {
			return err
		}
	}
	directory, err := directoryIdentity(journal.config.Root + "/" + relative)
	if err != nil {
		return err
	}
	dependencies := func() error {
		for _, controller := range forest.controllers {
			if err := controller.sandboxBackingDependencies(ctx, e.config.Root, allowed); err != nil {
				return err
			}
		}
		return nil
	}
	absent := func() error {
		for _, path := range allowed {
			if _, err := e.root.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("retired failed capture has remaining or successor backing: %s: %v", path, err)
			}
		}
		return nil
	}
	var intent failedCaptureBackingIntent
	err = journal.readCheckpointJSON(relative+"/retirement-intent.json", &intent)
	first := errors.Is(err, os.ErrNotExist)
	if err != nil && !first {
		return err
	}
	if first {
		if err := journal.resetUnacceptedRetirementPreparation(ctx, relative); err != nil {
			return err
		}
		if err := dependencies(); err != nil {
			return err
		}
		if admission.Version == 2 {
			if err := e.uncoverRecoveredCapture(journal, alias, record, request, pause.SnapshotID,
				generation, admissionSHA, capture); err != nil {
				return err
			}
		}
		paths := []string{}
		for _, payload := range allowed {
			if info, err := e.root.Lstat(payload); err == nil {
				// CRIU's image directory may carry the original guest's mapped
				// UID. Its root-owned private capture parent and confirmed Delete
				// establish ownership, not a requirement to chown guest payloads.
				if !info.IsDir() {
					return fmt.Errorf("failed capture backing is not its original private directory")
				}
				if err := e.boundedObject(e.config.Root + "/" + payload); err != nil {
					return err
				}
				paths = append(paths, payload)
			} else if !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		plan := ownedtree.Plan{Version: 1, Paths: paths}
		if len(paths) != 0 {
			plan, err = ownedtree.Capture(ctx, e.root, paths)
			if err != nil {
				return err
			}
		}
		before, err := e.storageCapacity()
		if err != nil {
			return err
		}
		hash, err := journal.writeRetirementInventory(relative, plan)
		if err != nil {
			return err
		}
		intent = failedCaptureBackingIntent{
			Version: 1, Request: request, SnapshotID: pause.SnapshotID, AdmissionSHA256: admissionSHA,
			Controller: e.config.Root, Capture: capture, Directory: directory, Bundle: *record.InputDirectory,
			InventorySHA256: hash, Payloads: paths, StartedAt: now(), Before: before,
		}
		if err := journal.publishCheckpoint(relative, "retirement-intent.json", intent); err != nil {
			return err
		}
	}
	if intent.Version != 1 || intent.Request != request || intent.SnapshotID != pause.SnapshotID ||
		intent.AdmissionSHA256 != admissionSHA || intent.Controller != e.config.Root ||
		intent.Capture != capture || intent.Directory != directory || intent.Bundle != *record.InputDirectory ||
		!digest.MatchString(intent.InventorySHA256) || intent.StartedAt == "" ||
		len(intent.Payloads) > len(allowed) {
		return fmt.Errorf("failed capture retirement changed its original owner/input")
	}
	for i, path := range intent.Payloads {
		if !slices.Contains(allowed, path) {
			return fmt.Errorf("failed capture retirement gained an unauthorized payload")
		}
		if i > 0 && path <= intent.Payloads[i-1] {
			return fmt.Errorf("failed capture retirement has duplicate or unordered payloads")
		}
	}
	data, _ := json.Marshal(intent)
	var result SandboxBackingRetirement
	if err := journal.readCheckpointJSON(relative+"/retirement-complete.json", &result); err == nil {
		if result.Version != 1 || result.PayloadKind != "failed-capture-backing" ||
			result.Request != request || result.IntentSHA256 != sum(data) || result.CompletedAt == "" ||
			!reflect.DeepEqual(result.Before, intent.Before) {
			return fmt.Errorf("failed capture completion differs from its original intent")
		}
		return absent()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	plan, err := journal.readRetirementInventory(relative, intent.InventorySHA256)
	if err != nil || !reflect.DeepEqual(plan.Paths, intent.Payloads) {
		return errors.Join(err, fmt.Errorf("failed capture inventory differs from its exact accepted paths"))
	}
	if err := dependencies(); err != nil {
		return err
	}
	if proved, err := e.deletedFailedPauseCapture(record, pause.SnapshotID); err != nil || !proved {
		return errors.Join(err, fmt.Errorf("failed capture Delete proof changed before retirement"))
	}
	if len(intent.Payloads) != 0 {
		if err := ownedtree.RemoveRemaining(ctx, e.root, plan); err != nil {
			return err
		}
	}
	if err := absent(); err != nil {
		return err
	}
	after, err := e.storageCapacity()
	if err != nil {
		return err
	}
	result = SandboxBackingRetirement{Version: 1, PayloadKind: "failed-capture-backing",
		Request: request, IntentSHA256: sum(data), CompletedAt: now(), Before: intent.Before, After: after}
	return journal.publishCheckpoint(relative, "retirement-complete.json", result)
}
