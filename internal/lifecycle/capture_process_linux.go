//go:build linux

package lifecycle

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

func processBirth(pid int) (string, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", err
	}
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 {
		return "", fmt.Errorf("missing thread command boundary")
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) < 20 || fields[0] == "Z" || fields[0] == "X" {
		return "", fmt.Errorf("source thread %d is absent or dead", pid)
	}
	start, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil || start == 0 {
		return "", fmt.Errorf("invalid source start time: %v", err)
	}
	return fields[19], nil
}

func parseCgroupCounters(data string) (map[string]uint64, error) {
	output := map[string]uint64{}
	for _, line := range strings.Split(strings.TrimSpace(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return nil, fmt.Errorf("invalid cgroup counter line: %q", line)
		}
		if _, exists := output[fields[0]]; exists {
			return nil, fmt.Errorf("duplicate cgroup counter: %q", fields[0])
		}
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return nil, err
		}
		output[fields[0]] = value
	}
	return output, nil
}

func captureReadFile(directory *os.File, name string, maximum int64) ([]byte, error) {
	fd, err := unix.Openat(int(directory.Fd()), name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), name)
	data, readErr := io.ReadAll(io.LimitReader(file, maximum+1))
	if err := errors.Join(readErr, file.Close()); err != nil {
		return nil, err
	}
	if int64(len(data)) > maximum {
		return nil, fmt.Errorf("cgroup file exceeds bound: %s", name)
	}
	return data, nil
}

// Traverse only the original cgroup mount, with bounded directory and thread
// counts. The reference's live-tracer rejection still checks every owned TID.
func captureGuardThreads(config captureGuardConfig, anchors captureGuardAnchors) (map[int]string, error) {
	threads := map[int]string{}
	directories := 0
	var visit func(*os.File, int) error
	visit = func(directory *os.File, depth int) error {
		directories++
		if depth > 32 || directories > 4096 {
			return fmt.Errorf("source cgroup tree exceeds capture bounds")
		}
		identity, err := cgroupFDIdentity(int(directory.Fd()))
		if err != nil || identity.MountID != config.Source.Cgroup.MountID {
			return fmt.Errorf("source cgroup tree crosses a foreign mount: %v", err)
		}
		data, err := captureReadFile(directory, "cgroup.threads", 256<<10)
		if err != nil {
			return err
		}
		for _, field := range strings.Fields(string(data)) {
			tid, err := strconv.Atoi(field)
			if err != nil || tid <= 0 || tid == os.Getpid() || len(threads) >= 32768 {
				return fmt.Errorf("invalid owned source thread or thread count")
			}
			start, err := processBirth(tid)
			if err != nil {
				return err
			}
			threads[tid] = start
		}
		fd, err := unix.Openat(int(directory.Fd()), ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			return err
		}
		list := os.NewFile(uintptr(fd), "source cgroup entries")
		entries, readErr := list.ReadDir(8193)
		closeErr := list.Close()
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return errors.Join(readErr, closeErr)
		}
		if closeErr != nil || len(entries) > 8192 {
			return errors.Join(closeErr, fmt.Errorf("source cgroup directory exceeds entry bound"))
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			fd, err := unix.Openat(int(directory.Fd()), entry.Name(),
				unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
			if err != nil {
				return err
			}
			child := os.NewFile(uintptr(fd), "source cgroup child")
			visitErr := visit(child, depth+1)
			if err := errors.Join(visitErr, child.Close()); err != nil {
				return err
			}
		}
		return nil
	}
	return threads, visit(anchors.group, 0)
}

func (guard *captureGuard) freeze(frozen bool) error {
	if err := guard.check(); err != nil {
		return err
	}
	if err := captureGuardValidate(guard.config, guard.anchor); err != nil {
		return err
	}
	value, want := "0", uint64(0)
	if frozen {
		value, want = "1", 1
	}
	fd, err := unix.Openat(int(guard.anchor.group.Fd()), "cgroup.freeze",
		unix.O_WRONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), "original source freezer")
	_, writeErr := file.WriteString(value)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return err
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if err := guard.check(); err != nil {
			return err
		}
		if err := captureGuardValidate(guard.config, guard.anchor); err != nil {
			return err
		}
		events, err := captureGuardEvents(guard.anchor.group)
		if err == nil && events["populated"] == 1 && events["frozen"] == want {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("source freeze state was not confirmed: %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
