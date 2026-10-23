//go:build linux

package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"factorybox/internal/ownedtree"
	"factorybox/internal/sysboxcapture"
)

type epochFenceNativeState struct {
	PreparationSHA string                    `json:"preparation_sha256"`
	Accepted       epochRetirementAcceptance `json:"accepted"`
	Successor      RetainedCheckpointRequest `json:"successor"`
	State          captureActorState         `json:"state"`
}

func epochFenceNativeEnabled(t *testing.T) {
	t.Helper()
	if os.Getenv("FACTORYBOX_NATIVE_EPOCH_RETIREMENT_FENCE") != "1" {
		t.Skip("owned epoch retirement coordination fixture only")
	}
}

func epochFenceRead(t *testing.T, name string, value any) {
	t.Helper()
	data, err := os.ReadFile("/output/" + name)
	if err != nil || json.Unmarshal(data, value) != nil {
		t.Fatal("read epoch retirement coordination evidence", name, err)
	}
}

func TestNativeEpochRetirementFencePrepare(t *testing.T) {
	epochFenceNativeEnabled(t)
	TestNativeRetainedSelectionPrepare(t)
}

func TestNativeEpochRetirementFenceRecover(t *testing.T) {
	epochFenceNativeEnabled(t)
	// Complete every original assertion and its original cleanup before the
	// independent fence scenario starts. None of those assertions is replaced.
	if !t.Run("original-complete-recovery", TestNativeRetainedSelectionRecover) {
		t.FailNow()
	}
	var pause boundedLossPause
	epochFenceRead(t, "bounded-loss-pause.json", &pause)
	status := boundedLossStatusNow(t)
	// The complete original scenario deliberately retains its deleted
	// descendants' checkpoints. Retire those three exact obsolete snapshots
	// through the delivered API before extending the same 1-GiB fixture.
	// Keep the original capacity assertion and every original lifecycle check.
	previous, err := Open(status.Lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	defer previous.Close()
	for _, generation := range []struct{ id, snapshot string }{
		{"recovered", strings.Repeat("b", 32)}, {"child", strings.Repeat("c", 32)}, {"child", strings.Repeat("d", 32)},
	} {
		record, err := previous.read(generation.id)
		if err != nil || record.Phase != "deleted" || record.Delete == nil || record.Delete.CompletedAt == "" {
			t.Fatal("original descendant is not confirmed deleted before explicit checkpoint retirement", err)
		}
		relative, _ := checkpointPath(generation.id, generation.snapshot)
		var saved Checkpoint
		if err := previous.readCheckpointJSON(relative+"/checkpoint.json", &saved); err != nil {
			t.Fatal(err)
		}
		identity, _ := json.Marshal(saved)
		request := CheckpointRequest{generation.id, generation.snapshot, record.Owner, sum(identity)}
		var retired CheckpointRetirement
		if err := json.Unmarshal(boundedLossAction(t, retirementArgs("retire-checkpoint", request, "")...), &retired); err != nil || retired.Request != request || retired.CompletedAt == "" {
			t.Fatal("explicit obsolete checkpoint retirement did not complete", retired, err)
		}
	}
	request := pause.Request
	view := boundedLossView(t, "recover-paused", "--source-root", request.SourceRoot, "--source", request.SourceID,
		"--snapshot-id", request.SnapshotID, "--source-owner", request.SourceOwner, "--checkpoint-sha256", request.CheckpointSHA256, "handover")
	boundedLossActor(t, view, pause.State)
	state, err := readCaptureActor(view.Runtime.PID, "advance")
	if err != nil || state.Counter != pause.State.Counter+1 || state.BallastSHA == pause.State.BallastSHA {
		t.Fatal("independent handover did not do useful private work", err)
	}
	boundedLossActor(t, view, state)
	snapshot := strings.Repeat("e", 32)
	paused := boundedLossView(t, "pause", "--snapshot-id", snapshot, "handover")
	if err := committedExitPause(paused.Record); err != nil {
		t.Fatal("handover successor is not durably paused", err)
	}
	current, err := Open(status.Lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	defer current.Close()
	lock, err := current.lock()
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	old, err := current.openRetainedController(request.SourceRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	oldLock, err := old.lockRoot()
	if err != nil {
		t.Fatal(err)
	}
	defer oldLock.Close()
	ctx := context.Background()
	checkpoint := CheckpointRequest{request.SourceID, request.SnapshotID, request.SourceOwner, request.CheckpointSHA256}
	if _, err := old.inspectRetainedCheckpoint(ctx, checkpoint); err != nil {
		t.Fatal(err)
	}
	successor := RetainedCheckpointRequest{SourceRoot: status.Lifecycle.Root, CheckpointRequest: CheckpointRequest{
		SourceID: "handover", SnapshotID: snapshot, SourceOwner: paused.Record.Owner, CheckpointSHA256: paused.Record.Pause.CheckpointSHA256}}
	relative, _ := successor.CheckpointRequest.path()
	saved, err := current.publishedCheckpointMetadata(ctx, paused.Record, snapshot, relative)
	if err != nil {
		t.Fatal(err)
	}
	if err := sysboxcapture.VerifyFilesystemSnapshot(ctx, current.config.Root+"/"+relative, saved.Filesystem); err != nil {
		t.Fatal("successor backing is not independently complete", err)
	}
	original, err := current.readForkIntent("forks/handover", request.SourceID, request.SnapshotID, "handover")
	if err != nil || original.Recovery == nil || original.Recovery.Request != request {
		t.Fatal("successor does not bind its original recovery request", err)
	}
	oldRelative, _ := checkpoint.path()
	// This fixture authorizes only the complete old RAM image directory. The
	// full epoch planner and public deletion API are separate unfinished work.
	plan, err := ownedtree.Capture(ctx, old.root, []string{oldRelative + "/images"})
	if err != nil {
		t.Fatal(err)
	}
	if err := old.root.Mkdir(epochRetirementDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	inventorySHA, err := old.writeRetirementInventory(epochRetirementDirectory, plan)
	if err != nil {
		t.Fatal(err)
	}
	prepared := epochRetirementPreparation{Version: 1, BootID: status.BootID, Image: status.Lifecycle.BoundedStore.Image,
		Attachment:      *status.Lifecycle.BoundedStore,
		Request:         epochRetirementRequest{SourceRoot: request.SourceRoot, Handovers: []epochRecoveryHandover{{Source: checkpoint, Successor: successor}}},
		InventorySHA256: inventorySHA, PreparedAt: now()}
	if err := old.publishCheckpoint(epochRetirementDirectory, "prepare.json", prepared); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(prepared)
	preparationSHA := sum(data)
	accepted, err := current.acceptEpochRetirement(ctx, old, preparationSHA)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := current.acceptEpochRetirement(ctx, old, preparationSHA)
	if err != nil || !reflect.DeepEqual(accepted, replay) {
		t.Fatal("accepted fence replay changed", err)
	}
	if _, err := current.acceptEpochRetirement(ctx, old, strings.Repeat("0", 64)); err == nil {
		t.Fatal("changed preparation reused the accepted fence")
	}
	store, err := current.epochRetirementStore(old)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	fenceBytes, err := store.root.ReadFile(epochRetirementFenceName)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.root.Rename(epochRetirementFenceName, "original-fence.json"); err != nil {
		t.Fatal(err)
	}
	if err := store.root.WriteFile(epochRetirementFenceName, fenceBytes, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := current.verifyEpochRetirementFence(ctx, store, accepted); err == nil {
		t.Fatal("same-byte successor inode adopted as original fence")
	}
	if err := store.root.Remove(epochRetirementFenceName); err != nil {
		t.Fatal(err)
	}
	if err := store.root.Rename("original-fence.json", epochRetirementFenceName); err != nil {
		t.Fatal(err)
	}
	if err := store.root.Link(epochRetirementFenceName, "foreign-fence-link"); err != nil {
		t.Fatal(err)
	}
	if _, err := current.verifyEpochRetirementFence(ctx, store, accepted); err == nil {
		t.Fatal("external hardlink to the maintenance fence accepted")
	}
	if err := store.root.Remove("foreign-fence-link"); err != nil {
		t.Fatal(err)
	}
	if _, err := current.verifyEpochRetirementFence(ctx, store, accepted); err != nil {
		t.Fatal(err)
	}
	// Release operation locks so subsequent CLI refusals prove the persisted
	// fence, not this test process retaining a transient flock.
	if err := oldLock.Close(); err != nil {
		t.Fatal(err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"resume", "--snapshot-id", snapshot, "handover"},
		{"delete", "handover"},
		{"checkpoint", "--snapshot-id", strings.Repeat("f", 32), "handover"},
		retirementArgs("select-checkpoint", successor.CheckpointRequest, "protected-successor"),
		append([]string{"release-recovery-selection", "--source-root", request.SourceRoot}, retirementArgs("release-checkpoint", checkpoint, "recover-primary")[1:]...),
	} {
		if _, err := boundedLossCommand(append([]string{"--worker-run-root", "/run/factorybox"}, args...)...); err == nil || !strings.Contains(err.Error(), "epoch retirement") {
			t.Fatal("accepted fence did not protect its inputs across controller invocations", args, err)
		}
	}
	explicit := []string{"--root", current.config.Root, "--runtime", current.config.Runtime, "--runtime-sha256", current.config.RuntimeSHA256,
		"--criu", current.config.Criu, "--criu-sha256", current.config.CriuSHA256, "--manager-root", current.config.ManagerRoot,
		"delete", "handover"}
	if _, err := boundedLossCommand(explicit...); err == nil || !strings.Contains(err.Error(), "epoch retirement") {
		t.Fatal("explicit controller options bypassed the durable fence", err)
	}
	boundedLossSave(t, "epoch-retirement-fence-accepted.json", epochFenceNativeState{preparationSHA, accepted, successor, state})
}

func TestNativeEpochRetirementFenceFinish(t *testing.T) {
	epochFenceNativeEnabled(t)
	var evidence epochFenceNativeState
	epochFenceRead(t, "epoch-retirement-fence-accepted.json", &evidence)
	status := boundedLossStatusNow(t)
	// Mount IDs can repeat across different mount namespaces. The driver
	// verifies exact old-worker removal and holds its old loop slot separately.
	if status.Lifecycle.Root == evidence.Successor.SourceRoot || status.Lifecycle.BoundedStore.BlockDevice == evidence.Accepted.Attachment.BlockDevice {
		t.Fatal("test did not replace the accepted-retirement worker on a different loop device")
	}
	engine, err := Open(status.Lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	if lock, err := engine.lock(); err == nil {
		lock.Close()
		t.Fatal("fresh worker did not inherit active retirement fence")
	}
	ctx := context.Background()
	lock, err := engine.lockRoot()
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	old, err := engine.openRetainedController(evidence.Accepted.Fence.SourceRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	oldLock, err := old.lockRoot()
	if err != nil {
		t.Fatal(err)
	}
	defer oldLock.Close()
	replayed, err := engine.acceptEpochRetirement(ctx, old, evidence.PreparationSHA)
	if err != nil || !reflect.DeepEqual(replayed, evidence.Accepted) {
		t.Fatal("worker-loss retry changed accepted operation", err)
	}
	if err := oldLock.Close(); err != nil {
		t.Fatal(err)
	}
	if reader, err := old.recoveryLock(RecoveryRequest{}); err == nil || !strings.Contains(err.Error(), "epoch retirement") {
		if reader != nil {
			reader.Close()
		}
		t.Fatal("retired source did not fence its ordinary recovery reader", err)
	}
	oldLock, err = old.lockRoot()
	if err != nil {
		t.Fatal(err)
	}
	defer oldLock.Close()
	acceptedBytes, _ := json.Marshal(replayed)
	complete := epochRetirementCompletion{Version: 1, SourceRoot: old.config.Root,
		PreparationSHA256: evidence.PreparationSHA, AcceptanceSHA256: sum(acceptedBytes), CompletedAt: now()}
	// Deliberately premature metadata must not clear the actual fence.
	if err := old.publishCheckpoint(epochRetirementDirectory, "complete.json", complete); err != nil {
		t.Fatal(err)
	}
	if err := engine.finishEpochRetirementFence(ctx, old, replayed); err == nil {
		t.Fatal("premature completion cleared the fence with source payload still present")
	}
	prepared, err := engine.epochRetirementPreparation(old, evidence.PreparationSHA)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := old.readRetirementInventory(epochRetirementDirectory, prepared.InventorySHA256)
	if err != nil {
		t.Fatal(err)
	}
	plan, err = rebindEpochRetirementPlan(plan, prepared.Attachment, *engine.config.BoundedStore)
	if err != nil {
		t.Fatal(err)
	}
	if err := ownedtree.RemoveRemaining(ctx, old.root, plan); err != nil {
		t.Fatal(err)
	}
	if err := engine.finishEpochRetirementFence(ctx, old, replayed); err != nil {
		t.Fatal(err)
	}
	if err := engine.finishEpochRetirementFence(ctx, old, replayed); err != nil {
		t.Fatal("finished retry changed", err)
	}
	if err := old.epochRetirementFence(); err == nil {
		t.Fatal("source tombstone no longer fences recovery")
	}
	if err := engine.epochRetirementFence(); err != nil {
		t.Fatal("fresh worker remained fenced after verified finish", err)
	}
	if err := oldLock.Close(); err != nil {
		t.Fatal(err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	successor := evidence.Successor
	view := boundedLossView(t, "recover-paused", "--source-root", successor.SourceRoot, "--source", successor.SourceID,
		"--snapshot-id", successor.SnapshotID, "--source-owner", successor.SourceOwner, "--checkpoint-sha256", successor.CheckpointSHA256, "successor")
	boundedLossActor(t, view, evidence.State)
	advanced, err := readCaptureActor(view.Runtime.PID, "advance")
	if err != nil || advanced.Counter != evidence.State.Counter+1 {
		t.Fatal("preserved successor cannot work", err)
	}
	boundedLossAction(t, "delete", "successor")
	original, err := old.root.ReadFile(epochRetirementDirectory + "/intent.json")
	if err != nil || !bytes.Equal(bytes.TrimSpace(original), acceptedBytes) {
		t.Fatal("source acceptance changed after worker loss and completion", err)
	}
	boundedLossSave(t, "epoch-retirement-fence-finished.json", map[string]any{"passed": true,
		"worker_loss_after_acceptance": true, "loop_device_changed": true, "exact_acceptance_replayed": true, "successor_recovered_and_useful": true,
		"same_byte_successor_fence_refused": true, "external_fence_link_refused": true, "premature_completion_refused": true,
		"source_tombstone_preserved": true, "exact_finish_replayed": true, "full_epoch_retirement_qualified": false})
}
