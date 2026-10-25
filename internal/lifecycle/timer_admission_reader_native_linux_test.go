//go:build linux && (amd64 || arm64)

package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"factorybox/internal/sysboxcapture"
	"factorybox/internal/timerstate"
	"golang.org/x/sys/unix"
)

func TestNativeTimerAdmissionReader(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_TIMER_ADMISSION_READER") != "1" {
		t.Skip("owned frozen timer admission reader regression only")
	}
	status := boundedLossStatusNow(t)
	view := timerReaderFixture(t, status, "timer-admission-reader", "exec sleep 300", false)
	engine, err := Open(status.Lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	guard, err := engine.startCaptureGuardWithExecutable(view.Record, view.Runtime.PID, os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := guard.finish(true); err != nil {
			t.Error("release only the unmodified owned timer reader source", err)
		}
	}()
	if err := guard.freeze(true); err != nil {
		t.Fatal(err)
	}
	birth, err := processBirth(view.Runtime.PID)
	if err != nil {
		t.Fatal(err)
	}
	ticks, err := strconv.ParseUint(birth, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	syscall, err := os.ReadFile(fmt.Sprintf("/proc/%d/syscall", view.Runtime.PID))
	if err != nil {
		t.Fatal(err)
	}
	read := timerstate.Read
	if runtime.GOARCH == "arm64" {
		read = timerstate.ReadWithRegisters
	}
	direct, err := read([]timerstate.Task{{PID: view.Runtime.PID, StartTicks: ticks}})
	if err != nil || len(direct.States) != 1 || !direct.States[0].RelativeTimer ||
		!direct.MapReleased || !direct.ProgramReleased {
		t.Fatal("independent frozen-source reader has no exact live timer", direct, err)
	}
	if err := guard.admitCheckpointTimers(); err != nil {
		t.Fatal(err)
	}
	data, err := captureReadFile(guard.anchor.evidence, "timer-state-plan.json", timerPlanLimit)
	var plan captureTimerPlan
	if err != nil || json.Unmarshal(data, &plan) != nil {
		t.Fatal("read exact frozen timer admission plan", err)
	}
	passed := len(plan.Targets) == 1
	if passed {
		admitted, observed := plan.Targets[0], direct.States[0]
		if admitted.Registers != nil && observed.Registers != nil {
			a, b := *admitted.Registers, *observed.Registers
			a.ObservedNS, b.ObservedNS = 0, 0
			admitted.Registers, observed.Registers = &a, &b
		}
		passed = reflect.DeepEqual(admitted, observed)
	}
	boundedLossSave(t, "timer-admission-reader.json", map[string]any{
		"passed": passed, "source_pid": view.Runtime.PID, "source_birth": birth,
		"frozen_syscall": string(syscall), "direct_observation": direct, "admission_plan": plan,
		"frozen_context": timerAdmissionFrozenContext(view.Runtime.PID, syscall),
		"scope":          "Exact frozen owned source: kernel restart-block timer must survive admission, regardless of proc syscall presentation. No preparation, CRIU, signal, thawed mutation or qualification claim.",
	})
	t.Logf("frozen syscall=%q direct_timer=%v admitted_targets=%d", syscall, direct.States[0].RelativeTimer, len(plan.Targets))
	if !passed {
		t.Fatal("owned frozen relative timer was omitted or changed by checkpoint admission")
	}
}

func TestNativeTimerAdmissionExpired(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_TIMER_ADMISSION_READER") != "1" {
		t.Skip("owned expired frozen restart admission regression only")
	}
	if runtime.GOARCH != "arm64" {
		t.Skip("native ARM64 unavailable-syscall settlement regression")
	}
	status := boundedLossStatusNow(t)
	program, _ := timerAdmissionSettledActor(t)
	view := timerReaderFixtureFiles(t, status, "timer-admission-expired",
		"exec /bin/timer-admission-settled", false, map[string][]byte{"timer-admission-settled": program})
	for deadline := time.Now().Add(time.Second); ; {
		call, err := os.ReadFile(fmt.Sprintf("/proc/%d/syscall", view.Runtime.PID))
		fields := strings.Fields(string(call))
		if err == nil && len(fields) > 4 && fields[0] == strconv.Itoa(unix.SYS_CLOCK_NANOSLEEP) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("expiry control did not enter its original native wait", string(call), err)
		}
		time.Sleep(time.Millisecond)
	}
	engine, err := Open(status.Lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	guard, err := engine.startCaptureGuardWithExecutable(view.Record, view.Runtime.PID, os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := guard.finish(true); err != nil {
			t.Error("release only the unmodified expiry fixture", err)
		}
	}()
	if err := guard.freeze(true); err != nil {
		t.Fatal(err)
	}
	time.Sleep(400 * time.Millisecond)
	criu, err := openRuntime(status.Lifecycle.Criu, status.Lifecycle.CriuSHA256)
	if err != nil {
		t.Fatal(err)
	}
	features := sysboxcapture.InspectFeatures(context.Background(), criu)
	criu.Close()
	kind, err := engine.admitCheckpointSource(context.Background(), guard, view.Record, features.RootOwnedTimeNamespaces)
	if err != nil {
		t.Fatal("complete admission did not settle an expired original wait", kind, err)
	}
	data, err := captureReadFile(guard.anchor.evidence, "timer-admission.json", timerPlanLimit)
	var admission captureTimerAdmission
	if err != nil || json.Unmarshal(data, &admission) != nil || !admission.Passed ||
		admission.ExpiredFirst == nil || admission.Attempts < 2 || admission.Attempts > 32 {
		t.Fatal("expired original wait did not settle through bounded unmodified admission", admission, err)
	}
	boundedLossSave(t, "timer-admission-expired.json", map[string]any{
		"passed": true, "admission": admission,
		"scope": "A real freezer-interrupted native timer expires while held. The actual complete production admission function must allow only the unchanged original source to settle before repeating descriptor/namespace/filesystem/timer admission; no CRIU or timer mutation occurs.",
	})
}

func timerAdmissionFrozenContext(pid int, syscall []byte) map[string]any {
	result := map[string]any{}
	stack, stackErr := os.ReadFile(fmt.Sprintf("/proc/%d/stack", pid))
	result["kernel_stack"], result["kernel_stack_error"] = string(stack), fmt.Sprint(stackErr)
	fields := strings.Fields(string(syscall))
	if len(fields) >= 3 {
		address, err := strconv.ParseUint(fields[len(fields)-1], 0, 64)
		if err == nil {
			instruction, readErr := timerMemoryRead(pid, address, 4)
			result["pc"], result["instruction"], result["instruction_error"] = address, fmt.Sprintf("%x", instruction), fmt.Sprint(readErr)
		}
	}
	return result
}

func timerAdmissionSettledActor(t *testing.T) ([]byte, uint64) {
	t.Helper()
	source := `#define _GNU_SOURCE
#include <stdint.h>
#include <poll.h>
#include <time.h>
volatile uint64_t settled;
int main(int argc, char **argv) {
 struct timespec delay = {0, 300000000};
 if (clock_nanosleep(CLOCK_MONOTONIC, 0, &delay, 0)) return 2;
 settled = 1;
 if (argc > 1) for (;;) ppoll(0, 0, 0, 0);
 for (;;) ++settled;
}
`
	binary := "/output/" + strings.ReplaceAll(t.Name(), "/", "-") + "-settled"
	c := binary + ".c"
	if err := os.WriteFile(c, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("cc", "-O2", "-static", "-no-pie", c, "-o", binary).CombinedOutput(); err != nil {
		t.Fatal("compile exact completed-restart control", err, string(out))
	}
	program, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	nm, err := exec.Command("nm", "-n", binary).Output()
	if err != nil {
		t.Fatal(err)
	}
	var settled uint64
	for _, line := range strings.Split(string(nm), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[2] == "settled" {
			settled, err = strconv.ParseUint(fields[0], 16, 64)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if settled == 0 {
		t.Fatal("completed-restart control has no owned counter")
	}
	return program, settled
}

func TestNativeTimerAdmissionNonTimers(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_TIMER_ADMISSION_READER") != "1" {
		t.Skip("owned non-timer admission controls only")
	}
	status := boundedLossStatusNow(t)
	program, settled := timerAdmissionSettledActor(t)
	proofs := map[string]any{}
	for _, mode := range []string{"compute", "completed-restart", "completed-restart-poll"} {
		t.Run(mode, func(t *testing.T) {
			command := "exec /bin/sh -c 'while :; do :; done'"
			var files map[string][]byte
			if mode != "compute" {
				command = "exec /bin/timer-admission-settled"
				if mode == "completed-restart-poll" {
					command += " poll"
				}
				files = map[string][]byte{"timer-admission-settled": program}
			}
			view := timerReaderFixtureFiles(t, status, "timer-admission-"+mode, command, false, files)
			if mode != "compute" {
				for deadline := time.Now().Add(time.Second); ; {
					call, err := os.ReadFile(fmt.Sprintf("/proc/%d/syscall", view.Runtime.PID))
					fields := strings.Fields(string(call))
					if err == nil && len(fields) > 4 && fields[0] == strconv.Itoa(unix.SYS_CLOCK_NANOSLEEP) {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("control never entered its initial real timer", string(call), err)
					}
					time.Sleep(time.Millisecond)
				}
				if err := captureNativeFreeze(view.Record, true); err != nil {
					t.Fatal(err)
				}
				if err := captureNativeFreeze(view.Record, false); err != nil {
					t.Fatal(err)
				}
				for deadline := time.Now().Add(time.Second); ; {
					value, err := timerMemoryRead(view.Runtime.PID, settled, 8)
					if err == nil && len(value) == 8 && value[0]|value[1]|value[2]|value[3]|value[4]|value[5]|value[6]|value[7] != 0 {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("interrupted native timer did not complete before computation", err)
					}
					time.Sleep(time.Millisecond)
				}
			}
			engine, err := Open(status.Lifecycle)
			if err != nil {
				t.Fatal(err)
			}
			defer engine.Close()
			guard, err := engine.startCaptureGuardWithExecutable(view.Record, view.Runtime.PID, os.Args[0])
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := guard.finish(true); err != nil {
					t.Error("release only this unmodified computation control", err)
				}
			}()
			if err := guard.freeze(true); err != nil {
				t.Fatal(err)
			}
			call, err := os.ReadFile(fmt.Sprintf("/proc/%d/syscall", view.Runtime.PID))
			if err != nil {
				t.Fatal(err)
			}
			if err := guard.admitCheckpointTimers(); err != nil {
				t.Fatal(err)
			}
			data, err := captureReadFile(guard.anchor.evidence, "timer-state-plan.json", timerPlanLimit)
			var plan captureTimerPlan
			if err != nil || json.Unmarshal(data, &plan) != nil {
				t.Fatal("read non-timer admission plan", err)
			}
			proofs[mode] = map[string]any{"frozen_syscall": string(call), "frozen_context": timerAdmissionFrozenContext(view.Runtime.PID, call), "plan": plan, "no_targets": len(plan.Targets) == 0}
			if len(plan.Targets) != 0 {
				t.Fatal("computation or an already completed restart was admitted as an active timer", mode, string(call), plan)
			}
		})
	}
	boundedLossSave(t, "timer-admission-non-timers.json", map[string]any{
		"passed": !t.Failed(), "cases": proofs, "scope": "Owned frozen computation, including completion of a genuinely freezer-interrupted timer. A stale restart union must not authorize timer mutation.",
	})
}
