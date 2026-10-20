//go:build linux

package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

type captureDescriptorBlocker struct {
	TID  int    `json:"tid"`
	FD   int    `json:"fd"`
	Kind string `json:"kind"`
}

type captureDescriptorAdmission struct {
	Version     int                        `json:"version"`
	Token       string                     `json:"token"`
	SourcePID   int                        `json:"source_pid"`
	SourceStart string                     `json:"source_start"`
	Attempts    int                        `json:"attempts"`
	Seconds     float64                    `json:"seconds"`
	First       []captureDescriptorBlocker `json:"first"`
	Last        []captureDescriptorBlocker `json:"last"`
	Passed      bool                       `json:"passed"`
}

func captureDescriptorBlockers(config captureGuardConfig, anchors captureGuardAnchors) ([]captureDescriptorBlocker, error) {
	threads, err := captureGuardThreads(config, anchors)
	if err != nil {
		return nil, err
	}
	tids := make([]int, 0, len(threads))
	for tid := range threads {
		tids = append(tids, tid)
	}
	slices.Sort(tids)
	representatives := map[int]int{}
	var blocked []captureDescriptorBlocker
	inspected := 0
	for _, tid := range tids {
		status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", tid))
		if err != nil {
			return nil, err
		}
		tgid := 0
		for _, line := range strings.Split(string(status), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[0] == "Tgid:" {
				tgid, err = strconv.Atoi(fields[1])
				if err != nil {
					return nil, err
				}
			}
		}
		if tgid <= 1 {
			return nil, fmt.Errorf("descriptor admission lost an original thread's process")
		}
		if other, found := representatives[tgid]; found {
			// Linux KCMP_FILES is 2. Only skip a file table after a real
			// kernel comparison; unshared thread tables must also be checked.
			same, _, errno := unix.Syscall6(unix.SYS_KCMP, uintptr(tid), uintptr(other), 2, 0, 0, 0)
			if errno == 0 && same == 0 {
				continue
			}
		} else {
			representatives[tgid] = tid
		}
		directory := fmt.Sprintf("/proc/%d/fd", tid)
		file, err := os.Open(directory)
		if err != nil {
			return nil, err
		}
		entries, readErr := file.ReadDir(8193)
		closeErr := file.Close()
		if (readErr != nil && !errors.Is(readErr, io.EOF)) || closeErr != nil || len(entries) > 8192 {
			return nil, fmt.Errorf("descriptor admission exceeds its directory bound: %v", errors.Join(readErr, closeErr))
		}
		for _, entry := range entries {
			inspected++
			if inspected > 65536 {
				return nil, fmt.Errorf("descriptor admission exceeds its total file-table bound")
			}
			fd, err := strconv.Atoi(entry.Name())
			if err != nil || fd < 0 {
				return nil, fmt.Errorf("descriptor admission encountered an invalid descriptor")
			}
			target, err := os.Readlink(directory + "/" + entry.Name())
			if err != nil {
				return nil, err
			}
			// Keep bounded diagnostic samples even when a workload holds
			// thousands of contexts. Any retained sample still blocks capture.
			if target == "anon_inode:[fscontext]" && len(blocked) < 16 {
				blocked = append(blocked, captureDescriptorBlocker{TID: tid, FD: fd, Kind: "fscontext"})
			}
		}
		birth, err := processBirth(tid)
		if err != nil || birth != threads[tid] {
			return nil, fmt.Errorf("descriptor admission replaced an original thread: %v", err)
		}
	}
	return blocked, nil
}

// A mount transaction is opaque host-kernel state that this CRIU cannot dump.
// Wait only before capture-enter, letting source code finish it between frozen
// inspections. Namespace/filesystem admission must run after this method.
func (guard *captureGuard) admitCheckpointDescriptors(ctx context.Context) (resultErr error) {
	started := time.Now()
	deadline := started.Add(2 * time.Second)
	observation := captureDescriptorAdmission{Version: 1, Token: guard.config.Token,
		SourcePID: guard.config.PID, SourceStart: guard.config.Start}
	defer func() {
		if len(observation.First) == 0 && resultErr == nil {
			return
		}
		observation.Seconds, observation.Passed = time.Since(started).Seconds(), resultErr == nil
		resultErr = errors.Join(resultErr,
			captureGuardWrite(guard.anchor.evidence, "descriptor-admission.json", observation))
	}()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := captureGuardCaptureReturned(guard.config, guard.anchor); err != nil {
			return err
		}
		blocked, err := captureDescriptorBlockers(guard.config, guard.anchor)
		observation.Attempts++
		observation.Last = blocked
		if err != nil {
			return err
		}
		if len(blocked) == 0 {
			return nil
		}
		if len(observation.First) == 0 {
			observation.First = append([]captureDescriptorBlocker(nil), blocked...)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("fscontext mount transaction did not settle within 2s: %v", blocked)
		}
		if err := guard.freeze(false); err != nil {
			return err
		}
		delay := 10 * time.Millisecond << min(observation.Attempts-1, 3)
		timer := time.NewTimer(min(delay, time.Until(deadline)))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		if err := guard.freeze(true); err != nil {
			return err
		}
	}
}
