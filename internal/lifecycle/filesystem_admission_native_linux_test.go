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

func TestFilesystemAdmissionNative(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_FILESYSTEM_ADMISSION") != "1" {
		t.Skip("requires its owned native filesystem-admission fixture")
	}
	engine, bundle := nativeEngine(t,
		"/bin/env FACTORYBOX_CAPTURE_ACTOR=1 /bin/capture-actor -test.run=^TestCaptureActor$ -test.timeout=0")
	binary, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundle+"/lower/bin/capture-actor", binary, 0755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(bundle + "/config.json")
	if err != nil {
		t.Fatal(err)
	}
	var spec map[string]any
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatal(err)
	}
	// This trusted no-op hook executes successfully on start. Hooks remain
	// outside capture admission; rejection must not interrupt the source.
	spec["hooks"] = map[string]any{
		"poststart": []map[string]any{{"path": "/bin/true", "args": []string{"/bin/true"}}},
	}
	data, err = json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundle+"/config.json", data, 0600); err != nil {
		t.Fatal(err)
	}
	engine.config.Criu, engine.config.CriuSHA256 = "/out/criu", os.Getenv("FACTORYBOX_CRIU_SHA256")
	engine.config.ManagerRoot = "/var/lib/sysbox/manager"
	ctx := context.Background()
	source, err := engine.Start(ctx, "rejected-hook", bundle)
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
			t.Log("rejected source remained held; outer fixture retains and cleans it", err)
			return
		}
		if _, err := engine.Delete(ctx, source.Record.ID); err != nil {
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
			t.Fatal("original actor did not become ready", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	command := func(operation, snapshot string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "/factorybox", "--root", engine.config.Root,
			"--runtime", engine.config.Runtime, "--runtime-sha256", engine.config.RuntimeSHA256,
			"--criu", engine.config.Criu, "--criu-sha256", engine.config.CriuSHA256,
			"--manager-root", engine.config.ManagerRoot,
			operation, "--snapshot-id", snapshot, source.Record.ID)
		output, err := cmd.CombinedOutput()
		if err == nil || !strings.Contains(string(output), "hooks=1") {
			return fmt.Errorf("capture accepted its unsupported hook or failed elsewhere: %v: %s", err, output)
		}
		return nil
	}
	for index, operation := range []string{"checkpoint", "pause"} {
		snapshot := strings.Repeat(fmt.Sprint(index+5), 32)
		if err := command(operation, snapshot); err != nil {
			t.Fatal(err)
		}
		current, err := engine.Inspect(ctx, source.Record.ID)
		if err != nil || current.Runtime == nil || current.Runtime.Status != "running" ||
			current.Runtime.PID != source.Runtime.PID || current.Runtime.Created != source.Runtime.Created ||
			current.Record.Phase != "running" {
			t.Fatalf("%s preflight rejection did not preserve its original running process: %+v: %v",
				operation, current.Runtime, err)
		}
		after, err := readCaptureActor(source.Runtime.PID, "get")
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatal("filesystem rejection changed original RAM", err)
		}
		relative, _ := checkpointPath(source.Record.ID, snapshot)
		for _, name := range []string{"images", "checkpoint.json", "capture-result.json",
			"guard/guard-capture-entered.json", "guard/capture-worker-intent.json"} {
			if _, err := os.Stat(filepath.Join(engine.config.Root, relative, name)); !os.IsNotExist(err) {
				t.Fatalf("preflight entered capture or published backing: %s: %v", name, err)
			}
		}
		if err := command(operation, snapshot); err != nil {
			t.Fatal("failed immutable operation did not replay its rejection", err)
		}
		if index == 0 {
			path := filepath.Join(engine.config.Root, relative, "admission-rejected.json")
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var changed map[string]any
			if err := json.Unmarshal(original, &changed); err != nil {
				t.Fatal(err)
			}
			changed["kind"] = "foreign"
			forged, _ := json.Marshal(changed)
			if err := os.WriteFile(path, forged, 0600); err != nil {
				t.Fatal(err)
			}
			_, rejected := engine.Checkpoint(ctx, source.Record.ID, snapshot)
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			if rejected == nil || !strings.Contains(rejected.Error(), "admission rejection differs") {
				t.Fatal("changed rejection kind was accepted or failed elsewhere", rejected)
			}
		}
		advanced, err := readCaptureActor(source.Runtime.PID, "advance")
		expected := before
		expected.Payload = append([]byte(nil), before.Payload...)
		expected.Payload[0] ^= 0xff
		expected.Counter++
		if err != nil || !reflect.DeepEqual(advanced, expected) {
			t.Fatal("source cannot continue useful work after preflight rejection and replay", err)
		}
		before = advanced
	}
	t.Log("checkpoint/pause rejected hooks before capture; original PID, RAM and useful work survived immutable replay")
}
