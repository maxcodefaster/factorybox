//go:build linux

package lifecycle

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTimerRecoveryCompletionRequiresItsClosedGuard(t *testing.T) {
	for _, scenario := range []string{"missing-guard", "null-proof", "invalid-proof", "foreign-guard"} {
		t.Run(scenario, func(t *testing.T) {
			directory, err := os.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer directory.Close()
			config := captureGuardConfig{Token: strings.Repeat("a", 32)}
			proof := any(map[string]any{"capture_id": strings.Repeat("b", 32)})
			switch scenario {
			case "null-proof":
				proof = nil
			case "invalid-proof":
				proof = "not a recovery proof"
			case "foreign-guard":
				proof = map[string]any{"capture_id": "../foreign"}
			}
			if err := captureGuardWrite(directory, "timer-prepare-intent.json", map[string]any{"token": config.Token}); err != nil {
				t.Fatal(err)
			}
			receipt := map[string]any{"version": 1, "token": config.Token, "passed": true,
				"source_frozen": true, "tracers_detached": true, "code_sha256": strings.Repeat("c", 64), "recovery": proof}
			if err := captureGuardWrite(directory, "timer-activation-complete.json", receipt); err != nil {
				t.Fatal(err)
			}
			if err := captureGuardTimersSafe(config, captureGuardAnchors{evidence: directory}); err == nil {
				t.Fatal("a recovery completion without its exact closed activation guard authorized release")
			}
		})
	}
}

func TestTimerRecoveryUnconfirmedRefusalPreservesReason(t *testing.T) {
	directory, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	config := captureGuardConfig{Token: strings.Repeat("a", 32)}
	if err := captureGuardWrite(directory, "timer-prepare-intent.json", map[string]any{"token": config.Token}); err != nil {
		t.Fatal(err)
	}
	err = recoverCuredCaptureTimers(config, captureGuardAnchors{evidence: directory})
	if err == nil || !strings.Contains(err.Error(), "timer mutation remains unconfirmed") {
		t.Fatalf("ungranted recovery lost the original staged-timer refusal: %v", err)
	}
	for _, name := range []string{"timer-activation-intent.json", "timer-activation-complete.json", "capture-abort-intent.json"} {
		if _, err := os.Stat(filepath.Join(directory.Name(), name)); !os.IsNotExist(err) {
			t.Fatalf("ungranted recovery changed its original held transaction: %s, %v", name, err)
		}
	}
}

func timerRecoveryReceiptFixture(t *testing.T) (captureGuardConfig, captureGuardConfig, captureGuardAnchors, *os.File, timerRecoveryProof) {
	t.Helper()
	rootPath := t.TempDir()
	old, id := strings.Repeat("a", 32), strings.Repeat("b", 32)
	root, err := os.Open(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	config := captureGuardConfig{Version: 1, Source: Record{Version: 1, ID: "source", Owner: strings.Repeat("c", 32),
		RuntimeID: "fbx-" + strings.Repeat("c", 32), RuntimeSHA256: strings.Repeat("d", 64), BootID: "same-boot", Cgroup: &KernelObject{}, Phase: "pausing"},
		PID: 10, Start: "101", Controller: 30, Root: rootPath, Token: old, CriuSHA256: strings.Repeat("e", 64)}
	sourceData, _ := json.MarshalIndent(config.Source, "", "  ")
	config.RecordSHA = sum(append(sourceData, '\n'))
	config.Evidence = filepath.Join(rootPath, "objects/source/captures", old, "guard")
	child := config
	child.Token, child.Controller, child.TimerRecoveryOf = id, 40, old
	child.CriuSHA256 = ""
	child.Evidence = filepath.Join(rootPath, "objects/source/captures", id, "guard")
	for _, path := range []string{config.Evidence, child.Evidence} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	evidence, err := os.Open(config.Evidence)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { evidence.Close() })
	phase, err := os.Open(child.Evidence)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { phase.Close() })
	write := func(file *os.File, name string, value any) {
		t.Helper()
		if err := captureGuardWrite(file, name, value); err != nil {
			t.Fatal(err)
		}
	}
	worker := captureWorkerIntent{Token: old, PID: 1 << 29, Start: "707", SourcePID: config.PID, CriuSHA256: config.CriuSHA256, Scope: "ordinary complete retained-source capture"}
	arm := retainedCaptureArm{Version: 3, Mode: "process_tree_retained", SourcePID: config.PID, SourceStart: config.Start,
		ControllerPID: config.Controller, ControllerStart: "303", GuardPID: child.Controller, GuardStart: "404",
		WorkerPID: worker.PID, WorkerStart: worker.Start, TracerPID: (1 << 29) + 1, TracerStart: "606",
		HelperPID: 50, HelperStart: "505", Nonce: strings.Repeat("f", 64), ActivePID: config.PID,
		ActiveStart: config.Start, ActiveVirtualPID: 1, ExitHoldArmed: true, Tree: []retainedCaptureThread{{PID: 10, Process: 10, Start: config.Start}}}
	write(evidence, "capture-worker-intent.json", worker)
	write(evidence, "criu-retained-arm.json", arm)
	write(evidence, "criu-retained-request.json", retainedCaptureRequest{Version: 1, Token: old, ControllerPID: config.Controller, GuardPID: arm.GuardPID, Nonce: arm.Nonce})
	write(evidence, "criu-retained-result.json", retainedCaptureResult{Version: 3, Passed: true, SourcePID: config.PID, SourceStart: config.Start,
		HelperPID: arm.HelperPID, Threads: 1, Processes: 1, ActivePID: config.PID, Registers: true, SignalMask: true, Held: true, Authorized: true, Nonce: arm.Nonce, ExitStopHeld: true})
	write(evidence, "capture-network-rollback.json", map[string]any{"token": old})
	state := []byte("unchanged original Sysbox state\n")
	if err := os.WriteFile(filepath.Join(filepath.Dir(config.Evidence), "source-state.json"), state, 0600); err != nil {
		t.Fatal(err)
	}
	anchors := captureGuardAnchors{root: root, evidence: evidence}
	hashes, err := captureRetainedHashes(anchors)
	if err != nil {
		t.Fatal(err)
	}
	write(evidence, "capture-process-restored.json", captureProcessRestoration{Version: 1, Worker: worker, SourceStart: config.Start,
		SourceStateSHA: sum(state), ArmSHA: hashes[0], RequestSHA: hashes[1], ResultSHA: hashes[2], NetworkSHA: hashes[3]})
	write(evidence, "timer-prepare-intent.json", map[string]any{"token": old})
	write(evidence, "timer-prepare-complete.json", map[string]any{"version": 1, "token": old, "passed": true,
		"source_frozen": true, "tracers_detached": true, "targets": []int{10}, "code_sha256": strings.Repeat("a", 64), "private_bytes_per_new_target": 8192})
	for _, name := range timerRecoveryPhaseFiles {
		if name == "guard-intent.json" {
			write(phase, name, child)
		} else if name == "guard-outcome.json" {
			write(phase, name, map[string]any{"config": child, "reason": "controller_disarmed_held", "status": "source_held_guard_disarmed", "capture_in_flight": false, "passed": true})
		} else if name == "timer-activation-complete.json" {
			write(phase, name, map[string]any{"version": 1, "token": id, "passed": true, "source_frozen": true, "tracers_detached": true, "code_sha256": strings.Repeat("c", 64), "targets": []int{10}})
		} else {
			write(phase, name, map[string]any{"token": id})
		}
	}
	witness, err := timerRecoveryWitness(config, anchors, phase)
	if err != nil {
		t.Fatal(err)
	}
	return config, child, anchors, phase, timerRecoveryProof{CaptureID: id, ActorPID: child.Controller, ActorStart: arm.GuardStart, Witness: witness}
}

func TestNativeCheckpointRetirementTimerRecoveryCertificate(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_RETIREMENT") != "1" {
		t.Skip("owned root-file retirement fixture only")
	}
	for _, scenario := range []string{"valid", "large-preparation", "unknown-preparation-field", "changed-witness", "missing-witness", "foreign-actor", "foreign-birth", "same-generation", "changed-source", "wrong-parent", "wrong-original-token", "unfinished-guard", "running-disarm", "changed-code", "changed-original-state", "missing-grant", "symlink-phase"} {
		t.Run(scenario, func(t *testing.T) {
			config, child, anchors, phase, proof := timerRecoveryReceiptFixture(t)
			update := func(file *os.File, name string, change func(map[string]any)) {
				t.Helper()
				path := filepath.Join(file.Name(), name)
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var value map[string]any
				if err := json.Unmarshal(data, &value); err != nil {
					t.Fatal(err)
				}
				change(value)
				data, err = json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
				proof.Witness, err = timerRecoveryWitness(config, anchors, phase)
				if err != nil {
					t.Fatal(err)
				}
			}
			switch scenario {
			case "large-preparation":
				update(anchors.evidence, "timer-prepare-complete.json", func(v map[string]any) {
					targets := make([]any, 8)
					for index := range targets {
						targets[index] = map[string]any{"tid": 10 + index, "frame": make([]byte, 8192)}
					}
					v["targets"] = targets
				})
			case "unknown-preparation-field":
				update(anchors.evidence, "timer-prepare-complete.json", func(v map[string]any) { v["unknown"] = true })
			case "changed-witness":
				proof.Witness["activation/guard-intent.json"] = strings.Repeat("0", 64)
			case "missing-witness":
				delete(proof.Witness, "activation/guard-outcome.json")
			case "foreign-actor":
				proof.ActorPID++
			case "foreign-birth":
				proof.ActorStart += "changed"
			case "same-generation":
				proof.CaptureID = config.Token
			case "changed-source":
				update(phase, "guard-intent.json", func(v map[string]any) { v["source"].(map[string]any)["owner"] = strings.Repeat("0", 32) })
			case "wrong-parent":
				update(phase, "guard-intent.json", func(v map[string]any) { v["controller"] = 999 })
			case "wrong-original-token":
				update(phase, "guard-intent.json", func(v map[string]any) { v["timer_recovery_of"] = child.Token })
			case "unfinished-guard":
				update(phase, "guard-outcome.json", func(v map[string]any) { v["capture_in_flight"] = true })
			case "running-disarm":
				update(phase, "guard-outcome.json", func(v map[string]any) {
					v["status"] = "source_running_guard_disarmed"
					v["reason"] = "controller_disarmed"
				})
			case "changed-code":
				update(phase, "timer-activation-complete.json", func(v map[string]any) { v["code_sha256"] = strings.Repeat("f", 64) })
			case "changed-original-state":
				if err := os.WriteFile(filepath.Join(filepath.Dir(config.Evidence), "source-state.json"), []byte("changed"), 0600); err != nil {
					t.Fatal(err)
				}
				var err error
				proof.Witness, err = timerRecoveryWitness(config, anchors, phase)
				if err != nil {
					t.Fatal(err)
				}
			case "missing-grant":
				if err := os.Remove(filepath.Join(config.Evidence, "criu-retained-request.json")); err != nil {
					t.Fatal(err)
				}
			case "symlink-phase":
				if err := os.Rename(child.Evidence, child.Evidence+".elsewhere"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(child.Evidence+".elsewhere", child.Evidence); err != nil {
					t.Fatal(err)
				}
			}
			raw, err := json.Marshal(proof)
			if err != nil {
				t.Fatal(err)
			}
			err = verifyTimerRecoveryCompletion(config, anchors, strings.Repeat("c", 64), raw)
			if (err == nil) != (scenario == "valid" || scenario == "large-preparation") {
				t.Fatalf("unexpected timer recovery certificate result: %v", err)
			}
		})
	}
}

func TestNativeCheckpointRetirementWorkerExitTimerRecoveryGuard(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_RETIREMENT") != "1" {
		t.Skip("owned root-file retirement fixture only")
	}
	for _, scenario := range []string{"valid", "unfinished", "missing-activation", "wrong-source", "same-generation", "missing-mode", "running-disarm"} {
		t.Run(scenario, func(t *testing.T) {
			config, child, _, phase, _ := timerRecoveryReceiptFixture(t)
			outcome := map[string]any{"config": child, "reason": "controller_disarmed_held",
				"status": "source_held_guard_disarmed", "capture_in_flight": false, "passed": true}
			switch scenario {
			case "unfinished":
				outcome["capture_in_flight"] = true
			case "missing-activation":
				if err := os.Remove(filepath.Join(child.Evidence, "timer-activation-complete.json")); err != nil {
					t.Fatal(err)
				}
			case "wrong-source":
				child.Source.Owner = strings.Repeat("0", 32)
			case "same-generation":
				child.TimerRecoveryOf = child.Token
			case "missing-mode":
				child.TimerRecoveryOf = ""
			case "running-disarm":
				outcome["reason"], outcome["status"] = "controller_disarmed", "source_running_guard_disarmed"
			}
			outcome["config"] = child
			for name, value := range map[string]any{"guard-intent.json": child, "guard-outcome.json": outcome} {
				data, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(phase.Name(), name), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			root, err := os.OpenRoot(config.Root)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			engine := Engine{root: root, config: Config{Root: config.Root}, boot: config.Source.BootID}
			record := config.Source
			record.Phase = "deleted"
			relative := filepath.Join("objects", record.ID, "captures", child.Token)
			accepted, err := engine.exitCompletedTimerGuard(record, child.Token, relative)
			if scenario == "valid" && (err != nil || !accepted) {
				t.Fatalf("completed held timer guard was not accepted: %t, %v", accepted, err)
			}
			if scenario != "valid" && (err == nil || accepted) {
				t.Fatalf("unconfirmed held timer guard was accepted: %t, %v", accepted, err)
			}
		})
	}
}
