//go:build linux

package lifecycle

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Read only this native fixture's private PID namespace. This adds evidence to
// an already failed unmount; it neither retries nor changes its assertion.
func recordBusyOverlay(t *testing.T, bundle string, cause error) {
	t.Helper()
	started := time.Now()
	deadline := started.Add(2 * time.Second)
	identity, identityErr := directoryIdentity(bundle + "/rootfs")
	report := map[string]any{"schema": 1, "test": t.Name(), "bundle": bundle,
		"error": cause.Error(), "mount": identity, "at": started.UTC().Format(time.RFC3339Nano),
		"scope": "Read-only bounded owned native PID-namespace diagnostics; original unmount failure retained."}
	if identityErr != nil {
		report["identity_error"] = identityErr.Error()
	}
	maximum := 2 << 20
	read := func(path string) string {
		if maximum <= 0 || time.Now().After(deadline) {
			return "scan limit reached"
		}
		file, err := os.Open(path)
		if err != nil {
			return "unavailable: " + err.Error()
		}
		defer file.Close()
		limit := min(maximum, 16384)
		if strings.HasSuffix(path, "/mountinfo") {
			limit = min(maximum, 128<<10)
		}
		data, err := io.ReadAll(io.LimitReader(file, int64(limit)))
		maximum -= len(data)
		if err != nil && len(data) == 0 {
			return "unavailable: " + err.Error()
		}
		return string(data)
	}
	link := func(path string) string {
		if maximum <= 0 || time.Now().After(deadline) {
			return "scan limit reached"
		}
		value, err := os.Readlink(path)
		if err != nil {
			return "unavailable: " + err.Error()
		}
		maximum -= len(value)
		return value
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		report["proc_error"] = err.Error()
	}
	var processes []map[string]any
	tasksRemaining := 256
	mountsByNamespace := map[string]string{}
	for _, entry := range entries {
		if len(processes) >= 128 || time.Now().After(deadline) || maximum <= 0 {
			break
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		birth, err := processBirth(pid)
		if err != nil {
			continue
		}
		path := "/proc/" + entry.Name()
		row := map[string]any{"pid": pid, "birth": birth, "stat": read(path + "/stat"), "wchan": read(path + "/wchan"),
			"cwd": link(path + "/cwd"), "root": link(path + "/root"), "mnt_namespace": link(path + "/ns/mnt")}
		var descriptors []map[string]string
		fds, _ := os.ReadDir(path + "/fd")
		for index, fd := range fds {
			if index >= 256 || maximum <= 0 || time.Now().After(deadline) {
				break
			}
			name := fd.Name()
			if _, err := strconv.Atoi(name); err != nil {
				continue
			}
			target := link(path + "/fd/" + name)
			info := read(path + "/fdinfo/" + name)
			matchesMount := false
			for _, line := range strings.Split(info, "\n") {
				fields := strings.Fields(line)
				matchesMount = matchesMount || (len(fields) == 2 && fields[0] == "mnt_id:" && fields[1] == strconv.FormatUint(identity.MountID, 10))
			}
			if strings.HasPrefix(target, bundle) || strings.HasPrefix(target, "mnt:[") || matchesMount {
				descriptors = append(descriptors, map[string]string{"fd": name, "target": target, "fdinfo": info})
			}
		}
		row["relevant_fds"] = descriptors
		var tasks []map[string]string
		threads, _ := os.ReadDir(path + "/task")
		for _, thread := range threads {
			if tasksRemaining <= 0 || maximum <= 0 || time.Now().After(deadline) {
				break
			}
			tasksRemaining--
			root := path + "/task/" + thread.Name()
			namespace := link(root + "/ns/mnt")
			mounts, seen := mountsByNamespace[namespace]
			if !seen {
				info := read(root + "/mountinfo")
				var relevant []string
				for _, line := range strings.Split(info, "\n") {
					if strings.Contains(line, bundle) || strings.HasPrefix(line, strconv.FormatUint(identity.MountID, 10)+" ") {
						relevant = append(relevant, line)
					}
				}
				mounts = strings.Join(relevant, "\n")
				mountsByNamespace[namespace] = mounts
			}
			tasks = append(tasks, map[string]string{"tid": thread.Name(), "mnt_namespace": namespace,
				"wchan": read(root + "/wchan"), "syscall": read(root + "/syscall"), "stack": read(root + "/stack"),
				"relevant_mountinfo": mounts})
		}
		row["tasks"] = tasks
		if current, err := processBirth(pid); err == nil && current == birth {
			processes = append(processes, row)
		}
	}
	report["processes"], report["seconds"], report["read_budget_remaining"] = processes, time.Since(started).Seconds(), maximum
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Log("busy-overlay diagnostic encoding failed", err)
		return
	}
	path := filepath.Join("/output", fmt.Sprintf("busy-overlay-%d.json", started.UnixNano()))
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Log("busy-overlay diagnostic could not be saved", err)
	} else {
		t.Log("busy-overlay diagnostic", path)
	}
}
