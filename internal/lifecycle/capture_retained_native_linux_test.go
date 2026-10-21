//go:build linux

package lifecycle

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"factorybox/internal/testtrace"
	"golang.org/x/sys/unix"
)

// Stop the exact retained helper at its first source-freezer open after the
// grant loop. The pinned C candidate reaches this only after accepting the
// original live guard's grant. No production pause hook or instruction patch.
func stopRetainedCureNative(t *testing.T, arm retainedCaptureArm, group *os.File, resumeGuard func()) func() {
	t.Helper()
	fd, err := unix.PidfdOpen(arm.HelperPID, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	birth, birthErr := processBirth(arm.HelperPID)
	actual, actualErr := os.Stat(fmt.Sprintf("/proc/%d/exe", arm.HelperPID))
	expected, expectedErr := os.Stat("/out/criu")
	if birthErr != nil || birth != arm.HelperStart || actualErr != nil || expectedErr != nil ||
		!os.SameFile(actual, expected) {
		t.Fatal("cannot pin original retained cure helper")
	}
	want, err := cgroupFDIdentity(int(group.Fd()))
	if err != nil {
		t.Fatal("pin original source cgroup", err)
	}
	return testtrace.HoldCure(t, arm.HelperPID, fd,
		testtrace.Cgroup{Device: want.Device, Inode: want.Inode, MountID: want.MountID}, resumeGuard)
}

func retainedPagesWrittenNative(images string, virtualPID int) bool {
	// Read the pinned CRIU pagemap_head, then inspect that process's own page
	// image. An earlier parent's completed pages must not trigger a child kill.
	file, err := os.Open(filepath.Join(images, fmt.Sprintf("pagemap-%d.img", virtualPID)))
	if err != nil {
		return false
	}
	var prefix [64]byte
	n, _ := file.ReadAt(prefix[:], 0)
	file.Close()
	if n < 14 || binary.LittleEndian.Uint32(prefix[:4]) != 0x54564319 ||
		binary.LittleEndian.Uint32(prefix[4:8]) != 0x56084025 {
		return false
	}
	size := int(binary.LittleEndian.Uint32(prefix[8:12]))
	if size < 2 || size > 16 || n < 12+size || prefix[12] != 8 {
		return false
	}
	id, length := binary.Uvarint(prefix[13 : 12+size])
	if length <= 0 || length+1 != size || id == 0 {
		return false
	}
	info, err := os.Stat(filepath.Join(images, fmt.Sprintf("pages-%d.img", id)))
	return err == nil && info.Size() >= 8<<20
}

func stopRetainedDumpNative(t *testing.T, pid, pidfd int, images string, activePID, virtualPID int) func() {
	t.Helper()
	pinned, err := unix.Dup(pidfd)
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("FACTORYBOX_CRIU_EXIT_HOLD") == "1" {
		finish := func() {
			if pinned >= 0 {
				_ = unix.PidfdSendSignal(pinned, unix.SIGKILL, nil, 0)
				if err := testtrace.WaitDead(pinned, time.Now().Add(2*time.Second)); err != nil {
					t.Error(err)
				}
				unix.Close(pinned)
				pinned = -1
			}
		}
		held := false
		defer func() {
			if !held {
				finish()
			}
		}()
		guard := filepath.Join(filepath.Dir(images), "guard")
		for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
			var arm retainedCaptureArm
			data, err := os.ReadFile(filepath.Join(guard, "criu-retained-arm.json"))
			if err == nil && json.Unmarshal(data, &arm) == nil && arm.ExitHoldArmed &&
				arm.TracerPID == pid && arm.ActivePID == activePID && arm.ActiveVirtualPID == virtualPID &&
				retainedPagesWrittenNative(images, virtualPID) {
				stopped, err := testtrace.PageHeld(guard, filepath.Base(filepath.Dir(images)),
					pid, arm.HelperPID, activePID, virtualPID, arm.Nonce)
				if err != nil {
					t.Fatal(err)
				}
				if stopped {
					held = true
					return finish
				}
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatal("owned CRIU did not acknowledge its active process page-image fault boundary")
	}
	waiter := testtrace.New("exact CRIU trace event exceeded its test deadline")
	if err := unix.PtraceSeize(pid); err != nil {
		waiter.Close()
		unix.Close(pinned)
		t.Fatal("seize only the pinned owned CRIU for exact fault injection", err)
	}
	active := true
	finish := func() {
		if active {
			_ = unix.PidfdSendSignal(pinned, unix.SIGKILL, nil, 0)
			status, err := waiter.Wait(pid, time.Now().Add(2*time.Second))
			if err != nil || (!status.Exited() && !status.Signaled()) {
				t.Error("reap only the pinned CRIU trace relationship", err, status)
			}
			unix.Close(pinned)
			waiter.Close()
			active = false
		}
	}
	held := false
	defer func() {
		if !held {
			finish()
		}
	}()
	if err := unix.PtraceInterrupt(pid); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	status, err := waiter.Wait(pid, deadline)
	if err != nil || !status.Stopped() {
		t.Fatal("owned CRIU did not enter the initial test trace stop", err, status)
	}
	if err := unix.PtraceSetOptions(pid, unix.PTRACE_O_TRACESYSGOOD); err != nil {
		t.Fatal(err)
	}
	for {
		var arm retainedCaptureArm
		data, err := os.ReadFile(filepath.Join(filepath.Dir(images), "guard/criu-retained-arm.json"))
		armed := err == nil && json.Unmarshal(data, &arm) == nil &&
			arm.TracerPID == pid && arm.ActivePID == activePID && arm.ActiveVirtualPID == virtualPID
		if armed && retainedPagesWrittenNative(images, virtualPID) {
			held = true
			return finish
		}
		signal := int(status.StopSignal())
		if signal == int(unix.SIGTRAP) || signal == int(unix.SIGTRAP)|0x80 {
			signal = 0
		}
		if err := unix.PtraceSyscall(pid, signal); err != nil {
			t.Fatal(err)
		}
		status, err = waiter.Wait(pid, deadline)
		if err != nil || !status.Stopped() {
			t.Fatal("owned CRIU exited before its process page-image fault boundary", err, status)
		}
	}
}

func pinRetainedChildNative(t *testing.T, parent, nested int) (int, string, int) {
	t.Helper()
	paths, err := filepath.Glob(fmt.Sprintf("/proc/%d/task/*/children", parent))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		children, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, value := range strings.Fields(string(children)) {
			pid, err := strconv.Atoi(value)
			if err != nil || pid <= 1 {
				t.Fatal("owned process tree contains an invalid child identity", err)
			}
			fd, err := unix.PidfdOpen(pid, 0)
			if err != nil {
				continue
			}
			status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
			matched := false
			for _, line := range strings.Split(string(status), "\n") {
				fields := strings.Fields(line)
				if len(fields) > 1 && fields[0] == "NSpid:" {
					matched = fields[len(fields)-1] == strconv.Itoa(nested)
				}
			}
			birth, birthErr := processBirth(pid)
			if err == nil && matched && birthErr == nil {
				return pid, birth, fd
			}
			unix.Close(fd)
		}
	}
	t.Fatal("cannot pin the original owned process tree child")
	return 0, "", -1
}

func stopRetainedGuardNative(t *testing.T, engine *Engine, relative string, controller, tracer int) (retainedCaptureArm, func()) {
	t.Helper()
	var arm retainedCaptureArm
	if err := engine.readCheckpointJSON(relative+"/guard/criu-retained-arm.json", &arm); err != nil ||
		arm.Version != 3 || arm.ControllerPID != controller || arm.TracerPID != tracer {
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
		maps, err := os.ReadFile(fmt.Sprintf("/proc/%d/maps", arm.ActivePID))
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
