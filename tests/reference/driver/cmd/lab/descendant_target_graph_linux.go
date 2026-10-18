package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

func descendantCloneInheritedTarget(capture *descendantInheritedCapture, target descendantLayout) (
	_ map[string]any, _ map[string]string, resultErr error) {
	return descendantCloneInheritedTargetInputPolicy(capture, target, descendantDirectRangeEnabled(),
		descendantPageInputLeaseOnlyEnabled())
}

func descendantCloneInheritedTargetPolicy(capture *descendantInheritedCapture, target descendantLayout, direct bool) (
	_ map[string]any, _ map[string]string, resultErr error) {
	return descendantCloneInheritedTargetInputPolicy(capture, target, direct, false)
}

func descendantCloneInheritedTargetInputPolicy(capture *descendantInheritedCapture, target descendantLayout,
	direct, leaseOnlyPages bool) (_ map[string]any, _ map[string]string, resultErr error) {
	return descendantCloneInheritedTargetProtectedPolicy(capture, target, direct, leaseOnlyPages, nil)
}

func descendantCloneInheritedTargetProtectedPolicy(capture *descendantInheritedCapture, target descendantLayout,
	direct, leaseOnlyPages bool, protectionOut **descendantBackingLeases) (_ map[string]any, _ map[string]string, resultErr error) {
	if leaseOnlyPages && !direct {
		return nil, nil, fmt.Errorf("lease-only page inputs require direct original-graph targets")
	}
	owned, err := descendantOwnedLayout(target.ID)
	fast := capture != nil && capture.Proof["ordinary_fast_collector"] == true
	validReference := capture != nil && capture.Proof["same_capture_original_writer"] == true && !fast
	if fast {
		validReference = capture.OriginalImages == "" && capture.Proof["same_capture_original_writer"] == false &&
			capture.Proof["single_source_capture"] == true &&
			capture.Proof["single_capture_transaction_completed"] == true
	}
	if err != nil || owned != target || !descendantID.MatchString(target.ID) ||
		!validReference {
		return nil, nil, fmt.Errorf("inherited target requires a canonical fresh descendant and an explicit captured reference")
	}
	if protectionOut != nil && (!fast || !direct || !leaseOnlyPages || *protectionOut != nil) {
		return nil, nil, fmt.Errorf("retained target leases require one fresh ordinary direct leased-input target")
	}
	if err := os.Mkdir(target.Root, 0700); err != nil {
		return nil, nil, err
	}
	created, err := os.Lstat(target.Root)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if resultErr != nil {
			if current, err := os.Lstat(target.Root); err == nil && os.SameFile(current, created) {
				resultErr = errors.Join(resultErr, os.RemoveAll(target.Root))
			}
		}
	}()
	inputRoot := target.Root + "/range-input"
	head := target.Root + "/images"
	var input, graph map[string]any
	var inputIdentity os.FileInfo
	var protection *descendantBackingLeases
	defer func() {
		if protection != nil {
			if resultErr != nil {
				resultErr = errors.Join(resultErr, protection.close(false))
			} else {
				*protectionOut = protection
			}
		}
	}()
	if direct {
		if protectionOut != nil {
			if _, err := descendantLeaseAdmission(target); err != nil {
				return nil, nil, err
			}
			graph, err = descendantCompileRangeSnapshotProtectedPolicy(capture.Images, head, true, true, &protection)
		} else if leaseOnlyPages {
			graph, err = descendantCompileRangeSnapshotLeasedInputs(capture.Images, head)
		} else {
			graph, err = descendantCompileDirectRangeSnapshot(capture.Images, head)
		}
	} else {
		input, err = descendantCloneImageGraphPolicy(capture.Images, inputRoot, true)
		if err != nil {
			return nil, nil, err
		}
		inputIdentity, err = os.Lstat(inputRoot)
		if err != nil {
			return nil, nil, err
		}
		graph, err = descendantCompileRangeSnapshot(input["head"].(string), head)
	}
	if err != nil {
		return nil, nil, err
	}
	compare, reference, matched := descendantCompareImageMemory, capture.OriginalImages, "all_original_writer_bytes_match"
	if fast {
		compare, reference, matched = descendantCompareCapturedMemory, capture.Images, "all_captured_graph_bytes_match"
	}
	compared, err := compare(head, reference)
	if err != nil || compared[matched] != true || compared["inherited_bytes"] != int64(0) {
		return nil, nil, fmt.Errorf("complete range target differs from its declared memory reference: %v", err)
	}
	// This private input graph has never been exposed to a restore/pager.
	// The compiler closed every input FD/lease, and all output bytes were
	// compared before retiring its exact newly created directory.
	if !direct {
		currentInput, err := os.Lstat(inputRoot)
		if err != nil || !os.SameFile(currentInput, inputIdentity) {
			return nil, nil, fmt.Errorf("temporary range input graph identity changed: %v", err)
		}
		if err := os.RemoveAll(inputRoot); err != nil {
			return nil, nil, err
		}
	}
	// The runtime bundle keeps its existing root ownership. Saved images
	// and all private graph nodes retain their mapped-root ownership.
	if err := errors.Join(os.Chown(target.Root, 0, 0), os.Chmod(target.Root, 0755)); err != nil {
		return nil, nil, err
	}
	expectedHead := map[string]string{}
	for path, checksum := range graph["hashes"].(map[string]string) {
		name := filepath.Base(path)
		if filepath.Dir(path) == head &&
			(strings.HasSuffix(name, ".img") || name == "runtime-config.json" || name == "descriptors.json") {
			expectedHead[name] = checksum
		}
	}
	if expectedHead["inventory.img"] == "" || expectedHead["pstree.img"] == "" {
		return nil, nil, fmt.Errorf("inherited target lacks certified complete process metadata")
	}
	graph["temporary_input_graph_created"] = !direct
	if !direct {
		graph["input_graph"] = input
		graph["input_graph_retired_after_complete_byte_oracle"] = true
	}
	if fast {
		graph["compiled_captured_graph_memory"] = compared
		graph["captured_reference_images"] = capture.Images
	} else {
		graph["compiled_original_writer_memory"] = compared
		graph["same_capture_original_writer_images"] = capture.OriginalImages
	}
	graph["payload_files"] = len(graph["hashes"].(map[string]string))
	graph["kernel_cloned_bytes"] = graph["kernel_range_clone_bytes"].(int64) + graph["kernel_cloned_metadata_bytes"].(int64)
	if direct {
		graph["verified_ram_parent_copy_bytes"] = graph["verified_ram_range_copy_bytes"]
		graph["verified_ram_parent_copy_files"] = graph["verified_ram_range_copy_files"]
	} else {
		graph["kernel_cloned_bytes"] = graph["kernel_cloned_bytes"].(int64) + input["kernel_cloned_bytes"].(int64)
		graph["verified_ram_parent_copy_bytes"] = input["verified_ram_parent_copy_bytes"]
		graph["verified_ram_parent_copy_files"] = input["verified_ram_parent_copy_files"]
	}
	graph["source_head"] = capture.Images
	if protection != nil {
		if err := protection.verify(); err != nil {
			return nil, nil, err
		}
		graph["retained_target_page_files"] = len(protection.Pages)
		graph["retained_target_page_bytes"] = protection.Bytes
	}
	return graph, expectedHead, nil
}

func descendantVerifyInheritedTargetGraph(graph map[string]any, target descendantLayout) (_ map[string]any, resultErr error) {
	start := time.Now()
	hashes, ok := graph["hashes"].(map[string]string)
	if !ok || len(hashes) == 0 || graph["head"] != target.Root+"/images" {
		return nil, fmt.Errorf("inherited target lost its original graph certificate")
	}
	var bytes int64
	links, ok := graph["parent_links"].(map[string]string)
	complete := graph["complete_range_snapshot"] == true
	if !ok || (!complete && len(links) == 0) || (complete && (len(links) != 0 || graph["nodes"] != 1)) {
		return nil, fmt.Errorf("inherited target lost its private parent relationships")
	}
	if complete {
		entries, err := os.ReadDir(target.Root + "/images")
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.Name() == "parent" || criuInheritAliasName.MatchString(entry.Name()) {
				return nil, fmt.Errorf("complete range target regained a parent dependency")
			}
		}
		if _, err := os.Lstat(target.Root + "/range-input"); !os.IsNotExist(err) {
			return nil, fmt.Errorf("temporary range input graph was not retired: %v", err)
		}
	}
	for path, expected := range links {
		if filepath.Clean(path) != path || !strings.HasPrefix(path, target.Root+"/") || filepath.IsAbs(expected) {
			return nil, fmt.Errorf("inherited parent relationship escaped its runtime")
		}
		actual, err := os.Readlink(path)
		resolved, resolveErr := filepath.EvalSymlinks(filepath.Join(filepath.Dir(path), actual))
		if err != nil || resolveErr != nil || actual != expected || !descendantGraphWithin(target.Root, resolved) {
			return nil, fmt.Errorf("private inherited parent relationship changed: %v %v", err, resolveErr)
		}
	}
	for path, expected := range hashes {
		if filepath.Clean(path) != path || !strings.HasPrefix(path, target.Root+"/") {
			return nil, fmt.Errorf("inherited target certificate escaped its runtime root")
		}
		file, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
		if err != nil {
			return nil, err
		}
		info, statErr := file.Stat()
		if statErr != nil || !info.Mode().IsRegular() {
			file.Close()
			return nil, fmt.Errorf("inherited target graph entry is not a regular file: %v", statErr)
		}
		actual, hashErr := descendantLeaseHash(file)
		err = errors.Join(hashErr, file.Sync(),
			unix.Fadvise(int(file.Fd()), 0, 0, unix.FADV_DONTNEED), file.Close())
		if err != nil || actual != expected {
			return nil, fmt.Errorf("independent inherited graph changed: %s %v", path, err)
		}
		bytes += info.Size()
	}
	return map[string]any{"files": len(hashes), "hashed_bytes": bytes, "elapsed_us": time.Since(start).Microseconds(),
		"all_graph_payload_hashes_match": true,
		"private_parent_links_verified":  len(links),
		"complete_range_snapshot":        complete,
		"scope":                          "Every private target head/parent image and PID alias retains its certified original bytes after real workloads."}, nil
}
