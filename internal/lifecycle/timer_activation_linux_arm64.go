//go:build linux && arm64

package lifecycle

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"factorybox/internal/timerstate"
	"golang.org/x/sys/unix"
)

const timerKernelCodeSize = 256
const timerKernelClockReturnOffset = 32

func timerKernelTemplate() []byte { return timerKernelARM64 }

func timerKernelImage(address uint64, frame []byte) ([]byte, error) {
	return timerARM64Image(timerKernelARM64, timerKernelSHA, timerKernelReturnBranchOffset, address, frame)
}

func timerActivationOriginal(frame []byte) (unix.PtraceRegs, error) {
	original, err := timerARM64Original(frame)
	if err != nil {
		return original, err
	}
	clock := binary.LittleEndian.Uint64(frame[24:])
	if clock != 1 && clock != 7 {
		return original, fmt.Errorf("staged ARM64 timer clock differs")
	}
	return original, nil
}

func activateTimerTarget(trace *timerTrace, target *timerActivationTarget, birth string, events <-chan os.Signal) error {
	frame := bytes.Clone(target.Frame)
	binary.LittleEndian.PutUint64(frame[96:], 0)
	code, err := timerKernelImage(target.Address, frame)
	if err != nil || sum(code) != target.CodeSHA256 {
		return fmt.Errorf("ARM64 native continuation differs from its durable exact image: %v", err)
	}
	blocked := ^uint64(0) &^ (uint64(1)<<(uint(unix.SIGKILL)-1) | uint64(1)<<(uint(unix.SIGSTOP)-1))
	if err := timerMask(trace.pid, unix.PTRACE_SETSIGMASK, &blocked); err != nil {
		return err
	}
	trace.maskChanged = true
	payload := make([]byte, 4096)
	copy(payload, code)
	if _, err := unix.PtracePokeData(trace.pid, uintptr(target.Address), payload); err != nil {
		return err
	}
	trace.changed = true
	if _, err := unix.PtracePokeData(trace.pid, uintptr(target.Address+4096), frame); err != nil {
		return err
	}
	actual, err := timerMemoryRead(trace.pid, target.Address, 8192)
	if err != nil || !bytes.Equal(actual[:4096], payload) || !bytes.Equal(actual[4096:], frame) {
		return fmt.Errorf("ARM64 activation private code/frame verification failed: %v", err)
	}
	if err := timerEnterContinuation(trace, target.Address); err != nil {
		return err
	}
	for count := 0; count < 3; count++ {
		if err := unix.PtraceSyscall(trace.pid, 0); err != nil {
			return err
		}
		trace.stopped = false
		status, err := timerWait(trace.pid, time.Now().Add(2*time.Second), events)
		if err != nil || !status.Stopped() || status.StopSignal() != unix.SIGTRAP|0x80 {
			return fmt.Errorf("ARM64 activation syscall stop differs: %v %v", status, err)
		}
		trace.stopped = true
		info, err := timerARM64Boundary(trace.pid)
		wantIP := target.Address + timerKernelClockReturnOffset
		if count == 2 {
			wantIP = target.Address + timerKernelReturnOffset
		}
		if err != nil || info.IP != wantIP || info.SP != target.Original.Sp {
			return fmt.Errorf("ARM64 activation left its exact private syscall instruction: %v", err)
		}
		if count < 2 {
			if count == 0 && (info.Operation != unix.PTRACE_SYSCALL_INFO_ENTRY ||
				info.Data[0] != unix.SYS_CLOCK_GETTIME || info.Data[1] != binary.LittleEndian.Uint64(frame[24:]) ||
				info.Data[2] != target.Address+4096+56) ||
				count == 1 && (info.Operation != unix.PTRACE_SYSCALL_INFO_EXIT || info.Data[0] != 0) {
				return fmt.Errorf("ARM64 activation clock boundary differs")
			}
			if count == 1 {
				clock, err := timerMemoryRead(trace.pid, target.Address+4096+56, 16)
				if err != nil {
					return err
				}
				now := int64(binary.LittleEndian.Uint64(clock))*1e9 + int64(binary.LittleEndian.Uint64(clock[8:]))
				if now >= int64(binary.LittleEndian.Uint64(frame[16:])) {
					r := target.Original
					r.Regs[0] = 0
					if err := unix.PtraceSetRegs(trace.pid, &r); err != nil {
						return err
					}
					number := int32(-1)
					if err := timerARM64Syscall(trace.pid, &number, true); err != nil {
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
		if info.Operation != unix.PTRACE_SYSCALL_INFO_ENTRY || info.Data[0] != unix.SYS_CLOCK_NANOSLEEP ||
			info.Data[1] != binary.LittleEndian.Uint64(frame[24:]) || info.Data[2] != 0 ||
			info.Data[3] != target.Address+4096+40 || info.Data[4] != binary.LittleEndian.Uint64(frame[32:]) {
			return fmt.Errorf("ARM64 activation native relative syscall ABI differs")
		}
	}
	// The mask is handed over at native syscall entry. No application
	// instruction can run before an already queued signal reaches its exit.
	if err := timerMask(trace.pid, unix.PTRACE_SETSIGMASK, &target.Mask); err != nil {
		return err
	}
	if err := unix.PtraceSyscall(trace.pid, 0); err != nil {
		return err
	}
	trace.stopped = false
	var stopped unix.WaitStatus
	for deadline := time.Now().Add(time.Second); ; {
		pid, err := unix.Wait4(trace.pid, &stopped, unix.WALL|unix.WNOHANG, nil)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return err
		}
		if pid != 0 {
			if !stopped.Stopped() || stopped.StopSignal() != unix.SIGTRAP|0x80 {
				return fmt.Errorf("ARM64 native timer reached an unexpected event before return: %v", stopped)
			}
			trace.stopped = true
			break
		}
		state, stateErr := os.ReadFile(fmt.Sprintf("/proc/%d/status", trace.pid))
		call, callErr := os.ReadFile(fmt.Sprintf("/proc/%d/syscall", trace.pid))
		fields := strings.Fields(string(call))
		if stateErr == nil && callErr == nil && bytes.Contains(state, []byte("\nState:\tS ")) &&
			len(fields) > 4 && fields[0] == strconv.Itoa(unix.SYS_CLOCK_NANOSLEEP) {
			if err := unix.PtraceInterrupt(trace.pid); err != nil {
				return err
			}
			stopped, err = timerWait(trace.pid, time.Now().Add(2*time.Second), events)
			if err != nil || !stopped.Stopped() ||
				(stopped.StopSignal() != unix.SIGTRAP && stopped.StopSignal() != unix.SIGTRAP|0x80) {
				return fmt.Errorf("ARM64 native activation interruption differs: %v %v", stopped, err)
			}
			trace.stopped = true
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("ARM64 native activation never armed or returned: %s", call)
		}
		time.Sleep(time.Millisecond)
	}
	var r unix.PtraceRegs
	if err := unix.PtraceGetRegs(trace.pid, &r); err != nil {
		return err
	}
	if stopped.StopSignal() == unix.SIGTRAP|0x80 {
		info, err := timerARM64Boundary(trace.pid)
		if err != nil || info.Operation != unix.PTRACE_SYSCALL_INFO_EXIT ||
			info.IP != target.Address+timerKernelReturnOffset {
			return fmt.Errorf("ARM64 activated timer has no exact native return: %v", err)
		}
		target.KernelReturn = int64(info.Data[0])
		if target.KernelReturn != -516 && target.KernelReturn != 0 && target.KernelReturn != -14 && target.KernelReturn != -4 {
			return fmt.Errorf("ARM64 activated timer has an unexpected raw return: %d", target.KernelReturn)
		}
	} else {
		var number int32
		if err := timerARM64Syscall(trace.pid, &number, false); err != nil || number != -1 ||
			r.Pc != target.Address+timerKernelWaitOffset ||
			(r.Regs[8] != unix.SYS_CLOCK_NANOSLEEP && r.Regs[8] != unix.SYS_RESTART_SYSCALL) {
			return fmt.Errorf("ARM64 activated kernel wait did not reach its rewound restart boundary: %v", err)
		}
		target.Rewound = true
	}
	if target.Rewound || target.KernelReturn == -516 {
		ticks, err := strconv.ParseUint(birth, 10, 64)
		if err != nil {
			return err
		}
		observation, err := timerstate.Read([]timerstate.Task{{PID: trace.pid, StartTicks: ticks}})
		if err != nil || !observation.ProgramReleased || !observation.MapReleased || len(observation.States) != 1 {
			return fmt.Errorf("ARM64 activated kernel timer cannot be verified: %v", err)
		}
		state := observation.States[0]
		offset, err := timerOffset(trace.pid, state.Clock)
		if err != nil {
			return err
		}
		delta := state.ExpiresNS + offset - int64(binary.LittleEndian.Uint64(frame[16:]))
		if !state.RelativeTimer || int64(state.Clock) != int64(binary.LittleEndian.Uint64(frame[24:])) ||
			state.Remainder != binary.LittleEndian.Uint64(frame[32:]) ||
			delta < -int64(250*time.Millisecond) || delta > int64(250*time.Millisecond) {
			return fmt.Errorf("ARM64 activated timer differs from its original deadline: %+v delta=%d", state, delta)
		}
		target.Armed = &observation
	}
	return nil
}
