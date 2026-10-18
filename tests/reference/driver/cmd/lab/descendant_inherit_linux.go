package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"golang.org/x/sys/unix"
)

type descendantParentMount struct {
	Path      string
	Device    uint64
	Inode     uint64
	MountID   uint64
	Directory bool
}

type descendantParentPins struct {
	Mounts []descendantParentMount
}

func (pins *descendantParentPins) close() error {
	var result error
	for index := len(pins.Mounts) - 1; index >= 0; index-- {
		mount := pins.Mounts[index]
		info, err := os.Lstat(mount.Path)
		id, mountErr := epochReplayMountID(unix.AT_FDCWD, mount.Path, unix.AT_SYMLINK_NOFOLLOW)
		if err != nil || mountErr != nil || info.IsDir() != mount.Directory ||
			(!mount.Directory && !info.Mode().IsRegular()) {
			result = errors.Join(result, fmt.Errorf("owned parent pin disappeared: %v %v", err, mountErr))
			continue
		}
		saved := descendantLeaseFileIdentity(info)
		if saved.Device != mount.Device || saved.Inode != mount.Inode || id != mount.MountID {
			result = errors.Join(result, fmt.Errorf("replaced parent pin preserved: %s", mount.Path))
			continue
		}
		result = errors.Join(result, unix.Unmount(mount.Path, 0))
	}
	pins.Mounts = nil
	return result
}

// RAM image clones contain readonly file mounts over empty placeholders.
// A nonrecursive directory bind would hide those payloads from the new capture.
func descendantParentPayloadMounts(directory string) ([]descendantParentMount, error) {
	rootID, err := epochReplayMountID(unix.AT_FDCWD, directory, unix.AT_SYMLINK_NOFOLLOW)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	var result []descendantParentMount
	for _, entry := range entries {
		path := filepath.Join(directory, entry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			return nil, fmt.Errorf("snapshot parent contains an unsupported nested directory")
		}
		mountID, err := epochReplayMountID(unix.AT_FDCWD, path, unix.AT_SYMLINK_NOFOLLOW)
		if err != nil {
			return nil, err
		}
		if mountID == rootID {
			continue
		}
		var fs unix.Statfs_t
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("snapshot parent contains a non-file payload mount")
		}
		if err := unix.Statfs(path, &fs); err != nil || fs.Flags&unix.ST_RDONLY == 0 {
			return nil, fmt.Errorf("snapshot parent payload mount is writable: %s %v", path, err)
		}
		identity := descendantLeaseFileIdentity(info)
		result = append(result, descendantParentMount{
			Path: path, Device: identity.Device, Inode: identity.Inode, MountID: mountID})
	}
	return result, nil
}

func descendantPinSnapshotParents(layout descendantLayout, images string) (_ *descendantParentPins, resultErr error) {
	pins := &descendantParentPins{}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, pins.close())
		}
	}()
	seen := map[string]bool{}
	for current := images; current != ""; {
		actual, err := filepath.EvalSymlinks(current)
		if err != nil || actual != current || !descendantGraphWithin(layout.Root, actual) {
			return nil, fmt.Errorf("inherited source parent escaped its runtime: %v", err)
		}
		if seen[current] {
			return nil, fmt.Errorf("inherited source parent graph contains a cycle")
		}
		seen[current] = true
		info, err := os.Lstat(current)
		if err != nil || !info.IsDir() {
			return nil, fmt.Errorf("inherited source parent is not a real directory: %v", err)
		}
		identity := descendantLeaseFileIdentity(info)
		if identity.UID != 165536 || identity.GID != 165536 || info.Mode().Perm() != 0700 {
			return nil, fmt.Errorf("inherited source parent lost mapped-root private ownership")
		}
		payloads, err := descendantParentPayloadMounts(current)
		if err != nil {
			return nil, err
		}
		var fs unix.Statfs_t
		if err := unix.Statfs(current, &fs); err != nil {
			return nil, err
		}
		if fs.Flags&unix.ST_RDONLY == 0 {
			if err := unix.Mount(current, current, "", unix.MS_BIND|unix.MS_REC, ""); err != nil {
				return nil, err
			}
			mountID, err := epochReplayMountID(unix.AT_FDCWD, current, unix.AT_SYMLINK_NOFOLLOW)
			if err != nil {
				return nil, fmt.Errorf("cannot identify new recursive snapshot pin; preserve its mounts: %w", err)
			}
			pins.Mounts = append(pins.Mounts, descendantParentMount{
				Path: current, Device: identity.Device, Inode: identity.Inode, MountID: mountID, Directory: true})
			var payloadErr error
			for _, payload := range payloads {
				clonedID, err := epochReplayMountID(unix.AT_FDCWD, payload.Path, unix.AT_SYMLINK_NOFOLLOW)
				if err != nil || clonedID == payload.MountID {
					payloadErr = errors.Join(payloadErr, fmt.Errorf("recursive snapshot payload mount was not cloned: %v", err))
					continue
				}
				pins.Mounts = append(pins.Mounts, descendantParentMount{
					Path: payload.Path, Device: payload.Device, Inode: payload.Inode, MountID: clonedID})
				info, err := os.Lstat(payload.Path)
				if err != nil {
					payloadErr = errors.Join(payloadErr, err)
					continue
				}
				actual := descendantLeaseFileIdentity(info)
				if actual.Device != payload.Device || actual.Inode != payload.Inode {
					payloadErr = errors.Join(payloadErr, fmt.Errorf("recursive snapshot pin changed a payload inode"))
				}
			}
			if payloadErr != nil {
				return nil, payloadErr
			}
			if err := unix.Mount("", current, "", unix.MS_BIND|unix.MS_REMOUNT|unix.MS_RDONLY|
				unix.MS_NOSUID|unix.MS_NODEV|unix.MS_NOEXEC, ""); err != nil {
				return nil, err
			}
		}
		link, err := os.Readlink(filepath.Join(current, "parent"))
		if os.IsNotExist(err) {
			break
		}
		if err != nil || filepath.IsAbs(link) {
			return nil, fmt.Errorf("inherited source parent link is invalid: %v", err)
		}
		current, err = filepath.EvalSymlinks(filepath.Join(current, link))
		if err != nil {
			return nil, err
		}
	}
	return pins, nil
}

type descendantInheritedCapture struct {
	Images, OriginalImages string
	Pins                   *descendantParentPins
	Proof                  map[string]any
}

func descendantAdmitCompleteRAM(images, parent string) (_ map[string]any, resultErr error) {
	graph, err := descendantOpenMemoryGraph(images)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, graph.close()) }()
	entries, err := os.ReadDir(images)
	if err != nil {
		return nil, err
	}
	var maps, pages uint64
	for _, entry := range entries {
		if !descendantMemoryName.MatchString(entry.Name()) {
			continue
		}
		key := entry.Name()[len("pagemap-") : len(entry.Name())-len(".img")]
		memory, err := graph.load(images, key)
		if err != nil {
			return nil, err
		}
		for _, span := range memory.Saved.Ranges {
			if span.Flags&4 == 0 || span.Flags&1 != 0 {
				return nil, fmt.Errorf("complete RAM writer retained an inherited memory dependency")
			}
			pages += span.Pages
		}
		if memory.Parent != nil {
			return nil, fmt.Errorf("complete RAM writer resolved an ancestor")
		}
		maps++
	}
	if maps == 0 || pages == 0 {
		return nil, fmt.Errorf("complete RAM writer has no local memory")
	}
	link := filepath.Join(images, "parent")
	if _, err := os.Lstat(link); err == nil {
		actual, err := filepath.EvalSymlinks(link)
		if err != nil || actual != parent {
			return nil, fmt.Errorf("complete RAM writer parent differs from admitted source: %v", err)
		}
		if err := os.Remove(link); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return map[string]any{"memory_maps": maps, "local_bytes": pages * 4096,
		"inherited_bytes": uint64(0), "parent_dependency_removed": true,
		"scope": "Complete original writer with every saved range locally present; no inherited selection or separate same-path byte oracle."}, nil
}

func descendantCaptureInheritedSource(layout descendantLayout, daemon *criuLazyDaemon,
	identity map[string]any, retained map[int]string, guard *descendantGuard,
	evidence string) (_ *descendantInheritedCapture, resultErr error) {
	if !descendantInheritProofEnabled() || guard == nil || daemon == nil || daemon.owner == nil {
		return nil, fmt.Errorf("inherited source capture requires its bound pager and original guard")
	}
	if err := daemon.owner.verify(daemon, identity); err != nil {
		return nil, err
	}
	pins, err := descendantPinSnapshotParents(layout, daemon.owner.Images)
	if err != nil {
		return nil, err
	}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, pins.close())
		}
	}()
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	capture := &descendantInheritedCapture{
		Images:         layout.Root + "/images-" + hex.EncodeToString(nonce[:16]),
		OriginalImages: layout.Root + "/images-" + hex.EncodeToString(nonce[16:]), Pins: pins,
		Proof: map[string]any{"runtime": layout, "parent_images": daemon.owner.Images,
			"daemon_pid": daemon.command.Process.Pid, "endpoint": daemon.inheritSocket,
			"native_pid": daemon.owner.KernelPID, "native_start": daemon.owner.KernelStart,
			"parent_device": daemon.owner.ImageDevice, "parent_inode": daemon.owner.ImageInode,
			"same_capture_original_writer": true,
			"scope": "Actual inheritance selection with the complete original writer from that same memory collection. " +
				"The later COW capture remains the full runtime control, not the inherited byte oracle."},
	}
	captureDirectory, oracleMode := capture.OriginalImages, "1"
	if descendantStreamOriginalEnabled() {
		captureDirectory, oracleMode = capture.Images, "2"
		capture.OriginalImages = capture.Images + "/original-writer"
		capture.Proof["native_stream_original_writer"] = true
	}
	if descendantFastSourceEnabled() {
		if !descendantSingleCaptureEnabled() || descendantStreamOriginalEnabled() {
			return nil, fmt.Errorf("ordinary fast collection requires single capture without a stream shadow")
		}
		captureDirectory, oracleMode = capture.Images, "0"
		capture.OriginalImages = ""
		capture.Proof["same_capture_original_writer"] = false
		capture.Proof["ordinary_fast_collector"] = true
		capture.Proof["scope"] = "Ordinary inherited collector skips admitted parent pages. No complete original shadow or per-operation source byte oracle. Complete target assembly is checked against this captured graph; independent source-selection qualification is a separate gate."
	}
	capture.Proof["images"] = capture.Images
	capture.Proof["original_writer_images"] = capture.OriginalImages
	capture.Proof["single_source_capture"] = descendantSingleCaptureEnabled()
	anchor, err := os.Open(evidence)
	if err != nil {
		return nil, err
	}
	if err := errors.Join(descendantGuardWrite(anchor, "inherited-capture-intent.json", capture.Proof),
		anchor.Close()); err != nil {
		return nil, err
	}
	if err := os.Mkdir(captureDirectory, 0700); err != nil {
		return nil, err
	}
	if err := os.Chown(captureDirectory, 165536, 165536); err != nil {
		return nil, err
	}
	if err := guard.capturePhase(true); err != nil {
		return nil, err
	}
	start := time.Now()
	args := []string{
		"RUNTIME_FORK_DESCENDANT_INHERIT_CAPTURE=1", "RUNTIME_FORK_DESCENDANT_PARENT=" + daemon.owner.Images,
		"RUNTIME_FORK_DESCENDANT_RUNTIME_ID=" + layout.ID, "RUNTIME_FORK_DESCENDANT_IMAGES=" + captureDirectory,
		"RUNTIME_FORK_PIPE_CAPTURE=0", "RUNTIME_FORK_COW_CAPTURE=0",
		"RUNTIME_FORK_LAZY_INHERIT_ORACLE=" + oracleMode,
		"CRIU_SYSBOX_LAZY_INHERIT=1", "CRIU_SYSBOX_ASYNC_TRACK=0", "CRIU_SYSBOX_PRE_DUMP=0",
		"CRIU_SYSBOX_DELTA=0", "RUNTIME_FORK_LAZY_IDENTITY=1", "RUNTIME_FORK_LAZY_WP=1",
		"RUNTIME_FORK_LAZY_INHERIT_FRESH=1", "RUNTIME_FORK_LAZY_INHERIT_SOCKET=" + daemon.inheritSocket,
		"RUNTIME_FORK_LAZY_INHERIT_DAEMON_PID=" + strconv.Itoa(daemon.command.Process.Pid),
		"/ramctl", "checkpoint-retain-lineage", "/experiment/runtime", layout.ID, captureDirectory,
	}
	output, err := criuCommand(60*time.Second, "env", args...)
	err = descendantCommandRecord("sysbox-descendant-inherited-source-recaptured", start, output, err)
	if err != nil {
		return nil, err
	}
	current, err := descendantThreads(layout)
	if err != nil || asJSON(current) != asJSON(retained) {
		return nil, fmt.Errorf("inherited source capture changed original owned threads: %v", err)
	}
	if err := guard.captureOperationReturned(); err != nil {
		return nil, err
	}
	if descendantSingleCaptureEnabled() {
		if err := guard.capturePhase(false); err != nil {
			return nil, err
		}
		capture.Proof["single_capture_transaction_completed"] = true
		capture.Proof["guard_safe_after_only_writer"] = true
	} else {
		capture.Proof["guard_transaction_remains_unsafe_until_original_writer_returns"] = true
	}
	capture.Proof["capture_elapsed_us"] = time.Since(start).Microseconds()
	if err := daemon.owner.verify(daemon, identity); err != nil {
		return nil, err
	}
	materialize := time.Now()
	if !descendantSingleCaptureEnabled() {
		if err := daemon.materialize(30 * time.Second); err != nil {
			return nil, fmt.Errorf("materialize inherited source after the same-capture original writer: %w", err)
		}
	} else {
		capture.Proof["source_pager_retained_after_capture"] = true
	}
	capture.Proof["materialize_after_same_capture_writer_us"] = time.Since(materialize).Microseconds()
	if descendantFastSourceEnabled() {
		if _, err := os.Lstat(capture.Images + "/original-writer"); !os.IsNotExist(err) {
			return nil, fmt.Errorf("ordinary collector unexpectedly created an original shadow: %v", err)
		}
		return capture, nil
	}
	deriveStart := time.Now()
	if descendantStreamOriginalEnabled() {
		stream, err := descendantPrepareStreamOriginal(capture.Images)
		capture.Proof["same_capture_stream_writer"] = stream
		capture.Proof["stream_metadata_completion_us"] = time.Since(deriveStart).Microseconds()
		record("sysbox-descendant-same-stream-original-writer-prepared", deriveStart, asJSON(stream), err)
		if err != nil {
			return nil, err
		}
		return capture, nil
	}
	derived, err := descendantDeriveInheritedOracle(capture.OriginalImages, capture.Images)
	capture.Proof["same_capture_derivation"] = derived
	capture.Proof["derivation_us"] = time.Since(deriveStart).Microseconds()
	record("sysbox-descendant-same-capture-inherited-image-derived", deriveStart, asJSON(derived), err)
	if err != nil {
		return nil, err
	}
	return capture, nil
}
