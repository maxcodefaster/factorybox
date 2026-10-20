//go:build linux

package lifecycle

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

type retainedCaptureArm struct {
	Version         int    `json:"version"`
	Mode            string `json:"mode"`
	SourcePID       int    `json:"source_pid"`
	SourceStart     string `json:"source_start"`
	HelperPID       int    `json:"helper_pid"`
	HelperStart     string `json:"helper_start"`
	TracerPID       int    `json:"tracer_pid"`
	TracerStart     string `json:"tracer_start"`
	WorkerPID       int    `json:"worker_pid"`
	WorkerStart     string `json:"worker_start"`
	ControllerPID   int    `json:"controller_pid"`
	ControllerStart string `json:"controller_start"`
	GuardPID        int    `json:"guard_pid"`
	GuardStart      string `json:"guard_start"`
	Nonce           string `json:"nonce"`
}

type retainedCaptureRequest struct {
	Version       int    `json:"version"`
	Token         string `json:"token"`
	ControllerPID int    `json:"controller_pid"`
	GuardPID      int    `json:"guard_pid"`
	Nonce         string `json:"nonce"`
}

type retainedCaptureResult struct {
	Version     int    `json:"version"`
	Passed      bool   `json:"passed"`
	SourcePID   int    `json:"source_pid"`
	SourceStart string `json:"source_start"`
	HelperPID   int    `json:"helper_pid"`
	Threads     int    `json:"threads"`
	Registers   bool   `json:"registers_match"`
	SignalMask  bool   `json:"signal_mask_match"`
	Maps        int    `json:"injected_maps_after"`
	Held        bool   `json:"source_held"`
	Authorized  bool   `json:"guard_authorized"`
	Nonce       string `json:"nonce"`
}

type captureProcessRestoration struct {
	Version        int                 `json:"version"`
	Worker         captureWorkerIntent `json:"worker"`
	SourceStart    string              `json:"source_start"`
	SourceStateSHA string              `json:"source_state_sha256"`
	ArmSHA         string              `json:"arm_sha256"`
	RequestSHA     string              `json:"request_sha256"`
	ResultSHA      string              `json:"result_sha256"`
	NetworkSHA     string              `json:"network_sha256"`
}

func captureRetainedRecords(config captureGuardConfig, anchors captureGuardAnchors) (retainedCaptureArm, captureWorkerIntent, error) {
	var arm retainedCaptureArm
	var worker captureWorkerIntent
	if err := captureCompletionRead(anchors.evidence, "criu-retained-arm.json", &arm); err != nil {
		return arm, worker, err
	}
	if err := captureCompletionRead(anchors.evidence, "capture-worker-intent.json", &worker); err != nil {
		return arm, worker, err
	}
	if arm.Version != 2 || arm.Mode != "single_process_retained" ||
		arm.SourcePID != config.PID || arm.SourceStart != config.Start ||
		arm.WorkerPID != worker.PID || arm.WorkerStart != worker.Start ||
		arm.ControllerPID != config.Controller || arm.ControllerStart == "" ||
		arm.HelperPID <= 1 || arm.HelperStart == "" || arm.TracerPID <= 1 || arm.TracerStart == "" ||
		arm.GuardPID <= 1 || arm.GuardStart == "" || !digest.MatchString(arm.Nonce) ||
		worker.Token != config.Token || worker.SourcePID != config.PID ||
		worker.CriuSHA256 != config.CriuSHA256 || worker.Scope != "ordinary complete retained-source capture" {
		return arm, worker, fmt.Errorf("retained context differs from the original capture transaction")
	}
	seen := map[int]bool{}
	for _, pid := range []int{arm.SourcePID, arm.WorkerPID, arm.ControllerPID, arm.HelperPID, arm.TracerPID, arm.GuardPID} {
		if pid <= 1 || seen[pid] {
			return arm, worker, fmt.Errorf("retained capture process identities overlap")
		}
		seen[pid] = true
	}
	for pid, start := range map[int]string{worker.PID: worker.Start, arm.TracerPID: arm.TracerStart} {
		if birth, err := processBirth(pid); err == nil && birth == start {
			return arm, worker, fmt.Errorf("retained context still has its original active writer")
		}
	}
	return arm, worker, nil
}

func captureRetainedState(config captureGuardConfig, anchors captureGuardAnchors) ([]byte, error) {
	object, err := objectPath(config.Source.ID)
	if err != nil {
		return nil, err
	}
	saved, err := captureCompletionBytes(anchors.root,
		filepath.Join(object, "captures", config.Token, "source-state.json"), checkpointManifestLimit)
	if err != nil {
		return nil, err
	}
	current, err := captureCompletionBytes(anchors.root,
		filepath.Join("runtime", config.Source.RuntimeID, "state.json"), checkpointManifestLimit)
	if err != nil || !bytes.Equal(saved, current) {
		return nil, fmt.Errorf("retained capture changed its original raw Sysbox state: %v", err)
	}
	return saved, nil
}

func captureRetainedHashes(anchors captureGuardAnchors) ([4]string, error) {
	var hashes [4]string
	for index, name := range []string{"criu-retained-arm.json", "criu-retained-request.json", "criu-retained-result.json", "capture-network-rollback.json"} {
		data, err := captureCompletionBytes(anchors.evidence, name, 64<<10)
		if err != nil {
			return hashes, err
		}
		hashes[index] = sum(data)
	}
	return hashes, nil
}

func captureRetainedResult(config captureGuardConfig, anchors captureGuardAnchors, arm retainedCaptureArm) error {
	var request retainedCaptureRequest
	if err := captureCompletionRead(anchors.evidence, "criu-retained-request.json", &request); err != nil {
		return err
	}
	if request.Version != 1 || request.Token != config.Token ||
		request.ControllerPID != config.Controller || request.GuardPID != arm.GuardPID || request.Nonce != arm.Nonce {
		return fmt.Errorf("retained context grant differs from the original guard")
	}
	var result retainedCaptureResult
	if err := captureCompletionRead(anchors.evidence, "criu-retained-result.json", &result); err != nil {
		return err
	}
	if result.Version != 2 || !result.Passed || result.SourcePID != config.PID ||
		result.SourceStart != config.Start || result.HelperPID != arm.HelperPID ||
		result.Threads <= 0 || result.Threads > 64 || !result.Registers || !result.SignalMask ||
		result.Maps != 0 || !result.Held || !result.Authorized || result.Nonce != arm.Nonce {
		return fmt.Errorf("retained context did not prove its authorized complete process cure")
	}
	return nil
}

// Only the original independent guard can grant temporary execution to the
// retained CRIU helper. The source remains held when this method returns.
func captureGuardCureRetained(config captureGuardConfig, anchors captureGuardAnchors) (resultErr error) {
	if err := captureGuardCaptureReturned(config, anchors); err != nil {
		return err
	}
	arm, worker, err := captureRetainedRecords(config, anchors)
	if err != nil {
		return err
	}
	guardBirth, err := processBirth(os.Getpid())
	if err != nil || arm.GuardPID != os.Getpid() || arm.GuardStart != guardBirth {
		return fmt.Errorf("retained context is not owned by this original guard")
	}
	controllerBirth, err := processBirth(config.Controller)
	if err != nil || controllerBirth != arm.ControllerStart {
		return fmt.Errorf("retained context lost its original active controller")
	}
	var network struct {
		Token        string            `json:"token"`
		SourcePID    int               `json:"source_pid"`
		SourceStart  string            `json:"source_start"`
		SourceFrozen bool              `json:"source_frozen"`
		EventsBefore map[string]uint64 `json:"events_before"`
		Passed       bool              `json:"passed"`
	}
	if err := captureCompletionRead(anchors.evidence, "capture-network-rollback.json", &network); err != nil {
		return err
	}
	if !network.Passed || !network.SourceFrozen || network.Token != config.Token ||
		network.SourcePID != config.PID || network.SourceStart != config.Start {
		return fmt.Errorf("retained context lacks its original checked network rollback")
	}
	saved, err := captureRetainedState(config, anchors)
	if err != nil {
		return err
	}
	binary, err := openRuntime(config.Criu, config.CriuSHA256)
	if err != nil {
		return err
	}
	defer binary.Close()
	fd, err := unix.PidfdOpen(arm.HelperPID, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	expected, statErr := binary.Stat()
	actual, actualErr := os.Stat(fmt.Sprintf("/proc/%d/exe", arm.HelperPID))
	birth, birthErr := processBirth(arm.HelperPID)
	if statErr != nil || actualErr != nil || birthErr != nil ||
		birth != arm.HelperStart || !os.SameFile(expected, actual) {
		return fmt.Errorf("retained helper executable or birth differs from pinned CRIU")
	}
	membership, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", arm.HelperPID))
	if err != nil || strings.Contains(string(membership), "/"+config.Source.RuntimeID) {
		return fmt.Errorf("retained helper is inside the captured source cgroup: %v", err)
	}
	defer func() {
		if resultErr != nil {
			// Stop the exact retained writer before reasserting a source
			// hold; no later helper write may undo that hold.
			_ = unix.PidfdSendSignal(fd, unix.SIGKILL, nil, 0)
			poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
			_, _ = unix.Poll(poll, 1000)
			_, holdErr := captureGuardHold(config, anchors)
			resultErr = errors.Join(resultErr, holdErr)
		}
	}()
	grant := retainedCaptureRequest{Version: 1, Token: config.Token,
		ControllerPID: config.Controller, GuardPID: os.Getpid(), Nonce: arm.Nonce}
	if err := captureGuardWrite(anchors.evidence, "criu-retained-request.json", grant); err != nil {
		return err
	}
	deadline := time.Now().Add(12 * time.Second)
	for {
		err = captureRetainedResult(config, anchors, arm)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrNotExist) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := captureGuardCaptureReturned(config, anchors); err != nil {
		return err
	}
	if _, err := captureRetainedState(config, anchors); err != nil {
		return err
	}
	hashes, err := captureRetainedHashes(anchors)
	if err != nil {
		return err
	}
	return captureGuardWrite(anchors.evidence, "capture-process-restored.json", captureProcessRestoration{
		Version: 1, Worker: worker, SourceStart: config.Start, SourceStateSHA: sum(saved),
		ArmSHA: hashes[0], RequestSHA: hashes[1], ResultSHA: hashes[2], NetworkSHA: hashes[3],
	})
}

func captureGuardRestoredReceipt(config captureGuardConfig, anchors captureGuardAnchors) (captureProcessCompletion, error) {
	var restored captureProcessRestoration
	if err := captureCompletionRead(anchors.evidence, "capture-process-restored.json", &restored); err != nil {
		return captureProcessCompletion{}, err
	}
	arm, worker, err := captureRetainedRecords(config, anchors)
	if err != nil {
		return captureProcessCompletion{}, err
	}
	if err := captureRetainedResult(config, anchors, arm); err != nil {
		return captureProcessCompletion{}, err
	}
	hashes, err := captureRetainedHashes(anchors)
	if err != nil || restored.Version != 1 || restored.Worker != worker ||
		restored.SourceStart != config.Start || restored.ArmSHA != hashes[0] ||
		restored.RequestSHA != hashes[1] || restored.ResultSHA != hashes[2] || restored.NetworkSHA != hashes[3] {
		return captureProcessCompletion{}, fmt.Errorf("retained process receipt was changed: %v", err)
	}
	saved, err := captureRetainedState(config, anchors)
	if err != nil || sum(saved) != restored.SourceStateSHA {
		return captureProcessCompletion{}, fmt.Errorf("retained source state changed after recovery: %v", err)
	}
	return captureProcessCompletion{Version: 1, Worker: worker,
		SourceStart: restored.SourceStart, SourceStateSHA: restored.SourceStateSHA}, nil
}
