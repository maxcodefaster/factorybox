//go:build linux

package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// Keep the original owned mount ID even when a later epoch adds a readonly
// directory bind at the same path. Every visible entry must name the same
// globally readonly tmpfs device; local-only RO and foreign overlays fail.
func committedMountWithAnchor(input, path string, expected uint64) (uint64, error) {
	ids := map[uint64]bool{}
	device := ""
	for _, line := range strings.Split(input, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 10 || fields[4] != path {
			continue
		}
		separator := -1
		for index, field := range fields {
			if field == "-" {
				separator = index
				break
			}
		}
		if separator < 6 || separator+3 >= len(fields) || fields[separator+1] != "tmpfs" ||
			(device != "" && device != fields[2]) {
			return 0, fmt.Errorf("foreign or malformed committed mount alias")
		}
		device = fields[2]
		readonly := false
		for _, option := range strings.Split(fields[separator+3], ",") {
			readonly = readonly || option == "ro"
		}
		if !readonly {
			return 0, fmt.Errorf("committed alias superblock is writable")
		}
		id, err := committedParseMountID(fields[0])
		if err != nil || ids[id] {
			return 0, fmt.Errorf("invalid or duplicate committed mount identity: %v", err)
		}
		ids[id] = true
	}
	if expected != 0 {
		if !ids[expected] {
			return 0, fmt.Errorf("original committed mount is absent")
		}
		return expected, nil
	}
	if len(ids) != 1 {
		return 0, fmt.Errorf("new committed mount is absent or ambiguous")
	}
	for id := range ids {
		return id, nil
	}
	panic("unreachable")
}

func committedQuiescent() error {
	if len(criuLazyDaemons) != 0 {
		return fmt.Errorf("snapshot readers must stop before retirement")
	}
	for _, path := range []string{criuOwnedSourceCgroup, "/sys/fs/cgroup/criu-probe-sysbox-copy"} {
		data, err := os.ReadFile(path + "/cgroup.events")
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		counters, err := criuRAMCounters(string(data))
		populated, exists := counters["populated"]
		if err != nil || !exists || populated != 0 {
			return fmt.Errorf("owned workload still exists during snapshot retirement: %s: %v", path, err)
		}
	}
	return nil
}

// Called only after the original exporter has synced and read back both
// complete manifests. Invalidate and close every certificate before permitting
// the pre-existing retirement-only unlink step. No published object becomes RW.
func criuRetireCommittedSnapshot(images string, manifest map[string]criuRAMFile, unlinkFiles bool) (resultErr error) {
	owner := committedSnapshots[images]
	if owner == nil {
		return nil
	}
	start := time.Now()
	var invalidated, writable bool
	defer func() {
		record("sysbox-committed-snapshot-retired", start, asJSON(map[string]any{
			"images": images, "certificate_invalidated": invalidated,
			"retirement_writable": writable, "verified_export_files": len(manifest),
			"scope": "after complete synced export/readback; owned workloads and pagers stopped; page digests compared to original anchors; certificate closed before any retirement-only remount/unlink",
		}), resultErr)
	}()
	if len(manifest) == 0 {
		return fmt.Errorf("retirement requires a verified complete export")
	}
	if err := committedQuiescent(); err != nil {
		return err
	}
	if id, err := committedCurrentMount(images, owner.Directory, owner.MountID); err != nil || id != owner.MountID {
		return fmt.Errorf("retiring snapshot identity/policy changed: %v", err)
	}
	for name, page := range owner.Pages {
		exported, ok := manifest[name]
		if !ok || exported.Name != name || exported.SHA256 != page.Digest ||
			exported.Bytes != page.Identity.Size || exported.UID != page.Identity.UID ||
			exported.GID != page.Identity.GID || exported.Mode != page.Identity.Mode&07777 {
			return fmt.Errorf("verified export differs from original committed page: %s", name)
		}
		info, err := page.File.Stat()
		if err != nil || committedIdentity(info) != page.Identity {
			return fmt.Errorf("retiring original page anchor changed: %s: %v", name, err)
		}
	}
	if unlinkFiles && (images != "/experiment/sysbox/images-again" ||
		os.Getenv("RUNTIME_FORK_EPOCH_REBASE") != "1" || os.Getenv("RUNTIME_FORK_EPOCH_RAM_PARENT") != "1") {
		return fmt.Errorf("writable retirement is outside the exact backed epoch")
	}
	identity := owner.Identity
	if err := criuCloseCommittedSnapshot(images); err != nil {
		return err
	}
	invalidated = true
	if !unlinkFiles {
		return nil
	}
	info, err := os.Lstat(images)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("retirement target disappeared: %v", err)
	}
	current := committedIdentity(info)
	if current.Device != identity.Device || current.Inode != identity.Inode {
		return fmt.Errorf("retirement target inode changed")
	}
	if err := unix.Mount("", images, "", unix.MS_REMOUNT|unix.MS_NOSUID|unix.MS_NODEV|unix.MS_NOEXEC, ""); err != nil {
		return err
	}
	writable = true
	return nil
}

func criuVerifyBackedCommittedParent(images string, expected map[string]string) (resultErr error) {
	start := time.Now()
	verification, err := startEpochParentVerification(images, expected)
	if err != nil {
		return err
	}
	result := verification.join(false)
	if result.Err == nil && (result.ReadBytes != result.Bytes || result.KernelBytes != 0 || result.KernelFiles != 0) {
		result.Err = fmt.Errorf("backed parent incorrectly reused an old RAM certificate")
	}
	record("sysbox-committed-backed-parent-full-verification", start, asJSON(map[string]any{
		"images": images, "files": result.Files, "logical_bytes": result.Bytes,
		"read_bytes": result.ReadBytes, "kernel_certified_bytes": result.KernelBytes,
		"scope": "after actual source suspend/RAM release/resume; old RAM anchors gone; backed P1 fully read and hashed before second child creation",
	}), result.Err)
	return result.Err
}
