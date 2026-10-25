//go:build linux && arm64

package lifecycle

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"factorybox/internal/timerstate"
	"golang.org/x/sys/unix"
)

func nativeArm64TimerTrace(t *testing.T, duration string) (*timerTrace, <-chan os.Signal) {
	t.Helper()
	if os.Getenv("FACTORYBOX_TIMER_ARM64_NATIVE") != "1" {
		t.Skip("explicit owned ARM64 timer CPU proof only")
	}
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)
	events := make(chan os.Signal, 1)
	signal.Notify(events, unix.SIGCHLD)
	t.Cleanup(func() { signal.Stop(events) })
	child := exec.Command("/bin/sleep", duration)
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	pid := child.Process.Pid
	t.Cleanup(func() {
		_ = child.Process.Kill()
		_ = unix.PtraceDetach(pid)
		_ = child.Wait()
	})
	for deadline := time.Now().Add(2 * time.Second); ; {
		wait, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/wchan")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(wait), "nanosleep") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("owned native sleep did not arm: %s", wait)
		}
		time.Sleep(time.Millisecond)
	}
	_, _, errno := unix.Syscall6(unix.SYS_PTRACE, unix.PTRACE_SEIZE, uintptr(pid), 0,
		unix.PTRACE_O_TRACESYSGOOD|unix.PTRACE_O_SUSPEND_SECCOMP, 0, 0)
	if errno != 0 {
		t.Fatal(errno)
	}
	if err := unix.PtraceInterrupt(pid); err != nil {
		t.Fatal(err)
	}
	status, err := timerWait(pid, time.Now().Add(2*time.Second), events)
	if err != nil || !status.Stopped() || status.StopSignal() != unix.SIGTRAP {
		t.Fatal("owned timer did not enter its trace stop", status, err)
	}
	trace := &timerTrace{pid: pid, stopped: true}
	if err := unix.PtraceGetRegs(pid, &trace.registers); err != nil {
		t.Fatal(err)
	}
	t.Logf("original native interrupted timer: %+v", trace.registers)
	return trace, events
}

func TestNativeArm64TimerRemote(t *testing.T) {
	trace, events := nativeArm64TimerTrace(t, "60")
	pid := trace.pid
	actual, err := timerRemote(trace, unix.SYS_GETPID, nil, events)
	if err != nil || actual != uint64(pid) {
		t.Fatal("native timer backend did not execute in the exact owned task", actual, pid, err)
	}
	for iteration := 0; iteration < 3; iteration++ {
		address, err := timerRemote(trace, unix.SYS_MMAP, []uint64{
			0, 8192, unix.PROT_READ | unix.PROT_WRITE, unix.MAP_PRIVATE | unix.MAP_ANON, math.MaxUint64, 0,
		}, events)
		if err != nil || address < 4096 {
			t.Fatal("owned private mapping failed", address, err)
		}
		marker := bytes.Repeat([]byte{byte(iteration + 1)}, 32)
		if _, err := unix.PtracePokeData(pid, uintptr(address), marker); err != nil {
			t.Fatal(err)
		}
		data, err := timerMemoryRead(pid, address, len(marker))
		if err != nil || !bytes.Equal(data, marker) {
			t.Fatal("owned memory verification failed", data, err)
		}
		if _, err := timerRemote(trace, unix.SYS_MPROTECT,
			[]uint64{address, 4096, unix.PROT_READ | unix.PROT_EXEC}, events); err != nil {
			t.Fatal(err)
		}
		if _, err := timerRemote(trace, unix.SYS_MUNMAP, []uint64{address, 8192}, events); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := timerRemote(trace, unix.SYS_OPENAT, []uint64{math.MaxUint64 - 99, 1, 0, 0}, events); !errors.Is(err, unix.EFAULT) {
		t.Fatal("native raw syscall error was not preserved", err)
	}
	if _, err := timerRemote(trace, unix.SYS_GETPID, make([]uint64, 7), events); err == nil {
		t.Fatal("oversized native syscall argument list was admitted")
	}
}

func TestNativeArm64TimerCodeABI(t *testing.T) {
	for _, mode := range []string{"staged", "kernel", "activated"} {
		t.Run(mode, func(t *testing.T) {
			trace, events := nativeArm64TimerTrace(t, "2")
			birth, err := processBirth(trace.pid)
			if err != nil {
				t.Fatal(err)
			}
			ticks, err := strconv.ParseUint(birth, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			observation, err := timerstate.Read([]timerstate.Task{{PID: trace.pid, StartTicks: ticks}})
			if err != nil || len(observation.States) != 1 || !observation.ProgramReleased || !observation.MapReleased {
				t.Fatal("original native timer plan is unconfirmed", observation, err)
			}
			if err := timerMask(trace.pid, unix.PTRACE_GETSIGMASK, &trace.mask); err != nil {
				t.Fatal(err)
			}
			target, err := prepareTimerTarget(trace, observation.States[0])
			if err != nil {
				t.Fatal(err)
			}
			stack, err := timerMemoryRead(trace.pid, target.Original.Sp-64, 64)
			if err != nil {
				t.Fatal(err)
			}
			target.Address, err = chooseTimerAddress(trace.pid, nil, target.Original.Pc)
			if err != nil {
				t.Fatal(err)
			}
			frame := bytes.Clone(target.Frame)
			code, err := timerContinuationImage(target.Address, frame)
			if mode == "kernel" {
				binary.LittleEndian.PutUint64(frame[96:], 0)
				code, err = timerARM64Image(timerKernelARM64, timerKernelSHA,
					timerKernelReturnBranchOffset, target.Address, frame)
			}
			if err != nil {
				t.Fatal(err)
			}
			address, err := timerRemote(trace, unix.SYS_MMAP, []uint64{target.Address, 8192,
				unix.PROT_READ | unix.PROT_WRITE, unix.MAP_PRIVATE | unix.MAP_ANON | unix.MAP_FIXED_NOREPLACE,
				math.MaxUint64, 0}, events)
			if err != nil || address != target.Address {
				t.Fatal("owned continuation mapping failed", address, err)
			}
			payload := make([]byte, 8192)
			copy(payload, code)
			copy(payload[4096:], frame)
			if _, err := unix.PtracePokeData(trace.pid, uintptr(address), payload); err != nil {
				t.Fatal(err)
			}
			if _, err := timerRemote(trace, unix.SYS_MPROTECT,
				[]uint64{address, 4096, unix.PROT_READ | unix.PROT_EXEC}, events); err != nil {
				t.Fatal(err)
			}
			if mode == "activated" {
				blocked := ^uint64(0) &^ (uint64(1)<<(uint(unix.SIGKILL)-1) | uint64(1)<<(uint(unix.SIGSTOP)-1))
				if err := timerMask(trace.pid, unix.PTRACE_SETSIGMASK, &blocked); err != nil {
					t.Fatal(err)
				}
			}
			if err := timerEnterContinuation(trace, address); err != nil {
				t.Fatal(err)
			}
			found, actualFrame, staged, err := timerPrivateFrame(trace.pid, address)
			if err != nil || found != address || !bytes.Equal(frame, actualFrame) || staged != (mode != "kernel") {
				t.Fatal("exact owned private continuation was not recognized", found, staged, err)
			}
			if mode == "activated" {
				if err := unix.PtraceGetRegs(trace.pid, &trace.registers); err != nil {
					t.Fatal(err)
				}
				kernelCode, err := timerKernelImage(address, frame)
				if err != nil {
					t.Fatal(err)
				}
				activation := timerActivationTarget{TID: trace.pid, Address: address, Frame: frame,
					Original: target.Original, Mask: target.Mask, CodeSHA256: sum(kernelCode)}
				if err := activateTimerTarget(trace, &activation, birth, events); err != nil {
					t.Fatal(err)
				}
				if activation.Expired || activation.Armed == nil ||
					(!activation.Rewound && activation.KernelReturn != -516) {
					t.Fatal("activation did not prove the actual native kernel deadline", activation)
				}
				var mask uint64
				if err := timerMask(trace.pid, unix.PTRACE_GETSIGMASK, &mask); err != nil || mask != target.Mask {
					t.Fatal("native syscall-entry mask handoff changed original signals", mask, err)
				}
				if !activation.Rewound {
					// PTRACE_SYSCALL reports the interrupted wait's raw
					// syscall-exit first. A later capture uses a fresh ordinary
					// interrupt, after the kernel has prepared its restart.
					if err := unix.PtraceCont(trace.pid, 0); err != nil {
						t.Fatal(err)
					}
					trace.stopped = false
					for deadline := time.Now().Add(time.Second); ; {
						var status unix.WaitStatus
						pid, err := unix.Wait4(trace.pid, &status, unix.WALL|unix.WNOHANG, nil)
						if errors.Is(err, unix.EINTR) {
							continue
						}
						if err != nil {
							t.Fatal(err)
						}
						if pid == trace.pid {
							if !status.Stopped() || status.StopSignal() != unix.SIGTRAP {
								t.Fatal("recaptured native wait reached an unexpected event", status)
							}
							trace.stopped = true
							break
						}
						call, err := os.ReadFile(fmt.Sprintf("/proc/%d/syscall", trace.pid))
						if err != nil {
							t.Fatal(err)
						}
						fields := strings.Fields(string(call))
						if len(fields) > 4 && (fields[0] == strconv.Itoa(unix.SYS_CLOCK_NANOSLEEP) ||
							fields[0] == strconv.Itoa(unix.SYS_RESTART_SYSCALL)) {
							if err := unix.PtraceInterrupt(trace.pid); err != nil {
								t.Fatal(err)
							}
							status, err = timerWait(trace.pid, deadline, events)
							if err != nil || !status.Stopped() || status.StopSignal() != unix.SIGTRAP {
								t.Fatal("fresh native restart stop did not settle", status, err)
							}
							trace.stopped = true
							break
						}
						if time.Now().After(deadline) {
							t.Fatal("native wait did not restart before recapture")
						}
						time.Sleep(time.Millisecond)
					}
				}
				if err := unix.PtraceGetRegs(trace.pid, &trace.registers); err != nil {
					t.Fatal(err)
				}
				recaptured, err := timerstate.Read([]timerstate.Task{{PID: trace.pid, StartTicks: ticks}})
				if err != nil || len(recaptured.States) != 1 || !recaptured.ProgramReleased || !recaptured.MapReleased {
					t.Fatal("recaptured kernel timer plan is unconfirmed", recaptured, err)
				}
				reused, err := prepareTimerTarget(trace, recaptured.States[0])
				if err != nil || !reused.Reused || reused.Address != address ||
					reused.Original != target.Original || reused.Mask != target.Mask {
					t.Fatal("recapture did not preserve the original ARM64 context", reused, err)
				}
			}
			var actual unix.PtraceRegs
			for count, deadline := 0, time.Now().Add(5*time.Second); ; count++ {
				if count > 4096 || time.Now().After(deadline) {
					t.Fatal("native continuation did not return to the original application")
				}
				if err := unix.PtraceSingleStep(trace.pid); err != nil {
					t.Fatal(err)
				}
				status, err := timerWait(trace.pid, deadline, events)
				if err != nil || !status.Stopped() || status.StopSignal() != unix.SIGTRAP {
					t.Fatal("native continuation left its exact trace", status, err)
				}
				if err := unix.PtraceGetRegs(trace.pid, &actual); err != nil {
					t.Fatal(err)
				}
				if actual.Pc == target.Original.Pc {
					break
				}
			}
			if actual.Regs[0] != 0 || actual.Sp != target.Original.Sp ||
				actual.Pstate&0xf0000000 != target.Original.Pstate&0xf0000000 {
				t.Fatal("native continuation changed return, original SP or NZCV", actual, target.Original)
			}
			for index := 1; index < len(actual.Regs); index++ {
				if actual.Regs[index] != target.Original.Regs[index] {
					t.Fatalf("native continuation changed X%d: got=%#x want=%#x", index,
						actual.Regs[index], target.Original.Regs[index])
				}
			}
			after, err := timerMemoryRead(trace.pid, target.Original.Sp-64, len(stack))
			if err != nil || !bytes.Equal(stack, after) {
				t.Fatal("native continuation borrowed original application stack memory", err)
			}
			request, err := timerMemoryRead(trace.pid, target.RequestAddress, 16)
			if err != nil || !bytes.Equal(request, target.RequestBytes) {
				t.Fatal("native continuation changed the original request", err)
			}
		})
	}
}
