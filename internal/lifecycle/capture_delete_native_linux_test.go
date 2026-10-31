//go:build linux

package lifecycle

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"factorybox/internal/sysboxcapture"
)

// The child creates a real guarded, frozen source and the failure ledger at
// the pre-retained-helper boundary. No CRIU success or safe-thaw receipt is
// invented. Its exit abandons exactly the state Delete must handle.
func failedPauseDeleteFixture(t *testing.T, root string) {
	t.Helper()
	engine, err := Open(Config{Root: root, Runtime: "/out/sysbox-runc",
		RuntimeSHA256: os.Getenv("FACTORYBOX_RUNTIME_SHA256"),
		Criu:          "/out/criu", CriuSHA256: strings.Repeat("a", 64),
		ManagerRoot: "/var/lib/sysbox/manager"})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	record, err := engine.read("workspace")
	if err != nil {
		t.Fatal(err)
	}
	state, err := engine.Inspect(context.Background(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := strings.Repeat("b", 32)
	record.Phase = "pausing"
	record.Pause = &PauseOperation{SnapshotID: snapshot, RequestedAt: now(), SourcePID: state.Runtime.PID}
	if err := engine.write(record); err != nil {
		t.Fatal(err)
	}
	intent := checkpointIntent{Version: 1, ID: snapshot, SourceID: record.ID,
		RuntimeID: record.RuntimeID, Owner: record.Owner, BootID: record.BootID,
		ConfigSHA256: record.ConfigSHA256, RuntimeSHA256: record.RuntimeSHA256,
		CriuSHA256: engine.config.CriuSHA256, ManagerRoot: engine.config.ManagerRoot,
		RequestedAt: now(), StopSource: true}
	executable, _ := os.Executable()
	guard, err := engine.startCheckpointGuard(record, state.Runtime.PID, executable, snapshot, &intent)
	if err != nil {
		t.Fatal(err)
	}
	if err := guard.freeze(true); err != nil {
		t.Fatal(err)
	}
	if err := guard.capturePhase(true); err != nil {
		t.Fatal(err)
	}
	writer := exec.Command("/bin/sleep", "300")
	if err := writer.Start(); err != nil {
		t.Fatal(err)
	}
	birth, err := processBirth(writer.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = writer.Wait()
	generation := filepath.Dir(guard.config.Evidence)
	sourceState := []byte("original source-state failure fixture\n")
	if err := os.WriteFile(generation+"/source-state.json", sourceState, 0600); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]any{
		"capture-worker-intent.json": captureWorkerIntent{Token: snapshot,
			PID: writer.Process.Pid, Start: birth, SourcePID: guard.config.PID,
			CriuSHA256: intent.CriuSHA256, Scope: "ordinary complete retained-source capture"},
		"capture-worker-outcome.json": map[string]any{"token": snapshot, "passed": false,
			"error": "fixture: fsnotify handle dump failed before retained-helper admission",
			"capture": sysboxcapture.Result{SourcePID: guard.config.PID,
				SourceStateSHA: sum(sourceState), Images: generation + "/images",
				CompleteWriter: true, LeaveRunning: true}},
		"capture-network-rollback.json": map[string]any{"token": snapshot,
			"source_pid": guard.config.PID, "source_start": guard.config.Start,
			"source_frozen": true, "passed": true,
			"events_before": map[string]uint64{"populated": 1, "frozen": 1}},
	} {
		if err := captureGuardWrite(guard.anchor.evidence, name, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := guard.finish(true); err != nil {
		t.Fatal(err)
	}
}

func TestNativeDeleteFailedPause(t *testing.T) {
	if root := os.Getenv("FACTORYBOX_FAILED_PAUSE_DELETE_FIXTURE"); root != "" {
		failedPauseDeleteFixture(t, root)
		return
	}
	engine, bundle := nativeEngine(t, "sleep 300")
	engine.config.Criu, engine.config.CriuSHA256 = "/out/criu", strings.Repeat("a", 64)
	engine.config.ManagerRoot = "/var/lib/sysbox/manager"
	ctx := context.Background()
	started, err := engine.Start(ctx, "workspace", bundle)
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestNativeDeleteFailedPause$", "-test.timeout=30s")
	child.Env = append(os.Environ(), "FACTORYBOX_FAILED_PAUSE_DELETE_FIXTURE="+engine.config.Root)
	if output, err := child.CombinedOutput(); err != nil {
		t.Fatalf("abandon original guarded capture: %v: %s", err, output)
	}
	record, err := engine.read("workspace")
	if err != nil {
		t.Fatal(err)
	}
	generation := engine.config.Root + "/objects/workspace/captures/" + record.Pause.SnapshotID
	guardRoot := generation + "/guard"
	before, err := os.ReadFile(engine.config.Root + "/objects/workspace/record.json")
	if err != nil {
		t.Fatal(err)
	}
	assertRefused := func(t *testing.T) {
		t.Helper()
		if _, err := engine.Delete(ctx, "workspace"); err == nil {
			t.Fatal("unproved failed capture authorized deletion")
		}
		after, err := os.ReadFile(engine.config.Root + "/objects/workspace/record.json")
		if err != nil || string(after) != string(before) {
			t.Fatal("refusal changed the original operation", err)
		}
		state, err := engine.Inspect(ctx, "workspace")
		if err != nil || state.Runtime == nil || state.Runtime.Status != "paused" ||
			state.Runtime.PID != started.Runtime.PID {
			t.Fatal("refusal destroyed or thawed the original source", state, err)
		}
		if _, err := os.Stat(generation + "/capture-delete-admission.json"); !os.IsNotExist(err) {
			t.Fatal("refusal published deletion authority", err)
		}
	}
	for _, control := range []struct {
		name, file string
		change     func(map[string]any)
	}{
		{"missing-writer-outcome", "capture-worker-outcome.json", nil},
		{"successful-writer", "capture-worker-outcome.json", func(v map[string]any) { v["passed"] = true }},
		{"foreign-token", "capture-worker-outcome.json", func(v map[string]any) { v["token"] = strings.Repeat("c", 32) }},
		{"live-writer", "capture-worker-intent.json", func(v map[string]any) {
			v["worker_pid"] = os.Getpid()
			v["worker_start"], _ = processBirth(os.Getpid())
		}},
		{"live-guard", "guard-armed.json", func(v map[string]any) { v["helper_pid"] = os.Getpid() }},
		{"unconfirmed-guard", "guard-outcome.json", func(v map[string]any) { v["passed"] = false }},
		{"changed-guard", "guard-outcome.json", func(v map[string]any) { v["config"].(map[string]any)["start"] = "changed" }},
		{"missing-network-rollback", "capture-network-rollback.json", nil},
		{"unsafe-network-rollback", "capture-network-rollback.json", func(v map[string]any) { v["passed"] = false }},
	} {
		t.Run(control.name, func(t *testing.T) {
			path := guardRoot + "/" + control.file
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := os.WriteFile(path, original, 0600); err != nil {
					t.Fatal(err)
				}
			})
			if control.change == nil {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			} else {
				var value map[string]any
				if err := json.Unmarshal(original, &value); err != nil {
					t.Fatal(err)
				}
				control.change(value)
				data, _ := json.Marshal(value)
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			assertRefused(t)
		})
	}
	for _, path := range []string{guardRoot + "/criu-retained-arm.json", generation + "/capture-result.json",
		generation + "/checkpoint.json"} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			if err := os.WriteFile(path, []byte("{}\n"), 0600); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Remove(path) })
			assertRefused(t)
		})
	}
	deleted, err := engine.Delete(ctx, "workspace")
	if err != nil || deleted.Runtime != nil || deleted.Record.Phase != "deleted" ||
		deleted.Record.Delete == nil || deleted.Record.Delete.CompletedAt == "" {
		t.Fatal("explicit Delete cannot settle the abandoned failed Pause", deleted, err)
	}
	if !reflect.DeepEqual(deleted.Record.Pause, record.Pause) {
		t.Fatal("Delete rewrote the incomplete Pause or invented its completion")
	}
	receipt, err := os.ReadFile(generation + "/capture-delete-admission.json")
	if err != nil {
		t.Fatal("Delete lacks its durable failed-capture admission", err)
	}
	if _, err := os.Stat(bundle + "/rootfs"); err != nil {
		t.Fatal("Delete removed retained input backing", err)
	}
	if replay, err := engine.Delete(ctx, "workspace"); err != nil || replay.Runtime != nil {
		t.Fatal("completed Delete did not reconcile", err)
	}
	after, err := os.ReadFile(generation + "/capture-delete-admission.json")
	if err != nil || string(after) != string(receipt) {
		t.Fatal("completed Delete replaced its admission receipt", err)
	}
}
