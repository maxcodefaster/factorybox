//go:build linux

package lifecycle

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func stopRetainedGuardNative(t *testing.T, engine *Engine, relative string, controller, tracer int) (retainedCaptureArm, func()) {
	t.Helper()
	var arm retainedCaptureArm
	if err := engine.readCheckpointJSON(relative+"/guard/criu-retained-arm.json", &arm); err != nil ||
		arm.Version != 2 || arm.ControllerPID != controller || arm.TracerPID != tracer {
		t.Fatal("grant control lacks its exact original retained context", err)
	}
	fd, err := unix.PidfdOpen(arm.GuardPID, 0)
	if err != nil {
		t.Fatal(err)
	}
	birth, birthErr := processBirth(arm.GuardPID)
	actual, statErr := os.Stat(fmt.Sprintf("/proc/%d/exe", arm.GuardPID))
	expected, expectedErr := os.Stat("/factorybox")
	if birthErr != nil || statErr != nil || expectedErr != nil ||
		birth != arm.GuardStart || !os.SameFile(actual, expected) {
		unix.Close(fd)
		t.Fatal("grant control cannot pin the original Factorybox guard")
	}
	if err := unix.PidfdSendSignal(fd, unix.SIGSTOP, nil, 0); err != nil {
		unix.Close(fd)
		t.Fatal(err)
	}
	resume := func() {
		if fd >= 0 {
			if err := unix.PidfdSendSignal(fd, unix.SIGCONT, nil, 0); err != nil && !errors.Is(err, unix.ESRCH) {
				t.Error("resume only the pinned original guard", err)
			}
			unix.Close(fd)
			fd = -1
		}
	}
	t.Cleanup(resume)
	ready := false
	defer func() {
		if !ready {
			resume()
		}
	}()
	deadline := time.Now().Add(time.Second)
	for {
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", arm.GuardPID))
		if err == nil && strings.Contains(string(data), "State:\tT") {
			ready = true
			return arm, resume
		}
		if time.Now().After(deadline) {
			t.Fatal("pinned original guard did not enter its stopped state", err)
		}
		time.Sleep(time.Millisecond)
	}
}

func probeRetainedGrantsNative(t *testing.T, engine *Engine, relative string, group *os.File, arm retainedCaptureArm) {
	t.Helper()
	evidence := filepath.Join(engine.config.Root, relative, "guard")
	requestPath := filepath.Join(evidence, "criu-retained-request.json")
	defer os.Remove(requestPath)
	deadline := time.Now().Add(5 * time.Second)
	for {
		birth, err := processBirth(arm.WorkerPID)
		if err != nil || birth != arm.WorkerStart {
			if _, err := os.Stat(filepath.Join(evidence, "capture-network-rollback.json")); err == nil {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("grant control did not observe the original failed worker's completed rollback")
		}
		time.Sleep(time.Millisecond)
	}
	helper, err := unix.PidfdOpen(arm.HelperPID, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(helper)
	if birth, err := processBirth(arm.HelperPID); err != nil || birth != arm.HelperStart {
		t.Fatal("grant control lost the original retained helper")
	}
	assertHeld := func(name string) {
		t.Helper()
		poll := []unix.PollFd{{Fd: int32(helper), Events: unix.POLLIN}}
		n, err := unix.Poll(poll, 100)
		if err != nil || n != 0 {
			t.Fatal("retained helper exited while testing a refused grant", name, err)
		}
		events, err := captureGuardEvents(group)
		if err != nil || events["populated"] != 1 || events["frozen"] != 1 {
			t.Fatal("refused grant changed the original source hold", name, err)
		}
		maps, err := os.ReadFile(fmt.Sprintf("/proc/%d/maps", arm.SourcePID))
		if err != nil || !strings.Contains(string(maps), "CRIUMFD") {
			t.Fatal("retained helper cured the original source under a refused grant", name, err)
		}
		for _, file := range []string{"criu-retained-result.json", "capture-process-restored.json", "guard-capture-aborted.json"} {
			if _, err := os.Stat(filepath.Join(evidence, file)); !os.IsNotExist(err) {
				t.Fatal("refused grant acquired a cure or release receipt", name, file, err)
			}
		}
	}
	assertHeld("absent")
	cases := []struct {
		name   string
		change func(*retainedCaptureRequest)
	}{
		{"nonce", func(r *retainedCaptureRequest) { r.Nonce = strings.Repeat("0", 64) }},
		{"guard", func(r *retainedCaptureRequest) { r.GuardPID++ }},
		{"controller", func(r *retainedCaptureRequest) { r.ControllerPID++ }},
		{"version", func(r *retainedCaptureRequest) { r.Version++ }},
		{"token", func(r *retainedCaptureRequest) { r.Token = strings.Repeat("d", 32) }},
		{"trailing", func(r *retainedCaptureRequest) {}},
		{"duplicate", func(r *retainedCaptureRequest) {}},
		{"partial", func(r *retainedCaptureRequest) {}},
		{"mode", func(r *retainedCaptureRequest) {}},
		{"hardlink", func(r *retainedCaptureRequest) {}},
		{"symlink", func(r *retainedCaptureRequest) {}},
	}
	refused := []string{"absent"}
	for _, control := range cases {
		grant := retainedCaptureRequest{Version: 1, Token: strings.Repeat("c", 32),
			ControllerPID: arm.ControllerPID, GuardPID: arm.GuardPID, Nonce: arm.Nonce}
		control.change(&grant)
		data, err := json.Marshal(grant)
		if err != nil {
			t.Fatal(err)
		}
		if control.name == "trailing" {
			data = append(data, []byte("\n{}")...)
		}
		if control.name == "duplicate" {
			data = append(data[:len(data)-1], []byte(",\"guard_pid\":1}")...)
		}
		data = append(data, '\n')
		if control.name == "partial" {
			data = data[:len(data)-1]
		}
		temporary := requestPath + ".control"
		if err := os.WriteFile(temporary, data, 0600); err != nil {
			t.Fatal(err)
		}
		switch control.name {
		case "mode":
			if err := os.Chmod(temporary, 0640); err != nil {
				t.Fatal(err)
			}
		case "hardlink":
			if err := os.Link(temporary, temporary+".alias"); err != nil {
				t.Fatal(err)
			}
			defer os.Remove(temporary + ".alias")
		case "symlink":
			if err := os.Rename(temporary, temporary+".target"); err != nil {
				t.Fatal(err)
			}
			defer os.Remove(temporary + ".target")
			if err := os.Symlink(filepath.Base(temporary)+".target", temporary); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Rename(temporary, requestPath); err != nil {
			t.Fatal(err)
		}
		assertHeld(control.name)
		if err := os.Remove(requestPath); err != nil {
			t.Fatal(err)
		}
		refused = append(refused, control.name)
	}
	data, err := json.Marshal(map[string]any{"passed": true, "refused": refused,
		"source_held": true, "helper_live": true, "guard_stopped": true})
	if err != nil || os.WriteFile("/output/interruption-grant-controls.json", append(data, '\n'), 0600) != nil {
		t.Fatal("save physical retained-grant refusal controls", err)
	}
	t.Log("absent and eleven invalid grants left the live retained helper idle and the original source held")
}
