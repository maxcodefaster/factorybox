//go:build linux

package lifecycle

import (
	"strconv"
	"testing"

	"factorybox/internal/timerstate"
	"golang.org/x/sys/unix"
)

func TestTimerAdmissionSyscall(t *testing.T) {
	for _, restart := range []bool{false, true} {
		for _, number := range []int{unix.SYS_NANOSLEEP, unix.SYS_CLOCK_NANOSLEEP, unix.SYS_RESTART_SYSCALL} {
			if !timerAdmissionSyscall(strconv.Itoa(number), restart) {
				t.Fatal("native relative-wait syscall was excluded", number, restart)
			}
		}
		for _, number := range []string{"", "-2", strconv.Itoa(unix.SYS_GETPID), strconv.Itoa(unix.SYS_PPOLL)} {
			if timerAdmissionSyscall(number, restart) {
				t.Fatal("unrelated syscall was treated as a timer candidate", number, restart)
			}
		}
		if timerAdmissionSyscall("-1", restart) != restart {
			t.Fatal("frozen native restart presentation changed its architecture boundary", restart)
		}
	}
}

func TestArm64TimerAdmissionContextRefusesAbsentAndUnrelatedState(t *testing.T) {
	state := timerstate.State{}
	if _, err := timerAdmissionARM64Wait(state, nil); err == nil {
		t.Fatal("ARM64 timer admission accepted absent saved registers")
	}
	state.Registers = &timerstate.Registers{Syscall: uint64(unix.SYS_PPOLL), PC: 0x400000}
	if active, err := timerAdmissionARM64Wait(state, []string{"-1", "0x800000", "0x400000"}); err != nil || active {
		t.Fatal("stale timer restart was adopted during an actual poll", active, err)
	}
	state.Registers.Syscall = uint64(unix.SYS_CLOCK_NANOSLEEP)
	for _, fields := range [][]string{nil, {"-1"}, {"-1", "0x800000", "0x400004"},
		{strconv.Itoa(unix.SYS_CLOCK_NANOSLEEP)}, {strconv.Itoa(unix.SYS_PPOLL), "0", "0", "0", "0", "0", "0", "0", "0"}} {
		if _, err := timerAdmissionARM64Wait(state, fields); err == nil {
			t.Fatal("malformed or changed native context reached task memory", fields)
		}
	}
}

func TestArm64TimerAdmissionReturnPathFailsClosed(t *testing.T) {
	if active, err := timerAdmissionARM64ReturnPath([]byte("[<0>] el0_svc+0x58/0x78\n")); err != nil || !active {
		t.Fatal("exact native syscall return path was not recognized", active, err)
	}
	if active, err := timerAdmissionARM64ReturnPath([]byte("[<0>] el0_interrupt+0x80/0xd8\n")); err != nil || active {
		t.Fatal("user interruption was adopted as a live relative wait", active, err)
	}
	for _, stack := range []string{"", "[<0>] unknown+0x0/0x1\n",
		"[<0>] el0_svc+0x58/0x78\n[<0>] el0t_64_irq_handler+0xc/0x18\n"} {
		if _, err := timerAdmissionARM64ReturnPath([]byte(stack)); err == nil {
			t.Fatal("missing or contradictory kernel context silently skipped timer admission", stack)
		}
	}
}
