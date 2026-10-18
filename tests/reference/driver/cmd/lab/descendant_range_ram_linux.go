package main

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

// The caller admits ancestors only, never a mutable current head. No ioctl
// error can select this path implicitly. It copies exactly the resolved range
// and records its digest. The assembler syncs and checks every copied extent
// as one batch while all original leases remain held.
type descendantRAMRangeReadback struct {
	Offset, Bytes int64
	SHA256        [32]byte
}

func descendantCopyLeasedRAMRange(lease *descendantFileLease, target *os.File,
	offset, length, destination int64) (_ descendantRAMRangeReadback, resultErr error) {
	var result descendantRAMRangeReadback
	if err := lease.verify(); err != nil {
		return result, err
	}
	var sourceFS, targetFS unix.Statfs_t
	if err := unix.Fstatfs(int(lease.File.Fd()), &sourceFS); err != nil ||
		sourceFS.Type != unix.TMPFS_MAGIC || sourceFS.Flags&unix.ST_RDONLY == 0 ||
		sourceFS.Bsize <= 0 || sourceFS.Blocks > criuRAMSnapshotLimit/uint64(sourceFS.Bsize) ||
		lease.Saved.Size < 0 || uint64(lease.Saved.Size) > criuRAMSnapshotLimit {
		return result, fmt.Errorf("RAM range copy requires a bounded readonly tmpfs ancestor: %v", err)
	}
	if err := unix.Fstatfs(int(target.Fd()), &targetFS); err != nil || targetFS.Type != unix.BTRFS_SUPER_MAGIC {
		return result, fmt.Errorf("RAM range copy requires the tested Btrfs destination: %v", err)
	}
	info, err := target.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != destination ||
		offset < 0 || length <= 0 || destination < 0 || offset%4096 != 0 || length%4096 != 0 ||
		destination%4096 != 0 || offset > lease.Saved.Size || length > lease.Saved.Size-offset {
		return result, fmt.Errorf("RAM range copy is outside its exact source/output extent: %v", err)
	}
	buffer := make([]byte, 64<<10)
	original := sha256.New()
	written, err := io.CopyBuffer(io.MultiWriter(io.NewOffsetWriter(target, destination), original),
		io.NewSectionReader(lease.File, offset, length), buffer)
	if err != nil || written != length {
		return result, fmt.Errorf("RAM range copy is incomplete: %d/%d %v", written, length, err)
	}
	info, err = target.Stat()
	if err != nil || info.Size() != destination+length {
		return result, fmt.Errorf("RAM range destination size changed: %v", err)
	}
	result.Offset, result.Bytes = destination, length
	copy(result.SHA256[:], original.Sum(nil))
	return result, lease.verify()
}

func descendantVerifySyncedRAMRanges(target *os.File, ranges []descendantRAMRangeReadback) error {
	if err := target.Sync(); err != nil {
		return err
	}
	buffer := make([]byte, 64<<10)
	for _, extent := range ranges {
		hash := sha256.New()
		read, err := io.CopyBuffer(hash, io.NewSectionReader(target, extent.Offset, extent.Bytes), buffer)
		var checksum [32]byte
		copy(checksum[:], hash.Sum(nil))
		if err != nil || read != extent.Bytes || checksum != extent.SHA256 {
			return fmt.Errorf("synced RAM range differs from its original leased bytes: %d/%d %v", read, extent.Bytes, err)
		}
	}
	return nil
}
