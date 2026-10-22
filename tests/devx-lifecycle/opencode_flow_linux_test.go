//go:build linux

package devx_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"time"

	"factorybox/internal/lifecycle"
	providerfixture "factorybox/tests/provider-fixture"
)

type agentToolState struct {
	PID         int    `json:"pid"`
	Nonce       string `json:"nonce"`
	Counter     int    `json:"counter"`
	Marker      string `json:"marker"`
	RAMBytes    int    `json:"ram_bytes"`
	RAMSHA256   string `json:"ram_sha256"`
	WatchMarker string `json:"watch_marker"`
}

type agentHeld struct {
	SessionID string         `json:"session_id"`
	State     agentToolState `json:"state"`
}

func (f *fixture) agentClient(view lifecycle.View, operation string, arguments ...string) []byte {
	f.t.Helper()
	if view.Runtime == nil || view.Runtime.PID <= 1 {
		f.t.Fatal("agent target lacks its exact live identity")
	}
	var anchors []*os.File
	defer func() {
		for _, file := range anchors {
			file.Close()
		}
	}()
	for _, suffix := range []string{"ns/mnt", "root", "ns/pid", "ns/net", "ns/user"} {
		file, err := os.Open(fmt.Sprintf("/proc/%d/%s", view.Runtime.PID, suffix))
		if err != nil {
			f.t.Fatal(err)
		}
		anchors = append(anchors, file)
	}
	duration := 30 * time.Second
	external := os.Getenv("FACTORYBOX_DEVX_EXTERNAL_PROVIDER") == "1" && (operation == "arm" || operation == "finish")
	if external {
		duration = 90 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()
	if external {
		lease, err := providerfixture.Acquire(ctx, "/output", operation, providerfixture.Target{
			RunOwner: os.Getenv("FACTORYBOX_DEVX_RUN"), Sandbox: view.Record.ID, Owner: view.Record.Owner,
			Runtime: view.Runtime.ID, PID: view.Runtime.PID, Created: view.Runtime.Created,
		})
		if err != nil {
			f.t.Fatal("attach exact external consumer provider", err)
		}
		defer func() {
			closed, release := context.WithTimeout(context.Background(), 10*time.Second)
			defer release()
			if err := lease.Close(closed); err != nil {
				f.t.Fatal("external provider did not fully exit before capture", err)
			}
		}()
	}
	options := []string{"--mount=/proc/self/fd/3", "--root=/proc/self/fd/4", "--wd=/proc/self/fd/4",
		"--pid=/proc/self/fd/5", "--net=/proc/self/fd/6", "--user=/proc/self/fd/7", "--setuid=0", "--setgid=0", "--",
		"/usr/local/bin/node", "/opt/factorybox/opencode-flow/client.cjs", operation}
	command := exec.CommandContext(ctx, "nsenter", append(options, arguments...)...)
	command.ExtraFiles = anchors
	command.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=/root"}
	output, err := command.CombinedOutput()
	if err != nil || len(output) > 1<<20 {
		f.t.Fatalf("actual OpenCode tool %s: %v: %s", operation, err, output)
	}
	return output
}

func (f *fixture) agentHeld(view lifecycle.View, operation string, arguments ...string) agentHeld {
	f.t.Helper()
	var observed agentHeld
	if err := json.Unmarshal(f.agentClient(view, operation, arguments...), &observed); err != nil {
		f.t.Fatal(err)
	}
	if observed.SessionID == "" || observed.State.PID <= 1 || len(observed.State.Nonce) != 64 || observed.State.RAMBytes != 16<<20 || len(observed.State.RAMSHA256) != 64 {
		f.t.Fatal("agent did not preserve its actual private tool process", observed)
	}
	return observed
}

func (f *fixture) agentFlowReady(phase string, view lifecycle.View) {
	f.t.Helper()
	if os.Getenv("FACTORYBOX_DEVX_OPENCODE_FLOW") != "1" {
		return
	}
	operation := "state"
	if phase == "prepared-start-first-use" && f.agentExpected == nil {
		operation = "arm"
	}
	observed := f.agentHeld(view, operation)
	if f.agentExpected == nil {
		f.agentExpected = &observed
	}
	if !reflect.DeepEqual(observed, *f.agentExpected) {
		f.t.Fatal("running agent/tool RAM or session changed across lifecycle", phase, observed, *f.agentExpected)
	}
	path := "/output/opencode-flow.json"
	var samples []map[string]any
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &samples); err != nil {
			f.t.Fatal(err)
		}
	} else if !os.IsNotExist(err) {
		f.t.Fatal(err)
	}
	samples = append(samples, map[string]any{"phase": phase, "sandbox": view.Record.ID, "observed": observed})
	data, err := json.MarshalIndent(samples, "", "  ")
	if err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0600); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) finishAgentTool(view lifecycle.View, marker string) {
	f.t.Helper()
	if os.Getenv("FACTORYBOX_DEVX_OPENCODE_FLOW") != "1" {
		return
	}
	before := f.agentHeld(view, "state")
	watched := f.agentHeld(view, "watch", marker)
	if watched.State.WatchMarker != marker || watched.SessionID != before.SessionID || watched.State.Nonce != before.State.Nonce ||
		watched.State.RAMSHA256 != before.State.RAMSHA256 || watched.State.Counter != before.State.Counter {
		f.t.Fatal("restored tool's actual filesystem watch failed or changed its private RAM", before, watched)
	}
	advanced := f.agentHeld(view, "advance", marker)
	if before.SessionID != advanced.SessionID || before.State.Nonce != advanced.State.Nonce || advanced.State.Counter != before.State.Counter+1 ||
		advanced.State.Marker != marker || advanced.State.RAMSHA256 == before.State.RAMSHA256 {
		f.t.Fatal("actual agent tool did not mutate its own RAM", before, advanced)
	}
	var result struct {
		SessionID string `json:"session_id"`
		Result    struct {
			agentToolState
			Exit int `json:"exit"`
		} `json:"result"`
		Ack          bool `json:"agent_ack"`
		ReceivedExit int  `json:"model_received_exit"`
	}
	data := f.agentClient(view, "finish")
	if err := json.Unmarshal(data, &result); err != nil {
		f.t.Fatal(err)
	}
	if result.SessionID != advanced.SessionID || !reflect.DeepEqual(result.Result.agentToolState, advanced.State) || result.Result.Exit != 37 || !result.Ack || result.ReceivedExit != 37 {
		f.t.Fatal("agent failed to ingest its actual tool result and nonzero exit", result)
	}
	if err := os.WriteFile("/output/opencode-result-"+view.Record.ID+".json", data, 0600); err != nil {
		f.t.Fatal(err)
	}
}
