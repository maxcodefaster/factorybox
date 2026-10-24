//go:build linux

package lifecycle

import (
	"context"
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

func TestMain(m *testing.M) {
	if handled, err := RestoreWorkerCommand(os.Args[1:]); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	if handled, err := TimerWorkerCommand(os.Args[1:]); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	if err := prepareTimerNativeCapture(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if handled, err := CaptureWorkerCommand(os.Args[1:]); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	if handled, err := CaptureGuardCommand(os.Args[1:]); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	if os.Getenv("FACTORYBOX_CAPTURE_NESTED_TIME") == "future-thread" {
		// Keep Go's initial OS thread occupied by the test runner. The
		// disposable future-children namespace must live on a retireable
		// worker thread, rather than m0 which the runtime keeps alive.
		runtime.LockOSThread()
	}
	os.Exit(m.Run())
}

func guardGroupPath(source Record) string { return cgroupRoot + "/" + source.RuntimeID }

func nativeGuardSource(t *testing.T) (*Engine, Record) {
	t.Helper()
	engine, err := Open(Config{Root: os.Getenv("GUARD_NATIVE_ROOT"),
		Runtime: "/out/sysbox-runc", RuntimeSHA256: os.Getenv("FACTORYBOX_RUNTIME_SHA256")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { engine.Close() })
	source, err := engine.read("source")
	if err != nil {
		t.Fatal(err)
	}
	return engine, source
}

func nativeGuardFixture(t *testing.T) (*Engine, Record) {
	t.Helper()
	base, err := os.MkdirTemp("/var/lib/sysbox", "factorybox-lifecycle-guard-")
	if err != nil {
		t.Fatal(err)
	}
	bundle := base + "/bundle"
	if err := os.MkdirAll(bundle+"/rootfs", 0700); err != nil {
		t.Fatal(err)
	}
	input := []byte(`{"root":{"path":"rootfs"},"linux":{}}`)
	engine, err := Open(Config{Root: base + "/controller", Runtime: "/out/sysbox-runc",
		RuntimeSHA256: os.Getenv("FACTORYBOX_RUNTIME_SHA256")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { engine.Close() })
	source, err := engine.prepare("source", bundle, "objects/source", input)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.prepareCgroup(&source); err != nil {
		t.Fatal(err)
	}
	return engine, source
}

// The control case deliberately freezes without a guard. The guarded cases
// use the production method, including its original PID and cgroup handles.
func captureNativeFreeze(source Record, frozen bool) error {
	path := guardGroupPath(source)
	identity, err := cgroupIdentity(path)
	if err != nil || source.Cgroup == nil || identity != *source.Cgroup {
		return fmt.Errorf("native source cgroup changed: %v", err)
	}
	value, want := "0", uint64(0)
	if frozen {
		value, want = "1", 1
	}
	if err := os.WriteFile(path+"/cgroup.freeze", []byte(value), 0600); err != nil {
		return err
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		data, readErr := os.ReadFile(path + "/cgroup.events")
		events, parseErr := parseCgroupCounters(string(data))
		if readErr == nil && parseErr == nil && events["populated"] == 1 && events["frozen"] == want {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("native freeze not confirmed: %s: %v %v", data, readErr, parseErr)
		}
		time.Sleep(time.Millisecond)
	}
}

func captureGuardNativeActor(t *testing.T) {
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
		request, err := captureGuardRead(3, -1)
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

func captureGuardNativeController(t *testing.T) {
	engine, source := nativeGuardSource(t)
	pid, err := strconv.Atoi(os.Getenv("GUARD_NATIVE_SOURCE_PID"))
	if err != nil {
		t.Fatal(err)
	}
	mode := os.Getenv("GUARD_NATIVE_MODE")
	socket := os.NewFile(3, "native controller test socket")
	defer socket.Close()
	var guard *captureGuard
	if mode != "off" {
		guard, err = engine.startCaptureGuard(source, pid)
		if err != nil {
			t.Fatal(err)
		}
	}
	freeze := func(frozen bool) error {
		if guard != nil {
			return guard.freeze(frozen)
		}
		return captureNativeFreeze(source, frozen)
	}
	if err := freeze(true); err != nil {
		t.Fatal(err)
	}
	if mode == "hold" || mode == "hold-running" || mode == "helper-running" || mode == "normal" || mode == "trace" ||
		mode == "dual" || mode == "hold-after-operation" || mode == "single-returned-loss" ||
		mode == "foreign-mount" {
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
		if err := freeze(false); err != nil {
			t.Fatal(err)
		}
	}
	helper := 0
	if guard != nil {
		helper = guard.cmd.Process.Pid
	}
	evidence := ""
	if guard != nil {
		evidence = guard.config.Evidence
	}
	ready, _ := json.Marshal(map[string]any{"ready": true, "helper_pid": helper,
		"single_writer_safe_ack": mode == "single-returned-loss", "evidence": evidence})
	if _, err := socket.Write(ready); err != nil {
		t.Fatal(err)
	}
	request, err := captureGuardRead(3, 5000)
	if err != nil {
		t.Fatal(err)
	}
	switch string(request) {
	case "normal":
		if err := guard.capturePhase(false); err != nil {
			t.Fatal(err)
		}
		if err := freeze(false); err != nil {
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
	case "reject-traced", "reject-foreign-mount":
		if err := guard.capturePhase(false); err == nil {
			t.Fatal("capture with a live tracer was accepted")
		}
		if err := guard.finish(true); err == nil {
			t.Fatal("rejected capture was silently disarmed")
		}
		data, err := os.ReadFile(guard.config.Evidence + "/guard-capture-rejected.json")
		var rejected map[string]any
		expected := "capture completion still has a live tracer"
		if string(request) == "reject-foreign-mount" {
			expected = "capture completion lost the original source: source cgroup tree crosses a foreign mount: <nil>"
		}
		if err != nil || json.Unmarshal(data, &rejected) != nil ||
			rejected["error"] != expected {
			t.Fatal("completion was rejected for a different reason", string(data), err)
		}
	default:
		t.Fatal("controller should have been killed", string(request))
	}
	if _, err := socket.Write([]byte(`{"handled":true}`)); err != nil {
		t.Fatal(err)
	}
}

func TestCaptureGuardNative(t *testing.T) {
	switch os.Getenv("GUARD_NATIVE_ROLE") {
	case "actor":
		captureGuardNativeActor(t)
		return
	case "controller":
		captureGuardNativeController(t)
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
	if os.Getenv("FACTORYBOX_NATIVE_CAPTURE_GUARD") != "1" {
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
	engine, source := nativeGuardFixture(t)
	t.Cleanup(func() {
		os.WriteFile(guardGroupPath(source)+"/cgroup.freeze", []byte("0"), 0600)
		os.Remove(guardGroupPath(source))
	})
	start := func(t *testing.T, role string, extra ...string) (*exec.Cmd, *os.File) {
		t.Helper()
		fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
		if err != nil {
			t.Fatal(err)
		}
		parent, child := os.NewFile(uintptr(fds[0]), "parent test socket"), os.NewFile(uintptr(fds[1]), "child test socket")
		command := exec.Command(os.Args[0], "-test.run=^TestCaptureGuardNative$", "-test.timeout=15s")
		command.Env = append(os.Environ(), append([]string{"GUARD_NATIVE_ROLE=" + role,
			"GUARD_NATIVE_ROOT=" + engine.config.Root, "GOMAXPROCS=1"}, extra...)...)
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
		data, err := captureGuardRead(int(socket.Fd()), 3000)
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
	if err := os.WriteFile(guardGroupPath(source)+"/cgroup.procs", []byte(strconv.Itoa(actor.Process.Pid)), 0600); err != nil {
		t.Fatal(err)
	}
	startTime, err := processBirth(actor.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	foreign, foreignSocket := start(t, "actor")
	foreignOriginal := read(t, foreignSocket)
	counter := 0
	assertActors := func(t *testing.T) {
		t.Helper()
		stamp, err := processBirth(actor.Process.Pid)
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
	for _, scenario := range []struct{ name, mode, outcome string }{
		{"baseline_controller_death_leaves_source_frozen", "off", ""},
		{"controller_death_thaws_original_safe_source", "guard", "guard-outcome.json"},
		{"inflight_capture_is_held_for_reconciliation", "hold", "guard-outcome.json"},
		{"controller_death_refreezes_temporarily_running_unsafe_source", "hold-running", "guard-outcome.json"},
		{"helper_death_is_detected_and_controller_recovers", "helper", "guard-controller-outcome.json"},
		{"helper_death_refreezes_temporarily_running_unsafe_source", "helper-running", "guard-controller-outcome.json"},
		{"live_ptrace_blocks_capture_completion_and_thaw", "trace", "guard-controller-outcome.json"},
		{"foreign_cgroup_mount_blocks_capture_completion", "foreign-mount", "guard-controller-outcome.json"},
		{"two_capture_operations_share_one_guard_transaction", "dual", "guard-outcome.json"},
		{"controller_death_after_intermediate_return_keeps_source_held", "hold-after-operation", "guard-outcome.json"},
		{"controller_death_after_single_writer_ack_recovers_source", "single-returned-loss", "guard-outcome.json"},
		{"normal_disarm_preserves_running_source", "normal", "guard-outcome.json"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			command, socket := start(t, "controller", "GUARD_NATIVE_SOURCE_PID="+strconv.Itoa(actor.Process.Pid),
				"GUARD_NATIVE_MODE="+scenario.mode)
			ready := read(t, socket)
			if ready["ready"] != true {
				t.Fatal("controller did not freeze the source", ready)
			}
			if scenario.mode == "single-returned-loss" && ready["single_writer_safe_ack"] != true {
				t.Fatal("controller was killed before the single writer's safe acknowledgement", ready)
			}
			if scenario.mode == "helper" || scenario.mode == "helper-running" || scenario.mode == "normal" ||
				scenario.mode == "trace" || scenario.mode == "dual" || scenario.mode == "foreign-mount" {
				request := "normal"
				var tracer *exec.Cmd
				var mounted, foreign, target string
				if scenario.mode == "helper" || scenario.mode == "helper-running" {
					request = "kill-helper"
				} else if scenario.mode == "trace" {
					request = "reject-traced"
					var traceSocket *os.File
					tracer, traceSocket = start(t, "tracer", "GUARD_NATIVE_SOURCE_PID="+strconv.Itoa(actor.Process.Pid))
					if ready := read(t, traceSocket); ready["tracing"] != true {
						t.Fatal("native tracer was not attached", ready)
					}
				} else if scenario.mode == "foreign-mount" {
					request = "reject-foreign-mount"
					foreign = guardGroupPath(source) + "-foreign"
					target = guardGroupPath(source) + "/foreign-view"
					for _, directory := range []string{foreign, target} {
						if err := os.Mkdir(directory, 0700); err != nil {
							t.Fatal(err)
						}
					}
					t.Cleanup(func() {
						if mounted != "" {
							unix.Unmount(mounted, 0)
						}
						os.Remove(target)
						os.Remove(foreign)
					})
					if err := unix.Mount(foreign, target, "", unix.MS_BIND, ""); err != nil {
						t.Fatal(err)
					}
					mounted = target
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
				if mounted != "" {
					if err := unix.Unmount(mounted, 0); err != nil {
						t.Fatal(err)
					}
					mounted = ""
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
			data, err := os.ReadFile(guardGroupPath(source) + "/cgroup.events")
			if err != nil {
				t.Fatal(err)
			}
			held := scenario.mode == "off" || scenario.mode == "hold" || scenario.mode == "hold-running" ||
				scenario.mode == "helper-running" || scenario.mode == "trace" || scenario.mode == "hold-after-operation" ||
				scenario.mode == "foreign-mount"
			if strings.Contains(string(data), "frozen 1\n") != held {
				t.Fatal("unexpected source freeze outcome", string(data))
			}
			if scenario.outcome != "" {
				var outcome map[string]any
				evidence, ok := ready["evidence"].(string)
				if !ok || evidence == "" {
					t.Fatal("controller did not report its exact generated guard evidence")
				}
				data, err := os.ReadFile(filepath.Join(evidence, scenario.outcome))
				if err != nil || json.Unmarshal(data, &outcome) != nil || outcome["passed"] != true {
					t.Fatal("durable guard outcome is absent/failed", string(data), err)
				}
				if (scenario.mode == "hold" || scenario.mode == "hold-running" || scenario.mode == "helper-running" ||
					scenario.mode == "trace" || scenario.mode == "hold-after-operation" || scenario.mode == "foreign-mount") &&
					outcome["status"] != "original_source_held_for_capture_reconciliation" {
					t.Fatal("unsafe capture was silently resumed", outcome)
				}
				t.Log(string(data))
			}
			if held {
				if err := captureNativeFreeze(source, false); err != nil {
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

func TestNativeCaptureGuardOnSysbox(t *testing.T) {
	engine, bundle := nativeEngine(t, "sleep 300")
	ctx := context.Background()
	view, err := engine.Start(ctx, "guarded", bundle)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := engine.Delete(ctx, "guarded"); err != nil {
			t.Errorf("delete guarded source: %v", err)
		}
	})
	before, err := os.Stat(bundle + "/upper")
	if err != nil {
		t.Fatal(err)
	}
	beforeOwner := before.Sys().(*syscall.Stat_t)
	lock, err := engine.lock()
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	// Use the actual CLI helper entry point, while the kernel-actor failure
	// suite above also checks the test executable's re-exec entry point.
	guard, err := engine.startCaptureGuardWithExecutable(view.Record, view.Runtime.PID, "/factorybox")
	if err != nil {
		t.Fatal(err)
	}
	finished := false
	defer func() {
		if !finished {
			if err := guard.finish(true); err != nil {
				t.Errorf("recover guarded source after failure: %v", err)
			}
		}
	}()
	if err := guard.freeze(true); err != nil {
		t.Fatal(err)
	}
	if err := guard.capturePhase(true); err != nil {
		t.Fatal(err)
	}
	if err := guard.captureOperationReturned(); err != nil || !guard.unsafe {
		t.Fatalf("intermediate completion exposed an unguarded source: %v", err)
	}
	if err := guard.capturePhase(false); err != nil {
		t.Fatal(err)
	}
	if err := guard.freeze(false); err != nil {
		t.Fatal(err)
	}
	err = guard.finish(false)
	finished = true
	if err != nil {
		t.Fatal(err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	current, err := engine.Inspect(ctx, "guarded")
	if err != nil || current.Runtime == nil || current.Runtime.PID != view.Runtime.PID ||
		current.Runtime.Created != view.Runtime.Created || current.Runtime.Status != "running" {
		t.Fatalf("guard cycle changed the actual Sysbox process: %+v: %v", current, err)
	}
	after, err := os.Stat(bundle + "/upper")
	if err != nil {
		t.Fatal(err)
	}
	afterOwner := after.Sys().(*syscall.Stat_t)
	if !os.SameFile(before, after) || beforeOwner.Uid != afterOwner.Uid || beforeOwner.Gid != afterOwner.Gid {
		t.Fatal("raw cgroup freeze changed the Sysbox upper identity or mapped ownership")
	}
	var outcome struct {
		Status          string `json:"status"`
		Passed          bool   `json:"passed"`
		CaptureInFlight bool   `json:"capture_in_flight"`
	}
	data, err := os.ReadFile(guard.config.Evidence + "/guard-outcome.json")
	if err != nil || json.Unmarshal(data, &outcome) != nil || !outcome.Passed ||
		outcome.CaptureInFlight || outcome.Status != "source_running_guard_disarmed" {
		t.Fatalf("CLI guard did not durably disarm its actual source: %s: %v", data, err)
	}
	t.Logf("CLI guard retained Sysbox PID %d and overlay UID/GID %d/%d through raw freeze and acknowledged completion; no CRIU capture was performed",
		view.Runtime.PID, beforeOwner.Uid, beforeOwner.Gid)
}
