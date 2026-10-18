//go:build linux

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

type committedFileIdentity struct {
	Device, Inode  uint64
	Size           int64
	Mode, UID, GID uint32
	Mtime, Ctime   syscall.Timespec
}

func committedIdentity(info os.FileInfo) committedFileIdentity {
	stat := info.Sys().(*syscall.Stat_t)
	return committedFileIdentity{uint64(stat.Dev), stat.Ino, stat.Size, stat.Mode, stat.Uid,
		stat.Gid, stat.Mtim, stat.Ctim}
}

type committedPage struct {
	File     *os.File
	Identity committedFileIdentity
	Digest   string
}

type committedSnapshot struct {
	Directory *os.File
	Identity  committedFileIdentity
	MountID   uint64
	Pages     map[string]committedPage
}

var committedSnapshots = map[string]*committedSnapshot{}

func committedSnapshotMode() (bool, error) {
	switch os.Getenv("RUNTIME_FORK_COMMITTED_SNAPSHOT") {
	case "", "0":
		return false, nil
	case "1":
		if os.Getenv("RUNTIME_FORK_RICH_EPOCH_CHAIN") != "1" ||
			(os.Getenv("RUNTIME_FORK_EPOCH_REBASE") != "0" && os.Getenv("RUNTIME_FORK_EPOCH_REBASE") != "1") {
			return false, fmt.Errorf("filesystem commit requires the exact source-epoch experiment")
		}
		return true, nil
	default:
		return false, fmt.Errorf("invalid committed-snapshot mode")
	}
}

// The superblock options follow the "-" separator. A local bind's "ro"
// option before that separator alone is insufficient.
func committedParseMountID(input string) (uint64, error) {
	id, err := strconv.ParseUint(input, 10, 64)
	if err != nil || id == 0 {
		return 0, fmt.Errorf("invalid committed mount identity")
	}
	return id, nil
}
func committedMountID(input, path string) (uint64, error) {
	return committedMountWithAnchor(input, path, 0)
}

func committedCurrentMount(path string, directory *os.File, expected uint64) (uint64, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return 0, fmt.Errorf("committed directory path changed: %v", err)
	}
	anchor, err := directory.Stat()
	if err != nil || !os.SameFile(info, anchor) {
		return 0, fmt.Errorf("committed directory inode changed: %v", err)
	}
	var fs unix.Statfs_t
	if err := unix.Fstatfs(int(directory.Fd()), &fs); err != nil ||
		fs.Type != unix.TMPFS_MAGIC || fs.Flags&unix.ST_RDONLY == 0 {
		return 0, fmt.Errorf("committed directory is not read-only tmpfs: %v", err)
	}
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return 0, err
	}
	return committedMountWithAnchor(string(data), path, expected)
}

func criuCommitSnapshot(images string) (resultErr error) {
	if images != "/experiment/sysbox/images-again" && images != "/experiment/sysbox/images-repeat" {
		return nil
	}
	enabled, err := committedSnapshotMode()
	if err != nil {
		return err
	}
	start := time.Now()
	var mountID uint64
	defer func() {
		record("sysbox-snapshot-filesystem-commit", start, asJSON(map[string]any{
			"images": images, "enabled": enabled, "mount_id": mountID,
			"scope": "filesystem-wide read-only publication after capture writer exit; existing tmpfs size/accounting retained; owning controller remains trusted; publication copies no page data; verification work reported separately",
		}), resultErr)
		if images == "/experiment/sysbox/images-again" {
			criuCommittedPreparation += time.Since(start)
		}
	}()
	if !enabled {
		return nil
	}
	snapshot := criuRAMSnapshotFind(images)
	if snapshot == nil || !snapshot.Mounted || snapshot.CopyImages != "" ||
		len(snapshot.Bindings) != 0 || committedSnapshots[images] != nil {
		return fmt.Errorf("commit is outside one owned completed capture")
	}
	fd, err := unix.Open(images, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	directory := os.NewFile(uintptr(fd), images)
	defer func() {
		if resultErr != nil {
			directory.Close()
		}
	}()
	var fs unix.Statfs_t
	if err := unix.Fstatfs(fd, &fs); err != nil || fs.Type != unix.TMPFS_MAGIC ||
		uint64(fs.Blocks)*uint64(fs.Bsize) > snapshot.Limit {
		return fmt.Errorf("commit requires the exact bounded owned tmpfs: %v", err)
	}
	// No MS_BIND: this sets the superblock read-only, including old writable
	// aliases. Live writable descriptors must make publication fail.
	if err := unix.Mount("", images, "", unix.MS_REMOUNT|unix.MS_RDONLY|
		unix.MS_NOSUID|unix.MS_NODEV|unix.MS_NOEXEC, ""); err != nil {
		return fmt.Errorf("publishing owned snapshot filesystem: %w", err)
	}
	mountID, err = committedCurrentMount(images, directory, 0)
	if err != nil {
		return err
	}
	info, err := directory.Stat()
	if err != nil {
		return err
	}
	committedSnapshots[images] = &committedSnapshot{Directory: directory,
		Identity: committedIdentity(info), MountID: mountID, Pages: map[string]committedPage{}}
	return nil
}

func criuRegisterCommittedParent(images string, expected map[string]string) (resultErr error) {
	enabled, err := committedSnapshotMode()
	if err != nil {
		return err
	}
	start := time.Now()
	var pages int
	var total int64
	defer func() {
		record("sysbox-committed-parent-registered", start, asJSON(map[string]any{
			"images": images, "enabled": enabled, "page_files": pages, "logical_page_bytes": total,
			"scope": "held read-only inode anchors registered after the existing complete reference hash; registration and prior P1 commit charged to second fork; metadata/final complete byte hashes retained",
		}), resultErr)
		criuCommittedPreparation += time.Since(start)
	}()
	if !enabled {
		return nil
	}
	owner := committedSnapshots[images]
	if images != "/experiment/sysbox/images-again" || owner == nil || len(owner.Pages) != 0 ||
		len(expected) == 0 || len(expected) > 8192 {
		return fmt.Errorf("committed parent registration is outside its exact snapshot")
	}
	id, err := committedCurrentMount(images, owner.Directory, owner.MountID)
	if err != nil || id != owner.MountID {
		return fmt.Errorf("committed mount identity differs: %v", err)
	}
	for name, digest := range expected {
		if !criuRAMPageName.MatchString(name) {
			continue
		}
		if filepath.Base(name) != name || len(digest) != 64 {
			return fmt.Errorf("invalid committed page expectation")
		}
		// Open from the original directory mount. Epoch rebasing can add
		// a self bind before registration; path-based opens pin that later
		// alias and prevent its normal unmount during verified retirement.
		fd, err := unix.Openat(int(owner.Directory.Fd()), name,
			unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
		if err != nil {
			return err
		}
		file := os.NewFile(uintptr(fd), filepath.Join(images, name))
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() < 0 ||
			info.Size() > int64(criuRAMSnapshotLimit)-total {
			file.Close()
			return fmt.Errorf("invalid committed page inode/bound: %v", err)
		}
		owner.Pages[name] = committedPage{file, committedIdentity(info), digest}
		total += info.Size()
		pages++
	}
	if pages == 0 {
		return fmt.Errorf("committed parent has no pages")
	}
	return nil
}

func criuCloseCommittedSnapshot(images string) (resultErr error) {
	owner := committedSnapshots[images]
	if owner == nil {
		return nil
	}
	start := time.Now()
	defer func() {
		record("sysbox-committed-snapshot-anchors-closed", start, asJSON(map[string]any{
			"images": images, "page_files": len(owner.Pages),
			"scope": "actual page/directory anchors closed only after full durable export/read-back and before original RAM unmount; child bindings keep their original separate lifetime",
		}), resultErr)
	}()
	for _, page := range owner.Pages {
		resultErr = errors.Join(resultErr, page.File.Close())
	}
	resultErr = errors.Join(resultErr, owner.Directory.Close())
	delete(committedSnapshots, images)
	return resultErr
}
