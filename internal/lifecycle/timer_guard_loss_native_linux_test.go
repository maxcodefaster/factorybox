//go:build linux && (amd64 || arm64)

package lifecycle

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// A surviving controller must hold an unfinished timer after the actual guard
// dies. This completed-process boundary has no retained-cure grant; it does
// not qualify automatic repair or the separate grant-in-flight boundary.
func TestNativeTimerGuardLoss(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_TIMER_GUARD_LOSS") != "1" {
		t.Skip("owned delivered-CLI timer guard-loss boundary only")
	}
	f := nativeTimerCaptureFaultFixture(t, "timer-guard-loss")
	proof := map[string]any{"passed": false, "production_qualified": false, "task_sha256": sum(f.program),
		"assembly_sha256": sum([]byte(f.assembly)), "source_pid": f.source.Runtime.PID, "controller_pid": f.command.Process.Pid}
	defer func() { boundedLossSave(t, "timer-guard-loss.json", proof) }()
	for _, name := range []string{"criu-retained-request.json", "timer-activation-complete.json"} {
		if _, err := os.Stat(f.guardDir + "/" + name); !os.IsNotExist(err) {
			t.Fatal("fault is outside the declared ungranted completed-process boundary", name, err)
		}
	}
	group, err := captureGuardOpen(cgroupRoot + "/" + f.source.Record.RuntimeID)
	if err != nil {
		t.Fatal(err)
	}
	defer group.Close()
	events, err := captureGuardEvents(group)
	if err != nil || events["frozen"] != 1 || events["populated"] != 1 {
		t.Fatal("source was not held before guard loss", events, err)
	}
	var prepared struct {
		Targets []timerPreparedTarget `json:"targets"`
	}
	if err := f.read("timer-prepare-complete.json", &prepared); err != nil || len(prepared.Targets) != 1 {
		t.Fatal("missing exact staged timer", err)
	}
	address := prepared.Targets[0].Address
	continuation := nativeTimerFaultContinuation(t, f.source.Runtime.PID, address)
	code, err := timerMemoryRead(f.source.Runtime.PID, address, len(continuation))
	if err != nil || !bytes.Equal(code, continuation) {
		t.Fatal("source lacks its exact original staged code", err)
	}
	if err := unix.PidfdSendSignal(f.sourceFD, unix.SIGUSR1, nil, 0); err != nil {
		t.Fatal(err)
	}
	if err := unix.PidfdSendSignal(f.guardFD, unix.SIGKILL, nil, 0); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(2 * time.Second); ; {
		poll := []unix.PollFd{{Fd: int32(f.guardFD), Events: unix.POLLIN}}
		n, err := unix.Poll(poll, 20)
		if err == nil && n == 1 && poll[0].Revents&unix.POLLIN != 0 {
			break
		}
		if err != nil && err != unix.EINTR || time.Now().After(deadline) {
			t.Fatal("original guard did not exit after actual SIGKILL", err)
		}
	}
	if err := unix.PidfdSendSignal(f.workerFD, unix.SIGKILL, nil, 0); err != nil {
		t.Fatal(err)
	}
	if err := unix.PidfdSendSignal(f.controllerFD, unix.SIGCONT, nil, 0); err != nil {
		t.Fatal(err)
	}
	waitErr := f.wait()
	if waitErr == nil || !f.command.ProcessState.Exited() || f.command.ProcessState.ExitCode() == 0 {
		t.Fatal("surviving controller did not report its unconfirmed capture", waitErr)
	}
	if _, err := processBirth(f.worker.PID); err == nil {
		t.Fatal("original controller failed to reap its dead capture worker")
	}
	if _, err := processBirth(f.guardPID); err == nil {
		t.Fatal("original controller failed to reap its dead guard")
	}
	var copyExit unix.Siginfo
	if err := unix.Waitid(unix.P_PIDFD, f.copyFD, &copyExit, unix.WEXITED, nil); err != nil {
		t.Fatal("reap only the exact adopted copy child", err)
	}
	var outcome struct {
		Config   captureGuardConfig `json:"config"`
		Passed   bool               `json:"passed"`
		InFlight bool               `json:"capture_in_flight"`
		Status   string             `json:"status"`
		Reason   string             `json:"reason"`
	}
	if err := f.read("guard-controller-outcome.json", &outcome); err != nil || !outcome.Passed || !outcome.InFlight ||
		outcome.Status != "original_source_held_for_capture_reconciliation" ||
		outcome.Reason != "helper_failed_controller_handled_original_source" || !reflect.DeepEqual(outcome.Config, f.config) {
		t.Fatal("original controller did not retain its exact source hold", outcome, err)
	}
	var refusal struct {
		Token  string `json:"token"`
		Passed bool   `json:"passed"`
		Error  string `json:"error"`
	}
	if err := f.read("guard-delegated-recovery.json", &refusal); err != nil || refusal.Token != f.snapshot || refusal.Passed ||
		refusal.Error != unix.ENOENT.Error() {
		t.Fatal("ungranted delegated cure refusal differs", refusal, err)
	}
	if _, err := os.Stat(f.guardDir + "/criu-retained-request.json"); !os.IsNotExist(err) {
		t.Fatal("ungranted boundary acquired a cure request", err)
	}
	before, err := os.ReadFile(f.recordPath)
	if err != nil {
		t.Fatal(err)
	}
	_, replayErr := boundedLossCommand("--worker-run-root", "/run/factorybox", "pause", "--snapshot-id", f.snapshot, f.source.Record.ID)
	after, err := os.ReadFile(f.recordPath)
	if replayErr == nil || err != nil || !bytes.Equal(before, after) {
		t.Fatal("unconfirmed pause replay changed its original record", replayErr, err)
	}
	for _, name := range []string{"timer-activation-complete.json", "capture-abort-intent.json", "guard-capture-aborted.json", "guard-outcome.json"} {
		if _, err := os.Stat(f.guardDir + "/" + name); !os.IsNotExist(err) {
			t.Fatal("guard loss was falsely completed", name, err)
		}
	}
	for deadline := time.Now().Add(1250 * time.Millisecond); ; {
		events, err := captureGuardEvents(group)
		count, countErr := timerMemoryRead(f.source.Runtime.PID, f.counter, 8)
		state, stateErr := os.ReadFile(fmt.Sprintf("/proc/%d/status", f.source.Runtime.PID))
		birth, birthErr := processBirth(f.source.Runtime.PID)
		queued := false
		for _, line := range strings.Split(string(state), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && (fields[0] == "SigPnd:" || fields[0] == "ShdPnd:") {
				bits, parseErr := strconv.ParseUint(fields[1], 16, 64)
				queued = queued || parseErr == nil && bits&(1<<(uint(unix.SIGUSR1)-1)) != 0
			}
		}
		if err != nil || events["frozen"] != 1 || events["populated"] != 1 || countErr != nil || binary.LittleEndian.Uint64(count) != 0 ||
			stateErr != nil || !bytes.Contains(state, []byte("\nTracerPid:\t0\n")) || !queued || birthErr != nil || birth != f.config.Start {
			t.Fatal("guard loss released or changed the original source", events, err, countErr, stateErr, birthErr, queued)
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	codeAfter, err := timerMemoryRead(f.source.Runtime.PID, address, len(continuation))
	if err != nil || !bytes.Equal(code, codeAfter) {
		t.Fatal("unconfirmed timer mutation changed after guard loss", err)
	}
	if err := os.Mkdir("/output/timer-guard-loss-evidence", 0755); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(f.guardDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(f.guardDir + "/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile("/output/timer-guard-loss-evidence/"+entry.Name(), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	proof["passed"], proof["guard_actual_sigkill"], proof["worker_actual_sigkill_and_reaped"], proof["original_controller_completed"] = true, true, true, true
	proof["source_frozen_untraced"], proof["queued_signal_handler_not_run"], proof["exact_replay_refused_record_unchanged"] = true, true, true
	proof["ungranted_cure_refused"], proof["automatic_repair_not_qualified"], proof["staged_code_unchanged"] = true, true, true
	proof["outcome"], proof["refusal"], proof["observed_hold_ms"], proof["staged_code_sha256"] = outcome, refusal, 1250, sum(codeAfter)
}
