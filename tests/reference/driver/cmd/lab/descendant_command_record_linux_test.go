package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A Pod can disappear before the post-run tar transfer. The streamed failure
// must still contain its actual cause after every ephemeral file is removed.
func TestDescendantCommandFailureSurvivesEvidenceLoss(t *testing.T) {
	for _, test := range []struct {
		name    string
		padding int
		lost    bool
	}{{"short", 0, false}, {"bounded", 12000, false}, {"already_lost", 0, true}} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			previousRoot := descendantEvidenceRoot
			descendantEvidenceRoot = directory
			defer func() { descendantEvidenceRoot = previousRoot }()
			if test.lost {
				if err := os.RemoveAll(directory); err != nil {
					t.Fatal(err)
				}
			}
			payload := strings.Repeat("x", test.padding) + "native-original-writer-oracle: socket peer retired\n"
			out, commandErr := criuCommand(5*time.Second, "/bin/sh", "-c", "printf '%s' \"$1\"; exit 7", "fixture", payload)
			if commandErr == nil {
				t.Fatal("native failure fixture unexpectedly succeeded")
			}
			reader, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			previousStdout := os.Stdout
			os.Stdout = writer
			recordErr := descendantCommandRecord("native-output-loss", time.Now(), out, commandErr)
			os.Stdout = previousStdout
			writer.Close()
			streamed, err := io.ReadAll(reader)
			reader.Close()
			if err != nil || recordErr == nil {
				t.Fatalf("failure stream fixture: %v %v", err, recordErr)
			}
			saved, err := os.ReadFile(filepath.Join(directory, "native-output-loss.log"))
			if test.lost && !os.IsNotExist(err) {
				t.Fatalf("lost evidence directory was unexpectedly restored: %v", err)
			}
			if !test.lost && (err != nil || string(saved) != payload) {
				t.Fatalf("complete output was not preserved before loss: %v", err)
			}
			if err := os.RemoveAll(directory); err != nil {
				t.Fatal(err)
			}
			var row result
			if err := json.Unmarshal(streamed, &row); err != nil {
				t.Fatal(err)
			}
			line := strings.SplitN(row.Output, "\n", 2)[0]
			var evidence struct {
				Bytes     int    `json:"stdout_bytes"`
				Saved     bool   `json:"output_saved"`
				Tail      string `json:"output_tail"`
				Truncated bool   `json:"output_tail_truncated"`
				Cause     string `json:"command_error"`
			}
			if err := json.Unmarshal([]byte(line), &evidence); err != nil {
				t.Fatal(err)
			}
			if row.Status != "failed" || evidence.Bytes != len(payload) || evidence.Saved == test.lost ||
				!strings.Contains(evidence.Tail, "native-original-writer-oracle: socket peer retired") ||
				!strings.Contains(evidence.Cause, "exit status 7") {
				t.Fatalf("actual failure cause disappeared with the evidence volume: %+v", evidence)
			}
			if len(evidence.Tail) > 4096 || evidence.Truncated != (test.padding > 0) {
				t.Fatalf("failure stream is not bounded: %d %t", len(evidence.Tail), evidence.Truncated)
			}
		})
	}
}

func TestDescendantOutcomeCauseSurvivesEvidenceLoss(t *testing.T) {
	directory := t.TempDir()
	proof := filepath.Join(directory, "proof.json")
	if err := os.RemoveAll(directory); err != nil {
		t.Fatal(err)
	}
	_, missing := os.Open(proof)
	if !os.IsNotExist(missing) {
		t.Fatal("fixture did not lose its evidence")
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = writer
	descendantResultRecord("native-full-outcome", time.Now(), map[string]any{"proof": proof, "passed": false},
		fmt.Errorf("actual-post-restore-outcome: %w; %s", missing, strings.Repeat("x", 6000)))
	os.Stdout = previous
	writer.Close()
	data, err := io.ReadAll(reader)
	reader.Close()
	if err != nil {
		t.Fatal(err)
	}
	var row result
	if err := json.Unmarshal(data, &row); err != nil {
		t.Fatal(err)
	}
	var summary struct {
		Error     string `json:"error"`
		Bytes     int    `json:"error_bytes"`
		Truncated bool   `json:"error_truncated"`
	}
	if err := json.Unmarshal([]byte(strings.SplitN(row.Output, "\n", 2)[0]), &summary); err != nil {
		t.Fatal(err)
	}
	if row.Status != "failed" || !strings.Contains(summary.Error, "actual-post-restore-outcome") ||
		!strings.Contains(summary.Error, "no such file or directory") || !summary.Truncated ||
		len(summary.Error) != 4096 || summary.Bytes <= len(summary.Error) {
		t.Fatalf("actual outcome cause disappeared or became unbounded: %+v", summary)
	}
}

func TestDescendantOutcomeTimingsSurviveEvidenceLoss(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "proof.json")
	proof := map[string]any{
		"complete_correctness_readiness_us": int64(12_345_678),
		"complete_resume_readiness_us":      int64(3_456_789),
		"coalesced_snapshot_hashes":         true,
		"phase_us":                          map[string]int64{"capture": 1_000_000, "restore": 2_000_000},
		"source_opencode_history":           strings.Repeat("unrelated-runtime-history", 10000),
	}
	if err := os.WriteFile(path, []byte(asJSON(proof)), 0600); err != nil {
		t.Fatal(err)
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = writer
	descendantResultRecord("native-timing-outcome", time.Now(), map[string]any{
		"proof": path, "passed": true, "timing": descendantOutcomeTiming(proof),
	}, nil)
	os.Stdout = previous
	writer.Close()
	if err := os.RemoveAll(directory); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	reader.Close()
	if err != nil || len(data) > 2048 || strings.Contains(string(data), "unrelated-runtime-history") {
		t.Fatal("timing outcome was not compact and isolated", len(data), err)
	}
	var row result
	if err := json.Unmarshal(data, &row); err != nil {
		t.Fatal(err)
	}
	var evidence struct {
		Timing struct {
			Readiness            int64            `json:"complete_correctness_readiness_us"`
			Resume               int64            `json:"complete_resume_readiness_us"`
			Diagnostic           bool             `json:"diagnostic_only"`
			RequiresVerification bool             `json:"requires_complete_lifecycle_verification"`
			Phases               map[string]int64 `json:"phase_us"`
		} `json:"timing"`
	}
	if err := json.Unmarshal([]byte(row.Output), &evidence); err != nil {
		t.Fatal(err)
	}
	if evidence.Timing.Readiness != 12_345_678 || evidence.Timing.Resume != 3_456_789 ||
		!evidence.Timing.Diagnostic || !evidence.Timing.RequiresVerification ||
		evidence.Timing.Phases["restore"] != 2_000_000 {
		t.Fatal("actual readiness/phase observations disappeared with the proof volume", row.Output)
	}
}
