package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

type descendantLeaseIdentity struct {
	Device, Inode, Links uint64
	Size                 int64
	Mode, UID, GID       uint32
	Modified, Changed    syscall.Timespec
}

func descendantLeaseFileIdentity(info os.FileInfo) descendantLeaseIdentity {
	value := info.Sys().(*syscall.Stat_t)
	return descendantLeaseIdentity{uint64(value.Dev), value.Ino, uint64(value.Nlink), value.Size, value.Mode,
		value.Uid, value.Gid, value.Mtim, value.Ctim}
}

type descendantFileLease struct {
	Path   string
	File   *os.File
	Saved  descendantLeaseIdentity
	SHA256 string
	closed bool
}

func descendantLeaseHash(file *os.File) (string, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	hash := sha256.New()
	var err error
	if descendantCoalescedHashesEnabled() {
		_, err = descendantCopySnapshotHash(hash, file)
	} else {
		_, err = io.CopyBuffer(hash, file, make([]byte, 64<<10))
	}
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// Callers handle SIGIO before acquiring a lease. F_GETLEASE reports a pending
// break before a blocked writer may change data. Original FD/inode/path checks
// must succeed on every reuse; a successful initial hash alone is insufficient.
func descendantAcquireFileLease(path string) (_ *descendantFileLease, resultErr error) {
	return descendantAcquireFileLeasePolicy(path, true)
}

// Range assembly does not use an input page-file digest as its certificate.
// Its original-FD read lease and identity checks remain mandatory, while the
// fully assembled output and original-writer comparison certify target bytes.
// Other callers retain their original complete-hash admission.
func descendantAcquireFileLeasePolicy(path string, hashContents bool) (_ *descendantFileLease, resultErr error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	held := false
	defer func() {
		if resultErr != nil {
			if held {
				unix.FcntlInt(file.Fd(), unix.F_SETLEASE, unix.F_UNLCK)
			}
			file.Close()
		}
	}()
	if _, err := unix.FcntlInt(file.Fd(), unix.F_SETLEASE, unix.F_RDLCK); err != nil {
		return nil, fmt.Errorf("acquire read lease: %w", err)
	}
	held = true
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() < 0 {
		return nil, fmt.Errorf("lease requires one regular backing inode: %v", err)
	}
	checksum := ""
	if hashContents {
		checksum, err = descendantLeaseHash(file)
		if err != nil {
			return nil, err
		}
	}
	lease := &descendantFileLease{Path: path, File: file, Saved: descendantLeaseFileIdentity(before), SHA256: checksum}
	if err := lease.verify(); err != nil {
		return nil, err
	}
	return lease, nil
}

func (lease *descendantFileLease) verify() error {
	if lease.closed {
		return fmt.Errorf("backing lease is closed")
	}
	kind, err := unix.FcntlInt(lease.File.Fd(), unix.F_GETLEASE, 0)
	if err != nil || kind != unix.F_RDLCK {
		return fmt.Errorf("read lease is absent or breaking: type=%d error=%v", kind, err)
	}
	anchor, err := lease.File.Stat()
	if err != nil || descendantLeaseFileIdentity(anchor) != lease.Saved {
		return fmt.Errorf("held leased inode identity changed: %v", err)
	}
	current, err := os.Lstat(lease.Path)
	if err != nil || !current.Mode().IsRegular() || descendantLeaseFileIdentity(current) != lease.Saved {
		return fmt.Errorf("leased backing path changed: %v", err)
	}
	kind, err = unix.FcntlInt(lease.File.Fd(), unix.F_GETLEASE, 0)
	if err != nil || kind != unix.F_RDLCK {
		return fmt.Errorf("read lease changed during verification: %d %v", kind, err)
	}
	return nil
}

func (lease *descendantFileLease) close() error {
	if lease.closed {
		return nil
	}
	lease.closed = true
	_, err := unix.FcntlInt(lease.File.Fd(), unix.F_SETLEASE, unix.F_UNLCK)
	return errors.Join(err, lease.File.Close())
}

type descendantBackingLeases struct {
	Path       string
	Directory  *os.File
	Identity   descendantLeaseIdentity
	Pages      map[string]*descendantFileLease
	Bytes      int64
	VerifyRuns []map[string]any
	closed     bool
}

func descendantLeaseAdmission(target descendantLayout) (map[string]any, error) {
	owned, err := descendantOwnedLayout(target.ID)
	if err != nil || owned != target {
		return nil, fmt.Errorf("backing lease admission is outside owned runtime")
	}
	var fs unix.Statfs_t
	if err := unix.Statfs(target.Root, &fs); err != nil || fs.Type != unix.BTRFS_SUPER_MAGIC {
		return nil, fmt.Errorf("backing lease policy supports only the natively tested Btrfs worker: %v", err)
	}
	header := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	var capabilities [2]unix.CapUserData
	if err := unix.Capget(&header, &capabilities[0]); err != nil {
		return nil, err
	}
	if capabilities[unix.CAP_LEASE/32].Effective&(uint32(1)<<uint(unix.CAP_LEASE%32)) == 0 {
		return nil, fmt.Errorf("mapped-root backing publication requires effective CAP_LEASE before pause")
	}
	signal.Ignore(syscall.SIGIO)
	return map[string]any{"filesystem_type": fs.Type, "cap_lease_effective": true,
		"scope": "Read-only native-worker admission before freezing/checkpointing. No kernel/sysctl changes."}, nil
}

func descendantProtectBacking(target descendantLayout, images string, expected map[string]string) (_ *descendantBackingLeases, resultErr error) {
	if filepath.Dir(images) != target.Root || !descendantCaptureGeneration.MatchString(filepath.Base(images)) ||
		len(expected) < 4 || len(expected) > 8192 {
		return nil, fmt.Errorf("backing publication is outside one complete owned generation")
	}
	fd, err := unix.Open(images, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	directory := os.NewFile(uintptr(fd), images)
	info, err := directory.Stat()
	if err != nil {
		directory.Close()
		return nil, err
	}
	identity := descendantLeaseFileIdentity(info)
	if !info.IsDir() || info.Mode().Perm() != 0700 || identity.UID != 165536 || identity.GID != 165536 {
		directory.Close()
		return nil, fmt.Errorf("backing publication lost private mapped-root ownership")
	}
	protection := &descendantBackingLeases{Path: images, Directory: directory, Identity: identity,
		Pages: map[string]*descendantFileLease{}}
	defer func() {
		if resultErr != nil {
			protection.close(false)
		}
	}()
	for name, checksum := range expected {
		if !criuRAMPageName.MatchString(name) {
			continue
		}
		if filepath.Base(name) != name || len(checksum) != 64 || len(protection.Pages) >= 128 {
			return nil, fmt.Errorf("invalid or excessive backing page lease")
		}
		lease, err := descendantAcquireFileLease(filepath.Join(images, name))
		if err != nil {
			return nil, err
		}
		protection.Pages[name] = lease
		if lease.Saved.Size > int64(criuRAMSnapshotLimit)-protection.Bytes ||
			lease.Saved.Mode&07777 != 0600 || lease.Saved.UID != 165536 || lease.Saved.GID != 165536 ||
			lease.SHA256 != checksum {
			return nil, fmt.Errorf("complete leased backing bytes/identity differ from original captured writer: %s", name)
		}
		protection.Bytes += lease.Saved.Size
	}
	if len(protection.Pages) == 0 {
		return nil, fmt.Errorf("no complete page payload received backing protection")
	}
	if err := protection.verify(); err != nil {
		return nil, err
	}
	return protection, nil
}

func (protection *descendantBackingLeases) verify() error {
	if protection.closed {
		return fmt.Errorf("backing protection is closed")
	}
	current, err := os.Lstat(protection.Path)
	anchor, anchorErr := protection.Directory.Stat()
	if err != nil || anchorErr != nil || !current.IsDir() || !os.SameFile(current, anchor) {
		return fmt.Errorf("backing directory inode changed: %v %v", err, anchorErr)
	}
	value := descendantLeaseFileIdentity(current)
	saved := protection.Identity
	if value.Device != saved.Device || value.Inode != saved.Inode || value.UID != saved.UID ||
		value.GID != saved.GID || value.Mode != saved.Mode {
		return fmt.Errorf("backing directory ownership/identity changed")
	}
	for _, lease := range protection.Pages {
		if err := lease.verify(); err != nil {
			return err
		}
	}
	return nil
}

func (protection *descendantBackingLeases) page(name string, file *os.File) (string, error) {
	lease := protection.Pages[name]
	if lease == nil {
		return "", fmt.Errorf("unregistered page payload in protected backing: %s", name)
	}
	if err := lease.verify(); err != nil {
		return "", err
	}
	info, err := file.Stat()
	if err != nil || descendantLeaseFileIdentity(info) != lease.Saved {
		return "", fmt.Errorf("opened page payload differs from original lease: %v", err)
	}
	return lease.SHA256, nil
}

func (protection *descendantBackingLeases) close(validate bool) (resultErr error) {
	if protection == nil || protection.closed {
		return nil
	}
	if validate {
		resultErr = protection.verify()
	}
	for _, lease := range protection.Pages {
		resultErr = errors.Join(resultErr, lease.close())
	}
	protection.closed = true
	return errors.Join(resultErr, protection.Directory.Close())
}
