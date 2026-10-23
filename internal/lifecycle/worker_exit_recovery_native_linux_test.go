//go:build linux

package lifecycle

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Exercise the actual locked, root-owned reader used by RecoverPaused. A
// well-formed intent permits only its selected read, including interrupted
// drain. It never reopens ordinary lifecycle operations on the sealed epoch.
func TestNativeCheckpointRetirementWorkerExitRecoveryFence(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_RETIREMENT") != "1" {
		t.Skip("owned delivered retirement fixture only")
	}
	for _, fault := range []string{"unsealed", "interrupted", "complete", "orphan-complete", "intent-mode", "intent-link",
		"intent-symlink", "intent-unknown", "intent-trailing", "completion-changed", "completion-unfinished",
		"record-changed", "record-mode", "operation-held"} {
		t.Run(fault, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Chmod(root, 0700); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"objects", "objects/source", "runtime", "forks", "bases"} {
				if err := os.Mkdir(root+"/"+name, 0700); err != nil {
					t.Fatal(err)
				}
			}
			config := Config{Root: root, RuntimeSHA256: strings.Repeat("a", 64),
				CriuSHA256: strings.Repeat("b", 64), ManagerRoot: root + "/mgr"}
			source := Record{Version: 1, ID: "source", Owner: strings.Repeat("c", 32),
				RuntimeID: "fbx-" + strings.Repeat("c", 32), RuntimeSHA256: config.RuntimeSHA256,
				ConfigSHA256: sum([]byte("{}")), BootID: "boot", Phase: "paused", RuntimeCreated: "created",
				Start: Operation{CompletedAt: "started"}, Pause: &PauseOperation{SnapshotID: strings.Repeat("d", 32),
					CheckpointSHA256: strings.Repeat("e", 64), RequestedAt: "requested", CompletedAt: "complete", SourcePID: 123}}
			request := RecoveryRequest{SourceRoot: root, SourceID: source.ID, SourceOwner: source.Owner,
				SnapshotID: source.Pause.SnapshotID, CheckpointSHA256: source.Pause.CheckpointSHA256}
			intent := workerBaseRelease{Version: 1, Root: root, BootID: "boot", RuntimeSHA256: config.RuntimeSHA256,
				CriuSHA256: config.CriuSHA256, ManagerRoot: config.ManagerRoot, RequestedAt: "exit-request", Paused: []Record{source}}
			write := func(name string, value any) {
				t.Helper()
				data, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(root+"/"+name, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			write("objects/source/record.json", source)
			if err := os.WriteFile(root+"/objects/source/config.json", []byte("{}"), 0600); err != nil {
				t.Fatal(err)
			}
			if fault != "unsealed" && fault != "orphan-complete" {
				write("worker-exit.json", intent)
			}
			complete := intent
			complete.CompletedAt = "exit-complete"
			switch fault {
			case "complete", "orphan-complete":
				write("worker-exit-complete.json", complete)
			case "intent-mode":
				if err := os.Chmod(root+"/worker-exit.json", 0644); err != nil {
					t.Fatal(err)
				}
			case "intent-link":
				if err := os.Link(root+"/worker-exit.json", root+"/second-link"); err != nil {
					t.Fatal(err)
				}
			case "intent-symlink":
				if err := os.Rename(root+"/worker-exit.json", root+"/moved-intent"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(root, "moved-intent"), root+"/worker-exit.json"); err != nil {
					t.Fatal(err)
				}
			case "intent-unknown", "intent-trailing":
				data, err := os.ReadFile(root + "/worker-exit.json")
				if err != nil {
					t.Fatal(err)
				}
				if fault == "intent-unknown" {
					data = append(append(data[:len(data)-1], []byte(",\"unrecognized\":true")...), '}')
				} else {
					data = append(data, []byte("{}")...)
				}
				if err := os.WriteFile(root+"/worker-exit.json", data, 0600); err != nil {
					t.Fatal(err)
				}
			case "completion-changed":
				complete.RequestedAt += "-changed"
				write("worker-exit-complete.json", complete)
			case "completion-unfinished":
				write("worker-exit-complete.json", intent)
			case "record-changed":
				source.InputSHA256 = strings.Repeat("f", 64)
				write("objects/source/record.json", source)
			case "record-mode":
				if err := os.Chmod(root+"/objects/source/record.json", 0644); err != nil {
					t.Fatal(err)
				}
			}
			opened, err := os.OpenRoot(root)
			if err != nil {
				t.Fatal(err)
			}
			defer opened.Close()
			engine := &Engine{root: opened, config: config, boot: "boot"}
			if fault == "operation-held" {
				held, err := engine.lockRoot()
				if err != nil {
					t.Fatal(err)
				}
				defer held.Close()
			}
			lock, err := engine.recoveryLock(request)
			accepted := fault == "unsealed" || fault == "interrupted" || fault == "complete"
			if lock != nil {
				lock.Close()
			}
			if accepted && err != nil {
				t.Fatal("confirmed read was refused", err)
			}
			if !accepted && err == nil {
				t.Fatal("unconfirmed recovery fence was accepted")
			}
			if !accepted {
				expected := map[string]string{
					"orphan-complete":       "completion lacks its original intent",
					"intent-mode":           "checkpoint record has invalid file identity or size",
					"intent-link":           "checkpoint record has invalid file identity or size",
					"intent-symlink":        "too many levels of symbolic links",
					"intent-unknown":        "unknown field",
					"intent-trailing":       "checkpoint manifest contains trailing data",
					"completion-changed":    "completion differs from its fenced intent",
					"completion-unfinished": "completion is incomplete",
					"record-changed":        "recovery differs from the exact pause",
					"record-mode":           "checkpoint record has invalid file identity or size",
					"operation-held":        "resource temporarily unavailable",
				}[fault]
				if expected == "" || !strings.Contains(err.Error(), expected) {
					t.Fatalf("refused at the wrong boundary: %v; expected %q", err, expected)
				}
			}
			if fault != "unsealed" {
				lock, err := engine.lock()
				if lock != nil {
					lock.Close()
				}
				if err == nil {
					t.Fatal("sealed epoch admitted a lifecycle operation")
				}
			}
		})
	}
}
