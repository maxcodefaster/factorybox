package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
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

	"golang.org/x/sys/unix"
)

func descendantGuardNativeActor(t *testing.T) {
	socket := os.NewFile(3, "native RAM actor")
	defer socket.Close()
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		t.Fatal(err)
	}
	nonce, counter := hex.EncodeToString(bytes[:]), 0
	for {
		data, _ := json.Marshal(map[string]any{"nonce": nonce, "counter": counter})
		if _, err := socket.Write(data); err != nil {
			t.Fatal(err)
		}
		request, err := descendantGuardRead(3, -1)
		if err != nil {
			t.Fatal(err)
		}
		if len(request) == 0 {
			return
		}
		if string(request) != "advance" {
			t.Fatal("unexpected native actor request")
		}
		counter++
	}
}

func descendantGuardNativeController(t *testing.T) {
	source, _ := descendantOwnedLayout("copy-sysbox")
	pid, err := strconv.Atoi(os.Getenv("GUARD_NATIVE_SOURCE_PID"))
	if err != nil {
		t.Fatal(err)
	}
	mode := os.Getenv("GUARD_NATIVE_MODE")
	socket := os.NewFile(3, "native controller test socket")
	defer socket.Close()
	var guard *descendantGuard
	if mode != "off" {
		guard, err = descendantStartGuard(source, pid, os.Getenv("GUARD_NATIVE_EVIDENCE"))
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := descendantFreeze(source, true); err != nil {
		t.Fatal(err)
	}
	if mode == "hold" || mode == "hold-running" || mode == "helper-running" || mode == "normal" || mode == "trace" ||
		mode == "dual" || mode == "hold-after-operation" || mode == "single-returned-loss" {
		if err := guard.capturePhase(true); err != nil {
			t.Fatal(err)
		}
	}
	if mode == "dual" || mode == "hold-after-operation" || mode == "single-returned-loss" {
		if err := guard.captureOperationReturned(); err != nil {
			t.Fatal(err)
		}
		if !guard.unsafe {
			t.Fatal("intermediate capture return exposed an unguarded safe point")
		}
		if mode == "dual" {
			if err := guard.captureOperationReturned(); err != nil {
				t.Fatal(err)
			}
		}
		if mode == "single-returned-loss" {
			if err := guard.capturePhase(false); err != nil || guard.unsafe {
				t.Fatal("single completed writer did not reach its acknowledged safe point", err)
			}
		}
	}
	if mode == "hold-running" || mode == "helper-running" {
		if err := descendantFreeze(source, false); err != nil {
			t.Fatal(err)
		}
	}
	helper := 0
	if guard != nil {
		helper = guard.cmd.Process.Pid
	}
	ready, _ := json.Marshal(map[string]any{"ready": true, "helper_pid": helper,
		"single_writer_safe_ack": mode == "single-returned-loss"})
	if _, err := socket.Write(ready); err != nil {
		t.Fatal(err)
	}
	request, err := descendantGuardRead(3, 5000)
	if err != nil {
		t.Fatal(err)
	}
	switch string(request) {
	case "normal":
		if err := guard.capturePhase(false); err != nil {
			t.Fatal(err)
		}
		if err := descendantFreeze(source, false); err != nil {
			t.Fatal(err)
		}
		if err := guard.finish(false); err != nil {
			t.Fatal(err)
		}
	case "kill-helper":
		if err := guard.cmd.Process.Kill(); err != nil {
			t.Fatal(err)
		}
		end := time.Now().Add(time.Second)
		for guard.check() == nil && time.Now().Before(end) {
			time.Sleep(time.Millisecond)
		}
		if err := guard.check(); err == nil {
			t.Fatal("dead helper was not detected")
		}
		if err := guard.finish(true); err == nil {
			t.Fatal("helper failure was hidden by recovery")
		}
	case "reject-traced":
		if err := guard.capturePhase(false); err == nil {
			t.Fatal("capture with a live tracer was accepted")
		}
		if err := guard.finish(true); err == nil {
			t.Fatal("rejected capture was silently disarmed")
		}
		data, err := os.ReadFile(os.Getenv("GUARD_NATIVE_EVIDENCE") + "/guard-capture-rejected.json")
		var rejected map[string]any
		if err != nil || json.Unmarshal(data, &rejected) != nil ||
			rejected["error"] != "capture completion still has a live tracer" {
			t.Fatal("completion was rejected for a different reason", string(data), err)
		}
	default:
		t.Fatal("controller should have been killed", string(request))
	}
	if _, err := socket.Write([]byte(`{"handled":true}`)); err != nil {
		t.Fatal(err)
	}
}

func TestDescendantGuardNative(t *testing.T) {
	switch os.Getenv("GUARD_NATIVE_ROLE") {
	case "actor":
		descendantGuardNativeActor(t)
		return
	case "controller":
		descendantGuardNativeController(t)
		return
	case "tracer":
		runtime.LockOSThread()
		pid, err := strconv.Atoi(os.Getenv("GUARD_NATIVE_SOURCE_PID"))
		if err != nil {
			t.Fatal(err)
		}
		if err := unix.PtraceSeize(pid); err != nil {
			t.Fatal(err)
		}
		socket := os.NewFile(3, "native tracer socket")
		defer socket.Close()
		if _, err := socket.Write([]byte(`{"tracing":true}`)); err != nil {
			t.Fatal(err)
		}
		select {}
	}
	if os.Getenv("DESCENDANT_GUARD_NATIVE") != "1" {
		t.Skip("isolated existing-worker private-cgroup native test")
	}
	if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	root, err := os.ReadFile("/sys/fs/cgroup/cgroup.procs")
	if err != nil || !strings.Contains("\n"+string(root), fmt.Sprintf("\n%d\n", os.Getpid())) {
		t.Fatal("test must own a bounded private cgroup root", err)
	}
	maximum, err := os.ReadFile("/sys/fs/cgroup/memory.max")
	memory, parseErr := strconv.ParseUint(strings.TrimSpace(string(maximum)), 10, 64)
	if err != nil || parseErr != nil || memory > 256<<20 {
		t.Fatal("private native memory bound is missing", string(maximum), err, parseErr)
	}
	source, _ := descendantOwnedLayout("copy-sysbox")
	if _, err := os.Lstat(source.Cgroup); !os.IsNotExist(err) {
		t.Fatal("fixture must own absent cgroup", err)
	}
	if err := os.Mkdir(source.Cgroup, 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		os.WriteFile(source.Cgroup+"/cgroup.freeze", []byte("0"), 0600)
		os.Remove(source.Cgroup)
	})
	base := "/experiment/descendant"
	if _, err := os.Lstat(base); !os.IsNotExist(err) {
		t.Fatal("fixture must own absent evidence root", err)
	}
	if err := os.MkdirAll(base, 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(base) })
	start := func(t *testing.T, role string, extra ...string) (*exec.Cmd, *os.File) {
		t.Helper()
		fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
		if err != nil {
			t.Fatal(err)
		}
		parent, child := os.NewFile(uintptr(fds[0]), "parent test socket"), os.NewFile(uintptr(fds[1]), "child test socket")
		command := exec.Command(os.Args[0], "-test.run=^TestDescendantGuardNative$", "-test.timeout=15s")
		command.Env = append(os.Environ(), append([]string{"GUARD_NATIVE_ROLE=" + role, "GOMAXPROCS=1"}, extra...)...)
		command.ExtraFiles = []*os.File{child}
		command.Stdout, command.Stderr = os.Stdout, os.Stderr
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		child.Close()
		t.Cleanup(func() { parent.Close(); command.Process.Kill(); command.Wait() })
		return command, parent
	}
	read := func(t *testing.T, socket *os.File) map[string]any {
		t.Helper()
		data, err := descendantGuardRead(int(socket.Fd()), 3000)
		if err != nil {
			t.Fatal(err)
		}
		var value map[string]any
		if err := json.Unmarshal(data, &value); err != nil {
			t.Fatal("invalid native child response", string(data), err)
		}
		return value
	}
	actor, actorSocket := start(t, "actor")
	original := read(t, actorSocket)
	if err := os.WriteFile(source.Cgroup+"/cgroup.procs", []byte(strconv.Itoa(actor.Process.Pid)), 0600); err != nil {
		t.Fatal(err)
	}
	startTime, err := criuSysboxThreadStart(actor.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	foreign, foreignSocket := start(t, "actor")
	foreignOriginal := read(t, foreignSocket)
	counter := 0
	assertActors := func(t *testing.T) {
		t.Helper()
		stamp, err := criuSysboxThreadStart(actor.Process.Pid)
		if err != nil || stamp != startTime {
			t.Fatal("original source kernel process changed", err)
		}
		for _, socket := range []*os.File{actorSocket, foreignSocket} {
			if _, err := socket.Write([]byte("advance")); err != nil {
				t.Fatal(err)
			}
		}
		counter++
		a, b := read(t, actorSocket), read(t, foreignSocket)
		if a["nonce"] != original["nonce"] || b["nonce"] != foreignOriginal["nonce"] ||
			a["counter"] != float64(counter) || b["counter"] != float64(counter) {
			t.Fatal("RAM identity/counters changed or unrelated actor affected", a, b)
		}
		if err := foreign.Process.Signal(syscall.Signal(0)); err != nil {
			t.Fatal(err)
		}
	}
	reapHelper := func(t *testing.T, pid int) {
		t.Helper()
		if pid == 0 {
			return
		}
		end := time.Now().Add(time.Second)
		for {
			var status unix.WaitStatus
			found, err := unix.Wait4(pid, &status, unix.WNOHANG, nil)
			if found == pid {
				if !status.Exited() || status.ExitStatus() != 0 {
					t.Fatal("orphaned guard failed", status)
				}
				return
			}
			if err != nil || time.Now().After(end) {
				t.Fatal("orphaned guard did not exit/reap", found, err)
			}
			time.Sleep(time.Millisecond)
		}
	}
	for _, scenario := range []struct{ name, mode, evidence, outcome string }{
		{"baseline_controller_death_leaves_source_frozen", "off", base, ""},
		{"controller_death_thaws_original_safe_source", "guard", base + "/generation-1", "guard-outcome.json"},
		{"inflight_capture_is_held_for_reconciliation", "hold", base + "/generation-2", "guard-outcome.json"},
		{"controller_death_refreezes_temporarily_running_unsafe_source", "hold-running", base + "/generation-1/source-guard", "guard-outcome.json"},
		{"helper_death_is_detected_and_controller_recovers", "helper", base + "/post-resume-fork", "guard-controller-outcome.json"},
		{"helper_death_refreezes_temporarily_running_unsafe_source", "helper-running", base + "/generation-2/source-guard", "guard-controller-outcome.json"},
		{"live_ptrace_blocks_capture_completion_and_thaw", "trace", base + "/source-guard", "guard-controller-outcome.json"},
		{"two_capture_operations_share_one_guard_transaction", "dual", base + "/generation-1/controller-loss", "guard-outcome.json"},
		{"controller_death_after_intermediate_return_keeps_source_held", "hold-after-operation", base + "/generation-2/controller-loss", "guard-outcome.json"},
		{"controller_death_after_single_writer_ack_recovers_source", "single-returned-loss", base + "/post-resume-fork/controller-loss", "guard-outcome.json"},
		{"normal_disarm_preserves_running_source", "normal", base, "guard-outcome.json"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			if scenario.evidence != base {
				if err := os.Mkdir(scenario.evidence, 0700); err != nil {
					t.Fatal(err)
				}
			}
			command, socket := start(t, "controller", "GUARD_NATIVE_SOURCE_PID="+strconv.Itoa(actor.Process.Pid),
				"GUARD_NATIVE_MODE="+scenario.mode, "GUARD_NATIVE_EVIDENCE="+scenario.evidence)
			ready := read(t, socket)
			if ready["ready"] != true {
				t.Fatal("controller did not freeze the source", ready)
			}
			if scenario.mode == "single-returned-loss" && ready["single_writer_safe_ack"] != true {
				t.Fatal("controller was killed before the single writer's safe acknowledgement", ready)
			}
			if scenario.mode == "helper" || scenario.mode == "helper-running" || scenario.mode == "normal" ||
				scenario.mode == "trace" || scenario.mode == "dual" {
				request := "normal"
				var tracer *exec.Cmd
				if scenario.mode == "helper" || scenario.mode == "helper-running" {
					request = "kill-helper"
				} else if scenario.mode == "trace" {
					request = "reject-traced"
					var traceSocket *os.File
					tracer, traceSocket = start(t, "tracer", "GUARD_NATIVE_SOURCE_PID="+strconv.Itoa(actor.Process.Pid))
					if ready := read(t, traceSocket); ready["tracing"] != true {
						t.Fatal("native tracer was not attached", ready)
					}
				}
				if _, err := socket.Write([]byte(request)); err != nil {
					t.Fatal(err)
				}
				if handled := read(t, socket); handled["handled"] != true {
					t.Fatal("controller failed to handle request", handled)
				}
				if err := command.Wait(); err != nil {
					t.Fatal("surviving controller failed", err)
				}
				if tracer != nil {
					if err := tracer.Process.Kill(); err != nil {
						t.Fatal(err)
					}
					tracer.Wait()
				}
			} else {
				if err := command.Process.Kill(); err != nil {
					t.Fatal(err)
				}
				var killed *exec.ExitError
				if err := command.Wait(); !errors.As(err, &killed) ||
					killed.ProcessState.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
					t.Fatal("controller was not killed", err)
				}
				if scenario.mode != "off" {
					reapHelper(t, int(ready["helper_pid"].(float64)))
				}
			}
			data, err := os.ReadFile(source.Cgroup + "/cgroup.events")
			if err != nil {
				t.Fatal(err)
			}
			held := scenario.mode == "off" || scenario.mode == "hold" || scenario.mode == "hold-running" ||
				scenario.mode == "helper-running" || scenario.mode == "trace" || scenario.mode == "hold-after-operation"
			if strings.Contains(string(data), "frozen 1\n") != held {
				t.Fatal("unexpected source freeze outcome", string(data))
			}
			if scenario.outcome != "" {
				var outcome map[string]any
				data, err := os.ReadFile(filepath.Join(scenario.evidence, scenario.outcome))
				if err != nil || json.Unmarshal(data, &outcome) != nil || outcome["passed"] != true {
					t.Fatal("durable guard outcome is absent/failed", string(data), err)
				}
				if (scenario.mode == "hold" || scenario.mode == "hold-running" || scenario.mode == "helper-running" ||
					scenario.mode == "trace" || scenario.mode == "hold-after-operation") &&
					outcome["status"] != "original_source_held_for_capture_reconciliation" {
					t.Fatal("unsafe capture was silently resumed", outcome)
				}
				t.Log(string(data))
			}
			if held {
				if err := descendantFreeze(source, false); err != nil {
					t.Fatal("owned fixture recovery failed", err)
				}
			}
			assertActors(t)
		})
	}
	if !t.Failed() {
		t.Logf("original source PID/start and private RAM nonce retained through%d failure/disarm cases; unrelated actor independent", counter)
	}
}
