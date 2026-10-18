package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// Hash complete captured pages once while the kernel excludes writable
// descriptors/mappings. The same held descriptors then supply FICLONE.
func descendantProtectCapture(layout descendantLayout, images string) (_ *descendantBackingLeases, _ map[string]string, resultErr error) {
	if err := descendantRAMPath(layout, images, true); err != nil {
		return nil, nil, err
	}
	if _, err := descendantLeaseAdmission(layout); err != nil {
		return nil, nil, err
	}
	fd, err := unix.Open(images, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, err
	}
	directory := os.NewFile(uintptr(fd), images)
	info, err := directory.Stat()
	if err != nil {
		directory.Close()
		return nil, nil, err
	}
	identity := descendantLeaseFileIdentity(info)
	if !info.IsDir() || info.Mode().Perm() != 0700 || identity.UID != 165536 || identity.GID != 165536 {
		directory.Close()
		return nil, nil, fmt.Errorf("capture lost private mapped-root directory ownership")
	}
	protection := &descendantBackingLeases{Path: images, Directory: directory, Identity: identity,
		Pages: map[string]*descendantFileLease{}}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, protection.close(false))
		}
	}()
	entries, err := criuRAMImageEntries(images, false)
	if err != nil || len(entries) > 8192 {
		return nil, nil, fmt.Errorf("capture entries exceed complete bounded scope: %v", err)
	}
	var total uint64
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || info.Size() < 0 ||
			uint64(info.Size()) > criuRAMSnapshotLimit-total {
			return nil, nil, fmt.Errorf("capture payload is not regular and bounded: %v", err)
		}
		total += uint64(info.Size())
		if !criuRAMPageName.MatchString(entry.Name()) {
			continue
		}
		if len(protection.Pages) >= 128 {
			return nil, nil, fmt.Errorf("capture exceeds bounded page-file count")
		}
		lease, err := descendantAcquireFileLease(filepath.Join(images, entry.Name()))
		if err != nil {
			return nil, nil, err
		}
		protection.Pages[entry.Name()] = lease
		if lease.Saved.Mode&07777 != 0600 || lease.Saved.UID != identity.UID ||
			lease.Saved.GID != identity.GID {
			return nil, nil, fmt.Errorf("captured pages lost mapped-root private ownership")
		}
		protection.Bytes += lease.Saved.Size
	}
	if len(protection.Pages) == 0 {
		return nil, nil, fmt.Errorf("capture lacks complete saved pages")
	}
	manifest, err := descendantSnapshotManifestProtected(images, protection)
	if err != nil {
		return nil, nil, err
	}
	return protection, manifest, nil
}

func descendantCloneLeasedPage(lease *descendantFileLease, destination string) (resultErr error) {
	if err := lease.verify(); err != nil {
		return err
	}
	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	// Success is mandatory. A copying fallback cannot certify target bytes
	// without reading them and therefore must not use this verification path.
	if err := unix.IoctlFileClone(int(file.Fd()), int(lease.File.Fd())); err != nil {
		return fmt.Errorf("complete kernel reflink is unsupported or failed: %w", err)
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
	if !info.Mode().IsRegular() || saved.Device != lease.Saved.Device || saved.Inode == lease.Saved.Inode ||
		saved.Size != lease.Saved.Size || saved.Mode != lease.Saved.Mode ||
		saved.UID != lease.Saved.UID || saved.GID != lease.Saved.GID {
		return fmt.Errorf("kernel reflink did not create complete independently owned pages")
	}
	current, err := os.Lstat(destination)
	if err != nil || !os.SameFile(current, info) || descendantLeaseFileIdentity(current) != saved {
		return fmt.Errorf("kernel reflink destination changed: %v", err)
	}
	return nil
}

func descendantCloneProtectedCapture(protection *descendantBackingLeases, target descendantLayout, expected map[string]string) (map[string]string, map[string]any, error) {
	owned, err := descendantOwnedLayout(target.ID)
	if err != nil || target != owned || !descendantID.MatchString(target.ID) {
		return nil, nil, fmt.Errorf("reflink clone requires a canonical fresh descendant")
	}
	if err := protection.verify(); err != nil {
		return nil, nil, err
	}
	images := target.Root + "/images"
	if err := os.Mkdir(images, 0700); err != nil {
		return nil, nil, err
	}
	entries, err := criuRAMImageEntries(protection.Path, false)
	if err != nil {
		return nil, nil, err
	}
	verified := map[string]string{}
	buffer := make([]byte, 64<<10)
	var bytes int64
	var pageFiles, metadataFiles int
	for _, entry := range entries {
		source, destination := filepath.Join(protection.Path, entry.Name()), filepath.Join(images, entry.Name())
		if criuRAMPageName.MatchString(entry.Name()) {
			lease := protection.Pages[entry.Name()]
			if lease == nil || lease.SHA256 != expected[entry.Name()] {
				return nil, nil, fmt.Errorf("cloned page file lacks its original held hash")
			}
			if err := descendantCloneLeasedPage(lease, destination); err != nil {
				return nil, nil, err
			}
			verified[entry.Name()] = lease.SHA256
			pageFiles++
			bytes += lease.Saved.Size
		} else {
			copied, err := criuRAMExportFile(source, destination, "", "", false, buffer)
			if err != nil {
				return nil, nil, err
			}
			if checksum, payload := expected[entry.Name()]; payload {
				if copied.SHA256 != checksum {
					return nil, nil, fmt.Errorf("private reflink metadata readback differs from complete source hash")
				}
				verified[entry.Name()] = copied.SHA256
				metadataFiles++
			}
		}
	}
	if len(verified) != len(expected) || pageFiles != len(protection.Pages) {
		return nil, nil, fmt.Errorf("reflink clone omitted complete source payload")
	}
	if err := errors.Join(os.Chown(images, int(protection.Identity.UID), int(protection.Identity.GID)),
		os.Chmod(images, os.FileMode(protection.Identity.Mode&07777)), protection.verify()); err != nil {
		return nil, nil, err
	}
	return verified, map[string]any{
		"kernel_reflink_page_files": pageFiles, "kernel_reflink_page_bytes": bytes,
		"private_readback_verified_metadata_files": metadataFiles,
		"target_page_hash_read_bytes":              int64(0),
		"source_read_leases_validated":             true,
		"verification":                             "complete_original_fd_hash_then_successful_FICLONE_under_read_lease",
		"scope":                                    "No fallback or shared mutable inode; later complete byte hashes and independent workload mutations remain mandatory.",
	}, nil
}
