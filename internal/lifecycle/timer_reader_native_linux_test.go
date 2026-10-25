//go:build linux && (amd64 || arm64)

package lifecycle

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"factorybox/internal/timerstate"
)

func TestNativeTimerReaderOwnInit(t *testing.T) {
	if os.Getenv("FACTORYBOX_TIMER_READER_INIT") != "1" {
		t.Skip("explicit owned PID namespace control only")
	}
	if os.Getpid() != 1 {
		t.Fatal("reader namespace control is not its own PID 1")
	}
	birth, err := processBirth(1)
	if err != nil {
		t.Fatal(err)
	}
	ticks, err := strconv.ParseUint(birth, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	result, err := timerstate.Read([]timerstate.Task{{PID: 1, StartTicks: ticks}})
	if err != nil || len(result.States) != 1 || result.States[0].RelativeTimer || !result.MapReleased || !result.ProgramReleased {
		t.Fatal("Go reader adopted another namespace's PID 1", result, err)
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("owned-init-result=" + string(data))
}

func TestNativeTimerReader(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_TIMER_READER") != "1" {
		t.Skip("owned read-only Go timer component fixture only")
	}
	status := boundedLossStatusNow(t)
	sleeper := timerReaderFixture(t, status, "reader-sleep", "exec /bin/sh -c 'sleep 300 & exec sleep 300'", false)
	actor := timerReaderFixture(t, status, "reader-actor", "exec /bin/env FACTORYBOX_CAPTURE_ACTOR=1 /bin/capture-actor -test.run=^TestCaptureActor$ -test.timeout=0", true)
	var before captureActorState
	var err error
	for deadline := time.Now().Add(5 * time.Second); ; {
		before, err = readCaptureActor(actor.Runtime.PID, "get")
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, view := range []View{sleeper, actor} {
		if err := captureNativeFreeze(view.Record, true); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := captureNativeFreeze(view.Record, false); err != nil {
				t.Error("release exact reader source freeze", err)
			}
		})
	}
	var tasks []timerstate.Task
	groups := map[string]int{}
	for _, view := range []View{sleeper, actor} {
		fd, err := captureGuardOpen(cgroupRoot + "/" + view.Record.RuntimeID)
		if err != nil {
			t.Fatal(err)
		}
		inventory, err := captureGuardThreads(captureGuardConfig{Source: view.Record}, captureGuardAnchors{group: fd})
		fd.Close()
		if err != nil {
			t.Fatal(err)
		}
		groups[view.Record.ID] = len(inventory)
		for pid, birth := range inventory {
			ticks, err := strconv.ParseUint(birth, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			tasks = append(tasks, timerstate.Task{PID: pid, StartTicks: ticks})
		}
	}
	if groups["reader-sleep"] != 2 || groups["reader-actor"] < 2 {
		t.Fatal("reader lacks process/non-leader thread controls", groups)
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].PID < tasks[j].PID })
	started := time.Now()
	result, err := timerstate.Read(tasks)
	elapsed := time.Since(started)
	if err != nil || len(result.States) != len(tasks) || !result.MapReleased || !result.ProgramReleased {
		t.Fatal("bulk Go timer state", result, err)
	}
	var timers, others int
	for _, state := range result.States {
		if state.RelativeTimer {
			timers++
			if state.Clock != 1 || state.ExpiresNS <= 0 || state.TimespecType < 0 || state.TimespecType > 1 {
				t.Fatal("invalid owned timer state", state)
			}
		} else {
			others++
			if state.Clock != 0 || state.Remainder != 0 || state.ExpiresNS != 0 {
				t.Fatal("non-timer leaked uninterpreted restart union", state)
			}
		}
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", state.PID))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(data, []byte("TracerPid:\t0\n")) {
			t.Fatal("read-only reader traced its task", state.PID)
		}
	}
	if timers < 2 || others == 0 {
		t.Fatal("reader missed timer or non-timer controls", timers, others)
	}
	for _, pid := range []int{sleeper.Runtime.PID, actor.Runtime.PID} {
		output, err := exec.Command("python3", "/timer-probe.py", strconv.Itoa(pid)).CombinedOutput()
		var independent struct {
			Names     []string `json:"restart_function_names"`
			Clock     int32    `json:"clock_id"`
			Type      int32    `json:"timespec_type"`
			Remainder string   `json:"remaining_user_pointer"`
			Expires   int64    `json:"expires_ns"`
		}
		if err != nil || json.Unmarshal(output, &independent) != nil {
			t.Fatal("independent frozen timer read", err, string(output))
		}
		for _, state := range result.States {
			if state.PID != pid {
				continue
			}
			isTimer := len(independent.Names) == 1 && independent.Names[0] == "hrtimer_nanosleep_restart"
			if state.RelativeTimer != isTimer {
				t.Fatal("Go/Python restart function mismatch", state, independent)
			}
			if isTimer && (state.Clock != independent.Clock || state.TimespecType != independent.Type || state.ExpiresNS != independent.Expires || fmt.Sprintf("0x%x", state.Remainder) != independent.Remainder) {
				t.Fatal("Go/Python frozen timer mismatch", state, independent)
			}
		}
	}
	wrong := append([]timerstate.Task(nil), tasks...)
	wrong[0].StartTicks++
	refused, err := timerstate.Read(wrong)
	if err == nil || !strings.Contains(err.Error(), "birth inventory") || !refused.ProgramReleased || !refused.MapReleased {
		t.Fatal("wrong-birth reader failed to refuse and release all objects", refused, err)
	}
	if _, err := timerstate.Read([]timerstate.Task{tasks[0], tasks[0]}); err == nil {
		t.Fatal("duplicate owned PID was accepted")
	}
	control := exec.Command("unshare", "--pid", "--fork", "--mount-proc", "/probe.test", "-test.v", "-test.run=^TestNativeTimerReaderOwnInit$")
	control.Env = append(os.Environ(), "FACTORYBOX_TIMER_READER_INIT=1")
	output, err := control.CombinedOutput()
	if err := os.WriteFile("/output/reader-own-init.txt", output, 0644); err != nil {
		t.Fatal(err)
	}
	if err != nil || !bytes.Contains(output, []byte("--- PASS: TestNativeTimerReaderOwnInit")) {
		t.Fatal("Go reader PID namespace separation", err, string(output))
	}
	for _, view := range []View{sleeper, actor} {
		if err := captureNativeFreeze(view.Record, false); err != nil {
			t.Fatal(err)
		}
	}
	after, err := readCaptureActor(actor.Runtime.PID, "get")
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("read-only reader changed useful actor RAM", err)
	}
	for _, id := range []string{"reader-sleep", "reader-actor"} {
		view := boundedLossView(t, "state", id)
		if view.Runtime == nil || view.Runtime.Status != "running" {
			t.Fatal("read-only reader stopped its source", id)
		}
	}
	boundedLossSave(t, "timer-reader.json", map[string]any{"schema": 1, "passed": true, "production_qualified": false,
		"observation": result, "non_leader_threads": true, "independent_python_agreement": true,
		"wrong_birth_refused_and_released": true, "duplicate_pid_refused": true, "pid_namespace_separated": true,
		"task_memory_unchanged": true, "sources_running": true, "first_read_nanoseconds": elapsed.Nanoseconds()})
}
