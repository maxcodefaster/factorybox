//go:build linux

package lifecycle

import (
	"os"
	"strings"
	"testing"
)

// Process/network cure must not turn a missing, foreign or partial timer
// completion into permission to thaw. Kernel anchors are checked by callers.
func TestTimerActivationReceiptBeforeCureRelease(t *testing.T) {
	for _, scenario := range []string{"unmodified", "prepared", "partial-activation", "foreign", "failed", "thawed", "traced", "bad-code", "complete", "symlink"} {
		t.Run(scenario, func(t *testing.T) {
			directory, err := os.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer directory.Close()
			config := captureGuardConfig{Token: strings.Repeat("a", 32)}
			anchor := captureGuardAnchors{evidence: directory}
			if scenario == "prepared" || scenario == "complete" {
				if err := captureGuardWrite(directory, "timer-prepare-intent.json", map[string]any{"token": config.Token}); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "partial-activation" {
				if err := captureGuardWrite(directory, "timer-activation-intent.json", map[string]any{"token": config.Token}); err != nil {
					t.Fatal(err)
				}
			}
			if scenario != "unmodified" && scenario != "prepared" && scenario != "partial-activation" {
				receipt := map[string]any{"version": 1, "token": config.Token, "passed": true, "source_frozen": true, "tracers_detached": true, "code_sha256": strings.Repeat("b", 64)}
				switch scenario {
				case "foreign":
					receipt["token"] = strings.Repeat("c", 32)
				case "failed":
					receipt["passed"] = false
				case "thawed":
					receipt["source_frozen"] = false
				case "traced":
					receipt["tracers_detached"] = false
				case "bad-code":
					receipt["code_sha256"] = "wrong"
				}
				name := "timer-activation-complete.json"
				if scenario == "symlink" {
					name = "foreign-complete.json"
				}
				if err := captureGuardWrite(directory, name, receipt); err != nil {
					t.Fatal(err)
				}
				if scenario == "symlink" {
					if err := os.Symlink(directory.Name()+"/"+name, directory.Name()+"/timer-activation-complete.json"); err != nil {
						t.Fatal(err)
					}
				}
			}
			err = captureGuardTimersSafe(config, anchor)
			wantSafe := scenario == "unmodified" || scenario == "complete"
			if (err == nil) != wantSafe {
				t.Fatalf("unsafe timer release: scenario=%s err=%v", scenario, err)
			}
		})
	}
}
