//go:build linux

package sysboxcapture

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

const (
	btrfsDestroyIOCTL = uintptr(0x5000943f) // SNAP_DESTROY_V2, 4096 bytes
	btrfsDeleteByID   = uint64(1 << 4)
	btrfsSearchIOCTL  = uintptr(0xd0009411) // TREE_SEARCH, 4096 bytes
)

func subvolumeRetirementParent(path string, expected subvolumeIdentity) (_ *os.File, resultErr error) {
	if !cleanAbsolute(path) || expected.Flags != btrfsReadonlyRoot ||
		expected.ID < 256 || expected.MountID == 0 || expected.FilesystemUUID == "" {
		return nil, fmt.Errorf("native retirement requires its sealed snapshot identity")
	}
	if err := immutableParents(path); err != nil {
		return nil, err
	}
	fd, err := unix.Openat2(unix.AT_FDCWD, filepath.Dir(path), &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOATIME,
		Resolve: unix.RESOLVE_NO_SYMLINKS,
	})
	if err != nil {
		return nil, err
	}
	parent := os.NewFile(uintptr(fd), "native snapshot retirement parent")
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, parent.Close())
		}
	}()
	filesystem, err := subvolumeFilesystem(fd)
	if err != nil || filesystem != expected.FilesystemUUID {
		return nil, errors.Join(err, fmt.Errorf("native retirement crosses its admitted filesystem"))
	}
	var mount unix.Statx_t
	if err := unix.Statx(fd, "", unix.AT_EMPTY_PATH, unix.STATX_MNT_ID, &mount); err != nil {
		return nil, err
	}
	if mount.Mask&unix.STATX_MNT_ID == 0 || mount.Mnt_id != expected.MountID {
		return nil, fmt.Errorf("native retirement crosses a foreign mount")
	}
	return parent, nil
}

// retireSubvolume withdraws one exact sealed snapshot without traversing or
// making its contents writable. The lifecycle caller must already have saved
// deletion intent and excluded live, selected and descendant dependencies.
// Success means durable namespace removal, not that space has been reclaimed.
// An absent path is not a successful retry without that caller's saved intent.
func retireSubvolume(ctx context.Context, path string, expected subvolumeIdentity) (resultErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	parent, err := subvolumeRetirementParent(path, expected)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, parent.Close()) }()
	if err := verifySubvolume(path, expected); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Target the verified kernel ID, not whatever might later occupy its name.
	// The filesystem identity was independently checked above. This never
	// falls back to recursive unlink or a path-only deletion.
	data := make([]byte, 4096)
	binary.LittleEndian.PutUint64(data[16:24], btrfsDeleteByID)
	binary.LittleEndian.PutUint64(data[56:64], expected.ID)
	if err := subvolumeIOCTL(int(parent.Fd()), btrfsDestroyIOCTL, data); err != nil {
		return err
	}
	if err := errors.Join(parent.Sync(), unix.Syncfs(int(parent.Fd()))); err != nil {
		return err
	}
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("native snapshot path is not absent after retirement")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// waitSubvolumeRetirement observes cleaning of this one already withdrawn ID.
// It neither waits for unrelated deletions nor infers free bytes from unlink.
// The root item remains in the root tree while the cleaner drops its extents.
// SUBVOL_SYNC_WAIT can block inside the kernel indefinitely, so it cannot honor
// a Go context. Bounded root-item queries let cancellation stop observation
// without a leaked waiter or a false receipt that all bytes are already free.
func waitSubvolumeRetirement(ctx context.Context, path string, expected subvolumeIdentity) (resultErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	parent, err := subvolumeRetirementParent(path, expected)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, parent.Close()) }()
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("native cleaning requires its withdrawn snapshot path")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		pending, err := subvolumeRetirementPending(parent, expected.ID)
		if err != nil || !pending {
			return err
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func subvolumeRetirementPending(parent *os.File, id uint64) (bool, error) {
	// TREE_SEARCH reads the committed root. Without committing the cleaner's
	// transaction, an already cleaned root can remain visible until the next
	// periodic commit. Do not turn that stale observation into a long wait.
	if err := unix.Syncfs(int(parent.Fd())); err != nil {
		return false, err
	}
	// UAPI btrfs_ioctl_search_args is a 104-byte search key and a bounded
	// result buffer. ROOT_TREE_OBJECTID=1; ROOT_ITEM_KEY=132. Search one exact
	// subvolume ID, not the queue of unrelated deleted roots.
	data := make([]byte, 4096)
	binary.LittleEndian.PutUint64(data[:8], 1)
	binary.LittleEndian.PutUint64(data[8:16], id)
	binary.LittleEndian.PutUint64(data[16:24], id)
	binary.LittleEndian.PutUint64(data[32:40], ^uint64(0))
	binary.LittleEndian.PutUint64(data[48:56], ^uint64(0))
	binary.LittleEndian.PutUint32(data[56:60], 132)
	binary.LittleEndian.PutUint32(data[60:64], 132)
	binary.LittleEndian.PutUint32(data[64:68], 1)
	if err := subvolumeIOCTL(int(parent.Fd()), btrfsSearchIOCTL, data); err != nil {
		return false, err
	}
	switch binary.LittleEndian.Uint32(data[64:68]) {
	case 0:
		return false, nil
	case 1:
		// btrfs_ioctl_search_header precedes the root item payload.
		if binary.LittleEndian.Uint64(data[112:120]) != id ||
			binary.LittleEndian.Uint32(data[128:132]) != 132 ||
			binary.LittleEndian.Uint32(data[132:136]) > uint32(len(data)-136) {
			return false, fmt.Errorf("native cleaning returned another root item")
		}
		return true, nil
	default:
		return false, fmt.Errorf("native cleaning returned an unbounded root set")
	}
}
