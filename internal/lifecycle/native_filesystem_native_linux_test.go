//go:build linux

package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	sourceOverlayReleased, sourceLowerReleased := false, false
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
		if sourceLowerReleased {
			return
		}
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
		if sourceOverlayReleased {
			return
		}
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
		if os.Getenv("FACTORYBOX_NATIVE_FILESYSTEM_EPOCH_RETIREMENT_PREPARE") == "1" {
			t.Cleanup(func() {
				if _, err := call("delete-epoch-peer-replay", "delete", "native-epoch-peer"); err != nil {
					t.Error("delete exact native epoch fixture peer", err)
				}
			})
			data, err := call("fork-epoch-peer", "fork", "--source", "native-source", "--snapshot-id", snapshot, "native-epoch-peer")
			var peer View
			if err != nil || json.Unmarshal(data, &peer) != nil || peer.Runtime == nil {
				t.Fatal("prepare native epoch working descendant", err)
			}
			boundedLossActor(t, peer, before)
			if advanced, err := readCaptureActor(peer.Runtime.PID, "advance"); err != nil || advanced.Counter != before.Counter+1 {
				t.Fatal("advance native epoch working descendant", err)
			}
			peerSnapshot := strings.Repeat("d", 32)
			if _, err := call("pause-epoch-peer", "pause", "--snapshot-id", peerSnapshot, peer.Record.ID); err != nil {
				t.Fatal("capture native epoch working descendant", err)
			}
			if _, err := call("resume-epoch-peer", "resume", "--snapshot-id", peerSnapshot, peer.Record.ID); err != nil {
				t.Fatal("resume native epoch working descendant", err)
			}
			if _, err := call("delete-epoch-peer", "delete", peer.Record.ID); err != nil {
				t.Fatal("close native epoch working descendant", err)
			}
		}
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
	var checkpointRetirement map[string]any
	if os.Getenv("FACTORYBOX_NATIVE_FILESYSTEM_RETIREMENT") == "1" {
		checkpointRetirement = nativeCheckpointRetirement(t, call, checkpoint, child)
	}
	if _, err := call("delete-source-before-child", "delete", "native-source"); err != nil {
		t.Fatal("delete original native source", err)
	}
	var backingRetirement map[string]any
	if os.Getenv("FACTORYBOX_NATIVE_FILESYSTEM_BACKING_RETIREMENT") == "1" {
		if err := unix.Unmount(bundle+"/rootfs", 0); err != nil {
			t.Fatal("release deleted native fixture overlay", err)
		}
		sourceOverlayReleased = true
		if err := sysboxcapture.RetireImmutableAlias(bundle+"/lower", bound); err != nil {
			t.Fatal("release deleted native fixture immutable alias", err)
		}
		sourceLowerReleased = true
		expected, err := readCaptureActor(child.Runtime.PID, "get")
		if err != nil {
			t.Fatal(err)
		}
		backingRetirement = nativeDeletedBackingRetirement(t, call, initial.Record, base)
		boundedLossActor(t, child, expected)
	}
	expectedChildCounter := before.Counter + 3
	if checkpointRetirement != nil {
		expectedChildCounter++
	}
	if actual, err := readCaptureActor(child.Runtime.PID, "advance"); err != nil || actual.Counter != expectedChildCounter {
		t.Fatal("native child depends on the deleted original source", err)
	}
	if backingRetirement != nil {
		backingRetirement["independent_child_useful"] = true
		response, err := call("delete-child-before-backing", "delete", child.Record.ID)
		if err != nil {
			t.Fatal("delete exact native child before its backing retirement", err)
		}
		var childDeleted View
		if json.Unmarshal(response, &childDeleted) != nil || childDeleted.Record.Phase != "deleted" || childDeleted.Runtime != nil {
			t.Fatal("native child backing requires its confirmed public Delete")
		}
		backingRetirement["child"] = nativeDeletedBackingRetirement(t, call, childDeleted.Record, base)
	}
	evidence := map[string]any{
		"passed": true, "production_qualified": false, "owner": initial.Record.Owner,
		"timings_us": timings, "pause": paused.Record.Pause, "native_roots": len(checkpoint.Filesystem.Roots) - 1,
		"same_private_ram": true, "useful_after_resume": true,
		"child_owner": child.Record.Owner, "independent_fork_ram": true,
		"child_pause": childPaused.Record.Pause, "child_resume_preserves_work": true,
		"child_useful_after_source_delete": true,
	}
	if checkpointRetirement != nil {
		evidence["native_checkpoint_retirement"] = checkpointRetirement
	}
	if backingRetirement != nil {
		evidence["native_backing_retirement"] = backingRetirement
	}
	proof, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("/output/native-filesystem-lifecycle.json", append(proof, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}

func nativeDeletedBackingRetirement(t *testing.T, call func(string, ...string) ([]byte, error), record Record, base Base) map[string]any {
	t.Helper()
	args := []string{"retire-sandbox", "--sandbox-owner", record.Owner, "--input-sha256", record.InputSHA256, record.ID}
	retired, err := call("retire-native-backing-"+record.ID, args...)
	var result SandboxBackingRetirement
	request := SandboxBackingRequest{record.ID, record.Owner, record.InputSHA256}
	if err != nil || json.Unmarshal(retired, &result) != nil || result.Request != request || result.CompletedAt == "" {
		t.Fatal("public native deleted-Sandbox backing retirement", err)
	}
	replayed, err := call("replay-native-backing-"+record.ID, args...)
	if err != nil || !bytes.Equal(retired, replayed) {
		t.Fatal("native backing retirement lost exact completion replay", err)
	}
	if _, err := os.Lstat(record.InputBundle + "/mutable"); !os.IsNotExist(err) {
		t.Fatal("native Sandbox mutable subvolume remains", err)
	}
	status := boundedLossStatusNow(t)
	for _, kind := range retiredManagerKinds {
		if _, err := os.Lstat(status.Lifecycle.ManagerRoot + "/" + kind + "/" + record.RuntimeID); !os.IsNotExist(err) {
			t.Fatal("native Sandbox manager backing remains", kind, err)
		}
	}
	if err := sysboxcapture.VerifyImmutableTree(base.Tree.Saved, base.Tree); err != nil {
		t.Fatal("native backing retirement changed the common immutable base", err)
	}
	return map[string]any{"passed": true, "exact_replay": true, "native_mutable_absent": true,
		"manager_roots_absent": true, "common_base_preserved": true, "retirement": result}
}

func nativeCheckpointRetirement(t *testing.T, call func(string, ...string) ([]byte, error), checkpoint Checkpoint, child View) map[string]any {
	t.Helper()
	identity, err := json.Marshal(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	request := CheckpointRequest{checkpoint.SourceID, checkpoint.ID, checkpoint.Owner, sum(identity)}
	selected, err := call("select-retirement", retirementArgs("select-checkpoint", request, "native-retirement")...)
	if err != nil || len(selected) == 0 {
		t.Fatal("protect exact native checkpoint", err)
	}
	if _, err := call("selected-retirement-refused", retirementArgs("retire-checkpoint", request, "")...); err == nil ||
		!strings.Contains(err.Error(), "protected by recovery selection") {
		t.Fatal("native retirement ignored its original recovery selection", err)
	}
	if _, err := call("release-retirement", retirementArgs("release-checkpoint", request, "native-retirement")...); err != nil {
		t.Fatal("release exact native checkpoint selection", err)
	}
	data, err := os.ReadFile("/output/native-checkpoint.json")
	if err != nil {
		t.Fatal(err)
	}
	generation := filepath.Dir(filepath.Dir(checkpoint.Filesystem.Roots[0].Saved))
	original, err := os.ReadFile(generation + "/checkpoint.json")
	if err != nil || !bytes.Equal(data, original) {
		t.Fatal("native retirement lost the original checkpoint metadata", err)
	}
	interrupted := os.Getenv("FACTORYBOX_NATIVE_FILESYSTEM_RETIREMENT_INTERRUPT") == "1"
	if interrupted {
		nativeInterruptCheckpointRetirement(t, call, request, generation)
	}
	retired, err := call("retire-native-checkpoint", retirementArgs("retire-checkpoint", request, "")...)
	var result CheckpointRetirement
	if err != nil || json.Unmarshal(retired, &result) != nil || result.Request != request || result.CompletedAt == "" {
		t.Fatal("public native checkpoint retirement", err)
	}
	replayed, err := call("replay-native-retirement", retirementArgs("retire-checkpoint", request, "")...)
	if err != nil || !bytes.Equal(retired, replayed) {
		t.Fatal("native retirement did not preserve its exact completion on replay", err)
	}
	for _, tree := range checkpoint.Filesystem.Roots {
		path := tree.Saved
		if tree.Native != nil && tree.Native.Member == "upper" {
			path = filepath.Dir(path)
		}
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatal("native checkpoint backing remains after retirement", path, err)
		}
	}
	if actual, err := os.ReadFile(generation + "/checkpoint.json"); err != nil || !bytes.Equal(actual, original) {
		t.Fatal("native retirement changed retained checkpoint provenance", err)
	}
	if _, err := call("retired-new-fork", "fork", "--source", request.SourceID, "--snapshot-id", request.SnapshotID,
		"native-retired-new"); err == nil || !strings.Contains(err.Error(), "retirement") {
		t.Fatal("retired native checkpoint admitted new execution")
	}
	before, err := readCaptureActor(child.Runtime.PID, "get")
	if err != nil {
		t.Fatal(err)
	}
	boundedLossActor(t, child, before)
	after, err := readCaptureActor(child.Runtime.PID, "advance")
	if err != nil || after.Counter != before.Counter+1 || after.BallastSHA == before.BallastSHA ||
		after.BallastBytes != before.BallastBytes {
		t.Fatal("native checkpoint retirement broke its independent live child", before, after, err)
	}
	return map[string]any{
		"passed": true, "selection_protected": true, "exact_replay": true, "native_roots_absent": true,
		"checkpoint_metadata_preserved": true, "new_fork_refused": true, "independent_child_useful": true,
		"retirement":                result,
		"actual_controller_sigkill": interrupted, "partial_retry_preserved_foreign": interrupted,
	}
}

func nativeInterruptCheckpointRetirement(t *testing.T, call func(string, ...string) ([]byte, error), request CheckpointRequest, generation string) {
	t.Helper()
	withdrawn := generation + "/filesystem/mutable"
	nativeKillRetirementAfterWithdrawal(t, retirementArgs("retire-checkpoint", request, ""), withdrawn,
		generation+"/retirement-complete.json", "checkpoint", func() bool {
			var intent checkpointRetirementIntent
			data, err := os.ReadFile(generation + "/retirement-intent.json")
			return err == nil && json.Unmarshal(data, &intent) == nil && intent.Request == request && len(intent.Native) == 8
		})
	nativeRetirementPreservesForeignRoot(t, withdrawn, func() error {
		_, err := call("native-retirement-foreign-refused", retirementArgs("retire-checkpoint", request, "")...)
		return err
	})
}

func nativeKillRetirementAfterWithdrawal(t *testing.T, operation []string, withdrawn, complete, label string, ready func() bool) {
	t.Helper()
	deadline, bounded := t.Deadline()
	if !bounded {
		t.Fatal("native retirement interruption requires its lifecycle deadline")
	}
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	status := boundedLossStatusNow(t)
	args := []string{"--worker-run-root", "/run/factorybox", "--worker-store-id", status.StoreID,
		"--worker-epoch", status.Epoch}
	command := exec.CommandContext(ctx, "/opt/factorybox/factorybox", append(args, operation...)...)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	defer func() {
		if !waited {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	}()
	pidfd, err := unix.PidfdOpen(command.Process.Pid, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(pidfd)
	for {
		_, missing := os.Lstat(withdrawn)
		if ready() && os.IsNotExist(missing) {
			if err := unix.PidfdSendSignal(pidfd, unix.SIGSTOP, nil, 0); err != nil {
				t.Fatal("stop original retirement controller after actual native withdrawal", err)
			}
			if _, err := os.Lstat(complete); !os.IsNotExist(err) {
				t.Fatal("native retirement completed before the real interruption", err)
			}
			if err := unix.PidfdSendSignal(pidfd, unix.SIGKILL, nil, 0); err != nil {
				t.Fatal(err)
			}
			break
		}
		poll := []unix.PollFd{{Fd: int32(pidfd), Events: unix.POLLIN}}
		n, err := unix.Poll(poll, 0)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil || n != 0 || ctx.Err() != nil {
			t.Fatal("native retirement did not expose its original incomplete withdrawal", missing, err, ctx.Err())
		}
		time.Sleep(time.Millisecond)
	}
	if err := command.Wait(); err == nil {
		t.Fatal("killed native retirement returned success")
	}
	waited = true
	if err := os.WriteFile("/output/native-"+label+"-retirement-interrupted.stderr", stderr.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
}

func nativeRetirementPreservesForeignRoot(t *testing.T, withdrawn string, retry func() error) {
	t.Helper()
	if err := os.Mkdir(withdrawn, 0700); err != nil {
		t.Fatal(err)
	}
	foreign := withdrawn + "/foreign-successor"
	if err := os.WriteFile(foreign, []byte("preserve foreign successor"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := retry(); err == nil {
		t.Fatal("native retirement retry adopted a foreign successor")
	}
	if data, err := os.ReadFile(foreign); err != nil || string(data) != "preserve foreign successor" {
		t.Fatal("native retirement retry changed foreign data", err)
	}
	if err := os.Remove(foreign); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(withdrawn); err != nil {
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
		StoreID   string `json:"store_id"`
		Epoch     string `json:"epoch"`
		Lifecycle Config `json:"lifecycle"`
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
	proof := map[string]any{
		"passed": true, "store_id": status.StoreID, "old_epoch": saved.Epoch, "new_epoch": status.Epoch,
		"owner": recovered.Record.Owner, "target_id": recovered.Record.ID,
		"original_request": saved.Request, "original_ram_and_files_preserved": true, "new_useful_work": true,
	}
	if os.Getenv("FACTORYBOX_NATIVE_FILESYSTEM_EPOCH_RETIREMENT") == "1" {
		proof["native_epoch_retirement"] = nativeRetireOriginalEpoch(t, call, saved, recovered, advanced, status.Lifecycle.Root)
	}
	data, err = json.MarshalIndent(proof, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("/output/native-recovered.json", append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}

func nativeRetireOriginalEpoch(t *testing.T, call func(...string) ([]byte, error), saved nativeFilesystemRetained, recovered View, state captureActorState, currentRoot string) map[string]any {
	t.Helper()
	generation := saved.Request.SourceRoot + "/objects/" + saved.Request.SourceID + "/captures/" + saved.Request.SnapshotID
	original, err := os.ReadFile(generation + "/checkpoint.json")
	if err != nil {
		t.Fatal(err)
	}
	var checkpoint Checkpoint
	if err := json.Unmarshal(original, &checkpoint); err != nil {
		t.Fatal(err)
	}
	snapshot := strings.Repeat("c", 32)
	data, err := call("pause", "--snapshot-id", snapshot, recovered.Record.ID)
	var paused View
	if err != nil || json.Unmarshal(data, &paused) != nil || committedExitPause(paused.Record) != nil {
		t.Fatal("commit independent native successor before old epoch retirement", err)
	}
	request := EpochRetirementRequest{SourceRoot: saved.Request.SourceRoot, Handovers: []EpochRecoveryHandover{{
		Source: CheckpointRequest{saved.Request.SourceID, saved.Request.SnapshotID, saved.Request.SourceOwner, saved.Request.CheckpointSHA256},
		Successor: RetainedCheckpointRequest{SourceRoot: currentRoot, CheckpointRequest: CheckpointRequest{
			paused.Record.ID, snapshot, paused.Record.Owner, paused.Record.Pause.CheckpointSHA256}},
	}}}
	data, err = json.MarshalIndent(request, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	file := "/output/native-epoch-retirement-request.json"
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	accepted, err := call("begin-epoch-retirement", "--request-file", file)
	if err != nil {
		t.Fatal("admit exact original native epoch retirement", err)
	}
	if err := os.WriteFile("/output/native-epoch-retirement-accepted.json", accepted, 0600); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(saved.Request.SourceRoot + "/" + epochRetirementDirectory + "/prepare.json")
	var prepared epochRetirementPreparation
	if err != nil || json.Unmarshal(data, &prepared) != nil || len(prepared.Native) != 24 {
		t.Fatal("native epoch omitted original snapshots or the cold working fork", len(prepared.Native), err)
	}
	withdrawn := epochNativeRoot(prepared.Native[0])
	stage := saved.Request.SourceRoot + "/" + epochRetirementDirectory
	nativeKillRetirementAfterWithdrawal(t, []string{"retire-epoch", "--request-file", file}, withdrawn,
		stage+"/complete.json", "epoch", func() bool {
			var accepted epochRetirementAcceptance
			data, err := os.ReadFile(stage + "/intent.json")
			return err == nil && json.Unmarshal(data, &accepted) == nil &&
				accepted.Fence.SourceRoot == saved.Request.SourceRoot && accepted.AcceptedAt != ""
		})
	nativeRetirementPreservesForeignRoot(t, withdrawn, func() error {
		_, err := call("retire-epoch", "--request-file", file)
		return err
	})
	retired, err := call("retire-epoch", "--request-file", file)
	var result EpochRetirement
	if err != nil || json.Unmarshal(retired, &result) != nil || result.CompletedAt == "" ||
		!reflect.DeepEqual(result.Request, request) {
		t.Fatal("complete original native epoch retirement", err)
	}
	replayed, err := call("retire-epoch", "--request-file", file)
	if err != nil || !bytes.Equal(retired, replayed) {
		t.Fatal("native epoch retirement did not preserve its exact completion", err)
	}
	for _, tree := range prepared.Native {
		if _, err := os.Lstat(epochNativeRoot(tree)); !os.IsNotExist(err) {
			t.Fatal("native epoch retirement retained admitted native backing", tree.Saved, err)
		}
	}
	for _, path := range []string{generation + "/images", saved.Request.SourceRoot + "/bases/native-base/rootfs"} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatal("old epoch ordinary payload remains", path, err)
		}
	}
	for _, tree := range checkpoint.Filesystem.Roots {
		path := tree.Saved
		if tree.Native != nil && tree.Native.Member == "upper" {
			path = filepath.Dir(path)
		}
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatal("old epoch native or alias payload remains", path, err)
		}
	}
	if after, err := os.ReadFile(generation + "/checkpoint.json"); err != nil || !bytes.Equal(original, after) {
		t.Fatal("native epoch retirement changed original checkpoint provenance", err)
	}
	if _, err := call("recover-paused", "--source-root", saved.Request.SourceRoot, "--source", saved.Request.SourceID,
		"--snapshot-id", saved.Request.SnapshotID, "--source-owner", saved.Request.SourceOwner,
		"--checkpoint-sha256", saved.Request.CheckpointSHA256, "native-retired-again"); err == nil ||
		!strings.Contains(err.Error(), "retirement") {
		t.Fatal("retired native epoch still permits recovery", err)
	}
	data, err = call("resume", "--snapshot-id", snapshot, recovered.Record.ID)
	var resumed View
	if err != nil || json.Unmarshal(data, &resumed) != nil || resumed.Runtime == nil {
		t.Fatal("resume native successor after original epoch withdrawal", err)
	}
	boundedLossActor(t, resumed, state)
	advanced, err := readCaptureActor(resumed.Runtime.PID, "advance")
	if err != nil || advanced.Counter != state.Counter+1 || advanced.BallastSHA == state.BallastSHA {
		t.Fatal("native successor lost useful work after original epoch withdrawal", err)
	}
	return map[string]any{"passed": true, "old_payloads_absent": true, "original_metadata_preserved": true,
		"exact_replay": true, "old_recovery_refused": true, "successor_resumed_and_useful": true,
		"actual_controller_sigkill": true, "foreign_successor_preserved": true, "retirement": result}
}
