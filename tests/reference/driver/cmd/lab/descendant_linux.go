package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

func descendantFreeze(layout descendantLayout, frozen bool) error {
	value, want := "0", uint64(0)
	if frozen {
		value, want = "1", 1
	}
	if err := os.WriteFile(layout.Cgroup+"/cgroup.freeze", []byte(value), 0644); err != nil {
		return err
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		data, err := os.ReadFile(layout.Cgroup + "/cgroup.events")
		events, parseErr := criuRAMCounters(string(data))
		if err == nil && parseErr == nil && events["populated"] == 1 && events["frozen"] == want {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("owned lineage freeze not confirmed: %s %v %v", data, err, parseErr)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func descendantThreads(layout descendantLayout) (map[int]string, error) {
	threads := map[int]string{}
	err := filepath.Walk(layout.Cgroup, func(path string, info os.FileInfo, err error) error {
		if err != nil || !info.IsDir() {
			return err
		}
		data, err := os.ReadFile(filepath.Join(path, "cgroup.threads"))
		if err != nil {
			return err
		}
		for _, field := range strings.Fields(string(data)) {
			pid, err := strconv.Atoi(field)
			if err != nil || pid <= 0 || pid == os.Getpid() {
				return fmt.Errorf("invalid owned lineage thread")
			}
			start, err := criuSysboxThreadStart(pid)
			if err != nil {
				return err
			}
			threads[pid] = start
		}
		return nil
	})
	return threads, err
}

func descendantSnapshotManifest(images string) (map[string]string, error) {
	return descendantSnapshotManifestProtected(images, nil)
}

func descendantSnapshotManifestProtected(images string, protection *descendantBackingLeases) (_ map[string]string, resultErr error) {
	start := time.Now()
	var hashedBytes, certifiedBytes int64
	var certifiedFiles int
	if protection != nil {
		if protection.Path != images {
			return nil, fmt.Errorf("backing certificate belongs to a different image generation")
		}
		if err := protection.verify(); err != nil {
			return nil, err
		}
		defer func() {
			resultErr = errors.Join(resultErr, protection.verify())
			protection.VerifyRuns = append(protection.VerifyRuns, map[string]any{
				"elapsed_us": time.Since(start).Microseconds(), "metadata_hashed_bytes": hashedBytes,
				"kernel_certified_page_bytes": certifiedBytes, "kernel_certified_page_files": certifiedFiles,
				"passed": resultErr == nil,
			})
		}()
	}
	entries, err := os.ReadDir(images)
	if err != nil {
		return nil, err
	}
	manifest := map[string]string{}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".img") && entry.Name() != "runtime-config.json" && entry.Name() != "descriptors.json" {
			continue
		}
		if !entry.Type().IsRegular() {
			return nil, fmt.Errorf("lineage snapshot payload is not a regular file")
		}
		file, err := os.OpenFile(filepath.Join(images, entry.Name()), os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
		if err != nil {
			return nil, err
		}
		var sum string
		var hashErr error
		if protection != nil && criuRAMPageName.MatchString(entry.Name()) {
			sum, hashErr = protection.page(entry.Name(), file)
			if hashErr == nil {
				certifiedBytes += protection.Pages[entry.Name()].Saved.Size
				certifiedFiles++
			}
		} else {
			sum, hashErr = criuRAMHashFile(file.Name())
			if info, err := file.Stat(); err == nil {
				hashedBytes += info.Size()
			}
		}
		syncErr := file.Sync()
		evictErr := unix.Fadvise(int(file.Fd()), 0, 0, unix.FADV_DONTNEED)
		closeErr := file.Close()
		if err := errors.Join(hashErr, syncErr, evictErr, closeErr); err != nil {
			return nil, err
		}
		manifest[entry.Name()] = sum
	}
	if len(manifest) < 4 || manifest["inventory.img"] == "" || manifest["pstree.img"] == "" {
		return nil, fmt.Errorf("lineage snapshot lacks complete runtime metadata")
	}
	return manifest, nil
}

func descendantCapture(id, images, operation string) (string, error) {
	// Observe mode keeps the original complete writer. Deferred mode must
	// publish bounded readonly RAM with shared immutable logical page files.
	cow := "0"
	pipe := "0"
	if os.Getenv("RUNTIME_FORK_DESCENDANT_COW") == "1" && operation == "checkpoint-retain-lineage" {
		layout, err := descendantOwnedLayout(id)
		if err != nil || (id != "copy-sysbox" && !descendantID.MatchString(id)) {
			return "", fmt.Errorf("COW capture requires the owned forked/restored source")
		}
		if err := descendantRAMPath(layout, images, true); err != nil {
			return "", err
		}
		mode := os.Getenv("RUNTIME_FORK_DESCENDANT_PIPE")
		deferred := mode == "defer-lazy"
		if os.Getenv("RUNTIME_FORK_DESCENDANT_GUARD") != "1" ||
			(os.Getenv("RUNTIME_FORK_DESCENDANT_RAM") == "1" && !deferred) ||
			(deferred && os.Getenv("RUNTIME_FORK_DESCENDANT_RAM") != "1") ||
			(os.Getenv("RUNTIME_FORK_DESCENDANT_REFLINK") == "1" && mode != "observe") {
			return "", fmt.Errorf("COW capture requires guarded original file capture or bounded deferred RAM")
		}
		cow = "1"
		if mode == "observe" || deferred {
			pipe = mode
		} else if mode != "" && mode != "0" {
			return "", fmt.Errorf("unknown descendant retained capture mode")
		}
	}
	args := []string{"RUNTIME_FORK_PIPE_CAPTURE=" + pipe, "RUNTIME_FORK_COW_CAPTURE=" + cow,
		"RUNTIME_FORK_DESCENDANT_INHERIT_CAPTURE=0",
		"RUNTIME_FORK_LAZY_INHERIT_ORACLE=0",
		"CRIU_SYSBOX_LAZY_INHERIT=0", "CRIU_SYSBOX_ASYNC_TRACK=0", "CRIU_SYSBOX_PRE_DUMP=0",
		"CRIU_SYSBOX_DELTA=0", "RUNTIME_FORK_LAZY_WP=0", "RUNTIME_FORK_LAZY_IDENTITY=0",
		"/ramctl", operation, "/experiment/runtime", id, images}
	return criuCommand(60*time.Second, "env", args...)
}

var descendantEvidenceRoot = "/experiment/descendant"

// Stream compact observations with the outcome. A later Pod deadline may
// remove the evidence volume before its proof files can be transferred.
// This never substitutes for complete lifecycle verification.
func descendantOutcomeTiming(proof map[string]any) map[string]any {
	result := map[string]any{"diagnostic_only": true, "requires_complete_lifecycle_verification": true}
	for _, key := range []string{
		"complete_correctness_readiness_us", "complete_resume_readiness_us",
		"elapsed_us", "elapsed_ms", "source_capture_count", "coalesced_snapshot_hashes",
		"materialization_before_capture_ms", "capture_and_stop_ms", "backing_and_complete_readback_ms",
		"restore_command_ms", "actual_snapshot_shmem_released_bytes", "tmpfs_allocated_bytes",
	} {
		if value, exists := proof[key]; exists {
			result[key] = value
		}
	}
	if phases, ok := proof["phase_us"].(map[string]int64); ok && len(phases) <= 64 {
		safe := map[string]int64{}
		for name, value := range phases {
			if len(name) > 96 || value < 0 {
				result["phase_timing_unavailable"] = true
				return result
			}
			safe[name] = value
		}
		result["phase_us"] = safe
	}
	return result
}

func descendantResultRecord(name string, start time.Time, summary map[string]any, resultErr error) {
	if resultErr != nil {
		message := resultErr.Error()
		summary["error_bytes"] = len(message)
		summary["error_truncated"] = len(message) > 4096
		if len(message) > 4096 {
			message = message[:4096]
		}
		summary["error"] = message
		record(name, start, asJSON(summary), fmt.Errorf("owned descendant outcome failed; bounded cause is included"))
		return
	}
	record(name, start, asJSON(summary), nil)
}

func descendantCommandRecord(name string, start time.Time, output string, commandErr error) error {
	path := filepath.Join(descendantEvidenceRoot, name+".log")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err == nil {
		_, writeErr := file.WriteString(output)
		err = errors.Join(writeErr, file.Sync(), file.Close())
	}
	resultErr := errors.Join(commandErr, err)
	evidence := map[string]any{"stdout_path": path, "stdout_bytes": len(output)}
	var recordErr error
	if resultErr != nil {
		recordErr = fmt.Errorf("owned descendant command failed; complete output retained at %s", path)
		const tailLimit = 4096
		tail := output
		if len(tail) > tailLimit {
			tail = tail[len(tail)-tailLimit:]
		}
		evidence["output_saved"] = err == nil
		evidence["output_tail"] = tail
		evidence["output_tail_truncated"] = len(output) > len(tail)
		evidence["command_error"] = resultErr.Error()
		if err != nil {
			recordErr = fmt.Errorf("owned descendant command failed and complete output could not be saved at %s", path)
		}
	}
	record(name, start, asJSON(evidence), recordErr)
	return resultErr
}

func descendantActive(expected ...string) (map[string]string, error) {
	out, err := criuRunc("list", "--format", "json")
	if err != nil {
		return nil, fmt.Errorf("list owned lineage runtimes: %s: %w", out, err)
	}
	var values []struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(out), &values); err != nil {
		return nil, err
	}
	active := map[string]string{}
	for _, value := range values {
		if value.Status == "running" || value.Status == "paused" {
			active[value.ID] = value.Status
		}
	}
	if len(active) != len(expected) || len(active) > 2 {
		return active, fmt.Errorf("owned live sandbox count differs: %v", active)
	}
	for _, id := range expected {
		if active[id] == "" {
			return active, fmt.Errorf("expected live lineage sandbox absent: %s", id)
		}
	}
	return active, nil
}

func descendantResourceProof(source, target descendantLayout) error {
	start := time.Now()
	values := map[string]any{}
	for branch, layout := range []descendantLayout{source, target} {
		outerLimit := strconv.Itoa(416 + branch*32)
		if err := os.WriteFile(layout.Cgroup+"/pids.max", []byte(outerLimit), 0644); err != nil {
			return err
		}
		innerLimit := strconv.Itoa(72 + branch*8)
		if out, err := criuRunc("exec", layout.ID, "docker", "update", "--pids-limit="+innerLimit, "alpha"); err != nil {
			return fmt.Errorf("independent descendant inner resource update: %s: %w", out, err)
		}
	}
	for branch, layout := range []descendantLayout{source, target} {
		for _, inner := range []string{"", "alpha"} {
			expected := strconv.Itoa(416 + branch*32)
			if inner != "" {
				expected = strconv.Itoa(72 + branch*8)
			}
			for _, exec := range []bool{false, true} {
				var state map[string]any
				var err error
				if exec {
					state, err = criuLiveExecState(layout.ID, inner, "/state")
				} else {
					state, err = criuState(layout.ID, inner, "/state")
				}
				if err != nil || state["pids_limit"] != expected || state["cgroup_fd_pids_limit"] != expected {
					return fmt.Errorf("descendant held cgroup FD crossed branches: %s/%s exec=%t expected=%s actual=%v error=%v",
						layout.ID, inner, exec, expected, state, err)
				}
				values[fmt.Sprintf("%s/%s/exec=%t", layout.ID, inner, exec)] = map[string]any{
					"pids_limit": state["pids_limit"], "cgroup_fd_pids_limit": state["cgroup_fd_pids_limit"],
					"cpu_max": state["cpu_max"], "cgroup_fd_cpu_max": state["cgroup_fd_cpu_max"],
				}
			}
		}
	}
	record("sysbox-descendant-independent-resource-fds", start, asJSON(values), nil)
	return nil
}

func descendantVirtualProof(source, target string) error {
	start := time.Now()
	before, err := os.ReadFile("/proc/sys/kernel/panic")
	if err != nil {
		return err
	}
	for branch, id := range []string{source, target} {
		value := strconv.Itoa(37 + 6*branch)
		if out, err := criuRunc("exec", id, "/bin/sh", "-c", "printf '"+value+"' > /proc/sys/kernel/panic"); err != nil {
			return fmt.Errorf("descendant virtual write: %s: %w", out, err)
		}
	}
	for branch, id := range []string{source, target} {
		out, err := criuRunc("exec", id, "cat", "/proc/sys/kernel/panic")
		if err != nil || strings.TrimSpace(out) != strconv.Itoa(37+6*branch) {
			return fmt.Errorf("descendant virtual values crossed branches: %s %q %v", id, out, err)
		}
	}
	after, err := os.ReadFile("/proc/sys/kernel/panic")
	if err != nil || string(after) != string(before) {
		return fmt.Errorf("descendant virtual write changed controller state: %v", err)
	}
	record("sysbox-descendant-independent-virtual-state", start, "two distinct Sysbox values; controller value unchanged", nil)
	return nil
}

func descendantOpenCodeHistory(id string) (map[string]any, error) {
	return criuOpenCodeState(id, "const active=await c.session.active();if(Object.keys(active).length)throw Error('Agent active during descendant snapshot');const rows=await c.session.list({limit:100});console.log(JSON.stringify({sessions:rows.data.map(({id,title})=>({id,title})).sort((a,b)=>a.id.localeCompare(b.id)),active}));")
}

func criuDescendantForkProof() (resultErr error) {
	if os.Getenv("RUNTIME_FORK_DESCENDANT") != "1" || criuRoot != "/experiment" ||
		os.Getenv("RUNTIME_FORK_EPOCH_BACKED_RESUME") == "1" {
		return fmt.Errorf("descendant proof is outside its owned full live-fork case")
	}
	postResumeFork := os.Getenv("RUNTIME_FORK_DESCENDANT_POST_RESUME_FORK") == "1"
	switch os.Getenv("RUNTIME_FORK_COALESCED_HASHES") {
	case "", "0":
	case "1":
		if !descendantFastSourceEnabled() || !descendantTargetCertificateEnabled() {
			return fmt.Errorf("coalesced snapshot hash qualification requires the ordinary collector and retained target certificates")
		}
	default:
		return fmt.Errorf("coalesced snapshot hash mode must be 0 or 1")
	}
	switch os.Getenv("RUNTIME_FORK_DESCENDANT_PRESSURE") {
	case "", "0", "1":
	default:
		return fmt.Errorf("descendant resource observation mode must be 0 or 1")
	}
	switch os.Getenv("RUNTIME_FORK_DESCENDANT_GRAPH_PROFILE") {
	case "", "0":
	case "1":
		if os.Getenv("RUNTIME_FORK_DESCENDANT_PRESSURE") != "1" || !descendantTargetCertificateEnabled() {
			return fmt.Errorf("target CPU profiling requires resource observations and retained target certificates")
		}
	default:
		return fmt.Errorf("target CPU profile mode must be 0 or 1")
	}
	switch os.Getenv("RUNTIME_FORK_DESCENDANT_COMPLETE_RAM") {
	case "", "0":
	case "1":
		if descendantSingleCaptureEnabled() || descendantInheritProofEnabled() ||
			os.Getenv("RUNTIME_FORK_DESCENDANT_RAM") != "1" ||
			os.Getenv("RUNTIME_FORK_DESCENDANT_GUARD") != "1" ||
			os.Getenv("RUNTIME_FORK_DESCENDANT_COW") == "1" ||
			(os.Getenv("RUNTIME_FORK_DESCENDANT_PIPE") != "" && os.Getenv("RUNTIME_FORK_DESCENDANT_PIPE") != "0") ||
			descendantInheritedTargetEnabled() || descendantFastSourceEnabled() || descendantStreamOriginalEnabled() {
			return fmt.Errorf("complete RAM capture requires the guarded ordinary complete writer without inheritance/COW/pipe capture")
		}
	default:
		return fmt.Errorf("complete RAM capture mode must be 0 or 1")
	}
	switch os.Getenv("RUNTIME_FORK_DESCENDANT_SOURCE_SYNC") {
	case "", "0":
	case "1":
		if !descendantSourceCertificateEnabled() {
			return fmt.Errorf("source capture sync requires continuous source inode certificates")
		}
	default:
		return fmt.Errorf("source capture sync mode must be 0 or 1")
	}
	switch os.Getenv("RUNTIME_FORK_DESCENDANT_SOURCE_CERTIFICATE") {
	case "", "0":
	case "1":
		if !descendantTargetCertificateEnabled() || !descendantFastSourceEnabled() {
			return fmt.Errorf("source inode certificates require the continuously leased ordinary target")
		}
	default:
		return fmt.Errorf("source certificate mode must be 0 or 1")
	}
	switch os.Getenv("RUNTIME_FORK_DESCENDANT_TARGET_CERTIFICATE") {
	case "", "0":
	case "1":
		if !descendantFastSourceEnabled() || !descendantSingleCaptureEnabled() ||
			!descendantDirectRangeEnabled() || !descendantPageInputLeaseOnlyEnabled() {
			return fmt.Errorf("retained target certificates require ordinary single direct leased-input collection")
		}
	default:
		return fmt.Errorf("target certificate mode must be 0 or 1")
	}
	switch os.Getenv("RUNTIME_FORK_DESCENDANT_SINGLE_CAPTURE") {
	case "", "0":
	case "1":
		if (descendantStreamOriginalEnabled() == descendantFastSourceEnabled()) || !descendantInheritedTargetEnabled() ||
			(os.Getenv("RUNTIME_FORK_DESCENDANT_COW") != "" && os.Getenv("RUNTIME_FORK_DESCENDANT_COW") != "0") ||
			(os.Getenv("RUNTIME_FORK_DESCENDANT_PIPE") != "" && os.Getenv("RUNTIME_FORK_DESCENDANT_PIPE") != "0") ||
			os.Getenv("RUNTIME_FORK_CAPTURE_CRASH_PROOF") == "1" || os.Getenv("RUNTIME_FORK_CONTROLLER_LOSS_PROOF") == "1" {
			return fmt.Errorf("single source capture requires exactly one stream-oracle or fast-collector policy and no COW/pipe/crash control")
		}
	default:
		return fmt.Errorf("single source capture mode must be 0 or 1")
	}
	switch os.Getenv("RUNTIME_FORK_DESCENDANT_FAST_SOURCE") {
	case "", "0":
	case "1":
		if !descendantSingleCaptureEnabled() || descendantStreamOriginalEnabled() ||
			!descendantInheritProofEnabled() || !descendantInheritedTargetEnabled() ||
			!descendantDirectRangeEnabled() {
			return fmt.Errorf("ordinary fast collection requires one direct complete inherited target and no stream shadow")
		}
	default:
		return fmt.Errorf("ordinary fast collector mode must be 0 or 1")
	}
	switch os.Getenv("RUNTIME_FORK_DESCENDANT_STREAM_ORACLE") {
	case "", "0":
	case "1":
		if !descendantInheritedTargetEnabled() || !descendantInheritProofEnabled() {
			return fmt.Errorf("stream original writer requires actual inherited targets and its byte oracle")
		}
	default:
		return fmt.Errorf("stream original writer mode must be 0 or 1")
	}
	switch os.Getenv("RUNTIME_FORK_DESCENDANT_PAGE_INPUT_LEASE_ONLY") {
	case "", "0":
	case "1":
		if !descendantDirectRangeEnabled() || !descendantInheritedTargetEnabled() {
			return fmt.Errorf("lease-only page inputs require direct complete inherited targets")
		}
	default:
		return fmt.Errorf("page input lease-only mode must be 0 or 1")
	}
	switch os.Getenv("RUNTIME_FORK_DESCENDANT_DIRECT_RANGE") {
	case "", "0":
	case "1":
		if !descendantInheritedTargetEnabled() {
			return fmt.Errorf("direct range assembly requires complete inherited targets")
		}
	default:
		return fmt.Errorf("direct range target mode must be 0 or 1")
	}
	switch os.Getenv("RUNTIME_FORK_DESCENDANT_INHERITED_TARGET") {
	case "", "0":
	case "1":
		if !descendantInheritProofEnabled() || os.Getenv("RUNTIME_FORK_DESCENDANT_LAZY") != "1" ||
			os.Getenv("RUNTIME_FORK_DESCENDANT_GUARD") != "1" ||
			os.Getenv("RUNTIME_FORK_DESCENDANT_RAM") == "1" || os.Getenv("RUNTIME_FORK_DESCENDANT_REFLINK") == "1" {
			return fmt.Errorf("inherited targets require their guarded same-capture original writer")
		}
		if !descendantSingleCaptureEnabled() &&
			(os.Getenv("RUNTIME_FORK_DESCENDANT_COW") != "1" || os.Getenv("RUNTIME_FORK_DESCENDANT_PIPE") != "observe") {
			return fmt.Errorf("reference inherited targets require the separate complete COW control")
		}
	default:
		return fmt.Errorf("descendant inherited target mode must be 0 or 1")
	}
	if postResumeFork && os.Getenv("RUNTIME_FORK_DESCENDANT_PAUSE") != "1" {
		return fmt.Errorf("post-resume descendant fork requires RAM-releasing pause/resume")
	}
	switch os.Getenv("RUNTIME_FORK_DESCENDANT_LAZY") {
	case "", "0":
	case "1":
		if os.Getenv("CRIU_SYSBOX_LAZY") != "1" {
			return fmt.Errorf("lazy descendant restore requires the existing lazy-page runtime")
		}
	default:
		return fmt.Errorf("descendant lazy mode must be 0 or 1")
	}
	switch os.Getenv("RUNTIME_FORK_DESCENDANT_LAZY_RESUME") {
	case "", "0":
	case "1":
		if os.Getenv("RUNTIME_FORK_DESCENDANT_PAUSE") != "1" || os.Getenv("CRIU_SYSBOX_LAZY") != "1" {
			return fmt.Errorf("lazy descendant resume requires pause and the existing lazy-page runtime")
		}
	default:
		return fmt.Errorf("descendant lazy resume mode must be 0 or 1")
	}
	switch os.Getenv("RUNTIME_FORK_DESCENDANT_LEASED_BACKING") {
	case "", "0":
	case "1":
		if os.Getenv("RUNTIME_FORK_DESCENDANT_LAZY_RESUME") != "1" || !postResumeFork {
			return fmt.Errorf("leased backing proof requires lazy resume and its following full fork")
		}
	default:
		return fmt.Errorf("descendant leased backing mode must be 0 or 1")
	}
	switch os.Getenv("RUNTIME_FORK_DESCENDANT_RAM") {
	case "", "0":
	case "1":
		if os.Getenv("RUNTIME_FORK_DESCENDANT_LAZY") != "1" ||
			os.Getenv("RUNTIME_FORK_DESCENDANT_LEASED_BACKING") == "1" {
			return fmt.Errorf("RAM descendant proof requires lazy descendants and reference backing")
		}
	default:
		return fmt.Errorf("descendant RAM mode must be 0 or 1")
	}
	depth := 1
	if value := os.Getenv("RUNTIME_FORK_DESCENDANT_DEPTH"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > 2 {
			return fmt.Errorf("descendant depth must be1 or2 inside the fixed proof deadline")
		}
		depth = parsed
	}
	const root = "/experiment/descendant"
	if err := os.Mkdir(root, 0700); err != nil {
		return err
	}
	var owned []descendantLayout
	var proofs []string
	var backingProtection *descendantBackingLeases
	var ownedRAM []*descendantRAMCapture
	defer func() {
		for index := len(owned) - 1; index >= 0; index-- {
			layout := owned[index]
			if _, err := os.Lstat("/experiment/runtime/" + layout.ID); err == nil {
				_, err := criuRunc("delete", "--force", layout.ID)
				resultErr = errors.Join(resultErr, err)
			} else if !os.IsNotExist(err) {
				resultErr = errors.Join(resultErr, err)
			}
			for _, leaf := range []string{"rootfs", "lower"} {
				_, _ = criuCommand(5*time.Second, "umount", "--", filepath.Join(layout.Root, leaf))
			}
		}
		if backingProtection != nil {
			start := time.Now()
			err := backingProtection.close(true)
			resultErr = errors.Join(resultErr, err)
			record("sysbox-descendant-backing-leases-closed", start,
				asJSON(map[string]any{"images": backingProtection.Path, "page_files": len(backingProtection.Pages),
					"page_bytes": backingProtection.Bytes, "validated_before_close": err == nil,
					"scope": "retained through resume and subsequent source materialization/fork; closed after owned workloads stop"}), err)
		}
		for index := len(ownedRAM) - 1; index >= 0; index-- {
			resultErr = errors.Join(resultErr, ownedRAM[index].cleanup())
		}
	}()
	ancestorID, sourceID := "source-sysbox", "copy-sysbox"
	for generation := 1; generation <= depth; generation++ {
		evidence := root
		if depth > 1 {
			evidence = filepath.Join(root, fmt.Sprintf("generation-%d", generation))
			if err := os.Mkdir(evidence, 0700); err != nil {
				return err
			}
		}
		descendantEvidenceRoot = evidence
		var target descendantLayout
		var ram *descendantRAMCapture
		stepErr := criuDescendantStep(ancestorID, sourceID, evidence, &target, &ram)
		if target.ID != "" {
			owned = append(owned, target)
		}
		if ram != nil {
			ownedRAM = append(ownedRAM, ram)
		}
		if stepErr != nil {
			return stepErr
		}
		proofs = append(proofs, evidence+"/proof.json")
		ancestorID, sourceID = sourceID, target.ID
	}
	summary := map[string]any{"passed": true, "additional_forks": depth, "proofs": proofs,
		"last_source_id": ancestorID, "last_target_id": sourceID, "max_live_sandboxes": 2}
	if os.Getenv("RUNTIME_FORK_DESCENDANT_PAUSE") == "1" {
		sibling, err := descendantOwnedLayout(ancestorID)
		if err != nil {
			return err
		}
		target, err := descendantOwnedLayout(sourceID)
		if err != nil {
			return err
		}
		if err := descendantPauseResume(sibling, target, &backingProtection); err != nil {
			return err
		}
		summary["ram_releasing_descendant_pause_resume"] = true
	}
	if postResumeFork {
		evidence := root + "/post-resume-fork"
		if err := os.Mkdir(evidence, 0700); err != nil {
			return err
		}
		descendantEvidenceRoot = evidence
		var target descendantLayout
		var ram *descendantRAMCapture
		stepErr := criuDescendantStep(ancestorID, sourceID, evidence, &target, &ram)
		if target.ID != "" {
			owned = append(owned, target)
		}
		if ram != nil {
			ownedRAM = append(ownedRAM, ram)
		}
		if stepErr != nil {
			return stepErr
		}
		summary["pre_pause_forks"] = depth
		summary["additional_forks"] = depth + 1
		summary["post_resume_fork"] = map[string]any{
			"source_id": sourceID, "target_id": target.ID, "proof": evidence + "/proof.json",
		}
		summary["last_source_id"], summary["last_target_id"] = sourceID, target.ID
		proofs = append(proofs, evidence+"/proof.json")
		summary["proofs"] = proofs
	}
	if err := os.WriteFile(root+"/chain.json", []byte(asJSON(summary)+"\n"), 0600); err != nil {
		return err
	}
	record("sysbox-descendant-chain-complete", time.Now(), asJSON(summary), nil)
	return nil
}

func criuDescendantStep(ancestorID, sourceID, evidenceRoot string, targetOut *descendantLayout, ramOut **descendantRAMCapture) (resultErr error) {
	start := time.Now()
	ancestor, err := descendantOwnedLayout(ancestorID)
	if err != nil {
		return err
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	child, err := descendantOwnedLayout(sourceID)
	if err != nil {
		return err
	}
	target, err := descendantOwnedLayout("branch-" + hex.EncodeToString(random[:]))
	if err != nil {
		return err
	}
	proof := map[string]any{
		"source": child, "target": target, "max_live_sandboxes": 2,
		"scope":                        "full developer-state fork of the existing forked child; complete independent recapture first, optimized descendant capture and latency distribution remain open",
		"lazy_descendant_restore":      os.Getenv("RUNTIME_FORK_DESCENDANT_LAZY") == "1",
		"ram_descendant_capture":       os.Getenv("RUNTIME_FORK_DESCENDANT_RAM") == "1",
		"reflink_descendant_capture":   os.Getenv("RUNTIME_FORK_DESCENDANT_REFLINK") == "1",
		"guarded_descendant_source":    os.Getenv("RUNTIME_FORK_DESCENDANT_GUARD") == "1",
		"cow_descendant_capture":       os.Getenv("RUNTIME_FORK_DESCENDANT_COW") == "1",
		"pipe_descendant_capture":      os.Getenv("RUNTIME_FORK_DESCENDANT_PIPE") == "observe" || os.Getenv("RUNTIME_FORK_DESCENDANT_PIPE") == "defer-lazy",
		"deferred_descendant_capture":  os.Getenv("RUNTIME_FORK_DESCENDANT_PIPE") == "defer-lazy",
		"inherited_target_restore":     descendantInheritedTargetEnabled(),
		"single_source_capture":        descendantSingleCaptureEnabled(),
		"ordinary_fast_source":         descendantFastSourceEnabled(),
		"complete_ram_original_writer": descendantCompleteRAMEnabled(),
		"retained_target_certificates": descendantTargetCertificateEnabled(),
		"retained_source_certificates": descendantSourceCertificateEnabled(),
		"source_capture_sync_enabled":  descendantSourceSyncEnabled(),
		"coalesced_snapshot_hashes":    descendantCoalescedHashesEnabled(),
	}
	if descendantInheritedTargetEnabled() {
		proof["scope"] = "Actual independent inherited target graph with a same-capture complete byte oracle " +
			"and separate full COW control. Direct fast source capture and latency acceptance remain open."
	}
	if descendantSingleCaptureEnabled() {
		proof["scope"] = "One canonical inherited source capture with a same-stream full original writer, " +
			"an acknowledged source guard and complete independent target; no separate COW capture or control clone."
	}
	if descendantFastSourceEnabled() {
		proof["scope"] = "One guarded ordinary inherited capture without a full original shadow; complete independent target checked against the captured graph. Independent source-selection qualification and full original-writer audits remain separate."
	}
	if descendantCompleteRAMEnabled() {
		proof["scope"] = "Ordinary complete writer in bounded RAM after source materialization, immutable saved page aliases and private target metadata. All source/target materialization and verified disk backing finish before charged readiness; no inherited fast-collector or repeated speed claim."
	}
	phases := map[string]int64{}
	resourceObservation := os.Getenv("RUNTIME_FORK_DESCENDANT_PRESSURE") == "1"
	proof["phase_resource_observation"] = resourceObservation
	var resourceSamples []descendantResourceSample
	var resourceCollectionUS int64
	observeResources := func(name string) {
		sample := descendantReadResources(start, name)
		resourceSamples = append(resourceSamples, sample)
		resourceCollectionUS += sample.CollectionUS
		proof["phase_resource_samples"] = resourceSamples
		proof["phase_resource_collection_us"] = resourceCollectionUS
	}
	if resourceObservation {
		observeResources("begin")
	}
	phaseStart := start
	markPhase := func(name string) {
		if resourceObservation {
			observeResources(name)
		}
		now := time.Now()
		phases[name] = now.Sub(phaseStart).Microseconds()
		phaseStart = now
	}
	proof["phase_us"] = phases
	defer func() {
		markPhase("final_hashes_and_cleanup")
		proof["elapsed_us"] = time.Since(start).Microseconds()
		proof["elapsed_ms"] = time.Since(start).Milliseconds()
		proof["passed"] = resultErr == nil
		if resultErr != nil {
			proof["error"] = resultErr.Error()
		}
		data := asJSON(proof) + "\n"
		if err := os.WriteFile(evidenceRoot+"/proof.json", []byte(data), 0600); err != nil {
			resultErr = errors.Join(resultErr, err)
		}
		summary := map[string]any{
			"proof": evidenceRoot + "/proof.json", "source_id": child.ID, "target_id": target.ID,
			"passed": resultErr == nil, "elapsed_ms": time.Since(start).Milliseconds(),
			"timing": descendantOutcomeTiming(proof),
		}
		descendantResultRecord("sysbox-descendant-full-state-fork", start, summary, resultErr)
	}()
	if err := os.WriteFile(evidenceRoot+"/intent.json", []byte(asJSON(proof)+"\n"), 0600); err != nil {
		return err
	}
	intent, err := os.Open(evidenceRoot + "/intent.json")
	if err != nil {
		return err
	}
	if err := errors.Join(intent.Sync(), intent.Close()); err != nil {
		return err
	}
	preparationIdentity, err := criuRuntimeIdentity(child.ID)
	if err != nil {
		return err
	}
	proof["source_before_preparation"] = preparationIdentity
	if os.Getenv("RUNTIME_FORK_CONTROLLER_LOSS_PROOF") == "1" && child.ID == "copy-sysbox" {
		controllerProof, err := descendantControllerLossProof(child, evidenceRoot)
		if err != nil {
			return err
		}
		if controllerProof["capture_worker_started"] == true {
			proof["controller_loss_during_capture"] = controllerProof
		} else {
			proof["controller_loss_before_capture"] = controllerProof
		}
	}
	var guard *descendantGuard
	if os.Getenv("RUNTIME_FORK_DESCENDANT_GUARD") == "1" {
		sourcePID, ok := preparationIdentity["init_pid"].(int)
		if !ok {
			return fmt.Errorf("source guard requires the actual original init PID")
		}
		guardEvidence := evidenceRoot + "/source-guard"
		if err := os.Mkdir(guardEvidence, 0700); err != nil {
			return err
		}
		guard, err = descendantStartGuard(child, sourcePID, guardEvidence)
		if err != nil {
			return err
		}
		proof["source_guard"] = map[string]any{
			"evidence": guardEvidence, "helper_pid": guard.cmd.Process.Pid,
			"source_pid": sourcePID, "original_start": guard.config.Start,
		}
		defer func() {
			failed := resultErr != nil
			finishErr := guard.finish(failed)
			proof["source_guard_finished_without_error"] = finishErr == nil
			data, readErr := os.ReadFile(guardEvidence + "/guard-outcome.json")
			if readErr == nil {
				var outcome struct {
					Config          descendantGuardConfig `json:"config"`
					Passed          bool                  `json:"passed"`
					CaptureInFlight bool                  `json:"capture_in_flight"`
					Status          string                `json:"status"`
				}
				parseErr := json.Unmarshal(data, &outcome)
				proof["source_guard_outcome"] = json.RawMessage(data)
				if !failed && (parseErr != nil || outcome.Config != guard.config || !outcome.Passed ||
					outcome.CaptureInFlight || outcome.Status != "source_running_guard_disarmed") {
					finishErr = errors.Join(finishErr, fmt.Errorf("source guard did not durably disarm its original source: %v", parseErr))
				}
			} else if !failed {
				finishErr = errors.Join(finishErr, readErr)
			}
			if data, err := os.ReadFile(guardEvidence + "/guard-controller-outcome.json"); err == nil {
				proof["source_guard_controller_outcome"] = json.RawMessage(data)
			}
			resultErr = errors.Join(resultErr, finishErr)
		}()
	}
	reflink := os.Getenv("RUNTIME_FORK_DESCENDANT_REFLINK") == "1"
	if reflink {
		if os.Getenv("RUNTIME_FORK_DESCENDANT_RAM") == "1" || os.Getenv("RUNTIME_FORK_DESCENDANT_LEASED_BACKING") == "1" {
			return fmt.Errorf("reflink capture requires reference file backing and no RAM transport")
		}
		if os.Getenv("RUNTIME_FORK_DESCENDANT_COW") == "1" &&
			os.Getenv("RUNTIME_FORK_DESCENDANT_PIPE") != "observe" {
			return fmt.Errorf("COW/reflink composition requires the complete original writer and retained observer")
		}
		admission, err := descendantLeaseAdmission(child)
		if err != nil {
			return err
		}
		proof["reflink_capture_admission"] = admission
	}
	markPhase("intent_and_source_identity")
	defer func() {
		if resultErr != nil {
			current, identityErr := criuRuntimeIdentity(child.ID)
			proof["failure_source_identity"] = current
			proof["failure_source_identity_preserved"] = identityErr == nil && asJSON(current) == asJSON(preparationIdentity)
		}
	}()
	var inheritPager *criuLazyDaemon
	if descendantInheritProofEnabled() {
		inheritPager, err = descendantBoundPager(child, preparationIdentity)
		if err != nil {
			return err
		}
		proof["inherited_source_proof"] = true
		proof["inherited_source_pager_images"] = inheritPager.owner.Images
	}
	if err := descendantFreeze(ancestor, true); err != nil {
		return err
	}
	defer func() {
		if identity, err := criuRuntimeIdentity(ancestor.ID); err == nil && identity != nil {
			_ = descendantFreeze(ancestor, false)
		}
	}()
	if err := descendantFreeze(child, true); err != nil {
		return err
	}
	preparationFrozen := true
	defer func() {
		if preparationFrozen {
			resultErr = errors.Join(resultErr, descendantFreeze(child, false))
		}
	}()
	materialize := time.Now()
	if err := descendantMaterializeDaemonsExcept(inheritPager, 30*time.Second); err != nil {
		return err
	}
	if guard != nil {
		if err := guard.check(); err != nil {
			return err
		}
	}
	record("sysbox-descendant-materialized-before-recapture", materialize, "all live owned page daemons completed; preparation is included in the descendant span", nil)
	if err := descendantFreeze(child, false); err != nil {
		return err
	}
	preparationFrozen = false
	markPhase("freeze_materialize_thaw")

	overlapPreparation := os.Getenv("RUNTIME_FORK_DESCENDANT_PREP_OVERLAP") == "1"
	preparationStart := time.Now()
	var sourceReady chan descendantExpectedResult
	sourceJoined := false
	if overlapPreparation {
		sourceReady = make(chan descendantExpectedResult, 1)
		go func() { sourceReady <- descendantCollectExpected(child.ID) }()
		defer func() {
			if !sourceJoined {
				// No collector may outlive a failed ancestor operation or
				// source guard. The underlying commands retain their bounds.
				collected := <-sourceReady
				resultErr = errors.Join(resultErr, collected.Err)
				proof["overlapped_source_collector_joined_after_failure"] = true
			}
		}()
	}
	ancestorImages := ancestor.Root + "/images-" + hex.EncodeToString(random[:])
	captureStart := time.Now()
	out, err := descendantCapture(ancestor.ID, ancestorImages, "checkpoint-only")
	err = descendantCommandRecord("sysbox-descendant-ancestor-archived", captureStart, out, err)
	if err != nil {
		return err
	}
	markPhase("ancestor_capture_and_stop")
	ancestorManifest, err := descendantSnapshotManifest(ancestorImages)
	if err != nil {
		return err
	}
	proof["retained_ancestor_payload_hashes"] = ancestorManifest
	if events, err := os.ReadFile(ancestor.Cgroup + "/cgroup.events"); err != nil || strings.Contains(string(events), "populated 1") {
		return fmt.Errorf("ancestor still has running processes before descendant create: %s %v", events, err)
	}
	proof["ancestor_stopped_before_descendant_create"] = true
	proof["active_after_ancestor_stop"], err = descendantActive(child.ID)
	if err != nil {
		return err
	}
	markPhase("ancestor_hash_sync_and_stop_assertions")
	ancestorPreparationDone := time.Now()
	var collected descendantExpectedResult
	if overlapPreparation {
		collected = <-sourceReady
		sourceJoined = true
	} else {
		collected = descendantCollectExpected(child.ID)
	}
	if collected.Err != nil {
		return collected.Err
	}
	before, execs := collected.State.States, collected.State.Execs
	inners := []string{"", "alpha", "beta"}
	criuComposeBefore, criuComposeMarker = collected.State.Compose, collected.State.Marker
	criuToolsBrowserNonce = collected.State.Browser
	history, virtual, beforeIdentity := collected.State.History, collected.State.Virtual, collected.State.Identity
	proof["source_browser_nonce"] = criuToolsBrowserNonce
	proof["source_opencode_history"] = history
	proof["source_virtual_panic"] = virtual
	proof["source_preparation_schedule"] = map[string]any{
		"overlap_enabled":                        overlapPreparation,
		"ancestor_done_us":                       ancestorPreparationDone.Sub(preparationStart).Microseconds(),
		"source_started_us":                      collected.Start.Sub(preparationStart).Microseconds(),
		"source_done_us":                         collected.Done.Sub(preparationStart).Microseconds(),
		"source_duration_us":                     collected.Done.Sub(collected.Start).Microseconds(),
		"all_complete_us":                        time.Since(preparationStart).Microseconds(),
		"all_work_charged_to_complete_readiness": true,
	}
	markPhase("source_expected_state_collection")
	if err := descendantFreeze(child, true); err != nil {
		return err
	}
	frozen := true
	defer func() {
		if frozen {
			if guard != nil && guard.unsafe {
				proof["source_held_for_capture_reconciliation"] = true
			} else {
				resultErr = errors.Join(resultErr, descendantFreeze(child, false))
			}
		}
		if resultErr != nil {
			source, sourceErr := criuRuntimeIdentity(child.ID)
			proof["failure_source_identity"] = source
			proof["failure_source_identity_preserved"] = sourceErr == nil && asJSON(source) == asJSON(beforeIdentity)
		}
	}()
	retained, err := descendantThreads(child)
	if err != nil || len(retained) == 0 {
		return fmt.Errorf("descendant source thread collection failed: %w", err)
	}
	rootPID, ok := beforeIdentity["init_pid"].(int)
	if !ok || retained[rootPID] == "" {
		return fmt.Errorf("source root PID is outside its exact frozen cgroup")
	}
	proof["source_before_capture"] = beforeIdentity
	markPhase("source_freeze_and_thread_collection")
	crashProof := os.Getenv("RUNTIME_FORK_CAPTURE_CRASH_PROOF") == "1" && child.ID == "copy-sysbox"
	if crashProof {
		attempts := 1
		if text := os.Getenv("RUNTIME_FORK_CAPTURE_CRASH_ATTEMPTS"); text != "" {
			attempts, err = strconv.Atoi(text)
			if err != nil || attempts < 1 || attempts > 3 {
				return fmt.Errorf("capture crash proof requires one to three bounded attempts")
			}
		}
		defer func() {
			if resultErr != nil {
				resultErr = errors.Join(resultErr, captureSourceDiagnostic(guard, "crash-source-after-recovery-failed.json"))
			}
		}()
		for attempt := 1; attempt <= attempts; attempt++ {
			crashStart := time.Now()
			if err := descendantCaptureCrashProof(guard, child, retained, proof, attempt, nil); err != nil {
				return err
			}
			if err := descendantFreeze(child, false); err != nil {
				return err
			}
			phase := fmt.Sprintf("capture-crash-recovery-%d", attempt)
			if _, err := criuRestoredStates(child.ID, inners, before); err != nil {
				return err
			}
			if err := criuSysboxServiceResponses(child.ID, phase, crashStart); err != nil {
				return err
			}
			if err := criuSysboxComposeProof(child.ID, phase); err != nil {
				return err
			}
			if err := criuSysboxProcProtection(child.ID, phase); err != nil {
				return err
			}
			if err := criuSysboxTheiaReady([]string{child.ID}, phase, crashStart); err != nil {
				return err
			}
			currentHistory, historyErr := descendantOpenCodeHistory(child.ID)
			currentVirtual, virtualErr := criuRunc("exec", child.ID, "cat", "/proc/sys/kernel/panic")
			identity, identityErr := criuRuntimeIdentity(child.ID)
			if historyErr != nil || asJSON(currentHistory) != asJSON(history) ||
				virtualErr != nil || strings.TrimSpace(currentVirtual) != strings.TrimSpace(virtual) ||
				identityErr != nil || asJSON(identity) != asJSON(beforeIdentity) {
				return fmt.Errorf("source state changed after actual capturer crash: %v %v %v",
					historyErr, virtualErr, identityErr)
			}
			if err := descendantFreeze(child, true); err != nil {
				return err
			}
			retained, err = descendantThreads(child)
			if err != nil || retained[rootPID] != guard.config.Start {
				return fmt.Errorf("recovered source identity unavailable for retry: %v", err)
			}
			continuation := captureCrashContinuation{Version: 1, Attempt: attempt, Source: child,
				PID: guard.config.PID, Start: guard.config.Start, Token: guard.config.Token, Passed: true}
			if err := descendantGuardWrite(guard.anchor.evidence,
				captureCrashPrefix(attempt)+"crash-source-continuation.json", continuation); err != nil {
				return err
			}
			proof["capture_crash_source_continuations"] = attempt
			markPhase(fmt.Sprintf("actual_capture_sigkill_recovery_and_source_continuation_%d", attempt))
		}
		proof["source_services_continued_after_actual_capture_crash"] = true
	}
	images := child.Root + "/images-" + hex.EncodeToString(random[:])
	var ram *descendantRAMCapture
	if os.Getenv("RUNTIME_FORK_DESCENDANT_RAM") == "1" {
		ram, err = descendantPrepareRAM(child, images, evidenceRoot)
		if err != nil {
			return err
		}
		*ramOut = ram
	}
	var inherited *descendantInheritedCapture
	if inheritPager != nil {
		inherited, err = descendantCaptureInheritedSource(child, inheritPager, beforeIdentity,
			retained, guard, evidenceRoot)
		if err != nil {
			return err
		}
		defer func() { resultErr = errors.Join(resultErr, inherited.Pins.close()) }()
		proof["inherited_source_capture"] = inherited.Proof
		markPhase("inherited_capture_and_reference_materialization")
	}
	singleCapture := descendantSingleCaptureEnabled()
	captureStart = time.Now()
	if singleCapture {
		if inherited == nil || guard == nil || guard.unsafe ||
			inherited.Proof["single_capture_transaction_completed"] != true {
			return fmt.Errorf("single capture lacks its original writer and acknowledged safe source")
		}
		images = inherited.OriginalImages
		proof["source_capture_count"] = 1
		if descendantFastSourceEnabled() {
			images = inherited.Images
			proof["source_captured_graph_images"] = images
			proof["per_operation_original_writer_oracle"] = false
		} else {
			proof["source_original_writer_images"] = images
		}
		record("sysbox-descendant-single-source-capture-committed", captureStart,
			asJSON(map[string]any{"captures": 1, "images": inherited.Images,
				"original_writer": inherited.OriginalImages, "ordinary_fast_collector": descendantFastSourceEnabled()}), nil)
	} else {
		if guard != nil && !crashProof {
			if inherited == nil {
				if err := guard.capturePhase(true); err != nil {
					return err
				}
			} else if !guard.unsafe {
				return fmt.Errorf("inherited capture lost its guarded original-writer transaction")
			}
		}
		out, err = descendantCapture(child.ID, images, "checkpoint-retain-lineage")
		err = descendantCommandRecord("sysbox-descendant-child-recaptured", captureStart, out, err)
		if err != nil {
			return err
		}
		current, err := descendantThreads(child)
		if err != nil || asJSON(current) != asJSON(retained) {
			return fmt.Errorf("descendant source threads changed during capture: %v", err)
		}
		if guard != nil {
			if err := guard.capturePhase(false); err != nil {
				return err
			}
		}
	}
	if ram != nil {
		if descendantCompleteRAMEnabled() {
			complete, err := descendantAdmitCompleteRAM(images, "")
			if err != nil {
				return err
			}
			proof["complete_local_memory"] = complete
			proof["source_capture_count"] = 1
			proof["source_original_writer_images"] = images
		}
		if err := ram.seal(); err != nil {
			return err
		}
		proof["ram_capture"] = map[string]any{
			"images": images, "bound_bytes": criuRAMSnapshotLimit,
			"logical_bytes": ram.Logical, "allocated_bytes": ram.Allocated,
			"filesystem_wide_readonly": true,
		}
	}
	markPhase("source_capture_and_thread_assertions")
	if inherited != nil && !descendantFastSourceEnabled() {
		start := time.Now()
		comparison, compareErr := descendantCompareImageMemory(inherited.Images, inherited.OriginalImages)
		proof["inherited_source_original_writer_memory"] = comparison
		record("sysbox-descendant-inherited-memory-original-writer", start, asJSON(comparison), compareErr)
		if compareErr != nil {
			return compareErr
		}
		markPhase("inherited_complete_original_writer_memory_comparison")
	}
	var manifest map[string]string
	var protected *descendantBackingLeases
	var sourceProtection *descendantSourceProtection
	defer func() {
		if protected != nil {
			resultErr = errors.Join(resultErr, protected.close(false))
		}
		if sourceProtection != nil {
			resultErr = errors.Join(resultErr, sourceProtection.close(false))
		}
	}()
	if descendantSourceCertificateEnabled() {
		sourceProtection, manifest, err = descendantProtectInheritedSource(child, inherited)
	} else if reflink {
		protected, manifest, err = descendantProtectCapture(child, images)
	} else {
		manifest, err = descendantSnapshotManifest(images)
	}
	if err != nil {
		return err
	}
	if sourceProtection != nil {
		proof["child_capture_metadata_hashes"] = manifest
		proof["source_page_content_hashes_computed"] = false
	} else {
		proof["child_capture_payload_hashes"] = manifest
	}
	if protected != nil {
		proof["reflink_capture_page_hash_bytes"] = protected.Bytes
		proof["reflink_capture_verification_runs"] = protected.VerifyRuns
	}
	if sourceProtection != nil {
		markPhase("source_metadata_hash_and_page_identity_admission")
		if descendantSourceSyncEnabled() {
			synced, err := sourceProtection.syncCapture()
			if err != nil {
				return err
			}
			proof["source_capture_sync"] = synced
			markPhase("source_capture_file_sync")
		}
	} else {
		markPhase("source_full_hash_sync")
	}
	if err := os.Mkdir("/experiment/branches", 0755); err != nil && !os.IsExist(err) {
		return err
	}
	targetSourceImages, controlCloneImages := images, target.Root+"/images"
	targetExpected := manifest
	var targetGraph map[string]any
	var targetProtection *descendantBackingLeases
	defer func() {
		if targetProtection != nil {
			proof["retained_target_certificate_checks"] = targetProtection.VerifyRuns
			proof["retained_target_certificate_page_bytes"] = targetProtection.Bytes
			proof["retained_target_certificate_page_files"] = len(targetProtection.Pages)
			closeErr := targetProtection.close(resultErr == nil)
			proof["retained_target_certificate_closed_successfully"] = closeErr == nil
			resultErr = errors.Join(resultErr, closeErr)
		}
	}()
	if descendantInheritedTargetEnabled() {
		if inherited == nil || ram != nil || protected != nil || reflink {
			return fmt.Errorf("inherited target requires its separate complete file-backed COW control")
		}
		graphStart := time.Now()
		cpuProfile, cloneErr := descendantProfileTargetGraph(evidenceRoot, func() error {
			if descendantTargetCertificateEnabled() {
				targetGraph, targetExpected, err = descendantCloneInheritedTargetProtectedPolicy(
					inherited, target, true, true, &targetProtection)
			} else {
				targetGraph, targetExpected, err = descendantCloneInheritedTarget(inherited, target)
			}
			return err
		})
		err = cloneErr
		if cpuProfile != nil {
			proof["target_graph_cpu_profile"] = cpuProfile
		}
		if err != nil {
			return err
		}
		targetSourceImages, controlCloneImages = inherited.Images, target.Root+"/control-images"
		proof["inherited_target_graph"] = targetGraph
		proof["inherited_target_expected_head_hashes"] = targetExpected
		if singleCapture {
			controlCloneImages = ""
			proof["separate_control_capture_and_clone_omitted"] = true
			proof["canonical_target_payload_certificate_policy"] = "complete_original_writer_memory_and_private_metadata"
			if descendantFastSourceEnabled() {
				proof["canonical_target_payload_certificate_policy"] = "complete_captured_graph_memory_and_private_metadata"
			}
		} else {
			proof["separate_cow_control_clone_images"] = controlCloneImages
		}
		record("sysbox-descendant-inherited-target-graph-cloned", graphStart, asJSON(map[string]any{
			"head": targetGraph["head"], "nodes": targetGraph["nodes"],
			"kernel_cloned_bytes":            targetGraph["kernel_cloned_bytes"],
			"direct_original_graph":          targetGraph["direct_original_graph"],
			"logical_memory_bytes":           targetGraph["logical_memory_bytes"],
			"kernel_range_clone_bytes":       targetGraph["kernel_range_clone_bytes"],
			"verified_ram_range_copy_bytes":  targetGraph["verified_ram_range_copy_bytes"],
			"lease_only_page_inputs":         targetGraph["lease_only_page_inputs"],
			"source_input_page_hash_bytes":   targetGraph["source_input_page_hash_bytes"],
			"source_unhashed_page_bytes":     targetGraph["source_unhashed_page_bytes"],
			"verified_ram_parent_copy_bytes": targetGraph["verified_ram_parent_copy_bytes"],
			"verified_ram_parent_copy_files": targetGraph["verified_ram_parent_copy_files"],
		}), nil)
		markPhase("inherited_target_graph_clone")
	} else {
		if err := os.Mkdir(target.Root, 0755); err != nil {
			return err
		}
	}
	*targetOut = target
	if err := os.Mkdir(target.Root+"/lower", 0700); err != nil {
		return err
	}
	if out, err := criuCommand(10*time.Second, "mount", "--bind", child.Root+"/lower", target.Root+"/lower"); err != nil {
		return fmt.Errorf("descendant immutable lower: %s: %w", out, err)
	}
	if out, err := criuCommand(10*time.Second, "mount", "-o", "remount,bind,ro", target.Root+"/lower"); err != nil {
		return fmt.Errorf("descendant readonly lower: %s: %w", out, err)
	}
	for _, name := range []string{"rootfs", "overlay-work"} {
		if err := os.Mkdir(filepath.Join(target.Root, name), 0700); err != nil {
			return err
		}
	}
	for _, pair := range [][2]string{{child.Root + "/upper", target.Root + "/upper"}} {
		if out, err := criuCommand(90*time.Second, "cp", "-a", "--reflink=auto", "--", pair[0], pair[1]); err != nil {
			return fmt.Errorf("complete descendant files clone: %s: %w", out, err)
		}
	}
	var clonedManifest map[string]string
	verificationImages := controlCloneImages
	expectedClone := manifest
	if singleCapture {
		verificationImages, expectedClone = target.Root+"/images", targetExpected
		for name, expected := range targetExpected {
			if !criuRAMPageName.MatchString(name) && !descendantMemoryName.MatchString(name) && manifest[name] != expected {
				return fmt.Errorf("canonical target metadata differs from its same-stream original writer: %s", name)
			}
		}
	}
	if ram != nil {
		if err := ram.clone(target, manifest); err != nil {
			return err
		}
	} else if protected != nil {
		var cloneProof map[string]any
		clonedManifest, cloneProof, err = descendantCloneProtectedCapture(protected, target, manifest)
		if err != nil {
			return err
		}
		proof["reflink_clone"] = cloneProof
		if err := protected.close(true); err != nil {
			return err
		}
		protected = nil
		proof["reflink_source_leases_closed_after_clone"] = true
	} else if !singleCapture {
		if out, err := criuCommand(90*time.Second, "cp", "-a", "--reflink=auto", "--", images, controlCloneImages); err != nil {
			return fmt.Errorf("complete descendant image clone: %s: %w", out, err)
		}
	}
	markPhase("filesystem_setup_and_reflink_clone")
	if ram != nil {
		clonedManifest = ram.Cloned
		proof["clone_verified_during_copy_without_duplicate_page_reads"] = true
	} else if !reflink {
		if targetProtection != nil {
			clonedManifest, err = descendantProtectedTargetManifest(targetProtection, targetGraph, target)
		} else {
			clonedManifest, err = descendantSnapshotManifest(verificationImages)
		}
	}
	if err != nil || asJSON(clonedManifest) != asJSON(expectedClone) {
		return fmt.Errorf("complete descendant clone payload differs from frozen source: %v", err)
	}
	if targetGraph != nil {
		clonedHead := clonedManifest
		var err error
		if targetProtection == nil {
			clonedHead, err = descendantSnapshotManifest(target.Root + "/images")
		} else {
			err = targetProtection.verify()
			proof["target_head_manifest_reused_under_read_leases"] = true
		}
		if err != nil || asJSON(clonedHead) != asJSON(targetExpected) {
			return fmt.Errorf("inherited target head differs from its held-inode certificate: %v", err)
		}
		memoryStart := time.Now()
		compare, reference, key, event := descendantCompareImageMemory, inherited.OriginalImages,
			"inherited_target_original_writer_memory", "sysbox-descendant-inherited-target-memory-original-writer"
		if descendantFastSourceEnabled() {
			compare, reference, key, event = descendantCompareCapturedMemory, inherited.Images,
				"inherited_target_captured_graph_memory", "sysbox-descendant-inherited-target-memory-captured-graph"
		}
		var compared map[string]any
		if targetProtection != nil {
			compared, err = descendantProtectedTargetMemory(targetProtection, targetGraph, inherited, target)
			proof["target_logical_comparison_reused_under_read_leases"] = true
		} else {
			compared, err = compare(target.Root+"/images", reference)
		}
		proof[key] = compared
		record(event, memoryStart, asJSON(compared), err)
		if err != nil {
			return err
		}
	}
	var sharedPages, privateMetadata int
	for name := range targetExpected {
		sourceFile, sourceErr := os.Stat(filepath.Join(targetSourceImages, name))
		targetFile, targetErr := os.Stat(filepath.Join(target.Root, "images", name))
		if sourceErr != nil || targetErr != nil {
			return fmt.Errorf("descendant payload inode unavailable for %s: %v %v", name, sourceErr, targetErr)
		}
		if ram != nil && criuRAMPageName.MatchString(name) {
			if !os.SameFile(sourceFile, targetFile) {
				return fmt.Errorf("RAM fork duplicated immutable saved pages: %s", name)
			}
			sharedPages++
		} else if os.SameFile(sourceFile, targetFile) {
			return fmt.Errorf("descendant mutable image inode is shared for %s: %v %v", name, sourceErr, targetErr)
		} else {
			privateMetadata++
		}
	}
	if ram != nil {
		proof["shared_readonly_page_files"] = sharedPages
		proof["private_metadata_files"] = privateMetadata
	}
	proof["complete_clone_payload_hashes_match"] = true
	proof["cloned_payload_inodes_independent"] = true
	markPhase("clone_full_hash_sync_inode_assertions")
	options := "lowerdir=" + target.Root + "/lower,upperdir=" + target.Root + "/upper,workdir=" + target.Root + "/overlay-work"
	if err := unix.Mount("overlay", target.Root+"/rootfs", "overlay", 0, options); err != nil {
		return err
	}
	if err := criuCgroup(target.ID); err != nil {
		return err
	}
	markPhase("overlay_and_cgroup_setup")
	restoreStart := time.Now()
	restoreArgs := []string{"--root", "/experiment/runtime",
		"experiment-restore-copy", "--copy-id", target.ID, "--copy-dir", target.Root,
		"--image-path", target.Root + "/images", child.ID}
	var pageDaemon *criuLazyDaemon
	if os.Getenv("RUNTIME_FORK_DESCENDANT_LAZY") == "1" {
		out, err = criuSysboxRestoreOwned(target.ID, target.Root+"/images", "/usr/local/sbin/sysbox-runc-experiment", restoreArgs...)
	} else {
		out, err = criuCommand(60*time.Second, "/usr/local/sbin/sysbox-runc-experiment", restoreArgs...)
	}
	err = descendantCommandRecord("sysbox-descendant-child-to-grandchild-restore", restoreStart, out, err)
	if err != nil {
		return err
	}
	if os.Getenv("RUNTIME_FORK_DESCENDANT_LAZY") == "1" {
		pageDaemon, err = descendantPageDaemon(target.Root + "/images")
		if err != nil {
			return err
		}
		proof["lazy_daemon_pid"] = pageDaemon.command.Process.Pid
		proof["lazy_daemon_images"] = target.Root + "/images"
	}
	proof["active_after_descendant_restore"], err = descendantActive(child.ID, target.ID)
	if err != nil {
		return err
	}
	markPhase("restore_and_active_count_assertion")
	if guard != nil {
		if err := guard.check(); err != nil {
			return err
		}
	}
	if err := descendantFreeze(child, false); err != nil {
		return err
	}
	frozen = false
	afterIdentity, err := criuRuntimeIdentity(child.ID)
	if err != nil || asJSON(afterIdentity) != asJSON(beforeIdentity) {
		return fmt.Errorf("forked source process/runtime identity changed: %v", err)
	}
	markPhase("source_thaw_and_identity_assertion")
	for index, id := range []string{child.ID, target.ID} {
		role := []string{"source", "target"}[index]
		if _, err := criuRestoredStates(id, inners, before); err != nil {
			return err
		}
		markPhase(role + "_restored_state_assertions")
		if err := criuSysboxServiceResponses(id, "descendant", start); err != nil {
			return err
		}
		markPhase(role + "_direct_service_responses")
		if err := criuSysboxComposeProof(id, "descendant"); err != nil {
			return err
		}
		if err := criuSysboxProcProtection(id, "descendant"); err != nil {
			return err
		}
		currentHistory, err := descendantOpenCodeHistory(id)
		if err != nil || asJSON(currentHistory) != asJSON(history) {
			return fmt.Errorf("descendant OpenCode history differs for %s: %v", id, err)
		}
		currentVirtual, err := criuRunc("exec", id, "cat", "/proc/sys/kernel/panic")
		if err != nil || strings.TrimSpace(currentVirtual) != strings.TrimSpace(virtual) {
			return fmt.Errorf("descendant did not inherit current Sysbox virtual state for %s: %q %v", id, currentVirtual, err)
		}
		markPhase(role + "_compose_proc_history_virtual_assertions")
	}
	if err := criuSysboxTheiaReady([]string{child.ID, target.ID}, "descendant", start); err != nil {
		return err
	}
	if guard != nil {
		if err := guard.check(); err != nil {
			return err
		}
	}
	markPhase("native_theia_read_edit")
	if descendantCompleteRAMEnabled() {
		// Charge required backing before readiness. No already-ready target
		// is frozen later to make persistence appear free.
		if ram == nil || pageDaemon == nil {
			return fmt.Errorf("complete RAM fork lost its owned capture or pager")
		}
		if err := pageDaemon.materialize(30 * time.Second); err != nil {
			return err
		}
		released, err := ram.release(manifest)
		if err != nil {
			return err
		}
		proof["ram_transport_release"] = released
		proof["complete_ram_backed_before_readiness"] = true
		markPhase("ram_materialization_and_verified_backing_before_readiness")
	}
	if pageDaemon != nil {
		proof["lazy_daemon_live_at_correctness_readiness"] = !pageDaemon.pollCompletion()
		if pageDaemon.finished && pageDaemon.exitErr != nil {
			return fmt.Errorf("descendant pager failed before complete readiness: %w", pageDaemon.exitErr)
		}
	}
	readyMemory, err := criuRAMMemoryRead()
	if err != nil {
		return err
	}
	peakData, err := os.ReadFile(filepath.Join(readyMemory.Cgroup, "memory.peak"))
	if err != nil {
		return err
	}
	peakBytes, err := strconv.ParseUint(strings.TrimSpace(string(peakData)), 10, 64)
	if err != nil {
		return err
	}
	proof["memory_at_complete_fork_readiness"] = readyMemory
	proof["probe_memory_peak_through_fork_readiness_bytes"] = peakBytes
	if resourceObservation {
		markPhase("readiness_resource_boundary")
	}
	proof["complete_correctness_readiness_us"] = time.Since(start).Microseconds()
	proof["readiness_timing_scope"] = "All preceding intent, materialization, sibling archival, full hash/sync, clone, restore and source/target state/service/Theia checks are charged. Later independent mutations and final complete hashes remain mandatory correctness gates. This is a diagnostic correctness observation, not a controlled availability sample."
	if err := criuSysboxLiveExecProof(child.ID, target.ID, execs); err != nil {
		return err
	}
	if err := descendantResourceProof(child, target); err != nil {
		return err
	}
	if err := descendantVirtualProof(child.ID, target.ID); err != nil {
		return err
	}
	markPhase("held_exec_resource_virtual_independence_assertions")
	if err := criuIndependentProof("sysbox", child.ID, target.ID, inners, before); err != nil {
		return err
	}
	markPhase("independent_workload_mutation_assertions")
	var after map[string]string
	if sourceProtection != nil {
		after, err = sourceProtection.manifest()
	} else {
		after, err = descendantSnapshotManifest(images)
	}
	if err != nil || asJSON(after) != asJSON(manifest) {
		return fmt.Errorf("descendant capture changed after independent workload writes: %v", err)
	}
	afterManifest, err := descendantSnapshotManifest(ancestorImages)
	if err != nil || asJSON(afterManifest) != asJSON(ancestorManifest) {
		return fmt.Errorf("retained ancestor snapshot changed: %v", err)
	}
	if ram != nil && ram.Mounted {
		materializeStart := time.Now()
		if err := descendantFreeze(target, true); err != nil {
			return err
		}
		materializeErr := pageDaemon.materialize(30 * time.Second)
		thawErr := descendantFreeze(target, false)
		if err := errors.Join(materializeErr, thawErr); err != nil {
			return err
		}
		proof["clone_materialization_before_ram_release_ms"] = time.Since(materializeStart).Milliseconds()
		released, err := ram.release(manifest)
		if err != nil {
			memory, memoryErr := criuRAMMemoryRead()
			proof["memory_at_ram_backing_failure"] = memory
			if memoryErr != nil {
				proof["memory_at_ram_backing_failure_error"] = memoryErr.Error()
			}
			return err
		}
		proof["ram_transport_release"] = released
	}
	if os.Getenv("RUNTIME_FORK_DESCENDANT_PIPE") == "observe" {
		releaseStart := time.Now()
		if err := criuLogicalRetireDirectory(images, controlCloneImages); err != nil {
			return err
		}
		proof["retained_pipe_capture_retired_after_original_writer_comparison"] = true
		proof["retained_pipe_capture_retirement_ms"] = time.Since(releaseStart).Milliseconds()
	} else if os.Getenv("RUNTIME_FORK_DESCENDANT_PIPE") == "defer-lazy" {
		if ram == nil || ram.Mounted {
			return fmt.Errorf("deferred capture was not backed and retired before transport cleanup")
		}
		proof["retained_deferred_capture_retired_after_complete_backing_readback"] = true
	}
	if !singleCapture {
		retirement, err := descendantRetireTransport(child, images, evidenceRoot)
		if err != nil {
			return err
		}
		proof["transient_capture_retirement"] = retirement
	} else {
		proof["single_capture_no_retained_transport"] = true
	}
	if targetProtection != nil {
		after, err = descendantProtectedTargetManifest(targetProtection, targetGraph, target)
	} else {
		after, err = descendantSnapshotManifest(verificationImages)
	}
	if err != nil || asJSON(after) != asJSON(expectedClone) {
		return fmt.Errorf("full independent descendant clone changed after transport retirement: %v", err)
	}
	if targetGraph != nil {
		graphStart := time.Now()
		if targetProtection != nil {
			if err := targetProtection.verify(); err != nil {
				return err
			}
		}
		verified, err := descendantVerifyInheritedTargetGraph(targetGraph, target)
		proof["inherited_target_graph_final_verification"] = verified
		record("sysbox-descendant-inherited-target-graph-retained-after-workloads", graphStart, asJSON(verified), err)
		if err != nil {
			return err
		}
		if targetProtection != nil {
			if err := targetProtection.verify(); err != nil {
				return err
			}
			proof["complete_target_final_hashes_verified_under_read_leases"] = true
		}
		if sourceProtection != nil {
			pageInodes := map[string]descendantLeaseIdentity{}
			for name, lease := range sourceProtection.Backing.Pages {
				pageInodes[name] = lease.Saved
			}
			if err := sourceProtection.close(true); err != nil {
				return err
			}
			proof["retained_source_inode_certificate"] = map[string]any{
				"images": sourceProtection.Backing.Path, "parent_images": sourceProtection.Parent,
				"directory": sourceProtection.Backing.Identity, "page_inodes": pageInodes,
				"page_files": sourceProtection.PageFiles, "page_bytes": sourceProtection.Backing.Bytes,
				"metadata_files": sourceProtection.MetaFiles, "page_content_hash_bytes": int64(0),
				"checks": sourceProtection.VerifyRuns, "closed_after_complete_target_verification": true,
				"scope": "Continuous original source page and metadata inode read leases, complete metadata hashes, " +
					"actual complete captured-graph target comparison and final target hashes. No full source page hashes claimed.",
			}
		}
		retireStart := time.Now()
		memoryKey := "inherited_target_original_writer_memory"
		if descendantFastSourceEnabled() {
			memoryKey = "inherited_target_captured_graph_memory"
		}
		retiredDiagnostics, err := descendantRetireInheritedDiagnostics(child, target, inherited, evidenceRoot,
			proof[memoryKey].(map[string]any), verified)
		proof["inherited_diagnostic_payload_retirement"] = retiredDiagnostics
		record("sysbox-descendant-inherited-diagnostic-payloads-retired", retireStart, asJSON(retiredDiagnostics), err)
		if err != nil {
			return err
		}
	}
	if sourceProtection != nil {
		proof["source_inodes_and_target_hashes_unchanged_after_workload_writes"] = true
	} else {
		proof["capture_and_clone_hashes_unchanged_after_workload_writes"] = true
	}
	if singleCapture {
		proof["canonical_target_hashes_unchanged_after_workload_writes"] = true
	} else {
		proof["clone_full_hashes_unchanged_after_transport_retirement"] = true
	}
	proof["source_preserved"] = true
	proof["native_services_and_theia_passed"] = true
	proof["ancestor_hashes_unchanged"] = true
	if sourceProtection != nil {
		proof["source_metadata_manifest_sha256"] = fmt.Sprintf("%x", sha256.Sum256([]byte(asJSON(manifest))))
	} else {
		proof["complete_snapshot_manifest_sha256"] = fmt.Sprintf("%x", sha256.Sum256([]byte(asJSON(manifest))))
	}
	*targetOut = target
	if guard != nil {
		return guard.check()
	}
	return nil
}
