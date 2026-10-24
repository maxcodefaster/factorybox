//go:build linux && amd64

package lifecycle

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

func TestNativeTimerDetachCompletion(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_TIMER_DETACH") != "1" {
		t.Skip("owned private-cgroup ptrace/freezer regression only")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	var originalCPUs unix.CPUSet
	if err := unix.SchedGetaffinity(0, &originalCPUs); err != nil {
		t.Fatal(err)
	}
	var cpus []int
	for cpu := 0; cpu < 1024; cpu++ {
		if originalCPUs.IsSet(cpu) {
			cpus = append(cpus, cpu)
		}
	}
	if len(cpus) < 2 {
		t.Fatal("detach regression requires two allowed CPUs inside the bounded worker")
	}
	var controllerCPU, sourceCPU unix.CPUSet
	controllerCPU.Set(cpus[0])
	sourceCPU.Set(cpus[1])
	if err := unix.SchedSetaffinity(0, &controllerCPU); err != nil {
		t.Fatal(err)
	}
	defer unix.SchedSetaffinity(0, &originalCPUs)
	engine, source := nativeGuardFixture(t)
	group := guardGroupPath(source)
	t.Cleanup(func() { os.Remove(group) })
	// Throttle only this owned child. It makes the real post-detach freezer
	// acknowledgement observable without changing host settings or the hold.
	if err := os.WriteFile(group+"/cpu.max", []byte("1000 100000"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(group+"/cpu.weight", []byte("1"), 0600); err != nil {
		t.Fatal(err)
	}
	actor := filepath.Join(filepath.Dir(engine.config.Root), "counter-actor")
	compile := exec.Command("gcc", "-O2", "-Wall", "-Wextra", "-Werror", "-x", "c", "-o", actor, "-")
	compile.Stdin = strings.NewReader(`#include <signal.h>
#include <stdint.h>
#include <sys/mman.h>
int main(void) {
 volatile uint64_t *counter = mmap(0,4096,PROT_READ|PROT_WRITE,MAP_SHARED,3,0);
 if (counter == MAP_FAILED) return 2;
 raise(SIGSTOP);
 for (;;) ++*counter;
}
`)
	if output, err := compile.CombinedOutput(); err != nil {
		t.Fatal("compile exact counter actor", err, string(output))
	}
	fd, err := unix.MemfdCreate("owned-detach-counter", unix.MFD_CLOEXEC)
	if err != nil {
		t.Fatal(err)
	}
	file := os.NewFile(uintptr(fd), "owned counter memory")
	defer file.Close()
	if err := file.Truncate(4096); err != nil {
		t.Fatal(err)
	}
	memory, err := unix.Mmap(fd, 0, 4096, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Munmap(memory)
	counter := (*uint64)(unsafe.Pointer(&memory[0]))
	command := exec.Command(actor)
	command.ExtraFiles = []*os.File{file}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		os.WriteFile(group+"/cgroup.freeze", []byte("0"), 0600)
		command.Process.Kill()
		command.Wait()
	})
	pid := command.Process.Pid
	var status unix.WaitStatus
	if waited, err := unix.Wait4(pid, &status, unix.WUNTRACED, nil); err != nil || waited != pid || !status.Stopped() || status.StopSignal() != unix.SIGSTOP {
		t.Fatal("exact actor did not enter its initial stop", waited, status, err)
	}
	if err := unix.SchedSetaffinity(pid, &sourceCPU); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(group+"/cgroup.procs", []byte(strconv.Itoa(pid)), 0600); err != nil {
		t.Fatal(err)
	}
	guard, err := engine.startCaptureGuardWithExecutable(source, pid, os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	traced := false
	defer func() {
		if traced {
			unix.PtraceDetach(pid)
		}
		if err := captureNativeFreeze(source, true); err != nil {
			t.Error("retain exact controlled source for cleanup", err)
		}
		if guard.unsafe {
			if err := guard.capturePhase(false); err != nil {
				t.Error("finish controlled tracer transaction", err)
			}
		}
		if err := guard.finish(true); err != nil {
			t.Error("finish original source guard", err)
		}
	}()
	if err := guard.freeze(true); err != nil {
		t.Fatal(err)
	}
	if err := guard.capturePhase(true); err != nil {
		t.Fatal(err)
	}
	if err := command.Process.Signal(unix.SIGCONT); err != nil {
		t.Fatal(err)
	}
	// Separate owned busy children share only a different counter. Their CPU
	// affinity delays the source's kernel acknowledgement without letting its
	// application execute or blocking the tracer's own CPU. The outer worker
	// still enforces the same three-CPU ceiling.
	stressFD, err := unix.MemfdCreate("owned-detach-pressure", unix.MFD_CLOEXEC)
	if err != nil {
		t.Fatal(err)
	}
	stressFile := os.NewFile(uintptr(stressFD), "separate owned pressure memory")
	defer stressFile.Close()
	if err := stressFile.Truncate(4096); err != nil {
		t.Fatal(err)
	}
	for range 8 {
		pressure := exec.Command(actor)
		pressure.ExtraFiles = []*os.File{stressFile}
		if err := pressure.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { pressure.Process.Kill(); pressure.Wait() })
		if waited, err := unix.Wait4(pressure.Process.Pid, &status, unix.WUNTRACED, nil); err != nil || waited != pressure.Process.Pid || !status.Stopped() {
			t.Fatal("owned pressure child did not stop", waited, status, err)
		}
		if err := unix.SchedSetaffinity(pressure.Process.Pid, &sourceCPU); err != nil {
			t.Fatal(err)
		}
		if err := pressure.Process.Signal(unix.SIGCONT); err != nil {
			t.Fatal(err)
		}
	}
	for cycle := range 256 {
		if err := unix.PtraceSeize(pid); err != nil {
			t.Fatal(err)
		}
		traced = true
		if err := unix.PtraceInterrupt(pid); err != nil {
			t.Fatal(err)
		}
		if err := timerSetFrozen(guard.config, guard.anchor, false); err != nil {
			t.Fatal(err)
		}
		if waited, err := unix.Wait4(pid, &status, unix.WALL, nil); err != nil || waited != pid || !status.Stopped() || status.StopSignal() != unix.SIGTRAP {
			t.Fatal("exact actor trace stop changed", waited, status, err)
		}
		if err := timerSetFrozen(guard.config, guard.anchor, true); err != nil {
			t.Fatal(err)
		}
		before := atomic.LoadUint64(counter)
		if err := unix.PtraceDetach(pid); err != nil {
			t.Fatal(err)
		}
		traced = false
		// Invoke the actual completion first. A diagnostic proc/cgroup read
		// here would itself allow the short kernel transition to settle.
		if err := timerDetachedCaptureReturned(guard.config, guard.anchor); err != nil {
			request, _ := os.ReadFile(group + "/cgroup.freeze")
			t.Fatalf("completion after detach cycle %d: %v; freeze request=%q; counter before=%d after=%d", cycle, err, request, before, atomic.LoadUint64(counter))
		}
		events, err := captureGuardEvents(guard.anchor.group)
		if err != nil || events["populated"] != 1 || events["frozen"] != 1 {
			t.Fatal("completion did not retain the exact frozen source", events, err)
		}
		if after := atomic.LoadUint64(counter); after != before {
			t.Fatal("application executed while the exact freezer request was retained", before, after)
		}
	}
	t.Run("foreign-thaw-refused", func(t *testing.T) {
		if err := timerSetFrozen(guard.config, guard.anchor, false); err != nil {
			t.Fatal(err)
		}
		defer captureNativeFreeze(source, true)
		start := time.Now()
		if err := timerDetachedCaptureReturned(guard.config, guard.anchor); err == nil {
			t.Fatal("completion silently repaired or accepted an external thaw")
		}
		if time.Since(start) >= time.Second {
			t.Fatal("changed freeze request was not refused immediately")
		}
		data, err := os.ReadFile(group + "/cgroup.freeze")
		if err != nil || strings.TrimSpace(string(data)) != "0" {
			t.Fatal("completion rewrote the changed freeze request", string(data), err)
		}
	})
	t.Run("changed-source-refused", func(t *testing.T) {
		changed := guard.config
		changed.Start = "0"
		if err := timerDetachedCaptureReturned(changed, guard.anchor); err == nil {
			t.Fatal("completion accepted a different source birth")
		}
	})
	t.Run("live-tracer-refused", func(t *testing.T) {
		if err := unix.PtraceSeize(pid); err != nil {
			t.Fatal(err)
		}
		traced = true
		defer func() {
			unix.PtraceInterrupt(pid)
			if err := timerSetFrozen(guard.config, guard.anchor, false); err != nil {
				t.Error(err)
			}
			unix.Wait4(pid, &status, unix.WALL, nil)
			if err := timerSetFrozen(guard.config, guard.anchor, true); err != nil {
				t.Error(err)
			}
			if err := unix.PtraceDetach(pid); err != nil {
				t.Error(err)
			}
			traced = false
		}()
		if err := timerDetachedCaptureReturned(guard.config, guard.anchor); err == nil || !strings.Contains(err.Error(), "live tracer") {
			t.Fatal("completion accepted its live tracer or rejected for another reason", err)
		}
	})
	t.Log("256 actual detach completions without a diagnostic pre-read; unchanged application counter and three negative controls")
}
