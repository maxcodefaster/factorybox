package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Page contents are deliberately not hashed here. Their original open inodes
// stay read-leased through the actual complete target byte comparison and final
// target verification. Metadata still receives complete hashes.
type descendantSourceProtection struct {
	Backing    *descendantBackingLeases
	Metadata   map[string]*descendantFileLease
	Parent     string
	ParentLink string
	ParentID   descendantLeaseIdentity
	PageFiles  int
	MetaFiles  int
	VerifyRuns []map[string]any
	closed     bool
}

func descendantProtectInheritedSource(layout descendantLayout, capture *descendantInheritedCapture) (
	_ *descendantSourceProtection, _ map[string]string, resultErr error) {
	owned, err := descendantOwnedLayout(layout.ID)
	if err != nil || owned != layout || layout.ID == "source-sysbox" || capture == nil ||
		capture.Proof["ordinary_fast_collector"] != true || capture.OriginalImages != "" ||
		capture.Proof["same_capture_original_writer"] != false ||
		capture.Proof["single_source_capture"] != true || capture.Proof["single_capture_transaction_completed"] != true ||
		capture.Proof["guard_safe_after_only_writer"] != true {
		return nil, nil, fmt.Errorf("source inode certificates require the acknowledged ordinary single collector")
	}
	runtime, bound := capture.Proof["runtime"].(descendantLayout)
	parent, parentOK := capture.Proof["parent_images"].(string)
	if !bound || runtime != layout || !parentOK || filepath.Clean(capture.Images) != capture.Images ||
		filepath.Dir(capture.Images) != layout.Root || !descendantCaptureGeneration.MatchString(filepath.Base(capture.Images)) ||
		!descendantGraphWithin(layout.Root, parent) || capture.Images == parent {
		return nil, nil, fmt.Errorf("source inode certificate escaped its exact fresh capture")
	}
	actual, err := filepath.EvalSymlinks(capture.Images)
	if err != nil || actual != capture.Images {
		return nil, nil, fmt.Errorf("source inode certificate requires a canonical capture directory: %v", err)
	}
	parentInfo, err := os.Lstat(parent)
	if err != nil || !parentInfo.IsDir() {
		return nil, nil, fmt.Errorf("source inode certificate lost its bound parent: %v", err)
	}
	parentID := descendantLeaseFileIdentity(parentInfo)
	device, deviceOK := capture.Proof["parent_device"].(uint64)
	inode, inodeOK := capture.Proof["parent_inode"].(uint64)
	if !deviceOK || !inodeOK || device != parentID.Device || inode != parentID.Inode {
		return nil, nil, fmt.Errorf("source inode certificate parent differs from its acknowledged capture")
	}
	info, err := os.Lstat(capture.Images)
	if err != nil {
		return nil, nil, err
	}
	owner := descendantLeaseFileIdentity(info)
	if owner.UID != 165536 || owner.GID != 165536 || owner.Mode&07777 != 0700 {
		return nil, nil, fmt.Errorf("source inode certificate lost private mapped-root ownership")
	}
	if _, err := descendantLeaseAdmission(layout); err != nil {
		return nil, nil, err
	}
	backing, err := descendantNewRangeProtection(capture.Images, owner)
	if err != nil {
		return nil, nil, err
	}
	link, err := filepath.Rel(capture.Images, parent)
	if err != nil {
		backing.close(false)
		return nil, nil, err
	}
	protection := &descendantSourceProtection{Backing: backing, Parent: parent, ParentLink: link,
		ParentID: parentID, Metadata: map[string]*descendantFileLease{}}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, protection.close(false))
		}
	}()
	entries, err := os.ReadDir(capture.Images)
	if err != nil || len(entries) > 8192 {
		return nil, nil, fmt.Errorf("source inode certificate population is outside its bound: %v", err)
	}
	for _, entry := range entries {
		if entry.Name() == "parent" {
			continue
		}
		path := filepath.Join(capture.Images, entry.Name())
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			return nil, nil, fmt.Errorf("source inode certificate contains an unexpected nonregular entry: %v", err)
		}
		if !criuInheritParentEntry(entry.Name()) && entry.Name() != "runtime-config.json" &&
			entry.Name() != "descriptors.json" {
			continue
		}
		lease, err := descendantAcquireFileLeasePolicy(path, false)
		if err != nil {
			return nil, nil, err
		}
		page := criuRAMPageName.MatchString(entry.Name())
		if page {
			backing.Pages[entry.Name()] = lease
		} else {
			protection.Metadata[entry.Name()] = lease
		}
		if lease.Saved.UID != 165536 || lease.Saved.GID != 165536 ||
			lease.Saved.Mode&07777 != 0600 || lease.Saved.Links != 1 || lease.Saved.Size < 0 {
			return nil, nil, fmt.Errorf("source inode certificate lost bounded private payload ownership")
		}
		if page {
			if len(backing.Pages) > 128 || lease.Saved.Size > int64(criuRAMSnapshotLimit)-backing.Bytes {
				return nil, nil, fmt.Errorf("source inode certificate page population exceeds its bound")
			}
			backing.Bytes += lease.Saved.Size
		}
	}
	if len(backing.Pages) == 0 || len(protection.Metadata) < 3 {
		return nil, nil, fmt.Errorf("source inode certificate lacks complete pages and metadata")
	}
	protection.PageFiles, protection.MetaFiles = len(backing.Pages), len(protection.Metadata)
	metadata, err := protection.manifest()
	if err != nil {
		return nil, nil, err
	}
	return protection, metadata, nil
}

func (protection *descendantSourceProtection) verify() error {
	if protection == nil || protection.closed {
		return fmt.Errorf("source inode certificate is closed")
	}
	if protection.PageFiles == 0 || len(protection.Backing.Pages) != protection.PageFiles ||
		len(protection.Metadata) != protection.MetaFiles {
		return fmt.Errorf("source inode certificate lost its original payload population")
	}
	if err := protection.Backing.verify(); err != nil {
		return err
	}
	directory, err := protection.Backing.Directory.Stat()
	if err != nil || descendantLeaseFileIdentity(directory) != protection.Backing.Identity {
		return fmt.Errorf("source inode certificate directory population changed: %v", err)
	}
	link, err := os.Readlink(filepath.Join(protection.Backing.Path, "parent"))
	parent, parentErr := os.Lstat(protection.Parent)
	if err != nil || parentErr != nil || link != protection.ParentLink || !parent.IsDir() {
		return fmt.Errorf("source inode certificate parent relationship changed: %v %v", err, parentErr)
	}
	parentID := descendantLeaseFileIdentity(parent)
	if parentID.Device != protection.ParentID.Device || parentID.Inode != protection.ParentID.Inode ||
		parentID.UID != protection.ParentID.UID || parentID.GID != protection.ParentID.GID || parentID.Mode != protection.ParentID.Mode {
		return fmt.Errorf("source inode certificate parent identity changed")
	}
	for _, lease := range protection.Metadata {
		if err := lease.verify(); err != nil {
			return err
		}
	}
	for _, lease := range protection.Backing.Pages {
		if lease.SHA256 != "" {
			return fmt.Errorf("source page identity certificate unexpectedly claims a complete page hash")
		}
	}
	return nil
}

func (protection *descendantSourceProtection) manifest() (_ map[string]string, resultErr error) {
	start := time.Now()
	var hashed int64
	defer func() {
		protection.VerifyRuns = append(protection.VerifyRuns, map[string]any{
			"elapsed_us": time.Since(start).Microseconds(), "metadata_hashed_bytes": hashed,
			"identity_verified_page_bytes": protection.Backing.Bytes,
			"identity_verified_page_files": len(protection.Backing.Pages),
			"page_content_hash_bytes":      int64(0), "passed": resultErr == nil,
		})
	}()
	if err := protection.verify(); err != nil {
		return nil, err
	}
	metadata := map[string]string{}
	for name, lease := range protection.Metadata {
		if lease.Saved.Size > 64<<20-hashed {
			return nil, fmt.Errorf("source inode certificate metadata exceeds its bound")
		}
		sum, err := descendantLeaseHash(lease.File)
		if err != nil {
			return nil, err
		}
		hashed += lease.Saved.Size
		if lease.SHA256 != "" && lease.SHA256 != sum {
			return nil, fmt.Errorf("source inode certificate metadata bytes changed")
		}
		lease.SHA256, metadata[name] = sum, sum
		if err := lease.verify(); err != nil {
			return nil, err
		}
	}
	if metadata["inventory.img"] == "" || metadata["pstree.img"] == "" {
		return nil, fmt.Errorf("source inode certificate lacks complete process metadata hashes")
	}
	if err := protection.verify(); err != nil {
		return nil, err
	}
	return metadata, nil
}

func (protection *descendantSourceProtection) syncCapture() (_ map[string]any, resultErr error) {
	start := time.Now()
	if err := protection.verify(); err != nil {
		return nil, err
	}
	var files int
	var synced int64
	for _, leases := range []map[string]*descendantFileLease{protection.Backing.Pages, protection.Metadata} {
		for _, lease := range leases {
			if err := errors.Join(lease.File.Sync(), lease.verify()); err != nil {
				return nil, err
			}
			files++
			synced += lease.Saved.Size
		}
	}
	if err := protection.verify(); err != nil {
		return nil, err
	}
	return map[string]any{
		"synced_files": files, "synced_logical_bytes": synced, "elapsed_us": time.Since(start).Microseconds(),
		"page_content_hash_bytes": int64(0), "original_read_leases_retained": true,
		"scope": "Flush only the exact original leased capture files before Btrfs target assembly; no content scan, copying or node-wide sync.",
	}, nil
}

func (protection *descendantSourceProtection) close(validate bool) (resultErr error) {
	if protection == nil || protection.closed {
		return nil
	}
	if validate {
		resultErr = protection.verify()
	}
	for _, lease := range protection.Metadata {
		resultErr = errors.Join(resultErr, lease.close())
	}
	resultErr = errors.Join(resultErr, protection.Backing.close(false))
	protection.closed = true
	return resultErr
}
