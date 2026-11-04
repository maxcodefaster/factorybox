//go:build linux

package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"factorybox/internal/sysboxcapture"
)

// Exit after the real capture worker has stopped the original source but before
// the controller publishes capture-result/checkpoint. No completion is forged.
func stoppedCaptureDeleteFixture(t *testing.T, root string) {
	t.Helper()
	config := Config{Root: root, Runtime: "/out/sysbox-runc",
		RuntimeSHA256: os.Getenv("FACTORYBOX_RUNTIME_SHA256"),
		Criu:          "/out/criu", CriuSHA256: os.Getenv("FACTORYBOX_CRIU_SHA256"),
		ManagerRoot: "/var/lib/sysbox/manager"}
	id := "workspace"
	if os.Getenv("FACTORYBOX_NATIVE_STOPPED_CAPTURE_RETIREMENT") == "1" {
		config = boundedLossStatusNow(t).Lifecycle
		id = "stopped-capture"
		if os.Getenv("FACTORYBOX_STOPPED_CAPTURE_NATIVE_LAYOUT") == "1" {
			id = "stopped-capture-native"
		}
		if config.Root != root {
			t.Fatal("stopped capture fixture targets another bounded worker")
		}
	}
	engine, err := Open(config)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	lock, err := engine.lock()
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	record, err := engine.read(id)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 420*time.Second)
	defer cancel()
	state, err := engine.state(ctx, record, lock)
	if err != nil || state == nil || state.Status != "running" {
		t.Fatal("fixture lacks its original running source", err)
	}
	snapshot := strings.Repeat("b", 32)
	record.Phase = "pausing"
	record.Pause = &PauseOperation{SnapshotID: snapshot, RequestedAt: now(), SourcePID: state.PID}
	if err := engine.write(record); err != nil {
		t.Fatal(err)
	}
	intent := checkpointIntent{Version: 1, ID: snapshot, SourceID: record.ID,
		RuntimeID: record.RuntimeID, Owner: record.Owner, BootID: record.BootID,
		ConfigSHA256: record.ConfigSHA256, RuntimeSHA256: record.RuntimeSHA256,
		CriuSHA256: engine.config.CriuSHA256, ManagerRoot: engine.config.ManagerRoot,
		RequestedAt: now(), StopSource: true}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	guard, err := engine.startCheckpointGuard(record, state.PID, executable, snapshot, &intent)
	if err != nil {
		t.Fatal(err)
	}
	if err := guard.freeze(true); err != nil {
		t.Fatal(err)
	}
	criu, err := openRuntime(engine.config.Criu, engine.config.CriuSHA256)
	if err != nil {
		t.Fatal(err)
	}
	features := sysboxcapture.InspectFeatures(ctx, criu)
	if err := criu.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.admitCheckpointSource(ctx, guard, record, features.RootOwnedTimeNamespaces); err != nil {
		t.Fatal(err)
	}
	if err := guard.capturePhase(true); err != nil {
		t.Fatal(err)
	}
	if err := engine.capture(ctx, guard, lock, executable); err != nil {
		t.Fatal("real source capture/stop", err)
	}
	if err := captureGuardValidateStopped(guard.config, guard.anchor); err != nil {
		t.Fatal(err)
	}
	// Reproduce Sysbox's stale paused state when exit confirmation failed
	// before clearing the empty freezer. There is no live source to thaw.
	freezer, err := os.OpenFile(cgroupRoot+"/"+record.RuntimeID+"/cgroup.freeze", os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := freezer.WriteString("1")
	if err := errors.Join(writeErr, freezer.Close()); err != nil {
		t.Fatal(err)
	}
	// Deliberately leave the original guard transaction unfinished. Its
	// controller exits normally; its orphan handler must settle independently.
}

func TestStoppedCaptureDeleteNative(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_STOPPED_CAPTURE_DELETE") != "1" {
		t.Skip("requires the owned stopped-capture fixture")
	}
	if root := os.Getenv("FACTORYBOX_STOPPED_CAPTURE_DELETE_FIXTURE"); root != "" {
		stoppedCaptureDeleteFixture(t, root)
		return
	}
	engine, bundle := nativeEngine(t, "sleep 300")
	engine.config.Criu, engine.config.CriuSHA256 = "/out/criu", os.Getenv("FACTORYBOX_CRIU_SHA256")
	engine.config.ManagerRoot = "/var/lib/sysbox/manager"
	prepareNativeBase(t, engine, bundle)
	ctx := context.Background()
	started, err := engine.Start(ctx, "workspace", bundle)
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestStoppedCaptureDeleteNative$", "-test.timeout=420s")
	child.Env = append(os.Environ(), "FACTORYBOX_STOPPED_CAPTURE_DELETE_FIXTURE="+engine.config.Root)
	if output, err := child.CombinedOutput(); err != nil {
		t.Fatalf("abandon completed unpublished capture: %v: %s", err, output)
	}
	record, err := engine.read("workspace")
	if err != nil {
		t.Fatal(err)
	}
	relative, err := checkpointPath(record.ID, record.Pause.SnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	var armed struct {
		Token string `json:"token"`
		PID   int    `json:"helper_pid"`
	}
	if err := engine.readCheckpointJSON(relative+"/guard/guard-armed.json", &armed); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(engine.config.Root + "/" + relative + "/guard/guard-outcome.json"); err == nil {
			if _, err := os.Stat("/proc/" + strconv.Itoa(armed.PID)); errors.Is(err, os.ErrNotExist) {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("original orphan guard did not exit")
		}
		time.Sleep(10 * time.Millisecond)
	}
	before, err := engine.root.ReadFile("objects/workspace/record.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"checkpoint.json", "capture-result.json"} {
		if _, err := engine.root.Lstat(relative + "/" + path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("fixture accidentally committed its pause", path, err)
		}
	}
	if _, err := os.Stat("/proc/" + strconv.Itoa(started.Runtime.PID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("fixture source is not confirmed absent", err)
	}
	assertRefused := func(t *testing.T) {
		t.Helper()
		if _, err := engine.Delete(ctx, "workspace"); err == nil {
			t.Fatal("unproved stopped capture authorized Delete")
		}
		after, err := engine.root.ReadFile("objects/workspace/record.json")
		if err != nil || string(after) != string(before) {
			t.Fatal("refusal changed the original Pause", err)
		}
		if _, err := engine.root.Lstat(relative + "/capture-delete-admission.json"); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("refusal published destructive authority", err)
		}
		freezer, err := os.ReadFile(cgroupRoot + "/" + record.RuntimeID + "/cgroup.freeze")
		if err != nil || strings.TrimSpace(string(freezer)) != "1" {
			t.Fatal("refusal changed the empty original freezer", err)
		}
	}
	for _, control := range []struct {
		name, file string
		change     func(map[string]any)
	}{
		{"missing-stop", "capture-stop-intent.json", nil},
		{"foreign-stop", "capture-stop-intent.json", func(v map[string]any) { v["token"] = strings.Repeat("c", 32) }},
		{"wrong-stop-method", "capture-stop-intent.json", func(v map[string]any) { v["stop_method"] = "numeric-pid" }},
		{"missing-completion", "capture-process-complete.json", nil},
		{"changed-completion", "capture-process-complete.json", func(v map[string]any) { v["source_start"] = "changed" }},
		{"missing-writer", "capture-worker-outcome.json", nil},
		{"changed-capture", "capture-worker-outcome.json", func(v map[string]any) { v["capture"].(map[string]any)["source_pid"] = 1 }},
		{"live-writer", "capture-worker-intent.json", func(v map[string]any) { v["worker_pid"] = os.Getpid() }},
		{"live-guard", "guard-armed.json", func(v map[string]any) { v["helper_pid"] = os.Getpid() }},
		{"changed-guard", "guard-outcome.json", func(v map[string]any) { v["config"].(map[string]any)["start"] = "changed" }},
		{"foreign-retained-helper", "criu-retained-arm.json", func(v map[string]any) { v["source_start"] = "changed" }},
	} {
		t.Run(control.name, func(t *testing.T) {
			path := relative + "/guard/" + control.file
			original, err := engine.root.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := engine.root.WriteFile(path, original, 0600); err != nil {
					t.Fatal(err)
				}
			})
			if control.change == nil {
				if err := engine.root.Remove(path); err != nil {
					t.Fatal(err)
				}
			} else {
				var value map[string]any
				if err := json.Unmarshal(original, &value); err != nil {
					t.Fatal(err)
				}
				control.change(value)
				data, _ := json.Marshal(value)
				if err := engine.root.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			assertRefused(t)
		})
	}
	deleted, err := engine.Delete(ctx, "workspace")
	if err != nil || deleted.Runtime != nil || deleted.Record.Phase != "deleted" ||
		deleted.Record.Delete == nil || deleted.Record.Delete.CompletedAt == "" {
		t.Fatal("explicit Delete cannot settle the stopped unpublished capture", err)
	}
	if !reflect.DeepEqual(record.Pause, deleted.Record.Pause) || deleted.Record.Pause.CompletedAt != "" {
		t.Fatal("Delete manufactured a successful Pause")
	}
	admission, err := engine.root.ReadFile(relative + "/capture-delete-admission.json")
	if err != nil {
		t.Fatal(err)
	}
	if proved, err := engine.deletedFailedPauseCapture(deleted.Record, record.Pause.SnapshotID); err != nil || !proved {
		t.Fatal("confirmed stopped Delete cannot be inventoried", err)
	}
	if deleted.Record.Base != nil {
		aliases, err := engine.exitCaptureAliases(ctx, deleted.Record, []sysboxcapture.FilesystemTree{*deleted.Record.Base})
		if err != nil || len(aliases) != 1 {
			t.Fatal("worker exit omitted the deleted capture's retained immutable alias", err)
		}
	}
	if _, err := engine.Delete(ctx, "workspace"); err != nil {
		t.Fatal("completed Delete cannot reconcile", err)
	}
	after, err := engine.root.ReadFile(relative + "/capture-delete-admission.json")
	if err != nil || string(admission) != string(after) {
		t.Fatal("Delete replay changed its original authority", err)
	}
	if _, err := os.Stat(bundle + "/rootfs"); err != nil {
		t.Fatal("Delete discarded retained input backing", err)
	}
}
