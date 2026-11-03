//go:build linux

package storefs

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

// These fixed-size requests are the Linux UAPI btrfs_ioctl_fs_info_args and
// btrfs_ioctl_dev_info_args on both admitted 64-bit architectures.
const (
	btrfsFilesystemInfo = 0x8400941f
	btrfsDeviceInfo     = 0xd000941e
)

// VerifyObjectBacking keeps the capacity boundary tied to the admitted image,
// not to one virtual st_dev value. Btrfs assigns another virtual device to a
// child subvolume even though it remains inside that same fixed filesystem.
// Such a child must independently prove the exact filesystem UUID and original
// store mount. A path on the host's Btrfs filesystem is not equivalent backing.
func VerifyObjectBacking(active Active, path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path ||
		(path != active.Target && !strings.HasPrefix(path, active.Target+"/")) {
		return fmt.Errorf("object is outside the admitted fixed filesystem")
	}
	if err := Verify(active); err != nil {
		return err
	}
	fd, err := unix.Openat2(unix.AT_FDCWD, path, &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK,
		Resolve: unix.RESOLVE_NO_SYMLINKS,
	})
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR && stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return fmt.Errorf("fixed-store object is not a directory or regular file")
	}
	if uint64(stat.Dev) == active.BlockDevice {
		return nil
	}
	if active.Image.Schema != 2 {
		return fmt.Errorf("object is not backed by its admitted fixed filesystem")
	}
	var mount unix.Statx_t
	var filesystem unix.Statfs_t
	if err := unix.Statx(fd, "", unix.AT_EMPTY_PATH, unix.STATX_MNT_ID, &mount); err != nil {
		return err
	}
	if err := unix.Fstatfs(fd, &filesystem); err != nil {
		return err
	}
	if mount.Mask&unix.STATX_MNT_ID == 0 || mount.Mnt_id != active.Mount.MountID ||
		filesystem.Type != unix.BTRFS_SUPER_MAGIC {
		return fmt.Errorf("child subvolume is on a foreign mount")
	}
	var info [1024]byte
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), btrfsFilesystemInfo,
		uintptr(unsafe.Pointer(&info[0]))); errno != 0 {
		return errno
	}
	if binary.LittleEndian.Uint64(info[:8]) != 1 || binary.LittleEndian.Uint64(info[8:16]) != 1 ||
		hex.EncodeToString(info[16:32]) != active.Image.UUID {
		return fmt.Errorf("child subvolume is outside the original single-device store")
	}
	return nil
}

// ValidateAttachmentIdentity checks saved metadata without adopting a mount.
// BlockDevice is the filesystem's st_dev. A CoW filesystem has a virtual
// device identity distinct from the physical loop device, which LoopNumber
// continues to bind through the independently verified kernel association.
func ValidateAttachmentIdentity(active Active) error {
	if !active.DirectIO || active.LoopNumber < 0 || active.LoopNumber > 1<<20 ||
		active.BlockDevice == 0 || active.Mount.Device != active.BlockDevice ||
		active.Mount.Inode == 0 || active.Mount.MountID == 0 ||
		active.FilesystemMax == 0 || active.FilesystemMax > uint64(active.Image.Bytes) ||
		(active.Image.Schema != 1 && active.Image.Schema != 2) ||
		(active.Image.Schema == 1 && active.BlockDevice != unix.Mkdev(7, uint32(active.LoopNumber))) {
		return fmt.Errorf("invalid active fixed filesystem identity")
	}
	return nil
}

func verifyCopyOnWriteMount(active Active) error {
	fd, err := unix.Open(active.Target, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	return verifyCopyOnWriteFD(active, fd)
}

func verifyCopyOnWriteFD(active Active, fd int) error {
	var stat unix.Stat_t
	var mount unix.Statx_t
	var filesystem unix.Statfs_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return err
	}
	if err := unix.Statx(fd, "", unix.AT_EMPTY_PATH, unix.STATX_MNT_ID, &mount); err != nil {
		return err
	}
	if err := unix.Fstatfs(fd, &filesystem); err != nil {
		return err
	}
	if uint64(stat.Dev) != active.BlockDevice || stat.Ino != active.Mount.Inode ||
		mount.Mnt_id != active.Mount.MountID || filesystem.Type != unix.BTRFS_SUPER_MAGIC {
		return fmt.Errorf("copy-on-write filesystem mount identity differs")
	}
	var info [1024]byte
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), btrfsFilesystemInfo,
		uintptr(unsafe.Pointer(&info[0]))); errno != 0 {
		return errno
	}
	if binary.LittleEndian.Uint64(info[:8]) != 1 || binary.LittleEndian.Uint64(info[8:16]) != 1 ||
		hex.EncodeToString(info[16:32]) != active.Image.UUID {
		return fmt.Errorf("copy-on-write mount is not the original single-device filesystem")
	}
	var device [4096]byte
	binary.LittleEndian.PutUint64(device[:8], 1)
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), btrfsDeviceInfo,
		uintptr(unsafe.Pointer(&device[0]))); errno != 0 {
		return errno
	}
	path := device[3072:]
	terminator := bytes.IndexByte(path, 0)
	if terminator <= 0 || binary.LittleEndian.Uint64(device[32:40]) != uint64(active.Image.Bytes) {
		return fmt.Errorf("copy-on-write filesystem device geometry differs")
	}
	var native unix.Stat_t
	if err := unix.Lstat(string(path[:terminator]), &native); err != nil {
		return err
	}
	if native.Mode&unix.S_IFMT != unix.S_IFBLK || native.Uid != 0 ||
		uint64(native.Rdev) != unix.Mkdev(7, uint32(active.LoopNumber)) {
		return fmt.Errorf("copy-on-write filesystem is not backed by its original loop device")
	}
	return nil
}
