//go:build linux && (amd64 || arm64)

package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"factorybox/internal/sandboxnetwork"
	"factorybox/internal/sysboxcapture"
)

// Exercise the actual second public capture without Docker or a consumer image
// export. Restored non-dumpable tasks must remain non-dumpable and checkpointable.
func TestNativeStandbyRepeat(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_STANDBY_REPEAT") != "1" {
		t.Skip("requires the owned repeated-pause fixture")
	}
	result := map[string]any{"passed": false, "production_qualified": false}
	var cases []map[string]any
	defer func() {
		result["cases"] = cases
		boundedLossSave(t, "standby-repeat.json", result)
	}()
	status := boundedLossStatusNow(t)
	standbyRepeatForeignNamespace(t)
	result["foreign_namespace_refused"] = true
	for index, name := range []string{"inspectable", "protected"} {
		protected := index == 1
		entry := map[string]any{"name": name, "protected": protected}
		cases = append(cases, entry)
		source := standbyRepeatFixture(t, status, "standby-repeat-"+name, protected, false, false)
		runtimeID := source.Record.RuntimeID
		initial := standbyRepeatRequest(t, status, source, "ready", protected)
		entry["initial_actor"] = initial
		var observations []map[string]any
		entry["observations"] = &observations
		for cycle := 1; cycle <= 2; cycle++ {
			observation := map[string]any{"cycle": cycle, "before": standbyRepeatNamespaces(t, source.Runtime.PID)}
			observations = append(observations, observation)
			snapshot := strings.Repeat(string(rune('a'+index*2+cycle)), 32)
			out, err := boundedLossCommand("--worker-run-root", "/run/factorybox",
				"pause", "--snapshot-id", snapshot, source.Record.ID)
			if err != nil {
				observation["pause_error"] = err.Error() + "\n" + string(out)
				t.Fatalf("%s cycle %d public Pause failed: %v\n%s", name, cycle, err, out)
			}
			var paused View
			if err := json.Unmarshal(out, &paused); err != nil {
				t.Fatal(err)
			}
			if paused.Record.Phase != "paused" || paused.Record.Pause == nil ||
				paused.Record.Pause.CompletedAt == "" || paused.Record.Pause.After.Anon != 0 ||
				!paused.Record.Pause.Snapshot.FileBacked || paused.Record.Pause.Snapshot.ResidentBytesAfter != 0 {
				t.Fatal("repeated Pause did not commit and release memory", paused)
			}
			observation["memory_after_pause"] = paused.Record.Pause.After
			observation["snapshot_after_pause"] = paused.Record.Pause.Snapshot
			source = boundedLossView(t, "resume", "--snapshot-id", snapshot, source.Record.ID)
			if source.Record.Phase != "running" || source.Runtime == nil || source.Record.RuntimeID != runtimeID {
				t.Fatal("repeated Resume replaced the original Sandbox", source)
			}
			observation["after"] = standbyRepeatNamespaces(t, source.Runtime.PID)
			response := standbyRepeatRequest(t, status, source, fmt.Sprint(cycle), protected)
			if response["nonce"] != initial["nonce"] || response["count"] != float64(cycle+1) {
				t.Fatal("restored actor lost its original memory", initial, response)
			}
			observation["actor"] = response
		}
		entry["passed"] = true
	}
	result["passed"] = true
}

func standbyRepeatNamespaces(t *testing.T, pid int) map[string]any {
	t.Helper()
	user, err := os.Readlink(fmt.Sprintf("/proc/%d/ns/user", pid))
	if err != nil {
		t.Fatal(err)
	}
	fd, err := unix.Open(fmt.Sprintf("/proc/%d/ns/net", pid), unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	owner, err := unix.IoctlRetInt(fd, unix.NS_GET_USERNS)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(owner)
	actual, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", owner))
	if err != nil || actual != user {
		t.Fatal("network namespace does not belong to the target user namespace", actual, user, err)
	}
	return map[string]any{"pid": pid, "user": user, "net_owner_userns": actual}
}

func standbyRepeatFixture(t *testing.T, status boundedLossStatus, id string, protected, factoryMapping, nested bool) View {
	view, _ := standbyRepeatFixtureWithBase(t, status, id, protected, factoryMapping, nested, nil)
	return view
}

func standbyRepeatFixtureWithBase(t *testing.T, status boundedLossStatus, id string, protected, factoryMapping, nested bool, base *Base) (View, func()) {
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
	shared := os.Getenv("FACTORYBOX_STANDBY_PEERS_SOURCE") == "1"
	var bound sysboxcapture.FilesystemTree
	if base == nil {
		standbyRepeatLower(t, bundle+"/lower", nested, shared)
	} else {
		var err error
		bound, err = sysboxcapture.BindFilesystemBase(base.Tree, bundle+"/lower")
		if err != nil {
			t.Fatal(err)
		}
	}
	var sourceDirectory string
	if shared {
		sourceDirectory = "/peer-source/" + id
		if err := os.MkdirAll(sourceDirectory, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chown(sourceDirectory, 101000, 101000); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(sourceDirectory+"/held", []byte(id), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chown(sourceDirectory+"/held", 101000, 101000); err != nil {
			t.Fatal(err)
		}
	}
	if err := unix.Mount("overlay", bundle+"/rootfs", "overlay", 0,
		"index=off,lowerdir="+bundle+"/lower,upperdir="+bundle+"/upper,workdir="+bundle+"/work"); err != nil {
		t.Fatal(err)
	}
	var mount unix.Statx_t
	if err := unix.Statx(unix.AT_FDCWD, bundle+"/rootfs", unix.AT_SYMLINK_NOFOLLOW, unix.STATX_MNT_ID, &mount); err != nil ||
		mount.Mask&unix.STATX_MNT_ID == 0 || mount.Mnt_id == 0 {
		t.Fatal("pin private peer overlay", err)
	}
	released := false
	release := func() {
		t.Helper()
		if released {
			return
		}
		var current unix.Statx_t
		if err := unix.Statx(unix.AT_FDCWD, bundle+"/rootfs", unix.AT_SYMLINK_NOFOLLOW, unix.STATX_MNT_ID, &current); err != nil ||
			current.Mnt_id != mount.Mnt_id {
			t.Error("refuse changed repeat control overlay", err)
			return
		}
		if err := unix.Unmount(bundle+"/rootfs", 0); err != nil {
			t.Error("release owned repeat control overlay", err)
			return
		}
		if base != nil {
			if err := sysboxcapture.RetireImmutableAlias(bundle+"/lower", bound); err != nil {
				t.Error("release owned repeat control base alias", err)
				return
			}
		}
		released = true
	}
	t.Cleanup(release)
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
	process["args"] = []string{"/bin/sh", "-c", "exec /bin/env FACTORYBOX_REPEAT_ACTOR=1 /bin/repeat-actor -test.run=^TestStandbyRepeatActor$ -test.timeout=0 </dev/null >/repeat-actor.log 2>&1"}
	process["env"] = []string{"HOME=/root", "PATH=/bin:/sbin:/usr/bin:/usr/sbin",
		fmt.Sprintf("FACTORYBOX_REPEAT_NONDUMPABLE=%t", protected)}
	spec["annotations"] = map[string]string{}
	if base != nil {
		spec["annotations"].(map[string]string)[baseAnnotation] = base.ID
	}
	if shared {
		process["env"] = append(process["env"].([]string), "FACTORYBOX_REPEAT_SHARED=1")
		data, err := json.Marshal(map[string]string{"source": sourceDirectory, "destination": "/workspace/project"})
		if err != nil {
			t.Fatal(err)
		}
		spec["annotations"].(map[string]string)[sysboxcapture.SharedSourceAnnotation] = string(data)
		spec["mounts"] = append(spec["mounts"].([]any), map[string]any{
			"type": "bind", "source": sourceDirectory, "destination": "/workspace/project",
			"options": []string{"rbind", "rw", "rprivate", "nosuid", "nodev"},
		})
	}
	spec["root"].(map[string]any)["readonly"] = false
	if os.Getenv("FACTORYBOX_STANDBY_PEERS_NETWORK") == "1" {
		annotations, _ := spec["annotations"].(map[string]string)
		if annotations == nil {
			annotations = map[string]string{}
		}
		annotations[sandboxnetwork.Annotation] = "true"
		spec["annotations"] = annotations
	}
	if factoryMapping {
		linux := spec["linux"].(map[string]any)
		linux["uidMappings"] = []map[string]any{{"containerID": 0, "hostID": 100000, "size": 65536}}
		linux["gidMappings"] = []map[string]any{{"containerID": 0, "hostID": 100000, "size": 65536}}
	}
	limit := 128
	if os.Getenv("FACTORYBOX_STANDBY_PEERS_DOCKER") == "1" {
		limit = 384 // The actual consumer's finite task ceiling.
	}
	spec["linux"].(map[string]any)["resources"] = map[string]any{
		"pids": map[string]any{"limit": limit}, "devices": []map[string]any{{"allow": true, "access": "rwm"}},
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
			t.Error("delete owned repeat control", err)
		}
	})
	return view, release
}

func standbyRepeatLower(t *testing.T, lower string, nested, shared bool) {
	t.Helper()
	copies := [][]string{{"-a", "/busybox", lower}, {"/probe.test", lower + "/bin/repeat-actor"}}
	if nested {
		copies = append(copies, []string{"/nested-runc", lower + "/bin/nested-runc"}, []string{"-a", "/busybox", lower + "/inner-rootfs"})
	}
	if os.Getenv("FACTORYBOX_STANDBY_PEERS_DOCKER") == "1" {
		copies = append(copies, []string{"-a", "/docker-input/.", lower + "/bin/"})
	}
	for _, args := range copies {
		if out, err := exec.Command("cp", args...).CombinedOutput(); err != nil {
			t.Fatal("copy private actor fixture", string(out), err)
		}
	}
	if os.Getenv("FACTORYBOX_STANDBY_PEERS_DOCKER") == "1" {
		// Match the actual Alpine consumer's /var/run alias. BusyBox's
		// minimal image has no /run or /var/run at all.
		if err := os.MkdirAll(lower+"/run", 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("../run", lower+"/var/run"); err != nil {
			t.Fatal(err)
		}
	}
	if shared {
		if err := os.MkdirAll(lower+"/workspace/project", 0755); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStandbyRepeatActor(t *testing.T) {
	if os.Getenv("FACTORYBOX_REPEAT_ACTOR") != "1" {
		t.Skip("owned repeated-pause Go process only")
	}
	if os.Getpid() != 1 {
		t.Fatal("Go actor must be the Sandbox init")
	}
	if os.Getenv("FACTORYBOX_REPEAT_NONDUMPABLE") == "true" {
		if err := unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0); err != nil {
			t.Fatal(err)
		}
	}
	if os.Getenv("FACTORYBOX_REPEAT_SHARED") == "1" {
		source, err := os.Open("/workspace/project/held")
		if err != nil {
			t.Fatal(err)
		}
		defer source.Close()
	}
	nonce, last, count := fmt.Sprint(time.Now().UnixNano()), "", 0
	for {
		data, _ := os.ReadFile("/repeat-request")
		if request := string(data); request != "" && request != last {
			for range 4 {
				if out, err := exec.Command("/bin/true").CombinedOutput(); err != nil {
					t.Fatal("Go subprocess after restore", string(out), err)
				}
			}
			dumpable, err := unix.PrctlRetInt(unix.PR_GET_DUMPABLE, 0, 0, 0, 0)
			if err != nil {
				t.Fatal(err)
			}
			count++
			data, err := json.Marshal(map[string]any{"request": request, "dumpable": dumpable,
				"nonce": nonce, "count": count})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile("/repeat-response.pending", data, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename("/repeat-response.pending", "/repeat-response"); err != nil {
				t.Fatal(err)
			}
			last = request
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func standbyRepeatRequest(t *testing.T, status boundedLossStatus, source View, request string, protected bool) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()
	script := `printf '%s' "$1" > /repeat-request
for iteration in $(seq 1 500); do
  if grep -q "\"request\":\"$1\"" /repeat-response 2>/dev/null; then
    cat /repeat-response
    exit 0
  fi
  sleep 0.01
done
cat /repeat-actor.log >&2
exit 1`
	data, err := exec.CommandContext(ctx, status.Lifecycle.Runtime, "--root", status.Lifecycle.Root+"/runtime",
		"exec", source.Record.RuntimeID, "/bin/sh", "-ec", script, "repeat-request", request).CombinedOutput()
	if err != nil {
		t.Fatalf("Go actor did not acknowledge %q: %v: %s", request, err, data)
	}
	var response map[string]any
	if err := json.Unmarshal(data, &response); err != nil || response["request"] != request {
		t.Fatal("Go actor response", string(data), err)
	}
	expected := float64(1)
	if protected {
		expected = 0
	}
	nonce, validNonce := response["nonce"].(string)
	if response["dumpable"] != expected || !validNonce || nonce == "" {
		t.Fatal("restored process changed its inspection policy", response)
	}
	return response
}

func TestStandbyRepeatForeignNamespace(t *testing.T) {
	if os.Getenv("FACTORYBOX_REPEAT_FOREIGN_NAMESPACE") != "1" {
		t.Skip("owned namespace-permission control only")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := unix.Setns(3, unix.CLONE_NEWNET); !errors.Is(err, unix.EPERM) {
		t.Fatal("a preopened handle must not grant access to a parent-owned namespace", err)
	}
}

func standbyRepeatForeignNamespace(t *testing.T) {
	t.Helper()
	net, err := os.Open("/proc/self/ns/net")
	if err != nil {
		t.Fatal(err)
	}
	defer net.Close()
	command := exec.Command("/probe.test", "-test.run=^TestStandbyRepeatForeignNamespace$")
	command.Env = append(os.Environ(), "FACTORYBOX_REPEAT_FOREIGN_NAMESPACE=1")
	command.ExtraFiles = []*os.File{net}
	command.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags:                 unix.CLONE_NEWUSER,
		UidMappings:                []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Geteuid(), Size: 1}},
		GidMappings:                []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Getegid(), Size: 1}},
		GidMappingsEnableSetgroups: false,
	}
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatal("foreign namespace permission control", string(out), err)
	}
}
