//go:build linux

package lifecycle

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"

	"factorybox/internal/sysboxcapture"
)

// A stopped source with unpublished capture data is not a committed Pause.
// Explicit Delete may withdraw its execution only after every original actor
// has gone. This never repeats capture, restores processes or clears a freezer.
func (e *Engine) admitStoppedCaptureDelete(record Record, relative string) error {
	if record.BootID != e.boot {
		return fmt.Errorf("stopped capture Delete belongs to a different worker boot")
	}
	admission, config, err := e.stoppedCaptureDeleteProof(record, relative)
	if err != nil {
		return err
	}
	var armed struct {
		Token string `json:"token"`
		PID   int    `json:"helper_pid"`
	}
	var writer captureWorkerIntent
	if err := e.readCheckpointJSON(relative+"/guard/guard-armed.json", &armed); err != nil {
		return err
	}
	if err := e.readCheckpointJSON(relative+"/guard/capture-worker-intent.json", &writer); err != nil {
		return err
	}
	seen := map[int]bool{}
	for _, pid := range []int{config.PID, config.Controller, armed.PID, writer.PID} {
		if pid <= 1 || seen[pid] {
			return fmt.Errorf("stopped capture Delete has overlapping process identities")
		}
		seen[pid] = true
		// These older receipts do not pin every helper's birth. Even PID reuse
		// is a refusal, never authority over a different process.
		if _, err := os.Lstat("/proc/" + strconv.Itoa(pid)); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("stopped capture Delete has a live or unproved original actor: %v", err)
		}
	}
	if _, retained := admission.Evidence["guard/criu-retained-arm.json"]; retained {
		var arm retainedCaptureArm
		if err := e.readCheckpointJSON(relative+"/guard/criu-retained-arm.json", &arm); err != nil {
			return err
		}
		for _, pid := range []int{arm.HelperPID, arm.TracerPID} {
			if seen[pid] {
				return fmt.Errorf("stopped capture Delete has overlapping retained helper identities")
			}
			seen[pid] = true
		}
		for _, thread := range arm.Tree {
			seen[thread.PID] = true
		}
		for pid := range seen {
			if _, err := os.Lstat("/proc/" + strconv.Itoa(pid)); !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("stopped capture Delete has a live or unproved retained actor: %v", err)
			}
		}
	}
	if err := e.validateCgroup(record); err != nil {
		return err
	}
	group, err := captureGuardOpen(cgroupRoot + "/" + record.RuntimeID)
	if err != nil {
		return err
	}
	defer group.Close()
	identity, err := cgroupFDIdentity(int(group.Fd()))
	if err != nil || record.Cgroup == nil || identity != *record.Cgroup {
		return fmt.Errorf("stopped capture Delete changed its original cgroup: %v", err)
	}
	events, err := captureGuardEvents(group)
	populated, present := events["populated"]
	if err != nil || !present || populated != 0 {
		return fmt.Errorf("stopped capture Delete still has populated owned cgroups: %v", err)
	}
	threads, err := captureGuardThreads(config, captureGuardAnchors{group: group})
	if err != nil || len(threads) != 0 {
		return fmt.Errorf("stopped capture Delete has remaining threads or a foreign cgroup mount: %v", err)
	}
	if err := e.validateCgroup(record); err != nil {
		return err
	}
	var previous failedCaptureDeleteAdmission
	err = e.readCheckpointJSON(relative+"/capture-delete-admission.json", &previous)
	if err == nil {
		admission.AdmittedAt = previous.AdmittedAt
		if admission.AdmittedAt == "" || !reflect.DeepEqual(admission, previous) {
			return fmt.Errorf("stopped capture Delete admission changed")
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	admission.AdmittedAt = now()
	return e.publishCheckpoint(relative, "capture-delete-admission.json", admission)
}

// Validate the immutable disposal evidence separately from current execution.
// After confirmed Delete its cgroup and PIDs no longer confer any authority.
func (e *Engine) stoppedCaptureDeleteProof(record Record, relative string) (failedCaptureDeleteAdmission, captureGuardConfig, error) {
	admission := failedCaptureDeleteAdmission{Version: 2, SourceID: record.ID,
		Owner: record.Owner, RuntimeID: record.RuntimeID, SnapshotID: record.Pause.SnapshotID,
		Evidence: map[string]string{}}
	var config captureGuardConfig
	root, err := e.root.Open(".")
	if err != nil {
		return admission, config, err
	}
	defer root.Close()
	read := func(name string, value any) error {
		data, err := captureCompletionBytes(root, relative+"/"+name, checkpointManifestLimit)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, value); err != nil {
			return err
		}
		admission.Evidence[name] = sum(data)
		return nil
	}
	fail := func() (failedCaptureDeleteAdmission, captureGuardConfig, error) {
		return admission, config, fmt.Errorf("stopped capture Delete differs from its original capture/stop evidence")
	}
	if err := read("guard/guard-intent.json", &config); err != nil {
		return admission, config, err
	}
	encoded, err := sandboxRecordBytes(record)
	if err != nil || config.Version != 1 || !config.StopSource || config.TimerRecoveryOf != "" ||
		config.Root != e.config.Root || config.Criu != e.config.Criu || config.CriuSHA256 != e.config.CriuSHA256 ||
		!digest.MatchString(config.CriuSHA256) || config.Token != record.Pause.SnapshotID ||
		config.PID != record.Pause.SourcePID || config.PID <= 1 || config.Start == "" ||
		config.Controller <= 1 || config.Evidence != filepath.Join(e.config.Root, relative, "guard") ||
		config.RecordSHA != sum(append(encoded, '\n')) || !reflect.DeepEqual(config.Source, record) {
		return fail()
	}
	admission.RecordSHA = config.RecordSHA
	var intent checkpointIntent
	if err := read("checkpoint-intent.json", &intent); err != nil {
		return admission, config, err
	}
	if intent.Version != 1 || intent.ID != config.Token || !intent.StopSource ||
		intent.SourceID != record.ID || intent.RuntimeID != record.RuntimeID || intent.Owner != record.Owner ||
		intent.BootID != record.BootID || intent.ConfigSHA256 != record.ConfigSHA256 ||
		intent.RuntimeSHA256 != record.RuntimeSHA256 || intent.CriuSHA256 != config.CriuSHA256 ||
		intent.ManagerRoot != e.config.ManagerRoot || intent.RequestedAt == "" {
		return fail()
	}
	for _, name := range []string{"checkpoint.json", "capture-result.json", "checkpoint-reconciled.json",
		"guard/criu-retained-request.json", "guard/criu-retained-result.json",
		"guard/capture-process-restored.json", "guard/guard-capture-aborted.json"} {
		if _, err := e.root.Lstat(relative + "/" + name); !errors.Is(err, os.ErrNotExist) {
			return fail()
		}
	}
	var worker captureWorkerIntent
	var complete captureProcessCompletion
	var armed struct {
		Token string `json:"token"`
		PID   int    `json:"helper_pid"`
	}
	var stop struct {
		Token       string               `json:"token"`
		SourcePID   int                  `json:"source_pid"`
		SourceStart string               `json:"source_start"`
		Method      string               `json:"stop_method"`
		Capture     sysboxcapture.Result `json:"capture"`
	}
	var outcome struct {
		Token   string               `json:"token"`
		Passed  bool                 `json:"passed"`
		Error   string               `json:"error,omitempty"`
		Capture sysboxcapture.Result `json:"capture"`
	}
	var guard struct {
		Config          captureGuardConfig `json:"config"`
		Passed          bool               `json:"passed"`
		Error           string             `json:"error,omitempty"`
		Reason          string             `json:"reason"`
		Status          string             `json:"status"`
		CaptureInFlight bool               `json:"capture_in_flight"`
	}
	var filesystem sysboxcapture.FilesystemSnapshot
	for name, value := range map[string]any{
		"guard/guard-armed.json": &armed, "guard/capture-worker-intent.json": &worker,
		"guard/capture-process-complete.json": &complete, "guard/capture-stop-intent.json": &stop,
		"guard/capture-worker-outcome.json": &outcome, "guard/guard-outcome.json": &guard,
		"filesystem.json": &filesystem,
	} {
		if err := read(name, value); err != nil {
			return admission, config, err
		}
	}
	captured := outcome.Capture
	if worker.Token != config.Token || worker.SourcePID != config.PID || worker.PID <= 1 || worker.Start == "" ||
		worker.CriuSHA256 != config.CriuSHA256 || worker.Scope != "ordinary complete retained-source capture" ||
		armed.Token != config.Token || armed.PID <= 1 || complete.Version != 1 || complete.Worker != worker ||
		complete.SourceStart != config.Start || !digest.MatchString(complete.SourceStateSHA) ||
		stop.Token != config.Token || stop.SourcePID != config.PID || stop.SourceStart != config.Start ||
		stop.Method != "frozen-owned-process-pidfds" || !reflect.DeepEqual(stop.Capture, captured) ||
		outcome.Token != config.Token || outcome.Passed != (outcome.Error == "") ||
		captured.SourcePID != config.PID || !captured.CompleteWriter || !captured.LeaveRunning ||
		captured.Images != filepath.Join(e.config.Root, relative, "images") || captured.SnapshotEntries == 0 ||
		captured.SourceStateSHA != complete.SourceStateSHA || captured.Filesystem == nil ||
		!reflect.DeepEqual(*captured.Filesystem, filesystem) || !reflect.DeepEqual(guard.Config, config) {
		return fail()
	}
	source, err := captureCompletionBytes(root, relative+"/source-state.json", checkpointManifestLimit)
	if err != nil || sum(source) != complete.SourceStateSHA {
		return fail()
	}
	admission.Evidence["source-state.json"] = sum(source)
	if _, err := e.root.Lstat(relative + "/guard/criu-retained-arm.json"); err == nil {
		var arm retainedCaptureArm
		if err := read("guard/criu-retained-arm.json", &arm); err != nil {
			return admission, config, err
		}
		if arm.Version != 3 || arm.Mode != "process_tree_retained" ||
			arm.SourcePID != config.PID || arm.SourceStart != config.Start ||
			arm.WorkerPID != worker.PID || arm.WorkerStart != worker.Start ||
			arm.ControllerPID != config.Controller || arm.ControllerStart == "" ||
			arm.GuardPID != armed.PID || arm.GuardStart == "" ||
			arm.HelperPID <= 1 || arm.HelperStart == "" || arm.TracerPID <= 1 || arm.TracerStart == "" ||
			arm.ActivePID <= 1 || arm.ActiveStart == "" || arm.ActiveVirtualPID <= 0 ||
			!arm.ExitHoldArmed || !digest.MatchString(arm.Nonce) || len(arm.Tree) == 0 || len(arm.Tree) > 512 {
			return fail()
		}
		seen := map[int]bool{}
		for _, pid := range []int{config.PID, worker.PID, config.Controller, armed.PID, arm.HelperPID, arm.TracerPID} {
			if seen[pid] {
				return fail()
			}
			seen[pid] = true
		}
		tree := map[int]retainedCaptureThread{}
		previous := 0
		for _, thread := range arm.Tree {
			if thread.PID <= previous || thread.Process <= 1 || thread.Start == "" ||
				seen[thread.PID] && thread.PID != config.PID {
				return fail()
			}
			tree[thread.PID], previous = thread, thread.PID
		}
		if tree[config.PID].Start != config.Start || tree[config.PID].Process != config.PID ||
			tree[arm.ActivePID].Start != arm.ActiveStart || tree[arm.ActivePID].Process != arm.ActivePID {
			return fail()
		}
		for _, thread := range arm.Tree {
			if tree[thread.Process].Process != thread.Process {
				return fail()
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return admission, config, err
	}
	return admission, config, nil
}

func (e *Engine) deletedStoppedCapture(record Record, relative string, admission failedCaptureDeleteAdmission) (bool, error) {
	original := record
	original.Phase, original.Delete = "pausing", nil
	expected, _, err := e.stoppedCaptureDeleteProof(original, relative)
	if err != nil {
		return false, err
	}
	expected.AdmittedAt = admission.AdmittedAt
	if admission.AdmittedAt == "" || !reflect.DeepEqual(expected, admission) {
		return false, fmt.Errorf("deleted stopped capture changed its original Delete admission")
	}
	return true, nil
}
