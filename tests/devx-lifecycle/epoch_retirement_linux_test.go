//go:build linux && factorybox_worker_probe

package devx_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"factorybox/internal/lifecycle"
	"factorybox/internal/ownedtree"
	"factorybox/internal/worker"
)

func consumerEpochCommand(t *testing.T, f *fixture, operation, requestPath string) []byte {
	t.Helper()
	// The new full-tree operation needs its own declared bound. Original
	// lifecycle commands keep their unchanged 90-second bounds; the driver
	// still owns one 420-second deadline for both workers and every operation.
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	f.index++
	command := exec.CommandContext(ctx, "/factorybox", "--worker-run-root", f.workerRunRoot,
		operation, "--request-file", requestPath)
	var output, failure bytes.Buffer
	command.Stdout, command.Stderr = &output, &failure
	before := f.resources()
	started := time.Now()
	err := command.Run()
	elapsed := time.Since(started).Seconds()
	stem := fmt.Sprintf("/output/cli-%02d-%s", f.index, operation)
	for suffix, data := range map[string][]byte{".json": output.Bytes(), ".stderr": failure.Bytes()} {
		if err := os.WriteFile(stem+suffix, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	contextError := ""
	if ctx.Err() != nil {
		contextError = ctx.Err().Error()
	}
	timing, marshalErr := json.Marshal(map[string]any{"arguments": []string{operation, "--request-file", requestPath},
		"seconds": elapsed, "operation_deadline_seconds": 180, "context_error": contextError,
		"worker_resources_before": before, "worker_resources_after": f.resources()})
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	if err := os.WriteFile(stem+".timing.json", append(timing, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if err != nil {
		t.Fatalf("public consumer %s failed: %v; context=%s: %s", operation, err, contextError, failure.String())
	}
	return output.Bytes()
}

// retireConsumerEpoch is selected only by the declared, ordered patch to a new
// worker-loss fixture copy. The repository's complete original test is intact.
func retireConsumerEpoch(t *testing.T, f *fixture, proof consumerPauseProof, restored lifecycle.View) lifecycle.View {
	t.Helper()
	status, err := worker.Current("/run/factorybox")
	if err != nil || status.Lifecycle.Root != f.root || proof.Paused.Base == nil || proof.Paused.Base.Immutable == nil {
		t.Fatal("epoch retirement lacks the original consumer's actually used base", err)
	}
	basePath := proof.SourceRoot + "/bases/developer-base/base.json"
	baseBytes, err := os.ReadFile(basePath)
	var base lifecycle.Base
	if err != nil || json.Unmarshal(baseBytes, &base) != nil || base.ID != "developer-base" ||
		base.Tree.Saved != proof.SourceRoot+"/bases/developer-base/rootfs" || base.Tree.Immutable == nil ||
		proof.Paused.Base.Immutable.Root != base.Tree.Immutable.Root {
		t.Fatal("consumer's registered base differs from its original paused source", err)
	}
	input := status.Store + "/devx/bundle/lower-input/opt/factorybox/sandbox.sh"
	inputBytes, err := os.ReadFile(input)
	if err != nil || len(inputBytes) == 0 {
		t.Fatal("consumer caller input marker unavailable", err)
	}
	snapshot := strings.Repeat("c", 32)
	paused := f.view("pause", "--snapshot-id", snapshot, "recovered")
	if paused.Record.Phase != "paused" || paused.Record.Pause == nil || paused.Record.Pause.CompletedAt == "" ||
		paused.Record.Pause.After.Anon != 0 || paused.Record.Pause.Snapshot.ResidentBytesAfter != 0 {
		t.Fatal("independent consumer successor lacks a committed cold pause")
	}
	request := lifecycle.EpochRetirementRequest{SourceRoot: proof.SourceRoot, Handovers: []lifecycle.EpochRecoveryHandover{{
		Source: lifecycle.CheckpointRequest{SourceID: proof.Paused.ID, SnapshotID: proof.Paused.Pause.SnapshotID,
			SourceOwner: proof.Paused.Owner, CheckpointSHA256: proof.Paused.Pause.CheckpointSHA256},
		Successor: lifecycle.RetainedCheckpointRequest{SourceRoot: f.root, CheckpointRequest: lifecycle.CheckpointRequest{
			SourceID: paused.Record.ID, SnapshotID: snapshot, SourceOwner: paused.Record.Owner,
			CheckpointSHA256: paused.Record.Pause.CheckpointSHA256}},
	}}}
	requestBytes, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	const requestPath = "/output/consumer-epoch-request.json"
	if err := os.WriteFile(requestPath, requestBytes, 0600); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	// Exercise the ordinary complete public command. Its own admission and
	// durable acceptance precede deletion in the same locked invocation. The
	// separate begin/kill/replay boundary has its dedicated native fixtures.
	completedBytes := consumerEpochCommand(t, f, "retire-epoch", requestPath)
	var completed lifecycle.EpochRetirement
	if err := json.Unmarshal(completedBytes, &completed); err != nil || completed.Version != 1 || completed.CompletedAt == "" ||
		completed.AcceptedAt == "" || len(completed.AcceptanceSHA256) != 64 || !reflect.DeepEqual(completed.Request, request) {
		t.Fatal("public consumer epoch retirement did not complete the accepted handover", err)
	}
	if replay := consumerEpochCommand(t, f, "retire-epoch", requestPath); !bytes.Equal(replay, completedBytes) {
		t.Fatal("public consumer retirement replay changed its completed report")
	}
	file, err := os.Open(proof.SourceRoot + "/.epoch-retirement/retirement-payload.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	var inventory ownedtree.Plan
	if err := json.NewDecoder(reader).Decode(&inventory); err != nil || inventory.Version != 1 || len(inventory.Paths) < 3 {
		t.Fatal("consumer retirement omitted its full authorized payload inventory", err)
	}
	foundBase, foundRAM := false, false
	for _, name := range inventory.Paths {
		if filepath.IsAbs(name) || filepath.Clean(name) != name || name == ".." || strings.HasPrefix(name, "../") {
			t.Fatal("consumer retirement payload escapes its source", name)
		}
		if _, err := os.Lstat(proof.SourceRoot + "/" + name); !os.IsNotExist(err) {
			t.Fatal("consumer retirement left an authorized payload or replacement", name, err)
		}
		foundBase = foundBase || name == "bases/developer-base/rootfs"
		foundRAM = foundRAM || name == "objects/source/captures/"+proof.Paused.Pause.SnapshotID+"/images"
	}
	if !foundBase || !foundRAM {
		t.Fatal("retirement did not remove both the used base and original RAM images")
	}
	if after, err := os.ReadFile(basePath); err != nil || !bytes.Equal(after, baseBytes) {
		t.Fatal("retirement changed original base provenance", err)
	}
	if after, err := os.ReadFile(input); err != nil || !bytes.Equal(after, inputBytes) {
		t.Fatal("retirement changed the caller input marker", err)
	}
	retirementSeconds := time.Since(started).Seconds()
	resumed := f.view("resume", "--snapshot-id", snapshot, "recovered")
	if resumed.Runtime == nil || resumed.Runtime.PID <= 1 || resumed.Record.Owner != restored.Record.Owner {
		t.Fatal("consumer successor cannot resume after retiring its complete old epoch")
	}
	if os.Getenv("FACTORYBOX_DEVX_MINIMAL") == "1" {
		if actual := minimalWorkerState(t, resumed, "", 60*time.Second); !reflect.DeepEqual(actual, *proof.Minimal) {
			t.Fatal("epoch retirement changed minimal successor RAM/files", actual, *proof.Minimal)
		}
	} else if actual := f.useful("consumer-after-epoch-retirement", resumed, time.Now(), ""); !reflect.DeepEqual(actual, *proof.Factory) {
		t.Fatal("epoch retirement changed Factory successor parent/child/app RAM/files", actual, *proof.Factory)
	}
	evidence, err := json.Marshal(map[string]any{"passed": true, "report": completed, "payload_roots": inventory.Paths,
		"used_registered_base_retired": true, "original_ram_images_retired": true, "all_authorized_payloads_absent": true,
		"base_provenance_preserved": true, "caller_input_marker_preserved": true, "request_replay_exact": true,
		"independent_successor_resumed_and_useful": true, "retirement_seconds": retirementSeconds,
		"full_epoch_retirement_qualified": false})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("/output/consumer-epoch-retirement.json", append(evidence, '\n'), 0644); err != nil {
		t.Fatal(err)
	}
	return resumed
}
