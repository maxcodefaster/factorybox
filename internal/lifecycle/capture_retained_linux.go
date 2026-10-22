//go:build linux

package lifecycle

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

type retainedCaptureArm struct {
	Version          int                     `json:"version"`
	Mode             string                  `json:"mode"`
	SourcePID        int                     `json:"source_pid"`
	SourceStart      string                  `json:"source_start"`
	HelperPID        int                     `json:"helper_pid"`
	HelperStart      string                  `json:"helper_start"`
	TracerPID        int                     `json:"tracer_pid"`
	TracerStart      string                  `json:"tracer_start"`
	WorkerPID        int                     `json:"worker_pid"`
	WorkerStart      string                  `json:"worker_start"`
	ControllerPID    int                     `json:"controller_pid"`
	ControllerStart  string                  `json:"controller_start"`
	GuardPID         int                     `json:"guard_pid"`
	GuardStart       string                  `json:"guard_start"`
	Nonce            string                  `json:"nonce"`
	ActivePID        int                     `json:"active_pid"`
	ActiveStart      string                  `json:"active_start"`
	ActiveVirtualPID int                     `json:"active_vpid"`
	ExitHoldArmed    bool                    `json:"exit_hold_armed"`
	Tree             []retainedCaptureThread `json:"tree"`
}

type retainedCaptureThread struct {
	PID     int    `json:"tid"`
	Process int    `json:"tgid"`
	Start   string `json:"start"`
}

type retainedCaptureRequest struct {
	Version       int    `json:"version"`
	Token         string `json:"token"`
	ControllerPID int    `json:"controller_pid"`
	GuardPID      int    `json:"guard_pid"`
	Nonce         string `json:"nonce"`
}

type retainedCaptureResult struct {
	Version      int    `json:"version"`
	Passed       bool   `json:"passed"`
	SourcePID    int    `json:"source_pid"`
	SourceStart  string `json:"source_start"`
	HelperPID    int    `json:"helper_pid"`
	Threads      int    `json:"threads"`
	Registers    bool   `json:"registers_match"`
	SignalMask   bool   `json:"signal_mask_match"`
	Maps         int    `json:"injected_maps_after"`
	Held         bool   `json:"source_held"`
	Authorized   bool   `json:"guard_authorized"`
	Nonce        string `json:"nonce"`
	ActivePID    int    `json:"active_pid"`
	Processes    int    `json:"processes"`
	ExitStopHeld bool   `json:"exit_stop_held"`
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

// This handle is opened before the guard acknowledges its original parent.
// A reused numeric PID can neither replace it nor authorize another caller.
type captureControllerIdentity struct {
	pid, pidfd int
	start      string
}

func capturePinController(pid int) (captureControllerIdentity, error) {
	identity := captureControllerIdentity{pid: pid, pidfd: -1}
	if pid <= 1 || pid != os.Getppid() {
		return identity, fmt.Errorf("guard controller is not its original parent")
	}
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		return identity, err
	}
	start, err := processBirth(pid)
	if err != nil {
		unix.Close(fd)
		return identity, err
	}
	identity.pidfd, identity.start = fd, start
	return identity, nil
}

func (identity captureControllerIdentity) exited() (bool, error) {
	if identity.pidfd < 0 || identity.start == "" {
		return false, fmt.Errorf("guard lacks its original controller identity")
	}
	for {
		poll := []unix.PollFd{{Fd: int32(identity.pidfd), Events: unix.POLLIN}}
		n, err := unix.Poll(poll, 0)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return false, err
		}
		if n == 0 {
			return false, nil
		}
		if n != 1 || poll[0].Revents&unix.POLLIN == 0 {
			return false, fmt.Errorf("original controller pidfd changed its event state")
		}
		return true, nil
	}
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
	if arm.Version != 3 || arm.Mode != "process_tree_retained" ||
		arm.SourcePID != config.PID || arm.SourceStart != config.Start ||
		arm.WorkerPID != worker.PID || arm.WorkerStart != worker.Start ||
		arm.ControllerPID != config.Controller || arm.ControllerStart == "" ||
		arm.HelperPID <= 1 || arm.HelperStart == "" || arm.TracerPID <= 1 || arm.TracerStart == "" ||
		arm.GuardPID <= 1 || arm.GuardStart == "" || !digest.MatchString(arm.Nonce) ||
		worker.Token != config.Token || worker.SourcePID != config.PID ||
		worker.CriuSHA256 != config.CriuSHA256 || worker.Scope != "ordinary complete retained-source capture" {
		return arm, worker, fmt.Errorf("retained context differs from the original capture transaction")
	}
	if len(arm.Tree) == 0 || len(arm.Tree) > 512 {
		return arm, worker, fmt.Errorf("retained capture tree exceeds its thread bound")
	}
	seen := map[int]bool{}
	for _, pid := range []int{arm.SourcePID, arm.WorkerPID, arm.ControllerPID, arm.HelperPID, arm.TracerPID, arm.GuardPID} {
		if pid <= 1 || seen[pid] {
			return arm, worker, fmt.Errorf("retained capture process identities overlap")
		}
		seen[pid] = true
	}
	tree := map[int]retainedCaptureThread{}
	previous := 0
	for _, thread := range arm.Tree {
		if thread.PID <= previous || thread.Process <= 1 || thread.Start == "" ||
			(seen[thread.PID] && thread.PID != arm.SourcePID) {
			return arm, worker, fmt.Errorf("retained capture tree has overlapping or unordered identities")
		}
		tree[thread.PID], previous = thread, thread.PID
	}
	if tree[arm.SourcePID].Start != config.Start || tree[arm.SourcePID].Process != arm.SourcePID ||
		tree[arm.ActivePID].Start != arm.ActiveStart || arm.ActiveStart == "" || arm.ActiveVirtualPID <= 0 ||
		tree[arm.ActivePID].Process != arm.ActivePID {
		return arm, worker, fmt.Errorf("retained capture lost its original root or active process")
	}
	for _, thread := range arm.Tree {
		if tree[thread.Process].Process != thread.Process {
			return arm, worker, fmt.Errorf("retained capture tree lacks an original process leader")
		}
	}
	for pid, start := range map[int]string{worker.PID: worker.Start, arm.TracerPID: arm.TracerStart} {
		if birth, err := processBirth(pid); err == nil && birth == start {
			return arm, worker, fmt.Errorf("retained context still has its original active writer")
		}
	}
	return arm, worker, nil
}

func captureRetainedTreeCurrent(config captureGuardConfig, anchors captureGuardAnchors, arm retainedCaptureArm, cured bool) error {
	actual, err := captureGuardThreads(config, anchors)
	if err != nil || len(actual) != len(arm.Tree) {
		return fmt.Errorf("retained capture changed its original complete thread set: expected=%d actual=%d: %v",
			len(arm.Tree), len(actual), err)
	}
	for _, thread := range arm.Tree {
		if actual[thread.PID] != thread.Start {
			return fmt.Errorf("retained capture replaced original thread %d in process %d: expected=%q actual=%q",
				thread.PID, thread.Process, thread.Start, actual[thread.PID])
		}
		status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", thread.PID))
		if err != nil {
			return err
		}
		matched := false
		for _, line := range strings.Split(string(status), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[0] == "Tgid:" {
				matched = fields[1] == fmt.Sprint(thread.Process)
			}
		}
		if !matched {
			return fmt.Errorf("retained capture changed an original thread's process")
		}
		if cured && thread.PID == thread.Process {
			file, err := os.Open(fmt.Sprintf("/proc/%d/maps", thread.Process))
			if err != nil {
				return err
			}
			data, readErr := io.ReadAll(io.LimitReader(file, (8<<20)+1))
			closeErr := file.Close()
			if readErr != nil || closeErr != nil || len(data) > 8<<20 || bytes.Contains(data, []byte("CRIUMFD")) {
				return fmt.Errorf("retained process cure left injected or unreadable mappings: %v", errors.Join(readErr, closeErr))
			}
		}
	}
	return nil
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

func capturePinRetainedHelper(config captureGuardConfig, arm retainedCaptureArm) (_ int, resultErr error) {
	binary, err := openRuntime(config.Criu, config.CriuSHA256)
	if err != nil {
		return -1, err
	}
	defer binary.Close()
	fd, err := unix.PidfdOpen(arm.HelperPID, 0)
	if err != nil {
		return -1, err
	}
	defer func() {
		if resultErr != nil {
			unix.Close(fd)
		}
	}()
	expected, statErr := binary.Stat()
	actual, actualErr := os.Stat(fmt.Sprintf("/proc/%d/exe", arm.HelperPID))
	birth, birthErr := processBirth(arm.HelperPID)
	if statErr != nil || actualErr != nil || birthErr != nil ||
		birth != arm.HelperStart || !os.SameFile(expected, actual) {
		return -1, fmt.Errorf("retained helper executable or birth differs from pinned CRIU")
	}
	membership, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", arm.HelperPID))
	if err != nil || strings.Contains(string(membership), "/"+config.Source.RuntimeID) {
		return -1, fmt.Errorf("retained helper is inside the captured source cgroup: %v", err)
	}
	return fd, nil
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
	processes := 0
	for _, thread := range arm.Tree {
		if thread.PID == thread.Process {
			processes++
		}
	}
	if result.Version != 3 || !result.Passed || result.SourcePID != config.PID ||
		result.SourceStart != config.Start || result.HelperPID != arm.HelperPID ||
		result.Threads != len(arm.Tree) || result.Processes != processes || result.ActivePID != arm.ActivePID ||
		!result.Registers || !result.SignalMask ||
		result.Maps != 0 || !result.Held || !result.Authorized || result.Nonce != arm.Nonce ||
		result.ExitStopHeld != arm.ExitHoldArmed {
		return fmt.Errorf("retained context did not prove its authorized complete process cure")
	}
	return nil
}

// Only the original independent guard can grant temporary execution to the
// retained CRIU helper. The source remains held when this method returns.
func captureGuardCureRetained(config captureGuardConfig, anchors captureGuardAnchors,
	controller captureControllerIdentity) (resultErr error) {
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
	if controller.pid != config.Controller || controller.start != arm.ControllerStart {
		return fmt.Errorf("retained context differs from the original pinned controller")
	}
	// Use the pinned kernel identity, not a fresh lookup of a numeric PID
	// which may disappear or be reused while the guard is curing.
	if _, err := controller.exited(); err != nil {
		return err
	}
	if err := captureRetainedTreeCurrent(config, anchors, arm, false); err != nil {
		return err
	}
	if err := captureRetainedNetwork(config, anchors); err != nil {
		return err
	}
	if _, err := captureRetainedState(config, anchors); err != nil {
		return err
	}
	fd, err := capturePinRetainedHelper(config, arm)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
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
	return captureRetainedPublishRestoration(config, anchors, arm, worker)
}

func captureRetainedNetwork(config captureGuardConfig, anchors captureGuardAnchors) error {
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
	return nil
}

func captureRetainedPublishRestoration(config captureGuardConfig, anchors captureGuardAnchors,
	arm retainedCaptureArm, worker captureWorkerIntent) error {
	if err := captureRetainedResult(config, anchors, arm); err != nil {
		return err
	}
	if err := captureGuardCaptureReturned(config, anchors); err != nil {
		return err
	}
	if err := captureRetainedTreeCurrent(config, anchors, arm, true); err != nil {
		return err
	}
	if err := captureRetainedNetwork(config, anchors); err != nil {
		return err
	}
	saved, err := captureRetainedState(config, anchors)
	if err != nil {
		return err
	}
	hashes, err := captureRetainedHashes(anchors)
	if err != nil {
		return err
	}
	expected := captureProcessRestoration{
		Version: 1, Worker: worker, SourceStart: config.Start, SourceStateSHA: sum(saved),
		ArmSHA: hashes[0], RequestSHA: hashes[1], ResultSHA: hashes[2], NetworkSHA: hashes[3],
	}
	var stored captureProcessRestoration
	if err := captureCompletionRead(anchors.evidence, "capture-process-restored.json", &stored); err == nil {
		if stored != expected {
			return fmt.Errorf("retained restoration receipt differs from its original proof")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return captureGuardWrite(anchors.evidence, "capture-process-restored.json", expected)
}

// The original caller may finish a cure already authorized by its original
// guard. It never writes a new grant or releases a live retained writer.
func (guard *captureGuard) completeDelegatedCure(cause error) (*checkpointCaptureError, error) {
	if !guard.exited || !guard.unsafe || cause == nil {
		return nil, fmt.Errorf("delegated cure is outside the original failed guard transaction")
	}
	arm, worker, err := captureRetainedRecords(guard.config, guard.anchor)
	if err != nil {
		return nil, err
	}
	birth, err := processBirth(os.Getpid())
	if err != nil || os.Getpid() != arm.ControllerPID || arm.ControllerPID != guard.config.Controller ||
		birth != arm.ControllerStart || guard.cmd.Process.Pid != arm.GuardPID {
		return nil, fmt.Errorf("delegated cure lost its original controller/guard identities")
	}
	var grant retainedCaptureRequest
	if err := captureCompletionRead(guard.anchor.evidence, "criu-retained-request.json", &grant); err != nil {
		return nil, err
	}
	if grant.Version != 1 || grant.Token != guard.config.Token || grant.Nonce != arm.Nonce ||
		grant.ControllerPID != arm.ControllerPID || grant.GuardPID != arm.GuardPID {
		return nil, fmt.Errorf("delegated cure lacks its original guard's exact grant")
	}
	helper := -1
	if current, err := processBirth(arm.HelperPID); err == nil && current == arm.HelperStart {
		helper, err = capturePinRetainedHelper(guard.config, arm)
		if err != nil {
			if current, birthErr := processBirth(arm.HelperPID); birthErr == nil && current == arm.HelperStart {
				return nil, err
			}
		}
	}
	if helper >= 0 {
		defer unix.Close(helper)
	}
	deadline := time.Now().Add(12 * time.Second)
	for {
		exited := helper < 0
		if helper >= 0 {
			poll := []unix.PollFd{{Fd: int32(helper), Events: unix.POLLIN}}
			n, err := unix.Poll(poll, 0)
			if errors.Is(err, unix.EINTR) {
				continue
			}
			if err != nil || (n != 0 && poll[0].Revents&unix.POLLIN == 0) {
				return nil, fmt.Errorf("delegated cure helper has no valid exit state: %v", err)
			}
			exited = n == 1
		}
		err = captureRetainedResult(guard.config, guard.anchor, arm)
		if err == nil && exited {
			break
		}
		if err != nil && (!errors.Is(err, os.ErrNotExist) || exited) {
			return nil, err
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("delegated original cure did not finish within 12s")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := captureRetainedPublishRestoration(guard.config, guard.anchor, arm, worker); err != nil {
		return nil, err
	}
	if err := captureGuardTimersSafe(guard.config, guard.anchor); err != nil {
		return nil, err
	}
	intent, err := captureGuardAbortIntent(guard.config, guard.anchor)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		intent = captureAbortIntent{Version: 1, Token: guard.config.Token, FailedAt: now(), Error: cause.Error()}
		if err := captureGuardWrite(guard.anchor.evidence, "capture-abort-intent.json", intent); err != nil {
			return nil, err
		}
	}
	var acknowledgement struct {
		Token    string `json:"token"`
		InFlight bool   `json:"capture_in_flight"`
	}
	if err := captureCompletionRead(guard.anchor.evidence, "guard-capture-aborted.json", &acknowledgement); err == nil {
		if acknowledgement.Token != guard.config.Token || acknowledgement.InFlight {
			return nil, fmt.Errorf("delegated abort acknowledgement differs from its original transaction")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	} else if err := captureGuardWrite(guard.anchor.evidence, "guard-capture-aborted.json",
		map[string]any{"token": guard.config.Token, "capture_in_flight": false}); err != nil {
		return nil, err
	}
	return &checkpointCaptureError{cause: errors.New(intent.Error), failedAt: intent.FailedAt}, nil
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
