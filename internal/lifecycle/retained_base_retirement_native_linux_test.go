//go:build linux

package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

const retiredBaseFiles = 4096

func TestNativeRetainedBasePrepare(t *testing.T) {
	TestNativeRetainedSelectionPrepare(t)
	status := boundedLossStatusNow(t)
	input := status.Store + "/unused-base-input"
	if err := os.Mkdir(input, 0700); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < retiredBaseFiles; index++ {
		data := bytes.Repeat([]byte{byte(index%251 + 1)}, 4096)
		if err := os.WriteFile(fmt.Sprintf("%s/%04d", input, index), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	var base Base
	if err := json.Unmarshal(boundedLossAction(t, "prepare-base", "--source", input, "unused-base"), &base); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(status.Lifecycle.Root + "/bases/unused-base/base.json")
	if err != nil {
		t.Fatal(err)
	}
	boundedLossSave(t, "retained-base-prepared.json", map[string]any{
		"base": base, "request": RetainedBaseRequest{status.Lifecycle.Root, base.ID, sum(raw)},
		"payload_bytes": retiredBaseFiles * 4096, "input": input,
	})
}

func TestNativeRetainedBaseRecover(t *testing.T) {
	for name, test := range map[string]func(*testing.T){
		"request-guards":     TestRetainedBaseRetirementRejectsUnboundRequests,
		"checkpoint-guards":  TestRetainedBaseRetirementProtectsCheckpointAndUncertainCapture,
		"deleted-checkpoint": TestRetainedBaseRetirementDoesNotWithdrawDeletedCheckpoint,
	} {
		if !t.Run(name, test) {
			t.FailNow()
		}
	}
	TestNativeRetainedSelectionRecover(t)
	var prepared struct {
		Base         Base                `json:"base"`
		Request      RetainedBaseRequest `json:"request"`
		PayloadBytes uint64              `json:"payload_bytes"`
		Input        string              `json:"input"`
	}
	epochFenceRead(t, "retained-base-prepared.json", &prepared)
	status := boundedLossStatusNow(t)
	arguments := func(request RetainedBaseRequest) []string {
		return []string{"--worker-run-root", "/run/factorybox", "retire-base",
			"--source-root", request.SourceRoot, "--base-sha256", request.BaseSHA256, request.BaseID}
	}
	original, err := os.ReadFile(prepared.Request.SourceRoot + "/bases/" + prepared.Base.ID + "/base.json")
	if err != nil {
		t.Fatal(err)
	}
	wrong := prepared.Request
	wrong.BaseSHA256 = strings.Repeat("0", 64)
	if _, err := boundedLossCommand(arguments(wrong)...); err == nil {
		t.Fatal("foreign base metadata was accepted")
	}
	peer := standbyFixture(t, status, "base-live-peer", nil)
	if _, err := boundedLossCommand(arguments(prepared.Request)...); err == nil {
		t.Fatal("retained base maintenance ran before current execution drained")
	}
	boundedLossAction(t, "delete", peer.Record.ID)
	// A covering mount cannot inherit the original directory's deletion authority.
	if err := unix.Mount(prepared.Base.Tree.Saved, prepared.Base.Tree.Saved, "", unix.MS_BIND, ""); err != nil {
		t.Fatal(err)
	}
	_, refused := boundedLossCommand(arguments(prepared.Request)...)
	unmountErr := unix.Unmount(prepared.Base.Tree.Saved, 0)
	if refused == nil || unmountErr != nil {
		t.Fatal("foreign base covering mount was not preserved", refused, unmountErr)
	}
	link := status.Store + "/foreign-base-link"
	if err := os.Link(prepared.Base.Tree.Saved+"/0000", link); err != nil {
		t.Fatal(err)
	}
	if _, err := boundedLossCommand(arguments(prepared.Request)...); err == nil {
		t.Fatal("base retirement accepted an external hardlink")
	}
	if info, err := os.Stat(link); err != nil || info.Size() != 4096 {
		t.Fatal("refusal changed the foreign hardlink", err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	engine, err := Open(status.Lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	before, err := engine.Capacity()
	if err != nil {
		t.Fatal(err)
	}
	// Stop the actual public unlink process after its first payload disappears.
	command := exec.Command("/opt/factorybox/factorybox", arguments(prepared.Request)...)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	stopped := false
	t.Cleanup(func() {
		if !stopped {
			command.Process.Kill()
			command.Wait()
		}
	})
	relative := baseRetirementPath(prepared.Base.ID)
	intent := prepared.Request.SourceRoot + "/" + relative + "/retirement-intent.json"
	complete := prepared.Request.SourceRoot + "/" + relative + "/retirement-complete.json"
	for deadline := time.Now().Add(20 * time.Second); ; {
		_, accepted := os.Stat(intent)
		_, removed := os.Stat(fmt.Sprintf("%s/%04d", prepared.Base.Tree.Saved, retiredBaseFiles-1))
		if accepted == nil && os.IsNotExist(removed) {
			if err := command.Process.Signal(syscall.SIGSTOP); err != nil {
				t.Fatal(err)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("public base unlink did not reach its actual partial-deletion window", stderr.String())
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := os.Stat(complete); !os.IsNotExist(err) {
		t.Fatal("unlink completed before its actual interruption")
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	command.Wait()
	stopped = true
	foreign := prepared.Base.Tree.Saved + "/foreign-after-interruption"
	if err := os.WriteFile(foreign, []byte("not in the accepted inventory"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := boundedLossCommand(arguments(prepared.Request)...); err == nil {
		t.Fatal("partial base deletion accepted an unowned successor")
	}
	if data, err := os.ReadFile(foreign); err != nil || string(data) != "not in the accepted inventory" {
		t.Fatal("partial retry changed the unowned successor", err)
	}
	if err := os.Remove(foreign); err != nil {
		t.Fatal(err)
	}
	receipt := boundedLossAction(t, arguments(prepared.Request)[2:]...)
	if replay := boundedLossAction(t, arguments(prepared.Request)[2:]...); !bytes.Equal(receipt, replay) {
		t.Fatal("base retirement replay changed its completed result")
	}
	var result BaseRetirement
	// Statfs observes net capacity, including the new compressed inventory and
	// durable completion metadata. It is not the byte size of the old payload.
	if err := json.Unmarshal(receipt, &result); err != nil || result.Request != prepared.Request ||
		result.CompletedAt == "" || result.After.AvailableBytes < before.AvailableBytes+prepared.PayloadBytes-(1<<20) {
		t.Fatal("base retirement did not return its actual allocated capacity", result, err)
	}
	if _, err := os.Stat(prepared.Base.Tree.Saved); !os.IsNotExist(err) {
		t.Fatal("retired base has remaining backing", err)
	}
	after, err := os.ReadFile(prepared.Request.SourceRoot + "/bases/" + prepared.Base.ID + "/base.json")
	if err != nil || !bytes.Equal(original, after) {
		t.Fatal("retirement rewrote the original import identity", err)
	}
	for index := 0; index < retiredBaseFiles; index++ {
		data, err := os.ReadFile(fmt.Sprintf("%s/%04d", prepared.Input, index))
		if err != nil || !bytes.Equal(data, bytes.Repeat([]byte{byte(index%251 + 1)}, 4096)) {
			t.Fatal("retirement changed the original caller input", index, err)
		}
	}
	var pause boundedLossPause
	epochFenceRead(t, "bounded-loss-pause.json", &pause)
	checkpoint := RetainedCheckpointRequest{pause.Request.SourceRoot,
		CheckpointRequest{pause.Request.SourceID, pause.Request.SnapshotID, pause.Request.SourceOwner, pause.Request.CheckpointSHA256}}
	if _, err := engine.InspectRetainedCheckpoint(context.Background(), checkpoint); err != nil {
		t.Fatal("unrelated original committed checkpoint was damaged", err)
	}
	old, err := engine.openRetainedMetadata(prepared.Request.SourceRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	if _, err := old.inventoryEpochPayloads(context.Background()); err != nil {
		t.Fatal("epoch inventory cannot recognize a completed base retirement", err)
	}
	// A path successor after completion never becomes part of the old request.
	if err := os.Mkdir(prepared.Base.Tree.Saved, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := boundedLossCommand(arguments(prepared.Request)...); err == nil {
		t.Fatal("completed replay accepted a replacement base")
	}
	if err := os.Remove(prepared.Base.Tree.Saved); err != nil {
		t.Fatal(err)
	}
	boundedLossSave(t, "retained-base-retired.json", map[string]any{
		"passed": true, "request": prepared.Request, "receipt": result,
		"metadata_guard_tests_passed": true,
		"wrong_metadata_refused":      true, "current_running_protected": true,
		"foreign_mount_preserved": true, "foreign_hardlink_preserved": true,
		"public_unlink_interrupted": true, "foreign_partial_entry_preserved": true,
		"exact_partial_continuation": true, "actual_capacity_returned": true,
		"original_metadata_preserved": true, "caller_input_preserved": true,
		"unrelated_checkpoint_preserved": true, "epoch_inventory_verified": true,
		"exact_completion_replayed": true, "foreign_successor_preserved": true,
		"base_root": filepath.Clean(prepared.Base.Tree.Saved),
	})
}
