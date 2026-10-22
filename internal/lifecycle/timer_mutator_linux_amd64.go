//go:build linux && amd64

package lifecycle

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/signal"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"factorybox/internal/timerstate"
	"golang.org/x/sys/unix"
)

type timerTrace struct {
	pid                           int
	stopped, changed, maskChanged bool
	registers                     unix.PtraceRegs
	mask                          uint64
}

type timerPreparedTarget struct {
	State          timerstate.State `json:"state"`
	Original       unix.PtraceRegs  `json:"original"`
	Mask           uint64           `json:"mask"`
	Address        uint64           `json:"address"`
	Reused         bool             `json:"reused"`
	Frame          []byte           `json:"frame"`
	RequestAddress uint64           `json:"request_address"`
	RequestBytes   []byte           `json:"request_bytes"`
}

func timerMemoryRead(pid int, address uint64, length int) ([]byte, error) {
	if address > math.MaxInt64 || length < 0 || length > 8192 {
		return nil, fmt.Errorf("invalid timer memory read bound")
	}
	file, err := os.Open(fmt.Sprintf("/proc/%d/mem", pid))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data := make([]byte, length)
	n, err := file.ReadAt(data, int64(address))
	if err != nil || n != length {
		return nil, fmt.Errorf("short exact timer task memory read: %v", err)
	}
	return data, nil
}

func timerMask(pid int, request uintptr, mask *uint64) error {
	var pinned runtime.Pinner
	pinned.Pin(mask)
	defer pinned.Unpin()
	_, _, errno := unix.Syscall6(unix.SYS_PTRACE, request, uintptr(pid), 8, uintptr(unsafe.Pointer(mask)), 0, 0)
	if errno != 0 {
		return errno
	}
	return nil
}

func timerWait(pid int, deadline time.Time, events <-chan os.Signal) (unix.WaitStatus, error) {
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	for {
		var status unix.WaitStatus
		actual, err := unix.Wait4(pid, &status, unix.WALL|unix.WNOHANG, nil)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil || actual == pid {
			return status, err
		}
		select {
		case <-events:
		case <-timer.C:
			return status, fmt.Errorf("owned timer trace event exceeded its deadline")
		}
	}
}

func timerSetFrozen(config captureGuardConfig, anchors captureGuardAnchors, frozen bool) error {
	if err := captureGuardValidate(config, anchors); err != nil {
		return err
	}
	value, want := "0", uint64(0)
	if frozen {
		value, want = "1", 1
	}
	fd, err := unix.Openat(int(anchors.group.Fd()), "cgroup.freeze", unix.O_WRONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), "exact timer source freezer")
	_, writeErr := file.WriteString(value)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return err
	}
	for deadline := time.Now().Add(2 * time.Second); ; {
		if err := captureGuardValidate(config, anchors); err != nil {
			return err
		}
		events, err := captureGuardEvents(anchors.group)
		if err != nil {
			return err
		}
		if events["populated"] == 1 && events["frozen"] == want {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("exact timer source freezer did not settle")
		}
		time.Sleep(time.Millisecond)
	}
}

func timerOffset(pid int, clock int32) (int64, error) {
	name := "monotonic"
	if clock == 7 {
		name = "boottime"
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/timens_offsets", pid))
	if err != nil {
		return 0, err
	}
	var found bool
	var result int64
	for _, row := range strings.Split(string(data), "\n") {
		fields := strings.Fields(row)
		if len(fields) != 3 || fields[0] != name {
			continue
		}
		seconds, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return 0, err
		}
		nanos, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil {
			return 0, err
		}
		if found || seconds > math.MaxInt64/1000000000 || seconds < math.MinInt64/1000000000 || nanos <= -1000000000 || nanos >= 1000000000 {
			return 0, fmt.Errorf("invalid bounded timer namespace offset")
		}
		base := seconds * 1000000000
		if (nanos > 0 && base > math.MaxInt64-nanos) || (nanos < 0 && base < math.MinInt64-nanos) {
			return 0, fmt.Errorf("timer namespace offset overflow")
		}
		found, result = true, base+nanos
	}
	if !found {
		return 0, fmt.Errorf("original timer clock namespace offset is absent")
	}
	return result, nil
}

type timerMapping struct {
	start, end        uint64
	permissions, name string
}

func timerMappings(pid int) ([]timerMapping, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/maps", pid))
	if err != nil {
		return nil, err
	}
	if len(data) > 16<<20 {
		return nil, fmt.Errorf("timer task mappings exceed bound")
	}
	var mappings []timerMapping
	for _, row := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		fields := strings.Fields(row)
		if len(fields) < 5 {
			return nil, fmt.Errorf("invalid timer task mapping")
		}
		bounds := strings.Split(fields[0], "-")
		if len(bounds) != 2 {
			return nil, fmt.Errorf("invalid timer task map range")
		}
		start, first := strconv.ParseUint(bounds[0], 16, 64)
		end, second := strconv.ParseUint(bounds[1], 16, 64)
		if first != nil || second != nil || start >= end {
			return nil, fmt.Errorf("invalid timer task map bounds")
		}
		name := ""
		if len(fields) > 5 {
			name = strings.Join(fields[5:], " ")
		}
		mappings = append(mappings, timerMapping{start, end, fields[1], name})
	}
	return mappings, nil
}

func prepareTimerTarget(trace *timerTrace, state timerstate.State) (timerPreparedTarget, error) {
	prepared := timerPreparedTarget{State: state, Original: trace.registers, Mask: trace.mask}
	r := trace.registers
	if r.Cs != 0x33 || int64(r.Rax) != -516 || (r.Orig_rax != 35 && r.Orig_rax != 230 && r.Orig_rax != 219) {
		return prepared, fmt.Errorf("relative timer CPU restart state differs from its kernel plan")
	}
	maps, err := timerMappings(trace.pid)
	if err != nil {
		return prepared, err
	}
	_ = maps // Mapping inventory also verifies the bounded native source view.
	if r.Rip < 2 {
		return prepared, fmt.Errorf("timer has no native syscall return instruction")
	}
	instruction, err := timerMemoryRead(trace.pid, r.Rip-2, 2)
	if err != nil || !bytes.Equal(instruction, []byte{0x0f, 0x05}) {
		return prepared, fmt.Errorf("timer native syscall instruction differs: %v", err)
	}
	if (r.Orig_rax == 35 || r.Orig_rax == 219) && r.Rdi >= 4096 && r.Rsi == state.Remainder {
		prepared.RequestAddress = r.Rdi
	} else if (r.Orig_rax == 230 || r.Orig_rax == 219) && (r.Rdi == 0 || r.Rdi == 1 || r.Rdi == 7 || r.Rdi == 11) && r.Rsi == 0 && r.R10 == state.Remainder {
		prepared.RequestAddress = r.Rdx
	} else {
		return prepared, fmt.Errorf("timer syscall arguments do not identify the exact original nanosleep")
	}
	prepared.RequestBytes, err = timerMemoryRead(trace.pid, prepared.RequestAddress, 16)
	if err != nil {
		return prepared, err
	}
	redzone, err := timerMemoryRead(trace.pid, r.Rsp-8, 8)
	if err != nil {
		return prepared, err
	}
	frame := make([]byte, 4096)
	copy(frame, "FBXTMR02")
	binary.LittleEndian.PutUint64(frame[8:], 2)
	offset, err := timerOffset(trace.pid, state.Clock)
	if err != nil {
		return prepared, err
	}
	if (offset > 0 && state.ExpiresNS > math.MaxInt64-offset) || (offset < 0 && state.ExpiresNS < math.MinInt64-offset) {
		return prepared, fmt.Errorf("timer namespace deadline overflow")
	}
	expires := state.ExpiresNS + offset
	if expires <= 0 {
		return prepared, fmt.Errorf("timer namespace deadline is not positive")
	}
	binary.LittleEndian.PutUint64(frame[16:], uint64(expires))
	binary.LittleEndian.PutUint64(frame[24:], uint64(state.Clock))
	binary.LittleEndian.PutUint64(frame[32:], state.Remainder)
	binary.LittleEndian.PutUint64(frame[104:], trace.mask)
	copy(frame[80:88], redzone)
	if state.Remainder == 0 {
		binary.LittleEndian.PutUint64(frame[96:], 1)
	}
	var encoded bytes.Buffer
	if err := binary.Write(&encoded, binary.LittleEndian, r); err != nil {
		return prepared, err
	}
	if encoded.Len() != 216 {
		return prepared, fmt.Errorf("unsupported native ptrace register size")
	}
	copy(frame[128:], encoded.Bytes())
	prepared.Frame = frame
	return prepared, nil
}

func timerRemote(trace *timerTrace, number uint64, args []uint64, events <-chan os.Signal) (uint64, error) {
	r := trace.registers
	r.Rip -= 2
	r.Rax, r.Orig_rax = number, math.MaxUint64
	registers := []*uint64{&r.Rdi, &r.Rsi, &r.Rdx, &r.R10, &r.R8, &r.R9}
	for index, value := range args {
		*registers[index] = value
	}
	if err := unix.PtraceSetRegs(trace.pid, &r); err != nil {
		return 0, err
	}
	trace.changed = true
	for phase := 0; phase < 2; phase++ {
		if err := unix.PtraceSyscall(trace.pid, 0); err != nil {
			return 0, err
		}
		trace.stopped = false
		status, err := timerWait(trace.pid, time.Now().Add(2*time.Second), events)
		if err != nil || !status.Stopped() || status.StopSignal() != unix.SIGTRAP|0x80 {
			return 0, fmt.Errorf("owned timer syscall step differs: %v %v", status, err)
		}
		trace.stopped = true
		if err := unix.PtraceGetRegs(trace.pid, &r); err != nil {
			return 0, err
		}
		if r.Rip != trace.registers.Rip || r.Orig_rax != number || (phase == 0 && int64(r.Rax) != -38) {
			return 0, fmt.Errorf("owned timer remote syscall boundary differs")
		}
	}
	if value := int64(r.Rax); value < 0 && value >= -4095 {
		return 0, unix.Errno(-value)
	}
	return r.Rax, nil
}

func chooseTimerAddress(pid int, reserved map[uint64]bool) (uint64, error) {
	maps, err := timerMappings(pid)
	if err != nil {
		return 0, err
	}
	for address := uint64(0x500000000000); address < 0x500080000000; address += 0x10000 {
		if reserved[address] {
			continue
		}
		available := true
		for _, mapping := range maps {
			if address < mapping.end && mapping.start < address+8192 {
				available = false
				break
			}
		}
		if available {
			return address, nil
		}
	}
	return 0, fmt.Errorf("owned timer continuation address range is exhausted")
}

func runTimerMutator() (resultErr error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if len(timerContinuationAMD64) == 0 || len(timerContinuationAMD64) > 4096 || sum(timerContinuationAMD64) != timerContinuationSHA {
		return fmt.Errorf("native timer continuation code changed")
	}
	request, anchors, sealed, err := readCaptureTimerRequest()
	defer anchors.close()
	defer sealed.Close()
	if err != nil {
		return err
	}
	plan, err := readTimerPlan(request, anchors)
	if err != nil {
		return err
	}
	gate := os.NewFile(8, "original timer mutator gate")
	defer gate.Close()
	parentStart, err := processBirth(os.Getppid())
	if err != nil {
		return err
	}
	birth, err := processBirth(unix.Gettid())
	if err != nil {
		return err
	}
	ready := timerMutatorReady{request.Guard.Token, os.Getppid(), parentStart, os.Getpid(), unix.Gettid(), birth}
	data, err := json.Marshal(ready)
	if err != nil {
		return err
	}
	if _, err := gate.Write(data); err != nil {
		return err
	}
	ack, err := captureGuardRead(int(gate.Fd()), 2000)
	if err != nil || string(ack) != "prepare:"+request.Guard.Token {
		return fmt.Errorf("timer mutator has no independent armed exit guard: %v", err)
	}
	actual, err := captureGuardThreads(request.Guard, anchors)
	if err != nil || !reflect.DeepEqual(actual, plan.Threads) {
		return fmt.Errorf("frozen timer thread inventory changed: %v", err)
	}
	traces := map[int]*timerTrace{}
	events := make(chan os.Signal, 1)
	signal.Notify(events, unix.SIGCHLD)
	defer signal.Stop(events)
	defer func() {
		if len(traces) == 0 {
			return
		}
		_, held := captureGuardHold(request.Guard, anchors)
		resultErr = errors.Join(resultErr, held)
		for _, trace := range traces {
			if !trace.stopped {
				continue
			}
			if trace.maskChanged {
				resultErr = errors.Join(resultErr, timerMask(trace.pid, unix.PTRACE_SETSIGMASK, &trace.mask))
			}
			if !trace.changed {
				resultErr = errors.Join(resultErr, unix.PtraceSetRegs(trace.pid, &trace.registers))
			}
			if held == nil {
				resultErr = errors.Join(resultErr, unix.PtraceDetach(trace.pid))
			}
		}
	}()
	var tids []int
	for tid := range plan.Threads {
		tids = append(tids, tid)
	}
	sort.Ints(tids)
	for _, tid := range tids {
		_, _, errno := unix.Syscall6(unix.SYS_PTRACE, unix.PTRACE_SEIZE, uintptr(tid), 0, unix.PTRACE_O_TRACESYSGOOD|unix.PTRACE_O_SUSPEND_SECCOMP, 0, 0)
		if errno != 0 {
			return errno
		}
		traces[tid] = &timerTrace{pid: tid}
		if err := unix.PtraceInterrupt(tid); err != nil {
			return err
		}
	}
	if err := timerSetFrozen(request.Guard, anchors, false); err != nil {
		return err
	}
	deadline := time.Now().Add(3 * time.Second)
	for _, tid := range tids {
		status, err := timerWait(tid, deadline, events)
		if err != nil || !status.Stopped() || status.StopSignal() != unix.SIGTRAP {
			return fmt.Errorf("owned source did not enter exact timer trace stop: %v %v", status, err)
		}
		trace := traces[tid]
		trace.stopped = true
		if err := unix.PtraceGetRegs(tid, &trace.registers); err != nil {
			return err
		}
		if err := timerMask(tid, unix.PTRACE_GETSIGMASK, &trace.mask); err != nil {
			return err
		}
	}
	var targets []timerPreparedTarget
	reserved := map[uint64]bool{}
	for _, state := range plan.Targets {
		trace, exists := traces[state.PID]
		if !exists {
			return fmt.Errorf("timer target is outside original source inventory")
		}
		if err := timerNoPendingCaughtSignal(state.PID); err != nil {
			return err
		}
		target, err := prepareTimerTarget(trace, state)
		if err != nil {
			return err
		}
		if !target.Reused {
			target.Address, err = chooseTimerAddress(state.PID, reserved)
			if err != nil {
				return err
			}
		}
		reserved[target.Address] = true
		targets = append(targets, target)
	}
	// CPU state and predetermined no-replace addresses are durable before any
	// task register or mapping changes. A killed mutator is held by the guard
	// before its ptrace relationships can auto-detach.
	if err := captureGuardWrite(anchors.evidence, "timer-prepare-intent.json", map[string]any{"version": 1, "token": request.Guard.Token, "targets": targets}); err != nil {
		return err
	}
	for _, target := range targets {
		trace := traces[target.State.PID]
		blocked := ^uint64(0) &^ (uint64(1)<<(uint(unix.SIGKILL)-1) | uint64(1)<<(uint(unix.SIGSTOP)-1))
		if err := timerMask(trace.pid, unix.PTRACE_SETSIGMASK, &blocked); err != nil {
			return err
		}
		trace.maskChanged = true
		if !target.Reused {
			address, err := timerRemote(trace, unix.SYS_MMAP, []uint64{target.Address, 8192, unix.PROT_READ | unix.PROT_WRITE, unix.MAP_PRIVATE | unix.MAP_ANON | unix.MAP_FIXED_NOREPLACE, math.MaxUint64, 0}, events)
			if err != nil || address != target.Address {
				return fmt.Errorf("owned timer private mapping differs: %x %v", address, err)
			}
			payload := make([]byte, 8192)
			copy(payload, timerContinuationAMD64)
			copy(payload[4096:], target.Frame)
			if _, err := unix.PtracePokeData(trace.pid, uintptr(address), payload); err != nil {
				return err
			}
			actual, err := timerMemoryRead(trace.pid, address, 8192)
			if err != nil || !bytes.Equal(actual, payload) {
				return fmt.Errorf("timer private mapping verification failed: %v", err)
			}
			if _, err := timerRemote(trace, unix.SYS_MPROTECT, []uint64{address, 4096, unix.PROT_READ | unix.PROT_EXEC}, events); err != nil {
				return err
			}
			actual, err = timerMemoryRead(trace.pid, target.RequestAddress, 16)
			if err != nil || !bytes.Equal(actual, target.RequestBytes) {
				return fmt.Errorf("original timer request changed: %v", err)
			}
		}
		r := trace.registers
		r.Rip, r.Rax, r.Orig_rax = target.Address, 0, math.MaxUint64
		if err := unix.PtraceSetRegs(trace.pid, &r); err != nil {
			return err
		}
		trace.changed = true
	}
	if err := timerSetFrozen(request.Guard, anchors, true); err != nil {
		return err
	}
	for _, target := range targets {
		trace := traces[target.State.PID]
		var blocked uint64
		if err := timerMask(trace.pid, unix.PTRACE_GETSIGMASK, &blocked); err != nil {
			return err
		}
		want := ^uint64(0) &^ (uint64(1)<<(uint(unix.SIGKILL)-1) | uint64(1)<<(uint(unix.SIGSTOP)-1))
		if blocked != want {
			return fmt.Errorf("timer continuation lost its blocked entry mask")
		}
		// The original mask is in the private frame. Keep entry blocked until
		// ppoll atomically installs that mask while waiting. A pending caught
		// signal must produce EINTR, including after checkpoint/restore.
		trace.maskChanged = false
	}
	for _, tid := range tids {
		if err := unix.PtraceDetach(tid); err != nil {
			return err
		}
		delete(traces, tid)
	}
	if err := captureGuardCaptureReturned(request.Guard, anchors); err != nil {
		return err
	}
	return captureGuardWrite(anchors.evidence, "timer-prepare-complete.json", map[string]any{"version": 1, "token": request.Guard.Token, "passed": true,
		"source_frozen": true, "tracers_detached": true, "targets": targets, "code_sha256": sum(timerContinuationAMD64), "private_bytes_per_new_target": 8192})
}
