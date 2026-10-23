//go:build linux && factorybox_worker_probe

package lifecycle

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Seed only the owned fixture's historical rejection metadata. All three
// stopped pauses, their RAM/files and all public base imports are real. This
// represents readable legacy history, not a claim of that many real failures.
func TestNativeWorkerMetadataBaseAdmission(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_WORKER_METADATA") != "1" {
		t.Skip("owned delivered worker-metadata fixture only")
	}
	TestNativeBoundedLossPrepare(t)
	status := boundedLossStatusNow(t)
	var actor boundedLossPause
	raw, err := os.ReadFile("/output/bounded-loss-pause.json")
	if err != nil || json.Unmarshal(raw, &actor) != nil {
		t.Fatal("read actual committed actor", err)
	}
	ids := []string{"source", "ledger-paused-a", "ledger-paused-b"}
	baseCount := 200
	minimal := os.Getenv("FACTORYBOX_WORKER_METADATA_MINIMAL") == "1"
	if minimal {
		ids = append(ids, "ledger-paused-c")
		baseCount = 1
	}
	for index, id := range ids[1:] {
		child := boundedLossView(t, "fork", "--source", "source", "--snapshot-id", actor.Pause.SnapshotID, id)
		boundedLossActor(t, child, actor.State)
		view := boundedLossView(t, "pause", "--snapshot-id", fmt.Sprintf("%032x", index+128), id)
		if err := committedExitPause(view.Record); err != nil || view.Runtime == nil || view.Runtime.Status != "stopped" {
			t.Fatal("requires a real committed stopped descendant", id, err, view.Runtime)
		}
	}
	const timestamp = "9999-12-31T23:59:59.999999999Z"
	ledger := workerBaseRelease{Version: 1, Root: status.Lifecycle.Root, BootID: status.BootID,
		RuntimeSHA256: status.Lifecycle.RuntimeSHA256, CriuSHA256: status.Lifecycle.CriuSHA256,
		ManagerRoot: status.Lifecycle.ManagerRoot, RequestedAt: timestamp, CompletedAt: timestamp}
	for _, id := range ids {
		view := boundedLossView(t, "state", id)
		if err := committedExitPause(view.Record); err != nil || view.Record.Base != nil {
			t.Fatal("fixture requires exact pauses without immutable aliases", id, err)
		}
		ledger.Paused = append(ledger.Paused, view.Record)
	}
	source := filepath.Join(status.Store, "worker-metadata-source")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	for len(source)+221 < 3500 {
		source += "/segment-" + strings.Repeat("x", 210)
		if err := os.Mkdir(source, 0700); err != nil {
			t.Fatal(err)
		}
	}
	const payload = "owned worker metadata base\n"
	if err := os.WriteFile(source+"/owned.txt", []byte(payload), 0600); err != nil {
		t.Fatal(err)
	}
	canonicalBytes := 0
	for index := 0; index < baseCount; index++ {
		id := fmt.Sprintf("ledger-base-%04d", index)
		data := boundedLossAction(t, "prepare-base", "--source", source, id)
		var base Base
		if err := json.Unmarshal(data, &base); err != nil || base.PreparedAt == "" {
			t.Fatal("public base import did not complete", err)
		}
		encoded, err := json.Marshal(base)
		if err != nil {
			t.Fatal(err)
		}
		canonicalBytes += len(encoded) + 1
		ledger.Bases = append(ledger.Bases, base)
	}
	if canonicalBytes >= (2<<20)-32768 {
		t.Fatal("fixture must leave canonical budget for the next public import", canonicalBytes)
	}
	encode := func() []byte {
		t.Helper()
		data, err := json.Marshal(ledger)
		if err != nil {
			t.Fatal(err)
		}
		return append(data, '\n')
	}
	for index := range ledger.Paused {
		pause := ledger.Paused[index].Pause
		ledger.Paused[index].PauseHistory = append(ledger.Paused[index].PauseHistory,
			PauseOperation{SnapshotID: fmt.Sprintf("%032x", index+256), SourcePID: pause.SourcePID,
				RequestedAt: pause.RequestedAt, RejectedAt: pause.CompletedAt, Error: "injected legacy rejection; "})
	}
	const target = (4 << 20) - 2048
	remaining := target - len(encode())
	if remaining <= 0 {
		t.Fatal("unseeded actual ledger already exhausts the target", len(encode()))
	}
	for index := range ledger.Paused {
		amount := remaining / (len(ledger.Paused) - index)
		record := &ledger.Paused[index]
		record.PauseHistory[len(record.PauseHistory)-1].Error += strings.Repeat("x", amount)
		remaining -= amount
	}
	if remaining != 0 || len(encode()) != target {
		t.Fatal("seeded complete ledger missed its exact encoded boundary", len(encode()), remaining)
	}
	recordBytes := make(map[string][]byte)
	for _, record := range ledger.Paused {
		data, err := json.MarshalIndent(record, "", "  ")
		if err != nil || len(data)+1 >= 1<<20 {
			t.Fatal("each seeded legacy record must remain readable", record.ID, len(data), err)
		}
		data = append(data, '\n')
		path := status.Lifecycle.Root + "/objects/" + record.ID + "/record.json"
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		view := boundedLossView(t, "state", record.ID)
		if view.Record.Phase != "paused" || view.Runtime == nil || view.Runtime.Status != "stopped" ||
			view.Runtime.Created != record.RuntimeCreated || view.Record.Pause.CheckpointSHA256 != record.Pause.CheckpointSHA256 {
			t.Fatal("history injection changed the actual committed pause", record.ID)
		}
		recordBytes[record.ID] = data
	}
	const next = "ledger-base-next"
	data, admissionErr := boundedLossCommand("--worker-run-root", "/run/factorybox", "prepare-base", "--source", source, next)
	if admissionErr == nil {
		var base Base
		if err := json.Unmarshal(data, &base); err != nil {
			t.Fatal(err)
		}
		ledger.Bases = append(ledger.Bases, base)
		encoded, err := json.Marshal(base)
		if err != nil {
			t.Fatal(err)
		}
		boundedLossSave(t, "worker-metadata-counterexample.json", map[string]any{
			"minimal_input":           minimal,
			"injected_legacy_history": true, "actual_committed_pauses": len(ids),
			"actual_public_base_imports": len(ledger.Bases), "prior_completion_bound_bytes": target,
			"accepted_completion_bound_bytes": len(encode()), "canonical_base_bytes": canonicalBytes + len(encoded) + 1,
			"limit_bytes": 4 << 20, "failure": "public import exceeded total worker exit metadata while canonical budget still fits",
		})
		t.Fatal("public import exceeded total worker exit metadata while canonical budget still fits", len(encode()))
	}
	if !strings.Contains(admissionErr.Error(), "new worker metadata refused") {
		t.Fatal("public import refused for an unrelated reason", admissionErr)
	}
	if _, err := os.Lstat(status.Lifecycle.Root + "/bases/" + next); !os.IsNotExist(err) {
		t.Fatal("refused import created backing or intent", err)
	}
	for id, expected := range recordBytes {
		data, err := os.ReadFile(status.Lifecycle.Root + "/objects/" + id + "/record.json")
		view := boundedLossView(t, "state", id)
		if err != nil || !bytes.Equal(data, expected) || view.Runtime == nil || view.Runtime.Status != "stopped" {
			t.Fatal("worker metadata refusal changed a committed source", id, err)
		}
	}
	for _, base := range ledger.Bases {
		data, err := os.ReadFile(base.Tree.Saved + "/owned.txt")
		if err != nil || string(data) != payload {
			t.Fatal("worker metadata refusal changed a prior canonical base", base.ID, err)
		}
	}
	boundedLossSave(t, "worker-metadata.json", map[string]any{
		"minimal_input": minimal,
		"passed":        true, "injected_legacy_history": true, "prior_completion_bound_bytes": target,
		"canonical_base_bytes": canonicalBytes, "actual_committed_pauses": len(ids),
		"actual_public_base_imports": len(ledger.Bases), "refused_before_intent": true,
		"committed_sources_unchanged": true, "canonical_backing_unchanged": true,
		"controller_root": status.Lifecycle.Root,
	})
}
