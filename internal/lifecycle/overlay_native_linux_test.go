//go:build linux

package lifecycle

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestNestedOverlayNative(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_OVERLAY") != "1" {
		t.Skip("requires the bounded nested-overlay diagnostic fixture")
	}
	for _, mode := range []string{"plain", "direct", "relative-link", "escaping-link"} {
		t.Run(mode, func(t *testing.T) { nestedOverlayNative(t, mode) })
	}
}

func nestedOverlayNative(t *testing.T, mode string) {
	engine, bundle := nativeEngine(t, "/bin/sh /bin/nested-overlay.sh")
	binary, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundle+"/lower/bin/capture-actor", binary, 0755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nset -eu\n"
	if mode != "plain" {
		script += `mkdir -p /var/lib/docker/lower /var/lib/docker/upper /var/lib/docker/work /var/lib/docker/merged /var/lib/docker/links
printf 'lower-before\n' > /var/lib/docker/lower/marker
ln -s ../lower /var/lib/docker/links/layer
`
		layer := "/var/lib/docker/lower"
		if mode != "direct" {
			layer = "/var/lib/docker/links/layer"
		}
		script += "mount -t overlay overlay -o lowerdir=" + layer +
			",upperdir=/var/lib/docker/upper,workdir=/var/lib/docker/work /var/lib/docker/merged\n"
		script += "printf 'upper-before\\n' > /var/lib/docker/merged/upper-marker\n"
		if mode == "escaping-link" {
			// The original mounted overlay retains its original lower. Its
			// recorded option now names a link escaping the copied volume.
			script += "rm /var/lib/docker/links/layer\nln -s / /var/lib/docker/links/layer\n"
		}
	}
	script += "exec /bin/env FACTORYBOX_CAPTURE_ACTOR=1 /bin/capture-actor -test.run=^TestCaptureActor$ -test.timeout=0\n"
	if err := os.WriteFile(bundle+"/lower/bin/nested-overlay.sh", []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	engine.config.Criu, engine.config.CriuSHA256 = "/out/criu", os.Getenv("FACTORYBOX_CRIU_SHA256")
	engine.config.ManagerRoot = "/var/lib/sysbox/manager"
	ctx := context.Background()
	source, err := engine.Start(ctx, "overlay-source", bundle)
	if err != nil {
		t.Fatal(err)
	}
	cleanup := func(id string) {
		t.Helper()
		t.Cleanup(func() {
			if _, err := engine.Delete(ctx, id); err != nil {
				t.Error("delete exact overlay fixture", id, err)
			}
		})
	}
	cleanup("overlay-source")
	deadline := time.Now().Add(5 * time.Second)
	var before captureActorState
	for {
		before, err = readCaptureActor(source.Runtime.PID, "get")
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("overlay actor did not become ready", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(before.Payload) != 8192 || before.Counter != 0 {
		t.Fatal("actor lacks its original RAM payload")
	}
	assertState := func(view View, expected captureActorState) {
		t.Helper()
		state, err := readCaptureActor(view.Runtime.PID, "get")
		if err != nil || !reflect.DeepEqual(state, expected) {
			t.Fatal("overlay lifecycle lost or shared actor RAM", view.Record.ID, err)
		}
	}
	layerFile := func(view View, name string) string {
		return fmt.Sprintf("/proc/%d/root/var/lib/docker/merged/%s", view.Runtime.PID, name)
	}
	assertLayers := func(view View, upper string) {
		t.Helper()
		if mode == "plain" {
			return
		}
		for name, expected := range map[string]string{"marker": "lower-before\n", "upper-marker": upper} {
			data, err := os.ReadFile(layerFile(view, name))
			if err != nil || string(data) != expected {
				t.Fatal("mounted overlay lost its layer contents", view.Record.ID, name, err)
			}
		}
	}
	assertLayers(source, "upper-before\n")
	first := strings.Repeat("a", 32)
	if _, err := engine.Checkpoint(ctx, "overlay-source", first); err != nil {
		t.Fatal("actual overlay checkpoint", err)
	}
	assertState(source, before)
	branch, forkErr := forkNativeCLI(engine, "overlay-source", first, "overlay-branch")
	logs, _ := filepath.Glob(engine.config.Root + "/forks/overlay-branch/materialized/images/*log")
	restoreLog := ""
	for _, path := range logs {
		data, err := os.ReadFile(path)
		if err == nil {
			if filepath.Base(path) == "restore.log" {
				restoreLog = string(data)
			}
			if err := os.WriteFile("/output/overlay-"+mode+"-"+filepath.Base(path), data, 0600); err != nil {
				t.Error(err)
			}
		}
	}
	if mode == "escaping-link" {
		if forkErr == nil || !strings.Contains(restoreLog, "Can't pin overlay layer beneath captured mount") {
			t.Fatal("restore did not refuse a layer link escaping its captured mount", forkErr, restoreLog)
		}
		assertState(source, before)
		assertLayers(source, "upper-before\n")
		advanced, err := readCaptureActor(source.Runtime.PID, "advance")
		if err != nil || advanced.Counter != 1 {
			t.Fatal("refused restore stopped its original source", err)
		}
		if _, err := engine.Delete(ctx, "overlay-branch"); err == nil ||
			!strings.Contains(err.Error(), "unresolved create") {
			t.Fatal("failed restore discarded its unresolved creation evidence", err)
		}
		t.Log("escaping layer link refused; original mounted contents, RAM and useful work retained")
		return
	}
	if forkErr != nil {
		t.Fatal("actual overlay fork restore", forkErr, restoreLog)
	}
	cleanup("overlay-branch")
	assertState(branch, before)
	assertLayers(branch, "upper-before\n")
	advanced, err := readCaptureActor(branch.Runtime.PID, "advance")
	if err != nil || advanced.Counter != 1 || reflect.DeepEqual(before.Payload, advanced.Payload) {
		t.Fatal("branch cannot independently advance", err)
	}
	if mode != "plain" {
		if err := os.WriteFile(layerFile(branch, "upper-marker"), []byte("branch-after\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	assertState(source, before)
	assertLayers(source, "upper-before\n")
	// Stay below two live sandboxes and remove the original daemon volume
	// before recapturing and restoring the branch.
	if _, err := engine.Delete(ctx, "overlay-source"); err != nil {
		t.Fatal(err)
	}
	second := strings.Repeat("b", 32)
	if _, err := engine.Checkpoint(ctx, "overlay-branch", second); err != nil {
		t.Fatal("recapture restored nested overlay", err)
	}
	descendant, err := forkNativeCLI(engine, "overlay-branch", second, "overlay-descendant")
	if err != nil {
		t.Fatal("fork nested-overlay branch without original source", err)
	}
	cleanup("overlay-descendant")
	assertState(descendant, advanced)
	assertLayers(descendant, "branch-after\n")
	if _, err := engine.Delete(ctx, "overlay-branch"); err != nil {
		t.Fatal(err)
	}
	third := strings.Repeat("c", 32)
	paused, err := pauseNativeCLI(engine, "overlay-descendant", third)
	if err != nil || paused.Record.Pause == nil || paused.Record.Phase != "paused" ||
		paused.Record.Pause.Snapshot.ResidentBytesAfter != 0 {
		t.Fatal("nested overlay pause lacks stopped runtime and snapshot RAM release", err)
	}
	resumed, err := transitionNativeCLI(engine, "resume", "overlay-descendant", third)
	if err != nil {
		t.Fatal("resume nested overlay", err)
	}
	assertState(resumed, advanced)
	assertLayers(resumed, "branch-after\n")
	fourth := strings.Repeat("d", 32)
	if _, err := engine.Checkpoint(ctx, "overlay-descendant", fourth); err != nil {
		t.Fatal("recapture resumed nested overlay", err)
	}
	resumedFork, err := forkNativeCLI(engine, "overlay-descendant", fourth, "overlay-resumed-fork")
	if err != nil {
		t.Fatal("fork resumed nested overlay", err)
	}
	cleanup("overlay-resumed-fork")
	assertState(resumedFork, advanced)
	assertLayers(resumedFork, "branch-after\n")
	changed, err := readCaptureActor(resumedFork.Runtime.PID, "advance")
	if err != nil || changed.Counter != advanced.Counter+1 {
		t.Fatal("resumed-source fork cannot independently continue", err)
	}
	assertState(resumed, advanced)
	t.Log("independent layer/RAM fork, descendant without original source, RAM-releasing pause/resume and resumed-source fork passed")
}
