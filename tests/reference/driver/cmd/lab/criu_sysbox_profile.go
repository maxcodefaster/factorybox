package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

func criuSysboxRestoreCommand(images, executable string, arguments ...string) (string, error) {
	return criuSysboxProfileCommand("restore", images, executable, arguments...)
}

func criuSysboxRecordCommandCounters(phase, images string, traceStart, commandStart time.Time,
	before criuPhaseCounters, beforeErr, commandErr error) {
	commandWallUS := time.Since(commandStart).Microseconds()
	after, afterErr := criuSysboxPhaseCountersRead()
	var delta criuPhaseDelta
	measurementErr := errors.Join(beforeErr, afterErr)
	if measurementErr == nil {
		delta, measurementErr = criuPhaseDifference(before, after)
	}
	record("sysbox-"+phase+"-resource-counters", traceStart, asJSON(map[string]any{
		"phase": phase, "images": images, "command_wall_us": commandWallUS,
		"command_succeeded": commandErr == nil, "before": before, "after": after, "delta": delta,
		"scope": "entire owned probe cgroup and descendants; snapshots are read sequentially, not atomically; pressure classes and native timers overlap; diagnostic, not a p95 sample",
	}), measurementErr)
}

func criuSysboxProfileCommand(phase, images, executable string, arguments ...string) (output string, commandErr error) {
	if phase != "restore" && phase != "capture" {
		return "", fmt.Errorf("unexpected owned profile phase: %q", phase)
	}
	traceStart := time.Now()
	before, beforeErr := criuSysboxPhaseCountersRead()
	commandStart := time.Now()
	defer func() {
		// Entire owned probe cgroup, including original source tasks and
		// manager/fs/broker children. The separate IDE is outside this scope.
		// Record missing/reset counters explicitly; retain command outcome.
		criuSysboxRecordCommandCounters(phase, images, traceStart, commandStart, before, beforeErr, commandErr)
	}()
	if os.Getenv("CRIU_SYSBOX_PROFILE") != "1" {
		return criuCommand(40*time.Second, executable, arguments...)
	}
	if os.Getenv("CRIU_SYSBOX_PROFILE_SCOPE") == "child-cgroup" {
		if phase != "restore" || images != "/experiment/sysbox-copy/images" {
			return criuCommand(40*time.Second, executable, arguments...)
		}
		if beforeErr != nil {
			return "", beforeErr
		}
		selector, err := criuSysboxProfileCgroupSelector(before.Cgroup)
		if err != nil {
			return "", err
		}
		profilePath := filepath.Join(criuRoot, "profile-child-cgroup.data")
		// A cgroup-filtered event includes existing managers/page daemon and
		// newly restored descendants. It excludes the IDE and other tenants.
		// Kernel callchains need no per-sample userspace stack dump.
		args := []string{"record", "--all-cpus", "--event=cpu-clock", "--cgroup=" + selector,
			"--freq=99", "--call-graph=fp", "--output=" + profilePath, "--", executable}
		out, err := criuCommand(40*time.Second, "/usr/local/bin/perf", append(args, arguments...)...)
		record("sysbox-restore-owned-cgroup-cpu-profile", traceStart, asJSON(map[string]any{
			"images": images, "profile": profilePath, "event": "cpu-clock", "frequency_hz": 99,
			"cgroup": before.Cgroup, "cgroup_selector": selector, "device": before.Device, "inode": before.Inode,
			"sample_scope": "entire owned probe cgroup and descendants during child restore; separate IDE and unrelated tasks excluded by the perf cgroup filter",
			"timing_scope": "instrumented diagnosis; kernel and available frame-pointer callchains, not blocked wall time or p95",
		})+out, err)
		return out, err
	}
	// Keep sampling data outside checkpoint images and writable guest layers.
	// Profiling runs establish causes, not the readiness latency distribution.
	name := strings.ReplaceAll(strings.TrimPrefix(images, "/experiment/"), "/", "-")
	if phase == "capture" {
		name = "capture-" + name
	}
	profilePath := filepath.Join(criuRoot, "profile-"+name+".data")
	statArgs := []string{"/usr/local/bin/perf", "stat", "--field-separator=,", "--output=" + profilePath + ".stat", "--event=task-clock,context-switches,cpu-migrations,page-faults,major-faults,minor-faults", "--", executable}
	args := []string{"record", "--event=cpu-clock", "--freq=199", "--call-graph=dwarf,8192", "--output=" + profilePath, "--"}
	args = append(args, statArgs...)
	var originalThreads map[int]string
	var originalProfile string
	if phase == "capture" && os.Getenv("CRIU_SYSBOX_RETAIN_SOURCE") == "1" && images == "/experiment/sysbox/images-again" {
		// The parasite executes in the original source tasks, outside the
		// command descendant profile. Attach a separate event only to the
		// exact owned, kernel-frozen TIDs before CRIU starts running them.
		var err error
		originalThreads, err = criuSysboxFrozenThreads()
		if err != nil {
			return "", err
		}
		tids := make([]int, 0, len(originalThreads))
		for tid := range originalThreads {
			tids = append(tids, tid)
		}
		sort.Ints(tids)
		values := make([]string, len(tids))
		for i, tid := range tids {
			values[i] = strconv.Itoa(tid)
		}
		originalProfile = filepath.Join(criuRoot, "profile-original-"+name+".data")
		originalArgs := []string{"record", "--event=cpu-clock", "--freq=199", "--call-graph=dwarf,8192", "--tid=" + strings.Join(values, ","), "--output=" + originalProfile, "--", "/usr/local/bin/perf"}
		args = append(originalArgs, args...)
	}
	start := time.Now()
	out, err := criuCommand(40*time.Second, "/usr/local/bin/perf", append(args, arguments...)...)
	record("sysbox-"+phase+"-cpu-profile", start, asJSON(map[string]any{"images": images, "profile": profilePath, "stat": profilePath + ".stat", "event": "cpu-clock", "frequency_hz": 199, "timing_scope": "instrumented diagnostic; not a p95 sample", "sample_scope": "owned command descendants; original source tasks and Sysbox mgr/fs outside that tree are not sampled"})+out, err)
	if originalProfile != "" {
		record("sysbox-capture-original-source-cpu-profile", start, asJSON(map[string]any{
			"images": images, "profile": originalProfile, "original_thread_start_times": originalThreads,
			"event": "cpu-clock", "frequency_hz": 199, "timing_scope": "instrumented diagnostic; not a p95 sample",
			"sample_scope": "separate event attached to exact owned frozen original source TIDs; command descendants remain a separate profile; outside Sysbox mgr/fs and unrelated tasks excluded"})+out, err)
	}
	return out, err
}

func criuSysboxProfilePreflight() error {
	if os.Getenv("CRIU_SYSBOX_PROFILE") != "1" {
		return nil
	}
	start := time.Now()
	args := []string{"stat", "--event=cpu-clock"}
	if os.Getenv("CRIU_SYSBOX_PROFILE_SCOPE") == "child-cgroup" {
		counters, err := criuSysboxPhaseCountersRead()
		if err != nil {
			return err
		}
		selector, err := criuSysboxProfileCgroupSelector(counters.Cgroup)
		if err != nil {
			return err
		}
		args = append(args, "--all-cpus", "--cgroup="+selector)
	}
	args = append(args, "--", "/bin/true")
	out, err := criuCommand(5*time.Second, "/usr/local/bin/perf", args...)
	if err != nil {
		err = fmt.Errorf("owned capture/restore profiling unavailable: %w", err)
	}
	record("sysbox-profile-kernel-preflight", start, out, err)
	return err
}

func criuSysboxProfileCgroupSelector(path string) (string, error) {
	if filepath.Clean(path) != path || !filepath.IsAbs(path) {
		return "", fmt.Errorf("unclean owned profile cgroup path: %q", path)
	}
	relative, err := filepath.Rel("/sys/fs/cgroup", path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, "../") {
		return "", fmt.Errorf("profile cgroup is outside its mounted hierarchy: %q", path)
	}
	if relative == "." {
		return "/", nil
	}
	return "/" + filepath.ToSlash(relative), nil
}
