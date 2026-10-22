//go:build linux

package lifecycle

import (
	"errors"
	"fmt"
	"math"

	"golang.org/x/sys/unix"
)

// StorageCapacity observes the filesystem containing this controller, not the
// size of its own files. AvailableBytes conservatively uses statfs Bavail. The
// admission floor neither reserves blocks nor limits concurrent guest writes.
type StorageCapacity struct {
	Version        int          `json:"version"`
	Root           string       `json:"root"`
	Directory      KernelObject `json:"directory"`
	ObservedAt     string       `json:"observed_at"`
	FilesystemType int64        `json:"filesystem_type"`
	TotalBytes     uint64       `json:"filesystem_total_bytes"`
	FreeBytes      uint64       `json:"filesystem_free_bytes"`
	AvailableBytes uint64       `json:"filesystem_available_bytes"`
	MinFreeBytes   uint64       `json:"min_free_bytes"`
	WriteReserve   uint64       `json:"write_reserve_bytes"`
	PolicyEnabled  bool         `json:"policy_enabled"`
	WriteAdmitted  bool         `json:"new_write_admitted"`
}

func validateStoragePolicy(config Config) error {
	if (config.MinFreeBytes == 0) != (config.WriteReserve == 0) || config.WriteReserve > math.MaxUint64-config.MinFreeBytes {
		return fmt.Errorf("storage admission requires a positive free-space floor and write reserve without overflow")
	}
	return nil
}

func (e *Engine) storageCapacity() (StorageCapacity, error) {
	report := StorageCapacity{Version: 1, Root: e.config.Root, ObservedAt: now(),
		MinFreeBytes: e.config.MinFreeBytes, WriteReserve: e.config.WriteReserve,
		PolicyEnabled: e.config.MinFreeBytes != 0}
	if err := validateStoragePolicy(e.config); err != nil {
		return report, err
	}
	root, err := e.root.Open(".")
	if err != nil {
		return report, err
	}
	defer root.Close()
	var filesystem unix.Statfs_t
	var stat unix.Stat_t
	var extended unix.Statx_t
	if err := errors.Join(unix.Fstatfs(int(root.Fd()), &filesystem), unix.Fstat(int(root.Fd()), &stat),
		unix.Statx(int(root.Fd()), "", unix.AT_EMPTY_PATH, unix.STATX_MNT_ID, &extended)); err != nil {
		return report, err
	}
	if extended.Mask&unix.STATX_MNT_ID == 0 || extended.Mnt_id == 0 {
		return report, fmt.Errorf("storage mount identity could not be verified")
	}
	report.Directory = KernelObject{Device: uint64(stat.Dev), Inode: stat.Ino, MountID: extended.Mnt_id}
	current, err := directoryIdentity(e.config.Root)
	if err != nil || current != report.Directory {
		return report, fmt.Errorf("storage directory identity changed: %v", err)
	}
	if filesystem.Bsize <= 0 || filesystem.Bavail > filesystem.Bfree || filesystem.Bfree > filesystem.Blocks ||
		filesystem.Blocks > math.MaxUint64/uint64(filesystem.Bsize) {
		return report, fmt.Errorf("storage capacity counters are invalid or overflow")
	}
	block := uint64(filesystem.Bsize)
	report.FilesystemType = int64(filesystem.Type)
	report.TotalBytes, report.FreeBytes, report.AvailableBytes = filesystem.Blocks*block, filesystem.Bfree*block, filesystem.Bavail*block
	report.WriteAdmitted = !report.PolicyEnabled || report.AvailableBytes >= report.MinFreeBytes+report.WriteReserve
	return report, nil
}

// Capacity remains available below the admission floor. It does not create a
// sandbox, capture or reservation and never infers store ownership from size.
func (e *Engine) Capacity() (StorageCapacity, error) {
	lock, err := e.lock()
	if err != nil {
		return StorageCapacity{}, err
	}
	defer lock.Close()
	return e.storageCapacity()
}

// Call under the existing controller operation lock, before new backing or a
// transition is published. Completed replays and deletion remain possible.
func (e *Engine) admitStorageWrite() error {
	if e.config.MinFreeBytes == 0 && e.config.WriteReserve == 0 {
		return nil // Legacy explicit-controller fixtures have no admission policy.
	}
	report, err := e.storageCapacity()
	if err != nil {
		return errors.Join(err, fmt.Errorf("new storage writes refused: capacity could not be verified"))
	}
	if !report.WriteAdmitted {
		return fmt.Errorf("new storage writes refused: filesystem available=%d bytes, required free floor=%d plus write reserve=%d",
			report.AvailableBytes, report.MinFreeBytes, report.WriteReserve)
	}
	return nil
}
