//go:build linux

package lifecycle

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"factorybox/internal/sysboxcapture"
	"golang.org/x/sys/unix"
)

type captureActorState struct {
	Payload []byte `json:"payload"`
	Counter int    `json:"counter"`
}

func TestCaptureActor(t *testing.T) {
	if os.Getenv("FACTORYBOX_CAPTURE_ACTOR") != "1" {
		t.Skip("native capture workload only")
	}
	state := captureActorState{Payload: make([]byte, 8192)}
	if _, err := rand.Read(state.Payload); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir("/state", 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("/state/marker", []byte("retained-source-file\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link("/state/marker", "/state/marker-hardlink"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("marker", "/state/marker-link"); err != nil {
		t.Fatal(err)
	}
	if err := unix.Setxattr("/state/marker", "user.factorybox", []byte("captured-xattr"), 0); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo("/state/fifo", 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("/var/lib/docker/capture-marker", []byte("private-daemon-before\n"), 0600); err != nil {
		t.Fatal(err)
	}
	server, err := net.Listen("unix", "/state/control.sock")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	var mutex sync.Mutex
	for {
		client, err := server.Accept()
		if err != nil {
			t.Fatal(err)
		}
		func() {
			defer client.Close()
			var request string
			if err := json.NewDecoder(io.LimitReader(client, 1024)).Decode(&request); err != nil {
				t.Error(err)
				return
			}
			mutex.Lock()
			defer mutex.Unlock()
			switch request {
			case "advance":
				state.Payload[0] ^= 0xff
				state.Counter++
				if err := os.WriteFile("/state/marker", []byte("source-after\n"), 0600); err != nil {
					t.Error(err)
				}
				if err := os.WriteFile("/var/lib/docker/capture-marker", []byte("private-daemon-after\n"), 0600); err != nil {
					t.Error(err)
				}
			case "get":
			default:
				t.Error("unknown RAM actor request")
				return
			}
			if err := json.NewEncoder(client).Encode(state); err != nil {
				t.Error(err)
			}
		}()
	}
}

func readCaptureActor(pid int, request string) (captureActorState, error) {
	var state captureActorState
	client, err := net.DialTimeout("unix", fmt.Sprintf("/proc/%d/root/state/control.sock", pid), time.Second)
	if err != nil {
		return state, err
	}
	defer client.Close()
	if err := client.SetDeadline(time.Now().Add(time.Second)); err != nil {
		return state, err
	}
	if err := json.NewEncoder(client).Encode(request); err != nil {
		return state, err
	}
	// Wait for the workload to close its connection. No external control
	// socket may remain open when CRIU captures the source.
	data, err := io.ReadAll(io.LimitReader(client, 64<<10))
	if err != nil {
		return state, err
	}
	return state, json.Unmarshal(data, &state)
}

func TestRetainedCaptureNative(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_RETAINED_CAPTURE") != "1" {
		t.Skip("requires the bounded capture fixture with rebuilt CRIU")
	}
	engine, bundle := nativeEngine(t, "/bin/env FACTORYBOX_CAPTURE_ACTOR=1 /bin/capture-actor -test.run=^TestCaptureActor$ -test.timeout=0")
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
	view, err := engine.Start(ctx, "captured", bundle)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := engine.Delete(ctx, "captured"); err != nil {
			t.Errorf("delete owned capture source: %v", err)
		}
	})
	hostPanic, err := os.ReadFile("/proc/sys/kernel/panic")
	if err != nil {
		t.Fatal(err)
	}
	writeVirtual := func(value string) {
		t.Helper()
		control, err := engine.lock()
		if err != nil {
			t.Fatal(err)
		}
		defer control.Close()
		if _, err := engine.command(ctx, control, "exec", view.Record.RuntimeID,
			"/bin/sh", "-c", "printf '"+value+"\\n' > /proc/sys/kernel/panic"); err != nil {
			t.Fatal("write private Sysbox virtual setting", err)
		}
	}
	writeVirtual("17")
	deadline := time.Now().Add(5 * time.Second)
	var before captureActorState
	for {
		before, err = readCaptureActor(view.Runtime.PID, "get")
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("RAM actor not ready", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(before.Payload) != 8192 || before.Counter != 0 {
		t.Fatal("RAM actor did not initialize its private payload")
	}
	snapshotID := strings.Repeat("d", 32)
	generation := engine.config.Root + "/objects/captured/captures/" + snapshotID
	checkpoint := func() (Checkpoint, error) {
		command := exec.Command("/factorybox", "--root", engine.config.Root,
			"--runtime", engine.config.Runtime, "--runtime-sha256", engine.config.RuntimeSHA256,
			"--criu", engine.config.Criu, "--criu-sha256", engine.config.CriuSHA256,
			"--manager-root", engine.config.ManagerRoot, "checkpoint", "--snapshot-id", snapshotID, "captured")
		output, err := command.CombinedOutput()
		var result Checkpoint
		if err != nil {
			return result, fmt.Errorf("%w: %s", err, output)
		}
		if err := json.Unmarshal(output, &result); err != nil {
			return result, fmt.Errorf("checkpoint output: %w: %s", err, output)
		}
		return result, nil
	}
	started := time.Now()
	saved, err := checkpoint()
	captureElapsed := time.Since(started)
	if err != nil {
		t.Fatal(err)
	}
	if saved.ID != snapshotID || saved.SourcePID != view.Runtime.PID || saved.ReleasedAt == "" || len(saved.Files) == 0 {
		t.Fatal("public checkpoint did not publish its exact generation and source release")
	}
	started = time.Now()
	replayed, err := checkpoint()
	replayElapsed := time.Since(started)
	if err != nil || !replayedCheckpoint(saved, replayed) {
		t.Fatal("checkpoint replay created another generation or changed outcome", err)
	}
	fsStatePath := generation + "/sysbox-state.json"
	fsState, err := os.ReadFile(fsStatePath)
	if err != nil {
		t.Fatal(err)
	}
	var virtual struct {
		Version int               `json:"version"`
		Source  string            `json:"source"`
		Data    map[string][]byte `json:"data"`
	}
	if err := json.Unmarshal(fsState, &virtual); err != nil || virtual.Version != 1 ||
		virtual.Source != view.Record.RuntimeID ||
		strings.TrimSpace(string(virtual.Data["/proc/sys/kernel/panic"])) != "17" {
		t.Fatalf("FUSE snapshot did not capture its source's virtual value: %v", err)
	}
	images := generation + "/images"
	files, err := filepath.Glob(images + "/pages-*.img")
	if err != nil || len(files) == 0 {
		t.Fatalf("actual captured memory pages absent: %v", err)
	}
	hashes := map[string]string{}
	payloadFound := false
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		hashes[path] = sum(data)
		payloadFound = payloadFound || bytes.Contains(data, before.Payload)
	}
	if !payloadFound {
		t.Fatal("complete ordinary page writer did not contain the source's private RAM payload")
	}
	for path, expected := range map[string]string{
		"/filesystem/upper/state/marker":            "retained-source-file\n",
		"/filesystem/volumes/docker/capture-marker": "private-daemon-before\n",
	} {
		data, err := os.ReadFile(generation + path)
		if err != nil || string(data) != expected {
			t.Fatalf("filesystem checkpoint content differs: %s: %v", path, err)
		}
	}
	one, err := os.Stat(generation + "/filesystem/upper/state/marker")
	if err != nil {
		t.Fatal(err)
	}
	two, err := os.Stat(generation + "/filesystem/upper/state/marker-hardlink")
	if err != nil || !os.SameFile(one, two) {
		t.Fatal("captured hardlink relationship lost", err)
	}
	if target, err := os.Readlink(generation + "/filesystem/upper/state/marker-link"); err != nil || target != "marker" {
		t.Fatal("captured symlink target differs", err)
	}
	writeVirtual("29")
	preserved, err := os.ReadFile(fsStatePath)
	if err != nil || !bytes.Equal(preserved, fsState) {
		t.Fatal("source virtual-setting mutation changed the saved checkpoint", err)
	}
	if err := os.WriteFile(fsStatePath, append(append([]byte(nil), fsState...), ' '), 0600); err != nil {
		t.Fatal(err)
	}
	_, corruptErr := checkpoint()
	if err := os.WriteFile(fsStatePath, fsState, 0600); err != nil {
		t.Fatal(err)
	}
	if corruptErr == nil {
		t.Fatal("checkpoint replay accepted corrupted metadata")
	}
	readyPath := generation + "/checkpoint.json"
	readyBytes, err := os.ReadFile(readyPath)
	if err != nil {
		t.Fatal(err)
	}
	checkCorruptManifest := func(name string, change func(Checkpoint) Checkpoint) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			original, err := engine.readCheckpoint("objects/captured/captures/"+snapshotID, "checkpoint.json")
			if err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(change(original))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(readyPath, data, 0600); err != nil {
				t.Fatal(err)
			}
			_, rejected := checkpoint()
			if err := os.WriteFile(readyPath, readyBytes, 0600); err != nil {
				t.Fatal(err)
			}
			if rejected == nil {
				t.Fatal("checkpoint accepted a changed manifest")
			}
		})
	}
	checkCorruptManifest("filesystem-root-omission", func(value Checkpoint) Checkpoint {
		value.Filesystem.Roots = value.Filesystem.Roots[:2]
		return value
	})
	checkCorruptManifest("capture-timestamp-change", func(value Checkpoint) Checkpoint {
		value.CapturedAt = now()
		return value
	})
	t.Run("oversized-record-and-symlink", func(t *testing.T) {
		if err := os.WriteFile(readyPath, append(append([]byte(nil), readyBytes...), bytes.Repeat([]byte(" "), checkpointManifestLimit)...), 0600); err != nil {
			t.Fatal(err)
		}
		_, oversized := checkpoint()
		if err := os.Rename(readyPath, readyPath+".oversized"); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(readyPath+".oversized", readyPath); err != nil {
			t.Fatal(err)
		}
		_, symlink := checkpoint()
		if err := os.Remove(readyPath); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(readyPath, readyBytes, 0600); err != nil {
			t.Fatal(err)
		}
		if oversized == nil || symlink == nil {
			t.Fatal("checkpoint accepted an oversized or symlinked record", oversized, symlink)
		}
	})
	control, err := engine.lock()
	if err != nil {
		t.Fatal(err)
	}
	_, liveExportErr := engine.command(ctx, control, "factorybox-fs-snapshot",
		"--file", fsStatePath, "--owner", view.Record.Owner, view.Record.RuntimeID)
	control.Close()
	if liveExportErr == nil {
		t.Fatal("unfrozen live source exported as an atomic snapshot")
	}
	afterHostPanic, err := os.ReadFile("/proc/sys/kernel/panic")
	if err != nil || !bytes.Equal(afterHostPanic, hostPanic) {
		t.Fatal("private virtual setting changed the worker kernel value", err)
	}
	after, err := readCaptureActor(view.Runtime.PID, "advance")
	expected := append([]byte(nil), before.Payload...)
	expected[0] ^= 0xff
	if err != nil || after.Counter != 1 || !bytes.Equal(after.Payload, expected) {
		t.Fatalf("original RAM process did not continue independently: %v", err)
	}
	for path, expected := range map[string]string{
		"/filesystem/upper/state/marker":            "retained-source-file\n",
		"/filesystem/volumes/docker/capture-marker": "private-daemon-before\n",
	} {
		data, err := os.ReadFile(generation + path)
		if err != nil || string(data) != expected {
			t.Fatalf("source filesystem mutation changed checkpoint: %s: %v", path, err)
		}
	}
	for path, expected := range hashes {
		data, err := os.ReadFile(path)
		if err != nil || sum(data) != expected {
			t.Fatalf("source mutation changed captured page backing: %s: %v", path, err)
		}
	}
	observed, err := engine.Inspect(ctx, "captured")
	if err != nil || observed.Runtime == nil || observed.Runtime.PID != view.Runtime.PID {
		t.Fatalf("capture replaced or lost its source: %+v: %v", observed, err)
	}
	// Simulate losing only the final publication after a fully persisted
	// capture and source release. Recovery must reuse the captured result,
	// even though the source's RAM, files and virtual state have advanced.
	if err := os.Rename(readyPath, generation+"/checkpoint-original.json"); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(generation+"/capture-result.json", generation+"/capture-result-hidden.json"); err != nil {
		t.Fatal(err)
	}
	_, incomplete := checkpoint()
	if err := os.Rename(generation+"/capture-result-hidden.json", generation+"/capture-result.json"); err != nil {
		t.Fatal(err)
	}
	if incomplete == nil || !strings.Contains(incomplete.Error(), "refusing a second capture") {
		t.Fatal("incomplete generation silently repeated capture", incomplete)
	}
	guardOutcome := generation + "/guard/guard-outcome.json"
	if err := os.Rename(guardOutcome, guardOutcome+".hidden"); err != nil {
		t.Fatal(err)
	}
	_, unresolved := checkpoint()
	if err := os.Rename(guardOutcome+".hidden", guardOutcome); err != nil {
		t.Fatal(err)
	}
	if unresolved == nil || !strings.Contains(unresolved.Error(), "guard outcome is unresolved") {
		t.Fatal("checkpoint recovery raced an unresolved guard", unresolved)
	}
	recovered, err := checkpoint()
	if err != nil || recovered.CapturedAt != saved.CapturedAt || recovered.SourcePID != saved.SourcePID {
		t.Fatal("completed capture was repeated or failed publication recovery", err)
	}
	again, err := checkpoint()
	if err != nil || !replayedCheckpoint(recovered, again) {
		t.Fatal("recovered checkpoint replay changed its result", err)
	}
	// A second lost publication must reuse the durable recovery outcome too.
	if err := os.Rename(readyPath, generation+"/checkpoint-first-recovery.json"); err != nil {
		t.Fatal(err)
	}
	again, err = checkpoint()
	if err != nil || !replayedCheckpoint(recovered, again) {
		t.Fatal("repeated publication recovery changed its result", err)
	}
	if _, err := engine.Delete(ctx, "captured"); err != nil {
		t.Fatal("delete original after complete capture", err)
	}
	afterDelete, err := checkpoint()
	if err != nil || !replayedCheckpoint(recovered, afterDelete) {
		t.Fatal("ready checkpoint depended on its original runtime", err)
	}
	if err := os.Rename(readyPath, generation+"/checkpoint-before-delete.json"); err != nil {
		t.Fatal(err)
	}
	afterDelete, err = checkpoint()
	if err != nil || !replayedCheckpoint(recovered, afterDelete) {
		t.Fatal("completed publication recovery depended on its original runtime", err)
	}
	for path, expected := range map[string]string{
		"/filesystem/upper/state/marker":            "retained-source-file\n",
		"/filesystem/volumes/docker/capture-marker": "private-daemon-before\n",
	} {
		data, err := os.ReadFile(generation + path)
		if err != nil || string(data) != expected {
			t.Fatalf("source deletion removed captured filesystem state: %s: %v", path, err)
		}
	}
	t.Run("independent-filesystem-materialization", func(t *testing.T) {
		leftPath := filepath.Dir(generation) + "/" + strings.Repeat("a", 32)
		rightPath := filepath.Dir(generation) + "/" + strings.Repeat("b", 32)
		left, err := sysboxcapture.MaterializeFilesystemSnapshot(ctx, generation, leftPath, recovered.Filesystem)
		if err != nil {
			t.Fatal("materialize first filesystem after original runtime deletion", err)
		}
		right, err := sysboxcapture.MaterializeFilesystemSnapshot(ctx, generation, rightPath, recovered.Filesystem)
		if err != nil {
			t.Fatal("materialize second independent filesystem", err)
		}
		if err := sysboxcapture.VerifyFilesystemSnapshot(ctx, rightPath, right); err != nil {
			t.Fatal(err)
		}
		for _, tree := range left.Roots {
			if tree.Saved != leftPath+"/filesystem/"+tree.Name {
				t.Fatal("materialized filesystem names a shared saved root", tree.Saved)
			}
		}
		marker := "/filesystem/upper/state/marker"
		leftMarker, err := os.Stat(leftPath + marker)
		if err != nil {
			t.Fatal(err)
		}
		rightMarker, err := os.Stat(rightPath + marker)
		if err != nil || os.SameFile(leftMarker, rightMarker) {
			t.Fatal("materialized siblings share a writable inode", err)
		}
		leftLink, err := os.Stat(leftPath + marker + "-hardlink")
		if err != nil || !os.SameFile(leftMarker, leftLink) {
			t.Fatal("materialization broke an internal hardlink", err)
		}
		for path, value := range map[string]string{
			marker: "left-private-file\n",
			"/filesystem/volumes/docker/capture-marker": "left-private-volume\n",
		} {
			if err := os.WriteFile(leftPath+path, []byte(value), 0600); err != nil {
				t.Fatal(err)
			}
		}
		if err := sysboxcapture.VerifyFilesystemSnapshot(ctx, leftPath, left); err == nil {
			t.Fatal("materialized target mutation escaped its saved digest")
		}
		if err := sysboxcapture.VerifyFilesystemSnapshot(ctx, generation, recovered.Filesystem); err != nil {
			t.Fatal("target mutation changed its parent checkpoint", err)
		}
		if err := sysboxcapture.VerifyFilesystemSnapshot(ctx, rightPath, right); err != nil {
			t.Fatal("target mutation changed its sibling", err)
		}
		if _, err := sysboxcapture.MaterializeFilesystemSnapshot(ctx, generation, leftPath, recovered.Filesystem); err == nil {
			t.Fatal("materialization replaced an existing target")
		}
		if _, err := sysboxcapture.MaterializeFilesystemSnapshot(ctx, generation, generation+"/nested", recovered.Filesystem); err == nil {
			t.Fatal("materialization wrote inside its source checkpoint")
		}
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		canceledPath := filepath.Dir(generation) + "/" + strings.Repeat("c", 32)
		if _, err := sysboxcapture.MaterializeFilesystemSnapshot(canceled, generation, canceledPath, recovered.Filesystem); err == nil {
			t.Fatal("materialization ignored cancellation")
		}
		if _, err := os.Lstat(canceledPath); !os.IsNotExist(err) {
			t.Fatal("canceled materialization created a partial target", err)
		}
		t.Log("two complete filesystem siblings survive original-runtime deletion; target writes change neither checkpoint nor sibling; internal hardlinks and separate inodes verified")
		t.Run("restore-without-original-and-fork-again", func(t *testing.T) {
			restored := restoreMaterializedNative(t, engine, view.Record, generation, rightPath, "restored")
			assertRestoredNative(t, engine, restored, before)
			forkFromRestoredNative(t, engine, restored, before)
			if err := sysboxcapture.VerifyFilesystemSnapshot(ctx, generation, recovered.Filesystem); err != nil {
				t.Fatal("restored descendant changed the original checkpoint", err)
			}
			t.Log("restored private RAM, virtual state and Docker-volume file after original runtime deletion; recaptured restored source and independently restored/mutated its descendant")
		})
	})
	afterHostPanic, err = os.ReadFile("/proc/sys/kernel/panic")
	if err != nil || !bytes.Equal(afterHostPanic, hostPanic) {
		t.Fatal("restored descendant's virtual-setting write changed the worker kernel", err)
	}
	t.Logf("actual capture under %s retained PID %d, included the 8192-byte private RAM payload in %d page files and preserved RAM, virtual state, overlay files and a managed Docker-volume file after source mutation; worker kernel value unchanged; independent restore and descendant checks passed; public fork/pause/resume remain separate gates",
		view.Record.RuntimeID, view.Runtime.PID, len(files))
	t.Logf("individual CLI observation: checkpoint=%s integrity-verified replay=%s; not a fork/resume or p95 benchmark", captureElapsed, replayElapsed)
}

func replayedCheckpoint(a, b Checkpoint) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return bytes.Equal(left, right)
}
