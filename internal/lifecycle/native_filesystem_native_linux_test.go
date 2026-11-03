//go:build linux

package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"factorybox/internal/sysboxcapture"
	"golang.org/x/sys/unix"
)

func TestNativeFilesystemLifecycle(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_FILESYSTEM_LIFECYCLE") != "1" {
		t.Skip("requires the exact delivered native-filesystem worker fixture")
	}
	deadline, bounded := t.Deadline()
	if !bounded {
		t.Fatal("native lifecycle requires the caller's deadline")
	}
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	t.Cleanup(cancel)
	runtimeAttempted, deleted, retainPause := false, false, false
	expectedStore, expectedEpoch := "", ""
	timings := map[string]int64{}
	call := func(label string, args ...string) ([]byte, error) {
		started := time.Now()
		flags := []string{"--worker-run-root", "/run/factorybox"}
		if expectedStore != "" {
			flags = append(flags, "--worker-store-id", expectedStore, "--worker-epoch", expectedEpoch)
		}
		cmd := exec.CommandContext(ctx, "/opt/factorybox/factorybox", append(flags, args...)...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		data, err := cmd.Output()
		timings[label] = time.Since(started).Microseconds()
		if err != nil {
			_ = os.WriteFile("/output/"+label+"-error.txt", stderr.Bytes(), 0600)
			return data, fmt.Errorf("%s: %w: %s", label, err, stderr.String())
		}
		return data, nil
	}
	var status struct {
		Store     string `json:"store"`
		StoreID   string `json:"store_id"`
		Epoch     string `json:"epoch"`
		Lifecycle Config `json:"lifecycle"`
	}
	data, err := exec.CommandContext(ctx, "/opt/factorybox/factorybox", "worker-status").Output()
	if err != nil || json.Unmarshal(data, &status) != nil || status.Lifecycle.BoundedStore == nil ||
		status.Lifecycle.BoundedStore.Image.Schema != 2 {
		t.Fatal("missing original delivered native store", err)
	}
	expectedStore, expectedEpoch = status.StoreID, status.Epoch
	bundle := status.Store + "/native-bundle"
	for _, path := range []string{bundle, bundle + "/rootfs"} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if out, err := exec.CommandContext(ctx, "cp", "-a", "/busybox", bundle+"/lower-input").CombinedOutput(); err != nil {
		t.Fatal(string(out), err)
	}
	actor, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundle+"/lower-input/bin/capture-actor", actor, 0755); err != nil {
		t.Fatal(err)
	}
	data, err = call("prepare-base", "prepare-base", "--source", bundle+"/lower-input", "native-base")
	var base Base
	if err != nil || json.Unmarshal(data, &base) != nil || base.Tree.Immutable == nil {
		t.Fatal("real immutable base import", err)
	}
	bound, err := sysboxcapture.BindFilesystemBase(base.Tree, bundle+"/lower")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if runtimeAttempted && !deleted && !retainPause {
			return
		}
		if err := sysboxcapture.RetireImmutableAlias(bundle+"/lower", bound); err != nil {
			t.Error("release exact fixture base alias", err)
		}
	})
	if out, err := exec.CommandContext(ctx, "btrfs", "subvolume", "create", "--", bundle+"/mutable").CombinedOutput(); err != nil {
		t.Fatal(string(out), err)
	}
	if err := os.Chmod(bundle+"/mutable", 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"upper", "work"} {
		mode := os.FileMode(0700)
		if name == "upper" {
			// Match the existing consumer's traversable overlay root. Sysbox
			// changes its owner before the init switches to the mapped view.
			mode = 0755
		}
		if err := os.Mkdir(bundle+"/mutable/"+name, mode); err != nil {
			t.Fatal(err)
		}
	}
	if err := unix.Mount("overlay", bundle+"/rootfs", "overlay", 0,
		"index=off,lowerdir="+bundle+"/lower,upperdir="+bundle+"/mutable/upper,workdir="+bundle+"/mutable/work"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if runtimeAttempted && !deleted && !retainPause {
			return
		}
		if err := unix.Unmount(bundle+"/rootfs", 0); err != nil {
			t.Error("release exact fixture overlay", err)
		}
	})
	if out, err := exec.CommandContext(ctx, status.Lifecycle.Runtime, "spec", "--bundle", bundle).CombinedOutput(); err != nil {
		t.Fatal(string(out), err)
	}
	data, err = os.ReadFile(bundle + "/config.json")
	if err != nil {
		t.Fatal(err)
	}
	var spec map[string]any
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatal(err)
	}
	process := spec["process"].(map[string]any)
	process["terminal"], process["cwd"] = false, "/"
	process["args"] = []string{"/bin/sh", "-c",
		"exec /bin/env FACTORYBOX_CAPTURE_ACTOR=1 FACTORYBOX_CAPTURE_BALLAST=1 /bin/capture-actor -test.run=^TestCaptureActor$ -test.timeout=0 </dev/null >/dev/null 2>&1"}
	process["env"] = []string{"HOME=/root", "PATH=/bin:/sbin:/usr/bin:/usr/sbin"}
	spec["root"].(map[string]any)["readonly"] = false
	spec["linux"].(map[string]any)["resources"] = map[string]any{
		"pids": map[string]any{"limit": 128}, "devices": []map[string]any{{"allow": true, "access": "rwm"}},
	}
	spec["annotations"] = map[string]string{baseAnnotation: base.ID}
	data, err = json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundle+"/config.json", data, 0600); err != nil {
		t.Fatal(err)
	}
	var initial View
	t.Cleanup(func() {
		if retainPause {
			return
		}
		response, err := call("delete", "delete", "native-source")
		if err != nil {
			t.Error("normal native Delete", err)
			return
		}
		var view View
		if err := json.Unmarshal(response, &view); err != nil || view.Record.Phase != "deleted" || view.Runtime != nil {
			t.Error("Delete lacks original terminal runtime confirmation", err)
			return
		}
		deleted = true
		if err := os.WriteFile("/output/native-delete.json", response, 0600); err != nil {
			t.Error(err)
		}
	})
	runtimeAttempted = true
	data, err = call("start", "start", "--bundle", bundle, "native-source")
	if err != nil || json.Unmarshal(data, &initial) != nil || initial.Runtime == nil {
		t.Fatal("native-layout Start", err)
	}
	var before captureActorState
	ready := time.Now()
	for {
		before, err = readCaptureActor(initial.Runtime.PID, "get")
		if err == nil {
			break
		}
		if ctx.Err() != nil {
			t.Fatal("first useful work", err, ctx.Err())
		}
		time.Sleep(20 * time.Millisecond)
	}
	timings["first-useful-work"] = time.Since(ready).Microseconds()
	if before.BallastBytes != 32<<20 || before.BallastSHA == "" {
		t.Fatal("missing actual private RAM ballast", before)
	}
	snapshot := strings.Repeat("a", 32)
	data, err = call("pause", "pause", "--snapshot-id", snapshot, "native-source")
	if err != nil {
		t.Fatal("native-layout Pause", err)
	}
	var paused View
	if err := json.Unmarshal(data, &paused); err != nil || paused.Record.Phase != "paused" ||
		paused.Record.Pause == nil || paused.Record.Pause.CompletedAt == "" ||
		paused.Record.Pause.After.Anon != 0 || paused.Record.Pause.Snapshot.ResidentBytesAfter != 0 {
		t.Fatal("native Pause lacks committed state or measured memory release", paused, err)
	}
	generation := status.Lifecycle.Root + "/objects/native-source/captures/" + snapshot
	data, err = os.ReadFile(generation + "/checkpoint.json")
	var checkpoint Checkpoint
	if err != nil || json.Unmarshal(data, &checkpoint) != nil || checkpoint.Filesystem == nil ||
		checkpoint.Filesystem.Version != 2 || !checkpoint.SourceStopped {
		t.Fatal("actual Pause did not publish the native filesystem manifest", err)
	}
	if err := os.WriteFile("/output/native-checkpoint.json", data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, tree := range checkpoint.Filesystem.Roots {
		if tree.Name != "lower" && (tree.Native == nil || tree.SHA256 != "" ||
			tree.HashedBytes != 0 || tree.SourceReadUS != 0 || tree.TargetReadUS != 0) {
			t.Fatal("Pause used a private file scan instead of native capture", tree)
		}
	}
	if os.Getenv("FACTORYBOX_NATIVE_FILESYSTEM_RETAINED") == "prepare" {
		retained := nativeFilesystemRetained{
			Request: RecoveryRequest{SourceRoot: status.Lifecycle.Root, SourceID: "native-source",
				SnapshotID: snapshot, SourceOwner: paused.Record.Owner,
				CheckpointSHA256: paused.Record.Pause.CheckpointSHA256},
			StoreID: status.StoreID, Epoch: status.Epoch, State: before,
		}
		data, err := json.MarshalIndent(retained, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile("/output/native-retained.json", append(data, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
		retainPause = true
		return
	}
	data, err = call("resume", "resume", "--snapshot-id", snapshot, "native-source")
	var resumed View
	if err != nil || json.Unmarshal(data, &resumed) != nil || resumed.Runtime == nil {
		t.Fatal("native-layout Resume", err)
	}
	after, err := readCaptureActor(resumed.Runtime.PID, "get")
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("native Resume lost original RAM or private state", err)
	}
	advanced, err := readCaptureActor(resumed.Runtime.PID, "advance")
	if err != nil || advanced.Counter != before.Counter+1 {
		t.Fatal("native Resume did not perform new useful work", err)
	}
	var child View
	t.Cleanup(func() {
		response, err := call("delete-child", "delete", "native-child")
		if err != nil {
			t.Error("normal native child Delete", err)
			return
		}
		if err := os.WriteFile("/output/native-child-delete.json", response, 0600); err != nil {
			t.Error(err)
		}
	})
	data, err = call("fork", "fork", "--source", "native-source", "--snapshot-id", snapshot, "native-child")
	if err != nil || json.Unmarshal(data, &child) != nil || child.Runtime == nil {
		t.Fatal("native-layout Fork", err)
	}
	copied, err := readCaptureActor(child.Runtime.PID, "get")
	if err != nil || !reflect.DeepEqual(before, copied) {
		t.Fatal("native Fork lost the original checkpoint's private RAM", err)
	}
	boundedLossActor(t, resumed, advanced)
	boundedLossActor(t, child, before)
	for range 2 {
		copied, err = readCaptureActor(child.Runtime.PID, "advance")
		if err != nil {
			t.Fatal("independent native child work", err)
		}
	}
	original, err := readCaptureActor(resumed.Runtime.PID, "get")
	if err != nil || !reflect.DeepEqual(original, advanced) || copied.Counter != before.Counter+2 {
		t.Fatal("native child changed original source RAM", err)
	}
	childSnapshot := strings.Repeat("b", 32)
	data, err = call("pause-child", "pause", "--snapshot-id", childSnapshot, "native-child")
	var childPaused View
	if err != nil || json.Unmarshal(data, &childPaused) != nil || childPaused.Record.Phase != "paused" ||
		childPaused.Record.Pause == nil || childPaused.Record.Pause.After.Anon != 0 ||
		childPaused.Record.Pause.Snapshot.ResidentBytesAfter != 0 {
		t.Fatal("native child Pause did not commit and release RAM", err)
	}
	data, err = call("resume-child", "resume", "--snapshot-id", childSnapshot, "native-child")
	if err != nil || json.Unmarshal(data, &child) != nil || child.Runtime == nil {
		t.Fatal("native child Resume", err)
	}
	if actual, err := readCaptureActor(child.Runtime.PID, "get"); err != nil || !reflect.DeepEqual(actual, copied) {
		t.Fatal("native child Resume lost its independently advanced state", err)
	}
	if _, err := call("delete-source-before-child", "delete", "native-source"); err != nil {
		t.Fatal("delete original native source", err)
	}
	if actual, err := readCaptureActor(child.Runtime.PID, "advance"); err != nil || actual.Counter != before.Counter+3 {
		t.Fatal("native child depends on the deleted original source", err)
	}
	proof, err := json.MarshalIndent(map[string]any{
		"passed": true, "production_qualified": false, "owner": initial.Record.Owner,
		"timings_us": timings, "pause": paused.Record.Pause, "native_roots": len(checkpoint.Filesystem.Roots) - 1,
		"same_private_ram": true, "useful_after_resume": true,
		"child_owner": child.Record.Owner, "independent_fork_ram": true,
		"child_pause": childPaused.Record.Pause, "child_resume_preserves_work": true,
		"child_useful_after_source_delete": true,
	}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("/output/native-filesystem-lifecycle.json", append(proof, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}

type nativeFilesystemRetained struct {
	Request RecoveryRequest   `json:"request"`
	StoreID string            `json:"store_id"`
	Epoch   string            `json:"epoch"`
	State   captureActorState `json:"state"`
}

func TestNativeFilesystemRetainedRecover(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_FILESYSTEM_RETAINED") != "recover" {
		t.Skip("requires the owned replacement worker and original committed native Pause")
	}
	deadline, bounded := t.Deadline()
	if !bounded {
		t.Fatal("retained recovery requires the caller's lifecycle deadline")
	}
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	t.Cleanup(cancel)
	data, err := os.ReadFile("/output/native-retained.json")
	var saved nativeFilesystemRetained
	if err != nil || json.Unmarshal(data, &saved) != nil {
		t.Fatal("original committed native Pause", err)
	}
	var status struct {
		StoreID string `json:"store_id"`
		Epoch   string `json:"epoch"`
	}
	data, err = exec.CommandContext(ctx, "/opt/factorybox/factorybox", "worker-status").Output()
	if err != nil || json.Unmarshal(data, &status) != nil ||
		status.StoreID != saved.StoreID || status.Epoch == saved.Epoch {
		t.Fatal("recovery requires the same original store and a new worker epoch", err)
	}
	call := func(args ...string) ([]byte, error) {
		flags := []string{"--worker-run-root", "/run/factorybox", "--worker-store-id", status.StoreID,
			"--worker-epoch", status.Epoch}
		cmd := exec.CommandContext(ctx, "/opt/factorybox/factorybox", append(flags, args...)...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		data, err := cmd.Output()
		if err != nil {
			return data, fmt.Errorf("%w: %s", err, stderr.String())
		}
		return data, nil
	}
	data, err = call("recover-paused", "--source-root", saved.Request.SourceRoot,
		"--source", saved.Request.SourceID, "--snapshot-id", saved.Request.SnapshotID,
		"--source-owner", saved.Request.SourceOwner, "--checkpoint-sha256", saved.Request.CheckpointSHA256,
		"native-recovered")
	if err != nil {
		t.Fatal("recover exact retained native Pause", err)
	}
	var recovered View
	if err := json.Unmarshal(data, &recovered); err != nil || recovered.Runtime == nil {
		t.Fatal("missing real recovered runtime", err)
	}
	t.Cleanup(func() {
		data, err := call("delete", "native-recovered")
		if err != nil {
			t.Error("normal recovered native Delete", err)
			return
		}
		if err := os.WriteFile("/output/native-recovered-delete.json", data, 0600); err != nil {
			t.Error(err)
		}
	})
	boundedLossActor(t, recovered, saved.State)
	advanced, err := readCaptureActor(recovered.Runtime.PID, "advance")
	if err != nil || advanced.Counter != saved.State.Counter+1 {
		t.Fatal("recovered native Sandbox did not perform new useful work", err)
	}
	data, err = json.MarshalIndent(map[string]any{
		"passed": true, "store_id": status.StoreID, "old_epoch": saved.Epoch, "new_epoch": status.Epoch,
		"owner": recovered.Record.Owner, "target_id": recovered.Record.ID,
		"original_request": saved.Request, "original_ram_and_files_preserved": true, "new_useful_work": true,
	}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("/output/native-recovered.json", append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}
