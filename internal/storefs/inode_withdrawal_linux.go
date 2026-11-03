//go:build linux

package storefs

import (
	"encoding/binary"
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
)

const btrfsInodeLookup = 0xd0009412 // btrfs_ioctl_ino_lookup_args, 4096 bytes.

// VerifyDirectoryUnlinked proves absence in the original filesystem tree,
// without treating a replacement at the old pathname as the original owner.
// The store format has one top-level filesystem tree. A different default
// subvolume, unknown ioctl result or still-linked inode is not withdrawal.
func VerifyDirectoryUnlinked(active Active, inode uint64) (uint64, error) {
	if active.Image.Schema != 2 || inode <= 256 {
		return 0, fmt.Errorf("directory withdrawal requires an ordinary inode in the bounded CoW store")
	}
	if err := Verify(active); err != nil {
		return 0, err
	}
	fd, err := unix.Open(active.Target, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return 0, err
	}
	defer unix.Close(fd)
	if err := verifyCopyOnWriteFD(active, fd); err != nil {
		return 0, err
	}
	var root [4096]byte
	binary.LittleEndian.PutUint64(root[8:16], 256)
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), btrfsInodeLookup,
		uintptr(unsafe.Pointer(&root[0]))); errno != 0 {
		return 0, errno
	}
	tree := binary.LittleEndian.Uint64(root[:8])
	if tree != 5 {
		return 0, fmt.Errorf("directory withdrawal cannot inspect a substituted filesystem tree")
	}
	var lookup [4096]byte
	binary.LittleEndian.PutUint64(lookup[:8], tree)
	binary.LittleEndian.PutUint64(lookup[8:16], inode)
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), btrfsInodeLookup,
		uintptr(unsafe.Pointer(&lookup[0])))
	if errors.Is(errno, unix.ENOENT) {
		return tree, nil
	}
	if errno != 0 {
		return 0, errno
	}
	return 0, fmt.Errorf("original directory still has a linked path in its admitted filesystem")
}
