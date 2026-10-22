//go:build linux && amd64

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

// A one-second handler makes the interruption-time remainder distinguishable
// from a remainder incorrectly computed only after that handler returns.
func nativeTimerRemainderAssembly() string {
	assembly := nativeTimerSignalAssembly()
	assembly = strings.Replace(assembly, "    mov %rsp, initial_sp(%rip)\n", `    mov $228, %eax
    mov $1, %edi
    lea sleep_started(%rip), %rsi
    syscall
    test %rax, %rax
    jne failed
    mov %rsp, initial_sp(%rip)
`, 1)
	assembly = strings.Replace(assembly, "    xor %r10d, %r10d\n", "    lea remaining(%rip), %r10\n", 1)
	assembly = strings.Replace(assembly, "    test %r10, %r10\n    jne failed\n", "    lea remaining(%rip), %rax\n    cmp %rax, %r10\n    jne failed\n", 1)
	assembly = strings.Replace(assembly, "signal_handler:\n    incq signal_count(%rip)\n    ret\n", `signal_handler:
    incq signal_count(%rip)
    mov $230, %eax
    mov $1, %edi
    xor %esi, %esi
    lea handler_delay(%rip), %rdx
    xor %r10d, %r10d
    syscall
    ret
`, 1)
	return assembly + ".section .rodata\nhandler_delay: .quad 1, 0\n.section .data\n.balign 8\nremaining: .quad 0x11111111, 0x22222222\n.section .bss\n.balign 8\nsleep_started: .skip 16\n"
}

func TestNativeTimerRemainder(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_TIMER_REMAINDER") != "1" {
		t.Skip("owned native interruption-time remainder fixture only")
	}
	status := boundedLossStatusNow(t)
	assembly := nativeTimerRemainderAssembly()
	if err := os.WriteFile("/output/timer-remainder.S", []byte(assembly), 0644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("cc", "-nostdlib", "-static", "-Wl,--build-id=none", "/output/timer-remainder.S", "-o", "/output/timer-remainder").CombinedOutput(); err != nil {
		t.Fatal("assemble remainder fixture", err, string(out))
	}
	program, err := os.ReadFile("/output/timer-remainder")
	if err != nil {
		t.Fatal(err)
	}
	nm, err := exec.Command("nm", "-n", "/output/timer-remainder").Output()
	if err != nil {
		t.Fatal(err)
	}
	var remainingAddress, startAddress int64
	for _, line := range strings.Split(string(nm), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[2] == "remaining" {
			remainingAddress, err = strconv.ParseInt(fields[0], 16, 64)
			if err != nil {
				t.Fatal(err)
			}
		}
		if len(fields) == 3 && fields[2] == "sleep_started" {
			startAddress, err = strconv.ParseInt(fields[0], 16, 64)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if remainingAddress == 0 || startAddress == 0 {
		t.Fatal("static remainder fixture has no output symbol")
	}
	proofs := map[string]any{}
	for _, mode := range []string{"uncaptured", "prepared"} {
		if !t.Run(mode, func(t *testing.T) {
			id := "timer-remainder-" + mode
			view := timerReaderFixtureFiles(t, status, id, "exec /bin/timer-remainder", false, map[string][]byte{"timer-remainder": program})
			root, err := os.Open(fmt.Sprintf("/proc/%d/root", view.Runtime.PID))
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			for deadline := time.Now().Add(time.Second); ; {
				data, err := os.ReadFile(fmt.Sprintf("/proc/%d/syscall", view.Runtime.PID))
				fields := strings.Fields(string(data))
				if err == nil && len(fields) > 4 && fields[0] == "230" && fields[1] == "0x1" && fields[2] == "0x0" && fields[4] != "0x0" {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("remainder task never entered its original wait", string(data), err)
				}
				time.Sleep(time.Millisecond)
			}
			// The ordinary control must not be interrupted just to observe it.
			// The task records its own monotonic time immediately before the
			// original twelve-second sleep and every ABI setup instruction.
			initialMemory, err := os.Open(fmt.Sprintf("/proc/%d/mem", view.Runtime.PID))
			if err != nil {
				t.Fatal(err)
			}
			startBytes := make([]byte, 16)
			_, err = initialMemory.ReadAt(startBytes, startAddress)
			initialMemory.Close()
			if err != nil {
				t.Fatal(err)
			}
			startedNS := int64(binary.LittleEndian.Uint64(startBytes[:8]))*1e9 + int64(binary.LittleEndian.Uint64(startBytes[8:]))
			if startedNS <= 0 {
				t.Fatal("native task did not record its real sleep start")
			}
			if offset, err := timerOffset(view.Runtime.PID, 1); err != nil || offset != 0 {
				t.Fatal("remainder comparison requires its verified zero time offset", offset, err)
			}
			if mode == "prepared" {
				boundedLossAction(t, "checkpoint", "--snapshot-id", strings.Repeat("d", 32), id)
			}
			for deadline := time.Now().Add(time.Second); ; {
				data, err := os.ReadFile(fmt.Sprintf("/proc/%d/syscall", view.Runtime.PID))
				fields := strings.Fields(string(data))
				active := len(fields) > 2 && (mode == "uncaptured" && (fields[0] == "230" || fields[0] == "219") || mode == "prepared" && fields[0] == "271")
				if err == nil && active {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("remainder task not in its actual active wait", string(data), err)
				}
				time.Sleep(time.Millisecond)
			}
			pidfd, err := unix.PidfdOpen(view.Runtime.PID, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer unix.Close(pidfd)
			var sent unix.Timespec
			if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &sent); err != nil {
				t.Fatal(err)
			}
			if err := unix.PidfdSendSignal(pidfd, unix.SIGUSR1, nil, 0); err != nil {
				t.Fatal(err)
			}
			var finishedNS int64
			for deadline := time.Now().Add(3 * time.Second); ; {
				data, err := os.ReadFile(fmt.Sprintf("/proc/self/fd/%d/tmp/timer-finished", root.Fd()))
				if err == nil && len(data) == 16 {
					finishedNS = int64(binary.LittleEndian.Uint64(data[:8]))*1e9 + int64(binary.LittleEndian.Uint64(data[8:]))
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("remainder task failed its raw EINTR/handler/ABI contract", err)
				}
				time.Sleep(time.Millisecond)
			}
			memory, err := os.Open(fmt.Sprintf("/proc/%d/mem", view.Runtime.PID))
			if err != nil {
				t.Fatal(err)
			}
			remaining := make([]byte, 16)
			_, err = memory.ReadAt(remaining, remainingAddress)
			memory.Close()
			if err != nil {
				t.Fatal(err)
			}
			seconds, nanos := binary.LittleEndian.Uint64(remaining[:8]), binary.LittleEndian.Uint64(remaining[8:])
			if seconds > 12 || nanos >= 1e9 {
				t.Fatal("invalid kernel-compatible remainder timespec", seconds, nanos)
			}
			actual := int64(seconds)*1e9 + int64(nanos)
			expected := startedNS + int64(12*time.Second) - sent.Nano()
			row := map[string]any{"mode": mode, "expected_at_signal_ns": expected, "actual_remainder_ns": actual, "difference_ns": actual - expected, "handler_elapsed_ns": finishedNS - sent.Nano(), "native_sleep_start_ns": startedNS, "deadline_observation": "Task records CLOCK_MONOTONIC immediately before ABI setup and the original twelve-second wait. No diagnostic freeze.", "raw_eintr_and_abi": true}
			if finishedNS-sent.Nano() < int64(950*time.Millisecond) {
				t.Fatal("handler did not impose its real one-second delay", row)
			}
			if actual-expected < -int64(250*time.Millisecond) || actual-expected > int64(250*time.Millisecond) {
				boundedLossSave(t, "timer-remainder-failed.json", map[string]any{"passed": false, "completed_controls": proofs, "failure": row, "scope": "Remainder must be measured at interruption, before the real delayed handler; same 250-ms bound as the original deadline regression."})
				t.Fatal("remainder was not preserved at the original interruption time", row)
			}
			proofs[mode] = row
		}) {
			return
		}
	}
	boundedLossSave(t, "timer-remainder.json", map[string]any{"passed": true, "production_qualified": false, "uncaptured_control": true, "interruption_time_remainder": true, "raw_eintr_and_abi": true, "cases": proofs, "task_sha256": sum(program), "assembly_sha256": sum([]byte(assembly))})
}
