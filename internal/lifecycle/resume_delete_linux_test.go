//go:build linux

package lifecycle

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"factorybox/internal/sysboxcapture"
)

func failedResumeDeleteFixture(t *testing.T) (*Engine, Record, string) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("root-owned checkpoint metadata is exercised in the explicit root lane")
	}
	engine, record, generation, relative, files := resumeCopyFixture(t)
	engine.config.Criu, engine.config.CriuSHA256 = "/owned/criu", strings.Repeat("c", 64)
	engine.config.RuntimeSHA256, engine.config.ManagerRoot = strings.Repeat("d", 64), "/owned/manager"
	engine.boot = "original-boot"
	record.Version, record.Owner, record.RuntimeID = 1, strings.Repeat("a", 32), "fbx-"+strings.Repeat("a", 32)
	record.ConfigSHA256, record.RuntimeSHA256, record.BootID = strings.Repeat("b", 64), engine.config.RuntimeSHA256, engine.boot
	record.Start = Operation{RequestedAt: "start-requested", Attempted: true, CompletedAt: "started"}
	record.RuntimeCreated = "original-runtime-birth"
	record.InputBundle, record.Rootfs = engine.config.Root+"/input", engine.config.Root+"/input/rootfs"
	record.Pause.RequestedAt = "pause-requested"
	record.Pause.Resume.Attempted, record.Pause.Resume.Error = true, "original failed restore"
	for _, file := range files {
		data, err := engine.root.ReadFile(generation + "/" + file.Name)
		if err != nil {
			t.Fatal(err)
		}
		if err := engine.root.WriteFile(relative+"/"+file.Name, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	saved := Checkpoint{Version: 1, ID: record.Pause.SnapshotID, SourceID: record.ID,
		RuntimeID: record.RuntimeID, Owner: record.Owner, BootID: record.BootID,
		ConfigSHA256: record.ConfigSHA256, RuntimeSHA256: record.RuntimeSHA256,
		CriuSHA256: engine.config.CriuSHA256, ManagerRoot: engine.config.ManagerRoot,
		CapturedAt: "captured", ReleasedAt: "released", SourcePID: 42, SourceStopped: true,
		Files: files, Filesystem: &sysboxcapture.FilesystemSnapshot{}}
	put := func(path string, value any) {
		t.Helper()
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := engine.root.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	put(generation+"/checkpoint.json", saved)
	raw, _ := json.Marshal(saved)
	record.Pause.CheckpointSHA256 = sum(raw)
	saved.ReleasedAt = ""
	put(generation+"/capture-result.json", saved)
	put(generation+"/checkpoint-intent.json", checkpointIntent{
		Version: 1, ID: saved.ID, SourceID: saved.SourceID, Owner: saved.Owner,
		RuntimeID: saved.RuntimeID, BootID: saved.BootID, ConfigSHA256: saved.ConfigSHA256,
		RuntimeSHA256: saved.RuntimeSHA256, CriuSHA256: saved.CriuSHA256,
		ManagerRoot: saved.ManagerRoot, RequestedAt: record.Pause.RequestedAt, StopSource: true,
	})
	put(relative+"/resume-intent.json", resumeIntent{Version: 1, Request: engine.resumeRequest(record),
		SnapshotSHA: record.Pause.CheckpointSHA256, CriuSHA256: engine.config.CriuSHA256,
		RuntimeSHA256: engine.config.RuntimeSHA256})
	put(relative+"/resume-prepared.json", files)
	admission, err := engine.failedResumeDeleteEvidence(context.Background(), record)
	if err != nil {
		t.Fatal(err)
	}
	admission.AdmittedAt = "admitted-before-delete"
	put(relative+"/resume-delete-admission.json", admission)
	record.Phase, record.Delete = "deleted", &Operation{
		RequestedAt: "delete-requested", Attempted: true, CompletedAt: "deleted"}
	return engine, record, relative
}

func TestFailedResumeDeleteRequiresOriginalTerminalFailure(t *testing.T) {
	_, deleted, _ := failedResumeDeleteFixture(t)
	original := deleted
	original.Phase, original.Delete = "resuming", nil
	if err := failedResumeDeleteRecord(original); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*Record){
		"running":          func(r *Record) { r.Phase = "running" },
		"unattempted":      func(r *Record) { r.Pause.Resume.Attempted = false },
		"unknown-outcome":  func(r *Record) { r.Pause.Resume.Error = "" },
		"completed":        func(r *Record) { r.Pause.Resume.CompletedAt = "completed" },
		"missing-resume":   func(r *Record) { r.Pause.Resume = nil },
		"incomplete-pause": func(r *Record) { r.Pause.CompletedAt = "" },
		"rejected-pause":   func(r *Record) { r.Pause.RejectedAt = "rejected" },
		"foreign-pause":    func(r *Record) { r.Pause.CheckpointSHA256 = "unconfirmed" },
		"incomplete-start": func(r *Record) { r.Start.CompletedAt = "" },
		"failed-start":     func(r *Record) { r.Start.Error = "failed" },
		"unknown-runtime":  func(r *Record) { r.RuntimeCreated = "" },
		"prior-delete":     func(r *Record) { r.Delete = &Operation{RequestedAt: "prior"} },
	} {
		t.Run(name, func(t *testing.T) {
			data, _ := json.Marshal(original)
			var changed Record
			if err := json.Unmarshal(data, &changed); err != nil {
				t.Fatal(err)
			}
			change(&changed)
			if err := failedResumeDeleteRecord(changed); err == nil {
				t.Fatal("unfinished or changed Resume gained destructive authority")
			}
		})
	}
}

func TestDeletedFailedResumePreservesOriginalInputs(t *testing.T) {
	engine, record, relative := failedResumeDeleteFixture(t)
	before, err := engine.checkpointFiles(context.Background(), relative)
	if err != nil {
		t.Fatal(err)
	}
	matched, err := engine.deletedFailedResume(context.Background(), record, *record.Pause)
	if err != nil || !matched {
		t.Fatal("exact confirmed Delete lost its failed Resume admission", err)
	}
	after, err := engine.checkpointFiles(context.Background(), relative)
	if err != nil || !checkpointPreparationIntact(before, after) ||
		record.Pause.Resume.Error != "original failed restore" ||
		!record.Pause.Resume.Attempted || record.Pause.Resume.CompletedAt != "" {
		t.Fatal("inspection changed failed Resume authority or original inputs", err)
	}
	inventory := epochInventoryBuilder{engine: engine, ctx: context.Background(), allowed: map[string]uint32{}}
	if err := inventory.resumes(record); err != nil {
		t.Fatal("confirmed failed Resume prevents later epoch cleanup", err)
	}
	if len(inventory.payloads) != 1 || inventory.payloads[0] != relative+"/images" ||
		inventory.allowed[relative+"/resume-delete-admission.json"] == 0 ||
		inventory.allowed[relative+"/resume-held.json"] != 0 ||
		inventory.allowed[relative+"/resume-result.json"] != 0 {
		t.Fatal("epoch cleanup lost admission or invented successful restore receipts")
	}
	// Capture retirement is a separate owner. Its saved manifest and proof
	// remain; the Resume inventory must not require or authorize its payload.
	generation, _ := checkpointPath(record.ID, record.Pause.SnapshotID)
	if err := engine.root.RemoveAll(generation + "/images"); err != nil {
		t.Fatal(err)
	}
	inventory = epochInventoryBuilder{engine: engine, ctx: context.Background(), allowed: map[string]uint32{}}
	if err := inventory.resumes(record); err != nil {
		t.Fatal("separate capture retirement stranded confirmed failed Resume metadata", err)
	}
}

func TestDeletedFailedResumeRejectsChangedOrUnfinishedEvidence(t *testing.T) {
	for _, scenario := range []string{
		"missing-admission", "changed-admission", "changed-record", "foreign-criu",
		"unknown-outcome", "unconfirmed-delete", "failed-delete", "changed-pause",
		"changed-intent", "changed-prepared", "changed-image", "held", "completed",
	} {
		t.Run(scenario, func(t *testing.T) {
			engine, record, relative := failedResumeDeleteFixture(t)
			path, content := "", []byte("{}")
			switch scenario {
			case "missing-admission":
				if err := engine.root.Remove(relative + "/resume-delete-admission.json"); err != nil {
					t.Fatal(err)
				}
			case "changed-admission":
				path = relative + "/resume-delete-admission.json"
			case "changed-record":
				record.Owner = strings.Repeat("f", 32)
			case "foreign-criu":
				engine.config.CriuSHA256 = strings.Repeat("f", 64)
			case "unknown-outcome":
				record.Pause.Resume.Error = ""
			case "unconfirmed-delete":
				record.Delete.CompletedAt = ""
			case "failed-delete":
				record.Delete.Error = "unconfirmed"
			case "changed-pause":
				record.Pause.CheckpointSHA256 = strings.Repeat("f", 64)
			case "changed-intent":
				path = relative + "/resume-intent.json"
			case "changed-prepared":
				path = relative + "/resume-prepared.json"
			case "changed-image":
				path, content = relative+"/images/pages-1.img", []byte("foreign RAM")
			case "held":
				path = relative + "/resume-held.json"
			case "completed":
				path = relative + "/resume-result.json"
			}
			if path != "" {
				if err := engine.root.WriteFile(path, content, 0600); err != nil {
					t.Fatal(err)
				}
			}
			matched, err := engine.deletedFailedResume(context.Background(), record, *record.Pause)
			if matched && err == nil {
				t.Fatal("changed or incomplete proof was accepted", scenario)
			}
			inventory := epochInventoryBuilder{engine: engine, ctx: context.Background(), allowed: map[string]uint32{}}
			if err := inventory.resumes(record); err == nil {
				t.Fatal("epoch cleanup accepted changed or incomplete failed Resume evidence", scenario)
			}
		})
	}
}
