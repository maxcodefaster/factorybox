//go:build linux && amd64

package lifecycle

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"factorybox/internal/timerstate"
	"golang.org/x/sys/unix"
)

const timerKernelCodeSize = 323

func timerKernelTemplate() []byte { return timerKernelAMD64 }

func timerKernelImage(_ uint64, _ []byte) ([]byte, error) { return timerKernelAMD64, nil }

func timerActivationOriginal(frame []byte) (unix.PtraceRegs, error) {
	var original unix.PtraceRegs
	if err := binary.Read(bytes.NewReader(frame[128:344]), binary.LittleEndian, &original); err != nil {
		return original, err
	}
	clock := binary.LittleEndian.Uint64(frame[24:])
	if original.Cs != 0x33 || (clock != 1 && clock != 7) || (original.Orig_rax != 35 && original.Orig_rax != 230 && original.Orig_rax != 219) {
		return original, fmt.Errorf("staged original timer ABI differs")
	}
	return original, nil
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
