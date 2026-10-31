//go:build linux

package lifecycle

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"factorybox/internal/sysboxcapture"
	"golang.org/x/sys/unix"
)

type failedCaptureDeleteAdmission struct {
	Version    int               `json:"version"`
	SourceID   string            `json:"source_id"`
	Owner      string            `json:"owner"`
	RuntimeID  string            `json:"runtime_id"`
	SnapshotID string            `json:"snapshot_id"`
	RecordSHA  string            `json:"record_sha256"`
	Evidence   map[string]string `json:"evidence_sha256"`
	AdmittedAt string            `json:"admitted_at"`
}

// An explicit Delete may destroy an abandoned failed capture without pretending
// that its process state is safe to resume. Never thaw it or replay CRIU. The
// original controller, guard and writer must all be gone, the original source
// must remain held and untraced, and no retained CRIU helper may have been armed.
// The ordinary Delete path still verifies and removes only the owned runtime.
func (e *Engine) admitFailedPauseDelete(record Record) (resultErr error) {
	if record.Phase != "pausing" || record.Delete != nil || record.Pause == nil ||
		record.Pause.CompletedAt != "" || record.Pause.RejectedAt != "" ||
		record.Pause.Resume != nil {
		return fmt.Errorf("Delete lacks an abandoned incomplete Pause")
	}
	relative, err := checkpointPath(record.ID, record.Pause.SnapshotID)
	if err != nil {
		return err
	}
	anchors := captureGuardAnchors{}
	defer func() { resultErr = errors.Join(resultErr, anchors.close()) }()
	anchors.root, err = e.root.Open(".")
	if err != nil {
		return err
	}
	anchors.evidence, err = e.root.Open(relative + "/guard")
	if err != nil {
		return err
	}
	admission := failedCaptureDeleteAdmission{Version: 1, SourceID: record.ID,
		Owner: record.Owner, RuntimeID: record.RuntimeID, SnapshotID: record.Pause.SnapshotID,
		Evidence: map[string]string{}}
	read := func(name string, value any) error {
		data, err := captureCompletionBytes(anchors.root, relative+"/"+name, checkpointManifestLimit)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, value); err != nil {
			return err
		}
		admission.Evidence[name] = sum(data)
		return nil
	}
	var config captureGuardConfig
	if err := read("guard/guard-intent.json", &config); err != nil {
		return err
	}
	if config.Token != record.Pause.SnapshotID || !config.StopSource ||
		config.Root != e.config.Root || config.Criu != e.config.Criu ||
		config.CriuSHA256 != e.config.CriuSHA256 || !digest.MatchString(config.CriuSHA256) ||
		!reflect.DeepEqual(config.Source, record) {
		return fmt.Errorf("failed Pause Delete differs from its original guard")
	}
	admission.RecordSHA = config.RecordSHA
	var intent checkpointIntent
	if err := read("checkpoint-intent.json", &intent); err != nil {
		return err
	}
	if intent.Version != 1 || intent.ID != config.Token || !intent.StopSource ||
		intent.SourceID != record.ID || intent.RuntimeID != record.RuntimeID ||
		intent.Owner != record.Owner || intent.BootID != record.BootID ||
		intent.ConfigSHA256 != record.ConfigSHA256 || intent.RuntimeSHA256 != record.RuntimeSHA256 ||
		intent.CriuSHA256 != e.config.CriuSHA256 || intent.ManagerRoot != e.config.ManagerRoot ||
		intent.RequestedAt == "" {
		return fmt.Errorf("failed Pause Delete differs from its original checkpoint intent")
	}
	for _, path := range []string{"checkpoint.json", "capture-result.json", "filesystem.json", "filesystem",
		"guard/criu-retained-arm.json", "guard/capture-process-complete.json", "guard/capture-process-restored.json"} {
		if _, err := e.root.Lstat(relative + "/" + path); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("failed Pause Delete cannot discard a completed, retained or uncertain capture: %s: %v", path, err)
		}
	}
	var outcome struct {
		Config          captureGuardConfig `json:"config"`
		Reason          string             `json:"reason"`
		Status          string             `json:"status"`
		CaptureInFlight bool               `json:"capture_in_flight"`
		Passed          bool               `json:"passed"`
		Error           string             `json:"error,omitempty"`
	}
	if err := read("guard/guard-outcome.json", &outcome); err != nil {
		return err
	}
	if !reflect.DeepEqual(outcome.Config, config) || !outcome.Passed || !outcome.CaptureInFlight ||
		outcome.Error != "" || outcome.Reason != "controller_requested_recovery" ||
		outcome.Status != "original_source_held_for_capture_reconciliation" {
		return fmt.Errorf("failed Pause Delete lacks its original completed hold outcome")
	}
	var armed struct {
		Token string `json:"token"`
		PID   int    `json:"helper_pid"`
	}
	if err := read("guard/guard-armed.json", &armed); err != nil {
		return err
	}
	var worker captureWorkerIntent
	if err := read("guard/capture-worker-intent.json", &worker); err != nil {
		return err
	}
	if armed.Token != config.Token || armed.PID <= 1 || worker.Token != config.Token ||
		worker.PID <= 1 || worker.Start == "" || worker.SourcePID != config.PID ||
		worker.CriuSHA256 != config.CriuSHA256 || worker.Scope != "ordinary complete retained-source capture" {
		return fmt.Errorf("failed Pause Delete lacks its exact original guard/writer identities")
	}
	seen := map[int]bool{config.PID: true}
	for _, pid := range []int{config.Controller, armed.PID, worker.PID} {
		if pid <= 1 || seen[pid] {
			return fmt.Errorf("failed Pause Delete process identities overlap")
		}
		seen[pid] = true
		// Old guard receipts do not carry every process birth. Refuse even a
		// reused PID rather than granting destructive authority from it.
		if _, err := processBirth(pid); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("failed Pause Delete has a live or unproved controller, guard or writer: %v", err)
		}
	}
	var workerOutcome struct {
		Token   string               `json:"token"`
		Passed  bool                 `json:"passed"`
		Error   string               `json:"error,omitempty"`
		Capture sysboxcapture.Result `json:"capture"`
	}
	if err := read("guard/capture-worker-outcome.json", &workerOutcome); err != nil {
		return err
	}
	if workerOutcome.Token != config.Token || workerOutcome.Passed || workerOutcome.Error == "" ||
		workerOutcome.Capture.SourcePID != config.PID ||
		workerOutcome.Capture.Images != filepath.Join(e.config.Root, relative, "images") ||
		workerOutcome.Capture.Filesystem != nil {
		return fmt.Errorf("failed Pause Delete lacks its terminal original failed writer")
	}
	sourceState, err := captureCompletionBytes(anchors.root, relative+"/source-state.json", checkpointManifestLimit)
	if err != nil || sum(sourceState) != workerOutcome.Capture.SourceStateSHA {
		return fmt.Errorf("failed Pause Delete source-state evidence changed: %v", err)
	}
	admission.Evidence["source-state.json"] = sum(sourceState)
	if err := captureRetainedNetwork(config, anchors); err != nil {
		return err
	}
	var network any
	if err := read("guard/capture-network-rollback.json", &network); err != nil {
		return err
	}
	anchors.group, err = captureGuardOpen(cgroupRoot + "/" + record.RuntimeID)
	if err != nil {
		return err
	}
	fd, err := unix.PidfdOpen(config.PID, 0)
	if err != nil {
		return err
	}
	anchors.pid = os.NewFile(uintptr(fd), "failed Pause Delete original source pidfd")
	if err := captureGuardCaptureReturned(config, anchors); err != nil {
		return err
	}
	var previous failedCaptureDeleteAdmission
	err = e.readCheckpointJSON(relative+"/capture-delete-admission.json", &previous)
	if err == nil {
		admission.AdmittedAt = previous.AdmittedAt
		if admission.AdmittedAt == "" || !reflect.DeepEqual(previous, admission) {
			return fmt.Errorf("failed Pause Delete admission changed")
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	admission.AdmittedAt = now()
	return e.publishCheckpoint(relative, "capture-delete-admission.json", admission)
}
