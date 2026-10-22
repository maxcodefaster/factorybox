//go:build linux && factorybox_worker_probe

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

// This fixture drives the public recovery API across complete Docker worker
// loss. It does not substitute for consumer or production worker qualification.
type coldPauseProof struct {
	Owner    string            `json:"owner"`
	Config   Config            `json:"config"`
	Source   Record            `json:"source"`
	Snapshot string            `json:"snapshot"`
	Before   captureActorState `json:"before"`
}

func TestWorkerRestartProbeNative(t *testing.T) {
	ctx := context.Background()
	owner := os.Getenv("FACTORYBOX_BASE_WORKER_OWNER")
	if len(owner) != 32 {
		t.Fatal("requires an owned cold-worker fixture")
	}
	const proofPath = "/var/lib/sysbox/cold-pause.json"
	if os.Getenv("FACTORYBOX_BASE_WORKER_PHASE") == "prepare" {
		engine, bundle := nativeEngine(t, "/bin/env FACTORYBOX_CAPTURE_ACTOR=1 FACTORYBOX_CAPTURE_BALLAST=1 /bin/capture-actor -test.run=^TestCaptureActor$ -test.timeout=0")
		binary, err := os.ReadFile(os.Args[0])
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(bundle+"/lower/bin/capture-actor", binary, 0755); err != nil {
			t.Fatal(err)
		}
		if os.Getenv("FACTORYBOX_NATIVE_WORKER_SUPERVISOR") == "1" {
			engine.Close()
			engine, err = Open(supervisedFixtureConfig(t))
			if err != nil {
				t.Fatal(err)
			}
			defer engine.Close()
		} else {
			engine.config.Criu, engine.config.CriuSHA256 = "/out/criu", os.Getenv("FACTORYBOX_CRIU_SHA256")
			engine.config.ManagerRoot = "/var/lib/sysbox/manager"
		}
		prepareNativeBase(t, engine, bundle)
		source, err := engine.Start(ctx, "cold-source", bundle)
		if err != nil {
			t.Fatal(err)
		}
		var before captureActorState
		for deadline := time.Now().Add(5 * time.Second); ; {
			before, err = readCaptureActor(source.Runtime.PID, "get")
			if err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal(err)
			}
			time.Sleep(10 * time.Millisecond)
		}
		if before.BallastBytes != 32<<20 || before.BallastSHA == "" {
			t.Fatal("cold-worker source lacks a complete anonymous RAM checksum")
		}
		snapshot := strings.Repeat("e", 32)
		paused, err := pauseNativeCLI(engine, "cold-source", snapshot)
		if err != nil || paused.Record.Phase != "paused" || paused.Record.Pause == nil ||
			paused.Record.Pause.CompletedAt == "" || paused.Record.Pause.After.Anon != 0 ||
			paused.Record.Pause.Snapshot.ResidentBytesAfter != 0 || !digest.MatchString(paused.Record.Pause.CheckpointSHA256) {
			t.Fatal("public pause lacks its committed stopped state and snapshot-RAM release", err)
		}
		proof := coldPauseProof{Owner: owner, Config: engine.config, Source: paused.Record, Snapshot: snapshot, Before: before}
		file, err := os.OpenFile(proofPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.NewEncoder(file).Encode(proof); err != nil {
			t.Fatal(err)
		}
		if err := file.Sync(); err != nil {
			t.Fatal(err)
		}
		file.Close()
		parent, err := os.Open(filepath.Dir(proofPath))
		if err != nil {
			t.Fatal(err)
		}
		if err := parent.Sync(); err != nil {
			t.Fatal(err)
		}
		parent.Close()
		fmt.Println("FACTORYBOX_PAUSE_COMMITTED=" + owner)
		if os.Getenv("FACTORYBOX_BASE_WORKER_ABRUPT") == "1" {
			select {}
		}
		return
	}
	if os.Getenv("FACTORYBOX_BASE_WORKER_PHASE") != "recover" {
		t.Fatal("unknown worker probe phase")
	}
	data, err := os.ReadFile(proofPath)
	var proof coldPauseProof
	if err != nil || json.Unmarshal(data, &proof) != nil || proof.Owner != owner ||
		proof.Source.Phase != "paused" || proof.Source.Pause == nil || proof.Source.Pause.CompletedAt == "" {
		t.Fatal("missing exact completed pause", err)
	}
	old, err := Open(proof.Config)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	generation, err := checkpointPath(proof.Source.ID, proof.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := old.publishedCheckpointMetadata(ctx, proof.Source, proof.Snapshot, generation)
	if err != nil || !saved.SourceStopped {
		t.Fatal("old file-backed checkpoint metadata or RAM changed", err)
	}
	config := proof.Config
	config.Root = "/var/lib/sysbox/cold-new-controller"
	if os.Getenv("FACTORYBOX_NATIVE_WORKER_SUPERVISOR") == "1" {
		config = supervisedFixtureConfig(t)
		t.Run("reject-stale-worker-readiness", func(t *testing.T) {
			path := "/run/factorybox/worker.json"
			current, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			stale, err := os.ReadFile(filepath.Dir(proof.Config.Root) + "/worker.json")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, stale, 0600); err != nil {
				t.Fatal(err)
			}
			_, rejected := exec.Command("/factorybox", "worker-status").CombinedOutput()
			if err := os.WriteFile(path, current, 0600); err != nil {
				t.Fatal(err)
			}
			if rejected == nil {
				t.Fatal("stale ready marker authorized a recreated worker")
			}
		})
	}
	fresh, err := Open(config)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	identity, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	request := RecoveryRequest{SourceRoot: old.config.Root, SourceID: proof.Source.ID,
		SnapshotID: proof.Snapshot, SourceOwner: proof.Source.Owner, CheckpointSHA256: sum(identity)}
	checkRecoveryRejectionsNative(t, fresh, old, generation, saved, request)
	restored, err := recoverNativeCLI(fresh, request, "cold-restored")
	if err != nil {
		t.Fatal("actual independent restore in a fresh worker", err)
	}
	actual, err := readCaptureActor(restored.Runtime.PID, "get")
	if err != nil || !reflect.DeepEqual(actual, proof.Before) {
		t.Fatal("fresh worker lost complete private RAM", actual, err)
	}
	replayed, err := recoverNativeCLI(fresh, request, "cold-restored")
	if err != nil || replayed.Record.RuntimeID != restored.Record.RuntimeID ||
		replayed.Runtime == nil || replayed.Runtime.PID != restored.Runtime.PID {
		t.Fatal("replayed recovery duplicated or changed the target", err)
	}
	if _, err := fresh.root.Lstat("objects/" + proof.Source.ID); !os.IsNotExist(err) {
		t.Fatal("file-only source provenance became a sandbox record", err)
	}
	wrong := request
	wrong.CheckpointSHA256 = strings.Repeat("0", 64)
	if _, err := recoverNativeCLI(fresh, wrong, "cold-restored"); err == nil {
		t.Fatal("completed target accepted a different recovery request")
	}
	advanced, err := readCaptureActor(restored.Runtime.PID, "advance")
	if err != nil || advanced.Counter != proof.Before.Counter+1 {
		t.Fatal("restored source cannot do useful work", err)
	}
	second := strings.Repeat("f", 32)
	if _, err := fresh.Checkpoint(ctx, "cold-restored", second); err != nil {
		t.Fatal("checkpoint after complete worker loss", err)
	}
	child, err := forkNativeCLI(fresh, "cold-restored", second, "cold-descendant")
	if err != nil {
		t.Fatal("fork after complete worker loss", err)
	}
	if _, err := fresh.Delete(ctx, "cold-restored"); err != nil {
		t.Fatal(err)
	}
	actual, err = readCaptureActor(child.Runtime.PID, "get")
	if err != nil || !reflect.DeepEqual(actual, advanced) {
		t.Fatal("recovered descendant lost independent RAM", err)
	}
	if _, err := fresh.Delete(ctx, "cold-descendant"); err != nil {
		t.Fatal(err)
	}
	t.Log("file-only cold-worker restore retained full 32-MiB RAM, useful work, checkpoint and independent descendant; original worker absent")
}

func supervisedFixtureConfig(t *testing.T) Config {
	t.Helper()
	data, err := exec.Command("/factorybox", "worker-status").CombinedOutput()
	var status struct {
		Schema    int    `json:"schema"`
		Lifecycle Config `json:"lifecycle"`
	}
	if err != nil || json.Unmarshal(data, &status) != nil || status.Schema != 1 {
		t.Fatal("actual live Go worker status", err, string(data))
	}
	return status.Lifecycle
}
