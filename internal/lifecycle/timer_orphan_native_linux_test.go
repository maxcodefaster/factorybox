//go:build linux && amd64

package lifecycle

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// Let the real guard examine a completed process cure with a dead, reaped
// worker and controller. Holding the exact guard delays its decision without
// changing the runtime or its evidence. The staged source must remain held.
func TestNativeTimerOrphan(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_TIMER_ORPHAN") != "1" {
		t.Skip("owned delivered-CLI timer orphan boundary only")
	}
	// This isolated probe owns the entire child tree. Reap the exact orphaned
	// worker before releasing the guard, so a zombie cannot mask the timer rule.
	if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	status := boundedLossStatusNow(t)
	assembly := nativeTimerRemainderProtectionAssembly()
	if err := os.WriteFile("/output/timer-orphan.S", []byte(assembly), 0644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("cc", "-nostdlib", "-static", "-Wl,--build-id=none", "/output/timer-orphan.S", "-o", "/output/timer-orphan").CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	program, err := os.ReadFile("/output/timer-orphan")
	if err != nil {
		t.Fatal(err)
	}
	nm, err := exec.Command("nm", "-n", "/output/timer-orphan").Output()
	if err != nil {
		t.Fatal(err)
	}
	var counter uint64
	for _, line := range strings.Split(string(nm), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[2] == "signal_count" {
			counter, err = strconv.ParseUint(fields[0], 16, 64)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if counter == 0 {
		t.Fatal("original signal counter absent")
	}
	id, snapshot := "timer-orphan", strings.Repeat("f", 32)
	recordPath := status.Lifecycle.Root + "/objects/" + id + "/record.json"
	cleanup := func(t *testing.T, source View) {
		before, beforeErr := os.ReadFile(recordPath)
		_, rejected := boundedLossCommand("--worker-run-root", "/run/factorybox", "delete", id)
		after, afterErr := os.ReadFile(recordPath)
		if beforeErr != nil || afterErr != nil || rejected == nil || !strings.Contains(rejected.Error(), "pause/resume is unresolved") || !bytes.Equal(before, after) {
			t.Error("unresolved pause evidence was not retained", beforeErr, afterErr, rejected)
		}
		boundedLossSave(t, "timer-orphan-retained.json", map[string]any{"record": json.RawMessage(after), "delete_refused": rejected != nil,
			"cleanup": "Source, unresolved pause and mounts retained until the unique owned outer worker/volume teardown. No diagnostic thaw."})
	}
	source := timerReaderFixtureFilesWithCleanup(t, status, id, "exec /bin/timer-orphan", false, map[string][]byte{"timer-orphan": program}, cleanup)
	sourceFD, err := unix.PidfdOpen(source.Runtime.PID, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(sourceFD)
	for deadline := time.Now().Add(time.Second); ; {
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/syscall", source.Runtime.PID))
		if err == nil && strings.HasPrefix(string(data), "230 0x1 0x0 ") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("source did not enter its original native wait", string(data), err)
		}
		time.Sleep(time.Millisecond)
	}
	directory := status.Store + "/" + id + "-bundle/rootfs/copy-window"
	if err := os.Mkdir(directory, 0755); err != nil {
		t.Fatal(err)
	}
	for number := range 4096 {
		if err := os.WriteFile(filepath.Join(directory, strconv.Itoa(number)), []byte("owned copy boundary\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/opt/factorybox/factorybox", "--worker-run-root", "/run/factorybox", "pause", "--snapshot-id", snapshot, id)
	command.WaitDelay = 2 * time.Second
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	defer func() {
		if !waited {
			command.Process.Kill()
			command.Wait()
		}
		for name, data := range map[string][]byte{"stdout": stdout.Bytes(), "stderr": stderr.Bytes()} {
			if err := os.WriteFile("/output/timer-orphan-controller."+name, data, 0600); err != nil {
				t.Error("retain controller output", err)
			}
		}
	}()
	guardDir := status.Lifecycle.Root + "/objects/" + id + "/captures/" + snapshot + "/guard"
	read := func(name string, value any) error {
		data, err := os.ReadFile(guardDir + "/" + name)
		if err != nil {
			return err
		}
		return json.Unmarshal(data, value)
	}
	var worker captureWorkerIntent
	copyFD := -1
	for deadline := time.Now().Add(10 * time.Second); ; {
		if read("capture-worker-intent.json", &worker) == nil && worker.Token == snapshot && worker.SourcePID == source.Runtime.PID && worker.CriuSHA256 == status.Lifecycle.CriuSHA256 {
			copyFD, err = stopCaptureCopyChild(worker.PID)
			if err != nil {
				t.Fatal(err)
			}
			if copyFD >= 0 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("owned filesystem copy boundary absent", stderr.String())
		}
		time.Sleep(time.Millisecond)
	}
	defer unix.Close(copyFD)
	defer unix.PidfdSendSignal(copyFD, unix.SIGKILL, nil, 0)
	var complete captureProcessCompletion
	var prepared struct {
		Token   string                `json:"token"`
		Passed  bool                  `json:"passed"`
		Targets []timerPreparedTarget `json:"targets"`
	}
	var config captureGuardConfig
	var armed struct {
		Token string `json:"token"`
		PID   int    `json:"helper_pid"`
	}
	if err := read("capture-process-complete.json", &complete); err != nil || complete.Worker != worker {
		t.Fatal("actual completed process receipt absent", err)
	}
	if err := read("timer-prepare-complete.json", &prepared); err != nil || prepared.Token != snapshot || !prepared.Passed || len(prepared.Targets) != 1 {
		t.Fatal("original timer preparation absent", err)
	}
	if err := read("guard-intent.json", &config); err != nil || config.Controller != command.Process.Pid || config.PID != source.Runtime.PID || config.Token != snapshot {
		t.Fatal("original guard intent differs", err)
	}
	if err := read("guard-armed.json", &armed); err != nil || armed.Token != snapshot || armed.PID <= 1 {
		t.Fatal("original guard identity absent", err)
	}
	pin := func(pid int, argument, birth string) int {
		actual, actualErr := os.Stat(fmt.Sprintf("/proc/%d/exe", pid))
		expected, expectedErr := os.Stat("/opt/factorybox/factorybox")
		args, argsErr := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
		state, stateErr := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
		if actualErr != nil || expectedErr != nil || !os.SameFile(actual, expected) || argsErr != nil ||
			string(args) != "/opt/factorybox/factorybox\x00"+argument+"\x00" || stateErr != nil ||
			!bytes.Contains(state, []byte(fmt.Sprintf("\nPPid:\t%d\n", command.Process.Pid))) {
			t.Fatal("refusing to stop a foreign capture helper", pid, actualErr, expectedErr, argsErr, stateErr)
		}
		if birth == "" {
			birth, err = processBirth(pid)
			if err != nil {
				t.Fatal(err)
			}
		}
		return timerSignalStoppedController(t, captureWorkerIntent{PID: pid, Start: birth})
	}
	workerFD := pin(worker.PID, "_capture", worker.Start)
	guardFD := pin(armed.PID, "_capture-guard", "")
	birth, err := processBirth(command.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	controllerFD := timerSignalStoppedController(t, captureWorkerIntent{PID: command.Process.Pid, Start: birth})
	if err := unix.PidfdSendSignal(workerFD, unix.SIGKILL, nil, 0); err != nil {
		t.Fatal(err)
	}
	if err := unix.PidfdSendSignal(controllerFD, unix.SIGKILL, nil, 0); err != nil {
		t.Fatal(err)
	}
	waitErr := command.Wait()
	waited = true
	if waitErr == nil || command.ProcessState.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
		t.Fatal("original controller did not die from actual SIGKILL", waitErr)
	}
	var workerStatus unix.WaitStatus
	if pid, err := unix.Wait4(worker.PID, &workerStatus, 0, nil); err != nil || pid != worker.PID || !workerStatus.Signaled() || workerStatus.Signal() != unix.SIGKILL {
		t.Fatal("original worker was not reaped after actual SIGKILL", pid, workerStatus, err)
	}
	if _, err := processBirth(worker.PID); err == nil {
		t.Fatal("worker remains visible and could mask the timer refusal")
	}
	var copyExit unix.Siginfo
	if err := unix.Waitid(unix.P_PIDFD, copyFD, &copyExit, unix.WEXITED, nil); err != nil {
		t.Fatal("reap only the exact adopted copy child", err)
	}
	if err := unix.PidfdSendSignal(sourceFD, unix.SIGUSR1, nil, 0); err != nil {
		t.Fatal(err)
	}
	pending, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", source.Runtime.PID))
	queued := false
	for _, line := range strings.Split(string(pending), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && (fields[0] == "SigPnd:" || fields[0] == "ShdPnd:") {
			bits, parseErr := strconv.ParseUint(fields[1], 16, 64)
			queued = queued || parseErr == nil && bits&(1<<(uint(unix.SIGUSR1)-1)) != 0
		}
	}
	if err != nil || !queued {
		t.Fatal("source did not actually retain queued SIGUSR1", err)
	}
	if err := unix.PidfdSendSignal(guardFD, unix.SIGCONT, nil, 0); err != nil {
		t.Fatal(err)
	}
	var outcome struct {
		Config   captureGuardConfig `json:"config"`
		Passed   bool               `json:"passed"`
		InFlight bool               `json:"capture_in_flight"`
		Status   string             `json:"status"`
	}
	for deadline := time.Now().Add(3 * time.Second); read("guard-outcome.json", &outcome) != nil; {
		if time.Now().After(deadline) {
			t.Fatal("original guard did not complete its orphan decision")
		}
		time.Sleep(time.Millisecond)
	}
	var guardStatus unix.WaitStatus
	if pid, err := unix.Wait4(armed.PID, &guardStatus, 0, nil); err != nil || pid != armed.PID || !guardStatus.Exited() || guardStatus.ExitStatus() != 0 {
		t.Fatal("original guard did not finish its own decision", pid, guardStatus, err)
	}
	proof := map[string]any{"passed": false, "production_qualified": false, "task_sha256": sum(program), "assembly_sha256": sum([]byte(assembly)),
		"controller_actual_sigkill": true, "worker_actual_sigkill_and_reaped": true, "original_process_capture_completed": true, "outcome": outcome}
	defer func() { boundedLossSave(t, "timer-orphan.json", proof) }()
	var rejection struct {
		Token  string `json:"token"`
		Passed bool   `json:"passed"`
		Error  string `json:"error"`
	}
	if err := read("guard-orphan-recovery.json", &rejection); err != nil || rejection.Token != snapshot || rejection.Passed ||
		!strings.Contains(rejection.Error, "timer mutation remains unconfirmed") || !outcome.Passed || !outcome.InFlight ||
		outcome.Status != "original_source_held_for_capture_reconciliation" || outcome.Config.PID != source.Runtime.PID {
		t.Fatal("orphan did not retain its exact staged timer", rejection, outcome, err)
	}
	for _, name := range []string{"timer-activation-complete.json", "capture-abort-intent.json", "guard-capture-aborted.json"} {
		if _, err := os.Stat(guardDir + "/" + name); !os.IsNotExist(err) {
			t.Fatal("unfinished timer was falsely completed", name, err)
		}
	}
	group, err := captureGuardOpen(cgroupRoot + "/" + source.Record.RuntimeID)
	if err != nil {
		t.Fatal(err)
	}
	defer group.Close()
	for deadline := time.Now().Add(1250 * time.Millisecond); ; {
		events, err := captureGuardEvents(group)
		count, countErr := timerMemoryRead(source.Runtime.PID, counter, 8)
		state, stateErr := os.ReadFile(fmt.Sprintf("/proc/%d/status", source.Runtime.PID))
		start, startErr := processBirth(source.Runtime.PID)
		if err != nil || events["frozen"] != 1 || events["populated"] != 1 || countErr != nil || binary.LittleEndian.Uint64(count) != 0 ||
			stateErr != nil || !bytes.Contains(state, []byte("\nTracerPid:\t0\n")) || startErr != nil || start != config.Start {
			t.Fatal("original source escaped its kernel hold", events, err, countErr, stateErr, startErr)
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := os.Mkdir("/output/timer-orphan-evidence", 0755); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(guardDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") || !entry.Type().IsRegular() {
			continue
		}
		data, err := os.ReadFile(guardDir + "/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile("/output/timer-orphan-evidence/"+entry.Name(), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	proof["passed"], proof["source_frozen_untraced"], proof["queued_signal_handler_not_run"], proof["timer_completion_refused"] = true, true, true, true
	proof["guard_rejection"], proof["observed_hold_ms"], proof["no_automatic_repair_claim"] = rejection, 1250, true
}
