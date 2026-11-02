//go:build linux

package lifecycle

import (
	"errors"
	"fmt"
	"os"
	"reflect"

	"factorybox/internal/sysboxcapture"
)

// An acknowledged process rollback is not a checkpoint. Once the source is
// closed, its exact failed Pause and completed guard can remain as diagnostics
// without preventing worker exit. This observes metadata only: it never thaws,
// deletes backing, manufactures a checkpoint or authorizes epoch retirement.
func (e *Engine) exitRecoveredFailedCapture(record Record, id, relative string) (bool, *sysboxcapture.FilesystemTree, error) {
	if epochClosedRecord(record) != nil {
		return false, nil, nil
	}
	var acknowledgement struct {
		Token           string `json:"token"`
		CaptureInFlight bool   `json:"capture_in_flight"`
	}
	err := e.readCheckpointJSON(relative+"/guard/guard-capture-aborted.json", &acknowledgement)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil, nil
	}
	if err != nil || acknowledgement.Token != id || acknowledgement.CaptureInFlight {
		return false, nil, fmt.Errorf("worker exit capture abort acknowledgement changed: %v", err)
	}
	index := -1
	for i, pause := range record.PauseHistory {
		if pause.SnapshotID == id {
			if index != -1 {
				return false, nil, fmt.Errorf("worker exit failed capture has duplicate Pause history")
			}
			index = i
		}
	}
	if index == -1 {
		return false, nil, fmt.Errorf("worker exit failed capture lacks its retained Pause")
	}
	pause := record.PauseHistory[index]
	if pause.RejectedAt == "" || pause.Error == "" || pause.SourcePID <= 1 ||
		pause.CompletedAt != "" || pause.CheckpointSHA256 != "" || pause.Resume != nil {
		return false, nil, fmt.Errorf("worker exit failed capture is not an unpublished recovered Pause")
	}
	var config captureGuardConfig
	if err := e.readCheckpointJSON(relative+"/guard/guard-intent.json", &config); err != nil {
		return false, nil, err
	}
	original := record
	original.Phase, original.Delete = "pausing", nil
	original.PauseHistory = append([]PauseOperation(nil), record.PauseHistory[:index]...)
	original.Pause = &pause
	original.Pause.RejectedAt, original.Pause.Error = "", ""
	data, err := sandboxRecordBytes(original)
	if err != nil || config.Version != 1 || config.Root != e.config.Root || config.Token != id ||
		!config.StopSource || config.PID != pause.SourcePID || config.Criu != e.config.Criu ||
		config.CriuSHA256 != e.config.CriuSHA256 || !reflect.DeepEqual(config.Source, original) ||
		config.RecordSHA != sum(append(data, '\n')) {
		return false, nil, fmt.Errorf("worker exit failed capture original source changed: %v", err)
	}
	// Reuse the pinned intent, outcome and original process-completion checks.
	// Unlike live reconciliation, do not reopen the now-deleted source/cgroup.
	if err := e.verifyAbortedCheckpointMetadata(original, id, relative, config); err != nil {
		return false, nil, err
	}
	anchors := captureGuardAnchors{}
	anchors.root, err = e.root.Open(".")
	if err != nil {
		return false, nil, err
	}
	defer anchors.root.Close()
	anchors.evidence, err = e.root.Open(relative + "/guard")
	if err != nil {
		return false, nil, err
	}
	defer anchors.evidence.Close()
	if err := captureGuardProcessReceipt(config, anchors); err != nil {
		return false, nil, err
	}
	abort, err := captureGuardAbortIntent(config, anchors)
	failure := &checkpointCaptureError{cause: errors.New(abort.Error)}
	retained := record.PauseHistory[index]
	if err != nil || abort.FailedAt != retained.RejectedAt ||
		sandboxErrorSummary(failure.Error()) != retained.Error {
		return false, nil, fmt.Errorf("worker exit failed capture differs from its retained failure: %v", err)
	}
	for _, path := range []string{"checkpoint.json", "capture-result.json", "filesystem.json"} {
		if _, err := e.root.Lstat(relative + "/" + path); !errors.Is(err, os.ErrNotExist) {
			return false, nil, fmt.Errorf("worker exit failed capture gained published backing: %s: %v", path, err)
		}
	}
	if record.Base == nil || record.Base.Immutable == nil {
		return true, nil, nil
	}
	// The writer binds the verified lower before copying mutable roots. A
	// failed copy has no manifest containing its underlying mountpoint identity.
	// Keep every byte; release only the currently verified read-only base alias.
	lower := e.config.Root + "/" + relative + "/filesystem/lower"
	if _, err := e.root.Lstat(relative + "/filesystem/lower"); errors.Is(err, os.ErrNotExist) {
		return true, nil, nil
	} else if err != nil {
		return false, nil, err
	}
	tree := *record.Base
	tree.Name, tree.Source, tree.Saved, tree.Alias = "lower", record.InputBundle+"/lower", lower, nil
	if err := sysboxcapture.VerifyImmutableTree(lower, tree); err != nil {
		return false, nil, err
	}
	return true, &tree, nil
}
