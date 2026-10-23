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

type PauseOperation struct {
	SnapshotID       string         `json:"snapshot_id"`
	CheckpointSHA256 string         `json:"checkpoint_sha256,omitempty"`
	RequestedAt      string         `json:"requested_at"`
	CompletedAt      string         `json:"completed_at,omitempty"`
	RejectedAt       string         `json:"rejected_at,omitempty"`
	Error            string         `json:"error,omitempty"`
	SourcePID        int            `json:"source_pid"`
	Before           SandboxMemory  `json:"before"`
	After            SandboxMemory  `json:"after"`
	Snapshot         SnapshotMemory `json:"snapshot"`
	Resume           *Operation     `json:"resume,omitempty"`
}

func (e *Engine) Pause(ctx context.Context, id, snapshot string) (View, error) {
	lock, err := e.lock()
	if err != nil {
		return View{}, err
	}
	defer lock.Close()
	relative, err := checkpointPath(id, snapshot)
	if err != nil {
		return View{}, err
	}
	record, err := e.read(id)
	if err != nil {
		return View{}, err
	}
	if record.Pause != nil && record.Pause.SnapshotID == snapshot && record.Pause.CompletedAt != "" {
		if record.Phase == "running" && record.Pause.Resume != nil && record.Pause.Resume.CompletedAt != "" {
			_, state, err := e.resumeCompleted(ctx, lock, record)
			return View{Record: record, Runtime: state}, err
		}
		state, err := e.state(ctx, record, lock)
		if err != nil || record.Phase != "paused" || state == nil || state.Status != "stopped" {
			return View{Record: record, Runtime: state}, errors.Join(err, fmt.Errorf("pause replay lacks its stopped original runtime"))
		}
		return View{Record: record, Runtime: state}, nil
	}
	if record.Phase == "running" {
		if err := e.admitSandboxMetadata(record, false); err != nil {
			return View{Record: record}, err
		}
		if err := e.admitWorkerMetadata(ctx, workerMetadataDelta{CaptureSource: &record, CaptureID: snapshot}); err != nil {
			return View{Record: record}, err
		}
		if err := e.admitStorageWrite(); err != nil {
			return View{Record: record}, err
		}
		if _, err := e.root.Lstat(relative); !errors.Is(err, os.ErrNotExist) {
			return View{Record: record}, fmt.Errorf("pause snapshot identity already exists or is inaccessible: %v", err)
		}
		state, err := e.state(ctx, record, lock)
		if err != nil || state == nil || state.Status != "running" {
			return View{Record: record, Runtime: state}, errors.Join(err, fmt.Errorf("pause requires the running original runtime"))
		}
		before, err := e.sandboxMemory(record)
		if err != nil {
			return View{Record: record}, err
		}
		record.Phase = "pausing"
		if record.Pause != nil {
			record.PauseHistory = append(record.PauseHistory, *record.Pause)
		}
		record.Pause = &PauseOperation{SnapshotID: snapshot, RequestedAt: now(), SourcePID: state.PID, Before: before}
		if err := e.write(record); err != nil {
			return View{Record: record}, err
		}
	}
	if record.Phase != "pausing" || record.Pause == nil || record.Pause.SnapshotID != snapshot {
		return View{Record: record}, fmt.Errorf("pause does not match the persisted transition")
	}
	var captured Checkpoint
	_, generationErr := e.root.Lstat(relative)
	if errors.Is(generationErr, os.ErrNotExist) {
		captured, err = e.captureCheckpoint(ctx, lock, record, snapshot, relative, true)
	} else if generationErr != nil {
		return View{Record: record}, generationErr
	} else {
		captured, err = e.reconcileCheckpoint(ctx, record, snapshot, relative)
	}
	if err != nil {
		var admission *checkpointAdmissionError
		var captureFailure *checkpointCaptureError
		rejectedAt, failure := "", ""
		if errors.As(err, &admission) && admission.recovered {
			rejectedAt, failure = admission.rejectedAt, admission.Error()
		} else if errors.As(err, &captureFailure) && captureFailure.recovered {
			rejectedAt, failure = captureFailure.failedAt, captureFailure.Error()
		}
		if rejectedAt != "" {
			state, stateErr := e.state(ctx, record, lock)
			if stateErr != nil || state == nil || state.Status != "running" ||
				state.PID != record.Pause.SourcePID {
				return View{Record: record, Runtime: state}, errors.Join(err, stateErr,
					fmt.Errorf("rejected pause lacks its confirmed original running source"))
			}
			record.Pause.RejectedAt, record.Pause.Error = rejectedAt, sandboxErrorSummary(failure)
			record.PauseHistory = append(record.PauseHistory, *record.Pause)
			record.Pause, record.Phase = nil, "running"
			return View{Record: record, Runtime: state}, errors.Join(err, e.write(record))
		}
		return View{Record: record}, err
	}
	if !captured.SourceStopped || captured.SourcePID != record.Pause.SourcePID {
		return View{Record: record}, fmt.Errorf("pause checkpoint did not stop its original source")
	}
	state, err := e.state(ctx, record, lock)
	if err != nil || state == nil || state.Status != "stopped" {
		return View{Record: record, Runtime: state}, errors.Join(err, fmt.Errorf("pause has no confirmed stopped runtime"))
	}
	snapshotMemory, err := e.releaseSnapshotMemory(ctx, relative)
	if err != nil {
		return View{Record: record, Runtime: state}, err
	}
	after, err := e.stoppedSandboxMemory(ctx, record)
	if err != nil {
		return View{Record: record, Runtime: state}, err
	}
	identity, err := json.Marshal(captured)
	if err != nil {
		return View{Record: record, Runtime: state}, err
	}
	record.Pause.CheckpointSHA256 = sum(identity)
	record.Pause.Snapshot, record.Pause.After, record.Pause.CompletedAt = snapshotMemory, after, now()
	record.Phase = "paused"
	return View{Record: record, Runtime: state}, e.write(record)
}

func (e *Engine) recoverStoppedCheckpoint(relative string, record Record, id string, captured Checkpoint) (string, error) {
	var config captureGuardConfig
	if err := e.readCheckpointJSON(relative+"/guard/guard-intent.json", &config); err != nil {
		return "", err
	}
	if !config.StopSource || config.Token != id || config.PID != captured.SourcePID ||
		config.Root != e.config.Root || !reflect.DeepEqual(config.Source, record) {
		return "", fmt.Errorf("stopped capture recovery differs from its original guard")
	}
	var outcome struct {
		Config          captureGuardConfig `json:"config"`
		Passed          bool               `json:"passed"`
		Status          string             `json:"status"`
		Reason          string             `json:"reason"`
		CaptureInFlight bool               `json:"capture_in_flight"`
		Error           string             `json:"error,omitempty"`
	}
	if err := e.readCheckpointJSON(relative+"/guard/guard-outcome.json", &outcome); err != nil {
		return "", fmt.Errorf("stopped capture guard remains unresolved: %w", err)
	}
	if !outcome.Passed || !reflect.DeepEqual(outcome.Config, config) ||
		(outcome.Status != "source_stopped_guard_disarmed" && outcome.Status != "original_source_stopped_capture_unconfirmed") {
		return "", fmt.Errorf("stopped capture guard has no confirmed original-source outcome")
	}
	data, err := e.root.ReadFile("objects/" + record.ID + "/record.json")
	if err != nil || sum(data) != config.RecordSHA {
		return "", fmt.Errorf("stopped capture original record changed: %v", err)
	}
	if err := e.validateCgroup(record); err != nil {
		return "", err
	}
	group, err := captureGuardOpen(cgroupRoot + "/" + record.RuntimeID)
	if err != nil {
		return "", err
	}
	defer group.Close()
	events, err := captureGuardEvents(group)
	if err != nil || events["populated"] != 0 {
		return "", fmt.Errorf("stopped original source became populated: %v", err)
	}
	raw, err := json.Marshal(captured)
	if err != nil {
		return "", err
	}
	var recovered checkpointRecovery
	err = e.readCheckpointJSON(relative+"/checkpoint-reconciled.json", &recovered)
	if err == nil {
		if recovered.Version != 1 || recovered.CaptureSHA256 != sum(raw) ||
			!reflect.DeepEqual(recovered.Config, config) || recovered.Status != "source_stopped_guard_disarmed" {
			return "", fmt.Errorf("stopped capture reconciliation differs")
		}
		return recovered.ReleasedAt, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	recovered = checkpointRecovery{Version: 1, Config: config, CaptureSHA256: sum(raw),
		ReleasedAt: now(), Status: "source_stopped_guard_disarmed"}
	return recovered.ReleasedAt, e.publishCheckpoint(relative, "checkpoint-reconciled.json", recovered)
}
