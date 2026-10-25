//go:build linux && (amd64 || arm64)

package lifecycle

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// Queue one real caught signal at the public checkpoint's first freeze,
// before timer preparation. A transient preflight condition must settle
// without losing the signal, repeating its handler or replaying tool work.
func TestNativeTimerAdmissionSignal(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_TIMER_ADMISSION_SIGNAL") != "1" {
		t.Skip("owned pending-signal checkpoint admission fixture only")
	}
	status := boundedLossStatusNow(t)
	assembly := nativeTimerSignalAssembly()
	if err := os.WriteFile("/output/timer-admission-signal.S", []byte(assembly), 0644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("cc", "-nostdlib", "-static", "-Wl,--build-id=none",
		"/output/timer-admission-signal.S", "-o", "/output/timer-admission-signal").CombinedOutput(); err != nil {
		t.Fatal("assemble original signal/ABI fixture", err, string(out))
	}
	program, err := os.ReadFile("/output/timer-admission-signal")
	if err != nil {
		t.Fatal(err)
	}
	nm, err := exec.Command("nm", "-n", "/output/timer-admission-signal").Output()
	if err != nil {
		t.Fatal(err)
	}
	var counterAddress int64
	for _, line := range strings.Split(string(nm), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[2] == "signal_count" {
			counterAddress, err = strconv.ParseInt(fields[0], 16, 64)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if counterAddress <= 0 {
		t.Fatal("signal fixture has no exact handler counter")
	}
	id := "timer-admission-signal"
	view := timerReaderFixtureFilesWithCleanup(t, status, id, "exec /bin/timer-admission-signal", false,
		map[string][]byte{"timer-admission-signal": program}, func(t *testing.T, view View) {
			if t.Failed() {
				t.Log("retain exact failed signal source and overlay for outer owned archival")
				return
			}
			if _, err := boundedLossCommand("--worker-run-root", "/run/factorybox", "delete", id); err != nil {
				t.Error("delete exact signal source", err)
				return
			}
			if err := unix.Unmount(status.Store+"/"+id+"-bundle/rootfs", 0); err != nil {
				t.Error("release exact signal fixture overlay", err)
			}
		})
	for deadline := time.Now().Add(time.Second); ; {
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/syscall", view.Runtime.PID))
		fields := strings.Fields(string(data))
		if err == nil && len(fields) > 2 && fields[0] == strconv.Itoa(unix.SYS_CLOCK_NANOSLEEP) && fields[1] == "0x1" && fields[2] == "0x0" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("source did not enter its original relative timer", string(data), err)
		}
		time.Sleep(time.Millisecond)
	}
	group, err := captureGuardOpen(cgroupRoot + "/" + view.Record.RuntimeID)
	if err != nil {
		t.Fatal(err)
	}
	defer group.Close()
	birth, err := processBirth(view.Runtime.PID)
	if err != nil {
		t.Fatal(err)
	}
	pidfd, err := unix.PidfdOpen(view.Runtime.PID, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(pidfd)
	type queuedSignal struct {
		Queued bool   `json:"queued_while_frozen"`
		Status string `json:"status_after_queue"`
		Error  string `json:"error,omitempty"`
	}
	queued := make(chan queuedSignal, 1)
	stop := make(chan struct{})
	go func() {
		for deadline := time.Now().Add(15 * time.Second); ; {
			events, err := captureGuardEvents(group)
			if err != nil {
				queued <- queuedSignal{Error: err.Error()}
				return
			}
			if events["frozen"] == 1 {
				current, err := processBirth(view.Runtime.PID)
				if err != nil || current != birth {
					queued <- queuedSignal{Error: "exact source birth changed"}
					return
				}
				if err := unix.PidfdSendSignal(pidfd, unix.SIGUSR1, nil, 0); err != nil {
					queued <- queuedSignal{Error: err.Error()}
					return
				}
				data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", view.Runtime.PID))
				row := queuedSignal{Queued: true, Status: string(data)}
				if err != nil {
					row.Error = err.Error()
				}
				queued <- row
				return
			}
			if time.Now().After(deadline) {
				queued <- queuedSignal{Error: "checkpoint did not freeze the exact source"}
				return
			}
			select {
			case <-stop:
				queued <- queuedSignal{Error: "checkpoint ended before source freeze"}
				return
			case <-time.After(100 * time.Microsecond):
			}
		}
	}()
	snapshot := strings.Repeat("a", 32)
	output, checkpointErr := boundedLossCommand("--worker-run-root", "/run/factorybox",
		"checkpoint", "--snapshot-id", snapshot, id)
	close(stop)
	observed := <-queued
	events, eventErr := captureGuardEvents(group)
	proof := map[string]any{"passed": false, "signal": observed, "events_after_checkpoint": events,
		"task_sha256": sum(program), "assembly_sha256": sum([]byte(assembly)),
		"source_pid": view.Runtime.PID, "source_birth": birth,
		"scope": "One real SIGUSR1 queued at initial freeze, before timer mutation. Original raw EINTR/handler/ABI assertions retained; successful public checkpoint, independent fork and exact replay required."}
	if checkpointErr != nil || !observed.Queued || observed.Error != "" || eventErr != nil {
		proof["checkpoint_error"], proof["checkpoint_output"] = fmt.Sprint(checkpointErr), string(output)
		proof["event_error"] = fmt.Sprint(eventErr)
		boundedLossSave(t, "timer-admission-signal-failed.json", proof)
		t.Fatal("caught signal at timer admission must settle before capture", checkpointErr, observed, eventErr)
	}
	if events["frozen"] != 0 {
		t.Fatal("successful checkpoint did not release the source", events)
	}
	var admission struct {
		Version     int    `json:"version"`
		Token       string `json:"token"`
		SourcePID   int    `json:"source_pid"`
		SourceStart string `json:"source_start"`
		Attempts    int    `json:"attempts"`
		Passed      bool   `json:"passed"`
		First       *struct {
			TID     int    `json:"tid"`
			Pending uint64 `json:"pending"`
		} `json:"first"`
	}
	relative, err := checkpointPath(id, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	path := status.Lifecycle.Root + "/" + relative + "/guard/timer-admission.json"
	data, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(data, &admission) != nil || !admission.Passed ||
		admission.Version != 1 || admission.Token != snapshot || admission.SourcePID != view.Runtime.PID ||
		admission.SourceStart != birth || admission.Attempts < 2 || admission.Attempts > 32 ||
		admission.First == nil || admission.First.TID != view.Runtime.PID || admission.First.Pending&(1<<9) == 0 {
		t.Fatal("checkpoint did not prove bounded settlement of this exact caught signal", admission, err)
	}
	proof["admission"] = admission
	assertFinished := func(view View) []byte {
		t.Helper()
		for deadline := time.Now().Add(2 * time.Second); ; {
			data, err := os.ReadFile(fmt.Sprintf("/proc/%d/root/tmp/timer-finished", view.Runtime.PID))
			if err == nil && len(data) == 16 {
				memory, err := os.Open(fmt.Sprintf("/proc/%d/mem", view.Runtime.PID))
				if err != nil {
					t.Fatal(err)
				}
				counter := make([]byte, 8)
				_, err = memory.ReadAt(counter, counterAddress)
				memory.Close()
				if err != nil || binary.LittleEndian.Uint64(counter) != 1 {
					t.Fatal("actual handler must execute exactly once", err, counter)
				}
				return data
			}
			if time.Now().After(deadline) {
				t.Fatal("source/fork lost its original raw EINTR or signal/ABI assertions", err)
			}
			time.Sleep(time.Millisecond)
		}
	}
	finished := assertFinished(view)
	boundedLossAction(t, "checkpoint", "--snapshot-id", snapshot, id)
	if !bytes.Equal(finished, assertFinished(view)) {
		t.Fatal("exact checkpoint replay reran source work")
	}
	branch := boundedLossView(t, "fork", "--source", id, "--snapshot-id", snapshot, id+"-branch")
	t.Cleanup(func() {
		if _, err := boundedLossCommand("--worker-run-root", "/run/factorybox", "delete", id+"-branch"); err != nil {
			t.Error("delete exact pending-signal fork", err)
		}
	})
	if !bytes.Equal(finished, assertFinished(branch)) {
		t.Fatal("fork did not inherit the exact one-time handler result")
	}
	proof["passed"], proof["checkpoint_succeeded"], proof["source_released"] = true, true, true
	proof["handler_exactly_once"], proof["exact_replay_preserved"], proof["independent_fork_preserved"] = true, true, true
	proof["raw_eintr_and_abi"] = true
	boundedLossSave(t, "timer-admission-signal.json", proof)
}
