//go:build linux && factorybox_worker_probe

package devx_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"factorybox/internal/lifecycle"
)

func recordSessionTopology(t *testing.T, source lifecycle.View, filename string) {
	t.Helper()
	if source.Runtime == nil || source.Runtime.PID <= 1 || source.Record.RuntimeID != "fbx-"+source.Record.Owner {
		t.Fatal("session diagnostic lacks its exact owned source")
	}
	var rows []map[string]string
	seen := map[int]bool{}
	root := "/sys/fs/cgroup/" + source.Record.RuntimeID
	directories := 0
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || !entry.IsDir() {
			return walkErr
		}
		directories++
		if directories > 4096 {
			return fmt.Errorf("owned cgroup topology exceeds its bound")
		}
		data, err := os.ReadFile(path + "/cgroup.procs")
		if err != nil {
			return err
		}
		for _, text := range strings.Fields(string(data)) {
			pid, err := strconv.Atoi(text)
			if err != nil || pid <= 1 || len(seen) >= 4096 {
				return fmt.Errorf("invalid or excessive owned process inventory")
			}
			if seen[pid] {
				continue
			}
			seen[pid] = true
			status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
			if os.IsNotExist(err) {
				continue
			}
			if err != nil || len(status) > 16<<10 {
				return fmt.Errorf("bounded owned process status: %v", err)
			}
			row := map[string]string{"worker_pid": text}
			for _, line := range strings.Split(string(status), "\n") {
				key, value, _ := strings.Cut(line, ":")
				if key == "Name" || key == "PPid" || key == "NStgid" || key == "NSpgid" || key == "NSsid" {
					row[key] = strings.TrimSpace(value)
				}
			}
			row["pid_namespace"], _ = os.Readlink(fmt.Sprintf("/proc/%d/ns/pid", pid))
			if parent, err := strconv.Atoi(row["PPid"]); err == nil && parent > 0 {
				row["parent_pid_namespace"], _ = os.Readlink(fmt.Sprintf("/proc/%d/ns/pid", parent))
			}
			for fd := 0; fd < 3; fd++ {
				row[fmt.Sprintf("stdio_%d", fd)], _ = os.Readlink(fmt.Sprintf("/proc/%d/fd/%d", pid, fd))
			}
			rows = append(rows, row)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(rows, "", "  ")
	if err != nil || os.WriteFile("/output/"+filename, data, 0600) != nil {
		t.Fatal("retain owned session topology", err)
	}
}

func TestNativeSessionTopology(t *testing.T) {
	if os.Getenv("FACTORYBOX_DEVX_SESSION_TOPOLOGY") != "1" {
		t.Fatal("requires the exact bounded session diagnostic fixture")
	}
	f, source, start := prepareConsumerFixture(t)
	if os.Getenv("FACTORYBOX_DEVX_MINIMAL") == "1" {
		minimalWorkerState(t, source, "", 180*time.Second)
	} else {
		f.useful("prepared-start-first-use", source, start, "")
	}
	recordSessionTopology(t, source, "session-topology.json")
	readableConsumerOutput(t)
	t.Log("owned live consumer topology recorded; no checkpoint/restore qualification")
}
