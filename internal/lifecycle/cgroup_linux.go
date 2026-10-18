//go:build linux

package lifecycle

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

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
	path := cgroupRoot + "/" + record.RuntimeID
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if err := e.validateCgroup(record); err != nil {
		return err
	}
	events, err := os.ReadFile(path + "/cgroup.events")
	if err != nil || !strings.Contains("\n"+string(events), "\npopulated 0\n") {
		return fmt.Errorf("cannot retire a populated or unproved cgroup: %v", err)
	}
	// Rmdir refuses non-empty trees. Do not recursively remove an unresolved
	// runtime's cgroups or signal its processes through a guessed pathname.
	return os.Remove(path)
}
