//go:build linux

package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

type cancelResumeRemoval struct {
	context.Context
	files []string
}

func (c cancelResumeRemoval) Err() error {
	for _, path := range c.files {
		if _, err := os.Lstat(path); os.IsNotExist(err) {
			return context.Canceled
		} else if err != nil {
			return err
		}
	}
	return nil
}

func TestNativeResumeImageRetirement(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_RESUME_RETIREMENT") != "1" {
		t.Skip("owned resume-image retirement fixture only")
	}
	TestNativeBoundedLossPrepare(t)
	status := boundedLossStatusNow(t)
	var original boundedLossPause
	data, err := os.ReadFile("/output/bounded-loss-pause.json")
	if err != nil || json.Unmarshal(data, &original) != nil {
		t.Fatal("original complete Pause", err)
	}
	request := CheckpointRequest{"source", original.Request.SnapshotID, original.Request.SourceOwner, original.Request.CheckpointSHA256}
	peer := boundedLossView(t, "fork", "--source", "source", "--snapshot-id", request.SnapshotID, "peer")
	t.Cleanup(func() {
		for _, id := range []string{"source", "peer"} {
			if _, err := boundedLossCommand("--worker-run-root", "/run/factorybox", "delete", id); err != nil {
				t.Error("delete exact resume-retirement fixture", id, err)
			}
		}
	})
	boundedLossActor(t, peer, original.State)
	source := boundedLossView(t, "resume", "--snapshot-id", request.SnapshotID, "source")
	boundedLossActor(t, source, original.State)
	boundedLossAction(t, retirementArgs("retire-checkpoint", request, "")...)
	second := strings.Repeat("b", 32)
	paused := boundedLossView(t, "pause", "--snapshot-id", second, "source")
	if paused.Record.Phase != "paused" || paused.Record.Pause == nil || paused.Record.Pause.After.Anon != 0 {
		t.Fatal("new Pause did not release the original processes")
	}
	images := status.Lifecycle.Root + "/objects/source/resumes/" + request.SnapshotID + "/images"
	if info, err := os.Stat(images); err != nil || !info.IsDir() {
		t.Fatal("obsolete private Resume images were already missing", err)
	}
	files, err := os.ReadDir(images)
	if err != nil || len(files) < 2 {
		t.Fatal("Resume fixture lacks its complete image inventory", err)
	}
	linked := status.Store + "/resume-foreign-hardlink"
	if err := os.Link(images+"/"+files[0].Name(), linked); err != nil {
		t.Fatal(err)
	}
	if _, err := boundedLossCommand(append([]string{"--worker-run-root", "/run/factorybox"},
		retirementArgs("retire-resume", request, "")...)...); err == nil {
		t.Fatal("external hardlink was accepted for Resume-image retirement")
	}
	beforeLink, err := os.Stat(images + "/" + files[0].Name())
	afterLink, linkErr := os.Stat(linked)
	if err != nil || linkErr != nil || !os.SameFile(beforeLink, afterLink) {
		t.Fatal("retirement changed the foreign hardlink", err, linkErr)
	}
	if err := os.Remove(linked); err != nil {
		t.Fatal(err)
	}
	var retired CheckpointRetirement
	response := boundedLossAction(t, retirementArgs("retire-resume", request, "")...)
	if err := json.Unmarshal(response, &retired); err != nil || retired.Version != 1 ||
		retired.Request != request || retired.CompletedAt == "" || retired.After.AvailableBytes <= retired.Before.AvailableBytes {
		t.Fatal("obsolete Resume images did not return actual filesystem capacity", retired, err)
	}
	if _, err := os.Lstat(images); !os.IsNotExist(err) {
		t.Fatal("obsolete Resume images remain after retirement", err)
	}
	if replay := boundedLossAction(t, retirementArgs("retire-resume", request, "")...); !bytes.Equal(response, replay) {
		t.Fatal("exact Resume-image retirement replay changed")
	}
	if err := os.Mkdir(images, 0700); err != nil {
		t.Fatal(err)
	}
	successor := images + "/foreign-successor"
	if err := os.WriteFile(successor, []byte("preserve successor\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := boundedLossCommand(append([]string{"--worker-run-root", "/run/factorybox"},
		retirementArgs("retire-resume", request, "")...)...); err == nil {
		t.Fatal("completed retirement accepted a foreign successor")
	}
	if data, err := os.ReadFile(successor); err != nil || string(data) != "preserve successor\n" {
		t.Fatal("retirement changed a successor", err)
	}
	if err := os.Remove(successor); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(images); err != nil {
		t.Fatal(err)
	}
	boundedLossActor(t, boundedLossView(t, "state", "peer"), original.State)
	source = boundedLossView(t, "resume", "--snapshot-id", second, "source")
	boundedLossActor(t, source, original.State)
	current := CheckpointRequest{"source", second, source.Record.Owner, source.Record.Pause.CheckpointSHA256}
	if _, err := boundedLossCommand(append([]string{"--worker-run-root", "/run/factorybox"},
		retirementArgs("retire-resume", current, "")...)...); err == nil || !strings.Contains(err.Error(), "active Resume") {
		t.Fatal("active Resume images were not protected", err)
	}
	boundedLossActor(t, boundedLossView(t, "state", "source"), original.State)
	boundedLossActor(t, boundedLossView(t, "state", "peer"), original.State)
	boundedLossAction(t, retirementArgs("retire-checkpoint", current, "")...)
	third := strings.Repeat("c", 32)
	paused = boundedLossView(t, "pause", "--snapshot-id", third, "source")
	latest := CheckpointRequest{"source", third, paused.Record.Owner, paused.Record.Pause.CheckpointSHA256}
	retirementRefused(t, latest, "unresumed pause")
	partialImages := status.Lifecycle.Root + "/objects/source/resumes/" + second + "/images"
	files, err = os.ReadDir(partialImages)
	if err != nil || len(files) < 2 {
		t.Fatal("partial retirement lacks complete original images", err)
	}
	cancel := cancelResumeRemoval{Context: context.Background()}
	for _, file := range files {
		cancel.files = append(cancel.files, partialImages+"/"+file.Name())
	}
	engine, err := Open(status.Lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { engine.Close() })
	if _, err := engine.RetireResumeImages(cancel, current); !errors.Is(err, context.Canceled) {
		t.Fatal("actual unlink did not stop after partial removal", err)
	}
	if _, err := os.Stat(strings.TrimSuffix(partialImages, "/images") + "/retirement-intent.json"); err != nil {
		t.Fatal("partial deletion has no durable original intent", err)
	}
	if _, err := os.Stat(strings.TrimSuffix(partialImages, "/images") + "/retirement-complete.json"); !os.IsNotExist(err) {
		t.Fatal("interrupted retirement falsely completed", err)
	}
	foreign := partialImages + "/foreign-added"
	if err := os.WriteFile(foreign, []byte("preserve partial\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := boundedLossCommand(append([]string{"--worker-run-root", "/run/factorybox"},
		retirementArgs("retire-resume", current, "")...)...); err == nil || !strings.Contains(err.Error(), "unknown entry") {
		t.Fatal("partial continuation accepted a foreign entry", err)
	}
	if data, err := os.ReadFile(foreign); err != nil || string(data) != "preserve partial\n" {
		t.Fatal("partial continuation changed foreign data", err)
	}
	if err := os.Remove(foreign); err != nil {
		t.Fatal(err)
	}
	boundedLossAction(t, retirementArgs("retire-resume", current, "")...)
	source = boundedLossView(t, "resume", "--snapshot-id", third, "source")
	boundedLossActor(t, source, original.State)
	boundedLossActor(t, boundedLossView(t, "state", "peer"), original.State)
	boundedLossAction(t, "delete", "source")
	last := CheckpointRequest{"source", third, source.Record.Owner, source.Record.Pause.CheckpointSHA256}
	var deleted CheckpointRetirement
	if err := json.Unmarshal(boundedLossAction(t, retirementArgs("retire-resume", last, "")...), &deleted); err != nil ||
		deleted.CompletedAt == "" || deleted.After.AvailableBytes <= deleted.Before.AvailableBytes {
		t.Fatal("confirmed Delete did not release the final Resume images", deleted, err)
	}
	boundedLossActor(t, boundedLossView(t, "state", "peer"), original.State)
	boundedLossSave(t, "resume-retirement.json", map[string]any{
		"passed": true, "actual_capacity_returned": true, "exact_completion_replayed": true,
		"active_resume_protected": true, "new_pause_resumed": true, "peer_preserved": true,
		"foreign_hardlink_preserved": true, "foreign_successor_preserved": true,
		"partial_unlink_interrupted": true, "foreign_partial_entry_preserved": true,
		"exact_partial_continuation": true, "deleted_final_resume_retired": true,
		"retirement": retired,
	})
}
