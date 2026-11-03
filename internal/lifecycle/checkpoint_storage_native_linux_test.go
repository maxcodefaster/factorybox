//go:build linux

package lifecycle

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestNativeStorageCaptureMemoryAdmission(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_STORAGE") != "1" {
		t.Skip("requires the owned native storage-pressure worker")
	}
	engine, bundle := nativeEngine(t, "/bin/env FACTORYBOX_CAPTURE_ACTOR=1 FACTORYBOX_CAPTURE_BALLAST=1 /bin/capture-actor -test.run=^TestCaptureActor$ -test.timeout=0")
	binary, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundle+"/lower/bin/capture-actor", binary, 0755); err != nil {
		t.Fatal(err)
	}
	engine.config.Criu, engine.config.CriuSHA256, engine.config.ManagerRoot =
		"/out/criu", os.Getenv("FACTORYBOX_CRIU_SHA256"), "/var/lib/sysbox/store/manager"
	ctx, cancel := context.WithTimeout(context.Background(), 420*time.Second)
	defer cancel()
	started, err := engine.Start(ctx, "source", bundle)
	if err != nil {
		t.Fatal(err)
	}
	startEvidence, err := json.MarshalIndent(started, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("/output/capture-memory-start.json", append(startEvidence, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	originalFloor, originalReserve := engine.config.MinFreeBytes, engine.config.WriteReserve
	t.Cleanup(func() {
		engine.config.MinFreeBytes, engine.config.WriteReserve = originalFloor, originalReserve
		if _, err := engine.Delete(context.Background(), "source"); err != nil {
			t.Error("delete exact memory-admission source", err)
		}
	})
	var before captureActorState
	for deadline := time.Now().Add(10 * time.Second); ; {
		before, err = readCaptureActor(started.Runtime.PID, "get")
		if err == nil && before.BallastBytes == 32<<20 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("original RAM actor did not become ready", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	memory, err := engine.sandboxMemory(started.Record)
	if err != nil || memory.Anon < uint64(before.BallastBytes) {
		t.Fatal("fixture lacks its actual private anonymous RAM", memory, err)
	}
	capacity, err := engine.Capacity()
	payload := memory.Anon + memory.Shmem
	if err != nil || capacity.AvailableBytes <= payload+(1<<20) {
		t.Fatal("owned store lacks room for the admission fixture", capacity, err)
	}
	// No disk is filled. Leave room above the ordinary floor, but less than
	// the source's known RAM payload. The old zero-byte admission accepts it.
	engine.config.WriteReserve = 1 << 20
	engine.config.MinFreeBytes = capacity.AvailableBytes - payload/2 - engine.config.WriteReserve
	if err := engine.admitStorageWrite(); err != nil {
		t.Fatal("fixture did not pass ordinary write admission", err)
	}
	recordBefore, err := engine.root.ReadFile("objects/source/record.json")
	if err != nil {
		t.Fatal(err)
	}
	var expected Record
	if err := json.Unmarshal(recordBefore, &expected); err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"checkpoint", "pause"} {
		snapshot := strings.Repeat("c", 32)
		if operation == "pause" {
			snapshot = strings.Repeat("d", 32)
			_, err = engine.Pause(ctx, "source", snapshot)
		} else {
			_, err = engine.Checkpoint(ctx, "source", snapshot)
		}
		if err == nil || !strings.Contains(err.Error(), "new storage writes refused") {
			t.Fatal(operation+" did not admit its RAM bytes before capture", err)
		}
		generation := "objects/source/captures/" + snapshot
		for _, path := range []string{"images", "filesystem", "guard/capture-worker-intent.json", "guard/guard-capture-entered.json"} {
			if _, err := engine.root.Lstat(generation + "/" + path); !os.IsNotExist(err) {
				t.Fatal(operation+" entered CRIU despite insufficient RAM backing", path, err)
			}
		}
		var rejection checkpointAdmissionRejection
		if err := engine.readCheckpointJSON(generation+"/admission-rejected.json", &rejection); err != nil ||
			rejection.Kind != "storage" || rejection.SourcePID != started.Runtime.PID {
			t.Fatal(operation+" lacks its bound pre-capture storage rejection", err)
		}
		if operation == "checkpoint" {
			_, replayErr := engine.Checkpoint(ctx, "source", snapshot)
			if replayErr == nil || !strings.HasPrefix(replayErr.Error(), "checkpoint storage admission: ") {
				t.Fatal("original storage rejection did not replay without another capture", replayErr)
			}
		}
		recordAfter, err := engine.root.ReadFile("objects/source/record.json")
		var actualRecord Record
		if err != nil || json.Unmarshal(recordAfter, &actualRecord) != nil {
			t.Fatal("read original source after RAM admission", err)
		}
		if operation == "pause" {
			if len(actualRecord.PauseHistory) != len(expected.PauseHistory)+1 {
				t.Fatal("rejected Pause did not retain its exact outcome")
			}
			last := actualRecord.PauseHistory[len(actualRecord.PauseHistory)-1]
			if last.SnapshotID != snapshot || last.SourcePID != started.Runtime.PID ||
				last.RejectedAt == "" || last.Error == "" || last.CompletedAt != "" {
				t.Fatal("rejected Pause outcome changed its request or claimed a capture")
			}
			expected.PauseHistory = append(expected.PauseHistory, last)
		}
		if !reflect.DeepEqual(actualRecord, expected) {
			t.Fatal(operation+" changed the original runtime transition", err)
		}
		state, err := engine.Inspect(ctx, "source")
		if err != nil || state.Runtime == nil || state.Runtime.Status != "running" ||
			state.Runtime.PID != started.Runtime.PID {
			t.Fatal(operation+" changed or froze the original source", err)
		}
		if operation == "pause" {
			data, err := json.MarshalIndent(state, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile("/output/capture-memory-pause-state.json", append(data, '\n'), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	actual, err := readCaptureActor(started.Runtime.PID, "get")
	if err != nil || !reflect.DeepEqual(before, actual) {
		t.Fatal("RAM admission changed private memory, files or watches", err)
	}
	proof, err := json.MarshalIndent(map[string]any{
		"passed": true, "preflight_rejections": 2, "known_ram_bytes": payload,
		"payload_headroom_bytes": payload / 2, "generic_write_admitted": true,
		"source_pid": started.Runtime.PID, "source_identity_and_ram_preserved": true,
	}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("/output/capture-memory-admission.json", append(proof, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(memory)
	t.Logf("Checkpoint and Pause refused before capture; original source and 32-MiB RAM nonce retained: %s", data)
}
