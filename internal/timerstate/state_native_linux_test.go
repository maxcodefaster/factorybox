//go:build linux && (amd64 || arm64)

package timerstate

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestNativeOwnedRelativeTimerState(t *testing.T) {
	if os.Getenv("FACTORYBOX_TIMERSTATE_NATIVE") != "1" {
		t.Skip("explicit owned native timer reader proof only")
	}
	child := exec.Command("/bin/sleep", "60")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = child.Process.Kill()
		_ = child.Wait()
	}()
	pid := child.Process.Pid
	deadline := time.Now().Add(2 * time.Second)
	for {
		wait, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/wchan")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(wait), "nanosleep") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("owned sleep did not enter its kernel timer: %s", wait)
		}
		time.Sleep(time.Millisecond)
	}
	if err := unix.Kill(pid, unix.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	var status unix.WaitStatus
	if actual, err := unix.Wait4(pid, &status, unix.WUNTRACED, nil); err != nil || actual != pid || !status.Stopped() {
		t.Fatal("owned child stop did not settle", actual, status, err)
	}
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(string(stat[strings.LastIndex(string(stat), ") ")+2:]))
	if len(fields) < 20 {
		t.Fatal("owned child stat is incomplete")
	}
	ticks, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	observation, err := Read([]Task{{PID: pid, StartTicks: ticks}})
	if err != nil {
		t.Fatal("native kernel timer read failed", err)
	}
	if len(observation.States) != 1 || !observation.ProgramReleased || !observation.MapReleased ||
		observation.ProgramID == 0 || observation.MapID == 0 || observation.KernelBTFHash == "" || observation.ProgramHash == "" {
		t.Fatal("native reader did not release its exact BPF objects", observation)
	}
	state := observation.States[0]
	if state.PID != pid || state.StartTicks != ticks || !state.RelativeTimer ||
		state.Clock != 1 || state.ExpiresNS <= 0 {
		t.Fatal("native reader lost the owned relative deadline", state)
	}
	if _, err := Read([]Task{{PID: pid, StartTicks: ticks + 1}}); err == nil {
		t.Fatal("native reader accepted another process birth")
	}
	t.Logf("owned relative timer observed and released: %+v", observation)
}
