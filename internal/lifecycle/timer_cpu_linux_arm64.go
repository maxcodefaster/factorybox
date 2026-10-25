//go:build linux && arm64

package lifecycle

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

const timerARM64SystemCall = 0x404 // NT_ARM_SYSTEM_CALL, one signed 32-bit number.
const timerARM64AuditArch = 0xc00000b7

type timerARM64SyscallInfo struct {
	Operation uint8
	Padding   [3]byte
	Arch      uint32
	IP, SP    uint64
	Data      [8]uint64
}

func timerARM64Syscall(pid int, number *int32, write bool) error {
	var pinned runtime.Pinner
	pinned.Pin(number)
	iov := unix.Iovec{Base: (*byte)(unsafe.Pointer(number)), Len: 4}
	pinned.Pin(&iov)
	defer pinned.Unpin()
	request := uintptr(unix.PTRACE_GETREGSET)
	if write {
		request = unix.PTRACE_SETREGSET
	}
	_, _, errno := unix.Syscall6(unix.SYS_PTRACE, request, uintptr(pid), timerARM64SystemCall,
		uintptr(unsafe.Pointer(&iov)), 0, 0)
	if errno != 0 {
		return errno
	}
	if iov.Len != 4 {
		return fmt.Errorf("ARM64 timer syscall register has an unexpected size")
	}
	return nil
}

func timerARM64Boundary(pid int) (timerARM64SyscallInfo, error) {
	var info timerARM64SyscallInfo
	var pinned runtime.Pinner
	pinned.Pin(&info)
	defer pinned.Unpin()
	size, _, errno := unix.Syscall6(unix.SYS_PTRACE, unix.PTRACE_GET_SYSCALL_INFO,
		uintptr(pid), unsafe.Sizeof(info), uintptr(unsafe.Pointer(&info)), 0, 0)
	if errno != 0 {
		return info, errno
	}
	minimum := uintptr(80)
	if info.Operation == unix.PTRACE_SYSCALL_INFO_EXIT {
		minimum = 33
	}
	if size < minimum || info.Arch != timerARM64AuditArch ||
		(info.Operation != unix.PTRACE_SYSCALL_INFO_ENTRY && info.Operation != unix.PTRACE_SYSCALL_INFO_EXIT) {
		return info, fmt.Errorf("ARM64 timer has no complete native syscall boundary")
	}
	return info, nil
}

func timerARM64Instruction(pid int, registers unix.PtraceRegs) (uint64, error) {
	if registers.Pc < 4 || registers.Pc%4 != 0 || registers.Pstate&0x1f != 0 {
		return 0, fmt.Errorf("timer is not at an aligned AArch64 EL0 instruction")
	}
	// An interrupted ARM64 restart has already rewound PC before the
	// PTRACE_EVENT_STOP. A syscall-exit stop still points after SVC.
	svc := []byte{0x01, 0x00, 0x00, 0xd4}
	for _, address := range []uint64{registers.Pc, registers.Pc - 4} {
		actual, err := timerMemoryRead(pid, address, 4)
		if err == nil && bytes.Equal(actual, svc) {
			return address, nil
		}
	}
	return 0, fmt.Errorf("ARM64 timer has no exact native SVC instruction")
}

func timerRemote(trace *timerTrace, number uint64, args []uint64, events <-chan os.Signal) (uint64, error) {
	if !trace.stopped || trace.pid <= 1 || number > math.MaxInt32 || len(args) > 6 {
		return 0, fmt.Errorf("invalid stopped ARM64 timer syscall request")
	}
	instruction, err := timerARM64Instruction(trace.pid, trace.registers)
	if err != nil {
		return 0, err
	}
	r := trace.registers
	r.Pc, r.Regs[8] = instruction, number
	var arguments [6]uint64
	copy(arguments[:], args)
	copy(r.Regs[:6], arguments[:])
	if err := unix.PtraceSetRegs(trace.pid, &r); err != nil {
		return 0, err
	}
	trace.changed = true
	step := func(operation uint8) (timerARM64SyscallInfo, error) {
		if err := unix.PtraceSyscall(trace.pid, 0); err != nil {
			return timerARM64SyscallInfo{}, err
		}
		trace.stopped = false
		status, err := timerWait(trace.pid, time.Now().Add(2*time.Second), events)
		if err != nil || !status.Stopped() || status.StopSignal() != unix.SIGTRAP|0x80 {
			return timerARM64SyscallInfo{}, fmt.Errorf("owned ARM64 timer syscall stop differs: %v %v", status, err)
		}
		trace.stopped = true
		info, err := timerARM64Boundary(trace.pid)
		if err != nil || info.Operation != operation || info.IP != instruction+4 || info.SP != r.Sp {
			return info, fmt.Errorf("owned ARM64 timer syscall boundary differs: %+v %v", info, err)
		}
		return info, nil
	}
	entry, err := step(unix.PTRACE_SYSCALL_INFO_ENTRY)
	if err != nil {
		return 0, err
	}
	for index, value := range arguments {
		if entry.Data[index+1] != value {
			return 0, fmt.Errorf("owned ARM64 timer syscall argument %d changed before entry", index)
		}
	}
	// Restart preparation may substitute restart_syscall after the initial
	// event stop. Nothing has executed yet at this entry. Set the actual
	// kernel syscall number as well as X8, rather than relying on X8 alone.
	r.Pc = instruction + 4
	if err := unix.PtraceSetRegs(trace.pid, &r); err != nil {
		return 0, err
	}
	syscall := int32(number)
	if err := timerARM64Syscall(trace.pid, &syscall, true); err != nil {
		return 0, err
	}
	entry, err = timerARM64Boundary(trace.pid)
	if err != nil || entry.Data[0] != number {
		return 0, fmt.Errorf("owned ARM64 timer syscall number did not settle: %v", err)
	}
	exit, err := step(unix.PTRACE_SYSCALL_INFO_EXIT)
	if err != nil {
		return 0, err
	}
	if err := unix.PtraceGetRegs(trace.pid, &r); err != nil {
		return 0, err
	}
	if r.Pc != instruction+4 || r.Regs[8] != number || r.Regs[0] != exit.Data[0] {
		return 0, fmt.Errorf("owned ARM64 timer syscall result registers differ")
	}
	if value := int64(r.Regs[0]); value < 0 && value >= -4095 {
		return 0, unix.Errno(-value)
	}
	return r.Regs[0], nil
}
