//go:build linux && amd64

package lifecycle

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/signal"
	"runtime"
	"sort"
	"strconv"
	"strings"
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
}

// Identify only an exact runtime-owned frame and its full RX page. Ordinary
// anonymous application mappings are never changed merely because they execute.
func timerPrivateFrame(pid int, rip uint64) (uint64, []byte, bool, error) {
	mappings, err := timerMappings(pid)
	if err != nil {
		return 0, nil, false, err
	}
	for _, mapping := range mappings {
		if rip < mapping.start || rip >= mapping.end || mapping.end-mapping.start != 4096 || mapping.permissions != "r-xp" || mapping.name != "" {
			continue
		}
		code, err := timerMemoryRead(pid, mapping.start, 4096)
		if err != nil {
			return 0, nil, false, err
		}
		native := bytes.Equal(code[:len(timerKernelAMD64)], timerKernelAMD64) && bytes.Equal(code[len(timerKernelAMD64):], make([]byte, 4096-len(timerKernelAMD64)))
		staged := bytes.Equal(code[:len(timerContinuationAMD64)], timerContinuationAMD64) && bytes.Equal(code[len(timerContinuationAMD64):], make([]byte, 4096-len(timerContinuationAMD64)))
		if !native && !staged {
			continue
		}
		frame, err := timerMemoryRead(pid, mapping.start+4096, 4096)
		if err != nil {
			return 0, nil, false, err
		}
		if string(frame[:8]) != "FBXTMR02" || binary.LittleEndian.Uint64(frame[8:]) != 2 {
			return 0, nil, false, fmt.Errorf("private timer frame changed")
		}
		return mapping.start, frame, staged, nil
	}
	return 0, nil, false, nil
}

func runTimerActivation() (resultErr error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if len(timerKernelAMD64) != 323 || sum(timerKernelAMD64) != timerKernelSHA {
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
		address, frame, staged, err := timerPrivateFrame(tid, trace.registers.Rip)
		if err != nil {
			return err
		}
		if address == 0 || !staged {
			continue
		}
		if trace.registers.Rip != address {
			return fmt.Errorf("staged timer ran before activation")
		}
		var original unix.PtraceRegs
		if err := binary.Read(bytes.NewReader(frame[128:344]), binary.LittleEndian, &original); err != nil {
			return err
		}
		clock := binary.LittleEndian.Uint64(frame[24:])
		if original.Cs != 0x33 || (clock != 1 && clock != 7) || (original.Orig_rax != 35 && original.Orig_rax != 230 && original.Orig_rax != 219) {
			return fmt.Errorf("staged original timer ABI differs")
		}
		targets = append(targets, timerActivationTarget{TID: tid, Address: address, Frame: frame, Original: original, Mask: binary.LittleEndian.Uint64(frame[104:])})
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
	if err := captureGuardCaptureReturned(request.Guard, anchors); err != nil {
		return err
	}
	return captureGuardWrite(anchors.evidence, "timer-activation-complete.json", map[string]any{"version": 1, "token": request.Guard.Token, "passed": true, "source_frozen": true, "tracers_detached": true, "targets": targets, "code_sha256": timerKernelSHA})
}

func activateTimerTarget(trace *timerTrace, target *timerActivationTarget, birth string, events <-chan os.Signal) error {
	frame := bytes.Clone(target.Frame)
	binary.LittleEndian.PutUint64(frame[96:], 0) // Native relative form, including NULL rmtp.
	blocked := ^uint64(0) &^ (uint64(1)<<(uint(unix.SIGKILL)-1) | uint64(1)<<(uint(unix.SIGSTOP)-1))
	if err := timerMask(trace.pid, unix.PTRACE_SETSIGMASK, &blocked); err != nil {
		return err
	}
	trace.maskChanged = true
	payload := make([]byte, 4096)
	copy(payload, timerKernelAMD64)
	if _, err := unix.PtracePokeData(trace.pid, uintptr(target.Address), payload); err != nil {
		return err
	}
	trace.changed = true
	if _, err := unix.PtracePokeData(trace.pid, uintptr(target.Address+4096), frame); err != nil {
		return err
	}
	actual, err := timerMemoryRead(trace.pid, target.Address, 8192)
	if err != nil || !bytes.Equal(actual[:4096], payload) || !bytes.Equal(actual[4096:], frame) {
		return fmt.Errorf("activation private code/frame verification failed: %v", err)
	}
	r := trace.registers
	r.Rip, r.Rax, r.Orig_rax = target.Address, 0, math.MaxUint64
	if err := unix.PtraceSetRegs(trace.pid, &r); err != nil {
		return err
	}
	for count := 0; count < 3; count++ {
		if err := unix.PtraceSyscall(trace.pid, 0); err != nil {
			return err
		}
		trace.stopped = false
		status, err := timerWait(trace.pid, time.Now().Add(2*time.Second), events)
		if err != nil || !status.Stopped() || status.StopSignal() != unix.SIGTRAP|0x80 {
			return fmt.Errorf("activation syscall stop differs: %v %v", status, err)
		}
		trace.stopped = true
		if err := unix.PtraceGetRegs(trace.pid, &r); err != nil {
			return err
		}
		if count < 2 {
			if r.Orig_rax != 228 || count == 0 && int64(r.Rax) != -38 || count == 1 && r.Rax != 0 {
				return fmt.Errorf("activation clock boundary differs")
			}
			if count == 1 {
				clock, err := timerMemoryRead(trace.pid, target.Address+4096+56, 16)
				if err != nil {
					return err
				}
				now := int64(binary.LittleEndian.Uint64(clock))*1e9 + int64(binary.LittleEndian.Uint64(clock[8:]))
				if now >= int64(binary.LittleEndian.Uint64(frame[16:])) {
					r = target.Original
					r.Rax, r.Orig_rax = 0, math.MaxUint64
					if err := unix.PtraceSetRegs(trace.pid, &r); err != nil {
						return err
					}
					if err := timerMask(trace.pid, unix.PTRACE_SETSIGMASK, &target.Mask); err != nil {
						return err
					}
					target.Expired = true
					return nil
				}
			}
			continue
		}
		if r.Orig_rax != 230 || int64(r.Rax) != -38 || r.Rip != target.Address+170 || r.Rsi != 0 || r.Rdx != target.Address+4096+40 || r.R10 != binary.LittleEndian.Uint64(frame[32:]) {
			return fmt.Errorf("activation native relative syscall ABI differs")
		}
	}
	// No application instruction can execute between this mask change and the
	// actual syscall. Any already pending signal reaches a kernel return stop.
	if err := timerMask(trace.pid, unix.PTRACE_SETSIGMASK, &target.Mask); err != nil {
		return err
	}
	if err := unix.PtraceSyscall(trace.pid, 0); err != nil {
		return err
	}
	trace.stopped = false
	for deadline := time.Now().Add(time.Second); ; {
		var status unix.WaitStatus
		pid, err := unix.Wait4(trace.pid, &status, unix.WALL|unix.WNOHANG, nil)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return err
		}
		if pid != 0 {
			if !status.Stopped() || status.StopSignal() != unix.SIGTRAP|0x80 {
				return fmt.Errorf("native timer reached an unexpected event before its return: %v", status)
			}
			trace.stopped = true
			break
		}
		state, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", trace.pid))
		call, callErr := os.ReadFile(fmt.Sprintf("/proc/%d/syscall", trace.pid))
		fields := strings.Fields(string(call))
		if err == nil && callErr == nil && bytes.Contains(state, []byte("\nState:\tS ")) && len(fields) > 4 && fields[0] == "230" {
			if err := unix.PtraceInterrupt(trace.pid); err != nil {
				return err
			}
			status, err := timerWait(trace.pid, time.Now().Add(2*time.Second), events)
			if err != nil || !status.Stopped() || (status.StopSignal() != unix.SIGTRAP && status.StopSignal() != unix.SIGTRAP|0x80) {
				return fmt.Errorf("native activation interruption differs: %v %v", status, err)
			}
			trace.stopped = true
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("native activation never armed or returned: %s", call)
		}
		time.Sleep(time.Millisecond)
	}
	if err := unix.PtraceGetRegs(trace.pid, &r); err != nil {
		return err
	}
	target.KernelReturn = int64(r.Rax)
	if r.Orig_rax != 230 || (target.KernelReturn != -516 && target.KernelReturn != 0 && target.KernelReturn != -14 && target.KernelReturn != -4) {
		return fmt.Errorf("activated timer has an unexpected raw kernel return: %d", target.KernelReturn)
	}
	if target.KernelReturn == -516 {
		ticks, err := strconv.ParseUint(birth, 10, 64)
		if err != nil {
			return err
		}
		observation, err := timerstate.Read([]timerstate.Task{{PID: trace.pid, StartTicks: ticks}})
		if err != nil || !observation.ProgramReleased || !observation.MapReleased || len(observation.States) != 1 {
			return fmt.Errorf("activated kernel timer cannot be verified: %v", err)
		}
		state := observation.States[0]
		offset, err := timerOffset(trace.pid, state.Clock)
		if err != nil {
			return err
		}
		delta := state.ExpiresNS + offset - int64(binary.LittleEndian.Uint64(frame[16:]))
		if !state.RelativeTimer || int64(state.Clock) != int64(binary.LittleEndian.Uint64(frame[24:])) || state.Remainder != binary.LittleEndian.Uint64(frame[32:]) || delta < -int64(250*time.Millisecond) || delta > int64(250*time.Millisecond) {
			return fmt.Errorf("activated kernel timer differs from its original deadline: %+v delta=%d", state, delta)
		}
		target.Armed = &observation
	}
	return nil
}
