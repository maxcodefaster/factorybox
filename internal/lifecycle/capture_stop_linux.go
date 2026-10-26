//go:build linux

package lifecycle

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func captureStopThreadGroup(status string) (int, error) {
	group := 0
	for _, line := range strings.Split(status, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "Tgid:" {
			continue
		}
		if len(fields) != 2 || group != 0 {
			return 0, fmt.Errorf("source thread has an invalid thread-group record")
		}
		value, err := strconv.Atoi(fields[1])
		if err != nil || value <= 1 {
			return 0, fmt.Errorf("source thread has an invalid thread-group identity")
		}
		group = value
	}
	if group == 0 {
		return 0, fmt.Errorf("source thread has no thread-group identity")
	}
	return group, nil
}

// The complete source is already frozen, untraced and captured. Pin every
// owned thread group before sending any signal, then kill those exact process
// identities. Do not use cgroup.kill: affected kernels compare its persistent
// kill counter against the wrong cgroup for later CLONE_INTO_CGROUP children.
func captureKillFrozenSource(config captureGuardConfig, anchors captureGuardAnchors) error {
	if !config.StopSource {
		return fmt.Errorf("source stop has no persisted stop intent")
	}
	if err := captureGuardCaptureReturned(config, anchors); err != nil {
		return err
	}
	threads, err := captureGuardThreads(config, anchors)
	if err != nil {
		return err
	}
	groups := map[int]bool{}
	for tid := range threads {
		status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", tid))
		if err != nil {
			return err
		}
		group, err := captureStopThreadGroup(string(status))
		if err != nil {
			return err
		}
		if _, owned := threads[group]; !owned {
			return fmt.Errorf("source thread group is outside the frozen owned tree")
		}
		groups[group] = true
	}
	if !groups[config.PID] || threads[config.PID] != config.Start {
		return fmt.Errorf("source stop lost its original process identity")
	}
	var pids []int
	for pid := range groups {
		if pid != config.PID {
			pids = append(pids, pid)
		}
	}
	slices.Sort(pids)
	pids = append(pids, config.PID)
	descriptors := make([]int, 0, len(pids))
	defer func() {
		for _, fd := range descriptors {
			unix.Close(fd)
		}
	}()
	for _, pid := range pids {
		var fd int
		if pid == config.PID {
			fd, err = unix.FcntlInt(anchors.pid.Fd(), unix.F_DUPFD_CLOEXEC, 0)
		} else {
			fd, err = unix.PidfdOpen(pid, 0)
		}
		if err != nil {
			return err
		}
		descriptors = append(descriptors, fd)
		birth, err := processBirth(pid)
		if err != nil || birth != threads[pid] {
			return fmt.Errorf("source process changed before its stop was pinned: %v", err)
		}
	}
	if err := captureGuardCaptureReturned(config, anchors); err != nil {
		return err
	}
	current, err := captureGuardThreads(config, anchors)
	if err != nil || len(current) != len(threads) {
		return fmt.Errorf("frozen source threads changed before stop: %v", err)
	}
	for tid, birth := range threads {
		if current[tid] != birth {
			return fmt.Errorf("frozen source thread identity changed before stop")
		}
	}
	for _, fd := range descriptors {
		if err := unix.PidfdSendSignal(fd, unix.SIGKILL, nil, 0); err != nil {
			// Killing a nested namespace's init may already have terminated
			// another pinned descendant. Only its own exited pidfd permits this.
			poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
			n, pollErr := unix.Poll(poll, 0)
			if !errors.Is(err, unix.ESRCH) || pollErr != nil || n != 1 ||
				poll[0].Revents&unix.POLLIN == 0 || poll[0].Revents&unix.POLLNVAL != 0 {
				return fmt.Errorf("stop pinned source process: %w", err)
			}
		}
	}
	return nil
}
