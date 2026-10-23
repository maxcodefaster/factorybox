//go:build linux

package lifecycle

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func recordMetadataEngine(t *testing.T) (*Engine, string) {
	t.Helper()
	path := t.TempDir()
	if err := os.MkdirAll(filepath.Join(path, "objects/source"), 0700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	return &Engine{root: root, config: Config{Root: path}}, filepath.Join(path, "objects/source/record.json")
}

func TestRecordMetadataWriteKeepsLastReadableHistory(t *testing.T) {
	engine, path := recordMetadataEngine(t)
	record := Record{Version: 1, ID: "source", Phase: "running"}
	var last Record
	for number := 0; ; number++ {
		encoded, err := json.MarshalIndent(record, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if len(encoded)+1 > 1<<20 {
			break
		}
		last = record
		record.PauseHistory = append(append([]PauseOperation(nil), record.PauseHistory...), PauseOperation{
			SnapshotID: fmt.Sprintf("%032x", number+1), RequestedAt: "2026-10-23T00:00:00Z",
			RejectedAt: "2026-10-23T00:00:01Z", Error: strings.Repeat("old admission failure ", 128),
		})
	}
	if err := engine.write(last); err != nil {
		t.Fatal("last readable history must remain publishable", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Record
	if err := json.NewDecoder(io.LimitReader(bytes.NewReader(before), 1<<20)).Decode(&decoded); err != nil {
		t.Fatal("baseline does not fit the production record reader", err)
	}
	if err := engine.write(record); err == nil {
		t.Fatalf("published unreadable pause history over the 1-MiB reader bound: %d entries", len(record.PauseHistory))
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("refused metadata update replaced the last readable generation", err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 || entries[0].Name() != "record.json" {
		t.Fatal("refused metadata update left temporary backing", entries, err)
	}
}

func TestRecordMetadataWriteCountsFinalNewline(t *testing.T) {
	engine, path := recordMetadataEngine(t)
	record := Record{Version: 1, ID: "source", Start: Operation{Error: "x"}}
	encoded, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	record.Start.Error += strings.Repeat("x", (1<<20)-len(encoded)-1)
	if err := engine.write(record); err != nil {
		t.Fatal("exact readable boundary refused", err)
	}
	before, err := os.ReadFile(path)
	if err != nil || len(before) != 1<<20 || before[len(before)-1] != '\n' {
		t.Fatal("exact boundary fixture is invalid", len(before), err)
	}
	record.Start.Error += "x"
	if err := engine.write(record); err == nil {
		t.Fatal("record publisher ignored the final newline in its size bound")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("one-byte overflow replaced the exact boundary generation", err)
	}
}

func TestRecordMetadataAdmissionPreservesInputAndExistingEscapedErrors(t *testing.T) {
	record := Record{Version: 1, ID: "source", Phase: "paused",
		Pause: &PauseOperation{SnapshotID: strings.Repeat("a", 32), CompletedAt: "2026-10-23T00:00:00Z",
			Resume: &Operation{RequestedAt: "2026-10-23T00:00:01Z"}}}
	engine := &Engine{config: Config{Root: "/owned/controller", Criu: "/owned/criu", ManagerRoot: "/owned/manager"}}
	before, err := json.Marshal(record)
	if err != nil || engine.admitSandboxMetadata(record, true) != nil {
		t.Fatal("ordinary record should reserve its next pause", err)
	}
	after, err := json.Marshal(record)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("prospective bound mutated the actual pause or its resume", err)
	}
	// Existing records are never shortened to make the bound pass. JSON escapes
	// make the stored size much larger than this error's raw byte length.
	record.Start.Error = strings.Repeat("\x00", (1<<20)/6-10000)
	encoded, err := json.MarshalIndent(record, "", "  ")
	if err != nil || len(encoded)+1 >= 1<<20 {
		t.Fatal("existing escaped-error fixture must still be readable", len(encoded), err)
	}
	if err := engine.admitSandboxMetadata(record, false); err == nil {
		t.Fatal("prospective bound discounted an existing escaped error")
	}
}

func TestRecordMetadataAdmissionHonorsSmallerGuardPacket(t *testing.T) {
	engine := &Engine{config: Config{Root: "/owned/controller", Criu: "/owned/criu", ManagerRoot: "/owned/manager"}}
	record := Record{Version: 1, ID: "source", Phase: "running", PauseHistory: []PauseOperation{
		{SnapshotID: strings.Repeat("a", 32), Error: strings.Repeat("x", captureGuardPacketLimit)},
	}}
	if _, err := sandboxRecordBytes(record); err != nil {
		t.Fatal("guard fixture should fit the durable record reader", err)
	}
	if err := engine.admitSandboxMetadata(record, false); err == nil || !strings.Contains(err.Error(), "guard packet") {
		t.Fatal("guard packet bound was not checked before another pause", err)
	}
}

func TestRecordErrorSummaryKeepsFullIdentityWithinItsBound(t *testing.T) {
	for _, original := range []string{"", "ordinary error", strings.Repeat("x", sandboxErrorLimit)} {
		if got := sandboxErrorSummary(original); got != original {
			t.Fatal("ordinary error changed", len(original))
		}
	}
	for _, original := range []string{
		strings.Repeat("x", sandboxErrorLimit+1), strings.Repeat("\x00\n\"", sandboxErrorLimit),
		strings.Repeat("Größe ", sandboxErrorLimit),
	} {
		got := sandboxErrorSummary(original)
		if len(got) > sandboxErrorLimit || !strings.HasPrefix(original, got[:32]) ||
			!strings.Contains(got, sum([]byte(original))) || !strings.Contains(got, fmt.Sprint(len(original))+" bytes") {
			t.Fatal("stored error lost its bound or full original identity", len(got), len(original))
		}
	}
}
