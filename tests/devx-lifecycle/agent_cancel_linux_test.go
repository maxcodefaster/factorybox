//go:build linux && factorybox_worker_probe

package devx_test

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

	"factorybox/internal/lifecycle"
	"factorybox/internal/worker"
)

type canceledAgent struct {
	SessionID  string `json:"session_id"`
	Canceled   bool   `json:"canceled"`
	Launches   int    `json:"launches"`
	ToolStatus string `json:"tool_status"`
	CallSHA256 string `json:"call_sha256"`
	ToolError  struct {
		Type     string `json:"type"`
		Message  string `json:"message"`
		Status   int    `json:"status,omitempty"`
		Response *struct {
			Body string `json:"body"`
		} `json:"response,omitempty"`
	} `json:"tool_error"`
	OriginalTarget struct {
		PID   int    `json:"pid"`
		Birth string `json:"birth"`
	} `json:"original_target"`
	Nonce                string `json:"original_nonce"`
	IdleRepeat           bool   `json:"idle_repeat"`
	ProcessGone          bool   `json:"original_process_gone"`
	ExecutionInterrupted bool   `json:"execution_interrupted"`
}

type canceledConsumerProof struct {
	Schema     int              `json:"schema"`
	StoreID    string           `json:"store_id"`
	BootID     string           `json:"boot_id"`
	SourceRoot string           `json:"source_root"`
	Paused     lifecycle.Record `json:"paused"`
	App        observedState    `json:"app"`
	Agent      canceledAgent    `json:"agent"`
}

func (f *fixture) canceledAgent(view lifecycle.View, operation string) canceledAgent {
	f.t.Helper()
	if view.Runtime == nil || view.Runtime.PID <= 1 {
		f.t.Fatal("canceled agent lacks its exact live sandbox")
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "nsenter",
		"--mount=/proc/self/fd/3", "--root=/proc/self/fd/4", "--wd=/proc/self/fd/4",
		"--pid=/proc/self/fd/5", "--net=/proc/self/fd/6", "--user=/proc/self/fd/7",
		"--setuid=0", "--setgid=0", "--", "/usr/local/bin/node",
		"/opt/factorybox/opencode-flow/cancel-client.cjs", operation)
	command.ExtraFiles = anchors
	command.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=/root"}
	output, err := command.CombinedOutput()
	if err != nil || len(output) > 1<<20 {
		path := fmt.Sprintf("/proc/%d/root/workspace/project/.factorybox-interruption-observed.json", view.Runtime.PID)
		if observation, readErr := os.ReadFile(path); readErr == nil && len(observation) <= 2<<20 {
			if writeErr := os.WriteFile("/output/agent-cancel-failed-observation-"+view.Record.ID+".json", observation, 0600); writeErr != nil {
				f.t.Log("retain canceled tool observation", writeErr)
			}
		}
		f.t.Fatalf("actual agent cancellation %s: %v: %s", operation, err, output)
	}
	var observed canceledAgent
	decoder := json.NewDecoder(strings.NewReader(string(output)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&observed); err != nil {
		f.t.Fatal(err)
	}
	if observed.SessionID == "" || !observed.Canceled || observed.Launches != 1 || observed.ToolStatus != "error" ||
		len(observed.CallSHA256) != 64 || len(observed.Nonce) != 64 || observed.ToolError.Type == "" || observed.ToolError.Message == "" || !observed.ExecutionInterrupted ||
		observed.OriginalTarget.PID <= 1 || observed.OriginalTarget.Birth == "" || !observed.IdleRepeat || !observed.ProcessGone {
		f.t.Fatal("incomplete actual cancellation proof", observed)
	}
	if err := os.WriteFile("/output/agent-cancel-"+operation+"-"+view.Record.ID+".json", output, 0600); err != nil {
		f.t.Fatal(err)
	}
	return observed
}

// Mutating application requests are never retried after an uncertain response.
func canceledConsumerApp(t *testing.T, view lifecycle.View, body string, timeout time.Duration) observedState {
	t.Helper()
	for deadline := time.Now().Add(timeout); ; {
		state, err := readApp(view.Runtime.PID, body)
		if err == nil {
			return state
		}
		if body != "" || time.Now().After(deadline) {
			t.Fatal("cancellation consumer app", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func saveCanceledConsumer(t *testing.T, path string, value any) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := json.NewEncoder(file).Encode(value); err != nil {
		t.Fatal(err)
	}
	if err := file.Sync(); err != nil {
		t.Fatal(err)
	}
	parent, err := os.Open(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	if err := parent.Sync(); err != nil {
		t.Fatal(err)
	}
}

func TestNativeAgentCancellation(t *testing.T) {
	if os.Getenv("FACTORYBOX_DEVX_WORKER_LOSS") != "1" || os.Getenv("FACTORYBOX_DEVX_OPENCODE_FLOW") != "1" {
		t.Fatal("requires its actual Factory agent worker-replacement fixture")
	}
	status, err := worker.Current("/run/factorybox")
	if err != nil {
		t.Fatal(err)
	}
	owner := os.Getenv("FACTORYBOX_DEVX_RUN")
	if len(owner) != 32 || status.StoreID != owner {
		t.Fatal("foreign canceled-agent store")
	}
	const snapshot = "cccccccccccccccccccccccccccccccc"
	proofPath := filepath.Join(status.Store, "agent-cancel-proof.json")
	phase := os.Getenv("FACTORYBOX_CONSUMER_WORKER_PHASE")
	if phase == "prepare" {
		f, source, initial := prepareDeveloperFixture(t)
		if f.agentExpected == nil {
			t.Fatal("actual held agent tool was not armed")
		}
		observed := f.canceledAgent(source, "interrupt")
		if observed.Nonce != f.agentExpected.State.Nonce {
			t.Fatal("interrupted a different tool")
		}
		if actual := f.canceledAgent(source, "verify"); !reflect.DeepEqual(actual, observed) {
			t.Fatal("idle cancellation changed", actual, observed)
		}
		if f.ide != nil {
			f.ide.finish()
			f.ide = nil
		}
		paused := f.view("pause", "--snapshot-id", snapshot, "source")
		if paused.Record.Phase != "paused" || paused.Record.Pause == nil || paused.Record.Pause.CompletedAt == "" ||
			len(paused.Record.Pause.CheckpointSHA256) != 64 || paused.Record.Pause.After.Anon != 0 || paused.Record.Pause.Snapshot.ResidentBytesAfter != 0 {
			t.Fatal("canceled source lacks its committed pause and actual RAM release")
		}
		proof := canceledConsumerProof{Schema: 1, StoreID: owner, BootID: status.BootID,
			SourceRoot: status.Lifecycle.Root, Paused: paused.Record, App: initial, Agent: observed}
		saveCanceledConsumer(t, proofPath, proof)
		saveCanceledConsumer(t, "/output/opencode-cancel-prepare.json", map[string]any{
			"schema": 1, "phase": phase, "passed": true, "agent": observed, "pause": paused.Record.Pause})
		readableConsumerOutput(t)
		fmt.Println("FACTORYBOX_CONSUMER_PAUSE_COMMITTED=" + owner)
		if os.Getenv("FACTORYBOX_CONSUMER_WORKER_ABRUPT") == "1" {
			select {}
		}
		return
	}
	if phase != "recover" {
		t.Fatal("unknown cancellation fixture phase")
	}
	var proof canceledConsumerProof
	data, err := os.ReadFile(proofPath)
	if err != nil || json.Unmarshal(data, &proof) != nil || proof.Schema != 1 || proof.StoreID != owner ||
		proof.BootID != status.BootID || proof.SourceRoot == status.Lifecycle.Root ||
		filepath.Base(proof.SourceRoot) != "controller" || filepath.Dir(filepath.Dir(proof.SourceRoot)) != filepath.Join(status.Store, "epochs") ||
		proof.Paused.Phase != "paused" || proof.Paused.Pause == nil {
		t.Fatal("missing exact canceled-agent committed pause", err)
	}
	f := &fixture{t: t, root: status.Lifecycle.Root, workerRunRoot: "/run/factorybox"}
	request := []string{"recover-paused", "--source-root", proof.SourceRoot, "--source", "source", "--snapshot-id", snapshot,
		"--source-owner", proof.Paused.Owner, "--checkpoint-sha256", proof.Paused.Pause.CheckpointSHA256, "recovered"}
	restored := f.view(request...)
	f.cleanup("recovered")
	replayed := f.view(request...)
	if restored.Runtime == nil || replayed.Runtime == nil || restored.Runtime.PID != replayed.Runtime.PID || restored.Record.Owner != replayed.Record.Owner {
		t.Fatal("canceled-agent recovery replay duplicated its sandbox")
	}
	if actual := canceledConsumerApp(t, restored, "", 60*time.Second); !reflect.DeepEqual(actual, proof.App) {
		t.Fatal("cancellation recovery changed Factory state", actual, proof.App)
	}
	if actual := f.canceledAgent(restored, "verify"); !reflect.DeepEqual(actual, proof.Agent) {
		t.Fatal("worker replacement changed cancellation", actual, proof.Agent)
	}
	const forkSnapshot = "dddddddddddddddddddddddddddddddd"
	f.command("checkpoint", "--snapshot-id", forkSnapshot, "recovered")
	branch := f.view("fork", "--source", "recovered", "--snapshot-id", forkSnapshot, "branch")
	f.cleanup("branch")
	if actual := canceledConsumerApp(t, branch, "", 60*time.Second); !reflect.DeepEqual(actual, proof.App) {
		t.Fatal("canceled-agent fork lost application state", actual, proof.App)
	}
	if actual := f.canceledAgent(branch, "verify"); !reflect.DeepEqual(actual, proof.Agent) {
		t.Fatal("independent fork changed cancellation", actual, proof.Agent)
	}
	advanced := canceledConsumerApp(t, restored, `{"increment":1,"marker":"canceled-source-after"}`, time.Second)
	if advanced.App.Counter != proof.App.App.Counter+1 || advanced.App.Instance != proof.App.App.Instance {
		t.Fatal("recovered application failed its independent mutation", advanced)
	}
	if actual := canceledConsumerApp(t, branch, "", time.Second); !reflect.DeepEqual(actual, proof.App) {
		t.Fatal("canceled-agent branch shares mutable source state", actual, proof.App)
	}
	f.command("delete", "recovered")
	if actual := canceledConsumerApp(t, branch, "", time.Second); !reflect.DeepEqual(actual, proof.App) {
		t.Fatal("branch depended on deleted recovered source", actual, proof.App)
	}
	if actual := f.canceledAgent(branch, "verify"); !reflect.DeepEqual(actual, proof.Agent) {
		t.Fatal("source deletion changed descendant cancellation", actual, proof.Agent)
	}
	saveCanceledConsumer(t, "/output/opencode-cancel-recover.json", map[string]any{
		"schema": 1, "phase": phase, "passed": true, "agent": proof.Agent, "independent_branch": true,
		"source_deleted": true, "recovery_replay": true, "original_proof": proof})
	readableConsumerOutput(t)
}
