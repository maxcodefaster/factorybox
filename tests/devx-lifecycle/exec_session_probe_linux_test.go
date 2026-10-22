//go:build linux && factorybox_worker_probe

package devx_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestNativeAttachedExecBoundary(t *testing.T) {
	if os.Getenv("FACTORYBOX_DEVX_EXEC_SESSION") != "1" || os.Getenv("FACTORYBOX_DEVX_MINIMAL") != "1" {
		t.Fatal("requires the exact owned attached Dev Container exec reproducer")
	}
	f, source, _ := prepareConsumerFixture(t)
	before := minimalWorkerState(t, source, "", 180*time.Second)
	recordSessionTopology(t, source, "session-topology.json")
	// Record only this exact source's cgroup subtree, including the inner
	// Docker task. No host-wide process inventory or numeric-PID guessing.
	var inventory []map[string]string
	seen := map[int]bool{}
	root := "/sys/fs/cgroup/" + source.Record.RuntimeID
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || !entry.IsDir() {
			return walkErr
		}
		data, err := os.ReadFile(path + "/cgroup.procs")
		if err != nil {
			return err
		}
		for _, text := range strings.Fields(string(data)) {
			pid, err := strconv.Atoi(text)
			if err != nil || pid <= 1 || len(seen) >= 4096 {
				return fmt.Errorf("owned process inventory is invalid or exceeds its bound")
			}
			if seen[pid] {
				continue
			}
			seen[pid] = true
			status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
			if os.IsNotExist(err) {
				continue // Preparation may still reap its own short-lived clients.
			}
			if err != nil || len(status) > 16<<10 {
				return fmt.Errorf("read exact owned process status: %v", err)
			}
			row := map[string]string{"worker_pid": text}
			for _, line := range strings.Split(string(status), "\n") {
				key, value, _ := strings.Cut(line, ":")
				if key == "Name" || key == "NStgid" || key == "NSpgid" || key == "NSsid" || key == "PPid" {
					row[key] = strings.TrimSpace(value)
				}
			}
			inventory = append(inventory, row)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(inventory, "", "  ")
	if err != nil || os.WriteFile("/output/attached-session-inventory.json", data, 0600) != nil {
		t.Fatal("retain exact session topology", err)
	}
	raw := func(name string, arguments ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 70*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, "/factorybox", append([]string{"--worker-run-root", "/run/factorybox"}, arguments...)...)
		out, failure := command.CombinedOutput()
		if len(out) > 1<<20 || os.WriteFile("/output/attached-"+name+".txt", out, 0600) != nil {
			t.Fatal("retain bounded public attached-session response")
		}
		return out, failure
	}
	snapshot := strings.Repeat("e", 32)
	output, rejected := raw("checkpoint", "checkpoint", "--snapshot-id", snapshot, "source")
	if rejected == nil {
		fork, failure := raw("fork", "fork", "--source", "source", "--snapshot-id", snapshot, "unsupported-branch")
		log, logErr := os.ReadFile(f.root + "/forks/unsupported-branch/materialized/images/restore.log")
		if logErr == nil && len(log) <= 2<<20 {
			if err := os.WriteFile("/output/attached-restore.log", log, 0600); err != nil {
				t.Fatal(err)
			}
		}
		if failure == nil || logErr != nil || !strings.Contains(string(log), "restore sid") {
			t.Fatalf("attached-session hypothesis differs from the historical failure: %v: %s", failure, fork)
		}
		if actual := minimalWorkerState(t, source, "", 5*time.Second); !reflect.DeepEqual(actual, before) {
			t.Fatal("failed attached-session fork changed the original RAM/files")
		}
		t.Fatal("unsupported attached-session checkpoint was admitted; independent restore fails on its session ID")
	}
	if !strings.Contains(string(output), "nested-exec-session") {
		t.Fatalf("checkpoint failed for another reason: %v: %s", rejected, output)
	}
	if _, err := os.Lstat(f.root + "/objects/source/captures/" + snapshot + "/guard/capture-worker-intent.json"); !os.IsNotExist(err) {
		t.Fatal("unsupported session entered CRIU instead of rejecting before capture", err)
	}
	if _, err := os.Lstat(f.root + "/objects/source/captures/" + snapshot + "/checkpoint.json"); !os.IsNotExist(err) {
		t.Fatal("unsupported session published a complete checkpoint", err)
	}
	if actual := minimalWorkerState(t, source, "", 5*time.Second); !reflect.DeepEqual(actual, before) {
		t.Fatal("session admission changed the original 32-MiB RAM/files")
	}
	if view := f.view("state", "source"); view.Runtime == nil || view.Runtime.PID != source.Runtime.PID || view.Record.Phase != "running" {
		t.Fatal("session admission did not return the exact original live source")
	}
	t.Log("attached cross-PID-namespace exec refused before CRIU; original RAM, files and identity remain usable")
	readableConsumerOutput(t)
}
