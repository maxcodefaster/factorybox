//go:build linux

package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestEpochStopNative(t *testing.T) {
	if os.Getenv("EPOCH_STOP_ACTOR") == "1" {
		runtime.LockOSThread()
		ready := make(chan bool, 2)
		for range 2 {
			go func() { runtime.LockOSThread(); ready <- true; select {} }()
		}
		<-ready
		<-ready
		handshake := os.NewFile(3, "native actor ready")
		fmt.Fprintln(handshake, "ready")
		handshake.Close()
		for {
			time.Sleep(time.Hour)
		}
	}
	if os.Getenv("EPOCH_STOP_NATIVE") != "1" {
		t.Skip("native cgroup2 private-container test only")
	}
	root := "/sys/fs/cgroup"
	data, err := os.ReadFile(root + "/cgroup.procs")
	if err != nil || !strings.Contains("\n"+string(data), fmt.Sprintf("\n%d\n", os.Getpid())) {
		t.Fatalf("controller is outside the bounded private root: %s: %v", data, err)
	}
	memory, err := os.ReadFile(root + "/memory.max")
	limit, parseErr := strconv.ParseUint(strings.TrimSpace(string(memory)), 10, 64)
	if err != nil || parseErr != nil || limit > 256<<20 {
		t.Fatalf("private root memory bound: %s %v %v", memory, err, parseErr)
	}
	startActor := func(group string) *exec.Cmd {
		t.Helper()
		read, write, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		defer read.Close()
		command := exec.Command(os.Args[0], "-test.run=^TestEpochStopNative$")
		command.Env = append(os.Environ(), "EPOCH_STOP_ACTOR=1")
		command.ExtraFiles = []*os.File{write}
		command.Stdout, command.Stderr = os.Stdout, os.Stderr
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		write.Close()
		t.Cleanup(func() { _ = command.Process.Kill(); _ = command.Wait() })
		if text, err := bufio.NewReader(read).ReadString('\n'); err != nil || text != "ready\n" {
			t.Fatalf("actor readiness: %q %v", text, err)
		}
		if group != "" {
			if err := os.WriteFile(group+"/cgroup.procs", []byte(strconv.Itoa(command.Process.Pid)), 0600); err != nil {
				t.Fatal(err)
			}
		}
		return command
	}
	freeze := func(group string) {
		t.Helper()
		if err := os.WriteFile(group+"/cgroup.freeze", []byte("1"), 0600); err != nil {
			t.Fatal(err)
		}
		end := time.Now().Add(3 * time.Second)
		for {
			data, err := os.ReadFile(group + "/cgroup.events")
			if err == nil && strings.Contains(string(data), "frozen 1\n") {
				return
			}
			if time.Now().After(end) {
				t.Fatalf("freeze confirmation: %s %v", data, err)
			}
			time.Sleep(time.Millisecond)
		}
	}
	killed := func(command *exec.Cmd) {
		t.Helper()
		err := command.Wait()
		var exited *exec.ExitError
		if !errors.As(err, &exited) || !exited.ProcessState.Sys().(syscall.WaitStatus).Signaled() ||
			exited.ProcessState.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
			t.Fatalf("actor did not terminate through SIGKILL: %v", err)
		}
	}
	domain := root + "/epoch-domain-control"
	if err := os.Mkdir(domain, 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.WriteFile(domain+"/cgroup.freeze", []byte("0"), 0600); _ = os.Remove(domain) })
	control := startActor(domain)
	freeze(domain)
	if err := os.WriteFile(domain+"/cgroup.kill", []byte("1"), 0600); err != nil {
		t.Fatalf("domain kill control: %v", err)
	}
	killed(control)
	if err := os.Remove(domain); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(criuOwnedSourceCgroup, 0700); err != nil {
		t.Fatal(err)
	}
	leaf := criuOwnedSourceCgroup + "/actor-leaf"
	t.Cleanup(func() {
		_ = os.WriteFile(criuOwnedSourceCgroup+"/cgroup.freeze", []byte("0"), 0600)
		_ = os.Remove(leaf)
		_ = os.Remove(criuOwnedSourceCgroup)
	})
	if err := os.WriteFile(criuOwnedSourceCgroup+"/cgroup.type", []byte("threaded"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(leaf, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(leaf+"/cgroup.type", []byte("threaded"), 0600); err != nil {
		t.Fatal(err)
	}
	first, second, foreign := startActor(criuOwnedSourceCgroup), startActor(leaf), startActor("")
	freeze(criuOwnedSourceCgroup)
	err = os.WriteFile(criuOwnedSourceCgroup+"/cgroup.kill", []byte("1"), 0600)
	if !errors.Is(err, syscall.EOPNOTSUPP) {
		t.Fatalf("threaded negative control expected EOPNOTSUPP, got %v", err)
	}
	retained := make(map[int]string)
	for _, command := range []*exec.Cmd{first, second} {
		entries, err := os.ReadDir(fmt.Sprintf("/proc/%d/task", command.Process.Pid))
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			tid, err := strconv.Atoi(filepath.Base(entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			stamp, err := criuSysboxThreadStart(tid)
			if err != nil {
				t.Fatal(err)
			}
			retained[tid] = stamp
		}
	}
	stamp, err := criuSysboxThreadStart(foreign.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	retained[foreign.Process.Pid] = stamp
	if count, err := criuEpochStopFrozenSource(retained); err == nil || count != 0 {
		t.Fatalf("foreign thread accepted or partial stop before rejection: %d %v", count, err)
	}
	for _, command := range []*exec.Cmd{first, second, foreign} {
		if err := command.Process.Signal(syscall.Signal(0)); err != nil {
			t.Fatalf("rejected plan affected an actor: %v", err)
		}
	}
	delete(retained, foreign.Process.Pid)
	if count, err := criuEpochStopFrozenSource(retained); err != nil || count != 2 {
		t.Fatalf("frozen pidfd source termination: count=%d err=%v", count, err)
	}
	killed(first)
	killed(second)
	if err := foreign.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("unrelated actor affected: %v", err)
	}
	if err := criuSysboxFrozenKernel(); err == nil {
		t.Fatal("empty source was still reported populated")
	}
	if err := os.WriteFile(criuOwnedSourceCgroup+"/cgroup.freeze", []byte("0"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(leaf); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(criuOwnedSourceCgroup); err != nil {
		t.Fatal(err)
	}
	t.Logf("domain kill passed; threaded cgroup.kill EOPNOTSUPP reproduced; foreign plan rejected before signals; two whole process groups/%d exact TIDs stopped through pidfds; unrelated actor alive", len(retained))
}
