//go:build linux && amd64

package lifecycle

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"factorybox/internal/timerstate"
	"golang.org/x/sys/unix"
)

// This experiment operates only on an exact frozen single-thread fixture and
// its already verified private continuation page. Application text is unchanged.
// The production CLI does not contain this activation command or test switch.
func TestNativeTimerKernelActivationChild(t *testing.T) {
	if os.Getenv("FACTORYBOX_TIMER_KERNEL_CHILD") != "1" {
		t.Skip("owned kernel activation experiment child only")
	}
	if err := nativeTimerKernelActivationChild(); err != nil {
		t.Fatal(err)
	}
}

func nativeTimerKernelActivationChild() (resultErr error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	request, anchors, sealed, err := readCaptureTimerRequest()
	defer anchors.close()
	defer sealed.Close()
	if err != nil {
		return err
	}
	gate := os.NewFile(8, "kernel activation experiment gate")
	defer gate.Close()
	payload := os.NewFile(9, "read-only native experiment code")
	defer payload.Close()
	code, err := io.ReadAll(io.LimitReader(payload, 4097))
	if err != nil || len(code) != 323 || sum(code) != "02f23238267d39195a106632b38e3b1c4ba58fa6d7d95adeae531e5ad553e15f" {
		return fmt.Errorf("experimental native code differs: %v", err)
	}
	parentBirth, err := processBirth(os.Getppid())
	if err != nil {
		return err
	}
	birth, err := processBirth(unix.Gettid())
	if err != nil {
		return err
	}
	ready := timerMutatorReady{request.Guard.Token, os.Getppid(), parentBirth, os.Getpid(), unix.Gettid(), birth}
	data, err := json.Marshal(ready)
	if err != nil {
		return err
	}
	if _, err := gate.Write(data); err != nil {
		return err
	}
	packet, err := captureGuardRead(int(gate.Fd()), 3000)
	if err != nil || string(packet) != "prepare:"+request.Guard.Token {
		return fmt.Errorf("activation experiment has no independent exit guard: %v", err)
	}
	threads, err := captureGuardThreads(request.Guard, anchors)
	if err != nil || len(threads) != 1 || threads[request.Guard.PID] != request.Guard.Start {
		return fmt.Errorf("activation experiment requires one exact original task: %v", err)
	}
	_, _, errno := unix.Syscall6(unix.SYS_PTRACE, unix.PTRACE_SEIZE, uintptr(request.Guard.PID), 0, unix.PTRACE_O_TRACESYSGOOD|unix.PTRACE_O_SUSPEND_SECCOMP, 0, 0)
	if errno != 0 {
		return errno
	}
	trace := timerTrace{pid: request.Guard.PID}
	defer func() {
		_, held := captureGuardHold(request.Guard, anchors)
		resultErr = errors.Join(resultErr, held)
		if trace.stopped && held == nil {
			if resultErr != nil && trace.maskChanged {
				resultErr = errors.Join(resultErr, timerMask(trace.pid, unix.PTRACE_SETSIGMASK, &trace.mask))
			}
			if !trace.changed {
				resultErr = errors.Join(resultErr, unix.PtraceSetRegs(trace.pid, &trace.registers))
			}
			resultErr = errors.Join(resultErr, unix.PtraceDetach(trace.pid))
		}
	}()
	events := make(chan os.Signal, 1)
	signal.Notify(events, unix.SIGCHLD)
	defer signal.Stop(events)
	if err := unix.PtraceInterrupt(trace.pid); err != nil {
		return err
	}
	if err := timerSetFrozen(request.Guard, anchors, false); err != nil {
		return err
	}
	status, err := timerWait(trace.pid, time.Now().Add(2*time.Second), events)
	if err != nil || !status.Stopped() || status.StopSignal() != unix.SIGTRAP {
		return fmt.Errorf("activation experiment trace stop: %v %v", status, err)
	}
	trace.stopped = true
	if err := unix.PtraceGetRegs(trace.pid, &trace.registers); err != nil {
		return err
	}
	if err := timerMask(trace.pid, unix.PTRACE_GETSIGMASK, &trace.mask); err != nil {
		return err
	}
	if trace.registers.Orig_rax != 271 || int64(trace.registers.Rax) != -514 {
		return fmt.Errorf("experiment did not stop an original private ppoll")
	}
	mappings, err := timerMappings(trace.pid)
	if err != nil {
		return err
	}
	var address uint64
	for _, mapping := range mappings {
		if mapping.start <= trace.registers.Rip && trace.registers.Rip < mapping.end && mapping.end-mapping.start == 4096 && mapping.permissions == "r-xp" && mapping.name == "" {
			address = mapping.start
			break
		}
	}
	if address == 0 {
		return fmt.Errorf("experiment instruction is outside its private RX page")
	}
	originalCode, err := timerMemoryRead(trace.pid, address, len(timerContinuationAMD64))
	if err != nil || !bytes.Equal(originalCode, timerContinuationAMD64) {
		return fmt.Errorf("experiment private original code changed: %v", err)
	}
	frame, err := timerMemoryRead(trace.pid, address+4096, 4096)
	if err != nil {
		return err
	}
	if string(frame[:8]) != "FBXTMR02" || binary.LittleEndian.Uint64(frame[8:]) != 2 || binary.LittleEndian.Uint64(frame[24:]) != 1 {
		return fmt.Errorf("experiment private native frame differs")
	}
	var original unix.PtraceRegs
	if err := binary.Read(bytes.NewReader(frame[128:344]), binary.LittleEndian, &original); err != nil {
		return err
	}
	if original.Cs != 0x33 || (original.Orig_rax != 230 && original.Orig_rax != 219) || original.Rdi != 1 || original.Rsi != 0 || original.R10 != binary.LittleEndian.Uint64(frame[32:]) {
		return fmt.Errorf("experiment original application ABI differs")
	}
	mask := binary.LittleEndian.Uint64(frame[104:])
	offset, err := timerOffset(trace.pid, 1)
	if err != nil {
		return fmt.Errorf("experiment cannot verify the original clock namespace offset: %v", err)
	}
	if err := captureGuardWrite(anchors.evidence, "timer-kernel-activation-intent.json", map[string]any{"token": request.Guard.Token, "address": address, "original_trace": trace.registers, "original_mask": mask, "frame": frame, "old_code_sha256": sum(originalCode), "new_code_sha256": sum(code)}); err != nil {
		return err
	}
	blocked := ^uint64(0) &^ (uint64(1)<<(uint(unix.SIGKILL)-1) | uint64(1)<<(uint(unix.SIGSTOP)-1))
	if err := timerMask(trace.pid, unix.PTRACE_SETSIGMASK, &blocked); err != nil {
		return err
	}
	trace.maskChanged = true
	// This is the runtime's own private page, never an application instruction.
	if _, err := unix.PtracePokeData(trace.pid, uintptr(address), code); err != nil {
		return err
	}
	trace.changed = true
	binary.LittleEndian.PutUint64(frame[96:], 0) // Native relative wait for both remainder forms.
	if _, err := unix.PtracePokeData(trace.pid, uintptr(address+4096), frame); err != nil {
		return err
	}
	actualCode, err := timerMemoryRead(trace.pid, address, len(code))
	if err != nil || !bytes.Equal(actualCode, code) {
		return fmt.Errorf("experiment private replacement code differs: %v", err)
	}
	actualFrame, err := timerMemoryRead(trace.pid, address+4096, len(frame))
	if err != nil || !bytes.Equal(actualFrame, frame) {
		return fmt.Errorf("experiment private replacement frame differs: %v", err)
	}
	r := trace.registers
	r.Rip, r.Rax, r.Orig_rax = address, 0, ^uint64(0)
	if err := unix.PtraceSetRegs(trace.pid, &r); err != nil {
		return err
	}
	entry := true
	stops := []map[string]any{}
	for count := 0; ; count++ {
		if count >= 8 {
			return fmt.Errorf("experiment did not reach native nanosleep entry")
		}
		if err := unix.PtraceSyscall(trace.pid, 0); err != nil {
			return err
		}
		trace.stopped = false
		status, err := timerWait(trace.pid, time.Now().Add(2*time.Second), events)
		if err != nil || !status.Stopped() || status.StopSignal() != unix.SIGTRAP|0x80 {
			return fmt.Errorf("experiment syscall stop: %v %v", status, err)
		}
		trace.stopped = true
		if err := unix.PtraceGetRegs(trace.pid, &r); err != nil {
			return err
		}
		stops = append(stops, map[string]any{"entry": entry, "number": r.Orig_rax, "rax": int64(r.Rax), "rip": r.Rip})
		if entry && r.Orig_rax == 230 {
			if int64(r.Rax) != -38 || r.Rip != address+170 || r.Rdi != 1 || r.Rsi != 0 || r.Rdx != address+4096+40 || r.R10 != original.R10 {
				return fmt.Errorf("experiment native syscall entry ABI differs")
			}
			break
		}
		if r.Orig_rax != 228 || entry && int64(r.Rax) != -38 || !entry && r.Rax != 0 {
			return fmt.Errorf("experiment encountered an unexpected syscall")
		}
		entry = !entry
	}
	// Restore the application mask at a kernel syscall-entry stop. No user
	// instruction can execute between this change and the real nanosleep.
	if err := timerMask(trace.pid, unix.PTRACE_SETSIGMASK, &mask); err != nil {
		return err
	}
	trace.maskChanged = false
	// Syscall tracing keeps a return or a signal-delivery stop from executing
	// any application instruction. Confirm an actual armed native kernel timer
	// before freezing and detaching; a frozen syscall-entry stop alone would
	// postpone the beginning of its relative duration until the later thaw.
	if err := unix.PtraceSyscall(trace.pid, 0); err != nil {
		return err
	}
	trace.stopped = false
	for deadline := time.Now().Add(time.Second); ; {
		data, readErr := os.ReadFile(fmt.Sprintf("/proc/%d/status", trace.pid))
		syscall, syscallErr := os.ReadFile(fmt.Sprintf("/proc/%d/syscall", trace.pid))
		fields := strings.Fields(string(syscall))
		if readErr == nil && syscallErr == nil && bytes.Contains(data, []byte("\nState:\tS ")) && len(fields) > 4 && fields[0] == "230" {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("experiment native timer never entered its running kernel wait: %s %v %v", syscall, readErr, syscallErr)
		}
		time.Sleep(time.Millisecond)
	}
	var interruptedAt unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &interruptedAt); err != nil {
		return err
	}
	if err := unix.PtraceInterrupt(trace.pid); err != nil {
		return err
	}
	status, err = timerWait(trace.pid, time.Now().Add(2*time.Second), events)
	if err != nil || !status.Stopped() || (status.StopSignal() != unix.SIGTRAP && status.StopSignal() != unix.SIGTRAP|0x80) {
		return fmt.Errorf("experiment native wait cannot be held before user execution: %v %v", status, err)
	}
	trace.stopped = true
	if err := unix.PtraceGetRegs(trace.pid, &r); err != nil {
		return err
	}
	if r.Orig_rax != 230 || int64(r.Rax) != -516 || r.Rip != address+170 {
		return fmt.Errorf("experiment armed native wait lost its exact restart boundary")
	}
	// nanosleep fills its restart block when interrupted. Read it only after
	// that stop; an active uninterrupted wait can retain an older restart block.
	start, err := strconv.ParseUint(request.Guard.Start, 10, 64)
	if err != nil {
		return err
	}
	observation, err := timerstate.Read([]timerstate.Task{{PID: trace.pid, StartTicks: start}})
	if err != nil || !observation.ProgramReleased || !observation.MapReleased || len(observation.States) != 1 {
		return fmt.Errorf("experiment cannot confirm its actual armed native kernel timer: %v", err)
	}
	armed := observation.States[0]
	if !armed.RelativeTimer || armed.Clock != 1 || armed.Remainder != original.R10 || armed.ExpiresNS <= 0 {
		return fmt.Errorf("experiment interrupted native timer differs: %+v", armed)
	}
	difference := armed.ExpiresNS + offset - int64(binary.LittleEndian.Uint64(frame[16:]))
	if difference < -int64(250*time.Millisecond) || difference > int64(250*time.Millisecond) {
		return fmt.Errorf("experiment armed timer differs from its original application deadline: %d ns", difference)
	}
	remaining, err := timerMemoryRead(trace.pid, original.R10, 16)
	if err != nil {
		return err
	}
	sec, nsec := binary.LittleEndian.Uint64(remaining[:8]), binary.LittleEndian.Uint64(remaining[8:])
	if sec > 12 || nsec >= 1e9 {
		return fmt.Errorf("experiment native interrupted remainder is invalid")
	}
	interruptionRemaining := int64(sec)*1e9 + int64(nsec)
	interruptionExpected := int64(binary.LittleEndian.Uint64(frame[16:])) - (interruptedAt.Nano() + offset)
	if delta := interruptionRemaining - interruptionExpected; delta < -int64(250*time.Millisecond) || delta > int64(250*time.Millisecond) {
		return fmt.Errorf("experiment native interruption-time remainder differs: %d ns", delta)
	}
	if err := timerSetFrozen(request.Guard, anchors, true); err != nil {
		return err
	}
	if err := unix.PtraceDetach(trace.pid); err != nil {
		return err
	}
	trace.stopped = false
	if err := captureGuardCaptureReturned(request.Guard, anchors); err != nil {
		return err
	}
	if err := captureGuardWrite(anchors.evidence, "timer-kernel-activation.json", map[string]any{"token": request.Guard.Token, "passed": true, "production_qualified": false, "syscall_entry_atomic_mask": true, "native_syscall": 230, "source_held": true, "kernel_timer_armed": true, "armed_observation": observation, "armed_deadline_difference_ns": difference, "native_restart_registers": r, "interruption_remaining_ns": interruptionRemaining, "interruption_expected_ns": interruptionExpected, "remainder_pointer": original.R10, "clock_namespace_offset_ns": offset, "stops": stops, "code_sha256": sum(code), "scope": "Test-only single-thread post-capture activation before first signal. No production pre-exposure integration, interrupted activation repair or production qualification."}); err != nil {
		return err
	}
	return captureGuardWrite(anchors.evidence, "timer-prepare-complete.json", map[string]any{"token": request.Guard.Token, "passed": true, "source_frozen": true, "tracers_detached": true})
}

func runNativeKernelActivationPrototype(t *testing.T, status boundedLossStatus, view View, queueBeforeThaw bool) map[string]any {
	t.Helper()
	if out, err := exec.Command("cc", "-nostdlib", "-static", "-Wl,--build-id=none,-Ttext=0,-e,timer_restart_entry", "/timer-kernel.S", "-o", "/output/timer-kernel.elf").CombinedOutput(); err != nil {
		t.Fatal("assemble kernel activation experiment", err, string(out))
	}
	if out, err := exec.Command("objcopy", "-O", "binary", "--only-section=.text", "/output/timer-kernel.elf", "/output/timer-kernel.bin").CombinedOutput(); err != nil {
		t.Fatal("extract kernel activation experiment", err, string(out))
	}
	code, err := os.Open("/output/timer-kernel.bin")
	if err != nil {
		t.Fatal(err)
	}
	defer code.Close()
	engine, err := Open(status.Lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	guard, err := engine.startCaptureGuardWithExecutable(view.Record, view.Runtime.PID, os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	finished := false
	defer func() {
		if !finished {
			if err := guard.finish(true); err != nil {
				t.Error("hold exact failed activation source", err)
			}
		}
	}()
	if err := guard.freeze(true); err != nil {
		t.Fatal(err)
	}
	if err := guard.capturePhase(true); err != nil {
		t.Fatal(err)
	}
	fd, err := unix.MemfdCreate("sealed native activation experiment", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		t.Fatal(err)
	}
	sealed := os.NewFile(uintptr(fd), "sealed native activation experiment")
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
	client, child := os.NewFile(uintptr(sockets[0]), "activation parent gate"), os.NewFile(uintptr(sockets[1]), "activation child gate")
	defer client.Close()
	defer child.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNativeTimerKernelActivationChild$", "-test.timeout=12s")
	command.Env = []string{"FACTORYBOX_TIMER_KERNEL_CHILD=1", "GOMAXPROCS=1", "GOMEMLIMIT=64MiB"}
	command.ExtraFiles = []*os.File{sealed, guard.anchor.group, guard.anchor.pid, guard.anchor.evidence, guard.anchor.root, child, code}
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
		t.Fatal("activation child readiness changed", string(packet), err)
	}
	parentBirth, err := processBirth(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if err := captureGuardWrite(guard.anchor.evidence, "capture-worker-intent.json", captureWorkerIntent{Token: guard.config.Token, PID: os.Getpid(), Start: parentBirth, SourcePID: view.Runtime.PID}); err != nil {
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
		t.Fatal("activation exit guard not armed", string(packet), err)
	}
	if _, err := client.Write([]byte("prepare:" + guard.config.Token)); err != nil {
		t.Fatal(err)
	}
	err = command.Wait()
	waited = true
	if err != nil {
		t.Fatal("native kernel activation experiment", err)
	}
	data, err = captureReadFile(guard.anchor.evidence, "timer-kernel-activation.json", timerPlanLimit)
	if err != nil {
		t.Fatal(err)
	}
	var proof map[string]any
	if err := json.Unmarshal(data, &proof); err != nil || proof["passed"] != true || proof["syscall_entry_atomic_mask"] != true {
		t.Fatal("activation experiment incomplete", string(data), err)
	}
	if queueBeforeThaw {
		fd, err := unix.PidfdOpen(view.Runtime.PID, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer unix.Close(fd)
		if err := captureGuardCaptureReturned(guard.config, guard.anchor); err != nil {
			t.Fatal("queued signal lacks its frozen untraced original source", err)
		}
		var sent unix.Timespec
		if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &sent); err != nil {
			t.Fatal(err)
		}
		if err := unix.PidfdSendSignal(fd, unix.SIGUSR1, nil, 0); err != nil {
			t.Fatal(err)
		}
		pending, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", view.Runtime.PID))
		if err != nil {
			t.Fatal(err)
		}
		var pendingMask uint64
		for _, line := range strings.Split(string(pending), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && (fields[0] == "SigPnd:" || fields[0] == "ShdPnd:") {
				value, err := strconv.ParseUint(fields[1], 16, 64)
				if err != nil {
					t.Fatal(err)
				}
				pendingMask |= value
			}
		}
		if pendingMask&(uint64(1)<<(uint(unix.SIGUSR1)-1)) == 0 {
			t.Fatal("original source did not retain the actual queued caught signal", string(pending))
		}
		proof["queued_before_thaw"] = true
		proof["queued_caught_signal"] = true
		proof["signal_sent_ns"] = sent.Nano()
		proof["queued_status"] = string(pending)
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
	return proof
}
