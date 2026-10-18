package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

type descendantRAMCapture struct {
	Source       descendantLayout
	Images       string
	TargetImages string
	BackingFS    int64
	Allocated    uint64
	Logical      uint64
	Mounted      bool
	Bindings     []string
	Sealed       committedFileIdentity
	Cloned       map[string]string
	Pages        map[string]descendantLeaseIdentity
}

func descendantRAMPath(layout descendantLayout, images string, capture bool) error {
	owned, err := descendantOwnedLayout(layout.ID)
	if err != nil || owned != layout || filepath.Clean(images) != images ||
		filepath.Dir(images) != layout.Root {
		return fmt.Errorf("RAM images are outside canonical owned lineage")
	}
	if capture {
		if !descendantCaptureGeneration.MatchString(filepath.Base(images)) {
			return fmt.Errorf("RAM capture requires a fresh image generation")
		}
	} else if filepath.Base(images) != "images" || !descendantID.MatchString(layout.ID) {
		return fmt.Errorf("RAM clone requires one canonical new descendant")
	}
	return nil
}

func descendantPrepareRAM(source descendantLayout, images, evidence string) (*descendantRAMCapture, error) {
	if err := descendantRAMPath(source, images, true); err != nil {
		return nil, err
	}
	if err := os.Mkdir(images, 0700); err != nil {
		return nil, err
	}
	var fs unix.Statfs_t
	if err := unix.Statfs(images, &fs); err != nil || fs.Type == unix.TMPFS_MAGIC {
		return nil, fmt.Errorf("RAM capture requires separate file backing: %v", err)
	}
	state := &descendantRAMCapture{Source: source, Images: images, BackingFS: fs.Type}
	intent, err := os.OpenFile(filepath.Join(evidence, "ram-intent.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, err
	}
	_, writeErr := intent.WriteString(asJSON(map[string]any{
		"source_id": source.ID, "images": images, "bound_bytes": criuRAMSnapshotLimit,
		"backing_filesystem": fs.Type,
	}) + "\n")
	if err := errors.Join(writeErr, intent.Sync(), intent.Close()); err != nil {
		return nil, err
	}
	if err := unix.Mount("owned-descendant-ram", images, "tmpfs",
		unix.MS_NOSUID|unix.MS_NODEV|unix.MS_NOEXEC, "size=1073741824,mode=0700"); err != nil {
		return nil, err
	}
	state.Mounted = true
	return state, nil
}

func (state *descendantRAMCapture) seal() error {
	var fs unix.Statfs_t
	if err := unix.Statfs(state.Images, &fs); err != nil || fs.Type != unix.TMPFS_MAGIC ||
		uint64(fs.Blocks)*uint64(fs.Bsize) > criuRAMSnapshotLimit {
		return fmt.Errorf("RAM capture lost bounded tmpfs identity: %v", err)
	}
	entries, err := criuRAMImageEntries(state.Images, false)
	if err != nil {
		return err
	}
	var pages int
	state.Logical = 0
	state.Pages = map[string]descendantLeaseIdentity{}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || uint64(info.Size()) > criuRAMSnapshotLimit-state.Logical {
			return fmt.Errorf("RAM capture exceeds its complete payload bound: %v", err)
		}
		state.Logical += uint64(info.Size())
		if criuRAMPageName.MatchString(entry.Name()) {
			pages++
			state.Pages[entry.Name()] = descendantLeaseFileIdentity(info)
		}
	}
	if pages == 0 || state.Logical == 0 {
		return fmt.Errorf("RAM capture lacks complete saved pages")
	}
	state.Allocated = uint64(fs.Blocks-fs.Bfree) * uint64(fs.Bsize)
	// No MS_BIND: publication makes the owned superblock read-only across
	// every alias. A surviving writable capture descriptor must reject it.
	if err := unix.Mount("", state.Images, "", unix.MS_REMOUNT|unix.MS_RDONLY|
		unix.MS_NOSUID|unix.MS_NODEV|unix.MS_NOEXEC, ""); err != nil {
		return fmt.Errorf("publish complete RAM capture read-only: %w", err)
	}
	if err := unix.Statfs(state.Images, &fs); err != nil || fs.Flags&unix.ST_RDONLY == 0 {
		return fmt.Errorf("RAM publication is not filesystem-wide read-only: %v", err)
	}
	info, err := os.Lstat(state.Images)
	if err != nil {
		return err
	}
	state.Sealed = committedIdentity(info)
	return nil
}

// The runtime must already have closed reader admission and completed its
// managed pager/keeper. This extra check rejects observable stray FDs/mappings;
// it does not prevent a foreign process from opening a new reader afterwards.
func (state *descendantRAMCapture) rejectLiveReaders() error {
	if err := state.verifyPublication(); err != nil {
		return err
	}
	identities := map[[2]uint64]bool{}
	for name, saved := range state.Pages {
		info, err := os.Lstat(filepath.Join(state.Images, name))
		if err != nil || !info.Mode().IsRegular() || descendantLeaseFileIdentity(info) != saved {
			return fmt.Errorf("RAM page identity changed before retirement: %s %v", name, err)
		}
		identities[[2]uint64{saved.Device, saved.Inode}] = true
	}
	if len(identities) == 0 {
		return fmt.Errorf("RAM retirement lacks sealed page identities")
	}
	processes, err := os.ReadDir("/proc")
	if err != nil {
		return err
	}
	for _, process := range processes {
		if _, err := strconv.ParseUint(process.Name(), 10, 32); err != nil {
			continue
		}
		root := filepath.Join("/proc", process.Name())
		fds, err := os.ReadDir(root + "/fd")
		if os.IsNotExist(err) || errors.Is(err, unix.ESRCH) {
			continue
		}
		if err != nil {
			return fmt.Errorf("cannot inspect snapshot readers: %w", err)
		}
		for _, fd := range fds {
			info, err := os.Stat(filepath.Join(root, "fd", fd.Name()))
			if os.IsNotExist(err) || errors.Is(err, unix.ESRCH) {
				continue
			}
			if err != nil {
				return fmt.Errorf("cannot inspect snapshot reader descriptor: %w", err)
			}
			identity := descendantLeaseFileIdentity(info)
			if identities[[2]uint64{identity.Device, identity.Inode}] {
				return fmt.Errorf("live snapshot page reader: pid=%s fd=%s", process.Name(), fd.Name())
			}
		}
		maps, err := os.ReadFile(root + "/maps")
		if os.IsNotExist(err) || errors.Is(err, unix.ESRCH) {
			continue
		}
		if err != nil {
			return fmt.Errorf("cannot inspect snapshot reader mappings: %w", err)
		}
		for _, line := range strings.Split(string(maps), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 5 {
				continue
			}
			device := strings.Split(fields[3], ":")
			if len(device) != 2 {
				return fmt.Errorf("invalid reader mapping device")
			}
			major, majorErr := strconv.ParseUint(device[0], 16, 32)
			minor, minorErr := strconv.ParseUint(device[1], 16, 32)
			inode, inodeErr := strconv.ParseUint(fields[4], 10, 64)
			if majorErr != nil || minorErr != nil || inodeErr != nil {
				return fmt.Errorf("invalid reader mapping identity")
			}
			if identities[[2]uint64{unix.Mkdev(uint32(major), uint32(minor)), inode}] {
				return fmt.Errorf("live snapshot page reader: pid=%s mapping=%s", process.Name(), fields[0])
			}
		}
	}
	return nil
}

func (state *descendantRAMCapture) verifyPublication() error {
	var fs unix.Statfs_t
	info, err := os.Lstat(state.Images)
	if err != nil || !info.IsDir() || committedIdentity(info) != state.Sealed {
		return fmt.Errorf("published RAM directory changed: %v", err)
	}
	if err := unix.Statfs(state.Images, &fs); err != nil || fs.Type != unix.TMPFS_MAGIC ||
		fs.Flags&unix.ST_RDONLY == 0 {
		return fmt.Errorf("published RAM filesystem became writable or changed: %v", err)
	}
	return nil
}

func (state *descendantRAMCapture) clone(target descendantLayout, expected map[string]string) error {
	images := target.Root + "/images"
	if err := descendantRAMPath(target, images, false); err != nil {
		return err
	}
	if !state.Mounted || state.TargetImages != "" {
		return fmt.Errorf("RAM transport already has a clone or is retired")
	}
	if err := state.verifyPublication(); err != nil {
		return err
	}
	if err := os.Mkdir(images, 0700); err != nil {
		return err
	}
	state.TargetImages = images
	entries, err := criuRAMImageEntries(state.Images, false)
	if err != nil {
		return err
	}
	info, err := os.Stat(state.Images)
	if err != nil {
		return err
	}
	owner := info.Sys().(*syscall.Stat_t)
	buffer := make([]byte, 64<<10)
	verified := map[string]string{}
	for _, entry := range entries {
		source, destination := filepath.Join(state.Images, entry.Name()), filepath.Join(images, entry.Name())
		if criuRAMPageName.MatchString(entry.Name()) {
			file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if err != nil {
				return err
			}
			file.Close()
			if err := criuRAMReadonlyBind(source, destination); err != nil {
				return err
			}
			state.Bindings = append(state.Bindings, destination)
			a, aErr := os.Stat(source)
			b, bErr := os.Stat(destination)
			if aErr != nil || bErr != nil || !os.SameFile(a, b) || len(expected[entry.Name()]) != 64 {
				return fmt.Errorf("immutable page alias differs from completely hashed source: %s", entry.Name())
			}
			verified[entry.Name()] = expected[entry.Name()]
		} else {
			copied, err := criuRAMExportFile(source, destination, "", "", false, buffer)
			if err != nil {
				return err
			}
			if checksum, payload := expected[entry.Name()]; payload {
				if copied.SHA256 != checksum {
					return fmt.Errorf("private clone metadata differs from complete source hash")
				}
				verified[entry.Name()] = copied.SHA256
			}
		}
	}
	if len(verified) != len(expected) {
		return fmt.Errorf("RAM clone omitted complete source payload")
	}
	if err := errors.Join(os.Chown(images, int(owner.Uid), int(owner.Gid)),
		os.Chmod(images, info.Mode().Perm()), state.verifyPublication()); err != nil {
		return err
	}
	state.Cloned = verified
	return nil
}

// Both workloads may run, but the clone's pager must have completed before
// this call. The source capture is not the original running source's backing.
func (state *descendantRAMCapture) release(expected map[string]string) (map[string]any, error) {
	start := time.Now()
	if !state.Mounted || state.TargetImages == "" {
		return nil, fmt.Errorf("RAM release has no complete owned capture/clone")
	}
	for _, daemon := range criuLazyDaemons {
		if filepath.Dir(daemon.logPath) == state.TargetImages &&
			(!daemon.pollCompletion() || daemon.exitErr != nil) {
			return nil, fmt.Errorf("RAM release still has a live or failed clone pager")
		}
	}
	if descendantCompleteRAMEnabled() {
		if err := state.rejectLiveReaders(); err != nil {
			return nil, err
		}
	}
	disk, targetDisk := state.Images+"-disk", state.TargetImages+"-disk"
	backing, err := criuRAMExportDirectory(state.Images, disk, "", nil, false)
	if err != nil {
		return nil, err
	}
	if _, err := criuRAMExportDirectory(state.TargetImages, targetDisk, disk, backing, true); err != nil {
		return nil, err
	}
	for _, path := range []string{disk, targetDisk} {
		got, err := descendantSnapshotManifest(path)
		if err != nil || asJSON(got) != asJSON(expected) {
			return nil, fmt.Errorf("complete RAM fork backing differs from original writer: %s %v", path, err)
		}
		var fs unix.Statfs_t
		if err := unix.Statfs(path, &fs); err != nil || fs.Type != state.BackingFS || fs.Type == unix.TMPFS_MAGIC {
			return nil, fmt.Errorf("RAM fork backing changed filesystem: %v", err)
		}
	}
	// The logical keeper may retain the exact raw image inode even after all
	// page clients have finished. Retire it against complete synced backing
	// before clearing/unmounting the tmpfs, including mandatory sparse views.
	if err := criuLogicalRetireDirectory(state.Images, disk); err != nil {
		return nil, fmt.Errorf("retire logical RAM capture before unmount: %w", err)
	}
	if err := state.rejectLiveReaders(); err != nil {
		return nil, err
	}
	before, err := criuRAMMemoryRead()
	if err != nil {
		return nil, err
	}
	for index := len(state.Bindings) - 1; index >= 0; index-- {
		if err := unix.Unmount(state.Bindings[index], 0); err != nil {
			return nil, err
		}
	}
	state.Bindings = nil
	// Complete verified backing now owns the snapshot. Clear only this
	// retired tmpfs so copied namespace references cannot retain its RAM.
	if err := unix.Mount("", state.Images, "", unix.MS_REMOUNT|unix.MS_NOSUID|unix.MS_NODEV|unix.MS_NOEXEC, ""); err != nil {
		return nil, err
	}
	entries, err := criuRAMImageEntries(state.Images, false)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		path := filepath.Join(state.Images, entry.Name())
		if saved, ok := state.Pages[entry.Name()]; ok {
			file, err := os.OpenFile(path, os.O_RDWR|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
			if err != nil {
				return nil, err
			}
			info, statErr := file.Stat()
			if statErr != nil || !info.Mode().IsRegular() || descendantLeaseFileIdentity(info) != saved {
				file.Close()
				return nil, fmt.Errorf("RAM page changed before exact-inode retirement: %s %v", entry.Name(), statErr)
			}
			if err := errors.Join(file.Truncate(0), file.Close()); err != nil {
				return nil, err
			}
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	}
	if err := unix.Unmount(state.Images, 0); err != nil {
		return nil, err
	}
	state.Mounted = false
	if err := os.Remove(state.Images); err != nil {
		return nil, err
	}
	if err := os.Rename(disk, state.Images); err != nil {
		return nil, err
	}
	previous := state.TargetImages + "-ram-retired"
	if err := os.Rename(state.TargetImages, previous); err != nil {
		return nil, err
	}
	if err := os.Rename(targetDisk, state.TargetImages); err != nil {
		return nil, err
	}
	if err := os.RemoveAll(previous); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(2 * time.Second)
	var after criuRAMMemory
	for {
		after, err = criuRAMMemoryRead()
		if err != nil {
			return nil, err
		}
		if before.Stat["shmem"] >= after.Stat["shmem"] &&
			before.Stat["shmem"]-after.Stat["shmem"] >= state.Allocated {
			break
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("RAM fork transport release not observed: allocated=%d before=%d after=%d",
				state.Allocated, before.Stat["shmem"], after.Stat["shmem"])
		}
		time.Sleep(10 * time.Millisecond)
	}
	return map[string]any{
		"released": true, "bound_bytes": criuRAMSnapshotLimit, "logical_bytes": state.Logical,
		"allocated_snapshot_bytes": state.Allocated, "actual_shmem_released_bytes": before.Stat["shmem"] - after.Stat["shmem"],
		"elapsed_ms": time.Since(start).Milliseconds(), "source_backing": state.Images, "clone_backing": state.TargetImages,
		"retained_capture_retired_before_unmount":            true,
		"exact_page_inodes_truncated_after_verified_backing": true,
		"observable_page_readers_absent_before_retirement":   true,
		"before": before, "after": after,
		"scope": "Complete source/clone backing synced and independently read back before all owned RAM payloads/aliases retire. Original source runtime identity and ancestor backing remain.",
	}, nil
}

func (state *descendantRAMCapture) cleanup() error {
	if state == nil || !state.Mounted {
		return nil
	}
	var result error
	for index := len(state.Bindings) - 1; index >= 0; index-- {
		result = errors.Join(result, unix.Unmount(state.Bindings[index], 0))
	}
	if result == nil {
		result = unix.Unmount(state.Images, 0)
		if result == nil {
			state.Mounted = false
			state.Bindings = nil
		}
	}
	return result
}
