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
	"path/filepath"
	"sync"
	"testing"
	"time"
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
	lock, err := engine.lock()
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	guard, err := engine.startCaptureGuardWithExecutable(view.Record, view.Runtime.PID, "/factorybox")
	if err != nil {
		t.Fatal(err)
	}
	ended := false
	defer func() {
		if !ended {
			if err := guard.finish(true); err != nil {
				t.Errorf("source guard retained unresolved capture: %v", err)
			}
		}
	}()
	if err := guard.freeze(true); err != nil {
		t.Fatal(err)
	}
	if err := guard.capturePhase(true); err != nil {
		t.Fatal(err)
	}
	if err := engine.capture(ctx, guard, lock, "/factorybox"); err != nil {
		t.Fatal(err)
	}
	images := filepath.Dir(guard.config.Evidence) + "/images"
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
	if err := guard.capturePhase(false); err != nil {
		t.Fatal(err)
	}
	if err := guard.freeze(false); err != nil {
		t.Fatal(err)
	}
	err = guard.finish(false)
	ended = true
	if err != nil {
		t.Fatal(err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := readCaptureActor(view.Runtime.PID, "advance")
	expected := append([]byte(nil), before.Payload...)
	expected[0] ^= 0xff
	if err != nil || after.Counter != 1 || !bytes.Equal(after.Payload, expected) {
		t.Fatalf("original RAM process did not continue independently: %v", err)
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
	t.Logf("actual CRIU capture under %s retained PID %d, included the 8192-byte private RAM payload in %d page files and preserved backing after source mutation; filesystem snapshot and restore remain separate gates",
		view.Record.RuntimeID, view.Runtime.PID, len(files))
}
