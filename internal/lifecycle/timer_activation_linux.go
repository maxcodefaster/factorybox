//go:build linux && (amd64 || arm64)

package lifecycle

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"sort"
	"time"

	"factorybox/internal/timerstate"
	"golang.org/x/sys/unix"
)

type timerActivationTarget struct {
	TID          int                     `json:"tid"`
	Address      uint64                  `json:"address"`
	Frame        []byte                  `json:"frame"`
	Original     unix.PtraceRegs         `json:"original"`
	Mask         uint64                  `json:"mask"`
	Expired      bool                    `json:"expired"`
	KernelReturn int64                   `json:"kernel_return"`
	Armed        *timerstate.Observation `json:"armed,omitempty"`
	CodeSHA256   string                  `json:"code_sha256"`
	Rewound      bool                    `json:"restart_rewound,omitempty"`
}

func runTimerActivation() (resultErr error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	template := timerKernelTemplate()
	if len(template) != timerKernelCodeSize || sum(template) != timerKernelSHA {
		return fmt.Errorf("native kernel continuation changed")
	}
	request, anchors, sealed, err := readCaptureTimerRequest()
	defer anchors.close()
	defer sealed.Close()
	if err != nil {
		return err
	}
	gate := os.NewFile(8, "original activation gate")
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
	packet, err := captureGuardRead(int(gate.Fd()), 3000)
	if err != nil || string(packet) != "activate:"+request.Guard.Token {
		return fmt.Errorf("activation has no original independent exit guard: %v", err)
	}
	if err := captureGuardCaptureReturned(request.Guard, anchors); err != nil {
		return err
	}
	threads, err := captureGuardThreads(request.Guard, anchors)
	if err != nil {
		return err
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
			if !trace.changed {
				resultErr = errors.Join(resultErr, unix.PtraceSetRegs(trace.pid, &trace.registers))
				if trace.maskChanged {
					resultErr = errors.Join(resultErr, timerMask(trace.pid, unix.PTRACE_SETSIGMASK, &trace.mask))
				}
			}
			if held == nil {
				resultErr = errors.Join(resultErr, unix.PtraceDetach(trace.pid))
			}
		}
	}()
	var tids []int
	for tid := range threads {
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
	for _, tid := range tids {
		status, err := timerWait(tid, time.Now().Add(3*time.Second), events)
		if err != nil || !status.Stopped() || status.StopSignal() != unix.SIGTRAP {
			return fmt.Errorf("activation thread trace stop differs: %v %v", status, err)
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
	var targets []timerActivationTarget
	for _, tid := range tids {
		trace := traces[tid]
		address, frame, staged, err := timerPrivateFrame(tid, trace.registers.PC())
		if err != nil {
			return err
		}
		if address == 0 || !staged {
			continue
		}
		if trace.registers.PC() != address {
			return fmt.Errorf("staged timer ran before activation")
		}
		original, err := timerActivationOriginal(frame)
		if err != nil {
			return err
		}
		code, err := timerKernelImage(address, frame)
		if err != nil {
			return err
		}
		targets = append(targets, timerActivationTarget{TID: tid, Address: address, Frame: frame,
			Original: original, Mask: binary.LittleEndian.Uint64(frame[104:]), CodeSHA256: sum(code)})
	}
	if err := captureGuardWrite(anchors.evidence, "timer-activation-intent.json", map[string]any{"version": 1, "token": request.Guard.Token, "threads": threads, "targets": targets, "code_sha256": timerKernelSHA}); err != nil {
		return err
	}
	for index := range targets {
		target := &targets[index]
		if err := activateTimerTarget(traces[target.TID], target, threads[target.TID], events); err != nil {
			return err
		}
	}
	if err := timerSetFrozen(request.Guard, anchors, true); err != nil {
		return err
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
	return captureGuardWrite(anchors.evidence, "timer-activation-complete.json", map[string]any{"version": 1, "token": request.Guard.Token, "passed": true, "source_frozen": true, "tracers_detached": true, "targets": targets, "code_sha256": timerKernelSHA})
}
