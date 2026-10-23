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
	"golang.org/x/sys/unix"
)

type epochRetirementNativeState struct {
	Request       EpochRetirementRequest `json:"request"`
	Accepted      EpochRetirement        `json:"accepted"`
	State         captureActorState      `json:"state"`
	RemovedRoot   string                 `json:"removed_root"`
	WrongRequests int                    `json:"wrong_requests"`
}

func epochRetirementNativeEnabled(t *testing.T) {
	t.Helper()
	if os.Getenv("FACTORYBOX_NATIVE_EPOCH_RETIREMENT") != "1" {
		t.Skip("owned public epoch retirement fixture only")
	}
}

func epochRetirementCLI(t *testing.T, operation string, request EpochRetirementRequest, name string) ([]byte, error) {
	t.Helper()
	path := "/output/" + name
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	return boundedLossCommand("--worker-run-root", "/run/factorybox", operation, "--request-file", path)
}

func TestNativeEpochRetirementPrepare(t *testing.T) {
	epochRetirementNativeEnabled(t)
	TestNativeRetainedSelectionPrepare(t)
}

func TestNativeEpochRetirementRecover(t *testing.T) {
	epochRetirementNativeEnabled(t)
	if !t.Run("full-original-recovery", TestNativeRetainedSelectionRecover) {
		t.FailNow()
	}
	var pause boundedLossPause
	epochFenceRead(t, "bounded-loss-pause.json", &pause)
	status := boundedLossStatusNow(t)
	engine, err := Open(status.Lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	old, err := engine.openRetainedController(pause.Request.SourceRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	// Keep the exact original 1-GiB profile. The completed original scenario's
	// three obsolete snapshots can be explicitly retired before extending it.
	for _, generation := range []struct{ id, snapshot string }{{"recovered", strings.Repeat("b", 32)}, {"child", strings.Repeat("c", 32)}, {"child", strings.Repeat("d", 32)}} {
		record, err := engine.read(generation.id)
		if err != nil || record.Phase != "deleted" {
			t.Fatal("original descendant not deleted", err)
		}
		relative, _ := checkpointPath(generation.id, generation.snapshot)
		var saved Checkpoint
		if err := engine.readCheckpointJSON(relative+"/checkpoint.json", &saved); err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(saved)
		boundedLossAction(t, retirementArgs("retire-checkpoint", CheckpointRequest{record.ID, saved.ID, record.Owner, sum(data)}, "")...)
	}
	// A private unaccepted preparation marker must not turn an intact pause
	// into an unreadable recovery source when preparation cannot finish.
	if err := old.root.Mkdir(epochRetirementDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	reader, err := old.recoveryLock(pause.Request)
	if err != nil {
		t.Fatal("unaccepted preparation fenced the old recovery reader", err)
	}
	reader.Close()
	oldRequest := CheckpointRequest{pause.Request.SourceID, pause.Request.SnapshotID, pause.Request.SourceOwner, pause.Request.CheckpointSHA256}
	inspect := retirementArgs("inspect-recovery-checkpoint", oldRequest, "")
	inspect = append([]string{inspect[0], "--source-root", old.config.Root}, inspect[1:]...)
	boundedLossAction(t, inspect...)
	if err := old.root.Remove(epochRetirementDirectory); err != nil {
		t.Fatal(err)
	}
	view := boundedLossView(t, "recover-paused", "--source-root", pause.Request.SourceRoot, "--source", pause.Request.SourceID,
		"--snapshot-id", pause.Request.SnapshotID, "--source-owner", pause.Request.SourceOwner, "--checkpoint-sha256", pause.Request.CheckpointSHA256, "handover")
	boundedLossActor(t, view, pause.State)
	state, err := readCaptureActor(view.Runtime.PID, "advance")
	if err != nil || state.Counter != pause.State.Counter+1 {
		t.Fatal("handover did not perform independent useful work", err)
	}
	boundedLossActor(t, view, state)
	request := EpochRetirementRequest{SourceRoot: old.config.Root}
	wrong := 0
	refuse := func(req EpochRetirementRequest, label string) {
		t.Helper()
		before, err := ownedtree.Capture(context.Background(), old.root, []string{"objects", "forks", "bases", "runtime"})
		if err != nil {
			t.Fatal(err)
		}
		if output, err := epochRetirementCLI(t, "begin-epoch-retirement", req, "epoch-wrong-"+label+".json"); err == nil {
			t.Fatal("invalid public epoch retirement accepted", label, string(output))
		}
		after, err := ownedtree.Capture(context.Background(), old.root, before.Paths)
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatal("refused epoch retirement changed old inputs", label, err)
		}
		if _, err := old.root.Lstat(epochRetirementDirectory); !os.IsNotExist(err) {
			t.Fatal("refusal published source preparation", label, err)
		}
		if _, err := os.Lstat(status.Store + "/" + epochRetirementFenceName); !os.IsNotExist(err) {
			t.Fatal("refusal published global maintenance fence", label, err)
		}
		wrong++
	}
	refuse(request, "running-successor")
	snapshot := strings.Repeat("e", 32)
	paused := boundedLossView(t, "pause", "--snapshot-id", snapshot, "handover")
	if err := committedExitPause(paused.Record); err != nil {
		t.Fatal(err)
	}
	parentCheckpoint := CheckpointRequest{"handover", snapshot, paused.Record.Owner, paused.Record.Pause.CheckpointSHA256}
	child := boundedLossView(t, "fork", "--source", "handover", "--snapshot-id", snapshot, "descendant")
	boundedLossActor(t, child, state)
	state, err = readCaptureActor(child.Runtime.PID, "advance")
	if err != nil || state.Counter != pause.State.Counter+2 {
		t.Fatal("handover descendant did not advance independently", err)
	}
	boundedLossActor(t, child, state)
	boundedLossAction(t, "delete", "handover")
	snapshot = strings.Repeat("f", 32)
	paused = boundedLossView(t, "pause", "--snapshot-id", snapshot, "descendant")
	if err := committedExitPause(paused.Record); err != nil {
		t.Fatal(err)
	}
	boundedLossAction(t, retirementArgs("retire-checkpoint", parentCheckpoint, "")...)
	successor := RetainedCheckpointRequest{SourceRoot: status.Lifecycle.Root, CheckpointRequest: CheckpointRequest{"descendant", snapshot, paused.Record.Owner, paused.Record.Pause.CheckpointSHA256}}
	request.Handovers = []EpochRecoveryHandover{{Source: oldRequest, Successor: successor}}
	missing := request
	missing.Handovers = nil
	refuse(missing, "no-handover")
	changed := request
	changed.Handovers = append([]EpochRecoveryHandover(nil), request.Handovers...)
	changed.Handovers[0].Source.SourceOwner = strings.Repeat("0", 32)
	refuse(changed, "wrong-source-owner")
	changed = request
	changed.Handovers = append([]EpochRecoveryHandover(nil), request.Handovers...)
	changed.Handovers[0].Successor.CheckpointSHA256 = strings.Repeat("0", 64)
	refuse(changed, "wrong-successor-hash")
	// A valid active selection still protects the exact old checkpoint.
	relative, _ := oldRequest.path()
	selected := CheckpointSelection{Version: 1, ID: "retirement-protected", Request: oldRequest, SelectedAt: now()}
	if err := old.publishCheckpoint(relative, "selection-retirement-protected.json", selected); err != nil {
		t.Fatal(err)
	}
	refuse(request, "active-selection")
	release := retirementArgs("release-recovery-selection", oldRequest, selected.ID)
	release = append([]string{release[0], "--source-root", old.config.Root}, release[1:]...)
	boundedLossAction(t, release...)
	foreign := relative + "/unrecognized.json"
	if err := old.root.WriteFile(foreign, []byte("keep this unknown input\n"), 0600); err != nil {
		t.Fatal(err)
	}
	refuse(request, "unknown-entry")
	if err := old.root.Remove(foreign); err != nil {
		t.Fatal(err)
	}
	// A current covering mount is foreign even when its complete bytes match.
	images := old.config.Root + "/" + relative + "/images"
	before, err := ownedtree.Capture(context.Background(), old.root, []string{"objects", "forks", "bases", "runtime"})
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Mount(images, images, "", unix.MS_BIND, ""); err != nil {
		t.Fatal(err)
	}
	_, rejected := epochRetirementCLI(t, "begin-epoch-retirement", request, "epoch-wrong-covering-mount.json")
	if err := unix.Unmount(images, 0); err != nil {
		t.Fatal(err)
	}
	if rejected == nil {
		t.Fatal("foreign self-bind over retained RAM images was accepted")
	}
	after, err := ownedtree.Capture(context.Background(), old.root, before.Paths)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("foreign-mount refusal changed original inputs", err)
	}
	wrong++
	foreignOverlay := "/var/lib/sysbox/epoch-retirement-foreign-overlay"
	if err := os.Mkdir(foreignOverlay, 0700); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{"mount", "upper", "work"} {
		if err := os.Mkdir(foreignOverlay+"/"+directory, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := unix.Mount("overlay", foreignOverlay+"/mount", "overlay", 0, "index=off,lowerdir="+old.config.Root+"/"+relative+"/filesystem/lower,upperdir="+foreignOverlay+"/upper,workdir="+foreignOverlay+"/work"); err != nil {
		t.Fatal(err)
	}
	_, rejected = epochRetirementCLI(t, "begin-epoch-retirement", request, "epoch-wrong-foreign-overlay.json")
	if err := unix.Unmount(foreignOverlay+"/mount", 0); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{"mount", "upper", "work/work", "work"} {
		if err := os.Remove(foreignOverlay + "/" + directory); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	if err := os.Remove(foreignOverlay); err != nil {
		t.Fatal(err)
	}
	if rejected == nil {
		t.Fatal("an overlay outside the epoch still using its lower was accepted")
	}
	wrong++
	acceptedBytes, err := epochRetirementCLI(t, "begin-epoch-retirement", request, "epoch-retirement-request.json")
	if err != nil {
		t.Fatal(err, string(acceptedBytes))
	}
	var accepted EpochRetirement
	if err := json.Unmarshal(acceptedBytes, &accepted); err != nil || accepted.Version != 1 || accepted.AcceptedAt == "" || accepted.CompletedAt != "" || !reflect.DeepEqual(accepted.Request, request) {
		t.Fatal("public begin did not durably accept the exact request", err)
	}
	if replay, err := epochRetirementCLI(t, "begin-epoch-retirement", request, "epoch-retirement-request.json"); err != nil || !bytes.Equal(replay, acceptedBytes) {
		t.Fatal("public acceptance replay changed", err)
	}
	// Interrupt after one real accepted payload root has been removed. This is
	// the same exact owned-object remover as the complete public deletion path;
	// a surviving RAM/file successor remains protected by the actual fence.
	plan, err := ownedtree.Capture(context.Background(), old.root, []string{relative + "/images"})
	if err != nil {
		t.Fatal(err)
	}
	if err := ownedtree.RemoveRemaining(context.Background(), old.root, plan); err != nil {
		t.Fatal(err)
	}
	if _, err := old.root.Lstat(relative + "/images"); !os.IsNotExist(err) {
		t.Fatal("partial retirement did not remove its exact original RAM root", err)
	}
	if _, err := boundedLossCommand("--worker-run-root", "/run/factorybox", "resume", "--snapshot-id", snapshot, "descendant"); err == nil || !strings.Contains(err.Error(), "epoch retirement") {
		t.Fatal("accepted partial retirement failed to protect its successor", err)
	}
	boundedLossSave(t, "epoch-retirement-accepted.json", epochRetirementNativeState{Request: request, Accepted: accepted, State: state, RemovedRoot: relative + "/images", WrongRequests: wrong})
}

func TestNativeEpochRetirementFinish(t *testing.T) {
	epochRetirementNativeEnabled(t)
	var evidence epochRetirementNativeState
	epochFenceRead(t, "epoch-retirement-accepted.json", &evidence)
	status := boundedLossStatusNow(t)
	if status.Lifecycle.Root == evidence.Request.Handovers[0].Successor.SourceRoot {
		t.Fatal("accepted worker was not replaced")
	}
	engine, err := Open(status.Lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	old, err := engine.openRetainedController(evidence.Request.SourceRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	for _, name := range []string{"foreign-after-acceptance.json", epochRetirementDirectory + "/foreign-after-acceptance.json"} {
		before, err := ownedtree.Capture(context.Background(), old.root, []string{"objects", "forks", "bases", "runtime"})
		if err != nil {
			t.Fatal(err)
		}
		content := []byte("preserve unknown input after accepted partial deletion\n")
		if err := old.root.WriteFile(name, content, 0600); err != nil {
			t.Fatal(err)
		}
		output, rejected := epochRetirementCLI(t, "retire-epoch", evidence.Request, "epoch-retirement-request.json")
		if rejected == nil || !strings.Contains(rejected.Error(), "unknown") {
			t.Fatal("unknown accepted-retirement input was not refused", name, rejected, string(output))
		}
		after, err := ownedtree.Capture(context.Background(), old.root, before.Paths)
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatal("refused accepted retirement changed original backing", name, err)
		}
		if saved, err := old.root.ReadFile(name); err != nil || !bytes.Equal(saved, content) {
			t.Fatal("refused retirement changed foreign input", name, err)
		}
		if _, err := os.Lstat(status.Store + "/" + epochRetirementFenceName); err != nil {
			t.Fatal("refused retirement lost its accepted fence", err)
		}
		if err := old.root.Remove(name); err != nil {
			t.Fatal(err)
		}
		evidence.WrongRequests++
	}
	data, err := epochRetirementCLI(t, "retire-epoch", evidence.Request, "epoch-retirement-request.json")
	if err != nil {
		t.Fatal(err, string(data))
	}
	var completed EpochRetirement
	if err := json.Unmarshal(data, &completed); err != nil || completed.Version != 1 || completed.CompletedAt == "" || completed.AcceptanceSHA256 != evidence.Accepted.AcceptanceSHA256 || completed.AcceptedAt != evidence.Accepted.AcceptedAt {
		t.Fatal("public retirement did not complete its original accepted request", err)
	}
	if replay, err := epochRetirementCLI(t, "retire-epoch", evidence.Request, "epoch-retirement-request.json"); err != nil || !bytes.Equal(replay, data) {
		t.Fatal("completed epoch retirement replay changed", err)
	}
	var prepared epochRetirementPreparation
	if err := old.readCheckpointJSON(epochRetirementDirectory+"/prepare.json", &prepared); err != nil {
		t.Fatal(err)
	}
	plan, err := old.readRetirementInventory(epochRetirementDirectory, prepared.InventorySHA256)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Paths) < 3 {
		t.Fatal("public epoch inventory omitted filesystem and RAM payload roots")
	}
	for _, name := range plan.Paths {
		if _, err := old.root.Lstat(name); !os.IsNotExist(err) {
			t.Fatal("retired epoch payload or successor remains", name, err)
		}
	}
	if _, err := os.Lstat(status.Store + "/" + epochRetirementFenceName); !os.IsNotExist(err) {
		t.Fatal("global maintenance fence remains", err)
	}
	if err := old.epochRetirementReadFence(); err == nil {
		t.Fatal("source tombstone no longer fences old recovery")
	}
	successor := evidence.Request.Handovers[0].Successor
	view := boundedLossView(t, "recover-paused", "--source-root", successor.SourceRoot, "--source", successor.SourceID, "--snapshot-id", successor.SnapshotID,
		"--source-owner", successor.SourceOwner, "--checkpoint-sha256", successor.CheckpointSHA256, "successor")
	boundedLossActor(t, view, evidence.State)
	state, err := readCaptureActor(view.Runtime.PID, "advance")
	if err != nil || state.Counter != evidence.State.Counter+1 {
		t.Fatal("independently preserved successor cannot do useful work", err)
	}
	// Retire the next obsolete epoch too. It contains completed imports,
	// forks, resume backing, retired checkpoints and a cold committed pause.
	paused := boundedLossView(t, "pause", "--snapshot-id", strings.Repeat("1", 32), "successor")
	if err := committedExitPause(paused.Record); err != nil {
		t.Fatal(err)
	}
	next := EpochRetirementRequest{SourceRoot: successor.SourceRoot, Handovers: []EpochRecoveryHandover{{Source: successor.CheckpointRequest,
		Successor: RetainedCheckpointRequest{SourceRoot: status.Lifecycle.Root, CheckpointRequest: CheckpointRequest{"successor", strings.Repeat("1", 32), paused.Record.Owner, paused.Record.Pause.CheckpointSHA256}}}}}
	second, err := epochRetirementCLI(t, "retire-epoch", next, "epoch-retirement-second.json")
	if err != nil {
		t.Fatal("second epoch with fork/resume history", err, string(second))
	}
	if replay, err := epochRetirementCLI(t, "retire-epoch", next, "epoch-retirement-second.json"); err != nil || !bytes.Equal(replay, second) {
		t.Fatal("second epoch replay changed", err)
	}
	view = boundedLossView(t, "resume", "--snapshot-id", strings.Repeat("1", 32), "successor")
	boundedLossActor(t, view, state)
	if after, err := readCaptureActor(view.Runtime.PID, "advance"); err != nil || after.Counter != state.Counter+1 {
		t.Fatal("successor cannot work after second epoch deletion", err)
	}
	boundedLossAction(t, "delete", "successor")
	boundedLossSave(t, "epoch-retirement-finished.json", map[string]any{"passed": true, "public_epoch_retirement_passed": true, "partial_payloads_removed_before_worker_loss": true,
		"all_authorized_epoch_payloads_absent": true, "source_metadata_preserved": true, "request_replay_exact": true, "independent_successor_recovered_and_useful": true,
		"source_tombstone_retained": true, "global_fence_cleared_exactly": true, "wrong_requests_refused": evidence.WrongRequests,
		"prepared_source_reader_available": true, "unknown_source_entry_refused": true, "foreign_covering_mount_refused": true, "active_selection_preserved": true,
		"full_original_lifecycle_assertions_passed": true, "fork_resume_epoch_retired": true, "descendant_handover_verified": true,
		"foreign_entries_after_acceptance_refused": true, "full_epoch_retirement_qualified": false, "report": completed})
}
