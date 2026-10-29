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

	"golang.org/x/sys/unix"
)

func backingRetirementArgs(request SandboxBackingRequest) []string {
	return []string{"retire-sandbox", "--sandbox-owner", request.SandboxOwner, "--input-sha256", request.InputSHA256, request.SandboxID}
}

func TestNativeSandboxBackingRetirement(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_BACKING_RETIREMENT") != "1" {
		t.Skip("owned Sandbox backing retirement fixture only")
	}
	// The existing fixture imports a real common base and a private copy
	// window. No fixture assertions or upstream timer tests are relaxed.
	TestNativeBoundedLossPrepare(t)
	status := boundedLossStatusNow(t)
	var original boundedLossPause
	data, err := os.ReadFile("/output/bounded-loss-pause.json")
	if err != nil || json.Unmarshal(data, &original) != nil {
		t.Fatal("original committed Pause", err)
	}
	victim := boundedLossView(t, "fork", "--source", "source", "--snapshot-id", original.Request.SnapshotID, "victim")
	t.Cleanup(func() {
		for _, id := range []string{"victim", "source"} {
			boundedLossAction(t, "delete", id)
		}
	})
	boundedLossActor(t, victim, original.State)
	if victim.Record.Base == nil || victim.Record.InputDirectory == nil {
		t.Fatal("fixture lacks common base or original directory admission")
	}
	request := SandboxBackingRequest{"victim", victim.Record.Owner, victim.Record.InputSHA256}
	command := append([]string{"--worker-run-root", "/run/factorybox"}, backingRetirementArgs(request)...)
	if _, err := boundedLossCommand(command...); err == nil {
		t.Fatal("running Sandbox backing was retired")
	}
	boundedLossAction(t, "delete", "victim")
	bundle := victim.Record.InputBundle
	renamed := bundle + "-original"
	if err := os.Rename(bundle, renamed); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(bundle, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := boundedLossCommand(command...); err == nil {
		t.Fatal("replaced bundle directory was accepted")
	}
	if err := os.Remove(bundle); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(renamed, bundle); err != nil {
		t.Fatal(err)
	}
	engine, err := Open(status.Lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { engine.Close() })
	manager := status.Lifecycle.ManagerRoot + "/docker/" + victim.Record.RuntimeID
	if err := os.MkdirAll(manager, 0700); err != nil {
		t.Fatal(err)
	}
	managerWindow := manager + "/zz-retirement-window"
	if err := os.WriteFile(managerWindow, []byte("private stopped manager data\n"), 0600); err != nil {
		t.Fatal(err)
	}
	managerCancel := cancelResumeRemoval{Context: context.Background(), files: []string{managerWindow}}
	if _, err := engine.RetireDeletedManagerBacking(managerCancel, request); !errors.Is(err, context.Canceled) {
		t.Fatal("actual manager unlink did not stop after partial deletion", err)
	}
	managerForeign := manager + "/foreign-added"
	if err := os.WriteFile(managerForeign, []byte("preserve foreign manager data\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.RetireDeletedBacking(context.Background(), request); err == nil {
		t.Fatal("ordinary cleanup accepted foreign manager data after interruption")
	}
	if data, err := os.ReadFile(managerForeign); err != nil || string(data) != "preserve foreign manager data\n" {
		t.Fatal("ordinary cleanup changed foreign manager data", err)
	}
	if err := os.Remove(managerForeign); err != nil {
		t.Fatal(err)
	}
	window := bundle + "/upper/import-window/0000"
	foreignLink := status.Store + "/foreign-backing-hardlink"
	if err := os.Link(window, foreignLink); err != nil {
		t.Fatal(err)
	}
	if _, err := boundedLossCommand(command...); err == nil {
		t.Fatal("foreign hardlink was accepted")
	}
	if data, err := os.ReadFile(foreignLink); err != nil || len(data) == 0 {
		t.Fatal("foreign hardlink was changed", err)
	}
	if err := os.Remove(foreignLink); err != nil {
		t.Fatal(err)
	}
	// Mount refusal must precede the first unlink.
	if err := unix.Mount("tmpfs", bundle+"/upper", "tmpfs", 0, "size=1m"); err != nil {
		t.Fatal(err)
	}
	_, mountErr := boundedLossCommand(command...)
	if err := unix.Unmount(bundle+"/upper", 0); err != nil {
		t.Fatal(err)
	}
	if mountErr == nil {
		t.Fatal("foreign mounted payload was accepted")
	}
	relative := status.Lifecycle.Root + "/objects/victim/backing-retirement"
	if err := os.MkdirAll(relative, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"retirement-payload.json.gz", ".retirement-reserve"} {
		if err := os.WriteFile(relative+"/"+name, []byte("interrupted ENOSPC preparation"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cancel := cancelResumeRemoval{Context: context.Background(), files: []string{window}}
	if _, err := engine.RetireDeletedBacking(cancel, request); !errors.Is(err, context.Canceled) {
		t.Fatal("actual backing unlink did not stop after partial deletion", err)
	}
	if _, err := os.Lstat(manager); !os.IsNotExist(err) {
		t.Fatal("ordinary backing cleanup retained the original private manager data", err)
	}
	if _, err := os.Stat(relative + "/retirement-intent.json"); err != nil {
		t.Fatal("partial unlink lacks durable intent", err)
	}
	if _, err := os.Stat(relative + "/retirement-complete.json"); !os.IsNotExist(err) {
		t.Fatal("partial retirement falsely completed", err)
	}
	foreign := bundle + "/upper/foreign-added"
	if err := os.WriteFile(foreign, []byte("preserve foreign\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := boundedLossCommand(command...); err == nil {
		t.Fatal("partial continuation accepted foreign data")
	}
	if data, err := os.ReadFile(foreign); err != nil || string(data) != "preserve foreign\n" {
		t.Fatal("foreign partial entry changed", err)
	}
	if err := os.Remove(foreign); err != nil {
		t.Fatal(err)
	}
	response := boundedLossAction(t, backingRetirementArgs(request)...)
	var retired SandboxBackingRetirement
	if err := json.Unmarshal(response, &retired); err != nil || retired.Version != 1 || retired.PayloadKind != "sandbox-backing" ||
		retired.Request != request || retired.CompletedAt == "" || retired.After.AvailableBytes <= retired.Before.AvailableBytes {
		t.Fatal("deleted private backing did not return real capacity", retired, err)
	}
	for _, path := range []string{bundle + "/upper", bundle + "/work", status.Lifecycle.Root + "/forks/victim/materialized/images"} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatal("deleted private payload remains", path, err)
		}
	}
	if replay := boundedLossAction(t, backingRetirementArgs(request)...); !bytes.Equal(response, replay) {
		t.Fatal("exact backing retirement replay changed")
	}
	if err := os.Mkdir(bundle+"/upper", 0700); err != nil {
		t.Fatal(err)
	}
	successor := bundle + "/upper/successor"
	if err := os.WriteFile(successor, []byte("preserve successor\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := boundedLossCommand(command...); err == nil {
		t.Fatal("completed retirement accepted a successor")
	}
	if data, err := os.ReadFile(successor); err != nil || string(data) != "preserve successor\n" {
		t.Fatal("foreign successor changed", err)
	}
	if err := os.Remove(successor); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(bundle + "/upper"); err != nil {
		t.Fatal(err)
	}
	builder := epochInventoryBuilder{engine: engine, ctx: context.Background(),
		previous: *status.Lifecycle.BoundedStore, allowed: map[string]uint32{}}
	deleted := boundedLossView(t, "state", "victim")
	retiredBacking, err := builder.retiredSandboxBacking(deleted.Record)
	if err != nil || !retiredBacking {
		t.Fatal("epoch inventory did not recognize exact completed private retirement", err)
	}
	// This is still a live worker with the common base mounted. Cold epoch
	// inventory must recognize the retired images, then retain its existing
	// covering-mount refusal rather than pretending worker fencing occurred.
	if err := builder.fork(deleted.Record, retiredBacking); err == nil || !strings.Contains(err.Error(), "unexpected covering mount") {
		t.Fatal("cold epoch inventory bypassed the still-live common base fence", err)
	}
	for _, path := range builder.payloads {
		if below(path, "forks/victim/materialized/filesystem/upper") || path == "forks/victim/materialized/images" {
			t.Fatal("epoch inventory selected absent retired backing", path)
		}
	}
	peer := boundedLossView(t, "resume", "--snapshot-id", original.Request.SnapshotID, "source")
	boundedLossActor(t, peer, original.State)
	if data, err := os.ReadFile(bundle + "/config.json"); err != nil || len(data) == 0 {
		t.Fatal("original configuration receipt was lost", err)
	}
	boundedLossSave(t, "backing-retirement.json", map[string]any{
		"passed": true, "actual_capacity_returned": true, "running_protected": true,
		"original_directory_protected": true, "foreign_hardlink_preserved": true, "foreign_mount_refused": true,
		"partial_unlink_interrupted": true, "foreign_partial_entry_preserved": true,
		"exact_partial_continuation": true, "exact_completion_replayed": true,
		"foreign_successor_preserved": true, "common_base_and_peer_preserved": true, "retirement": retired,
		"epoch_metadata_accepts_completed_retirement": true,
		"manager_partial_unlink_interrupted":          true, "foreign_manager_entry_preserved": true,
		"manager_automatically_retired": true, "unaccepted_preparation_automatically_recovered": true,
	})
}
