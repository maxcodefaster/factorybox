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
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// Derive a separate signal fixture without changing the original required
// timer assembly. It requires one handler and raw -EINTR, retaining every
// register, stack, flags and read-only request assertion from that task.
func nativeTimerSignalAssembly() string {
	assembly := strings.Replace(timerNativeAssembly, "_start:\n", `_start:
    mov $13, %eax
    mov $10, %edi
    lea signal_action(%rip), %rsi
    xor %edx, %edx
    mov $8, %r10d
    syscall
    test %rax, %rax
    jne failed
`, 1)
	assembly = strings.Replace(assembly, "cmpq $0, actual_result(%rip)", "cmpq $-4, actual_result(%rip)", 1)
	assembly = strings.Replace(assembly, "    mov $257, %eax\n", "    cmpq $1, signal_count(%rip)\n    jne failed\n    mov $257, %eax\n", 1)
	// Keep the successful task alive after its existing assertions, marker
	// write and fsync. Otherwise an immediate correct EINTR can exit before
	// the delivered resume command publishes its live-process receipt.
	assembly = strings.Replace(assembly, "    xor %edi, %edi\n    jmp exit_task\n", "signal_success_hold:\n    mov $34, %eax\n    syscall\n    jmp signal_success_hold\n", 1)
	assembly = strings.Replace(assembly, ".section .rodata\n", `signal_handler:
    incq signal_count(%rip)
    ret
signal_restorer:
    mov $15, %eax
    syscall
.section .rodata
signal_action: .quad signal_handler, 0x04000000, signal_restorer, 0
`, 1)
	return assembly + ".section .bss\n.balign 8\nsignal_count: .skip 8\n"
}

func timerSignalStoppedController(t *testing.T, worker captureWorkerIntent) int {
	t.Helper()
	birth, err := processBirth(worker.PID)
	if err != nil || birth != worker.Start {
		t.Fatal("capture worker birth changed", err)
	}
	fd, err := unix.PidfdOpen(worker.PID, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { unix.PidfdSendSignal(fd, unix.SIGCONT, nil, 0); unix.Close(fd) })
	if err := unix.PidfdSendSignal(fd, unix.SIGSTOP, nil, 0); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(time.Second); ; {
		tasks, err := os.ReadDir(fmt.Sprintf("/proc/%d/task", worker.PID))
		stopped := err == nil && len(tasks) != 0
		for _, task := range tasks {
			data, err := os.ReadFile(fmt.Sprintf("/proc/%d/task/%s/status", worker.PID, task.Name()))
			stopped = stopped && err == nil && bytes.Contains(data, []byte("\nState:\tT "))
		}
		if stopped {
			return fd
		}
		if time.Now().After(deadline) {
			t.Fatal("exact capture worker did not stop before signal injection", err)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestNativeTimerSignalBoundary(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_TIMER_SIGNAL") != "1" {
		t.Skip("owned native timer signal boundary fixture only")
	}
	status := boundedLossStatusNow(t)
	assembly := nativeTimerSignalAssembly()
	if err := os.WriteFile("/output/timer-signal.S", []byte(assembly), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("cc", "-nostdlib", "-static", "-Wl,--build-id=none", "/output/timer-signal.S", "-o", "/output/timer-signal").CombinedOutput()
	if err != nil {
		t.Fatal("build direct signal fixture", err, string(out))
	}
	program, err := os.ReadFile("/output/timer-signal")
	if err != nil {
		t.Fatal(err)
	}
	nm, err := exec.Command("nm", "-n", "/output/timer-signal").Output()
	if err != nil {
		t.Fatal(err)
	}
	var countAddress int64
	for _, row := range strings.Split(string(nm), "\n") {
		fields := strings.Fields(row)
		if len(fields) == 3 && fields[2] == "signal_count" {
			countAddress, err = strconv.ParseInt(fields[0], 16, 64)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if countAddress == 0 {
		t.Fatal("static signal fixture has no counter symbol")
	}
	proofs := map[string]any{}
	for _, mode := range []string{"uncaptured", "prepared-wait", "prepared-boundary", "recaptured-boundary"} {
		if !t.Run(mode, func(t *testing.T) {
			id := "timer-signal-" + mode
			view := timerReaderFixtureFiles(t, status, id, "exec /bin/timer-signal", false, map[string][]byte{"timer-signal": program})
			root, err := os.Open(fmt.Sprintf("/proc/%d/root", view.Runtime.PID))
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			for deadline := time.Now().Add(time.Second); ; {
				data, err := os.ReadFile(fmt.Sprintf("/proc/%d/syscall", view.Runtime.PID))
				fields := strings.Fields(string(data))
				if err == nil && len(fields) > 4 && fields[0] == "230" && fields[1] == "0x1" && fields[2] == "0x0" && fields[4] == "0x0" {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("signal task never entered its original relative wait", string(data), err)
				}
				time.Sleep(time.Millisecond)
			}
			pidfd, err := unix.PidfdOpen(view.Runtime.PID, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer unix.Close(pidfd)
			var beforeSignal map[string]any
			var pollingCost map[string]any
			if mode == "prepared-wait" || mode == "recaptured-boundary" {
				boundedLossAction(t, "checkpoint", "--snapshot-id", strings.Repeat("a", 32), id)
				for deadline := time.Now().Add(time.Second); ; {
					data, err := os.ReadFile(fmt.Sprintf("/proc/%d/syscall", view.Runtime.PID))
					fields := strings.Fields(string(data))
					// Support the old absolute-wait control and the new atomic-mask
					// wait. Both must satisfy the same raw EINTR/ABI assertions.
					active := nativeTimerActivatedWait(view.Runtime.PID, fields)
					if err == nil && active {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("prepared task did not enter its active atomic-mask continuation", string(data), err)
					}
					time.Sleep(time.Millisecond)
				}
				readCounters := func(name string) map[string]uint64 {
					data, err := os.ReadFile(guardGroupPath(view.Record) + "/" + name)
					if err != nil {
						t.Fatal(err)
					}
					values, err := parseCgroupCounters(string(data))
					if err != nil {
						t.Fatal(err)
					}
					return values
				}
				if mode == "prepared-wait" {
					cpuBefore, memoryBefore, started := readCounters("cpu.stat"), readCounters("memory.stat"), time.Now()
					time.Sleep(2 * time.Second)
					cpuAfter, memoryAfter := readCounters("cpu.stat"), readCounters("memory.stat")
					pollingCost = map[string]any{"observed_ns": time.Since(started).Nanoseconds(), "cpu_before": cpuBefore, "cpu_after": cpuAfter, "memory_before": memoryBefore, "memory_after": memoryAfter,
						"scope": "One owned prepared sleeping task, two seconds. Raw cgroup CPU and retained-memory observations, not a percentile or comparison claim."}
				}
			}
			if mode == "recaptured-boundary" {
				snapshot := strings.Repeat("c", 32)
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				command := exec.CommandContext(ctx, "/opt/factorybox/factorybox", "--worker-run-root", "/run/factorybox", "pause", "--snapshot-id", snapshot, id)
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
				}()
				guardDir := status.Lifecycle.Root + "/objects/" + id + "/captures/" + snapshot + "/guard"
				for deadline := time.Now().Add(5 * time.Second); ; {
					if _, err := os.Stat(guardDir + "/guard-capture-entered.json"); err == nil {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("second capture never entered its exact frozen transaction")
					}
					time.Sleep(time.Millisecond)
				}
				birth, err := processBirth(command.Process.Pid)
				if err != nil {
					t.Fatal(err)
				}
				controllerFD := timerSignalStoppedController(t, captureWorkerIntent{PID: command.Process.Pid, Start: birth, SourcePID: view.Runtime.PID, Token: snapshot})
				var captureFDs []int
				// A fast controller may already have started _capture. Stop only
				// that exact child too, and require it has no CRIU/copy child.
				tasks, err := os.ReadDir(fmt.Sprintf("/proc/%d/task", command.Process.Pid))
				if err != nil {
					t.Fatal(err)
				}
				for _, task := range tasks {
					data, err := os.ReadFile(fmt.Sprintf("/proc/%d/task/%s/children", command.Process.Pid, task.Name()))
					if err != nil {
						t.Fatal(err)
					}
					for _, child := range strings.Fields(string(data)) {
						pid, err := strconv.Atoi(child)
						if err != nil {
							t.Fatal(err)
						}
						args, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
						if os.IsNotExist(err) {
							continue
						}
						if err != nil {
							t.Fatal(err)
						}
						if !bytes.Contains(args, []byte("\x00_capture\x00")) {
							continue
						}
						birth, err := processBirth(pid)
						if err != nil {
							t.Fatal(err)
						}
						captureFDs = append(captureFDs, timerSignalStoppedController(t, captureWorkerIntent{PID: pid, Start: birth, SourcePID: view.Runtime.PID, Token: snapshot}))
						threads, err := os.ReadDir(fmt.Sprintf("/proc/%d/task", pid))
						if err != nil {
							t.Fatal(err)
						}
						for _, thread := range threads {
							children, err := os.ReadFile(fmt.Sprintf("/proc/%d/task/%s/children", pid, thread.Name()))
							if err != nil {
								t.Fatal(err)
							}
							if len(strings.Fields(string(children))) != 0 {
								t.Fatal("missed existing-wait boundary; capture already started a child", string(children))
							}
						}
					}
				}
				data, err := os.ReadFile(fmt.Sprintf("/proc/%d/syscall", view.Runtime.PID))
				fields := strings.Fields(string(data))
				if err != nil || !nativeTimerActivatedWait(view.Runtime.PID, fields) {
					t.Fatal("second capture is not an actual frozen native timer continuation", string(data), err)
				}
				groupData, err := os.ReadFile(guardGroupPath(view.Record) + "/cgroup.events")
				group, parseErr := parseCgroupCounters(string(groupData))
				trace, traceErr := os.ReadFile(fmt.Sprintf("/proc/%d/status", view.Runtime.PID))
				if err != nil || parseErr != nil || group["frozen"] != 1 || traceErr != nil || !bytes.Contains(trace, []byte("TracerPid:\t0\n")) {
					t.Fatal("second signal boundary lacks its exact untraced hold", group, err, parseErr, traceErr)
				}
				if err := unix.PidfdSendSignal(pidfd, unix.SIGUSR1, nil, 0); err != nil {
					t.Fatal(err)
				}
				pending, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", view.Runtime.PID))
				if err != nil {
					t.Fatal(err)
				}
				beforeSignal = map[string]any{"source_frozen": true, "existing_native_wait": true, "source_status_after_queue": string(pending)}
				for _, fd := range captureFDs {
					if err := unix.PidfdSendSignal(fd, unix.SIGCONT, nil, 0); err != nil {
						t.Fatal(err)
					}
				}
				if err := unix.PidfdSendSignal(controllerFD, unix.SIGCONT, nil, 0); err != nil {
					t.Fatal(err)
				}
				err = command.Wait()
				waited = true
				if err != nil {
					t.Fatal("pause after existing-wait signal", err, stderr.String())
				}
				view = boundedLossView(t, "resume", "--snapshot-id", snapshot, id)
				root.Close()
				root, err = os.Open(fmt.Sprintf("/proc/%d/root", view.Runtime.PID))
				if err != nil {
					t.Fatal("pin recaptured signal task root", err)
				}
				defer root.Close()
			}
			if mode == "prepared-boundary" {
				snapshot := strings.Repeat("b", 32)
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				command := exec.CommandContext(ctx, "/opt/factorybox/factorybox", "--worker-run-root", "/run/factorybox", "pause", "--snapshot-id", snapshot, id)
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
				}()
				guardDir := status.Lifecycle.Root + "/objects/" + id + "/captures/" + snapshot + "/guard"
				for deadline := time.Now().Add(5 * time.Second); ; {
					data, err := os.ReadFile(guardDir + "/timer-prepare-complete.json")
					var complete struct {
						Token  string `json:"token"`
						Passed bool   `json:"passed"`
					}
					if err == nil && json.Unmarshal(data, &complete) == nil && complete.Token == snapshot && complete.Passed {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("did not observe the actual completed production timer preparer", err)
					}
					time.Sleep(time.Millisecond)
				}
				var worker captureWorkerIntent
				data, err := os.ReadFile(guardDir + "/capture-worker-intent.json")
				if err != nil || json.Unmarshal(data, &worker) != nil || worker.Token != snapshot || worker.SourcePID != view.Runtime.PID {
					t.Fatal("exact capture worker intent changed", err)
				}
				workerFD := timerSignalStoppedController(t, worker)
				var ready timerMutatorReady
				data, err = os.ReadFile(guardDir + "/timer-mutator-ready.json")
				if err != nil || json.Unmarshal(data, &ready) != nil || ready.Token != snapshot {
					t.Fatal("original mutator identity unavailable", err)
				}
				// No CRIU or other capture child may have started. The stopped
				// capture worker cannot launch one while the signal is queued.
				tasks, err := os.ReadDir(fmt.Sprintf("/proc/%d/task", worker.PID))
				if err != nil {
					t.Fatal(err)
				}
				for _, task := range tasks {
					children, err := os.ReadFile(fmt.Sprintf("/proc/%d/task/%s/children", worker.PID, task.Name()))
					if err != nil {
						t.Fatal(err)
					}
					for _, child := range strings.Fields(string(children)) {
						if child != strconv.Itoa(ready.PID) {
							t.Fatal("missed preparation boundary; another capture child already exists", string(children))
						}
					}
				}
				eventsData, err := os.ReadFile(guardGroupPath(view.Record) + "/cgroup.events")
				events, parseErr := parseCgroupCounters(string(eventsData))
				sourceStatus, statusErr := os.ReadFile(fmt.Sprintf("/proc/%d/status", view.Runtime.PID))
				if err != nil || parseErr != nil || events["frozen"] != 1 || statusErr != nil || !bytes.Contains(sourceStatus, []byte("TracerPid:\t0\n")) {
					t.Fatal("signal boundary is not the exact frozen untraced source", events, err, parseErr, statusErr)
				}
				if err := unix.PidfdSendSignal(pidfd, unix.SIGUSR1, nil, 0); err != nil {
					t.Fatal(err)
				}
				pending, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", view.Runtime.PID))
				if err != nil {
					t.Fatal(err)
				}
				var pendingBits uint64
				for _, line := range strings.Split(string(pending), "\n") {
					fields := strings.Fields(line)
					if len(fields) == 2 && (fields[0] == "SigPnd:" || fields[0] == "ShdPnd:") {
						value, err := strconv.ParseUint(fields[1], 16, 64)
						if err != nil {
							t.Fatal(err)
						}
						pendingBits |= value
					}
				}
				if pendingBits&(uint64(1)<<(uint(unix.SIGUSR1)-1)) == 0 {
					t.Fatal("actual boundary signal is not queued on the frozen source")
				}
				beforeSignal = map[string]any{"source_status_after_queue": string(pending), "source_frozen": true, "capture_worker": worker, "mutator": ready, "no_criu_child": true}
				if err := unix.PidfdSendSignal(workerFD, unix.SIGCONT, nil, 0); err != nil {
					t.Fatal(err)
				}
				err = command.Wait()
				waited = true
				if err != nil {
					t.Fatal("pause after exact boundary signal", err, stderr.String())
				}
				view = boundedLossView(t, "resume", "--snapshot-id", snapshot, id)
				root.Close()
				root, err = os.Open(fmt.Sprintf("/proc/%d/root", view.Runtime.PID))
				if err != nil {
					t.Fatal("pin resumed signal task root", err)
				}
				defer root.Close()
			} else if mode != "recaptured-boundary" {
				if err := unix.PidfdSendSignal(pidfd, unix.SIGUSR1, nil, 0); err != nil {
					t.Fatal(err)
				}
			}
			stamp := time.Now()
			for deadline := stamp.Add(time.Second); ; {
				data, err := os.ReadFile(fmt.Sprintf("/proc/self/fd/%d/tmp/timer-finished", root.Fd()))
				if err == nil && len(data) == 16 {
					proofs[mode] = map[string]any{"raw_eintr": true, "one_handler": true, "syscall_registers_preserved": true, "observed_after_ns": time.Since(stamp).Nanoseconds(), "boundary": beforeSignal, "polling_cost": pollingCost}
					break
				}
				if time.Now().After(deadline) {
					memory, memErr := os.Open(fmt.Sprintf("/proc/%d/mem", view.Runtime.PID))
					counter := make([]byte, 8)
					var readErr error
					if memErr == nil {
						_, readErr = memory.ReadAt(counter, countAddress)
						memory.Close()
					}
					syscall, syscallErr := os.ReadFile(fmt.Sprintf("/proc/%d/syscall", view.Runtime.PID))
					boundedLossSave(t, "timer-signal-failed.json", map[string]any{"passed": false, "case": mode, "completed_controls": proofs, "boundary": beforeSignal, "handler_count": binary.LittleEndian.Uint64(counter), "memory_error": fmt.Sprint(memErr, readErr), "syscall": string(syscall), "syscall_error": fmt.Sprint(syscallErr)})
					t.Fatal("caught signal did not return the original wait as EINTR with unchanged registers", mode, "handler_count", binary.LittleEndian.Uint64(counter), "syscall", string(syscall), err)
				}
				time.Sleep(time.Millisecond)
			}
		}) {
			return
		}
	}
	boundedLossSave(t, "timer-signal.json", map[string]any{"passed": true, "production_qualified": false, "uncaptured_control": true, "active_wait_eintr": true, "prepared_boundary_eintr": true, "recaptured_boundary_eintr": true, "cases": proofs, "task_sha256": sum(program), "assembly_sha256": sum([]byte(assembly))})
}
