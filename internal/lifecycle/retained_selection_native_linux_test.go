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
)

func TestNativeRetainedSelectionPrepare(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_RETAINED_SELECTION") != "1" {
		t.Skip("owned retained-epoch selection fixture only")
	}
	TestNativeBoundedLossPrepare(t)
	var pause boundedLossPause
	data, err := os.ReadFile("/output/bounded-loss-pause.json")
	if err != nil || json.Unmarshal(data, &pause) != nil {
		t.Fatal("read committed source pause", err)
	}
	request := CheckpointRequest{pause.Request.SourceID, pause.Request.SnapshotID, pause.Request.SourceOwner, pause.Request.CheckpointSHA256}
	for _, id := range []string{"recover-primary", "recover-spare"} {
		selected := boundedLossAction(t, retirementArgs("select-checkpoint", request, id)...)
		if replay := boundedLossAction(t, retirementArgs("select-checkpoint", request, id)...); !bytes.Equal(replay, selected) {
			t.Fatal("original retained selection replay changed", id)
		}
	}
	boundedLossSave(t, "retained-selection-prepared.json", map[string]any{"passed": true, "request": pause.Request,
		"committed_private_ram_bytes": pause.State.BallastBytes, "independent_selections": 2})
}

func TestNativeRetainedSelectionRecover(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_RETAINED_SELECTION") != "1" {
		t.Skip("owned retained-epoch selection fixture only")
	}
	var pause boundedLossPause
	data, err := os.ReadFile("/output/bounded-loss-pause.json")
	if err != nil || json.Unmarshal(data, &pause) != nil {
		t.Fatal("read committed source pause", err)
	}
	status := boundedLossStatusNow(t)
	if status.Lifecycle.Root == pause.Request.SourceRoot || status.StoreID != pause.Status.StoreID {
		t.Fatal("retained selection requires actual replacement of its old worker")
	}
	config := status.Lifecycle
	config.Root = pause.Request.SourceRoot
	root, err := os.OpenRoot(config.Root)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	old := &Engine{config: config, boot: status.BootID, root: root}
	request := CheckpointRequest{pause.Request.SourceID, pause.Request.SnapshotID, pause.Request.SourceOwner, pause.Request.CheckpointSHA256}
	relative, err := request.path()
	if err != nil {
		t.Fatal(err)
	}
	before, err := ownedtree.Capture(context.Background(), root, []string{"objects", "forks", "bases"})
	if err != nil {
		t.Fatal("inventory all retained backing before release", err)
	}
	inspectArgs := retirementArgs("inspect-recovery-checkpoint", request, "")
	inspectArgs = append([]string{inspectArgs[0], "--source-root", pause.Request.SourceRoot}, inspectArgs[1:]...)
	var inspected RetainedCheckpointInspection
	if err := json.Unmarshal(boundedLossAction(t, inspectArgs...), &inspected); err != nil || inspected.Version != 1 ||
		!inspected.FullContentVerified || inspected.Files <= 0 || inspected.FileBytes < 32<<20 || inspected.FilesystemTrees < 2 ||
		inspected.Request.SourceRoot != pause.Request.SourceRoot || inspected.Request.CheckpointRequest != request || inspected.VerifiedAt == "" {
		t.Fatal("retained inspection did not verify the complete exact saved generation", inspected, err)
	}
	selected, err := old.selection(relative, "recover-primary")
	if err != nil || selected.ReleasedAt != "" {
		t.Fatal("old active selection was not retained", err)
	}
	fenced := false
	if _, err := root.Lstat("worker-exit.json"); err == nil {
		fenced = true
		if _, err := old.ReleaseCheckpointSelection(context.Background(), request, "recover-primary"); err == nil || !strings.Contains(err.Error(), "closed for worker exit") {
			t.Fatal("the old ordinary lifecycle did not retain its exit fence", err)
		}
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	args := func(root string, checkpoint CheckpointRequest, id string) []string {
		out := retirementArgs("release-recovery-selection", checkpoint, id)
		return append([]string{out[0], "--source-root", root}, out[1:]...)
	}
	wrongOwner, wrongHash := request, request
	wrongOwner.SourceOwner = strings.Repeat("0", 32)
	wrongHash.CheckpointSHA256 = strings.Repeat("0", 64)
	for _, invalid := range [][]string{
		args(status.Lifecycle.Root, request, "recover-primary"),
		args(pause.Request.SourceRoot+"/../controller", request, "recover-primary"),
		args("/foreign/epochs/"+pause.Status.Epoch+"/controller", request, "recover-primary"),
		args(pause.Request.SourceRoot, wrongOwner, "recover-primary"),
		args(pause.Request.SourceRoot, wrongHash, "recover-primary"),
		args(pause.Request.SourceRoot, request, "unknown-selection"),
	} {
		if _, err := boundedLossCommand(append([]string{"--worker-run-root", "/run/factorybox"}, invalid...)...); err == nil {
			t.Fatal("incorrect retained selection release was accepted", invalid)
		}
		if _, err := root.Lstat(relative + "/selection-recover-primary-released.json"); !os.IsNotExist(err) {
			t.Fatal("refused maintenance published a release", err)
		}
	}
	primary := boundedLossAction(t, args(pause.Request.SourceRoot, request, "recover-primary")...)
	var released CheckpointSelection
	if err := json.Unmarshal(primary, &released); err != nil || released.Request != request || released.ID != selected.ID ||
		released.SelectedAt != selected.SelectedAt || released.ReleasedAt == "" {
		t.Fatal("release did not preserve the original exact selection", released, err)
	}
	if replay := boundedLossAction(t, args(pause.Request.SourceRoot, request, "recover-primary")...); !bytes.Equal(replay, primary) {
		t.Fatal("retained release replay changed its durable completion")
	}
	if err := old.checkpointSelections(relative, request); err == nil || !strings.Contains(err.Error(), "recover-spare") {
		t.Fatal("release withdrew an unrelated recovery selection", err)
	}
	boundedLossAction(t, args(pause.Request.SourceRoot, request, "recover-spare")...)
	if err := old.checkpointSelections(relative, request); err != nil {
		t.Fatal("both explicit releases did not resolve their selections", err)
	}
	if err := old.checkpointDependencies(context.Background(), request, nil); err == nil || !strings.Contains(err.Error(), "unresumed pause") {
		t.Fatal("releasing selections withdrew the committed pause protection", err)
	}
	if fenced {
		if _, err := old.lock(); err == nil {
			t.Fatal("maintenance reopened the old fenced lifecycle")
		}
	}
	// Run every original RAM/file, replay, descendant, pause/resume and useful
	// work assertion after release. Explicit selection release never drops data.
	TestNativeBoundedLossRecover(t)
	after, err := ownedtree.Capture(context.Background(), root, []string{"objects", "forks", "bases"})
	if err != nil {
		t.Fatal("inventory retained backing after actual recovery", err)
	}
	entries := after.Entries[:0]
	for _, entry := range after.Entries {
		if entry.Path != relative+"/selection-recover-primary-released.json" && entry.Path != relative+"/selection-recover-spare-released.json" {
			entries = append(entries, entry)
		}
	}
	after.Entries = entries
	if !reflect.DeepEqual(before, after) {
		t.Fatal("selection maintenance or recovery changed original retained backing, metadata or object ownership")
	}
	boundedLossSave(t, "retained-selection-released.json", map[string]any{"passed": true, "production_qualified": false,
		"source_root": pause.Request.SourceRoot, "current_root": status.Lifecycle.Root,
		"old_lifecycle_fence_preserved": fenced, "wrong_requests_refused": 6, "exact_release_replayed": true,
		"retained_checkpoint_inspected": true,
		"other_selection_preserved":     true, "unresumed_pause_still_protected": true,
		"original_backing_inventory_unchanged": true, "full_original_recovery_assertions_passed": true})
}
