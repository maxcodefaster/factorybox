package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

// Cross-filesystem copies are limited to an explicitly admitted readonly RAM
// ancestor. Both the held source and the independent synced destination are
// read completely. This path cannot claim kernel-certified zero-copy bytes.
func descendantCopyLeasedRAMParent(lease *descendantFileLease, destination string) (resultErr error) {
	if err := lease.verify(); err != nil {
		return err
	}
	var sourceFS unix.Statfs_t
	if err := unix.Fstatfs(int(lease.File.Fd()), &sourceFS); err != nil ||
		sourceFS.Type != unix.TMPFS_MAGIC || sourceFS.Flags&unix.ST_RDONLY == 0 ||
		lease.Saved.Size < 0 || uint64(lease.Saved.Size) > criuRAMSnapshotLimit ||
		uint64(sourceFS.Blocks)*uint64(sourceFS.Bsize) > criuRAMSnapshotLimit {
		return fmt.Errorf("RAM parent copy requires a bounded readonly tmpfs source: type=%#x flags=%#x "+
			"filesystem_bytes=%d file_bytes=%d error=%v", sourceFS.Type, sourceFS.Flags,
			uint64(sourceFS.Blocks)*uint64(sourceFS.Bsize), lease.Saved.Size, err)
	}
	file, err := os.OpenFile(destination, os.O_RDWR|os.O_CREATE|os.O_EXCL|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	var targetFS unix.Statfs_t
	if err := unix.Fstatfs(int(file.Fd()), &targetFS); err != nil || targetFS.Type != unix.BTRFS_SUPER_MAGIC {
		return fmt.Errorf("RAM parent copy requires the tested Btrfs backing: %v", err)
	}
	written, err := io.CopyBuffer(file, io.NewSectionReader(lease.File, 0, lease.Saved.Size), make([]byte, 64<<10))
	if err != nil || written != lease.Saved.Size {
		return fmt.Errorf("RAM parent copy is incomplete: %d/%d %v", written, lease.Saved.Size, err)
	}
	if err := file.Sync(); err != nil {
		return err
	}
	checksum, err := descendantLeaseHash(file)
	if err != nil || checksum != lease.SHA256 {
		return fmt.Errorf("RAM parent destination differs from the held original bytes: %v", err)
	}
	if err := errors.Join(file.Chown(int(lease.Saved.UID), int(lease.Saved.GID)),
		file.Chmod(os.FileMode(lease.Saved.Mode&07777)), file.Sync(), lease.verify()); err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		return err
	}
	saved := descendantLeaseFileIdentity(info)
	if !info.Mode().IsRegular() || saved.Device == lease.Saved.Device ||
		saved.Size != lease.Saved.Size || saved.Mode != lease.Saved.Mode ||
		saved.UID != lease.Saved.UID || saved.GID != lease.Saved.GID {
		return fmt.Errorf("RAM parent backing is not complete, private and independently owned")
	}
	current, err := os.Lstat(destination)
	if err != nil || !os.SameFile(current, info) || descendantLeaseFileIdentity(current) != saved {
		return fmt.Errorf("RAM parent destination path changed: %v", err)
	}
	return nil
}
