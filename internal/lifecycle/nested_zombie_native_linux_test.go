//go:build linux

package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

// A nested PID-namespace init can retain a dead child whose image has no ids
// record. The real parent must still reap its original own-namespace PID and
// exit status independently after a fork and a fork of that fork.
func TestCaptureNestedZombieNamespaceNative(t *testing.T) {
	testCaptureNestedZombieNamespace(t, "make-nested-zombie", 47, "nested-zombie.json")
}

func TestCaptureNestedZombieHighExitNamespaceNative(t *testing.T) {
	testCaptureNestedZombieNamespace(t, "make-nested-zombie-high-exit", 201, "nested-zombie-high-exit.json")
}

func testCaptureNestedZombieNamespace(t *testing.T, request string, exitStatus int, output string) {
	t.Helper()
	if os.Getenv("FACTORYBOX_NATIVE_NAMESPACES") != "1" {
		t.Skip("requires the bounded native namespace fixture")
	}
	engine, bundle := nativeEngine(t,
		"/bin/env FACTORYBOX_CAPTURE_ACTOR=1 FACTORYBOX_CAPTURE_BALLAST=1 "+
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
	source, err := engine.Start(ctx, "zombie-source", bundle)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"zombie-source", "zombie-branch", "zombie-descendant"} {
		t.Cleanup(func() {
			record, err := engine.read(id)
			if errors.Is(err, os.ErrNotExist) {
				return
			}
			if err != nil {
				t.Error("read exact owned nested-zombie runtime", id, err)
				return
			}
			if record.Start.Attempted && record.RuntimeCreated == "" {
				t.Log("retain unresolved restore for the outer owned failure archive", id)
				return
			}
			if _, err := engine.Delete(ctx, id); err != nil {
				t.Error("delete exact owned nested-zombie runtime", id, err)
			}
		})
	}
	var original captureActorState
	for deadline := time.Now().Add(5 * time.Second); ; {
		original, err = readCaptureActor(source.Runtime.PID, "get")
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("nested-zombie source is not ready", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	created, err := readCaptureActor(source.Runtime.PID, request)
	if err != nil || created.NestedPID <= 1 || created.TimeMode != "nested-zombie" ||
		created.BallastSHA != original.BallastSHA || created.BallastBytes != 32<<20 {
		t.Fatal("nested PID init was not created with original RAM intact", err)
	}
	var before captureActorState
	for deadline := time.Now().Add(5 * time.Second); ; {
		before, err = readCaptureActorAt(source.Runtime.PID, "/zombie-state", "get")
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("nested init did not retain its exited child", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if before.ZombiePID <= 1 || !before.ZombieWaitable || before.ZombieReaped ||
		before.BallastBytes != 32<<20 || len(before.Payload) != 8192 {
		t.Fatal("source lacks its actual waitable zombie and private RAM", before)
	}
	first := strings.Repeat("7", 32)
	if _, err := engine.Checkpoint(ctx, "zombie-source", first); err != nil {
		t.Fatal("capture the actual nested zombie", err)
	}
	branch, err := forkNativeCLI(engine, "zombie-source", first, "zombie-branch")
	if err != nil {
		t.Fatal("fork must restore the nested parent's original waitable child", err)
	}
	cloned, err := readCaptureActorAt(branch.Runtime.PID, "/zombie-state", "get")
	if err != nil || !reflect.DeepEqual(cloned, before) {
		t.Fatal("fork changed the zombie PID, waitability or parent RAM", err)
	}
	reapedSource, err := readCaptureActorAt(source.Runtime.PID, "/zombie-state", "reap-zombie")
	if err != nil || !reapedSource.ZombieReaped || reapedSource.ZombieWaitable ||
		reapedSource.ZombiePID != before.ZombiePID || reapedSource.ZombieExit != exitStatus {
		t.Fatal("source cannot reap its original own-namespace PID and status", err)
	}
	stillWaiting, err := readCaptureActorAt(branch.Runtime.PID, "/zombie-state", "get")
	if err != nil || !reflect.DeepEqual(stillWaiting, before) {
		t.Fatal("source reaping consumed the independent fork's child", err)
	}
	second := strings.Repeat("8", 32)
	if _, err := engine.Checkpoint(ctx, "zombie-branch", second); err != nil {
		t.Fatal("recapture the restored nested zombie", err)
	}
	if _, err := engine.Delete(ctx, "zombie-source"); err != nil {
		t.Fatal("delete original source before the descendant restore", err)
	}
	descendant, err := forkNativeCLI(engine, "zombie-branch", second, "zombie-descendant")
	if err != nil {
		t.Fatal("fork of fork must preserve the original nested child", err)
	}
	for _, view := range []View{descendant, branch} {
		waiting, err := readCaptureActorAt(view.Runtime.PID, "/zombie-state", "get")
		if err != nil || !reflect.DeepEqual(waiting, before) {
			t.Fatal("independent descendant lost the original zombie or RAM", err)
		}
		reaped, err := readCaptureActorAt(view.Runtime.PID, "/zombie-state", "reap-zombie")
		if err != nil || !reaped.ZombieReaped || reaped.ZombieWaitable ||
			reaped.ZombiePID != before.ZombiePID || reaped.ZombieExit != exitStatus ||
			reaped.BallastSHA != before.BallastSHA || !reflect.DeepEqual(reaped.Payload, before.Payload) {
			t.Fatal("restored real parent cannot independently reap its original child", err)
		}
	}
	result, err := json.Marshal(map[string]any{
		"passed": true, "original_child_pid": before.ZombiePID, "exit_status": exitStatus,
		"source_and_branches_reaped_independently": true, "fork_of_fork": true,
		"original_deleted_before_descendant": true, "private_ram_per_parent": 32 << 20,
		"scope": "One nested PID namespace, retained exited child, original parent wait PID/status, " +
			"complete parent payload and independent reaping across checkpoint/fork/recapture/descendant.",
	})
	if err != nil || os.WriteFile("/output/"+output, append(result, '\n'), 0600) != nil {
		t.Fatal("save complete native nested-zombie proof", err)
	}
}
