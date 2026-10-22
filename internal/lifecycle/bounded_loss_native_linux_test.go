//go:build linux

package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"factorybox/internal/storefs"
	"golang.org/x/sys/unix"
)

type boundedLossStatus struct {
	Store     string `json:"store"`
	StoreID   string `json:"store_id"`
	Epoch     string `json:"epoch"`
	BootID    string `json:"boot_id"`
	Lifecycle Config `json:"lifecycle"`
}

type boundedLossPause struct {
	Schema  int               `json:"schema"`
	Passed  bool              `json:"passed"`
	Status  boundedLossStatus `json:"status"`
	Request RecoveryRequest   `json:"request"`
	State   captureActorState `json:"state"`
	Pause   PauseOperation    `json:"pause"`
}

func boundedLossCommand(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/opt/factorybox/factorybox", args...)
	command.WaitDelay = 2 * time.Second
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		return output, fmt.Errorf("delivered CLI %s: %w: %s", args, err, stderr.String())
	}
	return output, nil
}

func boundedLossStatusNow(t *testing.T) boundedLossStatus {
	t.Helper()
	var status boundedLossStatus
	data, err := boundedLossCommand("worker-status")
	if err != nil || json.Unmarshal(data, &status) != nil || status.Lifecycle.BoundedStore == nil || status.Epoch == "" {
		t.Fatal("live bounded worker status", err, string(data))
	}
	if status.Lifecycle.BoundedStore.Image.Bytes != 1<<30 || !status.Lifecycle.BoundedStore.DirectIO {
		t.Fatal("unexpected bounded replacement profile", status.Lifecycle.BoundedStore)
	}
	return status
}

func boundedLossAction(t *testing.T, args ...string) []byte {
	t.Helper()
	data, err := boundedLossCommand(append([]string{"--worker-run-root", "/run/factorybox"}, args...)...)
	if err != nil {
		t.Fatal(err, string(data))
	}
	return data
}

func boundedLossView(t *testing.T, args ...string) View {
	t.Helper()
	var view View
	if err := json.Unmarshal(boundedLossAction(t, args...), &view); err != nil {
		t.Fatal(err)
	}
	return view
}

func boundedLossSave(t *testing.T, name string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile("/output/"+name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func boundedLossActor(t *testing.T, view View, expected captureActorState) {
	t.Helper()
	if view.Runtime == nil || view.Runtime.Status != "running" {
		t.Fatal("missing live restored sandbox", view)
	}
	actual, err := readCaptureActor(view.Runtime.PID, "get")
	if err != nil || !reflect.DeepEqual(actual, expected) {
		t.Fatal("private RAM changed", actual, expected, err)
	}
	marker, daemon := "retained-source-file\n", "private-daemon-before\n"
	if expected.Counter > 0 {
		marker, daemon = "source-after\n", "private-daemon-after\n"
	}
	for path, value := range map[string]string{"/state/marker": marker, "/var/lib/docker/capture-marker": daemon} {
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/root%s", view.Runtime.PID, path))
		if err != nil || string(data) != value {
			t.Fatal("restored private file differs", path, string(data), err)
		}
	}
}

func TestNativeBoundedLossPrepare(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_BOUNDED_LOSS") != "1" {
		t.Skip("owned bounded worker-replacement fixture only")
	}
	status := boundedLossStatusNow(t)
	bundle := status.Store + "/loss-bundle"
	for _, name := range []string{"", "/upper", "/work", "/rootfs"} {
		mode := os.FileMode(0755)
		if name == "" {
			mode = 0700
		}
		if err := os.Mkdir(bundle+name, mode); err != nil {
			t.Fatal(err)
		}
	}
	if out, err := exec.Command("cp", "-a", "/busybox", bundle+"/lower").CombinedOutput(); err != nil {
		t.Fatal(string(out), err)
	}
	binary, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundle+"/lower/bin/capture-actor", binary, 0755); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mount("overlay", bundle+"/rootfs", "overlay", 0, "index=off,lowerdir="+bundle+"/lower,upperdir="+bundle+"/upper,workdir="+bundle+"/work"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := unix.Unmount(bundle+"/rootfs", 0); err != nil {
			t.Error("release exact paused fixture overlay", err)
		}
	})
	if out, err := exec.Command(status.Lifecycle.Runtime, "spec", "--bundle", bundle).CombinedOutput(); err != nil {
		t.Fatal(string(out), err)
	}
	data, err := os.ReadFile(bundle + "/config.json")
	if err != nil {
		t.Fatal(err)
	}
	var spec map[string]any
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatal(err)
	}
	process := spec["process"].(map[string]any)
	process["terminal"], process["cwd"] = false, "/"
	process["args"] = []string{"/bin/sh", "-c", "exec /bin/env FACTORYBOX_CAPTURE_ACTOR=1 FACTORYBOX_CAPTURE_BALLAST=1 /bin/capture-actor -test.run=^TestCaptureActor$ -test.timeout=0 </dev/null >/dev/null 2>&1"}
	process["env"] = []string{"HOME=/root", "PATH=/bin:/sbin:/usr/bin:/usr/sbin"}
	spec["root"].(map[string]any)["readonly"] = false
	spec["linux"].(map[string]any)["resources"] = map[string]any{"pids": map[string]any{"limit": 128}, "devices": []map[string]any{{"allow": true, "access": "rwm"}}}
	data, err = json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundle+"/config.json", data, 0600); err != nil {
		t.Fatal(err)
	}
	view := boundedLossView(t, "start", "--bundle", bundle, "source")
	var before captureActorState
	for deadline := time.Now().Add(10 * time.Second); ; {
		before, err = readCaptureActor(view.Runtime.PID, "get")
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if before.BallastBytes != 32<<20 || before.BallastSHA == "" {
		t.Fatal("missing anonymous RAM ballast", before)
	}
	boundedLossActor(t, view, before)
	snapshot := strings.Repeat("a", 32)
	paused := boundedLossView(t, "pause", "--snapshot-id", snapshot, "source")
	if paused.Record.Phase != "paused" || paused.Record.Pause == nil || paused.Record.Pause.CompletedAt == "" ||
		paused.Record.Pause.CheckpointSHA256 == "" || paused.Record.Pause.After.Anon != 0 ||
		!paused.Record.Pause.Snapshot.FileBacked || paused.Record.Pause.Snapshot.ResidentBytesAfter != 0 {
		t.Fatal("pause was not fully committed and released", paused.Record.Pause)
	}
	request := RecoveryRequest{SourceRoot: status.Lifecycle.Root, SourceID: "source", SnapshotID: snapshot,
		SourceOwner: paused.Record.Owner, CheckpointSHA256: paused.Record.Pause.CheckpointSHA256}
	boundedLossSave(t, "bounded-loss-pause.json", boundedLossPause{Schema: 1, Passed: true, Status: status, Request: request, State: before, Pause: *paused.Record.Pause})
}

func TestNativeBoundedLossRecover(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_BOUNDED_LOSS") != "1" {
		t.Skip("owned bounded worker-replacement fixture only")
	}
	status := boundedLossStatusNow(t)
	data, err := os.ReadFile("/output/bounded-loss-pause.json")
	if err != nil {
		t.Fatal(err)
	}
	var saved boundedLossPause
	if err := json.Unmarshal(data, &saved); err != nil || saved.Schema != 1 || !saved.Passed {
		t.Fatal(err)
	}
	if status.Epoch == saved.Status.Epoch || status.BootID != saved.Status.BootID || status.StoreID != saved.Status.StoreID ||
		status.Lifecycle.Root == saved.Status.Lifecycle.Root || status.Lifecycle.BoundedStore.Image != saved.Status.Lifecycle.BoundedStore.Image {
		t.Fatal("replacement changed store/boot or reused old epoch", status)
	}
	if err := storefs.Verify(*status.Lifecycle.BoundedStore); err != nil {
		t.Fatal(err)
	}
	request := saved.Request
	recoveryArgs := func(target, hash string) []string {
		return []string{"recover-paused", "--source-root", request.SourceRoot,
			"--source", request.SourceID, "--snapshot-id", request.SnapshotID, "--source-owner", request.SourceOwner, "--checkpoint-sha256", hash, target}
	}
	wrong := strings.Repeat("0", 64)
	if _, err := boundedLossCommand(append([]string{"--worker-run-root", "/run/factorybox"}, recoveryArgs("wrong-hash", wrong)...)...); err == nil {
		t.Fatal("wrong committed pause hash admitted")
	}
	if _, err := os.Lstat(status.Lifecycle.Root + "/forks/wrong-hash"); !os.IsNotExist(err) {
		t.Fatal("wrong recovery published backing", err)
	}
	recovered := boundedLossView(t, recoveryArgs("recovered", request.CheckpointSHA256)...)
	boundedLossActor(t, recovered, saved.State)
	replayed := boundedLossView(t, recoveryArgs("recovered", request.CheckpointSHA256)...)
	if !reflect.DeepEqual(replayed, recovered) {
		t.Fatal("exact recovery replay replaced its result")
	}
	t.Cleanup(func() {
		for _, id := range []string{"grandchild", "child", "recovered"} {
			if _, err := os.Stat(status.Lifecycle.Root + "/objects/" + id); os.IsNotExist(err) {
				continue
			}
			if _, err := boundedLossCommand("--worker-run-root", "/run/factorybox", "delete", id); err != nil {
				t.Error("delete exact owned replacement sandbox", id, err)
			}
		}
	})
	snapshot := strings.Repeat("b", 32)
	boundedLossAction(t, "checkpoint", "--snapshot-id", snapshot, "recovered")
	child := boundedLossView(t, "fork", "--source", "recovered", "--snapshot-id", snapshot, "child")
	boundedLossActor(t, child, saved.State)
	if _, err := readCaptureActor(child.Runtime.PID, "advance"); err != nil {
		t.Fatal(err)
	}
	boundedLossActor(t, recovered, saved.State)
	childState, err := readCaptureActor(child.Runtime.PID, "get")
	if err != nil {
		t.Fatal(err)
	}
	if childState.Counter != saved.State.Counter+1 || childState.BallastSHA == saved.State.BallastSHA {
		t.Fatal("child RAM did not advance independently")
	}
	boundedLossActor(t, child, childState)
	boundedLossAction(t, "delete", "recovered")
	childPauseID := strings.Repeat("c", 32)
	paused := boundedLossView(t, "pause", "--snapshot-id", childPauseID, "child")
	if paused.Record.Pause == nil || paused.Record.Pause.After.Anon != 0 || paused.Record.Pause.Snapshot.ResidentBytesAfter != 0 {
		t.Fatal("child pause retained measured RAM")
	}
	child = boundedLossView(t, "resume", "--snapshot-id", childPauseID, "child")
	actual, err := readCaptureActor(child.Runtime.PID, "get")
	if err != nil || !reflect.DeepEqual(actual, childState) {
		t.Fatal("resumed recovered descendant lost RAM", err)
	}
	boundedLossActor(t, child, childState)
	grandSnapshot := strings.Repeat("d", 32)
	boundedLossAction(t, "checkpoint", "--snapshot-id", grandSnapshot, "child")
	grandchild := boundedLossView(t, "fork", "--source", "child", "--snapshot-id", grandSnapshot, "grandchild")
	actual, err = readCaptureActor(grandchild.Runtime.PID, "get")
	if err != nil || !reflect.DeepEqual(actual, childState) {
		t.Fatal("fork of resumed recovered descendant lost RAM", err)
	}
	boundedLossActor(t, grandchild, childState)
	boundedLossAction(t, "delete", "child")
	continued, err := readCaptureActor(grandchild.Runtime.PID, "advance")
	if err != nil || continued.Counter != childState.Counter+1 {
		t.Fatal("grandchild cannot work after source deletion", err)
	}
	boundedLossAction(t, "delete", "grandchild")
	boundedLossSave(t, "bounded-loss-recovered.json", map[string]any{"schema": 1, "passed": true, "production_qualified": false,
		"status": status, "same_exact_image": true, "new_epoch": true, "wrong_hash_refused": true, "exact_replay": true,
		"restored_private_ram_bytes": saved.State.BallastBytes, "source_pause": saved.Pause, "descendant_pause": paused.Record.Pause,
		"private_files_preserved": true, "independent_fork": true, "fork_of_resumed_descendant": true,
		"useful_after_source_delete": true, "exact_sandbox_cleanup": true})
}
