//go:build linux

package lifecycle

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"factorybox/internal/sysboxcapture"
	"golang.org/x/sys/unix"
)

func restoreMaterializedNative(t *testing.T, engine *Engine, source Record, generation, destination, id string) View {
	t.Helper()
	started := time.Now()
	ctx := context.Background()
	// A private copy is writable by CRIU during restore. The parent generation
	// stays immutable, including its original dump log and statistics.
	for _, name := range []string{"images", "criu.conf"} {
		command := exec.Command("cp", "-a", "--reflink=auto", "--", generation+"/"+name, destination+"/"+name)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("copy independent restore input: %v: %s", err, output)
		}
	}
	bundle := destination + "/filesystem"
	for _, name := range []string{"work", "rootfs"} {
		if err := os.Mkdir(bundle+"/"+name, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := unix.Mount("overlay", bundle+"/rootfs", "overlay", 0,
		"lowerdir="+bundle+"/lower,upperdir="+bundle+"/upper,workdir="+bundle+"/work"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := unix.Unmount(bundle+"/rootfs", 0); err != nil {
			t.Error("unmount owned restored overlay", err)
		}
	})
	input, err := engine.root.ReadFile("objects/" + source.ID + "/config.json")
	if err != nil {
		t.Fatal(err)
	}
	var spec map[string]json.RawMessage
	if err := json.Unmarshal(input, &spec); err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(spec["root"], &root); err != nil {
		t.Fatal(err)
	}
	root["path"] = bundle + "/rootfs"
	spec["root"], err = json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	var annotations map[string]string
	if err := json.Unmarshal(spec["annotations"], &annotations); err != nil {
		t.Fatal(err)
	}
	delete(annotations, ownerAnnotation)
	spec["annotations"], err = json.Marshal(annotations)
	if err != nil {
		t.Fatal(err)
	}
	input, err = json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundle+"/config.json", input, 0600); err != nil {
		t.Fatal(err)
	}
	lock, err := engine.lock()
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	object, err := objectPath(id)
	if err != nil {
		t.Fatal(err)
	}
	record, err := engine.prepare(id, bundle, object, input)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := engine.Delete(context.Background(), id); err != nil {
			t.Error("delete exact restored target", err)
		}
	})
	if err := engine.prepareCgroup(&record); err != nil {
		t.Fatal(err)
	}
	record.Start.Attempted = true
	if err := engine.write(record); err != nil {
		t.Fatal(err)
	}
	state, err := os.ReadFile(generation + "/source-state.json")
	if err != nil {
		t.Fatal(err)
	}
	fsState, err := os.ReadFile(generation + "/sysbox-state.json")
	if err != nil {
		t.Fatal(err)
	}
	request := restoreRequest{Version: 1, SandboxID: id, Owner: record.Owner, SourceID: source.RuntimeID,
		SourceRootfs: source.Rootfs, Root: engine.config.Root, Filesystem: bundle, Images: destination + "/images",
		ManagerRoot: engine.config.ManagerRoot, ConfigSHA256: record.ConfigSHA256, State: state, FsState: fsState}
	restoreStarted := time.Now()
	if err := engine.restore(ctx, lock, record, request); err != nil {
		t.Fatal("restore from complete independent checkpoint", err)
	}
	restoreElapsed := time.Since(restoreStarted)
	actual, err := engine.state(ctx, record, lock)
	if err != nil || actual == nil || actual.Status != "running" || actual.PID <= 1 {
		t.Fatal("restored target is not its independent running identity", actual, err)
	}
	record.RuntimeCreated, record.Phase, record.Start.CompletedAt = actual.Created, "running", now()
	if err := engine.write(record); err != nil {
		t.Fatal(err)
	}
	t.Logf("individual internal restore observation for %s: private image copy, mount, intent, restore and running-state reconciliation=%s; backend restore=%s; filesystem materialization precedes this interval; not public fork readiness or p95",
		id, time.Since(started), restoreElapsed)
	return View{Record: record, Runtime: actual}
}

func assertRestoredNative(t *testing.T, engine *Engine, view View, expected captureActorState) {
	t.Helper()
	state, err := readCaptureActor(view.Runtime.PID, "get")
	left, _ := json.Marshal(state)
	right, _ := json.Marshal(expected)
	if err != nil || string(left) != string(right) {
		t.Fatal("restored process lost its exact private RAM payload/counter", state.Counter, expected.Counter, err)
	}
	lock, err := engine.lock()
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	for path, expected := range map[string]string{
		"/proc/sys/kernel/panic":         "17",
		"/state/marker":                  "retained-source-file",
		"/var/lib/docker/capture-marker": "private-daemon-before",
	} {
		output, err := engine.command(context.Background(), lock, "exec", view.Record.RuntimeID, "/bin/cat", path)
		if err != nil || strings.TrimSpace(string(output)) != expected {
			t.Fatal("restored private virtual/file/volume state differs", path, string(output), err)
		}
	}
}

func forkFromRestoredNative(t *testing.T, engine *Engine, restored View, expected captureActorState) {
	t.Helper()
	ctx := context.Background()
	snapshot := strings.Repeat("e", 32)
	checkpoint, err := engine.Checkpoint(ctx, restored.Record.ID, snapshot)
	if err != nil {
		t.Fatal("capture a previously restored independent source", err)
	}
	generation := engine.config.Root + "/objects/" + restored.Record.ID + "/captures/" + snapshot
	destination := engine.config.Root + "/objects/" + restored.Record.ID + "/captures/" + strings.Repeat("f", 32)
	if _, err := sysboxcapture.MaterializeFilesystemSnapshot(ctx, generation, destination, checkpoint.Filesystem); err != nil {
		t.Fatal("materialize descendant of a restored source", err)
	}
	descendant := restoreMaterializedNative(t, engine, restored.Record, generation, destination, "descendant")
	if descendant.Runtime.PID == restored.Runtime.PID {
		t.Fatal("descendant replaced its source process")
	}
	assertRestoredNative(t, engine, descendant, expected)
	if _, err := readCaptureActor(descendant.Runtime.PID, "advance"); err != nil {
		t.Fatal(err)
	}
	lock, err := engine.lock()
	if err != nil {
		t.Fatal(err)
	}
	_, virtualErr := engine.command(ctx, lock, "exec", descendant.Record.RuntimeID,
		"/bin/sh", "-c", "printf '41\\n' > /proc/sys/kernel/panic")
	closeErr := lock.Close()
	if virtualErr != nil || closeErr != nil {
		t.Fatal("mutate descendant's private virtual setting", virtualErr, closeErr)
	}
	assertRestoredNative(t, engine, restored, expected)
	if err := sysboxcapture.VerifyFilesystemSnapshot(ctx, generation, checkpoint.Filesystem); err != nil {
		t.Fatal("descendant mutation changed its source checkpoint", err)
	}
}
