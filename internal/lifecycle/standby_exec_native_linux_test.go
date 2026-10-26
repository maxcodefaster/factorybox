//go:build linux && (amd64 || arm64)

package lifecycle

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Exercise the consumer's actual nested runc without Docker image export,
// networking, a Connector, or any nested process surviving the checkpoint.
func TestNativeStandbyExec(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_STANDBY_EXEC") != "1" {
		t.Skip("requires the pinned native consumer runc input")
	}
	status := boundedLossStatusNow(t)
	var root string
	source := standbyFixture(t, status, "standby-exec", func(path string) {
		root = path
		runtime, err := os.ReadFile("/nested-runc")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(root+"/bin/nested-runc", runtime, 0755); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("cp", "-a", "/busybox", root+"/inner-rootfs").CombinedOutput(); err != nil {
			t.Fatal(string(out), err)
		}
	})
	guest := func(script string) (string, error) {
		t.Helper()
		out, err := exec.Command(status.Lifecycle.Runtime, "--root", status.Lifecycle.Root+"/runtime",
			"exec", source.Record.RuntimeID, "/bin/sh", "-ec", script).CombinedOutput()
		return string(out), err
	}
	setup := `mkdir -p /sys/fs/cgroup/init.scope /inner-bundle
printf '1\n' > /sys/fs/cgroup/init.scope/cgroup.procs
printf '%s\n' "$$" > /sys/fs/cgroup/init.scope/cgroup.procs
for controller in cpuset cpu io memory pids; do
  printf '+%s\n' "$controller" > /sys/fs/cgroup/cgroup.subtree_control
done
mkdir /sys/fs/cgroup/nested
for controller in cpuset cpu io memory pids; do
  printf '+%s\n' "$controller" > /sys/fs/cgroup/nested/cgroup.subtree_control
done
/bin/nested-runc --version
/bin/nested-runc spec --bundle /inner-bundle`
	before, err := guest(setup)
	if err != nil {
		t.Fatal("prepare nested runtime", before, err)
	}
	data, err := os.ReadFile(root + "/inner-bundle/config.json")
	if err != nil {
		t.Fatal(err)
	}
	var spec map[string]any
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatal(err)
	}
	spec["root"].(map[string]any)["path"] = "/inner-rootfs"
	process := spec["process"].(map[string]any)
	process["terminal"] = false
	process["args"] = []string{"/bin/sleep", "300"}
	spec["linux"].(map[string]any)["cgroupsPath"] = "/nested/child"
	data, err = json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root+"/inner-bundle/config.json", data, 0600); err != nil {
		t.Fatal(err)
	}
	run := `/bin/nested-runc --root /run/nested-runc --log /tmp/nested-runc.log --debug run --detach --bundle /inner-bundle child </dev/null >/tmp/nested-create.stdout 2>/tmp/nested-create.stderr`
	execute := `/bin/nested-runc --root /run/nested-runc --log /tmp/nested-runc.log --debug exec child /bin/echo nested-exec-ready`
	remove := `/bin/nested-runc --root /run/nested-runc delete --force child`
	for _, script := range []string{run, execute, remove} {
		out, err := guest(script)
		before += "\n" + out
		if err != nil {
			log, _ := guest("cat /tmp/nested-runc.log")
			t.Fatal("nested control before Pause", script, out, log, err)
		}
	}
	if !strings.Contains(before, "nested-exec-ready") {
		t.Fatal("positive nested exec control missing", before)
	}
	snapshot := strings.Repeat("e", 32)
	paused := boundedLossView(t, "pause", "--snapshot-id", snapshot, source.Record.ID)
	if paused.Record.Phase != "paused" || paused.Record.Pause == nil || paused.Record.Pause.CompletedAt == "" {
		t.Fatal("standby Pause did not complete", paused)
	}
	resumed := boundedLossView(t, "resume", "--snapshot-id", snapshot, source.Record.ID)
	if resumed.Record.Phase != "running" || resumed.Record.RuntimeID != source.Record.RuntimeID {
		t.Fatal("standby did not resume its original runtime", resumed)
	}
	if out, err := guest(run); err != nil {
		log, _ := guest("cat /tmp/nested-runc.log")
		t.Fatal("create new nested container after Resume", out, log, err)
	}
	after, execErr := guest(execute)
	if execErr != nil && os.Getenv("FACTORYBOX_STANDBY_EXEC_TRACE") == "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		trace := exec.CommandContext(ctx, "/exec-trace/usr/bin/strace",
			"-f", "-qq", "-ttt", "-s", "256", "-o", "/output/standby-exec.strace",
			status.Lifecycle.Runtime, "--root", status.Lifecycle.Root+"/runtime",
			"exec", source.Record.RuntimeID, "/bin/sh", "-ec", execute)
		trace.Env = append(os.Environ(),
			"LD_LIBRARY_PATH=/exec-trace/usr/lib/aarch64-linux-gnu:/exec-trace/usr/lib/x86_64-linux-gnu")
		out, err := trace.CombinedOutput()
		t.Log("diagnostic traced exec", string(out), err)
	}
	log, _ := guest("cat /tmp/nested-runc.log")
	boundedLossSave(t, "standby-exec.json", map[string]any{
		"passed": execErr == nil && after == "nested-exec-ready\n",
		"before": before, "after": after, "runtime_log": log,
		"production_qualified": false,
	})
	if execErr != nil || after != "nested-exec-ready\n" {
		t.Fatal("new nested container exec after Resume", after, log, execErr)
	}
	if out, err := guest(remove); err != nil {
		t.Fatal("delete owned nested container", out, err)
	}
}
