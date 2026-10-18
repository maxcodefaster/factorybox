package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const epochReplayCgroup = "/sys/fs/cgroup/criu-probe-sysbox-copy"

type epochReplayCgroupAnchor struct {
	directory *os.File
	identity  os.FileInfo
	mountID   uint64
	removed   int
}

func (anchor *epochReplayCgroupAnchor) evidence() map[string]any {
	stat := anchor.identity.Sys().(*syscall.Stat_t)
	return map[string]any{"path": anchor.directory.Name(), "device": uint64(stat.Dev),
		"inode": stat.Ino, "mount_id": anchor.mountID}
}

func epochReplayOpenCgroup() (*epochReplayCgroupAnchor, error) {
	fd, err := unix.Open(epochReplayCgroup, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return epochReplayAnchorDirectory(os.NewFile(uintptr(fd), epochReplayCgroup))
}

func epochReplayMountID(fd int, path string, flags int) (uint64, error) {
	var stat unix.Statx_t
	if err := unix.Statx(fd, path, flags, unix.STATX_MNT_ID, &stat); err != nil {
		return 0, err
	}
	if stat.Mask&unix.STATX_MNT_ID == 0 || stat.Mnt_id == 0 {
		return 0, fmt.Errorf("replay cleanup requires a known mount identity")
	}
	return stat.Mnt_id, nil
}

func epochReplayAnchorDirectory(directory *os.File) (*epochReplayCgroupAnchor, error) {
	fd := int(directory.Fd())
	info, statErr := directory.Stat()
	var filesystem unix.Statfs_t
	fsErr := unix.Fstatfs(fd, &filesystem)
	mountID, mountErr := epochReplayMountID(fd, "", unix.AT_EMPTY_PATH)
	if statErr != nil || fsErr != nil || mountErr != nil || !info.IsDir() || filesystem.Type != unix.CGROUP2_SUPER_MAGIC {
		return nil, errors.Join(fmt.Errorf("replay cleanup requires its original cgroup2 directory"),
			statErr, fsErr, mountErr, directory.Close())
	}
	return &epochReplayCgroupAnchor{directory: directory, identity: info, mountID: mountID}, nil
}

func (anchor *epochReplayCgroupAnchor) verifyFD() error {
	current, err := anchor.directory.Stat()
	if err != nil || !os.SameFile(anchor.identity, current) {
		return fmt.Errorf("original replay cgroup descriptor changed: %v", err)
	}
	return nil
}

func (anchor *epochReplayCgroupAnchor) close() error {
	if err := anchor.verifyFD(); err != nil {
		return err
	}
	return anchor.directory.Close()
}

func (anchor *epochReplayCgroupAnchor) current() (bool, error) {
	if err := anchor.verifyFD(); err != nil {
		return false, err
	}
	current, err := os.Lstat(anchor.directory.Name())
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil || !current.IsDir() || !os.SameFile(anchor.identity, current) {
		return false, fmt.Errorf("replay cgroup path belongs to another kernel object: %v", err)
	}
	mountID, err := epochReplayMountID(unix.AT_FDCWD, anchor.directory.Name(), unix.AT_SYMLINK_NOFOLLOW)
	if err != nil || mountID != anchor.mountID {
		return false, fmt.Errorf("replay cgroup path changed its mount identity: %v", err)
	}
	return true, nil
}

func (anchor *epochReplayCgroupAnchor) empty() error {
	fd, err := unix.Openat(int(anchor.directory.Fd()), "cgroup.events", unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), "original replay cgroup events")
	data, readErr := io.ReadAll(io.LimitReader(file, 1025))
	if err := errors.Join(readErr, file.Close()); err != nil {
		return err
	}
	events, err := criuRAMCounters(string(data))
	populated, known := events["populated"]
	if err != nil || len(data) > 1024 || !known || populated != 0 {
		return fmt.Errorf("replay cgroup is populated or its state is unproved: %v", err)
	}
	return nil
}

func (anchor *epochReplayCgroupAnchor) removeEmpty() (resultErr error) {
	if anchor.directory.Name() != epochReplayCgroup {
		return fmt.Errorf("replay retirement is outside the exact replay cgroup")
	}
	present, err := anchor.current()
	if err != nil || !present {
		return err
	}
	var planned, opened []*epochReplayCgroupAnchor
	defer func() {
		for _, child := range opened {
			resultErr = errors.Join(resultErr, child.close())
		}
	}()
	var collect func(*epochReplayCgroupAnchor, int) error
	collect = func(node *epochReplayCgroupAnchor, depth int) error {
		if depth > 32 || len(opened) >= 4096 {
			return fmt.Errorf("replay cgroup tree exceeds bounded retirement scope")
		}
		if present, err := node.current(); err != nil || !present {
			return err
		}
		if err := node.empty(); err != nil {
			return err
		}
		// A separate open description gives each observation a fresh
		// readdir offset without moving the retained ownership anchor.
		fd, err := unix.Openat(int(node.directory.Fd()), ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			return err
		}
		directory := os.NewFile(uintptr(fd), node.directory.Name())
		entries, readErr := directory.ReadDir(8193)
		closeErr := directory.Close()
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return errors.Join(readErr, closeErr)
		}
		if closeErr != nil {
			return closeErr
		}
		if len(entries) > 8192 {
			return fmt.Errorf("replay cgroup directory exceeds bounded entry count")
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			name := entry.Name()
			if name == "." || name == ".." || strings.Contains(name, "/") {
				return fmt.Errorf("invalid replay cgroup child name")
			}
			fd, err := unix.Openat(int(node.directory.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return err
			}
			child, err := epochReplayAnchorDirectory(os.NewFile(uintptr(fd), filepath.Join(node.directory.Name(), name)))
			if err != nil {
				return err
			}
			opened = append(opened, child)
			if child.mountID != anchor.mountID {
				return fmt.Errorf("replay cgroup subtree crosses a foreign mount")
			}
			if err := collect(child, depth+1); err != nil {
				return err
			}
		}
		planned = append(planned, node)
		return nil
	}
	// Validate the complete empty tree before removing anything.
	if err := collect(anchor, 0); err != nil {
		return err
	}
	deadline := time.Now().Add(2 * time.Second)
	for _, node := range planned {
		for {
			if present, err := anchor.current(); err != nil || !present {
				return err
			}
			if present, err := node.current(); err != nil {
				return err
			} else if !present {
				break
			}
			if err := node.empty(); err != nil {
				return err
			}
			err := os.Remove(node.directory.Name())
			if err == nil {
				anchor.removed++
				break
			}
			if os.IsNotExist(err) {
				break
			}
			if !errors.Is(err, unix.EBUSY) || time.Now().After(deadline) {
				return fmt.Errorf("remove exact empty replay cgroup %s: %w", node.directory.Name(), err)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	return nil
}
