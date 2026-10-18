//go:build linux

package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"factorybox/internal/sysboxcapture"
)

func pauseNativeCLI(engine *Engine, id, snapshot string) (View, error) {
	return transitionNativeCLI(engine, "pause", id, snapshot)
}

func transitionNativeCLI(engine *Engine, operation, id, snapshot string) (View, error) {
	command := exec.Command("/factorybox", "--root", engine.config.Root,
		"--runtime", engine.config.Runtime, "--runtime-sha256", engine.config.RuntimeSHA256,
		"--criu", engine.config.Criu, "--criu-sha256", engine.config.CriuSHA256,
		"--manager-root", engine.config.ManagerRoot, operation, "--snapshot-id", snapshot, id)
	output, err := command.CombinedOutput()
	var view View
	if err != nil {
		return view, fmt.Errorf("public %s: %w: %s", operation, err, output)
	}
	err = json.Unmarshal(output, &view)
	return view, err
}

func TestPauseNative(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_PAUSE") != "1" {
		t.Skip("requires the owned native pause fixture")
	}
	engine, bundle := nativeEngine(t, "/bin/env FACTORYBOX_CAPTURE_ACTOR=1 FACTORYBOX_CAPTURE_BALLAST=1 /bin/capture-actor -test.run=^TestCaptureActor$ -test.timeout=0")
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
	started, err := engine.Start(ctx, "pausable", bundle)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := engine.Delete(ctx, "pausable"); err != nil {
			t.Error("delete exact pause fixture source", err)
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	var before captureActorState
	for {
		before, err = readCaptureActor(started.Runtime.PID, "get")
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("pause RAM actor not ready", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if before.BallastBytes != 32<<20 || before.BallastSHA == "" {
		t.Fatal("pause fixture lacks its private 32-MiB RAM payload")
	}
	lock, err := engine.lock()
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := engine.command(ctx, lock, "exec", started.Record.RuntimeID,
		"/bin/sh", "-c", "printf '17\\n' > /proc/sys/kernel/panic")
	closeErr := lock.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatal(writeErr, closeErr)
	}
	snapshot := strings.Repeat("8", 32)
	begin := time.Now()
	paused, err := pauseNativeCLI(engine, "pausable", snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if paused.Record.Phase != "paused" || paused.Runtime == nil || paused.Runtime.Status != "stopped" ||
		paused.Record.Owner != started.Record.Owner || paused.Record.RuntimeCreated != started.Record.RuntimeCreated ||
		paused.Record.Pause == nil || paused.Record.Pause.SourcePID != started.Runtime.PID {
		t.Fatal("pause did not retain its stopped original runtime identity")
	}
	proof := paused.Record.Pause
	relative, _ := checkpointPath("pausable", snapshot)
	captured, err := engine.readCheckpoint(relative, "checkpoint.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := sysboxcapture.VerifyLiveFilesystemSnapshot(ctx, captured.Filesystem); err != nil {
		_, liveTmp := os.Lstat(bundle + "/upper/tmp")
		_, savedTmp := os.Lstat(engine.config.Root + "/" + relative + "/filesystem/upper/tmp")
		t.Fatalf("working backing changed before any fork/resume: %v; live tmp=%v saved tmp=%v", err, liveTmp, savedTmp)
	}
	if proof.Before.Anon < 32<<20 || proof.After.Anon >= proof.Before.Anon-24<<20 ||
		!proof.Snapshot.FileBacked || proof.Snapshot.Bytes < 32<<20 || proof.Snapshot.ResidentBytesBefore == 0 ||
		proof.Snapshot.ResidentBytesAfter != 0 {
		t.Fatal("pause did not prove process/snapshot RAM release", proof)
	}
	replayed, err := pauseNativeCLI(engine, "pausable", snapshot)
	if err != nil || !reflect.DeepEqual(replayed, paused) {
		t.Fatal("pause replay changed its stopped runtime or original evidence", err)
	}
	t.Logf("full pause=%s original anonymous bytes=%d stopped anonymous bytes=%d; snapshot bytes=%d resident before=%d after=%d; file-backed payload verified without global cache settings",
		time.Since(begin), proof.Before.Anon, proof.After.Anon, proof.Snapshot.Bytes,
		proof.Snapshot.ResidentBytesBefore, proof.Snapshot.ResidentBytesAfter)
	forked, err := forkNativeCLI(engine, "pausable", snapshot, "paused-fork")
	if err != nil {
		t.Fatal("fork from fully stopped pause checkpoint", err)
	}
	t.Cleanup(func() {
		if _, err := engine.Delete(ctx, "paused-fork"); err != nil {
			t.Error(err)
		}
	})
	assertRestoredNative(t, engine, forked, before)
	if _, err := engine.Delete(ctx, "paused-fork"); err != nil {
		t.Fatal(err)
	}
	resumeBegin := time.Now()
	resumed, err := transitionNativeCLI(engine, "resume", "pausable", snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Record.Phase != "running" || resumed.Runtime == nil || resumed.Runtime.Status != "running" ||
		resumed.Record.Owner != started.Record.Owner || resumed.Record.RuntimeID != started.Record.RuntimeID ||
		resumed.Record.RuntimeCreated != started.Record.RuntimeCreated || resumed.Runtime.PID == started.Runtime.PID ||
		resumed.Record.Pause.Resume == nil || resumed.Record.Pause.Resume.CompletedAt == "" {
		t.Fatal("resume did not retain runtime identity with a renewed process")
	}
	assertRestoredNative(t, engine, resumed, before)
	again, err := transitionNativeCLI(engine, "resume", "pausable", snapshot)
	if err != nil || !reflect.DeepEqual(again, resumed) {
		t.Fatal("resume replay replaced its original process", err)
	}
	t.Logf("full resume and first useful private RAM/file/volume/virtual-state checks=%s; retained runtime ID/creation, renewed PID and exact 32-MiB payload",
		time.Since(resumeBegin))
	t.Run("lost-resume-outcome", func(t *testing.T) {
		lost := resumed.Record
		operation := *lost.Pause.Resume
		pause := *lost.Pause
		operation.CompletedAt, operation.Error = "", "resume controller outcome lost"
		pause.Resume, lost.Pause, lost.Phase = &operation, &pause, "resuming"
		if err := engine.write(lost); err != nil {
			t.Fatal(err)
		}
		recovered, err := transitionNativeCLI(engine, "resume", "pausable", snapshot)
		if err != nil || !reflect.DeepEqual(recovered, resumed) {
			t.Fatal("resume receipt did not reconcile its original process", err)
		}
	})
	t.Run("missing-resume-receipt", func(t *testing.T) {
		path := engine.config.Root + "/objects/pausable/resumes/" + snapshot + "/resume-result.json"
		if err := os.Rename(path, path+".held"); err != nil {
			t.Fatal(err)
		}
		_, rejected := transitionNativeCLI(engine, "resume", "pausable", snapshot)
		if err := os.Rename(path+".held", path); err != nil {
			t.Fatal(err)
		}
		if rejected == nil {
			t.Fatal("resume replay accepted unconfirmed completion")
		}
		assertRestoredNative(t, engine, resumed, before)
	})
	advanced, err := readCaptureActor(resumed.Runtime.PID, "advance")
	if err != nil || advanced.Counter != 1 || advanced.BallastSHA == before.BallastSHA {
		t.Fatal("resumed process did not independently advance its private RAM", err)
	}
	resumedSnapshot := strings.Repeat("9", 32)
	if _, err := engine.Checkpoint(ctx, "pausable", resumedSnapshot); err != nil {
		t.Fatal("checkpoint a previously resumed source", err)
	}
	child, err := forkNativeCLI(engine, "pausable", resumedSnapshot, "resumed-fork")
	if err != nil {
		t.Fatal("fork a previously resumed source", err)
	}
	t.Cleanup(func() {
		if _, err := engine.Delete(ctx, "resumed-fork"); err != nil {
			t.Error(err)
		}
	})
	childState, err := readCaptureActor(child.Runtime.PID, "get")
	if err != nil || !reflect.DeepEqual(childState, advanced) {
		t.Fatal("fork did not retain the resumed source's current private RAM", err)
	}
	if _, err := readCaptureActor(child.Runtime.PID, "advance"); err != nil {
		t.Fatal(err)
	}
	parentState, err := readCaptureActor(resumed.Runtime.PID, "get")
	if err != nil || !reflect.DeepEqual(parentState, advanced) {
		t.Fatal("fork mutation changed its resumed source", err)
	}
	if _, err := engine.Delete(ctx, "resumed-fork"); err != nil {
		t.Fatal(err)
	}
	second := strings.Repeat("a", 32)
	pausedAgain, err := pauseNativeCLI(engine, "pausable", second)
	if err != nil {
		t.Fatal("second pause after independently useful resumed work", err)
	}
	if len(pausedAgain.Record.PauseHistory) != 1 || pausedAgain.Record.PauseHistory[0].SnapshotID != snapshot ||
		pausedAgain.Record.PauseHistory[0].Resume.CompletedAt != resumed.Record.Pause.Resume.CompletedAt ||
		pausedAgain.Record.Pause.Snapshot.ResidentBytesAfter != 0 || pausedAgain.Record.Pause.After.Anon >= 8<<20 {
		t.Fatal("second pause lost history or retained private RAM")
	}
	resumedAgain, err := transitionNativeCLI(engine, "resume", "pausable", second)
	if err != nil {
		t.Fatal("resume the second independent pause generation", err)
	}
	finalState, err := readCaptureActor(resumedAgain.Runtime.PID, "get")
	if err != nil || !reflect.DeepEqual(finalState, advanced) ||
		resumedAgain.Record.Owner != started.Record.Owner ||
		resumedAgain.Record.RuntimeCreated != started.Record.RuntimeCreated ||
		resumedAgain.Runtime.PID == resumed.Runtime.PID {
		t.Fatal("second resume lost current RAM or immutable runtime identity", err)
	}
	t.Log("forked and mutated a resumed source independently, then paused/resumed its current 32-MiB state a second time; original history/owner/creation retained, snapshot residency zero at each pause")
}
