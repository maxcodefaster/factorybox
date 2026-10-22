//go:build linux

package lifecycle

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// The source guard traces only the mutator's owning OS thread. An EXIT event
// stops that thread before the kernel detaches its source tracees, including
// on SIGKILL. Freeze the original cgroup before allowing that exit to finish.
type captureTimerWatch struct {
	config  captureGuardConfig
	anchors captureGuardAnchors
	ready   timerMutatorReady
	pidfd   int
	active  bool
	events  chan os.Signal
}

func armCaptureTimerWatch(config captureGuardConfig, anchors captureGuardAnchors) (_ *captureTimerWatch, resultErr error) {
	if err := captureGuardValidate(config, anchors); err != nil {
		return nil, err
	}
	data, err := captureReadFile(anchors.evidence, "timer-mutator-ready.json", 4096)
	if err != nil {
		return nil, err
	}
	var ready timerMutatorReady
	if json.Unmarshal(data, &ready) != nil || ready.Token != config.Token || ready.PID <= 1 || ready.TID <= 1 || ready.ParentPID <= 1 {
		return nil, fmt.Errorf("invalid exact timer mutator readiness")
	}
	data, err = captureReadFile(anchors.evidence, "capture-worker-intent.json", 4096)
	if err != nil {
		return nil, err
	}
	var worker captureWorkerIntent
	if json.Unmarshal(data, &worker) != nil || worker.Token != config.Token || worker.PID != ready.ParentPID || worker.Start != ready.ParentStart || worker.SourcePID != config.PID || worker.CriuSHA256 != config.CriuSHA256 {
		return nil, fmt.Errorf("timer mutator is outside its original capture worker")
	}
	parentBirth, err := processBirth(ready.ParentPID)
	if err != nil || parentBirth != ready.ParentStart {
		return nil, fmt.Errorf("timer worker parent identity changed: %v", err)
	}
	birth, err := processBirth(ready.TID)
	if err != nil || birth != ready.Start {
		return nil, fmt.Errorf("timer tracer thread identity changed: %v", err)
	}
	status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", ready.TID))
	if err != nil {
		return nil, err
	}
	matched := false
	for _, row := range strings.Split(string(status), "\n") {
		fields := strings.Fields(row)
		if len(fields) == 2 && fields[0] == "Tgid:" && fields[1] == strconv.Itoa(ready.PID) {
			matched = true
		}
		if len(fields) == 2 && fields[0] == "TracerPid:" && fields[1] != "0" {
			return nil, fmt.Errorf("timer mutator already has a foreign tracer")
		}
	}
	if !matched {
		return nil, fmt.Errorf("timer tracer thread belongs to another process")
	}
	actual, err := os.Stat(fmt.Sprintf("/proc/%d/exe", ready.PID))
	if err != nil {
		return nil, err
	}
	expected, err := os.Stat("/proc/self/exe")
	if err != nil || !os.SameFile(actual, expected) {
		return nil, fmt.Errorf("timer mutator executable differs from its original guard: %v", err)
	}
	fd, err := unix.PidfdOpen(ready.TID, unix.O_EXCL)
	if err != nil {
		return nil, err
	}
	watch := &captureTimerWatch{config: config, anchors: anchors, ready: ready, pidfd: fd, events: make(chan os.Signal, 1)}
	signal.Notify(watch.events, unix.SIGCHLD)
	defer func() {
		if resultErr != nil {
			signal.Stop(watch.events)
			unix.Close(fd)
		}
	}()
	_, _, errno := unix.Syscall6(unix.SYS_PTRACE, unix.PTRACE_SEIZE, uintptr(ready.TID), 0, unix.PTRACE_O_TRACEEXIT, 0, 0)
	if errno != 0 {
		return nil, errno
	}
	watch.active = true
	return watch, nil
}

func (watch *captureTimerWatch) holdBeforeExit() {
	// Never release the exiting tracer when the exact source hold cannot be
	// confirmed. The live guard stays in the owned worker for reconciliation;
	// the outer 420-second native deadline can still remove that whole worker.
	wrote := false
	for {
		if _, err := captureGuardHold(watch.config, watch.anchors); err == nil {
			return
		} else if !wrote {
			_ = captureGuardWrite(watch.anchors.evidence, "timer-exit-hold-unconfirmed.json", map[string]any{"token": watch.config.Token, "error": err.Error(), "mutator_tid": watch.ready.TID})
			wrote = true
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (watch *captureTimerWatch) poll() error {
	if !watch.active {
		return nil
	}
	var status unix.WaitStatus
	pid, err := unix.Wait4(watch.ready.TID, &status, unix.WALL|unix.WNOHANG, nil)
	if errors.Is(err, unix.EINTR) {
		return nil
	}
	if err != nil {
		return err
	}
	if pid == 0 {
		return nil
	}
	if status.Exited() || status.Signaled() {
		watch.active = false
		signal.Stop(watch.events)
		return unix.Close(watch.pidfd)
	}
	if !status.Stopped() {
		return fmt.Errorf("timer mutator guard received an unexpected wait state")
	}
	deliver := int(status.StopSignal())
	if status.TrapCause() == unix.PTRACE_EVENT_EXIT {
		watch.holdBeforeExit()
		message, err := unix.PtraceGetEventMsg(watch.ready.TID)
		if err != nil {
			return err
		}
		complete := false
		if data, err := captureReadFile(watch.anchors.evidence, "timer-prepare-complete.json", timerPlanLimit); err == nil {
			var receipt struct {
				Token  string `json:"token"`
				Passed bool   `json:"passed"`
			}
			complete = json.Unmarshal(data, &receipt) == nil && receipt.Token == watch.config.Token && receipt.Passed && captureGuardCaptureReturned(watch.config, watch.anchors) == nil
		}
		if err := captureGuardWrite(watch.anchors.evidence, "timer-mutator-exit-held.json", map[string]any{
			"token": watch.config.Token, "mutator_tid": watch.ready.TID, "exit_event": message, "source_held": true, "preparation_complete": complete}); err != nil {
			return err
		}
		deliver = 0
	}
	return unix.PtraceCont(watch.ready.TID, deliver)
}

func (watch *captureTimerWatch) closeHeld() error {
	if !watch.active {
		return nil
	}
	_ = unix.PidfdSendSignal(watch.pidfd, unix.SIGKILL, nil, 0)
	for watch.active {
		if err := watch.poll(); err != nil {
			// The guard must not disappear and auto-detach an unconfirmed
			// mutator. Preserve the live hold even if diagnostics fail.
			watch.holdBeforeExit()
			_ = captureGuardWrite(watch.anchors.evidence, "timer-mutator-watch-error.json", map[string]any{"token": watch.config.Token, "error": err.Error()})
		}
		if watch.active {
			time.Sleep(5 * time.Millisecond)
		}
	}
	return nil
}
