package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

func criuProcStatusIDs(data, key string) ([]int, error) {
	for _, line := range strings.Split(data, "\n") {
		if !strings.HasPrefix(line, key+":") {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(line, key+":"))
		if len(fields) == 0 {
			break
		}
		ids := make([]int, len(fields))
		for i, field := range fields {
			id, err := strconv.Atoi(field)
			if err != nil || id <= 0 {
				return nil, fmt.Errorf("invalid proc %s", key)
			}
			ids[i] = id
		}
		return ids, nil
	}
	return nil, fmt.Errorf("missing proc %s", key)
}

// Diagnose only failed lazy readiness. Read the owned sandbox's exact
// OpenCode threads and our already-created page services before teardown.
// No process is stopped, ptraced or executed; absent proc fields stay explicit.
func criuSysboxFailedServiceThreads(id, phase string, expected criuServiceExpected) {
	if os.Getenv("CRIU_SYSBOX_LAZY") != "1" || criuRoot != "/experiment" {
		return
	}
	start := time.Now()
	mode := "sysbox"
	if id == "copy-sysbox" {
		mode = "sysbox-copy"
	} else if id != "source-sysbox" {
		return
	}
	pid, err := strconv.Atoi(fmt.Sprint(expected.OpenCodeServer["pid"]))
	rootStatus, rootErr := os.ReadFile(fmt.Sprintf("/proc/%d/status", expected.PID))
	rootIDs, idsErr := criuProcStatusIDs(string(rootStatus), "NSpid")
	if err != nil || pid <= 0 || rootErr != nil || idsErr != nil || rootIDs[len(rootIDs)-1] != 1 {
		record("sysbox-lazy-failed-service-thread-selection", start, "owned root/expected OpenCode PID unavailable", fmt.Errorf("pid=%v root=%v ids=%v", err, rootErr, idsErr))
		return
	}
	threads, err := criuSourceThreads(mode)
	if err != nil {
		record("sysbox-lazy-failed-service-thread-selection", start, "owned cgroup threads unavailable", err)
		return
	}
	sort.Ints(threads)
	selected := map[int]string{}
	leaders := map[int]bool{}
	for _, tid := range threads {
		status, readErr := os.ReadFile(fmt.Sprintf("/proc/%d/status", tid))
		group, groupErr := criuProcStatusIDs(string(status), "Tgid")
		if readErr != nil || groupErr != nil || len(group) != 1 {
			continue
		}
		leader := group[0]
		matches, known := leaders[leader]
		if !known {
			status, readErr = os.ReadFile(fmt.Sprintf("/proc/%d/status", leader))
			ids, idsErr := criuProcStatusIDs(string(status), "NSpid")
			// Match at the sandbox's PID namespace depth, not the innermost
			// namespace of an unrelated nested Docker process.
			matches = readErr == nil && idsErr == nil && len(ids) >= len(rootIDs) && ids[len(rootIDs)-1] == pid
			leaders[leader] = matches
		}
		if matches && len(selected) < 128 {
			selected[tid] = "opencode"
		}
	}
	for _, daemon := range criuLazyDaemons {
		if daemon.command != nil && daemon.command.Process != nil {
			selected[daemon.command.Process.Pid] = "owned-lazy-page-service"
		}
	}
	identities := map[int]string{}
	for tid := range selected {
		if identity, err := criuSysboxThreadStart(tid); err == nil {
			identities[tid] = identity
		}
	}
	record("sysbox-lazy-failed-service-thread-selection", start, asJSON(map[string]any{
		"runtime_id": id, "phase": phase, "expected_opencode_pid": pid, "sandbox_pid_namespace_depth": len(rootIDs),
		"selected_threads": selected, "live_thread_start_times": identities,
		"scope": "read-only owned-cgroup OpenCode threads and already-created page services; at most 128 application threads; sequential non-atomic proc observations",
	}), nil)
	for sample := 0; sample < 2; sample++ {
		if sample != 0 {
			time.Sleep(100 * time.Millisecond)
		}
		for tid, identity := range identities {
			observed, err := criuSysboxThreadStart(tid)
			if err != nil || observed != identity {
				continue
			}
			output := map[string]any{"tid": tid, "role": selected[tid], "sample": sample, "start_time": identity, "runtime_id": id, "phase": phase}
			for name, limit := range map[string]int64{"comm": 80, "stat": 768, "wchan": 128, "syscall": 512, "stack": 1536} {
				file, readErr := os.Open(fmt.Sprintf("/proc/%d/%s", tid, name))
				if readErr != nil {
					output[name] = "unavailable: " + readErr.Error()
					continue
				}
				data, readErr := io.ReadAll(io.LimitReader(file, limit+1))
				file.Close()
				if readErr != nil {
					output[name] = "unavailable: " + readErr.Error()
				} else {
					if int64(len(data)) > limit {
						data = append(data[:limit], []byte(" [truncated]")...)
					}
					output[name] = strings.TrimSpace(string(data))
				}
			}
			observed, err = criuSysboxThreadStart(tid)
			if err == nil && observed == identity {
				record("sysbox-lazy-failed-service-thread-state", start, asJSON(output), nil)
			}
			if selected[tid] == "owned-lazy-page-service" {
				criuSysboxFailedServiceFDInfo(tid, identity, sample, id, phase)
			}
		}
	}
}

// Kernel fdinfo distinguishes queued events from already-consumed faults and
// shows which userfaultfds the owned daemon's epoll actually watches. Read
// only those two descriptor classes, with a fixed budget and stable PID.
func criuSysboxFailedServiceFDInfo(pid int, identity string, sample int, id, phase string) {
	start := time.Now()
	entries, err := os.ReadDir(fmt.Sprintf("/proc/%d/fd", pid))
	if err != nil {
		record("sysbox-lazy-failed-service-fdinfo-selection", start, "owned page daemon descriptors unavailable", err)
		return
	}
	count := 0
	for _, entry := range entries {
		if count == 128 {
			break
		}
		observed, err := criuSysboxThreadStart(pid)
		if err != nil || observed != identity {
			return
		}
		target, err := os.Readlink(fmt.Sprintf("/proc/%d/fd/%s", pid, entry.Name()))
		if err != nil || (target != "anon_inode:[userfaultfd]" && target != "anon_inode:[eventpoll]") {
			continue
		}
		count++
		output := map[string]any{"pid": pid, "start_time": identity, "sample": sample, "runtime_id": id, "phase": phase, "fd": entry.Name(), "descriptor_type": target,
			"scope": "read-only owned daemon userfaultfd/eventpoll fdinfo; sequential non-atomic descriptor observations; at most 128 descriptors and 4096 bytes each"}
		file, readErr := os.Open(fmt.Sprintf("/proc/%d/fdinfo/%s", pid, entry.Name()))
		if readErr != nil {
			output["fdinfo"] = "unavailable: " + readErr.Error()
		} else {
			data, readErr := io.ReadAll(io.LimitReader(file, 4097))
			file.Close()
			if readErr != nil {
				output["fdinfo"] = "unavailable: " + readErr.Error()
			} else {
				if len(data) > 4096 {
					data = append(data[:4096], []byte(" [truncated]")...)
				}
				output["fdinfo"] = strings.TrimSpace(string(data))
			}
		}
		observed, err = criuSysboxThreadStart(pid)
		if err == nil && observed == identity {
			record("sysbox-lazy-failed-service-fdinfo", start, asJSON(output), nil)
		}
	}
	record("sysbox-lazy-failed-service-fdinfo-selection", start, asJSON(map[string]any{"pid": pid, "sample": sample, "selected_descriptors": count, "bounded_at": 128}), nil)
}

// Docker's shim may discard nsexec's detailed error. A direct, read-only exec
// uses the same saved OCI state and writes runc's native debug stream to the
// owned workspace before the failed source is removed.
func criuSysboxLazyInnerExecDiagnostic(id, inner string) {
	start := time.Now()
	script := `container_id=$(docker inspect --format '{{.Id}}' "$1") || exit 1
for state_root in /run/docker/runtime-runc/moby /run/docker/containerd/runc/moby /run/containerd/runc/moby; do
    if [ -f "$state_root/$container_id/state.json" ]; then
        cat "$state_root/$container_id/state.json"
        runc --debug --log /workspace/lazy-inner-runc.log --log-format json --root "$state_root" exec "$container_id" /lab get /state
        status=$?
        cat /workspace/lazy-inner-runc.log
        exit "$status"
    fi
done
find /run -name state.json -print
exit 1`
	out, err := criuRunc("exec", id, "/bin/sh", "-c", script, "lazy-inner-diagnostic", inner)
	record("sysbox-lazy-direct-inner-runc-exec", start, out, err)
}

// Capture evidence before criuCase deletes the failed source. Late inspection
// cannot explain a failed exec once Docker and its namespace handles are gone.
func criuSysboxLazyFailureDiagnostic(id, inner string) {
	if os.Getenv("CRIU_SYSBOX_LAZY") != "1" {
		return
	}
	start := time.Now()
	dir, mode := "/experiment/sysbox", "sysbox"
	if id == "copy-sysbox" {
		dir, mode = "/experiment/sysbox-copy", "sysbox-copy"
	}
	paths := []string{filepath.Join(dir, "upper/workspace/docker.log"), filepath.Join(dir, "upper/workspace/stdio.log")}
	innerRuntimeLogs, _ := filepath.Glob(filepath.Join(dir, "upper/run/docker/containerd/daemon/io.containerd.runtime.v2.task/moby/*/log.json"))
	paths = append(paths, innerRuntimeLogs...)
	containerLogs, _ := filepath.Glob(filepath.Join(dir, "upper/var/lib/docker/containers/*/*-json.log"))
	containerConfigs, _ := filepath.Glob(filepath.Join(dir, "upper/var/lib/docker/containers/*/config.v2.json"))
	paths = append(paths, containerLogs...)
	paths = append(paths, containerConfigs...)
	for _, d := range criuLazyDaemons {
		paths = append(paths, filepath.Join(filepath.Dir(d.logPath), "lazy-pages.log"), d.logPath)
	}
	for _, path := range paths {
		criuLazyLogRecords("sysbox-lazy-failed-exec-log", start, path, nil)
	}
	threads, err := criuSourceThreads(mode)
	if err != nil {
		record("sysbox-lazy-failed-exec-processes", start, "", err)
		return
	}
	seen := map[int]bool{}
	processes := []map[string]any{}
	for _, tid := range threads {
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", tid))
		if err != nil {
			continue
		}
		pid := 0
		status := []string{}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "Tgid:") {
				pid, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "Tgid:")))
			}
			if strings.HasPrefix(line, "State:") || strings.HasPrefix(line, "NSpid:") || strings.HasPrefix(line, "Uid:") {
				status = append(status, line)
			}
		}
		if pid <= 0 || seen[pid] {
			continue
		}
		seen[pid] = true
		// cgroup.threads includes every thread. Record the leader's namespace
		// PIDs rather than those of the first thread encountered in that group.
		if leader, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid)); err == nil {
			status = nil
			for _, line := range strings.Split(string(leader), "\n") {
				if strings.HasPrefix(line, "State:") || strings.HasPrefix(line, "NSpid:") || strings.HasPrefix(line, "Uid:") {
					status = append(status, line)
				}
			}
		}
		comm, _ := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
		stat, _ := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		processes = append(processes, map[string]any{"pid": pid, "comm": strings.TrimSpace(string(comm)), "status": status, "stat": strings.TrimSpace(string(stat))})
	}
	record("sysbox-lazy-failed-exec-processes", start, asJSON(processes), nil)
}
