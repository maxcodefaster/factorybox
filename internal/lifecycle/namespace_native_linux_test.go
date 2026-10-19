//go:build linux

package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"factorybox/internal/sysboxcapture"
)

func TestCaptureNamespacesNative(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_NAMESPACES") != "1" {
		t.Skip("requires the bounded native namespace-admission fixture")
	}
	mode, expectedMode := "1", "child"
	probe, err := openRuntime("/out/criu", os.Getenv("FACTORYBOX_CRIU_SHA256"))
	if err != nil {
		t.Fatal(err)
	}
	if sysboxcapture.InspectFeatures(context.Background(), probe).RootOwnedTimeNamespaces {
		mode, expectedMode = "future-thread", "future-thread"
	}
	probe.Close()
	engine, bundle := nativeEngine(t,
		"/bin/env FACTORYBOX_CAPTURE_ACTOR=1 FACTORYBOX_CAPTURE_NESTED_TIME="+mode+" "+
			"/bin/capture-actor -test.run=^TestCaptureActor$ -test.timeout=0")
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
	source, err := engine.Start(ctx, "namespaces", bundle)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		// An unconfirmed failed CRIU call may leave the guard holding its
		// source. Do not exec or force lifecycle reconciliation into that
		// frozen tree; the outer fixture archives and stops its own namespace.
		group, err := captureGuardOpen(cgroupRoot + "/" + source.Record.RuntimeID)
		if err != nil {
			t.Error(err)
			return
		}
		defer group.Close()
		events, err := captureGuardEvents(group)
		if err != nil || events["frozen"] != 0 {
			t.Log("failed capture retained its frozen owned source for outer archive", err)
			return
		}
		if _, err := engine.Delete(ctx, source.Record.ID); err != nil {
			t.Error("delete exact native namespace source", err)
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	var before captureActorState
	for {
		before, err = readCaptureActor(source.Runtime.PID, "get")
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("minimal namespace actor not ready", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if before.NestedPID <= 1 || before.TimeMode != expectedMode || len(before.Payload) != 8192 || before.Counter != 0 {
		t.Fatal("minimal namespace actor lacks its child or RAM payload")
	}
	command := func(operation, snapshot string) ([]byte, error) {
		bounded, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		cmd := exec.CommandContext(bounded, "/factorybox", "--root", engine.config.Root,
			"--runtime", engine.config.Runtime, "--runtime-sha256", engine.config.RuntimeSHA256,
			"--criu", engine.config.Criu, "--criu-sha256", engine.config.CriuSHA256,
			"--manager-root", engine.config.ManagerRoot,
			operation, "--snapshot-id", snapshot, source.Record.ID)
		return cmd.CombinedOutput()
	}
	start := time.Now()
	snapshot := strings.Repeat("7", 32)
	output, rejected := command("checkpoint", snapshot)
	if err := os.WriteFile("/output/namespace-checkpoint.txt", output, 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("minimal nested time namespace checkpoint response=%s: %s", time.Since(start), output)
	group, err := captureGuardOpen(cgroupRoot + "/" + source.Record.RuntimeID)
	if err != nil {
		t.Fatal(err)
	}
	events, err := captureGuardEvents(group)
	group.Close()
	if err != nil || events["frozen"] != 0 {
		t.Fatal("unsupported namespace was not rejected before CRIU; original source remains frozen", err)
	}
	if rejected == nil || !strings.Contains(string(output), "namespace admission") ||
		!strings.Contains(string(output), "nested time namespace") {
		t.Fatal("public checkpoint did not reject the unsupported namespace before capture", rejected)
	}
	after, err := readCaptureActor(source.Runtime.PID, "get")
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("rejected checkpoint changed or stopped the complete original actor state", err)
	}
	generation := engine.config.Root + "/objects/namespaces/captures/" + snapshot
	if _, err := os.Stat(generation + "/images/dump.log"); !os.IsNotExist(err) {
		t.Fatal("namespace admission invoked CRIU", err)
	}
	if _, err := os.Stat(generation + "/guard/guard-capture-entered.json"); !os.IsNotExist(err) {
		t.Fatal("namespace admission marked a capture in flight", err)
	}
	if _, err := os.Stat(generation + "/admission-rejected.json"); err != nil {
		t.Fatal("namespace admission lost its retained rejection", err)
	}
	if _, err := command("checkpoint", snapshot); err == nil {
		t.Fatal("rejected immutable checkpoint identity was silently reused")
	}
	pausedOutput, pausedError := command("pause", strings.Repeat("8", 32))
	var rejectedPause View
	if pausedError == nil || json.NewDecoder(bytes.NewReader(pausedOutput)).Decode(&rejectedPause) != nil ||
		rejectedPause.Record.Phase != "running" || rejectedPause.Record.Pause != nil ||
		len(rejectedPause.Record.PauseHistory) != 1 {
		t.Fatal("rejected pause did not retain a running source and its failed intent", pausedError, string(pausedOutput))
	}
	failedPause := rejectedPause.Record.PauseHistory[0]
	if failedPause.SnapshotID != strings.Repeat("8", 32) || failedPause.RejectedAt == "" ||
		!strings.Contains(failedPause.Error, "namespace admission") || failedPause.CompletedAt != "" ||
		failedPause.Resume != nil || failedPause.SourcePID != source.Runtime.PID {
		t.Fatal("failed pause history claimed success or lost its original request", failedPause)
	}
	if _, err := command("pause", strings.Repeat("8", 32)); err == nil {
		t.Fatal("failed immutable pause identity was silently reused")
	}
	t.Run("recover-rejected-pause-outcome", func(t *testing.T) {
		var guard captureGuardConfig
		pending := engine.config.Root + "/objects/namespaces/captures/" + strings.Repeat("8", 32) + "/guard/guard-intent.json"
		data, err := os.ReadFile(pending)
		if err != nil || json.Unmarshal(data, &guard) != nil ||
			guard.Source.Phase != "pausing" || guard.Source.Pause == nil {
			t.Fatal("rejection has no original persisted pause intent", err)
		}
		if err := engine.write(guard.Source); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := engine.write(rejectedPause.Record); err != nil {
				t.Error(err)
			}
		}()
		outcomePath := strings.TrimSuffix(pending, "guard-intent.json") + "guard-outcome.json"
		outcome, err := os.ReadFile(outcomePath)
		if err != nil {
			t.Fatal(err)
		}
		var changed map[string]any
		if err := json.Unmarshal(outcome, &changed); err != nil {
			t.Fatal(err)
		}
		changed["capture_in_flight"] = true
		unsafe, err := json.Marshal(changed)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(outcomePath, unsafe, 0600); err != nil {
			t.Fatal(err)
		}
		unsafeOutput, unsafeError := command("pause", strings.Repeat("8", 32))
		restoreErr := os.WriteFile(outcomePath, outcome, 0600)
		var refused View
		if unsafeError == nil || restoreErr != nil ||
			json.NewDecoder(bytes.NewReader(unsafeOutput)).Decode(&refused) != nil ||
			!reflect.DeepEqual(refused.Record, guard.Source) {
			t.Fatal("unproved source outcome was promoted to a running pause result", unsafeError, restoreErr)
		}
		output, rejected := command("pause", strings.Repeat("8", 32))
		var recovered View
		if rejected == nil || json.NewDecoder(bytes.NewReader(output)).Decode(&recovered) != nil ||
			!strings.Contains(string(output), "namespace admission") ||
			!reflect.DeepEqual(recovered.Record, rejectedPause.Record) {
			t.Fatal("lost rejected-pause publication did not recover its original running outcome", rejected, string(output))
		}
	})
	after, err = readCaptureActor(source.Runtime.PID, "get")
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("rejected pause changed or stopped the original actor state", err)
	}
	advanced, err := readCaptureActor(source.Runtime.PID, "advance")
	if err != nil || advanced.Counter != 1 || advanced.Payload[0] != before.Payload[0]^0xff {
		t.Fatal("source did not continue useful work after both rejected operations", err)
	}
	withoutChild, err := readCaptureActor(source.Runtime.PID, "drop-time-child")
	if err != nil || withoutChild.NestedPID != 0 || withoutChild.Counter != 1 {
		t.Fatal("minimal source did not remove its exact time namespace child", err)
	}
	sharedChild, err := readCaptureActor(source.Runtime.PID, "make-shared-child")
	if err != nil || sharedChild.NestedPID <= 1 || sharedChild.TimeMode != "shared" || sharedChild.Counter != 1 {
		t.Fatal("source did not recreate the same process tree with a shared time namespace", err)
	}
	output, err = command("checkpoint", strings.Repeat("9", 32))
	if err != nil {
		t.Fatalf("ordinary capture after removing the unsupported child: %v: %s", err, output)
	}
	after, err = readCaptureActor(source.Runtime.PID, "get")
	if err != nil || !reflect.DeepEqual(sharedChild, after) {
		t.Fatal("ordinary capture did not preserve the continued original RAM/files", err)
	}
}

func TestCaptureChildrenNamespaceNative(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_NAMESPACES") != "1" {
		t.Skip("requires the bounded native namespace-admission fixture")
	}
	engine, bundle := nativeEngine(t,
		"/bin/env FACTORYBOX_CAPTURE_ACTOR=1 FACTORYBOX_CAPTURE_NESTED_TIME=children-only "+
			"/bin/capture-actor -test.run=^TestCaptureActor$ -test.timeout=0")
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
	source, err := engine.Start(ctx, "children", bundle)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := engine.Delete(ctx, "children"); err != nil {
			t.Error(err)
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	var before captureActorState
	for {
		before, err = readCaptureActor(source.Runtime.PID, "get")
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if before.TimeMode != "children-only" || before.NestedPID != 0 {
		t.Fatal("actor did not change only its future-children namespace")
	}
	_, err = engine.Checkpoint(ctx, "children", strings.Repeat("6", 32))
	if err == nil || !strings.Contains(err.Error(), "namespace admission") ||
		!strings.Contains(err.Error(), "time_for_children") {
		t.Fatal("namespace admission missed the thread's changed future-children namespace", err)
	}
	after, err := readCaptureActor(source.Runtime.PID, "get")
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("rejected future-children namespace changed or stopped the actor", err)
	}
}
