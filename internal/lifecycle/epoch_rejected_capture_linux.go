//go:build linux

package lifecycle

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
)

// A pre-capture rejection has no checkpoint payload or recovery authority.
// The closed record must retain its exact rejected Pause and the original
// guard's confirmed running-source outcome. This only classifies metadata;
// it never thaws a source, repeats capture or authorizes unknown files.
func (e *Engine) epochRejectedCapture(record Record, id, relative string) (bool, error) {
	if epochClosedRecord(record) != nil {
		return false, nil
	}
	var rejection checkpointAdmissionRejection
	if err := e.readCheckpointJSON(relative+"/admission-rejected.json", &rejection); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if rejection.Version != 1 || rejection.SourceID != record.ID || rejection.SnapshotID != id ||
		rejection.SourcePID <= 1 || rejection.RejectedAt == "" ||
		(rejection.Kind != "" && rejection.Kind != "filesystem" && rejection.Kind != "descriptors" && rejection.Kind != "timers") ||
		!strings.HasPrefix(rejection.Error, checkpointAdmissionPrefix(rejection.Kind)) {
		return false, fmt.Errorf("epoch rejected capture differs from its original rejection")
	}
	index := -1
	for i, pause := range record.PauseHistory {
		if pause.SnapshotID == id {
			if index != -1 {
				return false, fmt.Errorf("epoch rejected capture has duplicate retained generations")
			}
			index = i
		}
	}
	if index == -1 {
		return false, fmt.Errorf("epoch rejected capture lacks its retained Pause")
	}
	pause := record.PauseHistory[index]
	if pause.RejectedAt != rejection.RejectedAt || pause.Error != sandboxErrorSummary(rejection.Error) ||
		pause.SourcePID != rejection.SourcePID || pause.CompletedAt != "" || pause.Resume != nil {
		return false, fmt.Errorf("epoch rejected capture differs from its retained Pause")
	}
	pause.RejectedAt, pause.Error = "", ""
	original := record
	original.Phase, original.Delete, original.Pause = "pausing", nil, &pause
	original.PauseHistory = append([]PauseOperation(nil), record.PauseHistory[:index]...)
	var config captureGuardConfig
	if err := e.readCheckpointJSON(relative+"/guard/guard-intent.json", &config); err != nil {
		return false, err
	}
	data, err := sandboxRecordBytes(original)
	if err != nil || config.Version != 1 || config.Token != id || config.PID != rejection.SourcePID ||
		config.Root != e.config.Root || !config.StopSource || config.Criu != e.config.Criu ||
		config.CriuSHA256 != e.config.CriuSHA256 || !reflect.DeepEqual(config.Source, original) ||
		config.RecordSHA != sum(append(data, '\n')) {
		return false, fmt.Errorf("epoch rejected capture original source changed: %v", err)
	}
	if rejection.Kind == "descriptors" {
		var observation captureDescriptorAdmission
		err := e.readCheckpointJSON(relative+"/guard/descriptor-admission.json", &observation)
		if err != nil || observation.Version != 1 || observation.Token != config.Token ||
			observation.SourcePID != config.PID || observation.SourceStart != config.Start || observation.Passed {
			return false, fmt.Errorf("epoch descriptor rejection observation changed: %v", err)
		}
	}
	var intent checkpointIntent
	if err := e.readCheckpointJSON(relative+"/checkpoint-intent.json", &intent); err != nil {
		return false, err
	}
	if intent.Version != 1 || intent.ID != id || intent.SourceID != record.ID || !intent.StopSource ||
		intent.Owner != record.Owner || intent.RuntimeID != record.RuntimeID || intent.BootID != record.BootID ||
		intent.ConfigSHA256 != record.ConfigSHA256 || intent.RuntimeSHA256 != record.RuntimeSHA256 ||
		intent.CriuSHA256 != e.config.CriuSHA256 || intent.ManagerRoot != e.config.ManagerRoot || intent.RequestedAt == "" {
		return false, fmt.Errorf("epoch rejected capture has a different pinned intent")
	}
	var outcome struct {
		Config          captureGuardConfig `json:"config"`
		Reason          string             `json:"reason"`
		Status          string             `json:"status"`
		CaptureInFlight bool               `json:"capture_in_flight"`
		Passed          bool               `json:"passed"`
		Error           string             `json:"error,omitempty"`
	}
	err = e.readCheckpointJSON(relative+"/guard/guard-outcome.json", &outcome)
	if errors.Is(err, os.ErrNotExist) {
		err = e.readCheckpointJSON(relative+"/guard/guard-controller-outcome.json", &outcome)
	}
	if err != nil || !outcome.Passed || outcome.CaptureInFlight || outcome.Error != "" ||
		!reflect.DeepEqual(outcome.Config, config) ||
		(outcome.Status != "original_source_thawed" && outcome.Status != "source_already_running") {
		return false, fmt.Errorf("epoch rejected capture has no confirmed source outcome: %v", err)
	}
	for _, path := range []string{"guard/guard-capture-entered.json", "capture-result.json", "checkpoint.json",
		"images", "filesystem.json", "filesystem", "guard/criu-retained-arm.json"} {
		if _, err := e.root.Lstat(relative + "/" + path); !errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("epoch rejected capture has unexpected execution state: %s: %v", path, err)
		}
	}
	return true, nil
}
