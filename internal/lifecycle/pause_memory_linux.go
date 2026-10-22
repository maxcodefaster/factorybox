//go:build linux

package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

type SandboxMemory struct {
	Current uint64 `json:"current"`
	Anon    uint64 `json:"anon"`
	File    uint64 `json:"file"`
	Shmem   uint64 `json:"shmem"`
}

type SnapshotMemory struct {
	Files               int    `json:"files"`
	Bytes               uint64 `json:"bytes"`
	ResidentBytesBefore uint64 `json:"resident_bytes_before"`
	ResidentBytesAfter  uint64 `json:"resident_bytes_after"`
	FileBacked          bool   `json:"file_backed"`
}

func (e *Engine) sandboxMemory(record Record) (SandboxMemory, error) {
	var result SandboxMemory
	if err := e.validateCgroup(record); err != nil {
		return result, err
	}
	group, err := captureGuardOpen(cgroupRoot + "/" + record.RuntimeID)
	if err != nil {
		return result, err
	}
	defer group.Close()
	current, err := captureReadFile(group, "memory.current", 4096)
	if err != nil {
		return result, err
	}
	result.Current, err = strconv.ParseUint(strings.TrimSpace(string(current)), 10, 64)
	if err != nil {
		return result, err
	}
	data, err := captureReadFile(group, "memory.stat", 256<<10)
	if err != nil {
		return result, err
	}
	values, err := parseCgroupCounters(string(data))
	if err != nil {
		return result, err
	}
	for _, name := range []string{"anon", "file", "shmem"} {
		if _, exists := values[name]; !exists {
			return result, fmt.Errorf("memory.stat lacks %s", name)
		}
	}
	result.Anon, result.File, result.Shmem = values["anon"], values["file"], values["shmem"]
	return result, nil
}

// cgroup v2 batches memory.stat updates. A stopped small process can leave an
// old anon count visible for more than a second even with populated=0. Commit
// a pause only after both the original cgroup remains empty and its own anon
// accounting confirms release. Never substitute a fabricated zero or force a
// global flush. A timeout leaves the stopped checkpoint available for retry.
func (e *Engine) stoppedSandboxMemory(ctx context.Context, record Record) (SandboxMemory, error) {
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	for {
		if err := e.requireEmptyCgroup(record); err != nil {
			return SandboxMemory{}, err
		}
		memory, err := e.sandboxMemory(record)
		if err != nil {
			return memory, err
		}
		if memory.Anon == 0 {
			return memory, e.requireEmptyCgroup(record)
		}
		interval := time.NewTimer(50 * time.Millisecond)
		select {
		case <-ctx.Done():
			interval.Stop()
			return memory, ctx.Err()
		case <-deadline.C:
			interval.Stop()
			return memory, fmt.Errorf("stopped sandbox anonymous memory release remains unconfirmed: %d bytes", memory.Anon)
		case <-interval.C:
		}
	}
}

func fileResidentBytes(file *os.File) (uint64, error) {
	info, err := file.Stat()
	if err != nil {
		return 0, err
	}
	if info.Size() == 0 {
		return 0, nil
	}
	if info.Size() < 0 || info.Size() > 16<<30 {
		return 0, fmt.Errorf("snapshot residency probe exceeds bounded file size")
	}
	mapping, err := unix.Mmap(int(file.Fd()), 0, int(info.Size()), unix.PROT_NONE, unix.MAP_SHARED)
	if err != nil {
		return 0, err
	}
	defer unix.Munmap(mapping)
	pageSize := os.Getpagesize()
	vector := make([]byte, (len(mapping)+pageSize-1)/pageSize)
	_, _, errno := unix.Syscall(unix.SYS_MINCORE, uintptr(unsafe.Pointer(&mapping[0])),
		uintptr(len(mapping)), uintptr(unsafe.Pointer(&vector[0])))
	if errno != 0 {
		return 0, errno
	}
	var resident uint64
	for _, value := range vector {
		if value&1 != 0 {
			resident += uint64(pageSize)
		}
	}
	return resident, nil
}

// Release only the generation's synced RAM payload cache. This never writes a
// global drop_caches setting and rejects RAM-backed storage.
func (e *Engine) releaseSnapshotMemory(ctx context.Context, relative string) (SnapshotMemory, error) {
	result := SnapshotMemory{FileBacked: true}
	images, err := e.root.Open(relative + "/images")
	if err != nil {
		return result, err
	}
	defer images.Close()
	entries, err := images.ReadDir(65537)
	if err != nil || len(entries) > 65536 {
		return result, fmt.Errorf("snapshot memory entries exceed bound: %v", err)
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "pages-") || !strings.HasSuffix(entry.Name(), ".img") {
			continue
		}
		fd, err := unix.Openat(int(images.Fd()), entry.Name(), unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
		if err != nil {
			return result, err
		}
		file := os.NewFile(uintptr(fd), entry.Name())
		var stat unix.Stat_t
		var backing unix.Statfs_t
		if err := errors.Join(unix.Fstat(fd, &stat), unix.Fstatfs(fd, &backing)); err != nil {
			file.Close()
			return result, err
		}
		if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Nlink != 1 ||
			backing.Type == unix.TMPFS_MAGIC || backing.Type == unix.RAMFS_MAGIC {
			file.Close()
			return result, fmt.Errorf("snapshot RAM payload is not independent file-backed storage")
		}
		result.Files++
		result.Bytes += uint64(stat.Size)
		before, err := fileResidentBytes(file)
		if err != nil {
			file.Close()
			return result, err
		}
		result.ResidentBytesBefore += before
		if err := file.Sync(); err != nil {
			file.Close()
			return result, err
		}
		deadline := time.Now().Add(2 * time.Second)
		for {
			if err := ctx.Err(); err != nil {
				file.Close()
				return result, err
			}
			if err := unix.Fadvise(fd, 0, 0, unix.FADV_DONTNEED); err != nil {
				file.Close()
				return result, err
			}
			after, err := fileResidentBytes(file)
			if err != nil {
				file.Close()
				return result, err
			}
			if after == 0 {
				break
			}
			if time.Now().After(deadline) {
				file.Close()
				result.ResidentBytesAfter += after
				return result, fmt.Errorf("snapshot payload still has %d resident bytes", after)
			}
			time.Sleep(10 * time.Millisecond)
		}
		if err := file.Close(); err != nil {
			return result, err
		}
	}
	if result.Files == 0 || result.Bytes == 0 {
		return result, fmt.Errorf("pause has no complete RAM payload backing")
	}
	return result, nil
}
