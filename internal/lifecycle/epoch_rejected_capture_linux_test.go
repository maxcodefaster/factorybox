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

func rejectedEpochCaptureFixture(t *testing.T) (*Engine, *Engine, Record, string) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("requires root-owned controller metadata; exercised in the explicit metadata lane")
	}
	current, row := managerDependencyFixture(t, "current", strings.Repeat("a", 32), "deleted", "")
	engine, record := managerDependencyFixture(t, "peer", strings.Repeat("b", 32), "deleted", "")
	var filesystem unix.Stat_t
	if err := unix.Stat(current.config.Root, &filesystem); err != nil {
		t.Fatal(err)
	}
	current.config.BoundedStore = &storefs.Active{Target: "/tmp", BlockDevice: filesystem.Dev}
	engine.config.Criu, engine.config.CriuSHA256 = "/owned/criu", strings.Repeat("d", 64)
	for owner, value := range map[*Engine]*Record{current: &row, engine: &record} {
		value.Start = Operation{RequestedAt: "requested", Attempted: true, CompletedAt: "started"}
		value.Delete.RequestedAt = "delete-requested"
		if err := owner.write(*value); err != nil {
			t.Fatal(err)
		}
	}
	id := strings.Repeat("c", 32)
	pause := PauseOperation{SnapshotID: id, RequestedAt: "pause-requested", SourcePID: 42}
	original := record
	original.Phase, original.Delete, original.Pause = "pausing", nil, &pause
	data, _ := sandboxRecordBytes(original)
	config := captureGuardConfig{
		Version: 1, Token: id, PID: 42, Controller: 43, Start: "original-start",
		Root: engine.config.Root, StopSource: true, Source: original,
		Criu: engine.config.Criu, CriuSHA256: engine.config.CriuSHA256,
		RecordSHA: sum(append(data, '\n')),
	}
	rejection := checkpointAdmissionRejection{Version: 1, SourceID: record.ID, SnapshotID: id,
		SourcePID: 42, Kind: "timers", RejectedAt: "rejected",
		Error: checkpointAdmissionPrefix("timers") + "original source refused"}
	intent := checkpointIntent{
		Version: 1, ID: id, SourceID: record.ID, RuntimeID: record.RuntimeID, Owner: record.Owner,
		BootID: record.BootID, ConfigSHA256: record.ConfigSHA256, RuntimeSHA256: record.RuntimeSHA256,
		CriuSHA256: engine.config.CriuSHA256, ManagerRoot: engine.config.ManagerRoot,
		RequestedAt: pause.RequestedAt, StopSource: true,
	}
	record.PauseHistory = []PauseOperation{pause}
	record.PauseHistory[0].RejectedAt = rejection.RejectedAt
	record.PauseHistory[0].Error = rejection.Error
	if err := engine.write(record); err != nil {
		t.Fatal(err)
	}
	relative, _ := checkpointPath(record.ID, id)
	if err := engine.root.MkdirAll(relative+"/guard", 0700); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]any{
		"admission-rejected.json": rejection,
		"checkpoint-intent.json":  intent,
		"guard/guard-intent.json": config,
		"guard/guard-outcome.json": map[string]any{
			"config": config, "reason": "controller_requested_recovery",
			"status": "original_source_thawed", "passed": true, "capture_in_flight": false,
		},
	} {
		data, _ := json.Marshal(value)
		if err := engine.root.WriteFile(relative+"/"+name, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return current, engine, record, relative
}

func TestEpochRejectedCaptureIsRetainedMetadataWithoutCheckpoint(t *testing.T) {
	current, engine, record, relative := rejectedEpochCaptureFixture(t)
	before, err := engine.root.ReadFile(relative + "/admission-rejected.json")
	if err != nil {
		t.Fatal(err)
	}
	forest := &epochRetirementForest{controllers: map[string]*Engine{
		current.config.Root: current, engine.config.Root: engine,
	}}
	if err := current.epochDependencies(context.Background(), forest, EpochRetirementRequest{
		SourceRoot: "/different-retiring-epoch/controller",
	}); err != nil {
		t.Fatal("proved pre-capture rejection blocks unrelated retirement", err)
	}
	builder := &epochInventoryBuilder{engine: engine, ctx: context.Background(), allowed: map[string]uint32{}}
	if err := builder.captures(record); err != nil {
		t.Fatal("proved rejection cannot be retained in its own epoch inventory", err)
	}
	if len(builder.payloads) != 0 || builder.allowed[relative+"/admission-rejected.json"] != unix.S_IFREG|0600 {
		t.Fatal("a rejected capture became payload deletion authority", builder.allowed, builder.payloads)
	}
	after, err := engine.root.ReadFile(relative + "/admission-rejected.json")
	if err != nil || string(before) != string(after) {
		t.Fatal("inventory rewrote original rejection evidence", err)
	}
}

func TestEpochRejectedCaptureRequiresClosedHistoryAndSafeGuard(t *testing.T) {
	for _, scenario := range []string{
		"unconfirmed-delete", "failed-delete", "missing-history", "duplicate-generation",
		"changed-rejection-time", "changed-error", "completed-pause", "resume",
		"capture-entered", "published-checkpoint", "images", "retained-helper", "unknown-guard",
		"guard-still-capturing", "guard-not-passed", "guard-source-changed",
	} {
		t.Run(scenario, func(t *testing.T) {
			_, engine, record, relative := rejectedEpochCaptureFixture(t)
			write := func(name string, value any) {
				t.Helper()
				data, _ := json.Marshal(value)
				if err := engine.root.WriteFile(relative+"/"+name, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			switch scenario {
			case "unconfirmed-delete":
				record.Delete.CompletedAt = ""
			case "failed-delete":
				record.Delete.Error = "unconfirmed"
			case "missing-history":
				record.PauseHistory = nil
			case "duplicate-generation":
				record.PauseHistory = append(record.PauseHistory, record.PauseHistory[0])
			case "changed-rejection-time":
				record.PauseHistory[0].RejectedAt = "different"
			case "changed-error":
				record.PauseHistory[0].Error = "different"
			case "completed-pause":
				record.PauseHistory[0].CompletedAt = "published"
			case "resume":
				record.PauseHistory[0].Resume = &Operation{RequestedAt: "resume"}
			case "capture-entered":
				write("guard/guard-capture-entered.json", map[string]bool{})
			case "published-checkpoint":
				write("checkpoint.json", map[string]bool{})
			case "images":
				if err := engine.root.Mkdir(relative+"/images", 0700); err != nil {
					t.Fatal(err)
				}
			case "retained-helper":
				write("guard/criu-retained-arm.json", map[string]bool{})
			case "unknown-guard":
				write("guard/unknown.json", map[string]bool{})
			default:
				var outcome map[string]any
				if err := engine.readCheckpointJSON(relative+"/guard/guard-outcome.json", &outcome); err != nil {
					t.Fatal(err)
				}
				if scenario == "guard-still-capturing" {
					outcome["capture_in_flight"] = true
				} else if scenario == "guard-not-passed" {
					outcome["passed"] = false
				} else {
					outcome["config"].(map[string]any)["root"] = "/foreign/controller"
				}
				write("guard/guard-outcome.json", outcome)
			}
			builder := &epochInventoryBuilder{engine: engine, ctx: context.Background(), allowed: map[string]uint32{}}
			if err := builder.captures(record); err == nil {
				t.Fatal("unproved rejection was admitted to an epoch deletion inventory")
			}
		})
	}
}
