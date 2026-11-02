//go:build linux

package lifecycle

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func abortedExitCaptureFixture(t *testing.T) (*Engine, Record, string) {
	t.Helper()
	_, engine, record, relative := rejectedEpochCaptureFixture(t)
	var config captureGuardConfig
	if err := engine.readCheckpointJSON(relative+"/guard/guard-intent.json", &config); err != nil {
		t.Fatal(err)
	}
	if err := engine.root.Remove(relative + "/admission-rejected.json"); err != nil {
		t.Fatal(err)
	}
	abort := captureAbortIntent{Version: 1, Token: config.Token, FailedAt: record.PauseHistory[0].RejectedAt,
		Error: "fixture filesystem copy deadline"}
	record.PauseHistory[0].Error = (&checkpointCaptureError{cause: os.ErrDeadlineExceeded}).Error()
	abort.Error = os.ErrDeadlineExceeded.Error()
	if err := engine.write(record); err != nil {
		t.Fatal(err)
	}
	state := []byte(`{"fixture":"original source state"}`)
	worker := captureWorkerIntent{Token: config.Token, PID: 2147483646, Start: "original-worker-start",
		SourcePID: config.PID, CriuSHA256: config.CriuSHA256, Scope: "ordinary complete retained-source capture"}
	for name, value := range map[string]any{
		"guard/capture-abort-intent.json": abort,
		"guard/guard-capture-aborted.json": map[string]any{
			"token": config.Token, "capture_in_flight": false,
		},
		"guard/capture-worker-intent.json": worker,
		"guard/capture-process-complete.json": captureProcessCompletion{
			Version: 1, Worker: worker, SourceStart: config.Start, SourceStateSHA: sum(state),
		},
	} {
		data, _ := json.Marshal(value)
		if err := engine.root.WriteFile(relative+"/"+name, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := engine.root.WriteFile(relative+"/source-state.json", state, 0600); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{"images", "filesystem/upper", "filesystem/volumes"} {
		if err := engine.root.MkdirAll(relative+"/"+directory, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := engine.root.WriteFile(relative+"/images/partial.img", []byte("retained diagnostic payload"), 0600); err != nil {
		t.Fatal(err)
	}
	return engine, record, relative
}

func TestWorkerExitPreservesRecoveredFailedCapture(t *testing.T) {
	engine, record, relative := abortedExitCaptureFixture(t)
	before, err := engine.root.ReadFile(relative + "/images/partial.img")
	if err != nil {
		t.Fatal(err)
	}
	aliases, err := engine.exitCaptureAliases(context.Background(), record, nil)
	if err != nil || len(aliases) != 0 {
		t.Fatal("confirmed deleted source with recovered failed capture blocks worker exit", aliases, err)
	}
	after, err := engine.root.ReadFile(relative + "/images/partial.img")
	if err != nil || string(before) != string(after) {
		t.Fatal("worker exit inventory changed retained failed-capture bytes", err)
	}
	if _, err := engine.root.Lstat(relative + "/checkpoint.json"); !os.IsNotExist(err) {
		t.Fatal("worker exit inventory manufactured a complete checkpoint", err)
	}
}

func TestWorkerExitPreservesOtherClosedFailedCaptures(t *testing.T) {
	t.Run("pre-capture-rejection", func(t *testing.T) {
		_, engine, record, _ := rejectedEpochCaptureFixture(t)
		if _, err := engine.exitCaptureAliases(context.Background(), record, nil); err != nil {
			t.Fatal("confirmed closed pre-capture rejection blocks worker exit", err)
		}
	})
	t.Run("explicit-failed-capture-delete", func(t *testing.T) {
		current, forest, record, _ := deletedFailedCaptureDependency(t)
		for _, engine := range forest.controllers {
			if engine != current {
				if _, err := engine.exitCaptureAliases(context.Background(), record, nil); err != nil {
					t.Fatal("confirmed admitted failed-capture Delete blocks worker exit", err)
				}
			}
		}
	})
}

func TestWorkerExitRejectsUnprovedRecoveredCapture(t *testing.T) {
	for _, scenario := range []string{
		"unfinished-delete", "failed-delete", "missing-history", "duplicate-generation", "changed-error",
		"changed-time", "completed-pause", "resuming-pause", "missing-ack", "in-flight", "foreign-token",
		"guard-source-changed", "guard-not-passed", "guard-error", "changed-source-state",
		"missing-completion", "foreign-worker", "published-checkpoint", "published-capture", "filesystem-manifest",
	} {
		t.Run(scenario, func(t *testing.T) {
			engine, record, relative := abortedExitCaptureFixture(t)
			write := func(name string, value any) {
				t.Helper()
				data, _ := json.Marshal(value)
				if err := engine.root.WriteFile(relative+"/"+name, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			switch scenario {
			case "unfinished-delete":
				record.Delete.CompletedAt = ""
			case "failed-delete":
				record.Delete.Error = "unconfirmed"
			case "missing-history":
				record.PauseHistory = nil
			case "duplicate-generation":
				record.PauseHistory = append(record.PauseHistory, record.PauseHistory[0])
			case "changed-error":
				record.PauseHistory[0].Error = "different"
			case "changed-time":
				record.PauseHistory[0].RejectedAt = "different"
			case "completed-pause":
				record.PauseHistory[0].CompletedAt = "published"
			case "resuming-pause":
				record.PauseHistory[0].Resume = &Operation{RequestedAt: "resume"}
			case "missing-ack":
				if err := engine.root.Remove(relative + "/guard/guard-capture-aborted.json"); err != nil {
					t.Fatal(err)
				}
			case "in-flight", "foreign-token":
				token, inFlight := record.PauseHistory[0].SnapshotID, scenario == "in-flight"
				if scenario == "foreign-token" {
					token = strings.Repeat("f", 32)
				}
				write("guard/guard-capture-aborted.json", map[string]any{"token": token, "capture_in_flight": inFlight})
			case "guard-source-changed", "guard-not-passed", "guard-error":
				var outcome map[string]any
				if err := engine.readCheckpointJSON(relative+"/guard/guard-outcome.json", &outcome); err != nil {
					t.Fatal(err)
				}
				if scenario == "guard-source-changed" {
					outcome["config"].(map[string]any)["root"] = "/foreign/controller"
				} else if scenario == "guard-not-passed" {
					outcome["passed"] = false
				} else {
					outcome["error"] = "unconfirmed recovery"
				}
				write("guard/guard-outcome.json", outcome)
			case "changed-source-state":
				write("source-state.json", map[string]bool{"changed": true})
			case "missing-completion":
				if err := engine.root.Remove(relative + "/guard/capture-process-complete.json"); err != nil {
					t.Fatal(err)
				}
			case "foreign-worker":
				var worker captureWorkerIntent
				if err := engine.readCheckpointJSON(relative+"/guard/capture-worker-intent.json", &worker); err != nil {
					t.Fatal(err)
				}
				worker.Token = strings.Repeat("f", 32)
				write("guard/capture-worker-intent.json", worker)
			case "published-checkpoint":
				write("checkpoint.json", map[string]bool{})
			case "published-capture":
				write("capture-result.json", map[string]bool{})
			case "filesystem-manifest":
				write("filesystem.json", map[string]bool{})
			}
			if _, err := engine.exitCaptureAliases(context.Background(), record, nil); err == nil {
				t.Fatal("worker exit ignored an unproved or changed failed capture")
			}
		})
	}
}
