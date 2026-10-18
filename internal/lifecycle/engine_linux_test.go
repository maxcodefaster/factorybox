//go:build linux

package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func nativeEngine(t *testing.T, program string) (*Engine, string) {
	t.Helper()
	if os.Getenv("FACTORYBOX_NATIVE_LIFECYCLE") != "1" {
		t.Skip("requires the owned native Sysbox fixture")
	}
	base, err := os.MkdirTemp("/var/lib/sysbox", "factorybox-lifecycle-")
	if err != nil {
		t.Fatal(err)
	}
	// All paths are inside the fixture's owned volume. The outer runner owns
	// its cleanup, including artifacts from a failed runtime operation.
	bundle := filepath.Join(base, "bundle")
	if err := os.Mkdir(bundle, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"upper", "work", "rootfs"} {
		if err := os.Mkdir(filepath.Join(bundle, name), 0755); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("cp", "-a", "--reflink=auto", "/busybox", bundle+"/lower")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("prepare rootfs: %s: %v", out, err)
	}
	if err := unix.Mount("overlay", bundle+"/rootfs", "overlay", 0,
		"lowerdir="+bundle+"/lower,upperdir="+bundle+"/upper,workdir="+bundle+"/work"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := unix.Unmount(bundle+"/rootfs", 0); err != nil {
			t.Errorf("release test overlay: %v", err)
		}
	})
	cmd = exec.Command("/out/sysbox-runc", "spec", "--bundle", bundle)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create OCI spec: %s: %v", out, err)
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
	process["args"] = []string{"/bin/sh", "-c", "exec " + program + " </dev/null >/dev/null 2>&1"}
	process["env"] = []string{"PATH=/bin:/sbin:/usr/bin:/usr/sbin", "HOME=/root"}
	spec["root"].(map[string]any)["readonly"] = false
	groupName := "factorybox-native-" + filepath.Base(base)
	linux := spec["linux"].(map[string]any)
	linux["cgroupsPath"] = "/" + groupName
	linux["resources"] = map[string]any{
		"pids":    map[string]any{"limit": 64},
		"devices": []map[string]any{{"allow": true, "access": "rwm"}},
	}
	data, err = json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundle+"/config.json", data, 0600); err != nil {
		t.Fatal(err)
	}
	engine, err := Open(Config{Root: base + "/controller", Runtime: "/out/sysbox-runc",
		RuntimeSHA256: os.Getenv("FACTORYBOX_RUNTIME_SHA256")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { engine.Close() })
	return engine, bundle
}

func TestNativeStartRecoveryAndDelete(t *testing.T) {
	engine, bundle := nativeEngine(t, "sleep 300")
	ctx := context.Background()
	input, err := os.ReadFile(bundle + "/config.json")
	if err != nil {
		t.Fatal(err)
	}
	view, err := engine.Start(ctx, "workspace", bundle)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := engine.Delete(ctx, "workspace"); err != nil {
			t.Errorf("delete owned test sandbox: %v", err)
		}
	})
	original := *view.Runtime
	if original.PID <= 1 || view.Record.Start.CompletedAt == "" {
		t.Fatalf("start lacks actual process and durable outcome: %+v", view)
	}
	record := view.Record
	// Reproduce a controller losing its successful response after external
	// creation. Recovery must adopt this exact process rather than start again.
	record.Phase, record.RuntimeCreated, record.Start.CompletedAt = "starting", "", ""
	if err := engine.write(record); err != nil {
		t.Fatal(err)
	}
	view, err = engine.Start(ctx, "workspace", bundle)
	if err != nil || view.Runtime == nil || view.Runtime.PID != original.PID ||
		view.Runtime.Created != original.Created || view.Record.RuntimeID != original.ID {
		t.Fatalf("recovery did not retain the exact process: %+v: %v", view, err)
	}
	replayed, err := engine.Start(ctx, "workspace", bundle)
	if err != nil || replayed.Runtime.PID != original.PID {
		t.Fatalf("idempotent start changed process: %+v: %v", replayed, err)
	}
	if data, err := os.ReadFile(bundle + "/config.json"); err != nil || string(data) != string(input) {
		t.Fatalf("start modified its input configuration: %v", err)
	}
	command := exec.Command("/factorybox", "--root", engine.config.Root, "--runtime", engine.config.Runtime,
		"--runtime-sha256", engine.config.RuntimeSHA256, "state", "workspace")
	output, err := command.Output()
	var inspected View
	if err != nil || json.Unmarshal(output, &inspected) != nil ||
		inspected.Runtime == nil || inspected.Runtime.PID != original.PID {
		t.Fatalf("public CLI did not inspect the same process: %s: %v", output, err)
	}
	if _, err := engine.Start(ctx, "other", bundle); err == nil {
		t.Fatal("same writable rootfs accepted by a second sandbox")
	}
	if _, err := os.Lstat(engine.config.Root + "/objects/other"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("shared writable rootfs was not rejected before runtime creation intent: %v", err)
	}
	command = exec.Command("/factorybox", "--root", engine.config.Root, "--runtime", engine.config.Runtime,
		"--runtime-sha256", engine.config.RuntimeSHA256, "delete", "workspace")
	output, err = command.Output()
	var deleted View
	if err != nil || json.Unmarshal(output, &deleted) != nil ||
		deleted.Record.Phase != "deleted" || deleted.Record.Delete.CompletedAt == "" {
		t.Fatalf("public CLI delete did not retain its outcome: %s: %v", output, err)
	}
	again, err := engine.Delete(ctx, "workspace")
	if err != nil || again.Record.Delete.CompletedAt != deleted.Record.Delete.CompletedAt {
		t.Fatalf("delete replay changed its committed outcome: %+v: %v", again, err)
	}
	if _, err := os.Stat(bundle + "/rootfs/bin/sleep"); err != nil {
		t.Fatalf("deletion removed input rootfs: %v", err)
	}
	if _, err := engine.Start(ctx, "workspace", bundle); err == nil {
		t.Fatal("deleted identity was reused")
	}
	t.Logf("owned process %d reconciled without restart; tombstone retained", original.PID)
}

func TestNativeForeignIdentityPreservesRunningProcess(t *testing.T) {
	engine, bundle := nativeEngine(t, "sleep 300")
	ctx := context.Background()
	view, err := engine.Start(ctx, "owned", bundle)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := engine.Delete(ctx, "owned"); err != nil {
			t.Errorf("delete owned test sandbox: %v", err)
		}
	})
	original := view.Record
	if original.Cgroup == nil || original.Cgroup.MountID == 0 || original.Cgroup.Inode == 0 {
		t.Fatal("running sandbox lacks its recorded kernel cgroup identity")
	}
	foreign := original
	foreign.RuntimeCreated = "2000-01-01T00:00:00Z"
	if err := engine.write(foreign); err != nil {
		t.Fatal(err)
	}
	_, deleteErr := engine.Delete(ctx, "owned")
	restoreErr := engine.write(original)
	if deleteErr == nil || restoreErr != nil {
		t.Fatalf("foreign creation identity accepted: %v; restore: %v", deleteErr, restoreErr)
	}
	foreign = original
	group := *foreign.Cgroup
	group.Inode++
	foreign.Cgroup = &group
	if err := engine.write(foreign); err != nil {
		t.Fatal(err)
	}
	_, deleteErr = engine.Delete(ctx, "owned")
	restoreErr = engine.write(original)
	if deleteErr == nil || restoreErr != nil || unix.Kill(view.Runtime.PID, 0) != nil {
		t.Fatalf("foreign cgroup identity accepted: %v; restore: %v", deleteErr, restoreErr)
	}
	live, err := engine.Inspect(ctx, "owned")
	if err != nil || live.Runtime == nil || live.Runtime.PID != view.Runtime.PID ||
		unix.Kill(view.Runtime.PID, 0) != nil {
		t.Fatalf("foreign identity check changed the original process: %+v: %v", live, err)
	}
	object, _ := objectPath("owned")
	path := object + "/config.json"
	config, err := engine.root.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.root.WriteFile(path, append(config, ' '), 0600); err != nil {
		t.Fatal(err)
	}
	_, deleteErr = engine.Delete(ctx, "owned")
	restoreErr = engine.root.WriteFile(path, config, 0600)
	if deleteErr == nil || restoreErr != nil || unix.Kill(view.Runtime.PID, 0) != nil {
		t.Fatalf("changed configuration not rejected before deletion: %v; restore: %v", deleteErr, restoreErr)
	}
	t.Log("mismatched runtime creation, cgroup identity and changed OCI config rejected; original process survived")
}

func TestNativeUncertainStartDoesNotRecreate(t *testing.T) {
	engine, bundle := nativeEngine(t, "sleep 300")
	ctx := context.Background()
	object, _ := objectPath("uncertain")
	input, err := os.ReadFile(bundle + "/config.json")
	if err != nil {
		t.Fatal(err)
	}
	record, err := engine.prepare("uncertain", bundle, object, input)
	if err != nil {
		t.Fatal(err)
	}
	record.Start.Attempted = true
	record.Start.Error = "controller response lost"
	if err := engine.write(record); err != nil {
		t.Fatal(err)
	}
	view, err := engine.Start(ctx, "uncertain", bundle)
	if err == nil || !strings.Contains(err.Error(), "uncertain") ||
		view.Record.Start.Error != record.Start.Error {
		t.Fatalf("uncertain create was repeated or its evidence lost: %+v: %v", view, err)
	}
	if _, err := os.Lstat(engine.config.Root + "/runtime/" + record.RuntimeID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("uncertain replay created a runtime: %v", err)
	}
	if _, err := engine.Delete(ctx, "uncertain"); err == nil {
		t.Fatal("uncertain creation evidence was discarded")
	}
	t.Log("uncertain create neither repeated nor discarded")
}

func TestNativeControllerLockAndRuntimePin(t *testing.T) {
	engine, _ := nativeEngine(t, "sleep 300")
	lock, err := engine.lock()
	if err != nil {
		t.Fatal(err)
	}
	if other, err := engine.lock(); err == nil {
		other.Close()
		t.Fatal("concurrent controller acquired the operation lock")
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	config := engine.config
	config.RuntimeSHA256 = strings.Repeat("0", 64)
	if other, err := Open(config); err == nil {
		other.Close()
		t.Fatal("changed runtime pin was accepted")
	}
	if err := os.Rename(engine.config.Root, engine.config.Root+"-original"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(engine.config.Root, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Inspect(context.Background(), "absent"); err == nil ||
		!strings.Contains(err.Error(), "root identity changed") {
		t.Fatalf("replacement controller root accepted: %v", err)
	}
	t.Log("concurrent controller, wrong runtime digest and replacement root rejected")
}
