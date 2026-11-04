//go:build linux

package lifecycle

import (
	"context"
	"encoding/json"
	"testing"

	"factorybox/internal/sysboxcapture"
)

func unattemptedResumeFixture(t *testing.T) (*Engine, Record, string) {
	t.Helper()
	engine, record, relative := failedResumeDeleteFixture(t)
	record.Phase, record.Delete = "resuming", nil
	record.Pause.Resume.Attempted, record.Pause.Resume.Error = false, ""
	return engine, record, relative
}

func TestUnattemptedResumeDeleteNeedsTheCompleteOriginalPause(t *testing.T) {
	engine, record, _ := unattemptedResumeFixture(t)
	if err := engine.unattemptedResumeDeleteEvidence(context.Background(), record); err != nil {
		t.Fatalf("an unattempted Resume of a committed Pause must be removable: %v", err)
	}
	for name, change := range map[string]func(*Record){
		"attempted":       func(r *Record) { r.Pause.Resume.Attempted = true },
		"restore-error":   func(r *Record) { r.Pause.Resume.Error = "unconfirmed" },
		"completed":       func(r *Record) { r.Pause.Resume.CompletedAt = "completed" },
		"unknown-runtime": func(r *Record) { r.RuntimeCreated = "" },
		"foreign-pause": func(r *Record) {
			r.Pause.CheckpointSHA256 = "0000000000000000000000000000000000000000000000000000000000000000"
		},
		"rejected-pause":   func(r *Record) { r.Pause.RejectedAt = "rejected" },
		"unfinished-pause": func(r *Record) { r.Pause.CompletedAt = "" },
		"missing-resume":   func(r *Record) { r.Pause.Resume = nil },
		"prior-delete":     func(r *Record) { r.Delete = &Operation{RequestedAt: "prior"} },
		"running":          func(r *Record) { r.Phase = "running" },
		"changed-shared":   func(r *Record) { r.SharedSource = &sysboxcapture.SharedSource{} },
	} {
		t.Run(name, func(t *testing.T) {
			data, _ := json.Marshal(record)
			var changed Record
			if err := json.Unmarshal(data, &changed); err != nil {
				t.Fatal(err)
			}
			change(&changed)
			if err := engine.unattemptedResumeDeleteEvidence(context.Background(), changed); err == nil {
				t.Fatal("uncertain or changed Resume gained destructive authority")
			}
		})
	}
}

func TestUnattemptedResumeDeleteRefusesAHeldOrCompletedRestore(t *testing.T) {
	for _, name := range []string{"resume-held.json", "resume-result.json"} {
		t.Run(name, func(t *testing.T) {
			engine, record, relative := unattemptedResumeFixture(t)
			if err := engine.root.WriteFile(relative+"/"+name, []byte("{}"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := engine.unattemptedResumeDeleteEvidence(context.Background(), record); err == nil {
				t.Fatal("a held or completed restore was discarded")
			}
		})
	}
}
