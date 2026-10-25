//go:build linux && arm64

package timerstate

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

func TestNativeArm64SavedTimerRegisters(t *testing.T) {
	if os.Getenv("FACTORYBOX_TIMERSTATE_NATIVE") != "1" {
		t.Skip("explicit owned native ARM64 saved-register proof only")
	}
	var registers unix.PtraceRegs
	if unsafe.Offsetof(registers.Regs)+8*8 != 64 || unsafe.Offsetof(registers.Pc) != 256 ||
		unsafe.Sizeof(registers) != 272 {
		t.Fatal("native Linux ARM64 user_pt_regs ABI differs")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	child := exec.Command("/bin/sleep", "60")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	pid := child.Process.Pid
	defer func() {
		child.Process.Kill()
		unix.PtraceDetach(pid)
		child.Wait()
	}()
	for deadline := time.Now().Add(2 * time.Second); ; {
		wait, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/wchan")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(wait), "nanosleep") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("owned register control did not enter its real timer", string(wait))
		}
		time.Sleep(time.Millisecond)
	}
	if err := unix.PtraceSeize(pid); err != nil {
		t.Fatal(err)
	}
	if err := unix.PtraceInterrupt(pid); err != nil {
		t.Fatal(err)
	}
	var status unix.WaitStatus
	if actual, err := unix.Wait4(pid, &status, unix.WALL, nil); err != nil || actual != pid || !status.Stopped() {
		t.Fatal("owned register control did not enter its exact trace stop", actual, status, err)
	}
	if err := unix.PtraceGetRegs(pid, &registers); err != nil {
		t.Fatal(err)
	}
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(string(stat[strings.LastIndex(string(stat), ") ")+2:]))
	if len(fields) < 20 {
		t.Fatal("owned register control stat is incomplete")
	}
	ticks, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	observation, err := ReadWithRegisters([]Task{{PID: pid, StartTicks: ticks}})
	if err != nil || len(observation.States) != 1 || !observation.ProgramReleased || !observation.MapReleased {
		t.Fatal("native saved-register reader did not release its exact objects", observation, err)
	}
	context := observation.States[0].Registers
	if context == nil || context.PC != registers.Pc || context.Syscall != registers.Regs[8] ||
		context.Argument0 != registers.Regs[0] || context.Argument1 != registers.Regs[1] || context.ObservedNS <= 0 {
		t.Fatal("read-only kernel context differs from the independent actual ptrace snapshot", context, registers)
	}
	t.Logf("saved ARM64 X8/PC match exact owned ptrace registers: %+v", context)
}
