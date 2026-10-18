package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
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

func descendantPauseReferences(images string) map[string]any {
	result := map[string]any{}
	processes, _ := os.ReadDir("/proc")
	seen := map[string]bool{}
	var descriptors, mounts, mappings []map[string]any
	for _, process := range processes {
		pid, err := strconv.Atoi(process.Name())
		if err != nil || pid <= 0 {
			continue
		}
		base := filepath.Join("/proc", process.Name())
		files, _ := os.ReadDir(base + "/fd")
		for _, file := range files {
			target, err := os.Readlink(base + "/fd/" + file.Name())
			if err == nil && strings.Contains(target, images) && len(descriptors) < 32 {
				descriptors = append(descriptors, map[string]any{"pid": pid, "fd": file.Name(), "target": target})
			}
		}
		if data, err := os.ReadFile(base + "/maps"); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				if strings.Contains(line, images) && len(mappings) < 32 {
					mappings = append(mappings, map[string]any{"pid": pid, "entry": line})
				}
			}
		}
		namespace, err := os.Readlink(base + "/ns/mnt")
		if err != nil || seen[namespace] {
			continue
		}
		seen[namespace] = true
		data, err := os.ReadFile(base + "/mountinfo")
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.Contains(line, images) && len(mounts) < 32 {
				mounts = append(mounts, map[string]any{"pid": pid, "namespace": namespace, "entry": line})
			}
		}
	}
	result["open_descriptors"], result["mounted_namespaces"] = descriptors, mounts
	result["mapped_files"] = mappings
	return result
}

// Complete every owned lazy reader before capturing the descendant's actual
// current RAM into a new bounded tmpfs. Stop only its validated
// process tree, verify complete disk backing, release the tmpfs and resume
// the retained runtime identity. Its sibling remains owned and frozen.
func descendantPauseResume(sibling, target descendantLayout, retainedProtection **descendantBackingLeases) (resultErr error) {
	start := time.Now()
	leasedBacking := os.Getenv("RUNTIME_FORK_DESCENDANT_LEASED_BACKING") == "1"
	var admission map[string]any
	if leasedBacking {
		if retainedProtection == nil || *retainedProtection != nil {
			return fmt.Errorf("backing lease lifetime has no exclusive owned controller")
		}
		var err error
		admission, err = descendantLeaseAdmission(target)
		if err != nil {
			return err
		}
	}
	evidence := "/experiment/descendant/pause"
	if err := os.Mkdir(evidence, 0700); err != nil {
		return err
	}
	beforeIdentity, err := criuRuntimeIdentity(target.ID)
	if err != nil {
		return err
	}
	siblingIdentity, err := criuRuntimeIdentity(sibling.ID)
	if err != nil {
		return err
	}
	before := map[string]map[string]any{}
	execs := map[string]map[string]any{}
	for _, inner := range []string{"", "alpha", "beta"} {
		before[inner], err = criuState(target.ID, inner, "/state")
		if err != nil {
			return err
		}
	}
	for _, inner := range []string{"", "alpha"} {
		execs[inner], err = criuLiveExecState(target.ID, inner, "/state")
		if err != nil {
			return err
		}
	}
	criuComposeBefore, err = criuComposeState(target.ID, "/state")
	if err != nil {
		return err
	}
	criuComposeMarker, err = criuComposeMarkerRead(target.ID)
	if err != nil {
		return err
	}
	browser, err := criuRunc("exec", target.ID, "node", "/opt/browser-state.mjs", "get")
	if err != nil || strings.TrimSpace(browser) == "" {
		return fmt.Errorf("pause current browser state: %v", err)
	}
	criuToolsBrowserNonce = strings.TrimSpace(browser)
	history, err := descendantOpenCodeHistory(target.ID)
	if err != nil {
		return err
	}
	virtual, err := criuRunc("exec", target.ID, "cat", "/proc/sys/kernel/panic")
	if err != nil {
		return err
	}
	var tokens [32]byte
	if _, err := rand.Read(tokens[:]); err != nil {
		return err
	}
	ramImages := target.Root + "/images-" + hex.EncodeToString(tokens[:16])
	diskImages := target.Root + "/images-" + hex.EncodeToString(tokens[16:])
	proof := map[string]any{
		"target": target, "sibling": sibling, "before_identity": beforeIdentity,
		"ram_images": ramImages, "disk_images": diskImages, "max_live_sandboxes": 2,
		"leased_backing": leasedBacking, "lease_admission": admission,
		"scope": "Actual current full descendant RAM captured in tmpfs, verified complete disk backing, strict tmpfs unmount before retained-identity resume; source/sibling kernel identity preserved.",
	}
	defer func() {
		proof["passed"] = resultErr == nil
		proof["elapsed_ms"] = time.Since(start).Milliseconds()
		if resultErr != nil {
			proof["error"] = resultErr.Error()
		}
		if err := os.WriteFile(evidence+"/proof.json", []byte(asJSON(proof)+"\n"), 0600); err != nil {
			resultErr = errors.Join(resultErr, err)
		}
		descendantResultRecord("sysbox-descendant-ram-releasing-pause-resume", start,
			map[string]any{"target_id": target.ID, "proof": evidence + "/proof.json", "passed": resultErr == nil,
				"timing": descendantOutcomeTiming(proof)}, resultErr)
	}()
	intent, err := os.OpenFile(evidence+"/intent.json", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := intent.WriteString(asJSON(proof) + "\n")
	if err := errors.Join(writeErr, intent.Sync(), intent.Close()); err != nil {
		return err
	}
	if err := descendantFreeze(sibling, true); err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, descendantFreeze(sibling, false)) }()
	if err := descendantFreeze(target, true); err != nil {
		return err
	}
	materializeStart := time.Now()
	if err := descendantMaterializeDaemons(30 * time.Second); err != nil {
		return err
	}
	proof["materialization_before_capture_ms"] = time.Since(materializeStart).Milliseconds()
	if err := os.Mkdir(ramImages, 0700); err != nil {
		return err
	}
	if err := unix.Mount("owned-descendant-pause", ramImages, "tmpfs",
		unix.MS_NOSUID|unix.MS_NODEV, "size=1073741824,mode=0700"); err != nil {
		return err
	}
	mounted := true
	captureCompleted := false
	backingVerified := false
	defer func() {
		if mounted {
			if captureCompleted && !backingVerified {
				proof["authoritative_ram_snapshot_retained_after_backing_failure"] = ramImages
				return
			}
			resultErr = errors.Join(resultErr, unix.Unmount(ramImages, 0))
		}
	}()
	ramDirectory, err := os.Lstat(ramImages)
	if err != nil {
		return err
	}
	checkpointStart := time.Now()
	out, err := descendantCapture(target.ID, ramImages, "checkpoint-only")
	proof["capture_and_stop_ms"] = time.Since(checkpointStart).Milliseconds()
	if err := descendantCommandRecord("sysbox-descendant-pause-capture", checkpointStart, out, err); err != nil {
		return err
	}
	captureCompleted = true
	var stat unix.Statfs_t
	if err := unix.Statfs(ramImages, &stat); err != nil || stat.Type != unix.TMPFS_MAGIC {
		return fmt.Errorf("pause snapshot was not actual tmpfs RAM: %v", err)
	}
	if events, err := os.ReadFile(target.Cgroup + "/cgroup.events"); err != nil || strings.Contains(string(events), "populated 1") {
		return fmt.Errorf("paused target still populated: %s %v", events, err)
	}
	proof["active_while_target_stopped"], err = descendantActive(sibling.ID)
	if err != nil {
		return err
	}
	expected, err := descendantSnapshotManifest(ramImages)
	if err != nil {
		return err
	}
	proof["payload_hashes"] = expected
	var allocated uint64
	if err := filepath.Walk(ramImages, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			allocated += uint64(info.Sys().(*syscall.Stat_t).Blocks) * 512
		}
		return nil
	}); err != nil || allocated == 0 {
		return fmt.Errorf("actual tmpfs allocation absent: %d %v", allocated, err)
	}
	proof["tmpfs_allocated_bytes"] = allocated
	beforeRelease, err := criuRAMMemoryRead()
	if err != nil {
		return err
	}
	proof["before_ram_release"] = beforeRelease
	backingStart := time.Now()
	if out, err := criuCommand(60*time.Second, "cp", "-a", "--reflink=auto", "--", ramImages, diskImages); err != nil {
		return fmt.Errorf("back descendant pause: %s: %w", out, err)
	}
	var protection *descendantBackingLeases
	if leasedBacking {
		protection, err = descendantProtectBacking(target, diskImages, expected)
		if err != nil {
			return err
		}
		*retainedProtection = protection
	}
	backed, err := descendantSnapshotManifestProtected(diskImages, protection)
	if err != nil || asJSON(backed) != asJSON(expected) {
		return fmt.Errorf("complete synced pause backing differs: %v", err)
	}
	if err := unix.Statfs(diskImages, &stat); err != nil || stat.Type == unix.TMPFS_MAGIC {
		return fmt.Errorf("pause backing is not file backed: %v", err)
	}
	backingVerified = true
	proof["backing_and_complete_readback_ms"] = time.Since(backingStart).Milliseconds()
	// Retire verified payloads before unmount. A namespace held only by an
	// FD can retain the old tmpfs even with no live process in that namespace.
	if err := descendantRAMPath(target, ramImages, true); err != nil {
		return err
	}
	currentDirectory, err := os.Lstat(ramImages)
	if err != nil || !os.SameFile(ramDirectory, currentDirectory) {
		return fmt.Errorf("pause RAM generation changed before retirement: %v", err)
	}
	if err := unix.Statfs(ramImages, &stat); err != nil || stat.Type != unix.TMPFS_MAGIC {
		return fmt.Errorf("pause RAM generation lost its original tmpfs: %v", err)
	}
	retiredEntries, err := criuRAMImageEntries(ramImages, false)
	if err != nil {
		return err
	}
	for _, entry := range retiredEntries {
		if err := os.Remove(filepath.Join(ramImages, entry.Name())); err != nil {
			return err
		}
	}
	directory, err := os.Open(ramImages)
	if err != nil {
		return err
	}
	if err := errors.Join(directory.Sync(), directory.Close()); err != nil {
		return err
	}
	proof["owned_ram_files_unlinked_after_verified_backing"] = true
	proof["retired_ram_payload_files"] = len(retiredEntries)
	if err := unix.Unmount(ramImages, 0); err != nil {
		return err
	}
	mounted = false
	if err := unix.Statfs(ramImages, &stat); err != nil || stat.Type == unix.TMPFS_MAGIC {
		return fmt.Errorf("released pause tmpfs remained: %v", err)
	}
	entries, err := os.ReadDir(ramImages)
	if err != nil || len(entries) != 0 {
		return fmt.Errorf("unmounted pause directory retained payloads: %v", err)
	}
	releaseStart := time.Now()
	deadline := releaseStart.Add(2 * time.Second)
	var afterRelease criuRAMMemory
	for {
		afterRelease, err = criuRAMMemoryRead()
		if err != nil {
			return err
		}
		proof["after_ram_release"] = afterRelease
		if beforeRelease.Stat["shmem"] >= afterRelease.Stat["shmem"] &&
			beforeRelease.Stat["shmem"]-afterRelease.Stat["shmem"] >= allocated {
			break
		}
		if time.Now().After(deadline) {
			proof["unreleased_snapshot_references"] = descendantPauseReferences(ramImages)
			return fmt.Errorf("actual owned shmem did not release pause snapshot: allocated=%d before=%d after=%d",
				allocated, beforeRelease.Stat["shmem"], afterRelease.Stat["shmem"])
		}
		time.Sleep(10 * time.Millisecond)
	}
	proof["ram_release_observation_wait_ms"] = time.Since(releaseStart).Milliseconds()
	proof["actual_snapshot_shmem_released_bytes"] = beforeRelease.Stat["shmem"] - afterRelease.Stat["shmem"]
	proof["memory_before_resume"] = afterRelease
	resumeStart := time.Now()
	resumePhases := map[string]int64{}
	phaseStart := resumeStart
	markResumePhase := func(name string) {
		now := time.Now()
		resumePhases[name] = now.Sub(phaseStart).Microseconds()
		phaseStart = now
	}
	proof["resume_phase_us"] = resumePhases
	lazyResume := os.Getenv("RUNTIME_FORK_DESCENDANT_LAZY_RESUME") == "1"
	proof["lazy_resume"] = lazyResume
	resumeArgs := []string{"--mount", "--propagation", "private",
		"/usr/local/sbin/sysbox-runc-experiment", "--root", "/experiment/runtime",
		"experiment-restore-existing", "--image-path", diskImages, target.ID}
	var pageDaemon *criuLazyDaemon
	if lazyResume {
		out, err = criuSysboxRestoreOwned(target.ID, diskImages, "unshare", resumeArgs...)
	} else {
		out, err = criuCommand(60*time.Second, "unshare", resumeArgs...)
	}
	proof["restore_command_ms"] = time.Since(resumeStart).Milliseconds()
	if err := descendantCommandRecord("sysbox-descendant-pause-resume-command", resumeStart, out, err); err != nil {
		return err
	}
	if lazyResume {
		pageDaemon, err = descendantPageDaemon(diskImages)
		if err != nil {
			return err
		}
		proof["lazy_daemon_pid"] = pageDaemon.command.Process.Pid
		proof["lazy_daemon_images"] = diskImages
	}
	markResumePhase("restore_command_and_pager_setup")
	if err := descendantFreeze(target, false); err != nil {
		return err
	}
	afterIdentity, err := criuRuntimeIdentity(target.ID)
	if err != nil || afterIdentity["id"] != beforeIdentity["id"] ||
		afterIdentity["host_cgroup"] != beforeIdentity["host_cgroup"] ||
		afterIdentity["init_pid"] == beforeIdentity["init_pid"] {
		return fmt.Errorf("retained descendant runtime identity/renewed process differs: %v", err)
	}
	proof["after_identity"] = afterIdentity
	markResumePhase("thaw_and_identity")
	if _, err := criuRestoredStates(target.ID, []string{"", "alpha", "beta"}, before); err != nil {
		return err
	}
	for _, inner := range []string{"", "alpha"} {
		state, err := criuLiveExecState(target.ID, inner, "/state")
		if err != nil {
			return err
		}
		for key, expected := range execs[inner] {
			if key != "ticks" && asJSON(state[key]) != asJSON(expected) {
				return fmt.Errorf("paused descendant held exec field changed: %s/%s %s", target.ID, inner, key)
			}
		}
		oldTicks, oldOK := execs[inner]["ticks"].(float64)
		newTicks, newOK := state["ticks"].(float64)
		if !oldOK || !newOK || newTicks <= oldTicks {
			return fmt.Errorf("paused descendant held exec counter did not advance: %s/%s", target.ID, inner)
		}
	}
	markResumePhase("ram_and_held_exec_assertions")
	if err := criuSysboxServiceResponses(target.ID, "descendant-resumed", resumeStart); err != nil {
		return err
	}
	markResumePhase("direct_services")
	if err := criuSysboxComposeProof(target.ID, "descendant-resumed"); err != nil {
		return err
	}
	markResumePhase("compose_cli")
	if err := criuSysboxTheiaReady([]string{target.ID}, "descendant-resumed", resumeStart); err != nil {
		return err
	}
	markResumePhase("native_theia")
	afterHistory, err := descendantOpenCodeHistory(target.ID)
	if err != nil || asJSON(afterHistory) != asJSON(history) {
		return fmt.Errorf("paused descendant OpenCode history changed: %v", err)
	}
	afterVirtual, err := criuRunc("exec", target.ID, "cat", "/proc/sys/kernel/panic")
	if err != nil || strings.TrimSpace(afterVirtual) != strings.TrimSpace(virtual) {
		return fmt.Errorf("paused descendant virtual state changed: %v", err)
	}
	markResumePhase("history_and_virtual_state")
	finalManifest, err := descendantSnapshotManifestProtected(diskImages, protection)
	if err != nil || asJSON(finalManifest) != asJSON(expected) {
		return fmt.Errorf("pause backing changed after resume: %v", err)
	}
	markResumePhase("full_backing_hash")
	siblingAfter, err := criuRuntimeIdentity(sibling.ID)
	if err != nil || asJSON(siblingAfter) != asJSON(siblingIdentity) {
		return fmt.Errorf("pause/resume changed sibling kernel identity: %v", err)
	}
	if pageDaemon != nil {
		proof["lazy_daemon_live_at_correctness_readiness"] = !pageDaemon.pollCompletion()
		if pageDaemon.finished && pageDaemon.exitErr != nil {
			return fmt.Errorf("resumed descendant pager failed before readiness: %w", pageDaemon.exitErr)
		}
	}
	memory, err := criuRAMMemoryRead()
	if err != nil {
		return err
	}
	peak, err := os.ReadFile(filepath.Join(memory.Cgroup, "memory.peak"))
	if err != nil {
		return err
	}
	peakBytes, err := strconv.ParseUint(strings.TrimSpace(string(peak)), 10, 64)
	if err != nil {
		return err
	}
	proof["memory_at_complete_resume"] = memory
	proof["probe_memory_peak_through_resume_bytes"] = peakBytes
	markResumePhase("sibling_pager_identity_and_memory")
	proof["sibling_identity_preserved"] = true
	proof["native_services_and_theia_passed"] = true
	proof["payload_hashes_unchanged_after_resume"] = true
	if protection != nil {
		proof["backing_protection"] = map[string]any{
			"images": protection.Path, "page_files": len(protection.Pages),
			"complete_initial_page_hash_bytes": protection.Bytes,
			"verification_runs":                protection.VerifyRuns,
			"scope":                            "Every backing page was completely hashed once under its read lease and compared to original capture. Later page reads are replaced by original-FD lease/inode/path checks; metadata is still hashed. Protection remains owned beyond this return until the subsequent fork and cleanup.",
		}
	}
	proof["complete_resume_readiness_us"] = time.Since(resumeStart).Microseconds()
	proof["complete_resume_readiness_ms"] = time.Since(resumeStart).Milliseconds()
	data, err := json.Marshal(map[string]any{"target_id": target.ID, "snapshot_ram_released_bytes": proof["actual_snapshot_shmem_released_bytes"]})
	if err != nil {
		return err
	}
	record("sysbox-descendant-pause-ram-released-before-resume", start, string(data), nil)
	return nil
}
