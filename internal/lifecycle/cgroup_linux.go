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

const cgroupRoot = "/sys/fs/cgroup"

type KernelObject struct {
	Device  uint64 `json:"device"`
	Inode   uint64 `json:"inode"`
	MountID uint64 `json:"mount_id"`
}

// Match both the original cgroup inode and its mount. This is the same kernel
// identity check as the retained epoch-replay cleanup, without a fixture path.
func cgroupIdentity(path string) (KernelObject, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return KernelObject{}, err
	}
	defer unix.Close(fd)
	return cgroupFDIdentity(fd)
}

func cgroupFDIdentity(fd int) (KernelObject, error) {
	var stat unix.Stat_t
	var filesystem unix.Statfs_t
	var extended unix.Statx_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return KernelObject{}, err
	}
	if err := unix.Fstatfs(fd, &filesystem); err != nil {
		return KernelObject{}, err
	}
	if err := unix.Statx(fd, "", unix.AT_EMPTY_PATH, unix.STATX_MNT_ID, &extended); err != nil {
		return KernelObject{}, err
	}
	if filesystem.Type != unix.CGROUP2_SUPER_MAGIC ||
		extended.Mask&unix.STATX_MNT_ID == 0 || extended.Mnt_id == 0 {
		return KernelObject{}, fmt.Errorf("requires a cgroup2 directory with a known mount identity")
	}
	return KernelObject{Device: uint64(stat.Dev), Inode: stat.Ino, MountID: extended.Mnt_id}, nil
}

func boundedCgroupRoot() (KernelObject, error) {
	identity, err := cgroupIdentity(cgroupRoot)
	if err != nil {
		return identity, err
	}
	data, err := os.ReadFile(cgroupRoot + "/memory.max")
	if err != nil {
		return identity, err
	}
	limit, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	if err != nil || limit == 0 {
		return identity, fmt.Errorf("lifecycle requires a private worker cgroup root with a finite memory limit")
	}
	return identity, nil
}

func (e *Engine) validateCgroup(record Record) error {
	parent, err := cgroupIdentity(cgroupRoot)
	if err != nil || parent != record.CgroupParent {
		return fmt.Errorf("worker cgroup root identity changed: %v", err)
	}
	if record.Cgroup == nil {
		return fmt.Errorf("sandbox cgroup creation has not been confirmed")
	}
	actual, err := cgroupIdentity(cgroupRoot + "/" + record.RuntimeID)
	if err != nil || actual != *record.Cgroup {
		return fmt.Errorf("sandbox cgroup identity changed: %v", err)
	}
	return nil
}

// A stopped init does not prove that its descendants are gone. Resume admits
// only the verified original cgroup tree with no remaining process and with its
// freezer cleared. Inspect cgroup.events through the identity-checked directory.
func (e *Engine) requireEmptyCgroup(record Record) error {
	if err := e.validateCgroup(record); err != nil {
		return err
	}
	group, err := captureGuardOpen(cgroupRoot + "/" + record.RuntimeID)
	if err != nil {
		return err
	}
	defer group.Close()
	identity, err := cgroupFDIdentity(int(group.Fd()))
	if err != nil || record.Cgroup == nil || identity != *record.Cgroup {
		return fmt.Errorf("resume original cgroup identity changed: %v", err)
	}
	events, err := captureGuardEvents(group)
	populated, hasPopulated := events["populated"]
	frozen, hasFrozen := events["frozen"]
	if err != nil || !hasPopulated || !hasFrozen || populated != 0 || frozen != 0 {
		return fmt.Errorf("resume requires its empty original cgroup with a cleared freezer: %v", err)
	}
	return e.validateCgroup(record)
}

func (e *Engine) prepareCgroup(record *Record) error {
	if record.Cgroup != nil {
		return e.validateCgroup(*record)
	}
	parent, err := cgroupIdentity(cgroupRoot)
	if err != nil || parent != record.CgroupParent {
		return fmt.Errorf("worker cgroup root identity changed: %v", err)
	}
	// The unique path and create intent are already durable. A pre-existing
	// directory without its saved kernel identity has an uncertain owner.
	path := cgroupRoot + "/" + record.RuntimeID
	if err := os.Mkdir(path, 0755); err != nil {
		return fmt.Errorf("refusing to adopt a pre-existing cgroup: %w", err)
	}
	identity, err := cgroupIdentity(path)
	if err != nil {
		return err
	}
	record.Cgroup = &identity
	return e.write(*record)
}

func (e *Engine) retireEmptyCgroup(record Record) error {
	return e.updateEmptyCgroupTree(record, true)
}

// CRIU must briefly enter the namespace root before restoring delegation.
// A resumed hierarchy still has its old controllers enabled. Clear only the
// original, wholly empty tree; CRIU restores its captured controllers and
// limits before restoring process descriptors. Keep every cgroup inode.
func (e *Engine) prepareEmptyResumeCgroup(record Record) error {
	if err := e.requireEmptyCgroup(record); err != nil {
		return err
	}
	if err := e.updateEmptyCgroupTree(record, false); err != nil {
		return err
	}
	return e.requireEmptyCgroup(record)
}

// Both operations validate the complete tree before changing any descendant.
// Retirement removes directories; resume preparation clears delegation.
func (e *Engine) updateEmptyCgroupTree(record Record, retire bool) (resultErr error) {
	path := cgroupRoot + "/" + record.RuntimeID
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if err := e.validateCgroup(record); err != nil {
		return err
	}
	parent, err := captureGuardOpen(cgroupRoot)
	if err != nil {
		return err
	}
	defer parent.Close()
	parentIdentity, err := cgroupFDIdentity(int(parent.Fd()))
	if err != nil || parentIdentity != record.CgroupParent {
		return fmt.Errorf("retirement cgroup parent identity changed: %v", err)
	}
	if record.Cgroup == nil {
		return fmt.Errorf("retirement lacks its original cgroup identity")
	}
	var opened []*os.File
	defer func() {
		for _, group := range opened {
			resultErr = errors.Join(resultErr, group.Close())
		}
	}()
	var planned []func() error
	populatedErr := errors.New("cannot retire a populated cgroup")
	deadline := time.Now().Add(2 * time.Second)
	visited := 0
	var collect func(*os.File, string, *KernelObject, int) error
	collect = func(parent *os.File, name string, expected *KernelObject, depth int) error {
		visited++
		if depth > 32 || visited > 4096 {
			return fmt.Errorf("retirement cgroup tree exceeds bounds")
		}
		if err := e.validateCgroup(record); err != nil {
			return err
		}
		fd, err := unix.Openat(int(parent.Fd()), name,
			unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			return err
		}
		group := os.NewFile(uintptr(fd), "original empty cgroup descendant")
		opened = append(opened, group)
		identity, err := cgroupFDIdentity(fd)
		if err != nil || identity.MountID != record.Cgroup.MountID ||
			(expected != nil && identity != *expected) {
			return fmt.Errorf("retirement crosses a changed or foreign cgroup mount: %v", err)
		}
		empty := func() error {
			events, err := captureGuardEvents(group)
			populated, present := events["populated"]
			if err != nil || !present || populated > 1 {
				return fmt.Errorf("cannot retire an unproved cgroup: %v", err)
			}
			if populated != 0 {
				return populatedErr
			}
			return nil
		}
		for {
			if err := empty(); err == nil {
				break
			} else if !errors.Is(err, populatedErr) || depth != 0 || record.Delete == nil || !record.Delete.Attempted ||
				record.RuntimeCreated == "" || time.Now().After(deadline) {
				return err
			}
			// Sysbox may return before the killed original tasks finish
			// leaving their cgroups. Wait only for this confirmed deletion,
			// without signaling any remaining or newly observed process.
			if err := e.validateCgroup(record); err != nil {
				return err
			}
			time.Sleep(5 * time.Millisecond)
		}
		entries, err := group.ReadDir(8193)
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		if len(entries) > 8192 {
			return fmt.Errorf("retirement cgroup directory exceeds bounds")
		}
		for _, entry := range entries {
			if entry.IsDir() {
				if err := collect(group, entry.Name(), nil, depth+1); err != nil {
					return err
				}
			}
		}
		var controls []string
		if !retire {
			data, err := captureReadFile(group, "cgroup.subtree_control", 4096)
			if err != nil {
				return err
			}
			controls = strings.Fields(string(data))
			for _, control := range controls {
				switch control {
				case "cpuset", "cpu", "io", "memory", "hugetlb", "pids", "rdma", "misc", "dmem":
				default:
					return fmt.Errorf("resume delegation contains an unsupported controller %q", control)
				}
			}
		}
		planned = append(planned, func() error {
			if err := empty(); err != nil {
				return err
			}
			if err := e.validateCgroup(record); err != nil {
				return err
			}
			// Check the named entry again before descriptor-relative rmdir.
			// Replaced descendants and foreign bind mounts are retained.
			currentFD, err := unix.Openat(int(parent.Fd()), name,
				unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
			if err != nil {
				return err
			}
			current, identityErr := cgroupFDIdentity(currentFD)
			closeErr := unix.Close(currentFD)
			if identityErr != nil || closeErr != nil || current != identity {
				return fmt.Errorf("retirement cgroup descendant identity changed: %v", errors.Join(identityErr, closeErr))
			}
			if retire {
				return unix.Unlinkat(int(parent.Fd()), name, unix.AT_REMOVEDIR)
			}
			currentControls, err := captureReadFile(group, "cgroup.subtree_control", 4096)
			if err != nil || strings.Join(strings.Fields(string(currentControls)), " ") != strings.Join(controls, " ") {
				return fmt.Errorf("resume delegation changed after admission: %v", err)
			}
			if len(controls) == 0 {
				return nil
			}
			disabled := make([]string, len(controls))
			for index, control := range controls {
				disabled[index] = "-" + control
			}
			controlFD, err := unix.Openat(int(group.Fd()), "cgroup.subtree_control",
				unix.O_WRONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
			if err != nil {
				return err
			}
			file := os.NewFile(uintptr(controlFD), "original empty cgroup delegation")
			_, writeErr := file.WriteString(strings.Join(disabled, " ") + "\n")
			return errors.Join(writeErr, file.Close())
		})
		return nil
	}
	// Validate the complete tree before removing an owned sibling of a
	// foreign mount or an unresolved populated descendant.
	if err := collect(parent, record.RuntimeID, record.Cgroup, 0); err != nil {
		return err
	}
	for _, remove := range planned {
		for {
			err := remove()
			if err == nil {
				break
			}
			// Kernel cgroup release may finish asynchronously. Every retry
			// rechecks the original root, pinned descendant and empty state.
			if !errors.Is(err, unix.EBUSY) || time.Now().After(deadline) {
				return err
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	return nil
}
