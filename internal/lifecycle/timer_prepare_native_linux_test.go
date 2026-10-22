//go:build linux

package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"

	"factorybox/internal/testtrace"
	"golang.org/x/sys/unix"
)

// This exists only in the native test binary. It does not change the delivered
// CLI, CRIU or the original required regression's invocation of that CLI.
func prepareTimerNativeCapture() error {
	if len(os.Args) != 2 || os.Args[1] != "_capture" {
		return nil
	}
	marker, err := os.ReadFile("/output/timer-prepare-request.json")
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || len(marker) > 4096 {
		return fmt.Errorf("read native timer preparation request: %v", err)
	}
	var want struct {
		PID       int    `json:"pid"`
		Start     string `json:"start"`
		RuntimeID string `json:"runtime_id"`
		StubSHA   string `json:"stub_sha256"`
	}
	if err := json.Unmarshal(marker, &want); err != nil || !digest.MatchString(want.StubSHA) {
		return fmt.Errorf("invalid native timer preparation request: %v", err)
	}
	duplicate := func(fd int) (*os.File, error) {
		copy, err := unix.FcntlInt(uintptr(fd), unix.F_DUPFD_CLOEXEC, 8)
		if err != nil {
			return nil, err
		}
		return os.NewFile(uintptr(copy), "native duplicate of capture descriptor"), nil
	}
	requestFile, err := duplicate(3)
	if err != nil {
		return err
	}
	defer requestFile.Close()
	seals, err := unix.FcntlInt(requestFile.Fd(), unix.F_GET_SEALS, 0)
	if err != nil || seals&captureRequestSeals != captureRequestSeals {
		return fmt.Errorf("native capture request is not sealed: %v", err)
	}
	data := make([]byte, 64<<10)
	n, err := requestFile.ReadAt(data, 0)
	if n == 0 || n == len(data) {
		return fmt.Errorf("native sealed request absent or exceeds bound: %v", err)
	}
	var request captureRequest
	if err := json.Unmarshal(data[:n], &request); err != nil {
		return err
	}
	var anchors captureGuardAnchors
	defer anchors.close()
	for fd, target := range map[int]**os.File{4: &anchors.group, 5: &anchors.pid, 6: &anchors.evidence, 7: &anchors.root} {
		*target, err = duplicate(fd)
		if err != nil {
			return err
		}
	}
	// CaptureWorkerCommand retains the original descriptors and request offset.
	if err := captureGuardValidate(request.Guard, anchors); err != nil {
		return err
	}
	if want.PID != request.Guard.PID || want.Start != request.Guard.Start || want.RuntimeID != request.Guard.Source.RuntimeID {
		return fmt.Errorf("native timer request differs from sealed original source")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/usr/bin/python3", "/timer-prepare.py")
	command.Stdin = bytes.NewReader(marker)
	command.ExtraFiles = []*os.File{anchors.group}
	var diagnostic bytes.Buffer
	command.Stderr = &diagnostic
	output, err := command.Output()
	if err != nil {
		return fmt.Errorf("native timer preparation: %w: %s", err, diagnostic.String())
	}
	return os.WriteFile("/output/timer-prepare-result.json", output, 0644)
}

// Seize without interrupting a running wait. Observe natural signal/exit
// boundaries only, after both source capture and fork have already completed.
func observeTimerNativeExits(pids []int) (_ []map[string]any, resultErr error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	waiter := testtrace.New("natural timer exit observation exceeded its deadline")
	defer waiter.Close()
	fds := map[int]int{}
	defer func() {
		for pid, fd := range fds {
			if resultErr != nil {
				_ = unix.PidfdSendSignal(fd, unix.SIGKILL, nil, 0)
				_, _ = waiter.Wait(pid, time.Now().Add(time.Second))
			}
			unix.Close(fd)
		}
	}()
	for _, pid := range pids {
		fd, err := unix.PidfdOpen(pid, 0)
		if err != nil {
			return nil, err
		}
		fds[pid] = fd
		_, _, errno := unix.Syscall6(unix.SYS_PTRACE, unix.PTRACE_SEIZE, uintptr(pid), 0, unix.PTRACE_O_TRACEEXIT, 0, 0)
		if errno != 0 {
			return nil, errno
		}
	}
	deadline := time.Now().Add(16 * time.Second)
	var events []map[string]any
	for _, pid := range pids {
		for count := 0; ; count++ {
			if count >= 8 {
				return events, fmt.Errorf("too many natural timer trace events")
			}
			status, err := waiter.Wait(pid, deadline)
			if err != nil {
				return events, err
			}
			event := map[string]any{"pid": pid, "wait_status": uint32(status), "observed_at": time.Now().UTC()}
			if status.Exited() || status.Signaled() {
				event["exit_status"], event["signal"] = status.ExitStatus(), status.Signal().String()
				events = append(events, event)
				unix.Close(fds[pid])
				delete(fds, pid)
				break
			}
			if !status.Stopped() {
				return events, fmt.Errorf("unexpected natural timer wait status")
			}
			event["stop_signal"], event["trap_cause"] = status.StopSignal().String(), status.TrapCause()
			var registers unix.PtraceRegs
			if err := unix.PtraceGetRegs(pid, &registers); err != nil {
				return events, err
			}
			event["registers"] = registers
			bytes := make([]byte, 16)
			if n, err := unix.PtracePeekData(pid, uintptr(registers.Rip), bytes); err == nil {
				event["instruction_bytes"] = fmt.Sprintf("%x", bytes[:n])
			}
			signal := int(status.StopSignal())
			if status.TrapCause() == unix.PTRACE_EVENT_EXIT {
				message, err := unix.PtraceGetEventMsg(pid)
				if err != nil {
					return events, err
				}
				event["exit_event"] = message
				signal = 0
			}
			events = append(events, event)
			if err := unix.PtraceCont(pid, signal); err != nil {
				return events, err
			}
		}
	}
	return events, nil
}
