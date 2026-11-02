//go:build linux

package lifecycle

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func resumeRetirementFixture() (Record, CheckpointRequest) {
	request := CheckpointRequest{"source", strings.Repeat("a", 32), strings.Repeat("b", 32), strings.Repeat("c", 64)}
	previous := PauseOperation{SnapshotID: request.SnapshotID, CheckpointSHA256: request.CheckpointSHA256,
		CompletedAt: "original-pause", Resume: &Operation{Attempted: true, CompletedAt: "original-resume"}}
	record := Record{ID: request.SourceID, Owner: request.SourceOwner, Phase: "paused",
		PauseHistory: []PauseOperation{previous}, Pause: &PauseOperation{
			SnapshotID: strings.Repeat("d", 32), CheckpointSHA256: strings.Repeat("e", 64), CompletedAt: "later-pause",
		}}
	return record, request
}

func TestResumeImageRetirementEligibility(t *testing.T) {
	record, request := resumeRetirementFixture()
	before, _ := json.Marshal(record)
	if selected, err := resumeRetirementPause(record, request); err != nil || selected.SnapshotID != request.SnapshotID {
		t.Fatal("later complete Pause did not make obsolete inputs eligible", selected, err)
	}
	after, _ := json.Marshal(record)
	if !bytes.Equal(before, after) {
		t.Fatal("eligibility changed the original record")
	}
	record.Phase = "deleted"
	record.Pause = &record.PauseHistory[0]
	record.PauseHistory = nil
	record.Delete = &Operation{Attempted: true, CompletedAt: "deleted"}
	if _, err := resumeRetirementPause(record, request); err != nil {
		t.Fatal("confirmed Delete did not make the last Resume eligible", err)
	}
}

func TestResumeImageRetirementRefusesActiveUncertainAndChangedOwnership(t *testing.T) {
	cases := map[string]func(*Record, *CheckpointRequest){
		"owner":   func(record *Record, _ *CheckpointRequest) { record.Owner = strings.Repeat("f", 32) },
		"sandbox": func(record *Record, _ *CheckpointRequest) { record.ID = "other" },
		"hash":    func(_ *Record, request *CheckpointRequest) { request.CheckpointSHA256 = strings.Repeat("f", 64) },
		"active": func(record *Record, _ *CheckpointRequest) {
			record.Pause = &record.PauseHistory[0]
			record.PauseHistory = nil
			record.Phase = "running"
		},
		"later-pause-incomplete": func(record *Record, _ *CheckpointRequest) { record.Pause.CompletedAt = "" },
		"later-pause-hash":       func(record *Record, _ *CheckpointRequest) { record.Pause.CheckpointSHA256 = "" },
		"resume-incomplete":      func(record *Record, _ *CheckpointRequest) { record.PauseHistory[0].Resume.CompletedAt = "" },
		"resume-error":           func(record *Record, _ *CheckpointRequest) { record.PauseHistory[0].Resume.Error = "unconfirmed" },
		"resume-unattempted":     func(record *Record, _ *CheckpointRequest) { record.PauseHistory[0].Resume.Attempted = false },
		"pause-unfinished":       func(record *Record, _ *CheckpointRequest) { record.PauseHistory[0].CompletedAt = "" },
		"lifecycle-unresolved":   func(record *Record, _ *CheckpointRequest) { record.Phase = "pausing" },
		"delete-unconfirmed":     func(record *Record, _ *CheckpointRequest) { record.Phase = "deleted" },
		"duplicate-generation": func(record *Record, _ *CheckpointRequest) {
			record.PauseHistory = append(record.PauseHistory, record.PauseHistory[0])
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			record, request := resumeRetirementFixture()
			change(&record, &request)
			if _, err := resumeRetirementPause(record, request); err == nil {
				t.Fatal("unsafe or changed Resume generation was admitted")
			}
		})
	}
}

func TestUnattemptedResumeOnlyInventoriesItsUnchangedPausedInputs(t *testing.T) {
	record := Record{Phase: "resuming", Start: Operation{Attempted: true, CompletedAt: "started"},
		InputBundle: "/private/bundle", Rootfs: "/private/bundle/rootfs", Pause: &PauseOperation{
			SnapshotID: strings.Repeat("a", 32), CheckpointSHA256: strings.Repeat("b", 64),
			RequestedAt: "pause-requested", CompletedAt: "paused",
			Resume: &Operation{RequestedAt: "resume-requested"},
		}}
	if !unattemptedResumeInputs(record) {
		t.Fatal("original unattempted preparation lost its unchanged input inventory")
	}
	for name, change := range map[string]func(*Record){
		"phase":              func(r *Record) { r.Phase = "pausing" },
		"start-unattempted":  func(r *Record) { r.Start.Attempted = false },
		"start-unfinished":   func(r *Record) { r.Start.CompletedAt = "" },
		"delete":             func(r *Record) { r.Delete = &Operation{RequestedAt: "deleting"} },
		"missing-pause":      func(r *Record) { r.Pause = nil },
		"snapshot":           func(r *Record) { r.Pause.SnapshotID = "unknown" },
		"hash":               func(r *Record) { r.Pause.CheckpointSHA256 = "" },
		"unrequested-pause":  func(r *Record) { r.Pause.RequestedAt = "" },
		"unfinished-pause":   func(r *Record) { r.Pause.CompletedAt = "" },
		"rejected-pause":     func(r *Record) { r.Pause.RejectedAt = "rejected" },
		"missing-resume":     func(r *Record) { r.Pause.Resume = nil },
		"unrequested-resume": func(r *Record) { r.Pause.Resume.RequestedAt = "" },
		"attempted-restore":  func(r *Record) { r.Pause.Resume.Attempted = true },
		"completed-restore":  func(r *Record) { r.Pause.Resume.CompletedAt = "completed" },
		"restore-error":      func(r *Record) { r.Pause.Resume.Error = "unconfirmed" },
		"relative-input":     func(r *Record) { r.InputBundle = "relative" },
		"unclean-input":      func(r *Record) { r.InputBundle += "/.." },
		"changed-root":       func(r *Record) { r.Rootfs += "-foreign" },
	} {
		t.Run(name, func(t *testing.T) {
			data, _ := json.Marshal(record)
			var changed Record
			if err := json.Unmarshal(data, &changed); err != nil {
				t.Fatal(err)
			}
			change(&changed)
			if unattemptedResumeInputs(changed) {
				t.Fatal("uncertain or changed input authority was accepted")
			}
		})
	}
}
