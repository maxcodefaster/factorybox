//go:build linux

package lifecycle

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDelegatedCgroupForkNative(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_DELEGATION") != "1" {
		t.Skip("requires the owned delegated-cgroup checkpoint fixture")
	}
	engine, bundle := nativeEngine(t, "/bin/env FACTORYBOX_CAPTURE_ACTOR=1 /bin/capture-actor -test.run=^TestCaptureActor$ -test.timeout=0")
	binary, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundle+"/lower/bin/capture-actor", binary, 0755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(bundle + "/config.json")
	if err != nil {
		t.Fatal(err)
	}
	var spec map[string]any
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatal(err)
	}
	for _, value := range spec["mounts"].([]any) {
		mount := value.(map[string]any)
		if mount["destination"] == "/sys/fs/cgroup" {
			mount["options"] = []string{"nosuid", "noexec", "nodev", "rw"}
		}
	}
	data, err = json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundle+"/config.json", data, 0600); err != nil {
		t.Fatal(err)
	}
	engine.config.Criu, engine.config.CriuSHA256 = "/out/criu", os.Getenv("FACTORYBOX_CRIU_SHA256")
	engine.config.ManagerRoot = "/var/lib/sysbox/manager"
	ctx := context.Background()
	source, err := engine.Start(ctx, "delegated", bundle)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := engine.Delete(ctx, "delegated"); err != nil {
			t.Error(err)
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	var before captureActorState
	for {
		before, err = readCaptureActor(source.Runtime.PID, "get")
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("delegation actor not ready", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	control, err := engine.lock()
	if err != nil {
		t.Fatal(err)
	}
	script := `exec >/state/delegation-setup.log 2>&1
set -eux
cat /sys/fs/cgroup/cgroup.controllers
cat /sys/fs/cgroup/cgroup.subtree_control
mkdir -p /sys/fs/cgroup/init.scope
printf '1\n' > /sys/fs/cgroup/init.scope/cgroup.procs
printf '%s\n' "$$" > /sys/fs/cgroup/init.scope/cgroup.procs
for controller in cpuset cpu io memory pids; do
  printf '+%s\n' "$controller" > /sys/fs/cgroup/cgroup.subtree_control
done
mkdir /sys/fs/cgroup/docker
for controller in cpuset cpu io memory pids; do
  printf '+%s\n' "$controller" > /sys/fs/cgroup/docker/cgroup.subtree_control
done
for branch in first second; do
  mkdir "/sys/fs/cgroup/docker/$branch"
  /bin/sleep 300 </dev/null >/dev/null 2>&1 &
  printf '%s\n' "$!" > "/sys/fs/cgroup/docker/$branch/cgroup.procs"
done
`
	_, setupErr := engine.command(ctx, control, "exec", source.Record.RuntimeID, "/bin/sh", "-ec", script)
	closeErr := control.Close()
	if setupErr != nil || closeErr != nil {
		details, readErr := os.ReadFile(bundle + "/rootfs/state/delegation-setup.log")
		t.Fatalf("prepare delegated cgroups: %v %v; workload log=%s (%v)", setupErr, closeErr, details, readErr)
	}
	snapshot := strings.Repeat("e", 32)
	start := time.Now()
	captured, err := engine.Checkpoint(ctx, "delegated", snapshot)
	if err != nil || captured.ReleasedAt == "" {
		t.Fatal("capture delegated subtree", err)
	}
	t.Logf("delegated cgroup checkpoint=%s", time.Since(start))
	start = time.Now()
	forked, err := forkNativeCLI(engine, "delegated", snapshot, "delegated-fork")
	if err != nil {
		t.Fatal("restore delegated cgroup subtree", err)
	}
	t.Cleanup(func() {
		if _, err := engine.Delete(ctx, "delegated-fork"); err != nil {
			t.Error(err)
		}
	})
	after, err := readCaptureActor(forked.Runtime.PID, "get")
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("delegated restore lost private RAM", err)
	}
	for _, id := range []string{source.Record.RuntimeID, forked.Record.RuntimeID} {
		path := cgroupRoot + "/" + id + "/docker/cgroup.subtree_control"
		data, err := os.ReadFile(path)
		if err != nil || strings.TrimSpace(string(data)) != "cpuset cpu io memory pids" {
			t.Fatal("delegated controllers changed", id, string(data), err)
		}
		for _, branch := range []string{"first", "second"} {
			data, err := os.ReadFile(cgroupRoot + "/" + id + "/docker/" + branch + "/cgroup.procs")
			if err != nil || len(strings.Fields(string(data))) != 1 {
				t.Fatal("delegated child membership changed", id, branch, string(data), err)
			}
		}
	}
	t.Logf("delegated cgroup fork and first useful RAM=%s", time.Since(start))
}
