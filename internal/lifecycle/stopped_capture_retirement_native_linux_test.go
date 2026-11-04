//go:build linux

package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNativeStoppedCaptureBackingRetirement(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_STOPPED_CAPTURE_RETIREMENT") != "1" {
		t.Skip("requires the owned stopped-capture bounded worker")
	}
	if root := os.Getenv("FACTORYBOX_STOPPED_CAPTURE_DELETE_FIXTURE"); root != "" {
		stoppedCaptureDeleteFixture(t, root)
		return
	}
	status := boundedLossStatusNow(t)
	cli := os.Getenv("FACTORYBOX_RETIREMENT_CLI")
	if cli == "" {
		cli = "/opt/factorybox/factorybox"
	}
	command := func(args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 420*time.Second)
		defer cancel()
		return exec.CommandContext(ctx, cli, append([]string{"--worker-run-root", "/run/factorybox"}, args...)...).CombinedOutput()
	}
	var started, deleted View
	var generation string
	if !t.Run("real-unpublished-capture", func(t *testing.T) {
		started = standbyFixture(t, status, "stopped-capture", nil)
		child := exec.Command(os.Args[0], "-test.run=^TestNativeStoppedCaptureBackingRetirement$", "-test.timeout=420s")
		child.Env = append(os.Environ(), "FACTORYBOX_STOPPED_CAPTURE_DELETE_FIXTURE="+status.Lifecycle.Root)
		if output, err := child.CombinedOutput(); err != nil {
			t.Fatal("actual capture and controller exit", string(output), err)
		}
		generation = status.Lifecycle.Root + "/objects/stopped-capture/captures/" + strings.Repeat("b", 32)
		deadline := time.Now().Add(10 * time.Second)
		for {
			output, err := command("delete", "stopped-capture")
			if err == nil {
				if err := json.Unmarshal(output, &deleted); err != nil {
					t.Fatal(err)
				}
				break
			}
			// A refused pre-admission Delete has not dispatched runtime deletion.
			if !bytes.Contains(output, []byte("live or unproved original actor")) || time.Now().After(deadline) {
				t.Fatal("Delete after original actor exit", string(output), err)
			}
			time.Sleep(10 * time.Millisecond)
		}
		if deleted.Record.Phase != "deleted" || deleted.Record.Delete == nil ||
			deleted.Record.Delete.CompletedAt == "" || deleted.Record.Pause == nil ||
			deleted.Record.Pause.CompletedAt != "" {
			t.Fatal("Delete completed an unpublished Pause or lacks its own completion")
		}
	}) {
		return
	}
	evidence := map[string][]byte{}
	for _, name := range []string{"capture-delete-admission.json", "checkpoint-intent.json", "source-state.json",
		"filesystem.json", "guard/guard-intent.json", "guard/guard-armed.json",
		"guard/capture-worker-intent.json", "guard/capture-process-complete.json",
		"guard/capture-stop-intent.json", "guard/capture-worker-outcome.json", "guard/guard-outcome.json",
		"guard/criu-retained-arm.json"} {
		data, err := os.ReadFile(generation + "/" + name)
		if err != nil {
			t.Fatal(err)
		}
		evidence[name] = data
	}
	request := SandboxBackingRequest{started.Record.ID, started.Record.Owner, started.Record.InputSHA256}
	run := func() ([]byte, error) { return command(backingRetirementArgs(request)...) }
	admission := generation + "/capture-delete-admission.json"
	if err := os.Rename(admission, admission+".original"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(); err == nil {
		t.Fatal("unproved stopped-capture backing was retired")
	}
	if err := os.Rename(admission+".original", admission); err != nil {
		t.Fatal(err)
	}
	pages, err := filepath.Glob(generation + "/images/pages-*.img")
	if err != nil || len(pages) == 0 {
		t.Fatal("real capture lacks its page images", err)
	}
	hardlink := status.Store + "/foreign-stopped-capture-image"
	if err := os.Link(pages[0], hardlink); err != nil {
		t.Fatal(err)
	}
	if _, err := run(); err == nil {
		t.Fatal("capture backing with a foreign hardlink was accepted")
	}
	if err := os.Remove(hardlink); err != nil {
		t.Fatal(err)
	}
	output, err := run()
	if err != nil {
		t.Fatal("ordinary stopped-capture retirement", string(output), err)
	}
	for _, path := range []string{"images", "filesystem"} {
		if _, err := os.Lstat(generation + "/" + path); !os.IsNotExist(err) {
			t.Fatal("retirement left private capture backing", path, err)
		}
	}
	for name, before := range evidence {
		after, err := os.ReadFile(generation + "/" + name)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("retirement changed original capture/failure evidence", name, err)
		}
	}
	if replay, err := run(); err != nil || !bytes.Equal(output, replay) {
		t.Fatal("completed retirement failed exact replay", string(replay), err)
	}
	observed := boundedLossView(t, "state", started.Record.ID)
	before, _ := json.Marshal(deleted.Record)
	after, _ := json.Marshal(observed.Record)
	if !bytes.Equal(before, after) {
		t.Fatal("backing retirement changed the original Pause/Delete")
	}
	for _, name := range []string{"checkpoint.json", "capture-result.json"} {
		if _, err := os.Lstat(generation + "/" + name); !os.IsNotExist(err) {
			t.Fatal("cleanup invented a committed checkpoint", name, err)
		}
	}
	successor := generation + "/filesystem"
	if err := os.Mkdir(successor, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(successor+"/foreign", []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := run(); err == nil {
		t.Fatal("completed retirement accepted a foreign successor")
	}
	if data, err := os.ReadFile(successor + "/foreign"); err != nil || string(data) != "preserve" {
		t.Fatal("cleanup damaged a foreign successor", err)
	}
	if err := os.Remove(successor + "/foreign"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(successor); err != nil {
		t.Fatal(err)
	}
	boundedLossSave(t, "stopped-capture-retirement.json", map[string]bool{
		"passed": true, "real_capture_and_stop": true, "confirmed_delete": true,
		"images_and_filesystem_retired": true, "original_evidence_preserved": true,
		"no_checkpoint_manufactured": true, "exact_completion_replayed": true,
		"missing_admission_refused": true, "foreign_successor_preserved": true,
		"original_record_unchanged":        true,
		"unaccepted_preparation_recovered": true,
	})
}
