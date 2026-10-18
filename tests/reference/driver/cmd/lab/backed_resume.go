//go:build linux

package main

import (
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

var criuEpochResumedSourceIdentity map[string]any

// A threaded cgroup cannot use cgroup.kill. Stage kernel pidfds for complete
// owned thread groups while the whole source is frozen. Reject any missing,
// reused or foreign thread before delivering the first signal.
func criuEpochStopFrozenSource(retained map[int]string) (count int, resultErr error) {
	if len(retained) == 0 {
		return 0, fmt.Errorf("no frozen source thread identities")
	}
	if err := criuSysboxFrozenKernel(); err != nil {
		return 0, err
	}
	targets := make(map[int]int)
	defer func() {
		for _, fd := range targets {
			unix.Close(fd)
		}
	}()
	member := func(tid int) bool {
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", tid))
		if err != nil {
			return false
		}
		for _, line := range strings.Split(string(data), "\n") {
			if line == "0::/criu-probe-sysbox" || strings.HasPrefix(line, "0::/criu-probe-sysbox/") {
				return true
			}
		}
		return false
	}
	for tid, stamp := range retained {
		current, err := criuSysboxThreadStart(tid)
		if err != nil || current != stamp || !member(tid) {
			return 0, fmt.Errorf("source thread %d identity/membership changed: %v", tid, err)
		}
		status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", tid))
		if err != nil {
			return 0, err
		}
		tgid := 0
		for _, line := range strings.Split(string(status), "\n") {
			if strings.HasPrefix(line, "Tgid:") {
				tgid, err = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "Tgid:")))
				break
			}
		}
		if err != nil || tgid <= 0 || retained[tgid] == "" {
			return 0, fmt.Errorf("thread %d has no complete owned process leader: %v", tid, err)
		}
		if _, exists := targets[tgid]; exists {
			continue
		}
		threads, err := os.ReadDir(fmt.Sprintf("/proc/%d/task", tgid))
		if err != nil {
			return 0, err
		}
		for _, thread := range threads {
			pid, err := strconv.Atoi(thread.Name())
			if err != nil || retained[pid] == "" || !member(pid) {
				return 0, fmt.Errorf("process %d has an uncaptured/foreign thread %s", tgid, thread.Name())
			}
		}
		fd, err := unix.PidfdOpen(tgid, 0)
		if err != nil {
			return 0, fmt.Errorf("pin captured process %d: %w", tgid, err)
		}
		targets[tgid] = fd
		current, err = criuSysboxThreadStart(tgid)
		if err != nil || current != retained[tgid] {
			return 0, fmt.Errorf("pinned process %d start time changed: %v", tgid, err)
		}
	}
	current, err := criuSysboxFrozenThreads()
	if err != nil || asJSON(current) != asJSON(retained) {
		return 0, fmt.Errorf("complete frozen source changed before stop: %v", err)
	}
	for pid, fd := range targets {
		if err := unix.PidfdSendSignal(fd, unix.SIGKILL, nil, 0); err != nil && !errors.Is(err, unix.ESRCH) {
			return count, fmt.Errorf("stop pinned source process %d: %w", pid, err)
		}
		count++
	}
	return count, nil
}

// P2 was captured under the current source freeze. The first child is gone,
// the second child has not been created, and source files cannot advance.
// Preserve manager/fs registration and runtime metadata while replacing only
// the workload processes and the storage of the complete P0/P1/P2 images.
func criuEpochBackedSourceResume(id, images string, retained map[int]string) (resultErr error) {
	start := time.Now()
	proof := map[string]any{
		"images": images,
		"scope":  "explicit correctness-only suspend/resume between P2 capture and second child creation; all source processes/page daemons stop, P2/P1 tmpfs charges release, original Sysbox registrations/files remain, source resumes from the backed three-generation chain before normal second-child/full-state checks",
	}
	defer func() { record("sysbox-epoch-backed-source-resume", start, asJSON(proof), resultErr) }()
	if os.Getenv("RUNTIME_FORK_EPOCH_BACKED_RESUME") != "1" || id != "source-sysbox" ||
		images != "/experiment/sysbox/images-repeat" || !criuEpochNextParent ||
		os.Getenv("RUNTIME_FORK_EPOCH_REBASE") != "1" || criuCopyPipelineEnabled() {
		return fmt.Errorf("backed resume outside the exact source-only chain")
	}
	if _, err := os.Lstat("/experiment/runtime/copy-sysbox"); !os.IsNotExist(err) {
		return fmt.Errorf("second child already exists: %v", err)
	}
	if _, err := os.Lstat("/experiment/sysbox-copy"); !os.IsNotExist(err) {
		return fmt.Errorf("first child's files/mounts were not retired: %v", err)
	}
	if err := criuSysboxFrozenSourceProof(retained, "backed-resume-before-stop"); err != nil {
		return err
	}
	identity, err := criuRuntimeIdentity(id)
	if err != nil {
		return err
	}
	oldPID, ok := identity["init_pid"].(int)
	if !ok || retained[oldPID] == "" {
		return fmt.Errorf("source root has no exact captured PID/start time")
	}
	proof["old_source_identity"] = identity
	for _, path := range []string{images + "/inventory.img", images + "/pstree.img",
		images + "/runtime-config.json", "/experiment/runtime/source-sysbox/state.json"} {
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			return fmt.Errorf("required retained resume metadata %s: %v", path, err)
		}
	}
	for _, path := range []string{"/experiment/sysbox/images-again", images} {
		snapshot := criuRAMSnapshotFind(path)
		if snapshot == nil || snapshot.CopyImages != "" || snapshot.Bytes == 0 {
			return fmt.Errorf("source-only snapshot ownership differs for %s", path)
		}
	}
	before, err := criuRAMMemoryRead()
	if err != nil {
		return err
	}
	proof["before_suspend"] = before
	journal, err := os.OpenFile("/experiment/source-epoch-resume-intent.json", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := journal.WriteString(asJSON(proof) + "\n")
	syncErr := journal.Sync()
	journal.Close()
	if err := errors.Join(writeErr, syncErr); err != nil {
		return err
	}
	// The exact owned cgroup contains every captured source exec root too.
	// Do not destroy the runtime or unregister its Sysbox manager/fs state.
	stoppedGroups, err := criuEpochStopFrozenSource(retained)
	if err != nil {
		return err
	}
	proof["stop_method"] = "all frozen source thread identities validated before SIGKILL through staged process pidfds"
	proof["staged_process_groups"] = stoppedGroups
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, err := os.ReadFile(criuOwnedSourceCgroup + "/cgroup.events")
		if err != nil {
			return err
		}
		events, err := criuRAMCounters(string(data))
		if err != nil {
			return err
		}
		if events["populated"] == 0 {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("source cgroup is still populated after owned suspend")
		}
		time.Sleep(10 * time.Millisecond)
	}
	for pid := range retained {
		status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || !strings.Contains(string(status), "State:\tZ") ||
			strings.Contains(string(status), "VmRSS:") {
			return fmt.Errorf("captured source thread %d retains live RAM: %v", pid, err)
		}
	}
	// Sysbox refreshState checks the freezer before checking whether init
	// exited. Once every task is gone, clear only this empty group's freeze
	// so the retained runtime correctly reports Stopped for the restore.
	if err := os.WriteFile(criuOwnedSourceCgroup+"/cgroup.freeze", []byte("0"), 0600); err != nil {
		return err
	}
	stateJSON, err := criuRunc("state", id)
	if err != nil {
		return fmt.Errorf("retained stopped runtime: %s: %w", stateJSON, err)
	}
	var stopped struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(stateJSON), &stopped); err != nil ||
		stopped.ID != id || stopped.Status != "stopped" {
		return fmt.Errorf("source runtime was destroyed or is not stopped: %s: %v", stateJSON, err)
	}
	record("sysbox-epoch-backed-source-stopped", start,
		"all captured source TIDs absent or zero-RSS zombies; owned cgroup populated=0; source-sysbox runtime retained and stopped; no manager/Fs unregister", nil)
	criuSysboxLazyCleanup()
	if len(criuLazyDaemons) != 0 {
		return fmt.Errorf("owned page daemon list was not retired")
	}
	err = criuEpochRAMCleanup()
	if err != nil {
		return err
	}
	after, err := criuRAMMemoryRead()
	if err != nil {
		return err
	}
	proof["after_ram_release"] = after
	var retainedAllocated uint64
	for _, snapshot := range criuRAMSnapshots {
		if snapshot.Images == "/experiment/sysbox/images-again" || snapshot.Images == images {
			retainedAllocated += snapshot.Allocated
			if snapshot.Mounted {
				return fmt.Errorf("old generation still has a live tmpfs")
			}
		}
	}
	if before.Stat["shmem"] < after.Stat["shmem"] ||
		before.Stat["shmem"]-after.Stat["shmem"] < retainedAllocated {
		return fmt.Errorf("both retained generation RAM charges were not released")
	}
	proof["released_p1_p2_allocated_bytes"] = retainedAllocated
	if err := criuSysboxParentManifestProof(); err != nil {
		return err
	}
	// Use the existing restore command's supported source path. P0 remains
	// pinned at images-base; this writable metadata view points at backed P2.
	// P2's parent still resolves images-again, which resolves images-base.
	const alias = "/experiment/sysbox/images"
	for index := len(criuParentBindings) - 1; index >= 0; index-- {
		if criuParentBindings[index] != alias {
			continue
		}
		if err := unix.Unmount(alias, 0); err != nil {
			return err
		}
		criuParentBindings = append(criuParentBindings[:index], criuParentBindings[index+1:]...)
	}
	if err := unix.Mount(images, alias, "", unix.MS_BIND, ""); err != nil {
		return err
	}
	criuParentBindings = append(criuParentBindings, alias)
	sourceInfo, err := os.Stat(images)
	if err != nil {
		return err
	}
	aliasInfo, err := os.Stat(alias)
	if err != nil || !os.SameFile(sourceInfo, aliasInfo) {
		return fmt.Errorf("backed P2 restore view has different identity: %v", err)
	}
	proof["restore_view"] = alias
	// The old overlay's work directory belongs to the previous process
	// generation. Retain its inode under an owned historical name and give
	// the existing rootfs helper a fresh work directory at its fixed path.
	const work = "/experiment/sysbox/restore-work-images"
	const priorWork = "/experiment/sysbox/restore-work-images-before-backed-resume"
	info, err := os.Lstat(work)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("previous owned restore work directory: %v", err)
	}
	if _, err := os.Lstat(priorWork); !os.IsNotExist(err) {
		return fmt.Errorf("previous resume work target exists: %v", err)
	}
	if err := os.Rename(work, priorWork); err != nil {
		return err
	}
	for _, path := range []string{"/experiment/sysbox/lazy-inherit.socket", "/experiment/sysbox/lazy-epoch.socket"} {
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0600 {
			return fmt.Errorf("old owned source endpoint %s: %v", path, err)
		}
		if info.Sys().(*syscall.Stat_t).Uid != uint32(os.Geteuid()) {
			return fmt.Errorf("source endpoint has different owner")
		}
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	if err := os.WriteFile(criuOwnedSourceCgroup+"/cgroup.freeze", []byte("0"), 0600); err != nil {
		return err
	}
	restoreStart := time.Now()
	out, err := criuSysboxRestore(alias, "unshare", "--mount", "--propagation", "private",
		"/usr/local/sbin/sysbox-runc-experiment", "--root", "/experiment/runtime",
		"experiment-restore-existing", "--image-path", alias, id)
	record("sysbox-epoch-backed-source-resume-command", restoreStart, out, err)
	if err != nil {
		return err
	}
	if err := criuSysboxSetSourceFrozen(id, true); err != nil {
		return err
	}
	current, err := criuSysboxFrozenThreads()
	if err != nil {
		return err
	}
	restoredIdentity, err := criuRuntimeIdentity(id)
	if err != nil {
		return err
	}
	newPID, ok := restoredIdentity["init_pid"].(int)
	if !ok || newPID == oldPID || current[newPID] == "" {
		return fmt.Errorf("source did not resume as a fresh process tree")
	}
	clear(retained)
	for pid, stamp := range current {
		retained[pid] = stamp
	}
	criuEpochResumedSourceIdentity = restoredIdentity
	proof["resumed_source_identity"] = restoredIdentity
	proof["resumed_threads"] = current
	proof["performance_sample"] = false
	if err := os.WriteFile("/experiment/source-epoch-resume-result.json", []byte(asJSON(proof)+"\n"), 0600); err != nil {
		return err
	}
	return nil
}

func criuEpochResumeChildParentAbsent(path string) bool {
	if os.Getenv("RUNTIME_FORK_EPOCH_BACKED_RESUME") != "1" || path != "/experiment/sysbox-copy/images-again" {
		return false
	}
	if _, err := os.Lstat("/experiment/runtime/copy-sysbox"); !os.IsNotExist(err) {
		return false
	}
	_, err := os.Lstat(filepath.Dir(path))
	return os.IsNotExist(err)
}

func criuEpochBackedImagesUnchanged() (resultErr error) {
	start := time.Now()
	verified := make(map[string]int)
	defer func() {
		record("sysbox-epoch-backed-chain-images-unchanged", start, asJSON(map[string]any{
			"verified_immutable_files": verified,
			"scope":                    "full P1/P2 image and page payload hashes still match synced backing after source resume, second-child restore and all original final RAM/files/browser/agent/IDE/lifecycle mutations; logs/statistics are separate",
		}), resultErr)
	}()
	for _, name := range []string{"images-again", "images-repeat"} {
		images := "/experiment/sysbox/" + name
		data, err := os.ReadFile("/experiment/ram-capture-" + name + "-backing.json")
		if err != nil {
			return err
		}
		var manifest map[string]criuRAMFile
		if err := json.Unmarshal(data, &manifest); err != nil || len(manifest) == 0 {
			return fmt.Errorf("invalid backed image manifest for %s: %v", name, err)
		}
		if err := criuEpochChainParentLink(images); err != nil {
			return err
		}
		for file, expected := range manifest {
			if !criuInheritParentEntry(file) {
				continue
			}
			if filepath.Base(file) != file || expected.Name != file || len(expected.SHA256) != 64 {
				return fmt.Errorf("invalid immutable image entry %q", file)
			}
			path := filepath.Join(images, file)
			info, err := os.Lstat(path)
			if err != nil || !info.Mode().IsRegular() || info.Size() != expected.Bytes {
				return fmt.Errorf("backed image shape changed %s: %v", path, err)
			}
			digest, err := criuRAMHashFile(path)
			if err != nil || digest != expected.SHA256 {
				return fmt.Errorf("backed image bytes changed %s: %v", path, err)
			}
			verified[name]++
		}
		if verified[name] == 0 {
			return fmt.Errorf("no immutable backed image files for %s", name)
		}
	}
	return nil
}
