//go:build linux

package lifecycle

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func captureStatusParent(status []byte) (int, error) {
	if len(status) > 16<<10 {
		return 0, fmt.Errorf("source process status exceeds session admission bound")
	}
	parent := 0
	for _, line := range strings.Split(string(status), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "PPid:" {
			continue
		}
		if len(fields) != 2 || parent != 0 {
			return 0, fmt.Errorf("source process has an ambiguous parent identity")
		}
		var err error
		parent, err = strconv.Atoi(fields[1])
		if err != nil || parent <= 0 {
			return 0, fmt.Errorf("source process lacks a visible parent identity")
		}
	}
	if parent == 0 {
		return 0, fmt.Errorf("source process parent identity is absent")
	}
	return parent, nil
}

// The pinned nested CRIU reparents setns/exec tasks under their namespace's
// init. Identify these roots without depending on the consumer's executable.
// A single such root is part of the evidenced Factory profile. Multiple roots
// in one PID namespace have an unqualified session-helper topology and are
// rejected separately below, after the normal setup-settling window.
func captureExternalSessionRoot(process, ownPID int, status []byte) (bool, error) {
	if ownPID == 1 {
		return false, nil
	}
	parent, err := captureStatusParent(status)
	if err != nil {
		return false, err
	}
	var ownNamespace, parentNamespace unix.Stat_t
	if err := unix.Stat(fmt.Sprintf("/proc/%d/ns/pid", process), &ownNamespace); err != nil {
		return false, err
	}
	if err := unix.Stat(fmt.Sprintf("/proc/%d/ns/pid", parent), &parentNamespace); err != nil {
		return false, fmt.Errorf("source session parent changed during admission: %w", err)
	}
	after, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", process))
	if err != nil {
		return false, err
	}
	currentParent, err := captureStatusParent(after)
	if err != nil || currentParent != parent {
		return false, fmt.Errorf("source session parent changed during admission: %v", err)
	}
	return ownNamespace.Dev != parentNamespace.Dev || ownNamespace.Ino != parentNamespace.Ino, nil
}

type captureSessionRoot struct {
	TID       int
	Namespace [2]uint64
}

func captureMultipleSessionRoots(roots []captureSessionRoot) []captureDescriptorBlocker {
	counts := map[[2]uint64]int{}
	for _, root := range roots {
		counts[root.Namespace]++
	}
	var blocked []captureDescriptorBlocker
	for _, root := range roots {
		if counts[root.Namespace] > 1 && len(blocked) < 16 {
			blocked = append(blocked, captureDescriptorBlocker{TID: root.TID, FD: -1, Kind: "nested-exec-session"})
		}
	}
	return blocked
}
