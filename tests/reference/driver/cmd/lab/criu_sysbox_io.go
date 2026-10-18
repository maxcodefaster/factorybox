package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Inspect only processes in the captured cgroup. A supported application
// fallback is useful only if it actually avoids CRIU's unsupported kernel FD.
func criuSysboxIOPreflight(threads []int) error {
	start := time.Now()
	seen := map[int]bool{}
	found := []map[string]any{}
	watches := []map[string]any{}
	for _, tid := range threads {
		status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", tid))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		pid := 0
		for _, line := range strings.Split(string(status), "\n") {
			if strings.HasPrefix(line, "Tgid:") {
				pid, err = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "Tgid:")))
				if err != nil {
					return err
				}
			}
		}
		if pid <= 0 || seen[pid] {
			continue
		}
		seen[pid] = true
		root := fmt.Sprintf("/proc/%d/", pid)
		entries, err := os.ReadDir(root + "fd")
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		fds := []string{}
		for _, fd := range entries {
			link, _ := os.Readlink(root + "fd/" + fd.Name())
			if strings.Contains(link, "io_uring") {
				fds = append(fds, fd.Name()+":"+link)
			}
			if strings.Contains(link, "inotify") {
				info, infoErr := os.ReadFile(root + "fdinfo/" + fd.Name())
				if infoErr != nil && !os.IsNotExist(infoErr) {
					return infoErr
				}
				comm, _ := os.ReadFile(root + "comm")
				exe, _ := os.Readlink(root + "exe")
				watches = append(watches, map[string]any{"pid": pid, "comm": strings.TrimSpace(string(comm)), "exe": exe, "fd": fd.Name(), "fdinfo": string(info)})
			}
		}
		maps, err := os.ReadFile(root + "maps")
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		mapped := strings.Contains(string(maps), "io_uring")
		if len(fds) > 0 || mapped {
			comm, _ := os.ReadFile(root + "comm")
			exe, _ := os.Readlink(root + "exe")
			found = append(found, map[string]any{"pid": pid, "comm": strings.TrimSpace(string(comm)), "exe": exe, "fds": fds, "mapped": mapped})
		}
	}
	record("sysbox-source-inotify-fdinfo", start, asJSON(watches), nil)
	var err error
	if len(found) > 0 {
		err = fmt.Errorf("captured processes retain unsupported io_uring kernel objects: %s", asJSON(found))
	}
	record("sysbox-source-snapshot-io-preflight", start, asJSON(map[string]any{"processes_checked": len(seen), "io_uring": found, "policy": "io_uring setup/enter/register return EPERM; libuv environment fallback also set"}), err)
	return err
}

func criuSysboxIOPolicyProof(id, phase string) error {
	start := time.Now()
	out, err := criuRunc("exec", id, "/lab", "io-uring-policy-check")
	record("sysbox-"+id+"-"+phase+"-io-uring-policy", start, out, err)
	return err
}
