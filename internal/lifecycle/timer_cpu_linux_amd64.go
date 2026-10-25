//go:build linux && amd64

package lifecycle

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"time"

	"factorybox/internal/timerstate"
	"golang.org/x/sys/unix"
)

func timerContinuationTemplate() []byte { return timerContinuationAMD64 }

func timerContinuationImage(_ uint64, _ []byte) ([]byte, error) {
	return timerContinuationAMD64, nil
}

func timerEnterContinuation(trace *timerTrace, address uint64) error {
	r := trace.registers
	r.Rip, r.Rax, r.Orig_rax = address, 0, math.MaxUint64
	if err := unix.PtraceSetRegs(trace.pid, &r); err != nil {
		return err
	}
	trace.changed = true
	return nil
}

func prepareTimerTarget(trace *timerTrace, state timerstate.State) (timerPreparedTarget, error) {
	prepared := timerPreparedTarget{State: state, Original: trace.registers, Mask: trace.mask}
	r := trace.registers
	if r.Cs != 0x33 || int64(r.Rax) != -516 || (r.Orig_rax != 35 && r.Orig_rax != 230 && r.Orig_rax != 219) {
		return prepared, fmt.Errorf("relative timer CPU restart state differs from its kernel plan")
	}
	address, previous, staged, err := timerPrivateFrame(trace.pid, r.Rip)
	if err != nil {
		return prepared, err
	}
	if address != 0 {
		if staged || r.Rip != address+170 || r.Rdx != address+4096+40 || r.R10 != binary.LittleEndian.Uint64(previous[32:]) {
			return prepared, fmt.Errorf("recaptured private native timer differs from its exact wait")
		}
		if err := binary.Read(bytes.NewReader(previous[128:344]), binary.LittleEndian, &r); err != nil {
			return prepared, err
		}
		prepared.Reused, prepared.Address = true, address
		prepared.Original = r
		prepared.Mask = binary.LittleEndian.Uint64(previous[104:])
	}
	maps, err := timerMappings(trace.pid)
	if err != nil {
		return prepared, err
	}
	_ = maps // Mapping inventory also verifies the bounded native source view.
	if r.Rip < 2 {
		return prepared, fmt.Errorf("timer has no native syscall return instruction")
	}
	instruction, err := timerMemoryRead(trace.pid, r.Rip-2, 2)
	if err != nil || !bytes.Equal(instruction, []byte{0x0f, 0x05}) {
		return prepared, fmt.Errorf("timer native syscall instruction differs: %v", err)
	}
	if (r.Orig_rax == 35 || r.Orig_rax == 219) && r.Rdi >= 4096 && r.Rsi == state.Remainder {
		prepared.RequestAddress = r.Rdi
	} else if (r.Orig_rax == 230 || r.Orig_rax == 219) && (r.Rdi == 0 || r.Rdi == 1 || r.Rdi == 7 || r.Rdi == 11) && r.Rsi == 0 && r.R10 == state.Remainder {
		prepared.RequestAddress = r.Rdx
	} else {
		return prepared, fmt.Errorf("timer syscall arguments do not identify the exact original nanosleep")
	}
	prepared.RequestBytes, err = timerMemoryRead(trace.pid, prepared.RequestAddress, 16)
	if err != nil {
		return prepared, err
	}
	redzone, err := timerMemoryRead(trace.pid, r.Rsp-8, 8)
	if err != nil {
		return prepared, err
	}
	frame := make([]byte, 4096)
	copy(frame, "FBXTMR02")
	binary.LittleEndian.PutUint64(frame[8:], 2)
	offset, err := timerOffset(trace.pid, state.Clock)
	if err != nil {
		return prepared, err
	}
	if (offset > 0 && state.ExpiresNS > math.MaxInt64-offset) || (offset < 0 && state.ExpiresNS < math.MinInt64-offset) {
		return prepared, fmt.Errorf("timer namespace deadline overflow")
	}
	expires := state.ExpiresNS + offset
	if expires <= 0 {
		return prepared, fmt.Errorf("timer namespace deadline is not positive")
	}
	binary.LittleEndian.PutUint64(frame[16:], uint64(expires))
	binary.LittleEndian.PutUint64(frame[24:], uint64(state.Clock))
	binary.LittleEndian.PutUint64(frame[32:], state.Remainder)
	binary.LittleEndian.PutUint64(frame[104:], prepared.Mask)
	copy(frame[80:88], redzone)
	if state.Remainder == 0 {
		binary.LittleEndian.PutUint64(frame[96:], 1)
	}
	var encoded bytes.Buffer
	if err := binary.Write(&encoded, binary.LittleEndian, r); err != nil {
		return prepared, err
	}
	if encoded.Len() != 216 {
		return prepared, fmt.Errorf("unsupported native ptrace register size")
	}
	copy(frame[128:], encoded.Bytes())
	prepared.Frame = frame
	return prepared, nil
}

func timerRemote(trace *timerTrace, number uint64, args []uint64, events <-chan os.Signal) (uint64, error) {
	r := trace.registers
	r.Rip -= 2
	r.Rax, r.Orig_rax = number, math.MaxUint64
	registers := []*uint64{&r.Rdi, &r.Rsi, &r.Rdx, &r.R10, &r.R8, &r.R9}
	for index, value := range args {
		*registers[index] = value
	}
	if err := unix.PtraceSetRegs(trace.pid, &r); err != nil {
		return 0, err
	}
	trace.changed = true
	for phase := 0; phase < 2; phase++ {
		if err := unix.PtraceSyscall(trace.pid, 0); err != nil {
			return 0, err
		}
		trace.stopped = false
		status, err := timerWait(trace.pid, time.Now().Add(2*time.Second), events)
		if err != nil || !status.Stopped() || status.StopSignal() != unix.SIGTRAP|0x80 {
			return 0, fmt.Errorf("owned timer syscall step differs: %v %v", status, err)
		}
		trace.stopped = true
		if err := unix.PtraceGetRegs(trace.pid, &r); err != nil {
			return 0, err
		}
		if r.Rip != trace.registers.Rip || r.Orig_rax != number || (phase == 0 && int64(r.Rax) != -38) {
			return 0, fmt.Errorf("owned timer remote syscall boundary differs")
		}
	}
	if value := int64(r.Rax); value < 0 && value >= -4095 {
		return 0, unix.Errno(-value)
	}
	return r.Rax, nil
}

func chooseTimerAddress(pid int, reserved map[uint64]bool, _ uint64) (uint64, error) {
	maps, err := timerMappings(pid)
	if err != nil {
		return 0, err
	}
	for address := uint64(0x500000000000); address < 0x500080000000; address += 0x10000 {
		if reserved[address] {
			continue
		}
		available := true
		for _, mapping := range maps {
			if address < mapping.end && mapping.start < address+8192 {
				available = false
				break
			}
		}
		if available {
			return address, nil
		}
	}
	return 0, fmt.Errorf("owned timer continuation address range is exhausted")
}
