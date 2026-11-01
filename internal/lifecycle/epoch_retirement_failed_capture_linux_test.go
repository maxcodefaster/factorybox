//go:build linux

package lifecycle

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"factorybox/internal/storefs"
	"golang.org/x/sys/unix"
)

func deletedFailedCaptureDependency(t *testing.T) (*Engine, *epochRetirementForest, Record, string) {
	t.Helper()
	current, currentRecord := managerDependencyFixture(t, "current", strings.Repeat("a", 32), "deleted", "")
	peer, record := managerDependencyFixture(t, "peer", strings.Repeat("b", 32), "deleted", "")
	var filesystem unix.Stat_t
	if err := unix.Stat(current.config.Root, &filesystem); err != nil {
		t.Fatal(err)
	}
	current.config.BoundedStore = &storefs.Active{Target: "/tmp", BlockDevice: filesystem.Dev}
	peer.config.Criu, peer.config.CriuSHA256 = "/owned/criu", strings.Repeat("d", 64)
	for engine, row := range map[*Engine]*Record{current: &currentRecord, peer: &record} {
		row.Start = Operation{RequestedAt: "start-requested", Attempted: true, CompletedAt: "started"}
		row.Delete.RequestedAt = "delete-requested"
		if err := engine.write(*row); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := strings.Repeat("c", 32)
	record.Pause = &PauseOperation{SnapshotID: snapshot, RequestedAt: "pause-requested"}
	if err := peer.write(record); err != nil {
		t.Fatal(err)
	}
	original := record
	original.Phase, original.Delete = "pausing", nil
	source, err := sandboxRecordBytes(original)
	if err != nil {
		t.Fatal(err)
	}
	guard := captureGuardConfig{
		Version: 1, Token: snapshot, Root: peer.config.Root, Source: original,
		RecordSHA: sum(append(source, '\n')), StopSource: true,
		Criu: peer.config.Criu, CriuSHA256: peer.config.CriuSHA256,
	}
	relative, err := checkpointPath(record.ID, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := peer.root.MkdirAll(relative+"/guard", 0700); err != nil {
		t.Fatal(err)
	}
	admission := failedCaptureDeleteAdmission{
		Version: 1, SourceID: record.ID, Owner: record.Owner, RuntimeID: record.RuntimeID,
		SnapshotID: snapshot, RecordSHA: guard.RecordSHA, AdmittedAt: "admitted-before-delete",
		Evidence: map[string]string{},
	}
	for _, name := range []string{
		"guard/guard-intent.json", "checkpoint-intent.json", "guard/guard-outcome.json",
		"guard/guard-armed.json", "guard/capture-worker-intent.json", "guard/capture-worker-outcome.json",
		"source-state.json", "guard/capture-network-rollback.json",
	} {
		var value any = map[string]string{"original-evidence": name}
		if name == "guard/guard-intent.json" {
			value = guard
		}
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := peer.root.WriteFile(relative+"/"+name, data, 0600); err != nil {
			t.Fatal(err)
		}
		admission.Evidence[name] = sum(data)
	}
	data, _ := json.Marshal(admission)
	if err := peer.root.WriteFile(relative+"/capture-delete-admission.json", data, 0600); err != nil {
		t.Fatal(err)
	}
	return current, &epochRetirementForest{controllers: map[string]*Engine{
		current.config.Root: current, peer.config.Root: peer,
	}}, record, relative
}

func TestEpochDependenciesPreserveConfirmedDeletedFailedCapture(t *testing.T) {
	current, forest, record, relative := deletedFailedCaptureDependency(t)
	var peer *Engine
	for _, engine := range forest.controllers {
		if engine != current {
			peer = engine
		}
	}
	before, err := peer.root.ReadFile(relative + "/capture-delete-admission.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := current.epochDependencies(context.Background(), forest, EpochRetirementRequest{
		SourceRoot: "/different-retiring-epoch/controller",
	}); err != nil {
		t.Fatal("a confirmed deleted failed Pause blocks unrelated epoch retirement", err)
	}
	after, err := peer.root.ReadFile(relative + "/capture-delete-admission.json")
	if err != nil || string(before) != string(after) {
		t.Fatal("dependency inspection changed the retained failed-capture proof", err)
	}
	observed, err := peer.read(record.ID)
	if err != nil || observed.Phase != "deleted" || observed.Pause.CompletedAt != "" {
		t.Fatal("inspection rewrote deletion or invented a successful checkpoint", err)
	}
	if _, err := peer.root.Lstat(relative + "/checkpoint.json"); !os.IsNotExist(err) {
		t.Fatal("inspection manufactured a checkpoint", err)
	}
}

func TestEpochDependenciesRejectUnprovedFailedCapture(t *testing.T) {
	for _, scenario := range []string{
		"missing-admission", "changed-evidence", "extra-evidence", "foreign-owner",
		"foreign-original-record", "unconfirmed-delete", "failed-delete", "pausing",
		"published-checkpoint", "retained-helper", "completed-pause", "resuming-pause",
	} {
		t.Run(scenario, func(t *testing.T) {
			current, forest, record, relative := deletedFailedCaptureDependency(t)
			var peer *Engine
			for _, engine := range forest.controllers {
				if engine != current {
					peer = engine
				}
			}
			path := relative + "/capture-delete-admission.json"
			var admission failedCaptureDeleteAdmission
			if err := peer.readCheckpointJSON(path, &admission); err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "missing-admission":
				if err := peer.root.Remove(path); err != nil {
					t.Fatal(err)
				}
			case "changed-evidence":
				if err := peer.root.WriteFile(relative+"/guard/guard-outcome.json", []byte("changed"), 0600); err != nil {
					t.Fatal(err)
				}
			case "extra-evidence":
				admission.Evidence["unknown.json"] = strings.Repeat("f", 64)
			case "foreign-owner":
				admission.Owner = strings.Repeat("f", 32)
			case "foreign-original-record":
				admission.RecordSHA = strings.Repeat("f", 64)
			case "unconfirmed-delete":
				record.Delete.CompletedAt = ""
			case "failed-delete":
				record.Delete.Error = "unconfirmed runtime cleanup"
			case "pausing":
				record.Phase, record.Delete = "pausing", nil
			case "published-checkpoint":
				if err := peer.root.WriteFile(relative+"/checkpoint.json", []byte("{}"), 0600); err != nil {
					t.Fatal(err)
				}
			case "retained-helper":
				if err := peer.root.WriteFile(relative+"/guard/criu-retained-arm.json", []byte("{}"), 0600); err != nil {
					t.Fatal(err)
				}
			case "completed-pause":
				record.Pause.CompletedAt = "published"
			case "resuming-pause":
				record.Pause.Resume = &Operation{RequestedAt: "resume-requested"}
			}
			if scenario != "missing-admission" {
				data, _ := json.Marshal(admission)
				if err := peer.root.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := peer.write(record); err != nil {
				t.Fatal(err)
			}
			if err := current.epochDependencies(context.Background(), forest, EpochRetirementRequest{
				SourceRoot: "/different-retiring-epoch/controller",
			}); err == nil {
				t.Fatal("unproved or uncertain capture was ignored during dependency inspection")
			}
		})
	}
}
