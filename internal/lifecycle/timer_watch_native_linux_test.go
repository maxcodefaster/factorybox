//go:build linux && amd64

package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// A test-only tracer waits at a controlled boundary after seizing the exact
// source. The production source guard watches its owning OS thread. This lets
// the test kill a real tracer while the source cgroup is thawed, without a
// production fault switch or a timing race against the short real preparer.
func TestNativeTimerExitMutator(t *testing.T) {
	if os.Getenv("FACTORYBOX_TIMER_EXIT_MUTATOR") != "1" {
		t.Skip("owned timer exit guard child only")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	request, anchors, sealed, err := readCaptureTimerRequest()
	defer anchors.close()
	defer sealed.Close()
	if err != nil {
		t.Fatal(err)
	}
	gate := os.NewFile(8, "test timer tracer gate")
	defer gate.Close()
	parentBirth, err := processBirth(os.Getppid())
	if err != nil {
		t.Fatal(err)
	}
	birth, err := processBirth(unix.Gettid())
	if err != nil {
		t.Fatal(err)
	}
	ready := timerMutatorReady{request.Guard.Token, os.Getppid(), parentBirth, os.Getpid(), unix.Gettid(), birth}
	data, err := json.Marshal(ready)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gate.Write(data); err != nil {
		t.Fatal(err)
	}
	packet, err := captureGuardRead(int(gate.Fd()), 3000)
	if err != nil || string(packet) != "prepare:"+request.Guard.Token {
		t.Fatal("test tracer was not independently guarded", string(packet), err)
	}
	if err := unix.PtraceSeize(request.Guard.PID); err != nil {
		t.Fatal(err)
	}
	if err := unix.PtraceInterrupt(request.Guard.PID); err != nil {
		t.Fatal(err)
	}
	if err := timerSetFrozen(request.Guard, anchors, false); err != nil {
		t.Fatal(err)
	}
	// Wait4 uses only this exact owned source. A short poll avoids relying on
	// a Go SIGCHLD handler for this deliberately held test child.
	for deadline := time.Now().Add(3 * time.Second); ; {
		var status unix.WaitStatus
		pid, err := unix.Wait4(request.Guard.PID, &status, unix.WALL|unix.WNOHANG, nil)
		if err != nil {
			t.Fatal(err)
		}
		if pid == request.Guard.PID {
			if !status.Stopped() || status.StopSignal() != unix.SIGTRAP {
				t.Fatal("unexpected owned tracer stop", status)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("test tracer did not stop the exact source")
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := gate.Write([]byte("tracing:" + request.Guard.Token)); err != nil {
		t.Fatal(err)
	}
	packet, err = captureGuardRead(int(gate.Fd()), 5000)
	if err != nil || string(packet) != "exit:"+request.Guard.Token {
		t.Fatal("test tracer lost its controlled exit gate", err)
	}
	// Do not detach or freeze here. The independent production guard must
	// observe this OS thread's actual EXIT event before kernel auto-detach.
}

func TestNativeTimerExitGuard(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_TIMER_EXIT_GUARD") != "1" {
		t.Skip("owned native timer tracer exit fixture only")
	}
	status := boundedLossStatusNow(t)
	engine, err := Open(status.Lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	proofs := map[string]any{}
	for _, scenario := range []string{"normal", "sigkill"} {
		if !t.Run(scenario, func(t *testing.T) {
			view := timerReaderFixture(t, status, "timer-exit-"+scenario, "exec sleep 300", false)
			guard, err := engine.startCaptureGuardWithExecutable(view.Record, view.Runtime.PID, os.Args[0])
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				// Only this deliberately unmodified source may be thawed after
				// the exit assertions. No partial real preparation is repaired.
				if guard.unsafe {
					if err := guard.capturePhase(false); err != nil {
						t.Error("finish controlled unmodified trace", err)
					}
				}
				if err := guard.finish(true); err != nil {
					t.Error("finish exact timer test guard", err)
				}
			}()
			if err := captureNativeFreeze(view.Record, true); err != nil {
				t.Fatal(err)
			}
			if err := guard.capturePhase(true); err != nil {
				t.Fatal(err)
			}
			fd, err := unix.MemfdCreate("test sealed timer exit request", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
			if err != nil {
				t.Fatal(err)
			}
			sealed := os.NewFile(uintptr(fd), "test sealed timer exit request")
			defer sealed.Close()
			data, err := json.Marshal(captureRequest{Guard: guard.config})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := sealed.Write(data); err != nil {
				t.Fatal(err)
			}
			if _, err := unix.FcntlInt(sealed.Fd(), unix.F_ADD_SEALS, captureRequestSeals); err != nil {
				t.Fatal(err)
			}
			sockets, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
			if err != nil {
				t.Fatal(err)
			}
			client, child := os.NewFile(uintptr(sockets[0]), "test timer exit parent gate"), os.NewFile(uintptr(sockets[1]), "test timer exit child gate")
			defer client.Close()
			defer child.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNativeTimerExitMutator$", "-test.timeout=12s")
			command.Env = []string{"FACTORYBOX_TIMER_EXIT_MUTATOR=1", "GOMAXPROCS=1", "GOMEMLIMIT=64MiB"}
			command.ExtraFiles = []*os.File{sealed, guard.anchor.group, guard.anchor.pid, guard.anchor.evidence, guard.anchor.root, child}
			command.Stdout, command.Stderr = os.Stdout, os.Stderr
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			waited := false
			defer func() {
				if !waited {
					command.Process.Kill()
					command.Wait()
				}
			}()
			packet, err := captureGuardRead(int(client.Fd()), 3000)
			var ready timerMutatorReady
			if err != nil || json.Unmarshal(packet, &ready) != nil || ready.PID != command.Process.Pid || ready.ParentPID != os.Getpid() || ready.Token != guard.config.Token {
				t.Fatal("controlled tracer readiness changed", string(packet), err)
			}
			parentBirth, err := processBirth(os.Getpid())
			if err != nil {
				t.Fatal(err)
			}
			if err := captureGuardWrite(guard.anchor.evidence, "capture-worker-intent.json", captureWorkerIntent{
				Token: guard.config.Token, PID: os.Getpid(), Start: parentBirth, SourcePID: view.Runtime.PID}); err != nil {
				t.Fatal(err)
			}
			if err := captureGuardWrite(guard.anchor.evidence, "timer-mutator-ready.json", ready); err != nil {
				t.Fatal(err)
			}
			if _, err := guard.socket.Write([]byte("timer-watch:" + guard.config.Token)); err != nil {
				t.Fatal(err)
			}
			packet, err = captureGuardRead(int(guard.socket.Fd()), 3000)
			if err != nil || string(packet) != "timer-watched:"+guard.config.Token {
				t.Fatal("production exit guard did not arm", string(packet), err)
			}
			if _, err := client.Write([]byte("prepare:" + guard.config.Token)); err != nil {
				t.Fatal(err)
			}
			packet, err = captureGuardRead(int(client.Fd()), 3000)
			if err != nil || string(packet) != "tracing:"+guard.config.Token {
				t.Fatal("controlled tracer did not hold its real source", string(packet), err)
			}
			events, err := captureGuardEvents(guard.anchor.group)
			traceStatus, statusErr := os.ReadFile(fmt.Sprintf("/proc/%d/status", view.Runtime.PID))
			if err != nil || events["frozen"] != 0 || statusErr != nil || !strings.Contains(string(traceStatus), "TracerPid:\t"+strconv.Itoa(ready.TID)+"\n") {
				t.Fatal("source was not thawed and owned by the actual tracer", events, err, statusErr)
			}
			if scenario == "sigkill" {
				pidfd, err := unix.PidfdOpen(ready.PID, 0)
				if err != nil {
					t.Fatal(err)
				}
				err = unix.PidfdSendSignal(pidfd, unix.SIGKILL, nil, 0)
				unix.Close(pidfd)
				if err != nil {
					t.Fatal(err)
				}
			} else if _, err := client.Write([]byte("exit:" + guard.config.Token)); err != nil {
				t.Fatal(err)
			}
			err = command.Wait()
			waited = true
			if (scenario == "normal" && err != nil) || (scenario == "sigkill" && (err == nil || command.ProcessState.ExitCode() != -1)) {
				t.Fatal("controlled tracer did not really exit as requested", err)
			}
			if err := captureGuardCaptureReturned(guard.config, guard.anchor); err != nil {
				t.Fatal("auto-detached original source is not held and untraced", err)
			}
			data, err = captureReadFile(guard.anchor.evidence, "timer-mutator-exit-held.json", 4096)
			var receipt struct {
				Token    string `json:"token"`
				TID      int    `json:"mutator_tid"`
				Exit     int    `json:"exit_event"`
				Held     bool   `json:"source_held"`
				Complete bool   `json:"preparation_complete"`
			}
			if err != nil || json.Unmarshal(data, &receipt) != nil || receipt.Token != guard.config.Token || receipt.TID != ready.TID || !receipt.Held || receipt.Complete || receipt.Exit != map[string]int{"normal": 0, "sigkill": 9}[scenario] {
				t.Fatal("exit hold lacks its exact independent kernel event", string(data), err)
			}
			proofs[scenario] = map[string]any{"ready": ready, "exit_receipt": json.RawMessage(data), "source_pid": view.Runtime.PID,
				"source_start": guard.config.Start, "was_thawed_and_traced": true, "held_before_auto_detach": true, "no_preparation_claim": true}
		}) {
			return
		}
	}
	boundedLossSave(t, "timer-exit-guard.json", map[string]any{"passed": true, "production_qualified": false, "normal_exit": true, "actual_sigkill": true,
		"held_before_auto_detach": true, "no_preparation_claim": true, "cases": proofs,
		"scope": "Production exit guard against a controlled test-only tracer. Exact original source is thawed and ptrace-stopped before real normal/SIGKILL exit, then held and untraced. No real timer mutation rollback or controller-loss qualification."})
}
