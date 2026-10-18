package main

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// The compiler acquires each output page lease before its only initial hash.
// Ownership transfers to the runtime without closing or reacquiring any lease.
func descendantNewRangeProtection(images string, owner descendantLeaseIdentity) (*descendantBackingLeases, error) {
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
	var fs unix.Statfs_t
	if err := unix.Fstatfs(fd, &fs); err != nil || fs.Type != unix.BTRFS_SUPER_MAGIC ||
		!info.IsDir() || identity.Mode&07777 != 0700 ||
		identity.UID != owner.UID || identity.GID != owner.GID {
		directory.Close()
		return nil, fmt.Errorf("retained output leases require their private owned Btrfs directory: %v", err)
	}
	return &descendantBackingLeases{Path: images, Directory: directory, Identity: identity,
		Pages: map[string]*descendantFileLease{}}, nil
}

func descendantProtectedTargetManifest(protection *descendantBackingLeases, graph map[string]any,
	target descendantLayout) (map[string]string, error) {
	if protection == nil || graph["head"] != target.Root+"/images" || protection.Path != graph["head"] ||
		graph["retained_output_page_read_leases"] != true || graph["complete_range_snapshot"] != true {
		return nil, fmt.Errorf("target manifest lacks continuous complete output page protection")
	}
	manifest, err := descendantSnapshotManifestProtected(protection.Path, protection)
	if err != nil {
		return nil, err
	}
	hashes, ok := graph["hashes"].(map[string]string)
	if !ok || len(hashes) != len(manifest) {
		return nil, fmt.Errorf("protected target payload population differs from the compiled certificate")
	}
	for name, checksum := range manifest {
		if hashes[protection.Path+"/"+name] != checksum {
			return nil, fmt.Errorf("protected target metadata or page certificate changed: %s", name)
		}
	}
	return manifest, nil
}

func descendantProtectedTargetMemory(protection *descendantBackingLeases, graph map[string]any,
	capture *descendantInheritedCapture, target descendantLayout) (map[string]any, error) {
	if protection == nil || capture == nil || capture.Proof["ordinary_fast_collector"] != true ||
		capture.OriginalImages != "" || graph["captured_reference_images"] != capture.Images ||
		graph["source_head"] != capture.Images || graph["head"] != target.Root+"/images" ||
		protection.Path != graph["head"] || graph["retained_output_page_read_leases"] != true ||
		graph["complete_range_snapshot"] != true || graph["nodes"] != 1 {
		return nil, fmt.Errorf("captured graph comparison is not bound to the continuously leased complete target")
	}
	compared, ok := graph["compiled_captured_graph_memory"].(map[string]any)
	if !ok || compared["all_captured_graph_bytes_match"] != true ||
		compared["reference_is_captured_graph"] != true || compared["all_original_writer_bytes_match"] != nil ||
		compared["inherited_bytes"] != int64(0) || compared["compared_bytes"] != graph["logical_memory_bytes"] ||
		protection.Bytes != graph["logical_memory_bytes"] {
		return nil, fmt.Errorf("leased complete target lost its actual captured-graph byte comparison")
	}
	if err := protection.verify(); err != nil {
		return nil, err
	}
	return compared, nil
}
