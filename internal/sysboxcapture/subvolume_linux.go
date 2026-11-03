//go:build linux

package sysboxcapture

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

// A subvolume identity is a kernel storage proof, not a content SHA256.
// The filesystem UUID and subvolume UUID survive a mount-namespace loss; device
// and mount identities are additional guards for an operation in this epoch.
type subvolumeIdentity struct {
	FilesystemUUID string `json:"filesystem_uuid"`
	ID             uint64 `json:"id"`
	UUID           string `json:"uuid"`
	ParentUUID     string `json:"parent_uuid"`
	Generation     uint64 `json:"generation"`
	Changed        uint64 `json:"changed_transaction"`
	Created        uint64 `json:"created_transaction"`
	Flags          uint64 `json:"flags"`
	Device         uint64 `json:"device"`
	MountID        uint64 `json:"mount_id"`
}

const (
	btrfsReadonly      = uint64(1 << 1)      // SNAP_CREATE_V2 uses BTRFS_SUBVOL_RDONLY.
	btrfsReadonlyRoot  = uint64(1)           // GET_SUBVOL_INFO returns root-item flags.
	btrfsSnapshotIOCTL = uintptr(0x50009417) // SNAP_CREATE_V2, 4096 bytes
	btrfsFSInfoIOCTL   = uintptr(0x8400941f) // FS_INFO, 1024 bytes
	btrfsRootInfoIOCTL = uintptr(0x81f8943c) // GET_SUBVOL_INFO, 504 bytes
	btrfsRootRefsIOCTL = uintptr(0xd000943d) // GET_SUBVOL_ROOTREF, 4096 bytes
)

var errNotSubvolume = errors.New("filesystem root is not a Btrfs subvolume")
var errNestedSubvolume = errors.New("native snapshot would omit a nested subvolume")

func subvolumeIOCTL(fd int, request uintptr, data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("empty subvolume ioctl argument")
	}
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), request,
		uintptr(unsafe.Pointer(&data[0])))
	runtime.KeepAlive(data)
	if errno != 0 {
		return errno
	}
	return nil
}

func subvolumeFilesystem(fd int) (string, error) {
	var filesystem unix.Statfs_t
	if err := unix.Fstatfs(fd, &filesystem); err != nil {
		return "", err
	}
	if filesystem.Type != unix.BTRFS_SUPER_MAGIC {
		return "", errNotSubvolume
	}
	data := make([]byte, 1024)
	if err := subvolumeIOCTL(fd, btrfsFSInfoIOCTL, data); err != nil {
		return "", err
	}
	if binary.LittleEndian.Uint64(data[:8]) != 1 || binary.LittleEndian.Uint64(data[8:16]) != 1 {
		return "", fmt.Errorf("native snapshots require the admitted single-device filesystem")
	}
	return hex.EncodeToString(data[16:32]), nil
}

func inspectSubvolume(root *os.File) (identity subvolumeIdentity, resultErr error) {
	fd := int(root.Fd())
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return identity, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Ino != 256 {
		return identity, errNotSubvolume
	}
	identity.FilesystemUUID, resultErr = subvolumeFilesystem(fd)
	if resultErr != nil {
		return identity, resultErr
	}
	var mount unix.Statx_t
	if err := unix.Statx(fd, "", unix.AT_EMPTY_PATH, unix.STATX_MNT_ID, &mount); err != nil {
		return identity, err
	}
	if mount.Mask&unix.STATX_MNT_ID == 0 || mount.Mnt_id == 0 {
		return identity, fmt.Errorf("native snapshot mount identity is absent")
	}
	data := make([]byte, 504)
	if err := subvolumeIOCTL(fd, btrfsRootInfoIOCTL, data); err != nil {
		return identity, err
	}
	identity.ID = binary.LittleEndian.Uint64(data[:8])
	identity.Generation = binary.LittleEndian.Uint64(data[280:288])
	identity.Flags = binary.LittleEndian.Uint64(data[288:296])
	identity.UUID = hex.EncodeToString(data[296:312])
	identity.ParentUUID = hex.EncodeToString(data[312:328])
	identity.Changed = binary.LittleEndian.Uint64(data[344:352])
	identity.Created = binary.LittleEndian.Uint64(data[352:360])
	identity.Device, identity.MountID = uint64(stat.Dev), mount.Mnt_id
	if identity.ID < 256 || identity.Generation == 0 || identity.Created == 0 ||
		identity.UUID == "00000000000000000000000000000000" ||
		identity.Flags&^btrfsReadonlyRoot != 0 {
		return identity, fmt.Errorf("unsupported or incomplete native subvolume identity: id=%d generation=%d created=%d flags=%x",
			identity.ID, identity.Generation, identity.Created, identity.Flags)
	}
	return identity, nil
}

// cloneSubvolume creates one independent kernel snapshot. The caller still owns
// source admission, process/filesystem coherence, publication and retirement.
// In particular this primitive is not permission
// to replace a checkpoint's existing complete-content manifest.
func cloneSubvolume(ctx context.Context, source *os.File, destination string, readonly bool) (identity subvolumeIdentity, resultErr error) {
	if err := ctx.Err(); err != nil {
		return identity, err
	}
	if !cleanAbsolute(destination) {
		return identity, fmt.Errorf("native snapshot destination is not canonical")
	}
	origin, err := inspectSubvolume(source)
	if err != nil {
		return identity, err
	}
	// Btrfs snapshots are not recursive. Refuse even one child before creating
	// a destination rather than silently publishing the child's empty stub.
	if err := noNestedSubvolumes(source); err != nil {
		return identity, err
	}
	if err := immutableParents(destination); err != nil {
		return identity, err
	}
	parentFD, err := unix.Openat2(unix.AT_FDCWD, filepath.Dir(destination), &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOATIME,
		Resolve: unix.RESOLVE_NO_SYMLINKS,
	})
	if err != nil {
		return identity, err
	}
	parent := os.NewFile(uintptr(parentFD), "native snapshot parent")
	defer func() { resultErr = errors.Join(resultErr, parent.Close()) }()
	filesystem, err := subvolumeFilesystem(parentFD)
	if err != nil || filesystem != origin.FilesystemUUID {
		return identity, errors.Join(err, fmt.Errorf("native snapshot crosses its admitted filesystem"))
	}
	var enclosing unix.Statx_t
	if err := unix.Statx(parentFD, "", unix.AT_EMPTY_PATH, unix.STATX_MNT_ID, &enclosing); err != nil {
		return identity, err
	}
	if enclosing.Mask&unix.STATX_MNT_ID == 0 || enclosing.Mnt_id != origin.MountID {
		return identity, fmt.Errorf("native snapshot crosses a foreign mount")
	}
	name := filepath.Base(destination)
	if len(name) > 4039 {
		return identity, fmt.Errorf("native snapshot name exceeds its kernel bound")
	}
	data := make([]byte, 4096)
	binary.LittleEndian.PutUint64(data[:8], uint64(source.Fd()))
	if readonly {
		binary.LittleEndian.PutUint64(data[16:24], btrfsReadonly)
	}
	copy(data[56:], name)
	// SNAP_CREATE_V2 refuses an existing name atomically. No failed or uncertain
	// destination is removed, covered or retried under another identity.
	if err := subvolumeIOCTL(parentFD, btrfsSnapshotIOCTL, data); err != nil {
		return identity, err
	}
	target, err := openDirectory(destination)
	if err != nil {
		return identity, err
	}
	defer func() { resultErr = errors.Join(resultErr, target.Close()) }()
	if err := errors.Join(unix.Syncfs(int(target.Fd())), parent.Sync()); err != nil {
		return identity, err
	}
	if err := ctx.Err(); err != nil {
		return identity, err
	}
	identity, err = inspectSubvolume(target)
	if err != nil {
		return identity, err
	}
	wantFlags := uint64(0)
	if readonly {
		wantFlags = btrfsReadonlyRoot
	}
	if identity.FilesystemUUID != origin.FilesystemUUID || identity.ID == origin.ID ||
		identity.UUID == origin.UUID || identity.ParentUUID != origin.UUID || identity.Flags != wantFlags {
		return identity, fmt.Errorf("native snapshot differs from its original kernel lineage")
	}
	return identity, nil
}

func verifySubvolume(path string, expected subvolumeIdentity) error {
	if !cleanAbsolute(path) || expected.Flags != btrfsReadonlyRoot {
		return fmt.Errorf("immutable native verification requires its sealed snapshot identity")
	}
	if err := immutableParents(path); err != nil {
		return err
	}
	root, err := openDirectory(path)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := unix.Syncfs(int(root.Fd())); err != nil {
		return err
	}
	actual, err := inspectSubvolume(root)
	if err != nil {
		return err
	}
	// Creating a descendant advances the sealed source's bookkeeping
	// generation without changing its contents. GET_SUBVOL_INFO's ctransid
	// remains stable for that operation, but changes for a modified root.
	// UUID, lineage, creation/change transactions and the read-only flag must
	// remain exact. A generation rollback is not an allowed descendant effect.
	if actual.Generation < expected.Generation {
		return fmt.Errorf("sealed native snapshot generation moved backwards")
	}
	expected.Generation = actual.Generation
	if actual != expected {
		return fmt.Errorf("sealed native snapshot identity, generation or read-only property changed: expected=%+v actual=%+v",
			expected, actual)
	}
	return noImmutableDescendantMounts(path)
}
