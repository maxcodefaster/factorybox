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
