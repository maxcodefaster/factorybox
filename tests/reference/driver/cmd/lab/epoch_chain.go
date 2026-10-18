//go:build linux

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

var criuEpochNextParent bool
var criuEpochSecondForkStart time.Time
var criuEpochPreparation time.Duration

func criuEpochChainParentLink(images string) error {
	allowed := false
	for _, relative := range []string{"sysbox/images-again", "sysbox/images-again-disk",
		"sysbox/images-repeat", "sysbox/images-repeat-disk", "sysbox-copy/images",
		"sysbox-copy/images-disk", "sysbox-copy/images-again"} {
		allowed = allowed || images == filepath.Join("/experiment", relative)
	}
	if os.Getenv("RUNTIME_FORK_RICH_EPOCH_CHAIN") != "1" || !allowed {
		return fmt.Errorf("epoch parent link is outside exact owned images")
	}
	parentName := "images-base"
	if os.Getenv("RUNTIME_FORK_EPOCH_REBASE") == "1" && (filepath.Base(images) == "images-repeat" || filepath.Base(images) == "images-repeat-disk" ||
		(criuEpochNextParent && (images == "/experiment/sysbox-copy/images" || images == "/experiment/sysbox-copy/images-disk"))) {
		parentName = "images-again"
	}
	link, err := os.Readlink(filepath.Join(images, "parent"))
	if err != nil || link != "../"+parentName {
		return fmt.Errorf("epoch parent link differs: %q: %v", link, err)
	}
	parent := filepath.Join(filepath.Dir(images), parentName)
	info, err := os.Lstat(parent)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("epoch parent is not a real directory: %v", err)
	}
	expected, err := os.Stat("/experiment/sysbox/" + parentName)
	if err != nil || !os.SameFile(expected, info) {
		return fmt.Errorf("epoch parent inode differs from the exact source: %v", err)
	}
	var fs unix.Statfs_t
	if err := unix.Statfs(parent, &fs); err != nil || fs.Flags&unix.ST_RDONLY == 0 {
		return fmt.Errorf("epoch parent is writable: %v", err)
	}
	if parentName == "images-again" {
		return criuEpochChainParentLink(parent)
	}
	return nil
}

func criuEpochChainBindParent() error {
	if !criuEpochNextParent {
		return nil
	}
	const source = "/experiment/sysbox/images-again"
	const target = "/experiment/sysbox-copy/images-again"
	if err := criuEpochChainParentLink(source); err != nil {
		return err
	}
	if err := os.Mkdir(target, 0700); err != nil {
		return err
	}
	if err := criuParentReadonlyBind(source, target); err != nil {
		return err
	}
	criuParentBindings = append(criuParentBindings, target)
	return nil
}

func criuEpochChainExportLink(source, destination string) (criuRAMFile, error) {
	if err := criuEpochChainParentLink(filepath.Dir(source)); err != nil {
		return criuRAMFile{}, err
	}
	link, err := os.Readlink(source)
	if err != nil {
		return criuRAMFile{}, err
	}
	if err := os.Symlink(link, destination); err != nil {
		return criuRAMFile{}, err
	}
	if err := criuEpochChainParentLink(filepath.Dir(destination)); err != nil {
		return criuRAMFile{}, err
	}
	return criuRAMFile{Name: "parent", Link: link}, nil
}

func criuEpochChainCopies(id, images string, inners []string, before map[string]map[string]any,
	retained map[int]string) (copy criuSysboxCopy, nextImages string, resultErr error) {
	nextImages = images
	if os.Getenv("RUNTIME_FORK_RICH_EPOCH_CHAIN") != "1" || id != "source-sysbox" ||
		images != "/experiment/sysbox/images-again" || criuCopyPipelineEnabled() ||
		(os.Getenv("RUNTIME_FORK_EPOCH_REBASE") == "1" && !criuLazyWPEnabled()) || !criuSysboxInheritEnabled() {
		return copy, images, fmt.Errorf("epoch chain outside its exact frozen source")
	}
	if mode := os.Getenv("RUNTIME_FORK_EPOCH_REBASE"); mode != "0" && mode != "1" {
		return copy, images, fmt.Errorf("epoch comparison requires explicit rebase mode 0 or 1")
	}
	backedResume := os.Getenv("RUNTIME_FORK_EPOCH_BACKED_RESUME") == "1"
	if (backedResume && os.Getenv("RUNTIME_FORK_EPOCH_PIPELINE") != "0") ||
		(!backedResume && os.Getenv("RUNTIME_FORK_EPOCH_PIPELINE") != "1") {
		return copy, images, fmt.Errorf("explicit second-fork pipeline/backed-resume mode mismatch")
	}
	initial := make(map[int]string, len(retained))
	for pid, stamp := range retained {
		initial[pid] = stamp
	}
	sourceIdentity, err := criuRuntimeIdentity(id)
	if err != nil {
		return copy, images, err
	}
	rootPID, ok := sourceIdentity["init_pid"].(int)
	if !ok || initial[rootPID] == "" {
		return copy, images, fmt.Errorf("source root lacks a retained process identity")
	}
	snapshot := criuRAMSnapshotFind(images)
	if snapshot == nil {
		return copy, images, fmt.Errorf("epoch first capture has no RAM owner")
	}
	firstPayload := snapshot.Bytes
	start := time.Now()
	copy, err = criuSysboxCopyRestore(id, images)
	if err == nil {
		err = criuSysboxServiceResponses("copy-sysbox", "epoch-first", start)
	}
	if err == nil {
		err = criuSysboxTheiaReady([]string{"copy-sysbox"}, "epoch-first", start)
	}
	record("sysbox-epoch-first-child-ready", start,
		"first complete child from P1, direct services and native Theia; preceding capture is outside this component span", err)
	if err != nil {
		return copy, images, err
	}
	if err := criuSysboxCopyProof(copy, inners, before); err != nil {
		return copy, images, err
	}
	if err := criuEpochSaveFirstMetadata(copy.Images); err != nil {
		return copy, images, err
	}
	for index, inner := range inners {
		state, err := criuState("copy-sysbox", inner,
			fmt.Sprintf("/state?value=%d&ram_token=%032x", 990000+index, 990000+index))
		if err != nil || state["ram_token"] != fmt.Sprintf("%032x", 990000+index) {
			return copy, images, fmt.Errorf("first child mutation failed: %v %v", state, err)
		}
	}
	if out, err := criuRunc("exec", "copy-sysbox", "/bin/sh", "-c",
		"printf first-child-only > /workspace/epoch-first-child.txt"); err != nil {
		return copy, images, fmt.Errorf("first child file mutation: %s: %w", out, err)
	}
	if err := criuSysboxFrozenSourceProof(retained, "epoch-first-child-complete"); err != nil {
		return copy, images, err
	}
	if err := criuEpochRemoveFirstChild(images); err != nil {
		return copy, images, err
	}
	if os.Getenv("RUNTIME_FORK_EPOCH_REBASE") == "1" {
		epochStart := time.Now()
		if err := criuRichEpochRebase(id, images, retained); err != nil {
			return copy, images, err
		}
		criuEpochPreparation = time.Since(epochStart)
	}
	firstManifest, err := criuEpochParentManifest(images)
	if err != nil {
		return copy, images, err
	}
	if err := criuRegisterCommittedParent(images, firstManifest); err != nil {
		return copy, images, err
	}
	if err := os.WriteFile("/experiment/source-epoch-parent-hashes.json", []byte(asJSON(firstManifest)+"\n"), 0600); err != nil {
		return copy, images, err
	}
	if err := criuSysboxSetSourceFrozen(id, false); err != nil {
		return copy, images, err
	}
	// The first child has gone. These reads distinguish private first-child
	// writes from the original source before the next real source mutation.
	for index, inner := range inners {
		state, err := criuState(id, inner, "/state")
		if err != nil || state["ram_token"] != before[inner]["ram_token"] || state["value"] != before[inner]["value"] ||
			state["nonce"] != before[inner]["nonce"] || state["pid"] != before[inner]["pid"] {
			return copy, images, fmt.Errorf("first child changed source RAM: %v %v", state, err)
		}
		state, err = criuState(id, inner,
			fmt.Sprintf("/state?value=%d&ram_token=%032x", 995000+index, 995000+index))
		if err != nil || state["ram_token"] != fmt.Sprintf("%032x", 995000+index) {
			return copy, images, fmt.Errorf("second source mutation failed: %v %v", state, err)
		}
		before[inner] = state
	}
	if out, err := criuRunc("exec", id, "/bin/sh", "-c",
		"test ! -e /workspace/epoch-first-child.txt && printf second-source > /workspace/epoch-second-source.txt"); err != nil {
		return copy, images, fmt.Errorf("source file independence: %s: %w", out, err)
	}
	if err := criuWorkerCanary("before-second-fork"); err != nil {
		return copy, images, err
	}
	criuEpochSecondForkStart = time.Now()
	if err := criuSysboxSetSourceFrozen(id, true); err != nil {
		return copy, images, err
	}
	current, err := criuSysboxFrozenThreads()
	if err != nil {
		return copy, images, err
	}
	afterIdentity, err := criuRuntimeIdentity(id)
	if err != nil || asJSON(afterIdentity) != asJSON(sourceIdentity) || current[rootPID] != initial[rootPID] {
		return copy, images, fmt.Errorf("source runtime/root PID/start time/namespaces changed between forks: %v", err)
	}
	var exited, replaced []int
	stillPresent := 0
	for pid, stamp := range initial {
		if current[pid] == "" {
			exited = append(exited, pid)
		} else if current[pid] != stamp {
			replaced = append(replaced, pid)
		} else {
			stillPresent++
		}
	}
	clear(retained)
	for pid, stamp := range current {
		retained[pid] = stamp
	}
	record("sysbox-epoch-source-between-forks", criuEpochSecondForkStart, asJSON(map[string]any{
		"original_threads_still_present": stillPresent, "current_threads": len(current),
		"original_threads_exited_while_running": exited, "original_thread_ids_reused_while_running": replaced,
		"original_root_identity_unchanged":      sourceIdentity,
		"first_child_ram_and_files_independent": true,
		"scope":                                 "source actually thawed, first-child writes absent, source process nonce/RAM retained then RAM/file writes changed, refrozen under identical root PID/start time/namespaces. Threads may exit or be created during execution. Each complete frozen window separately retains its exact captured thread/start-time set.",
	}), nil)
	nextImages = "/experiment/sysbox/images-repeat"
	if err := criuSysboxPrepareRAMCapture(nextImages); err != nil {
		return copy, nextImages, err
	}
	daemon, err := criuInheritSourceDaemon()
	if err != nil {
		return copy, nextImages, err
	}
	criuEpochNextParent = os.Getenv("RUNTIME_FORK_EPOCH_REBASE") == "1"
	if !backedResume {
		restoreEnvironment, err := criuEpochEnableSecondPipeline()
		if err != nil {
			return copy, nextImages, err
		}
		defer func() { resultErr = errors.Join(resultErr, restoreEnvironment()) }()
	}
	if err := criuSysboxFrozenSourceProof(retained, "epoch-before-pipeline"); err != nil {
		return copy, nextImages, err
	}
	var pipeline *criuCopyPipeline
	if !backedResume {
		pipeline, err = criuSysboxStartCopyPipeline(id)
		if err != nil {
			return copy, nextImages, err
		}
		defer func() { resultErr = errors.Join(resultErr, pipeline.close()) }()
	}
	captureStart := time.Now()
	if pipeline != nil {
		pipeline.captureStart = captureStart
	}
	lazyWP := "0"
	if criuLazyWPEnabled() {
		lazyWP = "1"
	}
	out, err := criuSysboxProfileCommand("capture", nextImages, "env",
		"RUNTIME_FORK_LAZY_INHERIT_SOCKET="+daemon.inheritSocket,
		fmt.Sprintf("RUNTIME_FORK_LAZY_INHERIT_DAEMON_PID=%d", daemon.command.Process.Pid),
		"RUNTIME_FORK_LAZY_INHERIT_FRESH=1", "RUNTIME_FORK_LAZY_WP="+lazyWP,
		"/ramctl", "checkpoint-retain-source", "/experiment/runtime", id, nextImages)
	if pipeline != nil {
		pipeline.captureEnd = time.Now()
	}
	record("sysbox-epoch-next-checkpoint", captureStart, out, err)
	defer criuLazyLogRecords("sysbox-epoch-next-checkpoint-diagnostic", time.Now(), nextImages+"/dump.log", nil)
	if err != nil {
		return copy, nextImages, err
	}
	if err := criuSysboxCgroupPrefixProof(nextImages); err != nil {
		return copy, nextImages, err
	}
	if err := criuSysboxRAMCaptureProof(nextImages); err != nil {
		return copy, nextImages, err
	}
	criuEpochNextParent = os.Getenv("RUNTIME_FORK_EPOCH_REBASE") == "1"
	if err := criuEpochChainParentLink(nextImages); err != nil {
		return copy, nextImages, err
	}
	verificationStart := time.Now()
	verification, err := startEpochParentVerification(images, firstManifest)
	if err != nil {
		return copy, nextImages, err
	}
	verificationJoined := false
	joinVerification := func(cancel bool) error {
		result := verification.join(cancel)
		if !verificationJoined {
			verificationJoined = true
			record("sysbox-epoch-parent-verification-joined", verificationStart, asJSON(map[string]any{
				"parallel":       os.Getenv("RUNTIME_FORK_VERIFY_OVERLAP") == "1",
				"expected_files": len(firstManifest), "verified_files": result.Files,
				"verified_bytes": result.Bytes, "worker_us": result.Elapsed.Microseconds(),
				"read_bytes": result.ReadBytes, "kernel_certified_files": result.KernelFiles,
				"kernel_certified_bytes": result.KernelBytes,
				"finished_unix_ns":       result.FinishedNS, "cancel_requested": cancel,
				"scope": "one max-1GiB/8192-entry/64KiB-buffer verification job; logical bytes, actual hashed reads and kernel-certified page bytes reported separately; committed inode/filesystem proofs checked before/after; all metadata still hashed; actual worker joins before source thaw/services/native IDE or teardown inside readiness",
			}), result.Err)
		}
		return result.Err
	}
	defer func() { resultErr = errors.Join(resultErr, joinVerification(true)) }()
	if os.Getenv("RUNTIME_FORK_VERIFY_OVERLAP") == "0" {
		if err := joinVerification(false); err != nil {
			return copy, nextImages, err
		}
	}
	next := criuRAMSnapshotFind(nextImages)
	record("sysbox-epoch-next-payload", captureStart, asJSON(map[string]any{
		"first_capture_bytes": firstPayload, "next_capture_bytes": next.Bytes,
		"parent_files_to_verify": len(firstManifest),
		"scope":                  "actual next capture after source execution and writes; payload bytes are not full-fork latency",
	}), nil)
	if os.Getenv("RUNTIME_FORK_EPOCH_BACKED_RESUME") == "1" {
		if err := criuEpochBackedSourceResume(id, nextImages, retained); err != nil {
			return copy, nextImages, err
		}
		if err := criuVerifyBackedCommittedParent(images, firstManifest); err != nil {
			return copy, nextImages, err
		}
	}
	if os.Getenv("RUNTIME_FORK_EPOCH_PIPELINE_ABORT") == "1" {
		if pipeline == nil {
			return copy, nextImages, fmt.Errorf("abort proof requires actual second pipeline")
		}
		if err := joinVerification(false); err != nil {
			return copy, nextImages, err
		}
		if err := criuSysboxPipelineAbortProof(pipeline, id, nextImages, retained, sourceIdentity, inners, before); err != nil {
			return copy, nextImages, err
		}
		criuEpochPipelineCancelled = true
		return copy, nextImages, nil
	}
	if pipeline != nil {
		copy, err = pipeline.finish(nextImages)
	} else {
		copy, err = criuSysboxCopyRestore(id, nextImages)
	}
	verificationErr := joinVerification(err != nil)
	if err != nil || verificationErr != nil {
		return copy, nextImages, errors.Join(err, verificationErr)
	}
	if out, err := criuRunc("exec", "copy-sysbox", "/bin/sh", "-c",
		"test ! -e /workspace/epoch-first-child.txt && test \"$(cat /workspace/epoch-second-source.txt)\" = second-source"); err != nil {
		return copy, nextImages, fmt.Errorf("second fork files differ: %s: %w", out, err)
	}
	record("sysbox-epoch-next-fork-files", criuEpochSecondForkStart,
		"second child has the new source file and no first-child-only file; original full readiness/RAM/independence checks follow", nil)
	return copy, nextImages, nil
}

func criuEpochParentManifest(images string) (map[string]string, error) {
	if images != "/experiment/sysbox/images-again" {
		return nil, fmt.Errorf("epoch manifest outside exact first capture")
	}
	entries, err := criuRAMImageEntries(images, false)
	if err != nil {
		return nil, err
	}
	manifest := make(map[string]string)
	for _, entry := range entries {
		if entry.Name() == "parent" {
			manifest[entry.Name()] = "link:../images-base"
			continue
		}
		if !criuInheritParentEntry(entry.Name()) {
			continue
		}
		digest, err := criuRAMHashFile(filepath.Join(images, entry.Name()))
		if err != nil {
			return nil, err
		}
		manifest[entry.Name()] = digest
	}
	return manifest, nil
}

func criuEpochSaveFirstMetadata(images string) error {
	target := "/experiment/sysbox-epoch-first"
	if err := os.Mkdir(target, 0700); err != nil {
		return err
	}
	entries, err := os.ReadDir(images)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !(filepath.Ext(entry.Name()) == ".log" ||
			entry.Name() == "stats-restore" || entry.Name() == "stats-dump") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(images, entry.Name()))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(target, entry.Name()), data, 0600); err != nil {
			return err
		}
	}
	return nil
}

func criuEpochRemoveFirstChild(sourceImages string) (resultErr error) {
	start := time.Now()
	const child = "/experiment/sysbox-copy"
	output := map[string]any{"child": "copy-sysbox"}
	defer func() { record("sysbox-epoch-first-child-retired", start, asJSON(output), resultErr) }()
	if sourceImages != "/experiment/sysbox/images-again" {
		return fmt.Errorf("replay cleanup outside exact source")
	}
	state, err := criuRuntimeIdentity("copy-sysbox")
	if err != nil {
		return err
	}
	output["identity"] = state
	cgroupAnchor, err := epochReplayOpenCgroup()
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, cgroupAnchor.close()) }()
	output["cgroup_anchor_before_runtime_delete"] = cgroupAnchor.evidence()
	if out, err := criuRunc("delete", "--force", "copy-sysbox"); err != nil {
		return fmt.Errorf("retire exact replay child: %s: %w", out, err)
	}
	if _, err := os.Lstat("/experiment/runtime/copy-sysbox"); !os.IsNotExist(err) {
		return fmt.Errorf("replay child runtime still exists: %v", err)
	}
	for i := len(criuLazyDaemons) - 1; i >= 0; i-- {
		d := criuLazyDaemons[i]
		if filepath.Dir(d.logPath) != child+"/images" {
			continue
		}
		if !d.pollCompletion() {
			if err := d.command.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
				return err
			}
			select {
			case d.exitErr = <-d.done:
				d.finished = true
			case <-time.After(3 * time.Second):
				return fmt.Errorf("owned replay pager did not exit")
			}
		}
		criuLazyDaemons = append(criuLazyDaemons[:i], criuLazyDaemons[i+1:]...)
	}
	snapshot := criuRAMSnapshotFind(sourceImages)
	if snapshot == nil || snapshot.CopyImages != child+"/images" {
		return fmt.Errorf("replay snapshot ownership differs")
	}
	for i := len(snapshot.Bindings) - 1; i >= 0; i-- {
		path := snapshot.Bindings[i]
		if !strings.HasPrefix(path, sourceImages+"/pages-") && !strings.HasPrefix(path, child+"/images/pages-") {
			return fmt.Errorf("unexpected replay payload binding %q", path)
		}
		if err := unix.Unmount(path, 0); err != nil {
			return err
		}
	}
	snapshot.Bindings, snapshot.CopyImages = nil, ""
	for i := len(criuParentBindings) - 1; i >= 0; i-- {
		if criuParentBindings[i] == child+"/images-base" {
			if err := unix.Unmount(criuParentBindings[i], 0); err != nil {
				return err
			}
			criuParentBindings = append(criuParentBindings[:i], criuParentBindings[i+1:]...)
		}
	}
	for _, path := range []string{child + "/rootfs", child + "/lower"} {
		if err := unix.Unmount(path, 0); err != nil {
			return fmt.Errorf("unmount owned replay view %s: %w", path, err)
		}
	}
	mountinfo, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(mountinfo), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 4 && (fields[4] == child || strings.HasPrefix(fields[4], child+"/")) {
			return fmt.Errorf("owned child still has a mount: %s", line)
		}
	}
	if err := cgroupAnchor.removeEmpty(); err != nil {
		return err
	}
	output["empty_cgroup_directories_removed"] = cgroupAnchor.removed
	if err := os.RemoveAll(child); err != nil {
		return err
	}
	output["runtime_absent"], output["mounts_absent"], output["pager_reaped"] = true, true, true
	return nil
}
