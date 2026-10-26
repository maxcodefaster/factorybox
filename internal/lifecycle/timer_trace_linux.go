//go:build linux && (amd64 || arm64)

package lifecycle

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

type timerTrace struct {
	pid                           int
	stopped, changed, maskChanged bool
	registers                     unix.PtraceRegs
	mask                          uint64
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

func timerDetachedCaptureReturned(config captureGuardConfig, anchors captureGuardAnchors) error {
	// Detach wakes a ptrace-stopped task so it can acknowledge the cgroup
	// freezer again. The request stays set, but frozen can briefly become 0.
	// Observe that acknowledgement without repairing a changed freeze request.
	for deadline := time.Now().Add(2 * time.Second); ; {
		if err := captureGuardValidate(config, anchors); err != nil {
			return err
		}
		requested, err := captureReadFile(anchors.group, "cgroup.freeze", 2)
		if err != nil {
			return err
		}
		if !bytes.Equal(requested, []byte("1\n")) {
			return fmt.Errorf("timer completion freeze request changed: %q", requested)
		}
		events, err := captureGuardEvents(anchors.group)
		frozen, hasFrozen := events["frozen"]
		populated, hasPopulated := events["populated"]
		if err != nil || !hasFrozen || !hasPopulated || populated != 1 || frozen > 1 {
			return fmt.Errorf("detached timer source has invalid freezer state: %v: %v", events, err)
		}
		if frozen == 1 {
			err := captureGuardCaptureReturned(config, anchors)
			var pending *captureFreezerPending
			if err == nil || !errors.As(err, &pending) {
				return err
			}
			// A later detached thread can withdraw the aggregate frozen
			// acknowledgement between the two real reads. Retry only that
			// exact transition under the unchanged deadline. Every retry
			// revalidates identity and the still-set original freeze request.
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("detached timer source freezer did not settle")
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
