//go:build linux && factorybox_worker_probe

package devx_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"factorybox/internal/lifecycle"
	"factorybox/internal/worker"
)

type consumerPauseProof struct {
	Schema      int                       `json:"schema"`
	Owner       string                    `json:"owner"`
	StoreID     string                    `json:"store_id"`
	SourceRoot  string                    `json:"source_root"`
	Paused      lifecycle.Record          `json:"paused"`
	Minimal     *minimalState             `json:"minimal,omitempty"`
	Factory     *observedState            `json:"factory,omitempty"`
	Agent       *agentHeld                `json:"agent,omitempty"`
	Interrupted *interruptedConsumerPause `json:"interrupted,omitempty"`
}

func minimalWorkerState(t *testing.T, view lifecycle.View, mutation string, readiness time.Duration) minimalState {
	t.Helper()
	if view.Runtime == nil || view.Runtime.PID <= 1 {
		t.Fatal("minimal consumer lacks a live target")
	}
	transport := &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", fmt.Sprintf("/proc/%d/root/workspace/project/.minimal.sock", view.Runtime.PID))
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	for deadline := time.Now().Add(readiness); ; {
		method := http.MethodGet
		if mutation != "" {
			method = http.MethodPost
		}
		request, err := http.NewRequest(method, "http://minimal/state", strings.NewReader(mutation))
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Do(request)
		var state minimalState
		if err == nil {
			err = json.NewDecoder(response.Body).Decode(&state)
			response.Body.Close()
			if response.StatusCode != 200 {
				t.Fatal("minimal consumer status", response.StatusCode)
			}
			if err == nil {
				if state.RAMBytes != 32<<20 || len(state.RAMSHA256) != 64 || state.Marker == "" {
					t.Fatal("minimal RAM/file state incomplete", state)
				}
				return state
			}
		}
		if mutation != "" || time.Now().After(deadline) {
			t.Fatal("minimal worker first useful result", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestNativeConsumerWorkerLoss(t *testing.T) {
	if os.Getenv("FACTORYBOX_DEVX_WORKER_LOSS") != "1" {
		t.Fatal("requires its exact owned worker-loss fixture")
	}
	status, err := worker.Current("/run/factorybox")
	if err != nil {
		t.Fatal(err)
	}
	owner := os.Getenv("FACTORYBOX_DEVX_RUN")
	if len(owner) != 32 || status.StoreID != owner {
		t.Fatal("foreign persistent consumer store")
	}
	const proofPath = "/var/lib/sysbox/consumer-pause.json"
	const snapshot = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	minimal := os.Getenv("FACTORYBOX_DEVX_MINIMAL") == "1"
	agent := os.Getenv("FACTORYBOX_DEVX_OPENCODE_FLOW") == "1"
	if minimal && agent {
		t.Fatal("the unchanged minimal consumer does not contain an agent")
	}
	if os.Getenv("FACTORYBOX_CONSUMER_WORKER_PHASE") == "prepare" {
		f, source, start := prepareConsumerFixture(t)
		proof := consumerPauseProof{Schema: 1, Owner: owner, StoreID: status.StoreID, SourceRoot: f.root}
		if minimal {
			initial := minimalWorkerState(t, source, "", 180*time.Second)
			if initial.Counter != 0 || initial.Marker != "source" {
				t.Fatal("minimal fresh state", initial)
			}
			advanced := minimalWorkerState(t, source, `{"marker":"before-worker-loss"}`, 5*time.Second)
			if advanced.Counter != 1 || advanced.RAMSHA256 == initial.RAMSHA256 {
				t.Fatal("minimal source did not advance its RAM", advanced)
			}
			proof.Minimal = &advanced
		} else {
			f.attachIDE(source)
			initial := f.useful("prepared-start-first-use", source, start, "")
			if initial.App.Counter != 0 || initial.App.Marker != "source" {
				t.Fatal("Factory fresh state", initial)
			}
			advanced := f.useful("source-before-worker-loss", source, time.Now(), `{"increment":1,"marker":"devcontainer-before-loss"}`)
			if advanced.App.Counter != 1 || advanced.App.Instance != initial.App.Instance {
				t.Fatal("Factory source failed useful mutation", advanced)
			}
			proof.Factory = &advanced
			if agent {
				if f.agentExpected == nil {
					t.Fatal("real OpenCode tool was not armed before worker loss")
				}
				proof.Agent = f.agentExpected
			}
			if f.ide != nil {
				f.ide.finish()
				f.ide = nil
			}
		}
		paused := f.view("pause", "--snapshot-id", snapshot, "source")
		if paused.Record.Phase != "paused" || paused.Record.Pause == nil || paused.Record.Pause.CompletedAt == "" ||
			len(paused.Record.Pause.CheckpointSHA256) != 64 || paused.Record.Pause.After.Anon != 0 || paused.Record.Pause.Snapshot.ResidentBytesAfter != 0 {
			t.Fatal("consumer pause lacks its full commit and actual snapshot-RAM release")
		}
		proof.Paused = paused.Record
		file, err := os.OpenFile(proofPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
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
		if os.Getenv("FACTORYBOX_DEVX_INCOMPLETE_PAUSE") == "1" {
			holdIncompleteConsumerPause(t, f, &proof, proofPath)
			t.Fatal("incomplete-pause boundary unexpectedly returned")
		}
		readableConsumerOutput(t)
		fmt.Println("FACTORYBOX_CONSUMER_PAUSE_COMMITTED=" + owner)
		if os.Getenv("FACTORYBOX_CONSUMER_WORKER_ABRUPT") == "1" {
			select {}
		}
		return
	}
	if os.Getenv("FACTORYBOX_CONSUMER_WORKER_PHASE") != "recover" {
		t.Fatal("unknown consumer worker phase")
	}
	var proof consumerPauseProof
	data, err := os.ReadFile(proofPath)
	if err != nil || json.Unmarshal(data, &proof) != nil || proof.Schema != 1 || proof.Owner != owner || proof.StoreID != status.StoreID ||
		proof.SourceRoot == status.Lifecycle.Root || proof.Paused.Phase != "paused" || proof.Paused.Pause == nil ||
		(minimal != (proof.Minimal != nil)) || (!minimal != (proof.Factory != nil)) || (agent != (proof.Agent != nil)) {
		t.Fatal("missing exact consumer pause proof", err)
	}
	f := &fixture{t: t, root: status.Lifecycle.Root, workerRunRoot: "/run/factorybox", agentExpected: proof.Agent}
	if (os.Getenv("FACTORYBOX_DEVX_INCOMPLETE_PAUSE") == "1") != (proof.Interrupted != nil) {
		t.Fatal("interrupted-pause proof differs from the requested worker-loss case")
	}
	if proof.Interrupted != nil {
		rejectIncompleteConsumerPause(t, f, proof)
	}
	request := []string{"recover-paused", "--source-root", proof.SourceRoot, "--source", "source", "--snapshot-id", snapshot,
		"--source-owner", proof.Paused.Owner, "--checkpoint-sha256", proof.Paused.Pause.CheckpointSHA256, "recovered"}
	start := time.Now()
	restored := f.view(request...)
	f.cleanup("recovered")
	replayed := f.view(request...)
	if replayed.Runtime == nil || restored.Runtime == nil || restored.Runtime.PID != replayed.Runtime.PID || restored.Record.Owner != replayed.Record.Owner {
		t.Fatal("consumer recovery replay duplicated its target")
	}
	if _, err := os.Lstat(f.root + "/objects/source"); !os.IsNotExist(err) {
		t.Fatal("old consumer source was adopted as a sandbox", err)
	}
	if minimal {
		actual := minimalWorkerState(t, restored, "", 60*time.Second)
		if !reflect.DeepEqual(actual, *proof.Minimal) {
			t.Fatal("worker loss changed complete minimal RAM/files", actual, *proof.Minimal)
		}
	} else {
		f.attachIDE(restored)
		actual := f.useful("worker-recovery-first-use", restored, start, "")
		if !reflect.DeepEqual(actual, *proof.Factory) {
			t.Fatal("worker loss changed Factory parent/child/app RAM/files", actual, *proof.Factory)
		}
	}
	if proof.SourceRoot != status.Store+"/epochs/"+strings.Split(strings.TrimPrefix(proof.SourceRoot, status.Store+"/epochs/"), "/")[0]+"/controller" {
		t.Fatal("source cleanup escaped its recorded worker epoch")
	}
	if err := os.RemoveAll(proof.SourceRoot); err != nil {
		t.Fatal("remove only old saved consumer controller", err)
	}
	var minimalAfter minimalState
	var factoryAfter observedState
	if minimal {
		minimalAfter = minimalWorkerState(t, restored, `{"marker":"after-worker-loss"}`, 5*time.Second)
	} else {
		factoryAfter = f.useful("recovered-source-advance", restored, time.Now(), `{"increment":1,"marker":"devcontainer-after-loss"}`)
	}
	second := strings.Repeat("b", 32)
	f.command("checkpoint", "--snapshot-id", second, "recovered")
	branch := f.view("fork", "--source", "recovered", "--snapshot-id", second, "branch")
	f.cleanup("branch")
	if minimal {
		if actual := minimalWorkerState(t, branch, "", 60*time.Second); !reflect.DeepEqual(actual, minimalAfter) {
			t.Fatal("recovered minimal descendant changed RAM/files", actual, minimalAfter)
		}
		sourceAfter := minimalWorkerState(t, restored, `{"marker":"recovered-private"}`, 5*time.Second)
		minimalWorkerState(t, branch, `{"marker":"branch-private"}`, 5*time.Second)
		childAfter := minimalWorkerState(t, branch, `{"marker":"branch-private-again"}`, 5*time.Second)
		if sourceAfter.Counter+1 != childAfter.Counter || sourceAfter.Marker == childAfter.Marker || sourceAfter.RAMSHA256 == childAfter.RAMSHA256 {
			t.Fatal("minimal restored branches did not diverge independently")
		}
		if actual := minimalWorkerState(t, restored, "", 5*time.Second); !reflect.DeepEqual(actual, sourceAfter) {
			t.Fatal("branch mutation changed recovered source RAM/files")
		}
		minimalAfter = childAfter
	} else {
		actual := f.useful("fork-after-worker-recovery-first-use", branch, time.Now(), "")
		if !reflect.DeepEqual(actual, factoryAfter) {
			t.Fatal("recovered Factory descendant changed RAM/files", actual, factoryAfter)
		}
		sourceAfter := f.useful("recovered-private", restored, time.Now(), `{"increment":1,"marker":"devcontainer-recovered-private"}`)
		childAfter := f.useful("branch-private", branch, time.Now(), `{"increment":1,"marker":"devcontainer-branch-private"}`)
		if sourceAfter.App.Counter != childAfter.App.Counter || sourceAfter.App.Marker == childAfter.App.Marker {
			t.Fatal("Factory restored branches did not diverge independently")
		}
		factoryAfter = childAfter
		if f.ide != nil {
			f.ide.diverge(restored, branch)
			f.ide.finish()
			f.ide = nil
			factoryAfter, err = readApp(branch.Runtime.PID, "")
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if agent {
		f.finishAgentTool(restored, "recovered-agent")
		f.agentFlowReady("branch-after-recovered-agent-exit", branch)
	}
	f.command("delete", "recovered")
	if minimal {
		if actual := minimalWorkerState(t, branch, "", 5*time.Second); !reflect.DeepEqual(actual, minimalAfter) {
			t.Fatal("minimal descendant depended on its deleted recovered source")
		}
	} else {
		if actual := f.useful("descendant-after-source-deletion", branch, time.Now(), ""); !reflect.DeepEqual(actual, factoryAfter) {
			t.Fatal("Factory descendant depended on its deleted recovered source")
		}
	}
	if agent {
		f.finishAgentTool(branch, "branch-agent")
	}
	readableConsumerOutput(t)
	t.Log("actual consumer survived complete worker replacement, exact replay, removal of saved source files, new checkpoint/fork, independent mutations and source deletion")
}

func readableConsumerOutput(t *testing.T) {
	t.Helper()
	info, err := os.Stat("/output")
	if err != nil {
		t.Fatal(err)
	}
	owner := info.Sys().(*syscall.Stat_t)
	entries, err := os.ReadDir("/output")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Type().IsRegular() {
			if err := os.Chown("/output/"+entry.Name(), int(owner.Uid), int(owner.Gid)); err != nil {
				t.Fatal(err)
			}
		}
	}
}
