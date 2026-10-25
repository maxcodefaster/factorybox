//go:build linux && arm64

package lifecycle

import (
	"strconv"

	"factorybox/internal/timerstate"
	"golang.org/x/sys/unix"
)

// Observe the exact native ARM64 wait without interrupting the source.
func nativeTimerActivatedWait(pid int, fields []string) bool {
	if len(fields) == 3 && fields[0] == "-1" {
		pc, err := strconv.ParseUint(fields[2], 0, 64)
		if err != nil {
			return false
		}
		address, _, staged, err := timerPrivateFrame(pid, pc)
		if err != nil || address == 0 || staged || pc != address+timerKernelWaitOffset {
			return false
		}
		birth, err := processBirth(pid)
		if err != nil {
			return false
		}
		ticks, err := strconv.ParseUint(birth, 10, 64)
		if err != nil {
			return false
		}
		observation, err := timerstate.ReadWithRegisters([]timerstate.Task{{PID: pid, StartTicks: ticks}})
		if err != nil || len(observation.States) != 1 || !observation.ProgramReleased || !observation.MapReleased {
			return false
		}
		state := observation.States[0]
		context := state.Registers
		return state.RelativeTimer && state.Clock == 1 && context != nil && context.PC == pc &&
			(context.Syscall == uint64(unix.SYS_CLOCK_NANOSLEEP) || context.Syscall == uint64(unix.SYS_RESTART_SYSCALL)) &&
			context.Argument0 == 1 && context.Argument1 == 0
	}
	if len(fields) != 9 || (fields[0] != strconv.Itoa(unix.SYS_CLOCK_NANOSLEEP) &&
		fields[0] != strconv.Itoa(unix.SYS_RESTART_SYSCALL)) || fields[1] != "0x1" || fields[2] != "0x0" {
		return false
	}
	pc, err := strconv.ParseUint(fields[8], 0, 64)
	if err != nil {
		return false
	}
	address, _, staged, err := timerPrivateFrame(pid, pc)
	return err == nil && address != 0 && !staged && pc == address+timerKernelReturnOffset
}
