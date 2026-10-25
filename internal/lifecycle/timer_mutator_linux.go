//go:build linux && (amd64 || arm64)

package lifecycle

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/signal"
	"reflect"
	"runtime"
	"sort"
	"time"

	"factorybox/internal/timerstate"
	"golang.org/x/sys/unix"
)

type timerPreparedTarget struct {
	State          timerstate.State `json:"state"`
	Original       unix.PtraceRegs  `json:"original"`
	Mask           uint64           `json:"mask"`
	Address        uint64           `json:"address"`
	Reused         bool             `json:"reused"`
	Frame          []byte           `json:"frame"`
	RequestAddress uint64           `json:"request_address"`
	RequestBytes   []byte           `json:"request_bytes"`
	CodeSHA256     string           `json:"code_sha256"`
}

func runTimerMutator() (resultErr error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	template := timerContinuationTemplate()
	if len(template) == 0 || len(template) > 4096 || sum(template) != timerContinuationSHA {
		return fmt.Errorf("native timer continuation code changed")
	}
	request, anchors, sealed, err := readCaptureTimerRequest()
	defer anchors.close()
	defer sealed.Close()
	if err != nil {
		return err
	}
	plan, err := readTimerPlan(request, anchors)
	if err != nil {
		return err
	}
	gate := os.NewFile(8, "original timer mutator gate")
	defer gate.Close()
	parentStart, err := processBirth(os.Getppid())
	if err != nil {
		return err
	}
	birth, err := processBirth(unix.Gettid())
	if err != nil {
		return err
	}
	ready := timerMutatorReady{request.Guard.Token, os.Getppid(), parentStart, os.Getpid(), unix.Gettid(), birth}
	data, err := json.Marshal(ready)
	if err != nil {
		return err
	}
	if _, err := gate.Write(data); err != nil {
		return err
	}
	ack, err := captureGuardRead(int(gate.Fd()), 2000)
	if err != nil || string(ack) != "prepare:"+request.Guard.Token {
		return fmt.Errorf("timer mutator has no independent armed exit guard: %v", err)
	}
	actual, err := captureGuardThreads(request.Guard, anchors)
	if err != nil || !reflect.DeepEqual(actual, plan.Threads) {
		return fmt.Errorf("frozen timer thread inventory changed: %v", err)
	}
	traces := map[int]*timerTrace{}
	events := make(chan os.Signal, 1)
	signal.Notify(events, unix.SIGCHLD)
	defer signal.Stop(events)
	defer func() {
		if len(traces) == 0 {
			return
		}
		_, held := captureGuardHold(request.Guard, anchors)
		resultErr = errors.Join(resultErr, held)
		for _, trace := range traces {
			if !trace.stopped {
				continue
			}
			if trace.maskChanged {
				resultErr = errors.Join(resultErr, timerMask(trace.pid, unix.PTRACE_SETSIGMASK, &trace.mask))
			}
			if !trace.changed {
				resultErr = errors.Join(resultErr, unix.PtraceSetRegs(trace.pid, &trace.registers))
			}
			if held == nil {
				resultErr = errors.Join(resultErr, unix.PtraceDetach(trace.pid))
			}
		}
	}()
	var tids []int
	for tid := range plan.Threads {
		tids = append(tids, tid)
	}
	sort.Ints(tids)
	for _, tid := range tids {
		_, _, errno := unix.Syscall6(unix.SYS_PTRACE, unix.PTRACE_SEIZE, uintptr(tid), 0, unix.PTRACE_O_TRACESYSGOOD|unix.PTRACE_O_SUSPEND_SECCOMP, 0, 0)
		if errno != 0 {
			return errno
		}
		traces[tid] = &timerTrace{pid: tid}
		if err := unix.PtraceInterrupt(tid); err != nil {
			return err
		}
	}
	if err := timerSetFrozen(request.Guard, anchors, false); err != nil {
		return err
	}
	deadline := time.Now().Add(3 * time.Second)
	for _, tid := range tids {
		status, err := timerWait(tid, deadline, events)
		if err != nil || !status.Stopped() || status.StopSignal() != unix.SIGTRAP {
			return fmt.Errorf("owned source did not enter exact timer trace stop: %v %v", status, err)
		}
		trace := traces[tid]
		trace.stopped = true
		if err := unix.PtraceGetRegs(tid, &trace.registers); err != nil {
			return err
		}
		if err := timerMask(tid, unix.PTRACE_GETSIGMASK, &trace.mask); err != nil {
			return err
		}
	}
	var targets []timerPreparedTarget
	reserved := map[uint64]bool{}
	for _, state := range plan.Targets {
		trace, exists := traces[state.PID]
		if !exists {
			return fmt.Errorf("timer target is outside original source inventory")
		}
		target, err := prepareTimerTarget(trace, state)
		if err != nil {
			return err
		}
		// An exact previously activated private timer is already at its
		// native kernel interruption, with all source threads ptrace-stopped.
		// Preserve a newly queued signal behind the blocked staging mask;
		// activation installs the original mask only at native syscall entry.
		if err := timerNoPendingCaughtSignal(state.PID); err != nil && !target.Reused {
			return err
		}
		if !target.Reused {
			target.Address, err = chooseTimerAddress(state.PID, reserved, target.Original.PC())
			if err != nil {
				return err
			}
		}
		reserved[target.Address] = true
		code, err := timerContinuationImage(target.Address, target.Frame)
		if err != nil {
			return err
		}
		target.CodeSHA256 = sum(code)
		targets = append(targets, target)
	}
	// CPU state and predetermined no-replace addresses are durable before any
	// task register or mapping changes. A killed mutator is held by the guard
	// before its ptrace relationships can auto-detach.
	if err := captureGuardWrite(anchors.evidence, "timer-prepare-intent.json", map[string]any{"version": 1, "token": request.Guard.Token, "targets": targets}); err != nil {
		return err
	}
	for _, target := range targets {
		trace := traces[target.State.PID]
		blocked := ^uint64(0) &^ (uint64(1)<<(uint(unix.SIGKILL)-1) | uint64(1)<<(uint(unix.SIGSTOP)-1))
		if err := timerMask(trace.pid, unix.PTRACE_SETSIGMASK, &blocked); err != nil {
			return err
		}
		trace.maskChanged = true
		if !target.Reused {
			address, err := timerRemote(trace, unix.SYS_MMAP, []uint64{target.Address, 8192, unix.PROT_READ | unix.PROT_WRITE, unix.MAP_PRIVATE | unix.MAP_ANON | unix.MAP_FIXED_NOREPLACE, math.MaxUint64, 0}, events)
			if err != nil || address != target.Address {
				return fmt.Errorf("owned timer private mapping differs: %x %v", address, err)
			}
		}
		{
			address := target.Address
			payload := make([]byte, 8192)
			code, err := timerContinuationImage(address, target.Frame)
			if err != nil || sum(code) != target.CodeSHA256 {
				return fmt.Errorf("timer continuation differs from its durable exact image: %v", err)
			}
			copy(payload, code)
			copy(payload[4096:], target.Frame)
			if _, err := unix.PtracePokeData(trace.pid, uintptr(address), payload); err != nil {
				return err
			}
			actual, err := timerMemoryRead(trace.pid, address, 8192)
			if err != nil || !bytes.Equal(actual, payload) {
				return fmt.Errorf("timer private mapping verification failed: %v", err)
			}
			if !target.Reused {
				if _, err := timerRemote(trace, unix.SYS_MPROTECT, []uint64{address, 4096, unix.PROT_READ | unix.PROT_EXEC}, events); err != nil {
					return err
				}
			}
			actual, err = timerMemoryRead(trace.pid, target.RequestAddress, 16)
			if err != nil || !bytes.Equal(actual, target.RequestBytes) {
				return fmt.Errorf("original timer request changed: %v", err)
			}
		}
		if err := timerEnterContinuation(trace, target.Address); err != nil {
			return err
		}
		trace.changed = true
	}
	if err := timerSetFrozen(request.Guard, anchors, true); err != nil {
		return err
	}
	for _, target := range targets {
		trace := traces[target.State.PID]
		var blocked uint64
		if err := timerMask(trace.pid, unix.PTRACE_GETSIGMASK, &blocked); err != nil {
			return err
		}
		want := ^uint64(0) &^ (uint64(1)<<(uint(unix.SIGKILL)-1) | uint64(1)<<(uint(unix.SIGSTOP)-1))
		if blocked != want {
			return fmt.Errorf("timer continuation lost its blocked entry mask")
		}
		// The original mask is in the private frame. Keep entry blocked until
		// ppoll atomically installs that mask while waiting. A pending caught
		// signal must produce EINTR, including after checkpoint/restore.
		trace.maskChanged = false
	}
	for _, tid := range tids {
		if err := unix.PtraceDetach(tid); err != nil {
			return err
		}
		delete(traces, tid)
	}
	if err := timerDetachedCaptureReturned(request.Guard, anchors); err != nil {
		return err
	}
	return captureGuardWrite(anchors.evidence, "timer-prepare-complete.json", map[string]any{"version": 1, "token": request.Guard.Token, "passed": true,
		"source_frozen": true, "tracers_detached": true, "targets": targets, "code_sha256": sum(template), "private_bytes_per_new_target": 8192})
}
