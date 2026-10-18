//go:build linux

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

type criuRAMMemory struct {
	Cgroup  string            `json:"cgroup"`
	Current uint64            `json:"current_bytes"`
	Limit   string            `json:"limit_bytes"`
	Swap    uint64            `json:"swap_bytes"`
	Stat    map[string]uint64 `json:"stat"`
	Events  map[string]uint64 `json:"events"`
}

type criuRAMSnapshot struct {
	Images     string        `json:"images"`
	Limit      uint64        `json:"bound_bytes"`
	BackingFS  int64         `json:"backing_filesystem_magic"`
	Before     criuRAMMemory `json:"before_capture"`
	Bytes      uint64        `json:"file_bytes"`
	Allocated  uint64        `json:"allocated_bytes"`
	Mounted    bool          `json:"ram_mount_active"`
	CopyImages string        `json:"copy_images,omitempty"`
	Bindings   []string      `json:"readonly_payload_bindings,omitempty"`
}

type criuRAMFile struct {
	Name   string `json:"name"`
	Link   string `json:"link,omitempty"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
	UID    uint32 `json:"uid"`
	GID    uint32 `json:"gid"`
	Mode   uint32 `json:"mode"`
}

var criuRAMSnapshots []*criuRAMSnapshot

func criuRAMMemoryRead() (criuRAMMemory, error) {
	var result criuRAMMemory
	data, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return result, err
	}
	result.Cgroup, err = criuRAMCgroupPath(string(data))
	if err != nil {
		return result, err
	}
	for name, target := range map[string]*uint64{"memory.current": &result.Current, "memory.swap.current": &result.Swap} {
		data, err = os.ReadFile(filepath.Join(result.Cgroup, name))
		if err != nil {
			return result, err
		}
		*target, err = strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
		if err != nil {
			return result, err
		}
	}
	data, err = os.ReadFile(filepath.Join(result.Cgroup, "memory.max"))
	if err != nil {
		return result, err
	}
	result.Limit = strings.TrimSpace(string(data))
	for name, target := range map[string]*map[string]uint64{"memory.stat": &result.Stat, "memory.events": &result.Events} {
		data, err = os.ReadFile(filepath.Join(result.Cgroup, name))
		if err != nil {
			return result, err
		}
		*target, err = criuRAMCounters(string(data))
		if err != nil {
			return result, err
		}
	}
	if _, exists := result.Stat["shmem"]; !exists {
		return result, fmt.Errorf("missing shmem accounting")
	}
	return result, nil
}

func criuRAMSnapshotFind(images string) *criuRAMSnapshot {
	for _, snapshot := range criuRAMSnapshots {
		if snapshot.Images == images && snapshot.Mounted {
			return snapshot
		}
	}
	return nil
}

func criuSysboxPrepareRAMCapture(images string) (resultErr error) {
	if os.Getenv("CRIU_SYSBOX_RAM_CAPTURE") != "1" {
		return nil
	}
	start := time.Now()
	defer func() { record("sysbox-ram-capture-prepared", start, images, resultErr) }()
	if err := criuRAMSnapshotPath(criuRoot, images); err != nil {
		return err
	}
	if filepath.Base(filepath.Dir(images)) != "sysbox" {
		return fmt.Errorf("only source captures allocate RAM")
	}
	if criuRAMSnapshotFind(images) != nil {
		return fmt.Errorf("RAM capture is already active")
	}
	if err := os.MkdirAll(images, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(images)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("invalid capture directory: %v", err)
	}
	entries, err := os.ReadDir(images)
	if err != nil || len(entries) != 0 {
		return fmt.Errorf("capture directory must be empty: %v", err)
	}
	snapshot := &criuRAMSnapshot{Images: images, Limit: criuRAMSnapshotLimit}
	var backing unix.Statfs_t
	if err := unix.Statfs(images, &backing); err != nil {
		return err
	}
	if backing.Type == unix.TMPFS_MAGIC {
		return fmt.Errorf("RAM capture needs separate file-backed storage")
	}
	snapshot.BackingFS = backing.Type
	snapshot.Before, err = criuRAMMemoryRead()
	if err != nil {
		return err
	}
	// This intent precedes the owned mount; a failed run retains its exact path.
	intent := filepath.Join(criuRoot, "ram-capture-"+filepath.Base(images)+"-intent.json")
	data, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	intentFile, err := os.OpenFile(intent, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = intentFile.Write(append(data, '\n'))
	if err == nil {
		err = intentFile.Sync()
	}
	intentFile.Close()
	if err != nil {
		return err
	}
	if err := unix.Mount("runtime-fork-ram-capture", images, "tmpfs", unix.MS_NOSUID|unix.MS_NODEV|unix.MS_NOEXEC,
		fmt.Sprintf("size=%d,mode=0700", snapshot.Limit)); err != nil {
		return err
	}
	snapshot.Mounted = true
	criuRAMSnapshots = append(criuRAMSnapshots, snapshot)
	return nil
}

func criuRAMImageEntries(images string, allowLazySocket bool) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(images)
	if err != nil {
		return nil, err
	}
	output := make([]os.DirEntry, 0, len(entries))
	for _, entry := range entries {
		info, err := os.Lstat(filepath.Join(images, entry.Name()))
		if err != nil {
			return nil, err
		}
		if allowLazySocket && entry.Name() == "lazy-pages.socket" && info.Mode()&os.ModeSocket != 0 {
			continue
		}
		if entry.Name() == "parent" && info.Mode()&os.ModeSymlink != 0 {
			if err := criuSysboxParentLink(images); err != nil {
				return nil, err
			}
			output = append(output, entry)
			continue
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("non-regular snapshot entry: %q", entry.Name())
		}
		output = append(output, entry)
	}
	return output, nil
}

func criuSysboxRAMCaptureProof(images string) (resultErr error) {
	snapshot := criuRAMSnapshotFind(images)
	if snapshot == nil {
		return nil
	}
	start := time.Now()
	output := map[string]any{"snapshot": snapshot, "scope": "retained snapshot RAM; source termination alone is not RAM release"}
	defer func() { record("sysbox-ram-capture-accounting", start, asJSON(output), resultErr) }()
	var fs unix.Statfs_t
	if err := unix.Statfs(images, &fs); err != nil {
		return err
	}
	if fs.Type != unix.TMPFS_MAGIC || uint64(fs.Blocks)*uint64(fs.Bsize) > snapshot.Limit {
		return fmt.Errorf("unexpected RAM capture filesystem/bound")
	}
	snapshot.Allocated = uint64(fs.Blocks-fs.Bfree) * uint64(fs.Bsize)
	entries, err := criuRAMImageEntries(images, false)
	if err != nil {
		return err
	}
	var pageFiles int
	snapshot.Bytes = 0
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return err
		}
		snapshot.Bytes += uint64(info.Size())
		if criuRAMPageName.MatchString(entry.Name()) {
			pageFiles++
		}
	}
	if pageFiles == 0 || snapshot.Bytes == 0 || snapshot.Bytes > snapshot.Limit {
		return fmt.Errorf("missing or oversized complete page payload")
	}
	memory, err := criuRAMMemoryRead()
	output["after_capture"], output["page_files"] = memory, pageFiles
	if err == nil {
		err = criuCommitSnapshot(images)
	}
	return err
}

func criuRAMHashFile(path string) (digest string, resultErr error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	logical, release, err := criuLogicalImage(file)
	if err != nil {
		return "", err
	}
	defer func() { resultErr = errors.Join(resultErr, release()) }()
	hash := sha256.New()
	if descendantCoalescedHashesEnabled() {
		_, err = descendantCopySnapshotHash(hash, logical)
	} else {
		_, err = io.Copy(hash, logical)
	}
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// Every destination is private and created exclusively. Sync and read back
// actual bytes before allowing the original RAM-backed snapshot to disappear.
func criuRAMExportFile(source, destination, cloneFrom string, expectedHash string, durable bool, buffer []byte) (result criuRAMFile, resultErr error) {
	sourceFile, err := os.OpenFile(source, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return result, err
	}
	defer sourceFile.Close()
	logical, release, err := criuLogicalImage(sourceFile)
	if err != nil {
		return result, err
	}
	defer func() { resultErr = errors.Join(resultErr, release()) }()
	info, err := sourceFile.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return result, fmt.Errorf("invalid snapshot source: %v", err)
	}
	stat := info.Sys().(*syscall.Stat_t)
	result = criuRAMFile{Name: filepath.Base(source), Bytes: info.Size(), UID: stat.Uid, GID: stat.Gid, Mode: stat.Mode & 07777}
	target, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return result, err
	}
	defer target.Close()
	cloned := false
	if cloneFrom != "" {
		clone, err := os.Open(cloneFrom)
		if err != nil {
			return result, err
		}
		err = unix.IoctlFileClone(int(target.Fd()), int(clone.Fd()))
		clone.Close()
		if err != nil && !errors.Is(err, unix.EOPNOTSUPP) && !errors.Is(err, unix.EXDEV) && !errors.Is(err, unix.ENOTTY) && !errors.Is(err, unix.EINVAL) {
			return result, fmt.Errorf("owned backing clone: %w", err)
		}
		if err == nil {
			cloned = true
			result.SHA256 = expectedHash
		}
	}
	if !cloned {
		hash := sha256.New()
		if buffer == nil {
			buffer = make([]byte, 1<<20)
		}
		count, err := io.CopyBuffer(io.MultiWriter(target, hash), struct{ io.Reader }{logical}, buffer)
		if err != nil || count != info.Size() {
			if err == nil {
				err = io.ErrUnexpectedEOF
			}
			return result, fmt.Errorf("snapshot copy incomplete: source=%s destination=%s bytes=%d/%d: %w",
				source, destination, count, info.Size(), err)
		}
		result.SHA256 = hex.EncodeToString(hash.Sum(nil))
		if expectedHash != "" && expectedHash != result.SHA256 {
			return result, fmt.Errorf("shared snapshot source bytes differ")
		}
	}
	if err := target.Chown(int(stat.Uid), int(stat.Gid)); err != nil {
		return result, err
	}
	if err := unix.Fchmod(int(target.Fd()), result.Mode); err != nil {
		return result, err
	}
	actualInfo, err := target.Stat()
	if err != nil || actualInfo.Size() != info.Size() {
		return result, fmt.Errorf("backing snapshot size differs: %v", err)
	}
	if durable {
		if err := target.Sync(); err != nil {
			return result, err
		}
	}
	actual, err := criuRAMHashFile(destination)
	if err != nil || actual != result.SHA256 {
		return result, fmt.Errorf("backing snapshot bytes differ: %v", err)
	}
	if durable {
		// Clean disk page cache is reclaimable, but explicitly discard this
		// snapshot's verified bytes before acknowledging RAM-releasing pause.
		if err := unix.Fadvise(int(target.Fd()), 0, 0, unix.FADV_DONTNEED); err != nil {
			return result, err
		}
	}
	return result, nil
}

func criuRAMExportDirectory(images, target, cloneRoot string, cloned map[string]criuRAMFile, allowLazySocket bool) (map[string]criuRAMFile, error) {
	entries, err := criuRAMImageEntries(images, allowLazySocket)
	if err != nil {
		return nil, err
	}
	if err := os.Mkdir(target, 0700); err != nil {
		return nil, err
	}
	info, err := os.Stat(images)
	if err != nil {
		return nil, err
	}
	stat := info.Sys().(*syscall.Stat_t)
	manifest := map[string]criuRAMFile{}
	buffer := make([]byte, 1<<20)
	for _, entry := range entries {
		if entry.Name() == "parent" && entry.Type()&os.ModeSymlink != 0 {
			file, err := criuSysboxExportParentLink(filepath.Join(images, "parent"), filepath.Join(target, "parent"))
			if err != nil {
				return nil, err
			}
			manifest["parent"] = file
			continue
		}
		cloneFrom, expected := "", ""
		if saved, ok := cloned[entry.Name()]; ok && criuRAMPageName.MatchString(entry.Name()) {
			cloneFrom, expected = filepath.Join(cloneRoot, entry.Name()), saved.SHA256
		}
		file, err := criuRAMExportFile(filepath.Join(images, entry.Name()), filepath.Join(target, entry.Name()), cloneFrom, expected, true, buffer)
		if err != nil {
			return nil, err
		}
		manifest[entry.Name()] = file
	}
	if err := os.Chown(target, int(stat.Uid), int(stat.Gid)); err != nil {
		return nil, err
	}
	if err := os.Chmod(target, info.Mode().Perm()); err != nil {
		return nil, err
	}
	directory, err := os.Open(target)
	if err != nil {
		return nil, err
	}
	err = directory.Sync()
	directory.Close()
	return manifest, err
}

func criuRAMReadonlyBind(source, target string) error {
	if err := unix.Mount(source, target, "", unix.MS_BIND, ""); err != nil {
		return err
	}
	if err := unix.Mount("", target, "", unix.MS_BIND|unix.MS_REMOUNT|unix.MS_RDONLY|unix.MS_NOSUID|unix.MS_NODEV|unix.MS_NOEXEC, ""); err != nil {
		unix.Unmount(target, 0)
		return err
	}
	file, err := os.OpenFile(target, os.O_WRONLY, 0)
	if file != nil {
		file.Close()
	}
	if !errors.Is(err, unix.EROFS) {
		return fmt.Errorf("snapshot payload did not reject writes: %v", err)
	}
	return nil
}

func criuSysboxCloneRAMImages(source, target string) (handled bool, resultErr error) {
	snapshot := criuRAMSnapshotFind(source)
	if snapshot == nil {
		return false, nil
	}
	start := time.Now()
	defer func() {
		record("sysbox-ram-fork-readonly-payload", start,
			asJSON(map[string]any{"source_images": source, "copy_images": target, "readonly_bindings": len(snapshot.Bindings), "saved_bytes": snapshot.Bytes, "scope": "one immutable saved RAM payload; branch metadata and live RAM remain private"}), resultErr)
	}()
	if err := criuRAMSnapshotPath(criuRoot, target); err != nil {
		return true, err
	}
	entries, err := criuRAMImageEntries(source, false)
	if err != nil {
		return true, err
	}
	if err := os.Mkdir(target, 0700); err != nil {
		return true, err
	}
	info, err := os.Stat(source)
	if err != nil {
		return true, err
	}
	stat := info.Sys().(*syscall.Stat_t)
	buffer := make([]byte, 1<<20)
	for _, entry := range entries {
		a, b := filepath.Join(source, entry.Name()), filepath.Join(target, entry.Name())
		if entry.Name() == "parent" && entry.Type()&os.ModeSymlink != 0 {
			if _, err := criuSysboxExportParentLink(a, b); err != nil {
				return true, err
			}
			continue
		}
		if criuRAMPageName.MatchString(entry.Name()) {
			if err := criuRAMReadonlyBind(a, a); err != nil {
				return true, err
			}
			snapshot.Bindings = append(snapshot.Bindings, a)
			file, err := os.OpenFile(b, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if err != nil {
				return true, err
			}
			file.Close()
			if err := criuRAMReadonlyBind(a, b); err != nil {
				return true, err
			}
			snapshot.Bindings = append(snapshot.Bindings, b)
			x, err := os.Stat(a)
			if err != nil {
				return true, err
			}
			y, err := os.Stat(b)
			if err != nil {
				return true, err
			}
			if !os.SameFile(x, y) {
				return true, fmt.Errorf("fork duplicated or swapped saved RAM payload")
			}
		} else {
			if _, err := criuRAMExportFile(a, b, "", "", false, buffer); err != nil {
				return true, err
			}
		}
	}
	if err := os.Chown(target, int(stat.Uid), int(stat.Gid)); err != nil {
		return true, err
	}
	if err := os.Chmod(target, info.Mode().Perm()); err != nil {
		return true, err
	}
	snapshot.CopyImages = target
	return true, nil
}

func criuSysboxReleaseRAMCapture(images string) (resultErr error) {
	snapshot := criuRAMSnapshotFind(images)
	if snapshot == nil {
		return nil
	}
	start := time.Now()
	output := map[string]any{"images": images, "disk_file_cache_eviction_requested": true, "scope": "verified synced backing and removal of retained tmpfs snapshot RAM; clean disk file cache eviction requested; workloads/lazy readers must already be stopped"}
	defer func() { record("sysbox-ram-snapshot-backed-and-released", start, asJSON(output), resultErr) }()
	disk := images + "-disk"
	manifest, err := criuRAMExportDirectory(images, disk, "", nil, true)
	if err != nil {
		return err
	}
	if snapshot.CopyImages != "" {
		copyManifest, err := criuRAMExportDirectory(snapshot.CopyImages, snapshot.CopyImages+"-disk", disk, manifest, true)
		if err != nil {
			return err
		}
		output["copy_files_verified"] = len(copyManifest)
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	proof := filepath.Join(criuRoot, "ram-capture-"+filepath.Base(images)+"-backing.json")
	if err := os.WriteFile(proof, append(data, '\n'), 0600); err != nil {
		return err
	}
	output["manifest"], output["files_verified"] = proof, len(manifest)
	if err := criuLogicalRetireDirectory(images, disk); err != nil {
		return err
	}
	retireFiles := (images == criuRoot+"/sysbox/images-base" && os.Getenv("CRIU_SYSBOX_RAM_BASE") == "1") ||
		(images == "/experiment/sysbox/images-again" && os.Getenv("RUNTIME_FORK_RICH_EPOCH_CHAIN") == "1" &&
			os.Getenv("RUNTIME_FORK_EPOCH_RAM_PARENT") == "1" && os.Getenv("RUNTIME_FORK_EPOCH_REBASE") == "1")
	if err := criuRetireCommittedSnapshot(images, manifest, retireFiles); err != nil {
		return err
	}
	before, err := criuRAMMemoryRead()
	if err != nil {
		return err
	}
	output["before_release"] = before
	if retireFiles {
		// A copied mount namespace can retain this tmpfs after its controller
		// mounts disappear. All readers are stopped and every file above has
		// verified, synced backing. Retire the owned RAM files before unmount
		// so namespace references alone cannot keep their payload resident.
		for name := range manifest {
			if err := os.Remove(filepath.Join(images, name)); err != nil {
				return err
			}
		}
		output["retired_ram_base_files_removed"] = len(manifest)
	}
	for i := len(snapshot.Bindings) - 1; i >= 0; i-- {
		if err := unix.Unmount(snapshot.Bindings[i], 0); err != nil {
			return err
		}
	}
	snapshot.Bindings = nil
	if err := unix.Unmount(images, 0); err != nil {
		return err
	}
	snapshot.Mounted = false
	if err := os.Remove(images); err != nil {
		return err
	}
	if err := os.Rename(disk, images); err != nil {
		return err
	}
	if snapshot.CopyImages != "" {
		previous := snapshot.CopyImages + "-previous"
		if err := os.Rename(snapshot.CopyImages, previous); err != nil {
			return err
		}
		if err := os.Rename(snapshot.CopyImages+"-disk", snapshot.CopyImages); err != nil {
			return err
		}
		if err := os.RemoveAll(previous); err != nil {
			return err
		}
	}
	for _, path := range []string{images, snapshot.CopyImages} {
		if path == "" {
			continue
		}
		parent, err := os.Open(filepath.Dir(path))
		if err != nil {
			return err
		}
		err = parent.Sync()
		parent.Close()
		if err != nil {
			return err
		}
	}
	var fs unix.Statfs_t
	if err := unix.Statfs(images, &fs); err != nil {
		return err
	}
	if fs.Type != snapshot.BackingFS || fs.Type == unix.TMPFS_MAGIC {
		return fmt.Errorf("released snapshot is not on its original file-backed volume")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		after, err := criuRAMMemoryRead()
		if err != nil {
			return err
		}
		output["after_release"] = after
		if before.Stat["shmem"] >= after.Stat["shmem"] && before.Stat["shmem"]-after.Stat["shmem"] >= snapshot.Allocated {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("snapshot RAM release not established: before=%d after=%d expected=%d", before.Stat["shmem"], after.Stat["shmem"], snapshot.Allocated)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if criuParentManifestReuseEnabled() && images == criuRoot+"/sysbox/images" {
		registrationStart := time.Now()
		if err := criuRegisterVerifiedParentFiles(images, manifest); err != nil {
			return err
		}
		criuParentProofRegistrationTime = time.Since(registrationStart)
		output["verified_payload_identities_retained"] = len(criuVerifiedParentFiles)
		record("sysbox-verified-parent-cache-retained", registrationStart,
			asJSON(map[string]any{"files": len(criuVerifiedParentFiles),
				"scope": "metadata identities retained after existing full byte verification and synced backing; extra cost separately charged at fork"}), nil)
	}
	return nil
}

func criuSysboxRAMCaptureCleanup() error {
	if os.Getenv("RUNTIME_FORK_RICH_EPOCH_CHAIN") == "1" && os.Getenv("RUNTIME_FORK_EPOCH_RAM_PARENT") == "1" &&
		os.Getenv("RUNTIME_FORK_EPOCH_REBASE") == "1" {
		return criuEpochRAMCleanup()
	}
	var result error
	for _, snapshot := range criuRAMSnapshots {
		if snapshot.Mounted && snapshot.Images != criuRoot+"/sysbox/images-base" {
			if err := criuSysboxReleaseRAMCapture(snapshot.Images); err != nil {
				result = errors.Join(result, err)
			}
		}
	}
	if base := criuRAMSnapshotFind(criuRoot + "/sysbox/images-base"); base != nil && result == nil {
		// Child backing still resolves its parent link while exported above.
		// Remove both immutable directory binds before replacing the RAM base
		// with its verified file backing at the same fixed sibling path.
		if err := criuSysboxParentCleanup(); err != nil {
			return err
		}
		result = criuSysboxReleaseRAMCapture(base.Images)
	}
	return result
}

func criuSysboxRAMSnapshotScope(images string) string {
	for _, snapshot := range criuRAMSnapshots {
		if snapshot.Mounted && (snapshot.Images == images || snapshot.CopyImages == images) {
			return "complete bounded RAM snapshot retained and charged; not RAM-releasing suspend"
		}
	}
	return "complete file-backed snapshot"
}

func criuSysboxRAMLiveAccounting(images string) (resultErr error) {
	snapshot := criuRAMSnapshotFind(images)
	if snapshot == nil {
		return nil
	}
	start := time.Now()
	memory, err := criuRAMMemoryRead()
	output := map[string]any{
		"images": images, "allocated_snapshot_bytes": snapshot.Allocated, "snapshot_file_bytes": snapshot.Bytes,
		"shared_readonly_bindings": len(snapshot.Bindings), "memory": memory,
		"scope": "both live branches are ready; immutable snapshot RAM remains charged until backing/teardown"}
	if base := criuRAMSnapshotFind(criuRoot + "/sysbox/images-base"); base != nil {
		output["base_snapshot_allocated_bytes"] = base.Allocated
		output["base_snapshot_file_bytes"] = base.Bytes
	}
	record("sysbox-ram-live-fork-accounting", start, asJSON(output), err)
	return err
}
