//go:build linux && (amd64 || arm64)

package timerstate

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestNativeOwnedTimerPIDSet(t *testing.T) {
	if os.Getenv("FACTORYBOX_TIMERSTATE_NATIVE") != "1" {
		t.Skip("explicit owned native timer reader proof only")
	}
	for _, count := range []int{192, 1024} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			readOwnedTimerPIDSet(t, count)
		})
	}
}

func readOwnedTimerPIDSet(t *testing.T, count int) {
	t.Helper()
	tasks := make([]Task, 0, count)
	for range count {
		child := exec.Command("/bin/sleep", "60")
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
		defer func() {
			_ = child.Process.Kill()
			_ = child.Wait()
		}()
		pid := child.Process.Pid
		for deadline := time.Now().Add(2 * time.Second); ; {
			wait, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/wchan")
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(wait), "nanosleep") {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("owned timer %d did not enter its kernel wait: %s", pid, wait)
			}
			time.Sleep(time.Millisecond)
		}
		if err := unix.Kill(pid, unix.SIGSTOP); err != nil {
			t.Fatal(err)
		}
		var status unix.WaitStatus
		if actual, err := unix.Wait4(pid, &status, unix.WUNTRACED, nil); err != nil || actual != pid || !status.Stopped() {
			t.Fatal("owned timer stop did not settle", actual, status, err)
		}
		stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
		if err != nil {
			t.Fatal(err)
		}
		fields := strings.Fields(string(stat[strings.LastIndex(string(stat), ") ")+2:]))
		if len(fields) < 20 {
			t.Fatal("owned timer stat is incomplete")
		}
		ticks, err := strconv.ParseUint(fields[19], 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		tasks = append(tasks, Task{PID: pid, StartTicks: ticks})
	}
	read := Read
	if runtime.GOARCH == "arm64" {
		read = ReadWithRegisters
	}
	observation, err := read(tasks)
	if err != nil {
		t.Fatal("native reader rejected a realistic exact PID set", err)
	}
	if len(observation.States) != count || !observation.ProgramReleased ||
		!observation.MapReleased || observation.ProgramID == 0 || observation.MapID != 0 {
		t.Fatal("native reader lost tasks or leaked its exact objects", observation)
	}
	expected := make(map[int]uint64, count)
	for _, task := range tasks {
		expected[task.PID] = task.StartTicks
	}
	for _, state := range observation.States {
		birth, present := expected[state.PID]
		if !present || state.StartTicks != birth || !state.RelativeTimer || state.Clock != 1 || state.ExpiresNS <= 0 {
			t.Fatal("native reader changed exact task membership, birth or timer", state)
		}
		delete(expected, state.PID)
	}
	if len(expected) != 0 {
		t.Fatal("native reader omitted an owned timer", expected)
	}
	t.Logf("%d exact owned timers observed; native program %d released", count, observation.ProgramID)
}
