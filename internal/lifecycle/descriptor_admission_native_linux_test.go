//go:build linux

package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDescriptorAdmissionNative(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_FILESYSTEM_ADMISSION") != "1" {
		t.Skip("requires the owned pre-capture admission fixture")
	}
	for index, scenario := range []struct {
		name, operation  string
		child, transient bool
	}{
		{"held-root-checkpoint", "checkpoint", false, false},
		{"held-child-pause", "pause", true, false},
		{"transient-checkpoint", "checkpoint", false, true},
	} {
		if !t.Run(scenario.name, func(t *testing.T) {
			engine, bundle := nativeEngine(t,
				"/bin/env FACTORYBOX_CAPTURE_ACTOR=1 /bin/capture-actor -test.run=^TestCaptureActor$ -test.timeout=0")
			binary, err := os.ReadFile(os.Args[0])
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(bundle+"/lower/bin/capture-actor", binary, 0755); err != nil {
				t.Fatal(err)
			}
			engine.config.Criu, engine.config.CriuSHA256 = "/out/criu", os.Getenv("FACTORYBOX_CRIU_SHA256")
			engine.config.ManagerRoot = "/var/lib/sysbox/manager"
			ctx := context.Background()
			source, err := engine.Start(ctx, "descriptor", bundle)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				group, err := captureGuardOpen(cgroupRoot + "/" + source.Record.RuntimeID)
				if err != nil {
					t.Error(err)
					return
				}
				defer group.Close()
				events, err := captureGuardEvents(group)
				if err != nil || events["frozen"] != 0 {
					t.Log("unproved source remains held for owned outer-fixture archival", err)
					return
				}
				if _, err := engine.Delete(ctx, source.Record.ID); err != nil {
					t.Error(err)
				}
			})
			deadline := time.Now().Add(5 * time.Second)
			var initial captureActorState
			for {
				initial, err = readCaptureActor(source.Runtime.PID, "get")
				if err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("descriptor actor did not become ready", err)
				}
				time.Sleep(5 * time.Millisecond)
			}
			directory := "/state"
			if scenario.child {
				initial, err = readCaptureActor(source.Runtime.PID, "make-memory-child")
				if err != nil || initial.NestedPID <= 1 {
					t.Fatal("create owned descriptor child", err)
				}
				directory = "/child-state"
				deadline = time.Now().Add(5 * time.Second)
				for {
					if _, err := readCaptureActorAt(source.Runtime.PID, directory, "get"); err == nil {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("owned descriptor child did not become ready")
					}
					time.Sleep(5 * time.Millisecond)
				}
			}
			request := "open-fscontext"
			if scenario.transient {
				request = "open-transient-fscontext"
			} else if !scenario.child {
				request = "open-many-fscontext"
			}
			opened, err := readCaptureActorAt(source.Runtime.PID, directory, request)
			if err != nil || opened.MountContextFD <= 2 {
				t.Fatal("create the real guest fscontext descriptor", err)
			}
			if !scenario.child && !scenario.transient && opened.MountContexts != 128 {
				t.Fatal("bounded evidence control lacks its real descriptor pressure")
			}
			snapshot := strings.Repeat(fmt.Sprint(index+7), 32)
			relative, _ := checkpointPath(source.Record.ID, snapshot)
			command := func() error {
				ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, "/factorybox", "--root", engine.config.Root,
					"--runtime", engine.config.Runtime, "--runtime-sha256", engine.config.RuntimeSHA256,
					"--criu", engine.config.Criu, "--criu-sha256", engine.config.CriuSHA256,
					"--manager-root", engine.config.ManagerRoot,
					scenario.operation, "--snapshot-id", snapshot, source.Record.ID)
				output, err := cmd.CombinedOutput()
				if scenario.transient {
					if err != nil {
						return fmt.Errorf("capture after transient descriptor settled: %w: %s", err, output)
					}
				} else if err == nil || !strings.Contains(string(output), "fscontext") {
					return fmt.Errorf("held descriptor was accepted or failed elsewhere: %v: %s", err, output)
				}
				return nil
			}
			if err := command(); err != nil {
				t.Fatal(err)
			}
			view, err := engine.Inspect(ctx, source.Record.ID)
			if err != nil || view.Runtime == nil || view.Runtime.PID != source.Runtime.PID ||
				view.Runtime.Created != source.Runtime.Created ||
				view.Runtime.Status != "running" || view.Record.Phase != "running" {
				t.Fatalf("descriptor admission lost its original running source: %v: %+v", err, view)
			}
			if scenario.transient {
				current, err := readCaptureActor(source.Runtime.PID, "get")
				if err != nil || !reflect.DeepEqual(current, initial) {
					t.Fatal("transient descriptor changed original application RAM", err)
				}
				var observation map[string]any
				err = engine.readCheckpointJSON(relative+"/guard/descriptor-admission.json", &observation)
				attempts, ok := observation["attempts"].(float64)
				if err != nil || !ok || attempts < 2 || observation["passed"] != true {
					t.Fatal("transient proof did not observe and wait for the actual mount context", err, observation)
				}
				fork, err := forkNativeCLI(engine, source.Record.ID, snapshot, "settled-fork")
				if err != nil {
					t.Fatal("fork of settled source", err)
				}
				t.Cleanup(func() {
					if _, err := engine.Delete(ctx, "settled-fork"); err != nil {
						t.Error(err)
					}
				})
				current, err = readCaptureActor(fork.Runtime.PID, "get")
				if err != nil || !reflect.DeepEqual(current, initial) {
					t.Fatal("settled snapshot lost its independently usable application RAM", err)
				}
				if _, err := readCaptureActor(source.Runtime.PID, "advance"); err != nil {
					t.Fatal(err)
				}
				current, err = readCaptureActor(fork.Runtime.PID, "get")
				if err != nil || !reflect.DeepEqual(current, initial) {
					t.Fatal("settled fork shares mutable source RAM", err)
				}
				return
			}
			current, err := readCaptureActorAt(source.Runtime.PID, directory, "get")
			if err != nil || !reflect.DeepEqual(current, opened) {
				t.Fatal("descriptor rejection changed original RAM or its open descriptor", err)
			}
			for _, name := range []string{"images", "checkpoint.json", "capture-result.json",
				"guard/guard-capture-entered.json", "guard/capture-worker-intent.json"} {
				if _, err := os.Stat(filepath.Join(engine.config.Root, relative, name)); !os.IsNotExist(err) {
					t.Fatal("descriptor preflight entered CRIU capture or published backing", name, err)
				}
			}
			var rejection checkpointAdmissionRejection
			err = engine.readCheckpointJSON(relative+"/admission-rejected.json", &rejection)
			if err != nil || rejection.Kind != "descriptors" {
				t.Fatal("descriptor rejection lacks its exact durable kind", err, rejection)
			}
			evidence, err := os.ReadFile(filepath.Join(engine.config.Root, relative, "guard/descriptor-admission.json"))
			if err != nil || len(evidence) > 4096 {
				t.Fatal("descriptor pressure produced unbounded evidence", err, len(evidence))
			}
			beforeReplay, err := engine.read(source.Record.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err := command(); err != nil {
				t.Fatal("replay the same rejected operation without recapture", err)
			}
			afterReplay, err := engine.read(source.Record.ID)
			if err != nil || !reflect.DeepEqual(beforeReplay, afterReplay) {
				t.Fatal("descriptor rejection replay changed immutable history", err)
			}
			if scenario.operation == "checkpoint" {
				path := filepath.Join(engine.config.Root, relative, "guard/descriptor-admission.json")
				original, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var changed captureDescriptorAdmission
				if err := json.Unmarshal(original, &changed); err != nil {
					t.Fatal(err)
				}
				changed.Token = strings.Repeat("f", 32)
				corrupt, err := json.Marshal(changed)
				if err != nil || os.WriteFile(path, corrupt, 0600) != nil {
					t.Fatal("prepare owned descriptor observation corruption", err)
				}
				_, replayErr := engine.Checkpoint(ctx, source.Record.ID, snapshot)
				if err := os.WriteFile(path, original, 0600); err != nil {
					t.Fatal(err)
				}
				if replayErr == nil || !strings.Contains(replayErr.Error(), "descriptor admission observation differs") {
					t.Fatal("changed descriptor observation authorized immutable replay", replayErr)
				}
			}
			if scenario.operation == "pause" &&
				(view.Record.Pause != nil || len(view.Record.PauseHistory) != 1 || view.Record.PauseHistory[0].RejectedAt == "") {
				t.Fatal("descriptor rejection left an unresolved pause")
			}
			if _, err := readCaptureActorAt(source.Runtime.PID, directory, "advance"); err != nil {
				t.Fatal("rejected source cannot continue useful work", err)
			}
			if _, err := readCaptureActorAt(source.Runtime.PID, directory, "close-fscontext"); err != nil {
				t.Fatal(err)
			}
		}) {
			// A failed source remains held. Do not create another live sandbox
			// before the exact outer fixture has archived and cleaned it.
			return
		}
	}
}
