package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

const controllerLossChild = "RUNTIME_FORK_CONTROLLER_LOSS_CHILD"

type controllerLossRequest struct {
	Version  int              `json:"version"`
	Source   descendantLayout `json:"source"`
	PID      int              `json:"original_pid"`
	Start    string           `json:"original_start"`
	Evidence string           `json:"evidence"`
	Mode     string           `json:"mode"`
}

type controllerLossReady struct {
	Version    int                   `json:"version"`
	Config     descendantGuardConfig `json:"config"`
	Controller captureProcessStamp   `json:"controller"`
	Helper     captureProcessStamp   `json:"helper"`
	Mode       string                `json:"mode"`
	JournalSHA string                `json:"capture_journal_sha256,omitempty"`
}

func init() {
	if os.Getenv(controllerLossChild) == "1" && os.Getenv(descendantGuardChild) != "1" {
		if len(os.Args) != 1 {
			os.Exit(2)
		}
		if err := controllerLossServe(); err != nil {
			fmt.Fprintln(os.Stderr, "owned controller-loss fixture:", err)
			os.Exit(1)
		}
		os.Exit(0)
	}
}

func controllerLossRead(fd int, value any) error {
	data, err := descendantGuardRead(fd, 5000)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(value)
}

func controllerLossServe() (resultErr error) {
	socket := os.NewFile(3, "owned worker/controller socket")
	defer socket.Close()
	var request controllerLossRequest
	if err := controllerLossRead(3, &request); err != nil {
		return err
	}
	owned, err := descendantOwnedLayout(request.Source.ID)
	if err != nil || owned != request.Source || request.Version != 1 ||
		request.Evidence != "/experiment/descendant/generation-1/controller-loss" ||
		(request.Mode != "before-capture" && request.Mode != "armed-capture") {
		return fmt.Errorf("controller-loss request is outside its owned first source")
	}
	original, err := captureProcess(request.PID)
	if err != nil || original.Start != request.Start || original.NSPID != 1 {
		return fmt.Errorf("controller-loss source is not its original PID-1: %v", err)
	}
	guard, err := descendantStartGuard(request.Source, request.PID, request.Evidence)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, guard.finish(true)) }()
	if err := descendantFreeze(request.Source, true); err != nil {
		return err
	}
	if request.Mode == "armed-capture" {
		threads, err := descendantThreads(request.Source)
		if err != nil {
			return err
		}
		return descendantCaptureCrashProof(guard, request.Source, threads, map[string]any{}, 1,
			func(journal captureCrashJournal) error {
				return controllerLossBarrier(socket, guard, request.Mode, &journal)
			})
	}
	if err := guard.capturePhase(true); err != nil {
		return err
	}
	if err := descendantFreeze(request.Source, false); err != nil {
		return err
	}
	return controllerLossBarrier(socket, guard, request.Mode, nil)
}

func controllerLossBarrier(socket *os.File, guard *descendantGuard, mode string,
	journal *captureCrashJournal) error {
	controller, err := captureProcess(os.Getpid())
	if err != nil {
		return err
	}
	helper, err := captureProcess(guard.cmd.Process.Pid)
	if err != nil {
		return err
	}
	ready := controllerLossReady{Version: 1, Config: guard.config, Controller: controller, Helper: helper, Mode: mode}
	if journal != nil {
		ready.JournalSHA, err = captureFileSHA(guard.config.Evidence + "/crash-armed-journal.json")
		if err != nil {
			return err
		}
	}
	data, err := json.Marshal(ready)
	if err != nil {
		return err
	}
	if _, err := socket.Write(data); err != nil {
		return err
	}
	// The owner SIGKILLs this real controller at the journaled barrier.
	if _, err := descendantGuardRead(3, -1); err != nil {
		return err
	}
	return fmt.Errorf("controller-loss fixture ended without the expected SIGKILL")
}

func controllerLossSubreaper() (int, error) {
	var value int32
	_, _, errno := unix.Syscall6(unix.SYS_PRCTL, unix.PR_GET_CHILD_SUBREAPER,
		uintptr(unsafe.Pointer(&value)), 0, 0, 0, 0)
	if errno != 0 {
		return 0, errno
	}
	return int(value), nil
}

func descendantControllerLossProof(source descendantLayout, evidenceRoot string) (_ map[string]any, resultErr error) {
	if source.ID != "copy-sysbox" || evidenceRoot != "/experiment/descendant/generation-1" ||
		os.Getenv("RUNTIME_FORK_CONTROLLER_LOSS_PROOF") != "1" {
		return nil, fmt.Errorf("controller-loss proof is outside its explicit owned first source")
	}
	start := time.Now()
	mode := os.Getenv("RUNTIME_FORK_CONTROLLER_LOSS_MODE")
	if mode == "" {
		mode = "before-capture"
	}
	if mode != "before-capture" && mode != "armed-capture" {
		return nil, fmt.Errorf("controller-loss proof has an unsupported phase")
	}
	beforeIdentity, err := criuRuntimeIdentity(source.ID)
	if err != nil {
		return nil, err
	}
	pid, ok := beforeIdentity["init_pid"].(int)
	if !ok {
		return nil, fmt.Errorf("controller-loss proof has no original source PID")
	}
	original, err := captureProcess(pid)
	if err != nil || original.NSPID != 1 {
		return nil, fmt.Errorf("controller-loss proof has no original source PID-1: %v", err)
	}
	states := map[string]map[string]any{}
	execs := map[string]map[string]any{}
	for _, inner := range []string{"", "alpha", "beta"} {
		states[inner], err = criuState(source.ID, inner, "/state")
		if err != nil {
			return nil, err
		}
	}
	for _, inner := range []string{"", "alpha"} {
		execs[inner], err = criuLiveExecState(source.ID, inner, "/state")
		if err != nil {
			return nil, err
		}
	}
	history, err := descendantOpenCodeHistory(source.ID)
	if err != nil {
		return nil, err
	}
	browser, err := criuRunc("exec", source.ID, "node", "/opt/browser-state.mjs", "get")
	if err != nil {
		return nil, err
	}
	composeBefore, err := criuComposeState(source.ID, "/state")
	if err != nil {
		return nil, err
	}
	markerBefore, err := criuComposeMarkerRead(source.ID)
	if err != nil {
		return nil, err
	}
	evidence := evidenceRoot + "/controller-loss"
	if err := os.Mkdir(evidence, 0700); err != nil {
		return nil, err
	}
	anchor := descendantGuardAnchors{}
	defer func() { resultErr = errors.Join(resultErr, anchor.close()) }()
	anchor.group, err = descendantGuardOpen(source.Cgroup)
	if err != nil {
		return nil, err
	}
	anchor.evidence, err = descendantGuardOpen(evidence)
	if err != nil {
		return nil, err
	}
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		return nil, err
	}
	anchor.pid = os.NewFile(uintptr(fd), "original controller-loss source pidfd")
	var originalNetwork *captureNetwork
	if mode == "armed-capture" {
		originalNetwork, err = captureNetworkOriginal(pid)
		if err != nil {
			return nil, err
		}
		defer originalNetwork.anchor.Close()
	}
	request := controllerLossRequest{1, source, pid, original.Start, evidence, mode}
	if err := descendantGuardWrite(anchor.evidence, "worker-intent.json", map[string]any{
		"request": request, "before_identity": beforeIdentity, "capture_worker_started": false,
		"phase": mode,
	}); err != nil {
		return nil, err
	}
	previousSubreaper, err := controllerLossSubreaper()
	if err != nil || previousSubreaper < 0 || previousSubreaper > 1 {
		return nil, fmt.Errorf("read owned worker subreaper state: %v", err)
	}
	if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0); err != nil {
		return nil, err
	}
	subreaperRestored := false
	defer func() {
		if !subreaperRestored {
			resultErr = errors.Join(resultErr, unix.Prctl(unix.PR_SET_CHILD_SUBREAPER,
				uintptr(previousSubreaper), 0, 0, 0))
		}
	}()
	sockets, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	socket := os.NewFile(uintptr(sockets[0]), "owned controller-loss worker socket")
	childSocket := os.NewFile(uintptr(sockets[1]), "owned controller-loss child socket")
	defer socket.Close()
	defer childSocket.Close()
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	log, err := os.OpenFile(evidence+"/controller.log", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, err
	}
	defer log.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable)
	command.Env = append(os.Environ(), controllerLossChild+"=1", "GOMAXPROCS=1", "GOMEMLIMIT=32MiB")
	command.ExtraFiles = []*os.File{childSocket}
	command.Stdout, command.Stderr, command.WaitDelay = log, log, time.Second
	if err := command.Start(); err != nil {
		return nil, err
	}
	childSocket.Close()
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	exited := false
	defer func() {
		if !exited {
			command.Process.Kill()
			<-done
		}
	}()
	controller, err := captureProcess(command.Process.Pid)
	if err != nil {
		return nil, err
	}
	if err := descendantGuardWrite(anchor.evidence, "worker-controller-started.json", controller); err != nil {
		return nil, err
	}
	data, _ := json.Marshal(request)
	if _, err := socket.Write(data); err != nil {
		return nil, err
	}
	var ready controllerLossReady
	if err := controllerLossRead(int(socket.Fd()), &ready); err != nil {
		return nil, err
	}
	if ready.Version != 1 || ready.Config.Source != source || ready.Config.Evidence != evidence ||
		ready.Config.PID != pid || ready.Config.Start != original.Start ||
		ready.Config.Controller != controller.PID || ready.Controller.PID != controller.PID ||
		ready.Controller.Start != controller.Start || ready.Helper.PPID != controller.PID || ready.Mode != mode {
		return nil, fmt.Errorf("controller-loss barrier differs from its exact owned processes")
	}
	if err := descendantGuardValidate(ready.Config, anchor); err != nil {
		return nil, err
	}
	events, err := descendantGuardEvents(anchor.group)
	expectedFrozen := uint64(0)
	if mode == "armed-capture" {
		expectedFrozen = 1
	}
	if err != nil || events["populated"] != 1 || events["frozen"] != expectedFrozen {
		return nil, fmt.Errorf("controller-loss barrier has an unexpected actual source state: %v", err)
	}
	// These are passive original worker anchors, not a second guard process.
	reconciliation := &descendantGuard{config: ready.Config, anchor: anchor, unsafe: true}
	var capture *controllerLossCapture
	if mode == "armed-capture" {
		capture, err = controllerLossCaptureOpen(reconciliation, ready, originalNetwork)
		if err != nil {
			return nil, err
		}
		defer func() { resultErr = errors.Join(resultErr, capture.close(resultErr != nil)) }()
	}
	helper, err := captureProcess(ready.Helper.PID)
	if err != nil || helper.Start != ready.Helper.Start || helper.PPID != controller.PID {
		return nil, fmt.Errorf("controller-loss guard helper identity changed: %v", err)
	}
	controllerFD, err := unix.PidfdOpen(controller.PID, 0)
	if err != nil {
		return nil, err
	}
	defer unix.Close(controllerFD)
	currentController, err := captureProcess(controller.PID)
	if err != nil || currentController.Start != controller.Start {
		return nil, fmt.Errorf("controller changed while opening its kill pidfd: %v", err)
	}
	if err := descendantGuardWrite(anchor.evidence, "worker-kill-intent.json", map[string]any{
		"ready": ready, "kernel_before_kill_events": events, "capture_worker_started": capture != nil,
	}); err != nil {
		return nil, err
	}
	if err := unix.PidfdSendSignal(controllerFD, unix.SIGKILL, nil, 0); err != nil {
		return nil, err
	}
	select {
	case err := <-done:
		exited = true
		var killed *exec.ExitError
		if !errors.As(err, &killed) || !killed.ProcessState.Sys().(syscall.WaitStatus).Signaled() ||
			killed.ProcessState.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
			return nil, fmt.Errorf("owned controller was not actually SIGKILLed: %v", err)
		}
	case <-time.After(3 * time.Second):
		return nil, fmt.Errorf("owned controller did not exit after SIGKILL")
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		helper, err := captureProcess(ready.Helper.PID)
		if err != nil || helper.Start != ready.Helper.Start || helper.PPID != os.Getpid() {
			return nil, fmt.Errorf("owned worker did not adopt its exact guard helper: %v", err)
		}
		var status unix.WaitStatus
		found, err := unix.Wait4(helper.PID, &status, unix.WNOHANG, nil)
		if err != nil {
			return nil, err
		}
		if found == helper.PID {
			if !status.Exited() || status.ExitStatus() != 0 {
				return nil, fmt.Errorf("owned controller-loss guard failed: %v", status)
			}
			break
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("owned controller-loss guard did not exit within its bound")
		}
		time.Sleep(time.Millisecond)
	}
	var outcome struct {
		Config          descendantGuardConfig `json:"config"`
		Reason          string                `json:"reason"`
		Status          string                `json:"status"`
		Passed          bool                  `json:"passed"`
		CaptureInFlight bool                  `json:"capture_in_flight"`
	}
	content, err := os.ReadFile(filepath.Join(evidence, "guard-outcome.json"))
	if err != nil || json.Unmarshal(content, &outcome) != nil || outcome.Config != ready.Config ||
		!outcome.Passed || !outcome.CaptureInFlight || outcome.Reason != "controller_disconnected" ||
		outcome.Status != "original_source_held_for_capture_reconciliation" {
		return nil, fmt.Errorf("controller-loss guard did not durably hold its original source: %s %v", content, err)
	}
	var captureOutcome *controllerCaptureResult
	var replayProof map[string]any
	if capture != nil {
		captureOutcome, err = capture.abortAndRecover(reconciliation, originalNetwork)
		if err != nil {
			return nil, err
		}
		if os.Getenv("RUNTIME_FORK_CONTROLLER_REPLAY_PROOF") == "1" {
			replayProof, err = controllerCaptureReplayProof(capture, reconciliation, originalNetwork, captureOutcome)
			if err != nil {
				return nil, err
			}
		}
	}
	if err := descendantGuardCaptureReturned(ready.Config, anchor); err != nil {
		return nil, err
	}
	held, err := descendantGuardEvents(anchor.group)
	if err != nil || held["frozen"] != 1 || held["populated"] != 1 {
		return nil, fmt.Errorf("controller-loss source was not actually refrozen: %v", err)
	}
	if _, err := descendantGuardRecover(ready.Config, anchor); err != nil {
		return nil, err
	}
	if _, err := criuRestoredStates(source.ID, []string{"", "alpha", "beta"}, states); err != nil {
		return nil, err
	}
	for _, inner := range []string{"", "alpha"} {
		current, err := criuLiveExecState(source.ID, inner, "/state")
		if err != nil {
			return nil, err
		}
		for key, expected := range execs[inner] {
			if key != "ticks" && asJSON(current[key]) != asJSON(expected) {
				return nil, fmt.Errorf("controller loss changed held exec %s field %s", inner, key)
			}
		}
		oldTicks, oldOK := execs[inner]["ticks"].(float64)
		newTicks, newOK := current["ticks"].(float64)
		if !oldOK || !newOK || newTicks < oldTicks {
			return nil, fmt.Errorf("controller loss regressed held exec counter %s", inner)
		}
	}
	afterIdentity, err := criuRuntimeIdentity(source.ID)
	currentHistory, historyErr := descendantOpenCodeHistory(source.ID)
	currentBrowser, browserErr := criuRunc("exec", source.ID, "node", "/opt/browser-state.mjs", "get")
	if err != nil || asJSON(afterIdentity) != asJSON(beforeIdentity) ||
		historyErr != nil || asJSON(currentHistory) != asJSON(history) ||
		browserErr != nil || currentBrowser != browser {
		return nil, fmt.Errorf("controller loss changed source identity/browser/OpenCode state: %v %v %v",
			err, historyErr, browserErr)
	}
	composeAfter, err := criuComposeState(source.ID, "/state")
	if err != nil {
		return nil, err
	}
	for key, expected := range composeBefore {
		if key != "ticks" && asJSON(composeAfter[key]) != asJSON(expected) {
			return nil, fmt.Errorf("controller loss changed retained Dev Container field %s", key)
		}
	}
	oldTicks, oldOK := composeBefore["ticks"].(float64)
	newTicks, newOK := composeAfter["ticks"].(float64)
	if !oldOK || !newOK || newTicks < oldTicks {
		return nil, fmt.Errorf("controller loss regressed retained Dev Container counter")
	}
	markerAfter, err := criuComposeMarkerRead(source.ID)
	if err != nil || markerAfter != markerBefore {
		return nil, fmt.Errorf("controller loss changed retained Dev Container file: %v", err)
	}
	previousMarker := criuComposeMarker
	criuComposeMarker = strings.TrimSpace(markerBefore)
	theiaErr := criuSysboxTheiaReady([]string{source.ID}, "controller-loss-reconciled", start)
	criuComposeMarker = previousMarker
	if theiaErr != nil {
		return nil, theiaErr
	}
	if replayProof != nil {
		beforeReplay, err := descendantGuardEvents(anchor.group)
		if err != nil || beforeReplay["frozen"] != 0 {
			return nil, fmt.Errorf("running source was not ready for completed recovery replay: %v", err)
		}
		replayed, err := capture.abortAndRecover(reconciliation, originalNetwork)
		if err != nil || asJSON(replayed) != asJSON(captureOutcome) {
			return nil, fmt.Errorf("completed recovery replay failed after source continuation: %v", err)
		}
		afterReplay, err := descendantGuardEvents(anchor.group)
		if err != nil || asJSON(beforeReplay) != asJSON(afterReplay) {
			return nil, fmt.Errorf("completed replay changed the running source freeze state: %v", err)
		}
		running := map[string]any{"passed": true, "source_events_before": beforeReplay,
			"source_events_after": afterReplay, "completed_result_unchanged": true}
		if err := descendantGuardWrite(anchor.evidence, "worker-capture-replay-running.json", running); err != nil {
			return nil, err
		}
		replayProof["running_source_replay"] = running
	}
	if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, uintptr(previousSubreaper), 0, 0, 0); err != nil {
		return nil, err
	}
	restoredSubreaper, err := controllerLossSubreaper()
	if err != nil || restoredSubreaper != previousSubreaper {
		return nil, fmt.Errorf("worker subreaper state was not restored: %v", err)
	}
	subreaperRestored = true
	result := map[string]any{
		"passed": true, "source": source, "original_pid": pid, "original_start": original.Start,
		"before_identity": beforeIdentity, "after_identity": afterIdentity, "ready": ready,
		"controller_actually_sigkilled": true, "guard_helper_adopted_and_reaped": true,
		"kernel_held_events": held, "guard_outcome": json.RawMessage(content),
		"source_ram_held_exec_browser_and_idle_opencode_preserved": true,
		"retained_devcontainer_ram_files_and_native_theia_passed":  true,
		"worker_subreaper_before":                                  previousSubreaper, "worker_subreaper_after": restoredSubreaper,
		"capture_worker_started": capture != nil, "elapsed_ms": time.Since(start).Milliseconds(),
		"phase": mode, "capture_reconciliation": captureOutcome,
		"completed_capture_replay": replayProof,
		"scope": "Real controller SIGKILL at its explicit journaled phase. " +
			"Surviving worker validates original anchors and the orphaned processes, reconciles the known phase, " +
			"then requires zero tracers before source continuation.",
	}
	if err := descendantGuardWrite(anchor.evidence, "worker-proof.json", result); err != nil {
		return nil, err
	}
	record("sysbox-controller-loss-original-source-reconciled", start, asJSON(map[string]any{
		"proof": evidence + "/worker-proof.json", "source_id": source.ID, "original_pid": pid,
	}), nil)
	return result, nil
}
