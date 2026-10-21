//go:build linux

package devx_test

import (
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"testing"
	"time"

	"factorybox/internal/testtrace"
	"golang.org/x/sys/unix"
)

func TestNativeFaultWaitCost(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_FAULT_WAIT") != "1" {
		t.Skip("requires the owned native ptrace fixture")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	waiter := testtrace.New("owned CRIU trace deadline expired")
	defer waiter.Close()
	// Use a single-threaded producer: tracing only a Go program's leader
	// would silently miss syscalls executed by its other runtime threads.
	command := exec.Command("/usr/bin/python3", "-S", "-c",
		"import os\nfor _ in range(1000): os.getpid()\n")
	command.SysProcAttr = &syscall.SysProcAttr{Ptrace: true}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer command.Process.Release()
	waited := false
	defer func() {
		if !waited {
			command.Process.Kill()
			waiter.Wait(command.Process.Pid, time.Now().Add(2*time.Second))
		}
	}()
	started := time.Now()
	deadline := started.Add(10 * time.Second)
	status, err := waiter.Wait(command.Process.Pid, deadline)
	if err != nil || !status.Stopped() {
		t.Fatal("owned producer did not reach its initial exec stop", status, err)
	}
	if err := unix.PtraceSetOptions(command.Process.Pid, unix.PTRACE_O_TRACESYSGOOD); err != nil {
		t.Fatal(err)
	}
	stops := 0
	for status.Stopped() {
		deliver := int(status.StopSignal())
		if deliver == int(unix.SIGTRAP) || deliver == int(unix.SIGTRAP)|0x80 {
			deliver = 0
		}
		if err := unix.PtraceSyscall(command.Process.Pid, deliver); err != nil {
			t.Fatal(err)
		}
		status, err = waiter.Wait(command.Process.Pid, deadline)
		if err != nil {
			t.Fatal("owned producer did not finish within its diagnostic bound", err)
		}
		stops++
	}
	waited = status.Exited() || status.Signaled()
	elapsed := time.Since(started)
	t.Logf("actual syscall stops=%d elapsed=%s", stops, elapsed)
	if !status.Exited() || status.ExitStatus() != 0 || stops < 2000 {
		t.Fatal("native wait diagnostic missed actual producer syscalls", status, stops)
	}
	if elapsed > time.Second {
		t.Fatalf("native trace waiting exceeds its 1s diagnostic budget: %s for %d stops", elapsed, stops)
	}
}

func TestNativeFaultWaitDeadline(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_FAULT_WAIT") != "1" {
		t.Skip("requires the owned native ptrace fixture")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	waiter := testtrace.New("owned CRIU trace deadline expired")
	defer waiter.Close()
	command := exec.Command("/bin/sleep", "30")
	command.SysProcAttr = &syscall.SysProcAttr{Ptrace: true}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer command.Process.Release()
	defer func() {
		command.Process.Kill()
		status, err := waiter.Wait(command.Process.Pid, time.Now().Add(2*time.Second))
		if err != nil || !status.Signaled() {
			t.Error("owned sleeping producer was not reaped", status, err)
		}
	}()
	status, err := waiter.Wait(command.Process.Pid, time.Now().Add(2*time.Second))
	if err != nil || !status.Stopped() {
		t.Fatal("owned sleeping producer did not reach its initial exec stop", status, err)
	}
	if err := unix.PtraceCont(command.Process.Pid, 0); err != nil {
		t.Fatal(err)
	}
	// A real unrelated child notification must not count as this PID's
	// next stop, nor should waiting for another signal lose the deadline.
	if err := exec.Command("/bin/true").Run(); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	status, err = waiter.Wait(command.Process.Pid, started.Add(20*time.Millisecond))
	elapsed := time.Since(started)
	if err == nil || status != 0 || elapsed < 20*time.Millisecond || elapsed > time.Second {
		t.Fatal("native trace waiting lost its exact PID or absolute deadline", status, err, elapsed)
	}
	t.Logf("no-stop deadline elapsed=%s", elapsed)
}
