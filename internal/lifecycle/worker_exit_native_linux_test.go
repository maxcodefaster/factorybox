//go:build linux

package lifecycle

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Exercise the real generation inventory and strict root-owned JSON reads.
// The positive fixture has a completed internal timer guard, never a checkpoint.
func TestNativeCheckpointRetirementWorkerExitGuard(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_RETIREMENT") != "1" {
		t.Skip("owned delivered retirement fixture only")
	}
	for _, fault := range []string{"", "owner", "token", "root", "record-hash", "checkpoint-mode",
		"outcome-config", "outcome-failed", "in-flight", "activation-missing", "activation-token", "extra-payload", "checkpoint-intent"} {
		t.Run("guard-"+fault, func(t *testing.T) {
			root := t.TempDir()
			id := strings.Repeat("c", 32)
			relative := "objects/source/captures/" + id
			guard := filepath.Join(root, relative, "guard")
			if err := os.MkdirAll(guard, 0700); err != nil {
				t.Fatal(err)
			}
			source := Record{Version: 1, ID: "source", Owner: strings.Repeat("a", 32),
				RuntimeID: "fbx-" + strings.Repeat("a", 32), BootID: "fixture-boot", Phase: "starting",
				RuntimeSHA256: strings.Repeat("b", 64), ConfigSHA256: strings.Repeat("b", 64),
				InputSHA256: strings.Repeat("d", 64), InputBundle: root + "/bundle", Rootfs: root + "/bundle/rootfs",
				Cgroup: &KernelObject{Device: 1, Inode: 2, MountID: 3}}
			data, err := json.MarshalIndent(source, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			config := captureGuardConfig{Version: 1, Source: source, PID: 100, Controller: 200,
				Start: "1", Token: id, Evidence: guard, Root: root, RecordSHA: sum(append(data, '\n'))}
			record := source
			record.Phase = "deleted"
			outcome := map[string]any{"config": config, "reason": "controller_disarmed",
				"status": "source_running_guard_disarmed", "capture_in_flight": false, "passed": true}
			activation := map[string]any{"version": 1, "token": id, "passed": true,
				"source_frozen": true, "tracers_detached": true, "code_sha256": strings.Repeat("e", 64)}
			switch fault {
			case "owner":
				config.Source.Owner = strings.Repeat("f", 32)
			case "token":
				config.Token = strings.Repeat("f", 32)
			case "root":
				config.Root += "/different"
			case "record-hash":
				config.RecordSHA = strings.Repeat("f", 64)
			case "checkpoint-mode":
				config.CriuSHA256 = strings.Repeat("f", 64)
			case "outcome-config":
				outcome["config"] = captureGuardConfig{}
			case "outcome-failed":
				outcome["passed"] = false
			case "in-flight":
				outcome["capture_in_flight"] = true
			case "activation-token":
				activation["token"] = strings.Repeat("f", 32)
			}
			write := func(name string, value any) {
				t.Helper()
				data, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(guard, name), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			write("guard-intent.json", config)
			write("guard-outcome.json", outcome)
			if fault != "activation-missing" {
				write("timer-activation-complete.json", activation)
			}
			if fault == "extra-payload" || fault == "checkpoint-intent" {
				name := "filesystem"
				if fault == "checkpoint-intent" {
					name = "checkpoint-intent.json"
				}
				if err := os.WriteFile(filepath.Join(root, relative, name), []byte("unconfirmed input"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			opened, err := os.OpenRoot(root)
			if err != nil {
				t.Fatal(err)
			}
			defer opened.Close()
			engine := Engine{root: opened, config: Config{Root: root}, boot: source.BootID}
			aliases, err := engine.exitCaptureAliases(context.Background(), record, nil)
			if fault == "" && (err != nil || len(aliases) != 0) {
				t.Fatal("completed internal guard was treated as a checkpoint", aliases, err)
			}
			if fault != "" && err == nil {
				t.Fatal("unconfirmed internal generation was accepted", fault)
			}
		})
	}
}
