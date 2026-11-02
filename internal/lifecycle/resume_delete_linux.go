//go:build linux

package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
)

type failedResumeDeleteAdmission struct {
	Version       int    `json:"version"`
	SourceID      string `json:"source_id"`
	Owner         string `json:"owner"`
	RuntimeID     string `json:"runtime_id"`
	SnapshotID    string `json:"snapshot_id"`
	RecordSHA     string `json:"record_sha256"`
	IntentSHA     string `json:"intent_sha256"`
	CheckpointSHA string `json:"checkpoint_sha256"`
	PreparedSHA   string `json:"prepared_sha256"`
	AdmittedAt    string `json:"admitted_at"`
}

func failedResumeDeleteRecord(record Record) error {
	if record.Phase != "resuming" || record.Delete != nil ||
		!record.Start.Attempted || record.Start.RequestedAt == "" ||
		record.Start.CompletedAt == "" || record.Start.Error != "" || record.RuntimeCreated == "" ||
		record.Pause == nil || !tokenID.MatchString(record.Pause.SnapshotID) ||
		!digest.MatchString(record.Pause.CheckpointSHA256) || record.Pause.RequestedAt == "" ||
		record.Pause.CompletedAt == "" || record.Pause.RejectedAt != "" ||
		record.Pause.Resume == nil || record.Pause.Resume.RequestedAt == "" ||
		!record.Pause.Resume.Attempted || record.Pause.Resume.CompletedAt != "" ||
		record.Pause.Resume.Error == "" {
		return fmt.Errorf("Delete requires an original attempted and failed Resume of a committed Pause")
	}
	return nil
}

// Admission verifies the complete original capture before granting Delete.
func (e *Engine) failedResumeDeleteEvidence(ctx context.Context, record Record) (failedResumeDeleteAdmission, error) {
	var admission failedResumeDeleteAdmission
	if err := failedResumeDeleteRecord(record); err != nil {
		return admission, err
	}
	generation, err := checkpointPath(record.ID, record.Pause.SnapshotID)
	if err != nil {
		return admission, err
	}
	checkpoint, err := e.publishedCheckpointMetadata(ctx, record, record.Pause.SnapshotID, generation)
	if err != nil {
		return admission, err
	}
	return e.failedResumeDeleteMetadata(ctx, record, checkpoint)
}

func (e *Engine) failedResumeDeleteMetadata(ctx context.Context, record Record, checkpoint Checkpoint) (failedResumeDeleteAdmission, error) {
	var admission failedResumeDeleteAdmission
	if err := failedResumeDeleteRecord(record); err != nil {
		return admission, err
	}
	data, _ := json.Marshal(checkpoint)
	if !checkpoint.SourceStopped || checkpoint.Filesystem == nil ||
		sum(data) != record.Pause.CheckpointSHA256 ||
		!reflect.DeepEqual(checkpoint.Filesystem.SharedSource, record.SharedSource) {
		return admission, fmt.Errorf("failed Resume Delete differs from its complete original Pause")
	}
	relative := "objects/" + record.ID + "/resumes/" + record.Pause.SnapshotID
	for _, name := range []string{"resume-held.json", "resume-result.json"} {
		if _, err := e.root.Lstat(relative + "/" + name); !errors.Is(err, os.ErrNotExist) {
			return admission, fmt.Errorf("failed Resume Delete cannot discard a held or completed restore: %s: %v", name, err)
		}
	}
	var intent resumeIntent
	if err := e.readCheckpointJSON(relative+"/resume-intent.json", &intent); err != nil {
		return admission, err
	}
	if intent.Version != 1 || intent.Request != e.resumeRequest(record) ||
		intent.SnapshotSHA != record.Pause.CheckpointSHA256 ||
		intent.CriuSHA256 != e.config.CriuSHA256 || intent.RuntimeSHA256 != e.config.RuntimeSHA256 {
		return admission, fmt.Errorf("failed Resume Delete recipe differs from its original intent")
	}
	var prepared []CheckpointFile
	if err := e.readCheckpointJSON(relative+"/resume-prepared.json", &prepared); err != nil {
		return admission, err
	}
	actual, err := e.checkpointFiles(ctx, relative)
	if err != nil || !reflect.DeepEqual(prepared, checkpoint.Files) ||
		!checkpointPreparationIntact(prepared, actual) {
		return admission, fmt.Errorf("failed Resume Delete inputs changed: %v", err)
	}
	recordData, err := sandboxRecordBytes(record)
	if err != nil {
		return admission, err
	}
	intentData, _ := json.Marshal(intent)
	preparedData, _ := json.Marshal(prepared)
	return failedResumeDeleteAdmission{Version: 1, SourceID: record.ID, Owner: record.Owner,
		RuntimeID: record.RuntimeID, SnapshotID: record.Pause.SnapshotID,
		RecordSHA: sum(recordData), IntentSHA: sum(intentData),
		CheckpointSHA: record.Pause.CheckpointSHA256, PreparedSHA: sum(preparedData)}, nil
}

// Delete owns the original exclusive operation lock. Every accepted restore
// worker and backend inherits that lock, so it cannot still be running here.
// Require the original stopped runtime and wholly empty, unfrozen cgroup as
// separate positive facts. A timeout, Error string or missing result alone
// never grants destructive authority.
func (e *Engine) admitFailedResumeDelete(ctx context.Context, lock *os.File, record Record) error {
	admission, err := e.failedResumeDeleteEvidence(ctx, record)
	if err != nil {
		return err
	}
	state, err := e.state(ctx, record, lock)
	if err != nil || state == nil || state.Status != "stopped" || state.PID != 0 {
		return fmt.Errorf("failed Resume Delete requires its original stopped runtime: %v", err)
	}
	if err := e.requireEmptyCgroup(record); err != nil {
		return err
	}
	relative := "objects/" + record.ID + "/resumes/" + record.Pause.SnapshotID
	var previous failedResumeDeleteAdmission
	err = e.readCheckpointJSON(relative+"/resume-delete-admission.json", &previous)
	if err == nil {
		admission.AdmittedAt = previous.AdmittedAt
		if admission.AdmittedAt == "" || !reflect.DeepEqual(previous, admission) {
			return fmt.Errorf("failed Resume Delete admission changed")
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	admission.AdmittedAt = now()
	return e.publishCheckpoint(relative, "resume-delete-admission.json", admission)
}

func (e *Engine) deletedFailedResume(ctx context.Context, record Record, pause PauseOperation) (bool, error) {
	if pause.Resume == nil || pause.Resume.CompletedAt != "" {
		return false, nil
	}
	if record.Phase != "deleted" || epochClosedRecord(record) != nil ||
		record.Pause == nil || !reflect.DeepEqual(*record.Pause, pause) {
		return false, fmt.Errorf("failed Resume lacks confirmed original Delete")
	}
	original := record
	original.Phase, original.Delete = "resuming", nil
	// Capture retirement has its own ownership and complete-payload verifier.
	// Confirmed Delete may already have retired those RAM images. This reader
	// owns only the still-present private Resume inputs and their admission;
	// the checkpoint's retained manifest remains bound by its original hash.
	generation, err := checkpointPath(record.ID, pause.SnapshotID)
	if err != nil {
		return false, err
	}
	var checkpoint Checkpoint
	if err := e.readCheckpointJSON(generation+"/checkpoint.json", &checkpoint); err != nil {
		return false, err
	}
	expected, err := e.failedResumeDeleteMetadata(ctx, original, checkpoint)
	if err != nil {
		return false, err
	}
	relative := "objects/" + record.ID + "/resumes/" + pause.SnapshotID
	var admission failedResumeDeleteAdmission
	if err := e.readCheckpointJSON(relative+"/resume-delete-admission.json", &admission); err != nil {
		return false, err
	}
	expected.AdmittedAt = admission.AdmittedAt
	if expected.AdmittedAt == "" || !reflect.DeepEqual(expected, admission) {
		return false, fmt.Errorf("deleted failed Resume differs from its original admission")
	}
	return true, nil
}
