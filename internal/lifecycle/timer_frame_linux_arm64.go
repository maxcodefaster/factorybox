//go:build linux && arm64

package lifecycle

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"

	"factorybox/internal/timerstate"
	"golang.org/x/sys/unix"
)

func timerContinuationTemplate() []byte { return timerContinuationARM64 }

func timerARM64Original(frame []byte) (unix.PtraceRegs, error) {
	var original unix.PtraceRegs
	if len(frame) != 4096 || string(frame[:8]) != "FBXARM03" || binary.LittleEndian.Uint64(frame[8:]) != 3 {
		return original, fmt.Errorf("private ARM64 timer frame changed")
	}
	if err := binary.Read(bytes.NewReader(frame[128:400]), binary.LittleEndian, &original); err != nil {
		return original, err
	}
	if original.Pc < 4 || original.Pc > math.MaxInt64 || original.Pc%4 != 0 ||
		original.Sp < 4096 || original.Sp%16 != 0 || original.Pstate&0x1f != 0 ||
		(original.Regs[8] != unix.SYS_NANOSLEEP && original.Regs[8] != unix.SYS_CLOCK_NANOSLEEP &&
			original.Regs[8] != unix.SYS_RESTART_SYSCALL) {
		return original, fmt.Errorf("private ARM64 original timer ABI changed")
	}
	return original, nil
}

func timerARM64Image(template []byte, checksum string, branch int, address uint64, frame []byte) ([]byte, error) {
	original, err := timerARM64Original(frame)
	if err != nil {
		return nil, err
	}
	if len(template) > 4096 || sum(template) != checksum || branch < 0 || branch+4 > len(template) ||
		binary.LittleEndian.Uint32(template[branch:]) != 0x14000000 ||
		address < 4096 || address%4096 != 0 || address > math.MaxInt64-4096 {
		return nil, fmt.Errorf("invalid exact ARM64 timer template or code address")
	}
	delta := int64(original.Pc) - int64(address+uint64(branch))
	if delta%4 != 0 || delta/4 < -(1<<25) || delta/4 >= 1<<25 {
		return nil, fmt.Errorf("ARM64 timer return exceeds its direct branch range")
	}
	code := bytes.Clone(template)
	binary.LittleEndian.PutUint32(code[branch:], 0x14000000|uint32(int32(delta/4))&0x03ffffff)
	return code, nil
}

func timerContinuationImage(address uint64, frame []byte) ([]byte, error) {
	return timerARM64Image(timerContinuationARM64, timerContinuationSHA, timerContinuationReturnOffset, address, frame)
}

func timerEnterContinuation(trace *timerTrace, address uint64) error {
	r := trace.registers
	r.Pc, r.Regs[0] = address, 0
	if err := unix.PtraceSetRegs(trace.pid, &r); err != nil {
		return err
	}
	trace.changed = true
	number := int32(-1)
	return timerARM64Syscall(trace.pid, &number, true)
}

func chooseTimerAddress(pid int, reserved map[uint64]bool, returnPC uint64) (uint64, error) {
	maps, err := timerMappings(pid)
	if err != nil {
		return 0, err
	}
	const window = uint64(64 << 20)
	if returnPC < 4 || returnPC > math.MaxInt64-window || returnPC%4 != 0 {
		return 0, fmt.Errorf("invalid native ARM64 timer return address")
	}
	first := uint64(0x10000)
	if returnPC > window {
		first = (returnPC - window + 0xffff) &^ 0xffff
	}
	for address := first; address+8192 < returnPC+window; address += 0x10000 {
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
	return 0, fmt.Errorf("ARM64 timer has no owned mapping within direct return range")
}

func timerARM64TemplateMatches(page, template []byte, branch int) bool {
	if len(page) != 4096 || len(template) > 4096 || branch < 0 || branch+4 > len(template) {
		return false
	}
	actual := bytes.Clone(page[:len(template)])
	copy(actual[branch:branch+4], template[branch:branch+4])
	return bytes.Equal(actual, template) && bytes.Equal(page[len(template):], make([]byte, 4096-len(template)))
}

func timerPrivateFrame(pid int, pc uint64) (uint64, []byte, bool, error) {
	mappings, err := timerMappings(pid)
	if err != nil {
		return 0, nil, false, err
	}
	for _, mapping := range mappings {
		if pc < mapping.start || pc >= mapping.end || mapping.end-mapping.start != 4096 ||
			mapping.permissions != "r-xp" || mapping.name != "" {
			continue
		}
		page, err := timerMemoryRead(pid, mapping.start, 4096)
		if err != nil {
			return 0, nil, false, err
		}
		staged := timerARM64TemplateMatches(page, timerContinuationARM64, timerContinuationReturnOffset)
		native := timerARM64TemplateMatches(page, timerKernelARM64, timerKernelReturnBranchOffset)
		if !staged && !native {
			continue
		}
		frame, err := timerMemoryRead(pid, mapping.start+4096, 4096)
		if err != nil {
			return 0, nil, false, err
		}
		template, checksum, branch := timerKernelARM64, timerKernelSHA, timerKernelReturnBranchOffset
		if staged {
			template, checksum, branch = timerContinuationARM64, timerContinuationSHA, timerContinuationReturnOffset
		}
		expected, err := timerARM64Image(template, checksum, branch, mapping.start, frame)
		if err != nil || !bytes.Equal(page[:len(expected)], expected) {
			return 0, nil, false, fmt.Errorf("private ARM64 timer code/frame changed: %v", err)
		}
		return mapping.start, frame, staged, nil
	}
	return 0, nil, false, nil
}

func prepareTimerTarget(trace *timerTrace, state timerstate.State) (timerPreparedTarget, error) {
	prepared := timerPreparedTarget{State: state, Original: trace.registers, Mask: trace.mask}
	r := trace.registers
	var current int32
	if !trace.stopped || state.PID != trace.pid || !state.RelativeTimer ||
		timerARM64Syscall(trace.pid, &current, false) != nil || current != -1 ||
		r.Pstate&0x1f != 0 || (r.Regs[8] != unix.SYS_NANOSLEEP && r.Regs[8] != unix.SYS_CLOCK_NANOSLEEP &&
		r.Regs[8] != unix.SYS_RESTART_SYSCALL) {
		return prepared, fmt.Errorf("relative ARM64 timer is not at its exact native restart stop")
	}
	address, previous, staged, err := timerPrivateFrame(trace.pid, r.Pc)
	if err != nil {
		return prepared, err
	}
	if address != 0 {
		if staged || r.Pc != address+timerKernelWaitOffset || r.Regs[1] != 0 ||
			r.Regs[2] != address+4096+40 || r.Regs[3] != binary.LittleEndian.Uint64(previous[32:]) {
			return prepared, fmt.Errorf("recaptured private ARM64 timer differs from its exact wait")
		}
		r, err = timerARM64Original(previous)
		if err != nil {
			return prepared, err
		}
		prepared.Reused, prepared.Address = true, address
		prepared.Mask = binary.LittleEndian.Uint64(previous[104:])
	} else {
		instruction, err := timerARM64Instruction(trace.pid, r)
		if err != nil || instruction != r.Pc {
			return prepared, fmt.Errorf("ARM64 timer restart has no exact rewound SVC: %v", err)
		}
		r.Pc += 4
	}
	if _, err := timerMappings(trace.pid); err != nil {
		return prepared, err
	}
	instruction, err := timerMemoryRead(trace.pid, r.Pc-4, 4)
	if err != nil || !bytes.Equal(instruction, []byte{1, 0, 0, 0xd4}) {
		return prepared, fmt.Errorf("original ARM64 timer SVC changed: %v", err)
	}
	if (r.Regs[8] == unix.SYS_NANOSLEEP || r.Regs[8] == unix.SYS_RESTART_SYSCALL) &&
		r.Regs[0] >= 4096 && r.Regs[1] == state.Remainder {
		prepared.RequestAddress = r.Regs[0]
	} else if (r.Regs[8] == unix.SYS_CLOCK_NANOSLEEP || r.Regs[8] == unix.SYS_RESTART_SYSCALL) &&
		(r.Regs[0] == 0 || r.Regs[0] == 1 || r.Regs[0] == 7 || r.Regs[0] == 11) &&
		r.Regs[1] == 0 && r.Regs[3] == state.Remainder {
		prepared.RequestAddress = r.Regs[2]
	} else {
		return prepared, fmt.Errorf("ARM64 timer arguments do not identify the original relative sleep")
	}
	prepared.RequestBytes, err = timerMemoryRead(trace.pid, prepared.RequestAddress, 16)
	if err != nil {
		return prepared, err
	}
	frame := make([]byte, 4096)
	copy(frame, "FBXARM03")
	binary.LittleEndian.PutUint64(frame[8:], 3)
	offset, err := timerOffset(trace.pid, state.Clock)
	if err != nil {
		return prepared, err
	}
	if (offset > 0 && state.ExpiresNS > math.MaxInt64-offset) || (offset < 0 && state.ExpiresNS < math.MinInt64-offset) {
		return prepared, fmt.Errorf("ARM64 timer namespace deadline overflow")
	}
	expires := state.ExpiresNS + offset
	if expires <= 0 || (state.Clock != 1 && state.Clock != 7) {
		return prepared, fmt.Errorf("ARM64 timer deadline or clock is invalid")
	}
	binary.LittleEndian.PutUint64(frame[16:], uint64(expires))
	binary.LittleEndian.PutUint64(frame[24:], uint64(state.Clock))
	binary.LittleEndian.PutUint64(frame[32:], state.Remainder)
	binary.LittleEndian.PutUint64(frame[104:], prepared.Mask)
	if state.Remainder == 0 {
		binary.LittleEndian.PutUint64(frame[96:], 1)
	}
	var encoded bytes.Buffer
	if err := binary.Write(&encoded, binary.LittleEndian, r); err != nil {
		return prepared, err
	}
	if encoded.Len() != 272 {
		return prepared, fmt.Errorf("unsupported native ARM64 ptrace register extent")
	}
	copy(frame[128:], encoded.Bytes())
	if _, err := timerARM64Original(frame); err != nil {
		return prepared, err
	}
	prepared.Original, prepared.Frame = r, frame
	return prepared, nil
}
