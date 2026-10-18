package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

type descendantDiagnosticRetirement struct {
	Path      string
	Directory *os.File
	Identity  os.FileInfo
	Pages     map[string]descendantLeaseIdentity
	Logical   int64
	Allocated int64
}

func descendantDiagnosticReferences(plans []*descendantDiagnosticRetirement) error {
	identities := map[[2]uint64]bool{}
	ownDirectories := map[string]bool{}
	for _, plan := range plans {
		identity := descendantLeaseFileIdentity(plan.Identity)
		identities[[2]uint64{identity.Device, identity.Inode}] = true
		ownDirectories[strconv.Itoa(int(plan.Directory.Fd()))] = true
		for _, page := range plan.Pages {
			identities[[2]uint64{page.Device, page.Inode}] = true
		}
	}
	matches := func(info os.FileInfo) bool {
		saved := descendantLeaseFileIdentity(info)
		return identities[[2]uint64{saved.Device, saved.Inode}]
	}
	processes, err := os.ReadDir("/proc")
	if err != nil {
		return err
	}
	seenNamespaces := map[string]bool{}
	unescape := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	for _, process := range processes {
		pid, err := strconv.Atoi(process.Name())
		if err != nil {
			continue
		}
		base := "/proc/" + process.Name()
		fds, err := os.ReadDir(base + "/fd")
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("cannot inspect diagnostic reader descriptors: pid=%d %w", pid, err)
		}
		for _, fd := range fds {
			if pid == os.Getpid() && ownDirectories[fd.Name()] {
				continue
			}
			if info, err := os.Stat(base + "/fd/" + fd.Name()); err == nil && matches(info) {
				return fmt.Errorf("diagnostic replica still has an open descriptor: pid=%d fd=%s", pid, fd.Name())
			}
		}
		for _, name := range []string{"cwd", "root"} {
			if info, err := os.Stat(base + "/" + name); err == nil && matches(info) {
				return fmt.Errorf("diagnostic replica is a live process %s: pid=%d", name, pid)
			}
		}
		if data, err := os.ReadFile(base + "/maps"); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				fields := strings.Fields(line)
				if len(fields) < 5 {
					continue
				}
				device := strings.Split(fields[3], ":")
				if len(device) != 2 {
					continue
				}
				major, firstErr := strconv.ParseUint(device[0], 16, 32)
				minor, secondErr := strconv.ParseUint(device[1], 16, 32)
				inode, thirdErr := strconv.ParseUint(fields[4], 10, 64)
				if firstErr == nil && secondErr == nil && thirdErr == nil &&
					identities[[2]uint64{unix.Mkdev(uint32(major), uint32(minor)), inode}] {
					return fmt.Errorf("diagnostic replica still has a memory mapping: pid=%d", pid)
				}
			}
		}
		namespace, err := os.Readlink(base + "/ns/mnt")
		if err != nil || seenNamespaces[namespace] {
			continue
		}
		seenNamespaces[namespace] = true
		data, err := os.ReadFile(base + "/mountinfo")
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 6 {
				continue
			}
			// Btrfs mountinfo devices need not equal a subvolume inode's
			// st_dev. Check the actual namespace mount target instead.
			if info, err := os.Stat(base + "/root" + unescape.Replace(fields[4])); err == nil && matches(info) {
				return fmt.Errorf("diagnostic replica still has a mount reference: pid=%d", pid)
			}
		}
	}
	return nil
}

// These are diagnostic replicas, never a restored sandbox's images or any
// private target graph node. Keep the compact metadata after verifying the
// independent inherited target and retiring the complete COW observer.
func descendantRetireInheritedDiagnostics(source, target descendantLayout, capture *descendantInheritedCapture,
	evidence string, memory, graphVerified map[string]any) (_ map[string]any, resultErr error) {
	ownedSource, sourceErr := descendantOwnedLayout(source.ID)
	ownedTarget, targetErr := descendantOwnedLayout(target.ID)
	fast := capture != nil && capture.Proof["ordinary_fast_collector"] == true
	validReference := capture != nil && capture.Proof["same_capture_original_writer"] == true &&
		memory["all_original_writer_bytes_match"] == true && !fast
	if fast {
		validReference = capture.OriginalImages == "" && capture.Proof["same_capture_original_writer"] == false &&
			capture.Proof["single_source_capture"] == true && capture.Proof["native_stream_original_writer"] != true &&
			memory["all_captured_graph_bytes_match"] == true && memory["reference_is_captured_graph"] == true &&
			memory["all_original_writer_bytes_match"] == nil
	}
	if sourceErr != nil || targetErr != nil || source != ownedSource || target != ownedTarget ||
		source.ID == "source-sysbox" || source.ID == target.ID || !descendantID.MatchString(target.ID) ||
		!validReference || graphVerified["all_graph_payload_hashes_match"] != true {
		return nil, fmt.Errorf("diagnostic retirement requires the verified independent inherited target")
	}
	intentFile, err := os.OpenFile(filepath.Join(evidence, "inherited-capture-intent.json"),
		os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(intentFile, 16385))
	info, statErr := intentFile.Stat()
	closeErr := intentFile.Close()
	var intent struct {
		Runtime  descendantLayout `json:"runtime"`
		Images   string           `json:"images"`
		Original string           `json:"original_writer_images"`
		Parent   string           `json:"parent_images"`
		Stream   bool             `json:"native_stream_original_writer"`
		Single   bool             `json:"single_source_capture"`
		Fast     bool             `json:"ordinary_fast_collector"`
	}
	if readErr != nil || statErr != nil || closeErr != nil || !info.Mode().IsRegular() ||
		len(data) > 16384 || json.Unmarshal(data, &intent) != nil || intent.Runtime != source ||
		intent.Images != capture.Images || intent.Original != capture.OriginalImages ||
		intent.Parent != capture.Proof["parent_images"] ||
		intent.Stream != (capture.Proof["native_stream_original_writer"] == true) ||
		intent.Single != (capture.Proof["single_source_capture"] == true) || intent.Fast != fast ||
		(intent.Single && (intent.Stream == intent.Fast)) {
		return nil, fmt.Errorf("diagnostic retirement lost its persisted capture identity")
	}
	paths := []string{capture.OriginalImages, capture.Images, target.Root + "/control-images"}
	if intent.Single {
		paths = paths[:2]
	}
	if fast {
		paths = []string{capture.Images}
	}
	if len(paths) > 1 && paths[0] == paths[1] {
		return nil, fmt.Errorf("diagnostic replicas share a generation identity")
	}
	var plans []*descendantDiagnosticRetirement
	defer func() {
		for _, plan := range plans {
			resultErr = errors.Join(resultErr, plan.Directory.Close())
		}
	}()
	for index, path := range paths {
		streamOriginal := index == 0 && intent.Stream && path == capture.Images+"/original-writer" &&
			filepath.Dir(capture.Images) == source.Root && descendantCaptureGeneration.MatchString(filepath.Base(capture.Images))
		if index < 2 && !streamOriginal && (filepath.Dir(path) != source.Root ||
			!descendantCaptureGeneration.MatchString(filepath.Base(path)) || path == intent.Parent) {
			return nil, fmt.Errorf("diagnostic retirement escaped its fresh source capture")
		}
		for _, daemon := range criuLazyDaemons {
			if filepath.Dir(daemon.logPath) == path {
				return nil, fmt.Errorf("diagnostic retirement refuses recorded restore images")
			}
		}
		for _, snapshot := range criuRAMSnapshots {
			if snapshot.Images == path || snapshot.CopyImages == path {
				return nil, fmt.Errorf("diagnostic retirement refuses retained RAM snapshot identities")
			}
		}
		refs := descendantPauseReferences(path)
		var foreignDescriptors []map[string]any
		for _, reference := range refs["open_descriptors"].([]map[string]any) {
			owned := false
			for _, prior := range plans {
				if reference["pid"] == os.Getpid() &&
					reference["fd"] == strconv.Itoa(int(prior.Directory.Fd())) && reference["target"] == prior.Path {
					current, err := prior.Directory.Stat()
					owned = err == nil && os.SameFile(current, prior.Identity)
				}
			}
			if !owned {
				foreignDescriptors = append(foreignDescriptors, reference)
			}
		}
		refs["open_descriptors"] = foreignDescriptors
		for _, key := range []string{"open_descriptors", "mounted_namespaces", "mapped_files"} {
			if len(refs[key].([]map[string]any)) != 0 {
				return nil, fmt.Errorf("diagnostic replica still has %s: %s", key, path)
			}
		}
		fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			return nil, err
		}
		plan := &descendantDiagnosticRetirement{Path: path, Directory: os.NewFile(uintptr(fd), path),
			Pages: map[string]descendantLeaseIdentity{}}
		plans = append(plans, plan)
		plan.Identity, err = plan.Directory.Stat()
		if err != nil {
			return nil, err
		}
		identity := descendantLeaseFileIdentity(plan.Identity)
		var fs unix.Statfs_t
		if unix.Fstatfs(fd, &fs) != nil || fs.Type != unix.BTRFS_SUPER_MAGIC ||
			identity.UID != 165536 || identity.GID != 165536 || plan.Identity.Mode().Perm() != 0700 {
			return nil, fmt.Errorf("diagnostic replica lost its private mapped-root Btrfs directory")
		}
		entries, err := plan.Directory.ReadDir(-1)
		if err != nil || len(entries) > 8192 {
			return nil, fmt.Errorf("diagnostic entry population is outside its bound: %v", err)
		}
		for _, entry := range entries {
			name := entry.Name()
			info, err := os.Lstat(filepath.Join(path, name))
			if err != nil {
				return nil, err
			}
			if name == "parent" && index < 2 {
				if streamOriginal {
					return nil, fmt.Errorf("complete stream original writer must not retain a parent link")
				}
				link, linkErr := os.Readlink(filepath.Join(path, name))
				expected, relativeErr := filepath.Rel(path, intent.Parent)
				if linkErr != nil || relativeErr != nil || link != expected {
					return nil, fmt.Errorf("diagnostic parent does not match its capture intent")
				}
				continue
			}
			if index == 1 && intent.Stream && name == "original-writer" {
				if !info.IsDir() || filepath.Join(path, name) != capture.OriginalImages ||
					len(plans) != 2 || !os.SameFile(info, plans[0].Identity) {
					return nil, fmt.Errorf("stream original writer directory changed before diagnostic retirement")
				}
				continue
			}
			if !info.Mode().IsRegular() {
				return nil, fmt.Errorf("diagnostic replica contains an unexpected nonregular entry")
			}
			if !criuRAMPageName.MatchString(name) {
				continue
			}
			saved := descendantLeaseFileIdentity(info)
			if saved.UID != identity.UID || saved.GID != identity.GID || saved.Mode&07777 != 0600 ||
				saved.Links != 1 || saved.Size < 0 || uint64(saved.Size) > criuRAMSnapshotLimit-uint64(plan.Logical) {
				return nil, fmt.Errorf("diagnostic page payload lost its bounded private ownership")
			}
			plan.Pages[name] = saved
			plan.Logical += saved.Size
			plan.Allocated += info.Sys().(*syscall.Stat_t).Blocks * 512
		}
		if len(plan.Pages) == 0 {
			return nil, fmt.Errorf("diagnostic replica has no exclusive page payload")
		}
	}
	anchor, err := os.Open(evidence)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, anchor.Close()) }()
	if err := descendantDiagnosticReferences(plans); err != nil {
		return nil, err
	}
	var records []map[string]any
	var logical, allocated int64
	for _, plan := range plans {
		records = append(records, map[string]any{"path": plan.Path,
			"directory": descendantLeaseFileIdentity(plan.Identity), "page_files": plan.Pages,
			"logical_bytes": plan.Logical, "accounted_inode_allocated_bytes": plan.Allocated})
		logical += plan.Logical
		allocated += plan.Allocated
	}
	if err := descendantGuardWrite(anchor, "diagnostic-retirement-intent.json", records); err != nil {
		return nil, err
	}
	retiredFiles := 0
	for _, plan := range plans {
		current, err := os.Lstat(plan.Path)
		if err != nil || !os.SameFile(current, plan.Identity) {
			return nil, fmt.Errorf("changed diagnostic directory preserved: %v", err)
		}
		for name, expected := range plan.Pages {
			current, err := os.Lstat(filepath.Join(plan.Path, name))
			if err != nil || descendantLeaseFileIdentity(current) != expected {
				return nil, fmt.Errorf("changed diagnostic page preserved: %s %v", name, err)
			}
			if err := unix.Unlinkat(int(plan.Directory.Fd()), name, 0); err != nil {
				return nil, err
			}
			retiredFiles++
		}
		if err := plan.Directory.Sync(); err != nil {
			return nil, err
		}
		for name := range plan.Pages {
			if _, err := os.Lstat(filepath.Join(plan.Path, name)); !os.IsNotExist(err) {
				return nil, fmt.Errorf("retired diagnostic page still exists: %s %v", name, err)
			}
		}
	}
	proof := map[string]any{"retired": true, "replicas": records, "retired_page_files": retiredFiles,
		"logical_bytes": logical, "accounted_inode_allocated_bytes": allocated,
		"complete_metadata_retained": true, "no_open_descriptor_mount_or_mapping_references": true,
		"scope": "Only the independently verified fork's explicitly declared diagnostic page replicas are unlinked. " +
			"Actual target head/parents, original restore images, user files and stopped ancestors remain. " +
			"Inode accounting removed is not a claim of physically freed shared Btrfs extents."}
	if err := descendantGuardWrite(anchor, "diagnostic-retirement-complete.json", proof); err != nil {
		return nil, err
	}
	return proof, nil
}
