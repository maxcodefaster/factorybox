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

func captureMountIDs(threads map[int]string) (map[uint64]bool, error) {
	mounts := map[uint64]bool{}
	seen := map[[2]uint64]bool{}
	rows := 0
	for tid := range threads {
		var namespace unix.Stat_t
		if err := unix.Stat(fmt.Sprintf("/proc/%d/ns/mnt", tid), &namespace); err != nil {
			return nil, err
		}
		key := [2]uint64{uint64(namespace.Dev), namespace.Ino}
		if seen[key] {
			continue
		}
		seen[key] = true
		file, err := os.Open(fmt.Sprintf("/proc/%d/mountinfo", tid))
		if err != nil {
			return nil, err
		}
		data, readErr := io.ReadAll(io.LimitReader(file, (8<<20)+1))
		closeErr := file.Close()
		if readErr != nil || closeErr != nil || len(data) > 8<<20 {
			return nil, fmt.Errorf("source mount inventory exceeds its byte bound: %v", errors.Join(readErr, closeErr))
		}
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			fields := strings.Fields(line)
			rows++
			if len(fields) < 10 || rows > 65536 {
				return nil, fmt.Errorf("source mount inventory exceeds its row bound")
			}
			id, err := strconv.ParseUint(fields[0], 10, 64)
			if err != nil || id == 0 {
				return nil, fmt.Errorf("source mount inventory has an invalid identity")
			}
			mounts[id] = true
		}
	}
	return mounts, nil
}

func captureForeignProcDirectory(path, target string, owner int, threads map[int]string,
	mounts *map[uint64]bool) (bool, error) {
	if !strings.HasSuffix(target, "/fd") {
		return false, nil
	}
	// Opening the original proc magic link with O_PATH pins its existing
	// inode/mount without reading data or changing the source file offset.
	fd, err := unix.Open(path, unix.O_PATH|unix.O_CLOEXEC, 0)
	if err != nil {
		return false, err
	}
	defer unix.Close(fd)
	var stat unix.Stat_t
	var filesystem unix.Statfs_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return false, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		return false, nil
	}
	if err := unix.Fstatfs(fd, &filesystem); err != nil {
		return false, err
	}
	if filesystem.Type != unix.PROC_SUPER_MAGIC {
		return false, nil
	}
	var extended unix.Statx_t
	if err := unix.Statx(fd, "", unix.AT_EMPTY_PATH, unix.STATX_MNT_ID, &extended); err != nil {
		return false, err
	}
	if extended.Mask&unix.STATX_MNT_ID == 0 || extended.Mnt_id == 0 {
		return false, fmt.Errorf("proc directory lacks an original mount identity")
	}
	if *mounts == nil {
		*mounts, err = captureMountIDs(threads)
		if err != nil {
			return false, err
		}
	}
	if (*mounts)[extended.Mnt_id] {
		return false, nil
	}
	// The pinned CRIU supports its own canonical readable fd directory in
	// a detached proc mount. Foreign process/thread directories are not
	// reconstructable through that path and must reject before infection.
	return target != fmt.Sprintf("/%d/task/%d/fd", owner, owner), nil
}

func captureDetachedFile(tid, descriptor int, target string, threads map[int]string,
	mounts *map[uint64]bool) (bool, error) {
	if !strings.HasPrefix(target, "/") {
		return false, nil
	}
	info, err := os.Open(fmt.Sprintf("/proc/%d/fdinfo/%d", tid, descriptor))
	if err != nil {
		return false, err
	}
	data, readErr := io.ReadAll(io.LimitReader(info, 4097))
	closeErr := info.Close()
	if readErr != nil || closeErr != nil || len(data) > 4096 {
		return false, fmt.Errorf("file descriptor flags exceed admission bounds: %v", errors.Join(readErr, closeErr))
	}
	var flags uint64
	found := false
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "flags:") {
			flags, err = strconv.ParseUint(strings.TrimSpace(strings.TrimPrefix(line, "flags:")), 8, 64)
			if err != nil {
				return false, err
			}
			found = true
		}
	}
	if !found {
		return false, fmt.Errorf("file descriptor flags are absent")
	}
	if flags&unix.O_PATH == 0 {
		return false, nil
	}
	fd, err := unix.Open(fmt.Sprintf("/proc/%d/fd/%d", tid, descriptor), unix.O_PATH|unix.O_CLOEXEC, 0)
	if err != nil {
		return false, err
	}
	defer unix.Close(fd)
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return false, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return false, nil
	}
	var extended unix.Statx_t
	if err := unix.Statx(fd, "", unix.AT_EMPTY_PATH, unix.STATX_MNT_ID, &extended); err != nil {
		return false, err
	}
	if *mounts == nil {
		*mounts, err = captureMountIDs(threads)
		if err != nil {
			return false, err
		}
	}
	if (*mounts)[extended.Mnt_id] {
		return false, nil
	}
	// CRIU routes anonymous shmem/memfd objects through their own serializer.
	// Compare the actual kernel device, as CRIU does, rather than trusting a
	// workload-controlled filename which happens to start with "memfd".
	sample, err := unix.MemfdCreate("factorybox-admission-device", unix.MFD_CLOEXEC)
	if err != nil {
		return false, err
	}
	var shmem unix.Stat_t
	statErr := unix.Fstat(sample, &shmem)
	closeErr = unix.Close(sample)
	if statErr != nil || closeErr != nil {
		return false, errors.Join(statErr, closeErr)
	}
	if stat.Dev == shmem.Dev {
		return false, nil
	}
	var filesystem unix.Statfs_t
	if err := unix.Fstatfs(fd, &filesystem); err != nil {
		return false, err
	}
	if filesystem.Type == unix.HUGETLBFS_MAGIC {
		return false, nil
	}
	if extended.Mask&unix.STATX_MNT_ID == 0 || extended.Mnt_id == 0 {
		return false, fmt.Errorf("regular file lacks an original mount identity")
	}
	return true, nil
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
	var mounts map[uint64]bool
	inspected := 0
	for _, tid := range tids {
		status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", tid))
		if err != nil {
			return nil, err
		}
		tgid := 0
		ownPID := 0
		for _, line := range strings.Split(string(status), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[0] == "Tgid:" {
				tgid, err = strconv.Atoi(fields[1])
				if err != nil {
					return nil, err
				}
			}
			if len(fields) >= 2 && fields[0] == "NStgid:" {
				ownPID, err = strconv.Atoi(fields[len(fields)-1])
				if err != nil {
					return nil, err
				}
			}
		}
		if tgid <= 1 || ownPID <= 0 {
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
			foreign, err := captureForeignProcDirectory(directory+"/"+entry.Name(), target, ownPID, threads, &mounts)
			if err != nil {
				return nil, err
			}
			if foreign && len(blocked) < 16 {
				blocked = append(blocked, captureDescriptorBlocker{TID: tid, FD: fd, Kind: "detached-proc"})
			}
			detached, err := captureDetachedFile(tid, fd, target, threads, &mounts)
			if err != nil {
				return nil, err
			}
			if detached && len(blocked) < 16 {
				blocked = append(blocked, captureDescriptorBlocker{TID: tid, FD: fd, Kind: "detached-file"})
			}
		}
		birth, err := processBirth(tid)
		if err != nil || birth != threads[tid] {
			return nil, fmt.Errorf("descriptor admission replaced an original thread: %v", err)
		}
	}
	return blocked, nil
}

// Opaque mount transactions and unsupported detached proc/file references cannot
// enter CRIU. Source code may finish them between bounded frozen inspections.
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
			return fmt.Errorf("capture descriptor state did not settle within 2s: %v", blocked)
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
