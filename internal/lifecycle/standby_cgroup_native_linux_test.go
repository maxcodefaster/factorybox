//go:build linux && (amd64 || arm64)

package lifecycle

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// Isolate the empty-delegation exec-placement defect without Docker or a
// multi-gigabyte consumer filesystem. The complete Docker proof remains required.
func TestNativeStandbyCgroup(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_STANDBY_CGROUP") != "1" {
		t.Skip("requires the owned paused-standby cgroup fixture")
	}
	t.Run("new-parent-from-empty-delegation", func(t *testing.T) { standbyCgroupCase(t, false) })
	t.Run("existing-empty-parent", func(t *testing.T) { standbyCgroupCase(t, true) })
	if !t.Failed() {
		boundedLossSave(t, "standby-cgroup.json", map[string]any{
			"passed": true, "new_parent_after_resume": true, "existing_empty_parent": true,
			"production_qualified": false,
		})
	}
}

func standbyCgroupCase(t *testing.T, existing bool) {
	t.Helper()
	status := boundedLossStatusNow(t)
	id := "standby-new-parent"
	if existing {
		id = "standby-existing-parent"
	}
	source := standbyCgroupFixture(t, status, id)
	guest := func(script string) (string, error) {
		t.Helper()
		out, err := exec.Command(status.Lifecycle.Runtime, "--root", status.Lifecycle.Root+"/runtime",
			"exec", source.Record.RuntimeID, "/bin/sh", "-ec", script).CombinedOutput()
		return string(out), err
	}
	diagnostic := `readlink /proc/1/ns/cgroup; cat /proc/1/cgroup /proc/self/cgroup
grep cgroup /proc/self/mountinfo
cat /sys/fs/cgroup/cgroup.controllers /sys/fs/cgroup/cgroup.subtree_control
stat -f /sys/fs/cgroup
ls -lai /sys/fs/cgroup
if test -d /sys/fs/cgroup/docker; then ls -lai /sys/fs/cgroup/docker; fi`
	setup := `mkdir -p /sys/fs/cgroup/init.scope
printf '1\n' > /sys/fs/cgroup/init.scope/cgroup.procs
printf '%s\n' "$$" > /sys/fs/cgroup/init.scope/cgroup.procs
for controller in cpuset cpu io memory pids; do
  printf '+%s\n' "$controller" > /sys/fs/cgroup/cgroup.subtree_control
done`
	parent := `mkdir /sys/fs/cgroup/docker
for controller in cpuset cpu io memory pids; do
  printf '+%s\n' "$controller" > /sys/fs/cgroup/docker/cgroup.subtree_control
done
test -f /sys/fs/cgroup/docker/cgroup.procs`
	if existing {
		setup += "\n" + parent
	} else {
		setup += `
printf '%s\n' '-cpuset -cpu -io -memory -pids' > /sys/fs/cgroup/cgroup.subtree_control
test -z "$(cat /sys/fs/cgroup/cgroup.subtree_control)"`
	}
	if out, err := guest(setup); err != nil {
		t.Fatal("prepare the same empty delegated Docker parent", out, err)
	}
	before, err := guest(diagnostic)
	if err != nil {
		t.Fatal("observe original guest cgroups", before, err)
	}
	t.Log("before Pause:\n" + before)
	requireLeaf := func(phase, observation string) {
		t.Helper()
		lines := strings.Split(observation, "\n")
		if len(lines) < 3 || lines[1] != "0::/init.scope" || lines[2] != "0::/init.scope" {
			t.Fatalf("%s exec populated the delegation root instead of the original init leaf:\n%s", phase, observation)
		}
	}
	requireLeaf("before Pause", before)
	snapshot := strings.Repeat("d", 32)
	paused := boundedLossView(t, "pause", "--snapshot-id", snapshot, source.Record.ID)
	if paused.Record.Phase != "paused" || paused.Record.Pause == nil || paused.Record.Pause.CompletedAt == "" {
		t.Fatal("the actual standby Pause did not complete", paused)
	}
	resumed := boundedLossView(t, "resume", "--snapshot-id", snapshot, source.Record.ID)
	if resumed.Record.Phase != "running" || resumed.Record.RuntimeID != source.Record.RuntimeID {
		t.Fatal("standby did not resume its original runtime", resumed)
	}
	after, err := guest(diagnostic)
	t.Log("after Resume:\n" + after)
	if err != nil {
		t.Fatal("observe resumed guest cgroups", after, err)
	}
	requireLeaf("after Resume", after)
	if !existing {
		enable := `for controller in cpuset cpu io memory pids; do
  printf '+%s\n' "$controller" > /sys/fs/cgroup/cgroup.subtree_control
done
`
		if out, err := guest(enable + parent); err != nil {
			t.Fatal("create a new delegated parent after Resume", out, err)
		}
	}
	out, err := guest(`test -f /sys/fs/cgroup/docker/cgroup.subtree_control
mkdir /sys/fs/cgroup/docker/new-child
test -f /sys/fs/cgroup/docker/new-child/cgroup.procs
/bin/sleep 100 </dev/null >/dev/null 2>&1 &
printf '%s\n' "$!" > /sys/fs/cgroup/docker/new-child/cgroup.procs
printf 'standby-cgroup-child-ready'`)
	boundedLossSave(t, id+".json", map[string]any{
		"passed": err == nil && out == "standby-cgroup-child-ready",
		"before": before, "after": after, "child_output": out,
		"production_qualified": false,
	})
	if err != nil || out != "standby-cgroup-child-ready" {
		t.Fatal("resumed empty delegation cannot admit a new child", out, err)
	}
}

// The existing reader fixture is hash-protected by its ARM64 port declaration.
// Keep it unchanged and give this writable-cgroup control its own tiny rootfs.
func standbyCgroupFixture(t *testing.T, status boundedLossStatus, id string) View {
	t.Helper()
	return standbyFixture(t, status, id, nil)
}

func standbyFixture(t *testing.T, status boundedLossStatus, id string, prepare func(string)) View {
	t.Helper()
	bundle := status.Store + "/" + id + "-bundle"
	for _, name := range []string{"", "/upper", "/work", "/rootfs"} {
		mode := os.FileMode(0755)
		if name == "" {
			mode = 0700
		}
		if err := os.Mkdir(bundle+name, mode); err != nil {
			t.Fatal(err)
		}
	}
	if out, err := exec.Command("cp", "-a", "/busybox", bundle+"/lower").CombinedOutput(); err != nil {
		t.Fatal(string(out), err)
	}
	if err := unix.Mount("overlay", bundle+"/rootfs", "overlay", 0,
		"index=off,lowerdir="+bundle+"/lower,upperdir="+bundle+"/upper,workdir="+bundle+"/work"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := unix.Unmount(bundle+"/rootfs", 0); err != nil {
			t.Error("release owned standby input overlay", err)
		}
	})
	if prepare != nil {
		prepare(bundle + "/rootfs")
	}
	if out, err := exec.Command(status.Lifecycle.Runtime, "spec", "--bundle", bundle).CombinedOutput(); err != nil {
		t.Fatal(string(out), err)
	}
	data, err := os.ReadFile(bundle + "/config.json")
	if err != nil {
		t.Fatal(err)
	}
	var spec map[string]any
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatal(err)
	}
	process := spec["process"].(map[string]any)
	process["terminal"], process["cwd"] = false, "/"
	process["args"] = []string{"/bin/sh", "-c", "exec /bin/sleep 300 </dev/null >/dev/null 2>&1"}
	process["env"] = []string{"HOME=/root", "PATH=/bin:/sbin:/usr/bin:/usr/sbin"}
	spec["root"].(map[string]any)["readonly"] = false
	for _, item := range spec["mounts"].([]any) {
		mount := item.(map[string]any)
		if mount["destination"] == "/sys/fs/cgroup" {
			mount["options"] = []string{"nosuid", "noexec", "nodev", "rw"}
		}
	}
	spec["linux"].(map[string]any)["resources"] = map[string]any{
		"pids": map[string]any{"limit": 128}, "devices": []map[string]any{{"allow": true, "access": "rwm"}},
	}
	data, err = json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundle+"/config.json", data, 0600); err != nil {
		t.Fatal(err)
	}
	view := boundedLossView(t, "start", "--bundle", bundle, id)
	t.Cleanup(func() {
		if _, err := boundedLossCommand("--worker-run-root", "/run/factorybox", "delete", id); err != nil {
			t.Error("delete owned standby cgroup control", err)
		}
	})
	return view
}
