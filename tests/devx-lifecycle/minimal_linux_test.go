//go:build linux

package devx_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"factorybox/internal/lifecycle"
)

type minimalState struct {
	Counter   int    `json:"counter"`
	Marker    string `json:"marker"`
	RAMSHA256 string `json:"ram_sha256"`
	RAMBytes  int    `json:"ram_bytes"`
}

func TestNativeMinimalLifecycle(t *testing.T) {
	if os.Getenv("FACTORYBOX_DEVX_MINIMAL") != "1" {
		t.Skip("requires the owned minimal Dev Container fixture")
	}
	fixture, source, start := prepareConsumerFixture(t)
	var observations []map[string]any
	useful := func(phase string, view lifecycle.View, start time.Time, mutation string) minimalState {
		t.Helper()
		if view.Runtime == nil || view.Runtime.PID <= 1 {
			t.Fatal("minimal runtime lacks a live identity")
		}
		transport := &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", fmt.Sprintf("/proc/%d/root/workspace/project/.minimal.sock", view.Runtime.PID))
		}}
		defer transport.CloseIdleConnections()
		client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
		deadline := time.Now().Add(180 * time.Second)
		var state minimalState
		for {
			if phase == "prepared-start-first-use" {
				_, readyErr := os.Stat(fmt.Sprintf("/proc/%d/root/workspace/project/.minimal-exec-complete", view.Runtime.PID))
				if readyErr != nil {
					if time.Now().After(deadline) {
						t.Fatal("minimal setup client did not complete", readyErr)
					}
					time.Sleep(100 * time.Millisecond)
					continue
				}
			}
			method := http.MethodGet
			if mutation != "" {
				method = http.MethodPost
			}
			request, err := http.NewRequest(method, "http://minimal/state", strings.NewReader(mutation))
			if err != nil {
				t.Fatal(err)
			}
			response, err := client.Do(request)
			if err == nil {
				err = json.NewDecoder(response.Body).Decode(&state)
				response.Body.Close()
				if response.StatusCode != 200 {
					t.Fatal("minimal actor HTTP status", response.StatusCode)
				}
				if err == nil {
					break
				}
			}
			if mutation != "" || time.Now().After(deadline) {
				t.Fatal("minimal useful work", err)
			}
			time.Sleep(100 * time.Millisecond)
		}
		if state.RAMBytes != 32<<20 || len(state.RAMSHA256) != 64 || state.Marker == "" {
			t.Fatal("incomplete minimal RAM/file state", state)
		}
		observations = append(observations, map[string]any{"phase": phase, "milliseconds": float64(time.Since(start)) / float64(time.Millisecond), "state": state, "worker_resources": fixture.resources()})
		data, err := json.MarshalIndent(observations, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile("/output/devx-observations.json", append(data, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
		t.Logf("%s through the minimal Dev Container actor: %s", phase, time.Since(start))
		return state
	}
	initial := useful("prepared-start-first-use", source, start, "")
	if initial.Counter != 0 || initial.Marker != "source" {
		t.Fatal("minimal initial state", initial)
	}
	imageData, err := os.ReadFile(fmt.Sprintf("/proc/%d/root/workspace/project/.minimal-image.json", source.Runtime.PID))
	if err != nil {
		t.Fatal(err)
	}
	var images []struct {
		Architecture string
		Os           string
		RepoDigests  []string
	}
	if err := json.Unmarshal(imageData, &images); err != nil {
		t.Fatal(err)
	}
	configData, err := os.ReadFile("/example/.devcontainer/devcontainer.json")
	if err != nil {
		t.Fatal(err)
	}
	var config struct{ Image string }
	if err := json.Unmarshal(configData, &config); err != nil {
		t.Fatal(err)
	}
	repository, digest, pinned := strings.Cut(config.Image, "@")
	if !pinned {
		t.Fatal("minimal example has no digest")
	}
	repository = strings.TrimPrefix(repository, "docker.io/library/")
	repository, _, _ = strings.Cut(repository, ":")
	wanted := repository + "@" + digest
	found := false
	if len(images) == 1 {
		for _, digest := range images[0].RepoDigests {
			found = found || digest == wanted
		}
	}
	if len(images) != 1 || images[0].Architecture != runtime.GOARCH || images[0].Os != "linux" || !found {
		t.Fatal("minimal image lost its exact digest/native platform", string(imageData))
	}
	if err := os.WriteFile("/output/minimal-image.json", imageData, 0600); err != nil {
		t.Fatal(err)
	}
	snapshot := strings.Repeat("a", 32)
	fixture.command("checkpoint", "--snapshot-id", snapshot, "source")
	start = time.Now()
	branch := fixture.view("fork", "--source", "source", "--snapshot-id", snapshot, "branch")
	fixture.cleanup("branch")
	if got := useful("fork-first-use", branch, start, ""); !reflect.DeepEqual(initial, got) {
		t.Fatal("minimal fork changed RAM/files", initial, got)
	}
	sourceAfter := useful("source-advance", source, time.Now(), `{"marker":"source-after"}`)
	branchAfter := useful("branch-advance", branch, time.Now(), `{"marker":"branch-after"}`)
	if sourceAfter.Counter != 1 || branchAfter.Counter != 1 || sourceAfter.Marker != "source-after" || branchAfter.Marker != "branch-after" || sourceAfter.RAMSHA256 == initial.RAMSHA256 {
		t.Fatal("minimal sources did not diverge", sourceAfter, branchAfter)
	}
	if got := useful("source-independent", source, time.Now(), ""); got != sourceAfter {
		t.Fatal("minimal branch changed source", got, sourceAfter)
	}
	deleted := fixture.view("delete", "source")
	if deleted.Record.Phase != "deleted" || deleted.Runtime != nil {
		t.Fatal("minimal source deletion incomplete", deleted)
	}
	descendantSnapshot := strings.Repeat("b", 32)
	fixture.command("checkpoint", "--snapshot-id", descendantSnapshot, "branch")
	start = time.Now()
	descendant := fixture.view("fork", "--source", "branch", "--snapshot-id", descendantSnapshot, "descendant")
	fixture.cleanup("descendant")
	if got := useful("fork-of-fork-first-use", descendant, start, ""); got != branchAfter {
		t.Fatal("minimal descendant lost state after original deletion", got, branchAfter)
	}
	fixture.view("delete", "descendant")
	pauseSnapshot := strings.Repeat("c", 32)
	paused := fixture.view("pause", "--snapshot-id", pauseSnapshot, "branch")
	if paused.Record.Phase != "paused" || paused.Record.Pause == nil || paused.Record.Pause.CompletedAt == "" || paused.Record.Pause.After.Anon > 1<<20 || paused.Record.Pause.Before.Anon <= paused.Record.Pause.After.Anon || !paused.Record.Pause.Snapshot.FileBacked || paused.Record.Pause.Snapshot.ResidentBytesAfter != 0 {
		t.Fatal("minimal pause lacks actual RAM/snapshot release", paused)
	}
	start = time.Now()
	resumed := fixture.view("resume", "--snapshot-id", pauseSnapshot, "branch")
	if got := useful("resume-first-use", resumed, start, ""); got != branchAfter {
		t.Fatal("minimal resume lost RAM/files", got, branchAfter)
	}
	resumedSnapshot := strings.Repeat("d", 32)
	fixture.command("checkpoint", "--snapshot-id", resumedSnapshot, "branch")
	start = time.Now()
	after := fixture.view("fork", "--source", "branch", "--snapshot-id", resumedSnapshot, "after-resume")
	fixture.cleanup("after-resume")
	if got := useful("fork-of-resumed-first-use", after, start, ""); got != branchAfter {
		t.Fatal("minimal resumed-source fork lost RAM/files", got, branchAfter)
	}
}
