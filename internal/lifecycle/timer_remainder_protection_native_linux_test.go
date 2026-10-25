//go:build linux && (amd64 || arm64)

package lifecycle

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func nativeTimerRemainderProtection(t *testing.T, activate func(*testing.T, boundedLossStatus, View, bool) map[string]any) {
	if os.Getenv("FACTORYBOX_NATIVE_TIMER_REMAINDER_PROTECTION") != "1" {
		t.Skip("owned native remainder copy-order fixture only")
	}
	status := boundedLossStatusNow(t)
	assembly := nativeTimerRemainderProtectionAssembly()
	if err := os.WriteFile("/output/timer-remainder-protection.S", []byte(assembly), 0644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("cc", "-nostdlib", "-static", "-Wl,--build-id=none", "/output/timer-remainder-protection.S", "-o", "/output/timer-remainder-protection").CombinedOutput(); err != nil {
		t.Fatal("assemble protected-remainder fixture", err, string(out))
	}
	program, err := os.ReadFile("/output/timer-remainder-protection")
	if err != nil {
		t.Fatal(err)
	}
	nm, err := exec.Command("nm", "-n", "/output/timer-remainder-protection").Output()
	if err != nil {
		t.Fatal(err)
	}
	symbols := map[string]uint64{}
	for _, line := range strings.Split(string(nm), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 {
			value, err := strconv.ParseUint(fields[0], 16, 64)
			if err != nil {
				t.Fatal(err)
			}
			symbols[fields[2]] = value
		}
	}
	if symbols["remaining"] == 0 || symbols["remaining"]%4096 != 0 || symbols["remaining"]/4096 == symbols["actual_result"]/4096 || symbols["remaining"]/4096 == symbols["signal_count"]/4096 {
		t.Fatal("remainder protection could affect ordinary task data", symbols)
	}
	proofs := map[string]any{}
	activations := map[string]any{}
	kernelPrototype := activate != nil
	modes := []string{"uncaptured", "prepared", "fork", "resumed"}
	if kernelPrototype {
		modes = []string{"uncaptured", "frozen-control", "prepared", "fork", "resumed", "queued"}
	}
	for _, mode := range modes {
		if !t.Run(mode, func(t *testing.T) {
			id := "timer-rem-protection-" + mode
			view := timerReaderFixtureFiles(t, status, id, "exec /bin/timer-remainder-protection", false, map[string][]byte{"timer-remainder-protection": program})
			waitFor := func(prepared bool) {
				for deadline := time.Now().Add(time.Second); ; {
					data, err := os.ReadFile(fmt.Sprintf("/proc/%d/syscall", view.Runtime.PID))
					fields := strings.Fields(string(data))
					active := len(fields) > 4 && (!prepared && fields[0] == strconv.Itoa(unix.SYS_CLOCK_NANOSLEEP) && fields[1] == "0x1" && fields[2] == "0x0" || prepared && nativeTimerActivatedWait(view.Runtime.PID, fields))
					if err == nil && active {
						return
					}
					if time.Now().After(deadline) {
						t.Fatal("protection task never entered its actual wait", prepared, string(data), err)
					}
					time.Sleep(time.Millisecond)
				}
			}
			waitFor(false)
			if mode != "uncaptured" && mode != "frozen-control" {
				snapshot := strings.Repeat("e", 32)
				if mode == "resumed" {
					boundedLossAction(t, "pause", "--snapshot-id", snapshot, id)
					view = boundedLossView(t, "resume", "--snapshot-id", snapshot, id)
				} else {
					boundedLossAction(t, "checkpoint", "--snapshot-id", snapshot, id)
					if mode == "fork" {
						target := id + "-target"
						view = boundedLossView(t, "fork", "--source", id, "--snapshot-id", snapshot, target)
						t.Cleanup(func() {
							if _, err := boundedLossCommand("--worker-run-root", "/run/factorybox", "delete", target); err != nil {
								t.Error("delete exact protection fork", target, err)
							}
						})
					}
				}
				waitFor(true)
			}
			root, err := os.Open(fmt.Sprintf("/proc/%d/root", view.Runtime.PID))
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			offset, err := timerOffset(view.Runtime.PID, 1)
			if err != nil {
				t.Fatal("protection deadline comparison requires the actual clock namespace offset", offset, err)
			}
			initialMemory, err := os.Open(fmt.Sprintf("/proc/%d/mem", view.Runtime.PID))
			if err != nil {
				t.Fatal(err)
			}
			startBytes := make([]byte, 16)
			_, err = initialMemory.ReadAt(startBytes, int64(symbols["sleep_started"]))
			initialMemory.Close()
			if err != nil {
				t.Fatal(err)
			}
			startedNS := int64(binary.LittleEndian.Uint64(startBytes[:8]))*1e9 + int64(binary.LittleEndian.Uint64(startBytes[8:]))
			if startedNS <= 0 {
				t.Fatal("protection fixture did not record its actual original wait start")
			}
			var sentNS int64
			var heldRemaining int64
			controlFrozen := false
			defer func() {
				if controlFrozen {
					if err := captureNativeFreeze(view.Record, false); err != nil {
						t.Error("release exact frozen native control", err)
					}
				}
			}()
			if mode == "frozen-control" {
				var beforeFreeze unix.Timespec
				if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &beforeFreeze); err != nil {
					t.Fatal(err)
				}
				if err := captureNativeFreeze(view.Record, true); err != nil {
					t.Fatal(err)
				}
				controlFrozen = true
				remaining, err := timerMemoryRead(view.Runtime.PID, symbols["remaining"], 16)
				if err != nil {
					t.Fatal(err)
				}
				sec, nsec := binary.LittleEndian.Uint64(remaining[:8]), binary.LittleEndian.Uint64(remaining[8:])
				if sec > 12 || nsec >= 1e9 {
					t.Fatal("uncaptured frozen control did not receive an actual kernel remainder", sec, nsec)
				}
				heldRemaining = int64(sec)*1e9 + int64(nsec)
				expected := startedNS + int64(12*time.Second) - (beforeFreeze.Nano() + offset)
				if delta := heldRemaining - expected; delta < -int64(250*time.Millisecond) || delta > int64(250*time.Millisecond) {
					t.Fatal("uncaptured frozen control remainder differs from its actual interruption", heldRemaining, expected)
				}
				// Distinguish the native interruption time from the later signal
				// queue time by more than the unchanged 250-ms deadline bound.
				time.Sleep(600 * time.Millisecond)
			}
			if kernelPrototype && mode != "uncaptured" && mode != "frozen-control" {
				activation := activate(t, status, view, mode == "queued")
				activations[mode] = activation
				if mode == "queued" {
					sentNS = activation["signal_sent_ns"].(int64)
					heldRemaining = int64(activation["interruption_remaining_ns"].(float64))
				}
			}
			pidfd, err := unix.PidfdOpen(view.Runtime.PID, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer unix.Close(pidfd)
			if sentNS == 0 {
				var sent unix.Timespec
				if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &sent); err != nil {
					t.Fatal(err)
				}
				sentNS = sent.Nano()
				if err := unix.PidfdSendSignal(pidfd, unix.SIGUSR1, nil, 0); err != nil {
					t.Fatal(err)
				}
			}
			if controlFrozen {
				if err := captureNativeFreeze(view.Record, false); err != nil {
					t.Fatal(err)
				}
				controlFrozen = false
			}
			for deadline := time.Now().Add(3 * time.Second); ; {
				base := fmt.Sprintf("/proc/self/fd/%d/tmp/", root.Fd())
				if data, err := os.ReadFile(base + "timer-failed"); err == nil && len(data) == 64 {
					row := map[string]any{"mode": mode, "actual_raw_result": int64(binary.LittleEndian.Uint64(data[8:16])), "expected_raw_result": -4, "failure_words": fmt.Sprintf("%x", data), "completed_controls": proofs}
					boundedLossSave(t, "timer-remainder-protection-failed.json", map[string]any{"passed": false, "failure": row})
					t.Fatal("remainder protection changed the original interrupted syscall result", row)
				}
				if data, err := os.ReadFile(base + "timer-finished"); err == nil && len(data) == 16 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("protection task did not pass its EINTR/ABI checks")
				}
				time.Sleep(time.Millisecond)
			}
			maps, err := os.ReadFile(fmt.Sprintf("/proc/%d/maps", view.Runtime.PID))
			if err != nil {
				t.Fatal(err)
			}
			readOnly := false
			for _, line := range strings.Split(string(maps), "\n") {
				fields := strings.Fields(line)
				if len(fields) < 2 {
					continue
				}
				bounds := strings.Split(fields[0], "-")
				if len(bounds) != 2 {
					t.Fatal("invalid mapping")
				}
				start, err := strconv.ParseUint(bounds[0], 16, 64)
				if err != nil {
					t.Fatal(err)
				}
				end, err := strconv.ParseUint(bounds[1], 16, 64)
				if err != nil {
					t.Fatal(err)
				}
				if start <= symbols["remaining"] && symbols["remaining"]+16 <= end {
					readOnly = strings.HasPrefix(fields[1], "r") && !strings.Contains(fields[1], "w")
				}
			}
			if !readOnly {
				t.Fatal("handler did not actually protect its remainder page")
			}
			memory, err := os.Open(fmt.Sprintf("/proc/%d/mem", view.Runtime.PID))
			if err != nil {
				t.Fatal(err)
			}
			remaining := make([]byte, 16)
			_, err = memory.ReadAt(remaining, int64(symbols["remaining"]))
			memory.Close()
			if err != nil {
				t.Fatal(err)
			}
			sec, nsec := binary.LittleEndian.Uint64(remaining[:8]), binary.LittleEndian.Uint64(remaining[8:])
			if sec > 12 || nsec >= 1e9 {
				t.Fatal("native remainder was not written before protection", sec, nsec)
			}
			// The saved start and deadline belong to the application's clock.
			// CRIU may restore a different namespace offset; signal delivery is
			// timestamped by this observer's unshifted monotonic clock.
			signalExpected := startedNS + int64(12*time.Second) - (sentNS + offset)
			expected := signalExpected
			actual := int64(sec)*1e9 + int64(nsec)
			if heldRemaining != 0 {
				// The uncaptured frozen control above must establish this native
				// behavior before the queued activation case can rely on it.
				if actual != heldRemaining {
					t.Fatal("signal queued during a native hold changed its already written kernel remainder", mode, actual, heldRemaining)
				}
				expected = heldRemaining
			}
			if actual-expected < -int64(250*time.Millisecond) || actual-expected > int64(250*time.Millisecond) {
				t.Fatal("protected remainder did not retain the original interruption-time deadline", mode, actual, expected)
			}
			proofs[mode] = map[string]any{"raw_eintr_and_abi": true, "remainder_actually_read_only": true, "remainder_seconds": sec, "remainder_nanoseconds": nsec, "expected_at_native_interruption_ns": expected, "expected_at_signal_ns": signalExpected, "held_remaining_ns": heldRemaining, "clock_namespace_offset_ns": offset, "difference_ns": actual - expected, "difference_from_signal_time_ns": actual - signalExpected}
		}) {
			return
		}
	}
	if kernelPrototype {
		boundedLossSave(t, "kernel-activation-prototype.json", map[string]any{"passed": true, "production_qualified": false, "native_frozen_remainder_control": true, "cases": activations, "scope": "Test-only activation after delivered source checkpoint, fork or resume, including signal queued while frozen after native kernel wait activation. The uncaptured frozen control establishes interruption-time remainder behavior before that queued case. Restore already exposed the ppoll continuation before test activation; no production pre-exposure integration, multi-thread or interrupted mutation repair."})
	}
	boundedLossSave(t, "timer-remainder-protection.json", map[string]any{"passed": true, "production_qualified": false, "uncaptured_control": true, "copy_before_handler_protection": true, "raw_eintr_and_abi": true, "cases": proofs, "task_sha256": sum(program), "assembly_sha256": sum([]byte(assembly))})
}
