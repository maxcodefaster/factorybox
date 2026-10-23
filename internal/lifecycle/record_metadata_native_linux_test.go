//go:build linux && factorybox_worker_probe

package lifecycle

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

// Fill only the owned fixture record with representable historical rejections.
// The history is injected metadata, not a claim that these events occurred.
// RAM, files, checkpoint bytes and the actual committed pause are unchanged.
func TestNativeRecordMetadataResumeAdmission(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_RECORD_METADATA") != "1" {
		t.Skip("owned delivered record-metadata fixture only")
	}
	TestNativeBoundedLossPrepare(t)
	status := boundedLossStatusNow(t)
	path := status.Lifecycle.Root + "/objects/source/record.json"
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var record Record
	if err := json.Unmarshal(before, &record); err != nil || record.Phase != "paused" || record.Pause == nil {
		t.Fatal("requires an actual committed stopped pause", err)
	}
	var pause boundedLossPause
	raw, err := os.ReadFile("/output/bounded-loss-pause.json")
	if err != nil || json.Unmarshal(raw, &pause) != nil {
		t.Fatal("read committed actor receipt", err)
	}
	entry := PauseOperation{SnapshotID: strings.Repeat("f", 32), RequestedAt: record.Pause.RequestedAt,
		RejectedAt: record.Pause.CompletedAt, SourcePID: record.Pause.SourcePID,
		Error: strings.Repeat("historical rejected capture; ", 16)}
	encode := func() []byte {
		t.Helper()
		data, err := json.MarshalIndent(record, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		return append(data, '\n')
	}
	record.PauseHistory = append(record.PauseHistory, entry)
	one := len(encode())
	record.PauseHistory = append(record.PauseHistory, entry)
	step := len(encode()) - one
	const target = (1 << 20) - 150
	count := 1 + (target-one)/step
	record.PauseHistory = make([]PauseOperation, count)
	for index := range record.PauseHistory {
		record.PauseHistory[index] = entry
		record.PauseHistory[index].SnapshotID = fmt.Sprintf("%032x", index+64)
	}
	data := encode()
	if len(data) > target || len(data) < target-step {
		t.Fatal("history sizing fixture differs", len(data), target, step)
	}
	record.PauseHistory[len(record.PauseHistory)-1].Error += strings.Repeat("x", target-len(data))
	data = encode()
	if len(data) != target {
		t.Fatal("fixture does not reach the real reader boundary", len(data))
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	original := boundedLossView(t, "state", "source")
	if original.Record.Phase != "paused" || original.Runtime == nil || original.Runtime.Status != "stopped" {
		t.Fatal("injected history changed the stopped runtime", original.Runtime)
	}
	_, resumeErr := boundedLossCommand("--worker-run-root", "/run/factorybox", "resume", "--snapshot-id", record.Pause.SnapshotID, "source")
	if resumeErr == nil || !strings.Contains(resumeErr.Error(), "new sandbox metadata refused") {
		t.Fatal("resume did not refuse before consuming its next pause metadata allowance", resumeErr)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, data) {
		t.Fatal("metadata refusal changed the committed pause record", err)
	}
	unchanged := boundedLossView(t, "state", "source")
	if unchanged.Record.Phase != "paused" || unchanged.Runtime == nil || unchanged.Runtime.Status != "stopped" ||
		unchanged.Runtime.Created != original.Runtime.Created {
		t.Fatal("metadata refusal exposed a running or different runtime", unchanged.Runtime)
	}
	if _, err := os.Lstat(status.Lifecycle.Root + "/objects/source/resumes"); !os.IsNotExist(err) {
		t.Fatal("metadata refusal created resume backing", err)
	}
	child := boundedLossView(t, "fork", "--source", "source", "--snapshot-id", record.Pause.SnapshotID, "metadata-child")
	boundedLossActor(t, child, pause.State)
	boundedLossView(t, "delete", "metadata-child")
	after, err = os.ReadFile(path)
	if err != nil || !bytes.Equal(after, data) {
		t.Fatal("independent fork changed the protected source record", err)
	}
	proof, err := json.MarshalIndent(map[string]any{
		"passed": true, "injected_history": true, "record_bytes": len(data), "history_entries": len(record.PauseHistory),
		"resume_refused_before_transition": true, "committed_pause_record_unchanged": true,
		"source_still_stopped": true, "no_resume_backing": true, "prior_checkpoint_independent": true,
		"record_sha256": sum(data), "private_ram_sha256": sum(pause.State.Payload),
	}, "", "  ")
	if err != nil || os.WriteFile("/output/record-metadata.json", append(proof, '\n'), 0644) != nil {
		t.Fatal("save record-metadata proof", err)
	}
}
