//go:build linux

package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"factorybox/internal/ownedtree"
	"factorybox/internal/storefs"
	"factorybox/internal/sysboxcapture"
	"golang.org/x/sys/unix"
)

const mountedImportWindowFiles = 5000

func prepareRecoveryImportMountedFixture(t *testing.T, bundle string) string {
	t.Helper()
	if os.Getenv("FACTORYBOX_NATIVE_IMPORT_MOUNT_INTERRUPTION") != "1" {
		return ""
	}
	var base Base
	if err := json.Unmarshal(boundedLossAction(t, "prepare-base", "--source", bundle+"/lower", "import-mount-base"), &base); err != nil || base.Tree.Immutable == nil {
		t.Fatal("public immutable base preparation", err)
	}
	if err := os.Rename(bundle+"/lower", bundle+"/lower-input"); err != nil {
		t.Fatal(err)
	}
	bound, err := sysboxcapture.BindFilesystemBase(base.Tree, bundle+"/lower")
	if err != nil {
		t.Fatal(err)
	}
	// Registered canonical bases remain the worker exit guard's responsibility.
	// This private fixture alias is released after the overlay cleanup, which
	// the original Prepare test registers after this helper returns.
	t.Cleanup(func() {
		if err := sysboxcapture.RetireImmutableAlias(bundle+"/lower", bound); err != nil {
			t.Error("release only the exact private fixture base alias", err)
		}
	})
	window := bundle + "/upper/import-window"
	if err := os.Mkdir(window, 0700); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < mountedImportWindowFiles; index++ {
		if err := os.WriteFile(fmt.Sprintf("%s/%04d", window, index), []byte(fmt.Sprintf("private-import-window-%04d\n", index)), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return base.ID
}

type mountedImportInterruption struct {
	Passed            bool             `json:"passed"`
	SourceRoot        string           `json:"source_root"`
	TargetID          string           `json:"target_id"`
	Request           RecoveryRequest  `json:"request"`
	Attachment        storefs.Active   `json:"attachment"`
	Original          ownedtree.Plan   `json:"original"`
	PreparationSHA256 string           `json:"preparation_sha256"`
	Mount             ownedtree.Object `json:"mount"`
	ControllerPID     int              `json:"controller_pid"`
	ControllerBirth   string           `json:"controller_birth"`
	CopyStopped       bool             `json:"exact_copy_child_stopped"`
	ExactSIGKILL      bool             `json:"exact_controller_sigkill"`
	MountedRefusal    bool             `json:"mounted_inspection_refused"`
	RefusalError      string           `json:"mounted_inspection_error"`
	IntentAbsent      bool             `json:"intent_and_target_absent"`
}

func TestNativeRecoveryImportMountedInterrupt(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_IMPORT_MOUNT_INTERRUPTION") != "1" {
		t.Skip("owned mounted import interruption fixture only")
	}
	status := boundedLossStatusNow(t)
	var saved boundedLossPause
	data, err := os.ReadFile("/output/bounded-loss-pause.json")
	if err != nil || json.Unmarshal(data, &saved) != nil || !saved.Passed || status.Epoch == saved.Status.Epoch {
		t.Fatal("original committed pause or replacement epoch unavailable", err)
	}
	old, err := os.OpenRoot(saved.Request.SourceRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	original, err := ownedtree.Capture(context.Background(), old, []string{"objects", "forks", "bases", "runtime"})
	if err != nil {
		t.Fatal(err)
	}
	request := saved.Request
	command := exec.Command("/opt/factorybox/factorybox", "--worker-run-root", "/run/factorybox", "recover-paused",
		"--source-root", request.SourceRoot, "--source", request.SourceID, "--snapshot-id", request.SnapshotID,
		"--source-owner", request.SourceOwner, "--checkpoint-sha256", request.CheckpointSHA256, "mounted-import")
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr, command.WaitDelay = &stdout, &stderr, 2*time.Second
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	defer func() {
		if !waited {
			command.Process.Kill()
			command.Wait()
		}
	}()
	fd, err := unix.PidfdOpen(command.Process.Pid, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	birth, err := processBirth(command.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	operation := status.Lifecycle.Root + "/forks/mounted-import"
	basePath := operation + "/recovery/input/base/rootfs"
	var mounted ownedtree.Object
	for deadline := time.Now().Add(30 * time.Second); ; {
		file, err := os.Open(basePath)
		if err == nil {
			object, _, identityErr := initialImportObject(file)
			closeErr := file.Close()
			if identityErr == nil && closeErr == nil && object.MountID != status.Lifecycle.BoundedStore.Mount.MountID {
				copyFD, err := stopCaptureCopyChild(command.Process.Pid)
				if err != nil {
					t.Fatal(err)
				}
				if copyFD >= 0 {
					defer unix.Close(copyFD)
					defer unix.PidfdSendSignal(copyFD, unix.SIGKILL, nil, 0)
					if err := unix.PidfdSendSignal(fd, unix.SIGSTOP, nil, 0); err != nil {
						t.Fatal(err)
					}
					if err := unix.PidfdSendSignal(fd, unix.SIGKILL, nil, 0); err != nil {
						t.Fatal(err)
					}
					if err := unix.PidfdSendSignal(copyFD, unix.SIGKILL, nil, 0); err != nil {
						t.Fatal(err)
					}
					poll := []unix.PollFd{{Fd: int32(copyFD), Events: unix.POLLIN}}
					if n, err := unix.Poll(poll, 5000); err != nil || n != 1 {
						t.Fatal("exact stopped copy child did not exit", n, err)
					}
					mounted = object
					break
				}
			}
		}
		poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		if n, err := unix.Poll(poll, 0); err != nil || n != 0 || time.Now().After(deadline) {
			t.Fatal("actual mounted import copy window was not observed", n, err)
		}
		time.Sleep(time.Millisecond)
	}
	err = command.Wait()
	waited = true
	wait, ok := command.ProcessState.Sys().(syscall.WaitStatus)
	if err == nil || !ok || !wait.Signaled() || wait.Signal() != syscall.SIGKILL {
		t.Fatal("public import did not terminate through its exact pidfd SIGKILL", err)
	}
	for _, path := range []string{operation + "/intent.json", status.Lifecycle.Root + "/objects/mounted-import"} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("interruption happened after public import acceptance", path, err)
		}
	}
	journal, err := os.ReadFile(operation + "/import.json")
	if err != nil {
		t.Fatal(err)
	}
	_, refusal := boundedLossCommand("--worker-run-root", "/run/factorybox", "inspect-recovery-import", "--target", "mounted-import")
	if refusal == nil || !strings.Contains(refusal.Error(), "owned tree crosses foreign mount") {
		t.Fatal("mounted incomplete copy was not refused at its exact mount boundary", refusal)
	}
	file, err := os.Open(basePath)
	if err != nil {
		t.Fatal("refused inspection removed mounted backing", err)
	}
	after, _, statErr := initialImportObject(file)
	if err := errors.Join(statErr, file.Close()); err != nil || after != mounted {
		t.Fatal("refused inspection changed the original import mount", err)
	}
	if err := ownedtree.ValidateComplete(context.Background(), old, original); err != nil {
		t.Fatal("interrupted import changed original backup", err)
	}
	boundedLossSave(t, "recovery-import-mounted-interrupted.json", mountedImportInterruption{Passed: true,
		SourceRoot: status.Lifecycle.Root, TargetID: "mounted-import", Request: request,
		Attachment: *status.Lifecycle.BoundedStore, Original: original, PreparationSHA256: sum(journal), Mount: mounted,
		ControllerPID: command.Process.Pid, ControllerBirth: birth, CopyStopped: true, ExactSIGKILL: true,
		MountedRefusal: true, RefusalError: refusal.Error(), IntentAbsent: true})
}

func cancelRetainedRecoveryImportMountedFixture(t *testing.T, status boundedLossStatus, saved boundedLossPause) {
	t.Helper()
	data, err := os.ReadFile("/output/recovery-import-mounted-interrupted.json")
	var interrupted mountedImportInterruption
	if err != nil || json.Unmarshal(data, &interrupted) != nil || !interrupted.Passed ||
		!interrupted.CopyStopped || !interrupted.ExactSIGKILL || !interrupted.MountedRefusal || !interrupted.IntentAbsent ||
		interrupted.SourceRoot == status.Lifecycle.Root || !reflect.DeepEqual(interrupted.Request, saved.Request) {
		t.Fatal("exact interrupted retained import is unavailable", err)
	}
	proof := map[string]any{"schema": 1, "passed": false, "production_qualified": false,
		"interruption": interrupted, "current_root": status.Lifecycle.Root}
	t.Cleanup(func() {
		proof["full_original_recovery_assertions_passed"], proof["passed"] = !t.Failed(), !t.Failed()
		boundedLossSave(t, "recovery-import-mounted-recovered.json", proof)
	})
	old, err := os.OpenRoot(saved.Request.SourceRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	plan, err := rebindEpochRetirementPlan(interrupted.Original, interrupted.Attachment, *status.Lifecycle.BoundedStore)
	if err != nil {
		t.Fatal("original backup attachment cannot be rebound", err)
	}
	if err := ownedtree.ValidateComplete(context.Background(), old, plan); err != nil {
		t.Fatal("fenced worker loss changed original backup", err)
	}
	data = boundedLossAction(t, "inspect-recovery-import", "--source-root", interrupted.SourceRoot, "--target", interrupted.TargetID)
	var inspected RecoveryImportInspection
	if err := json.Unmarshal(data, &inspected); err != nil || inspected.ReservationAbsent || inspected.Owner == "" || inspected.Cancelled != nil {
		t.Fatal("retained mounted import did not become an exact unpublished file inventory", err)
	}
	journalPath := interrupted.SourceRoot + "/forks/" + interrupted.TargetID + "/import.json"
	journal, err := os.ReadFile(journalPath)
	if err != nil || sum(journal) != interrupted.PreparationSHA256 {
		t.Fatal("original import reservation changed across worker loss", err)
	}
	requestFile := "/output/mounted-import-cancel-request.json"
	if err := os.WriteFile(requestFile, data, 0644); err != nil {
		t.Fatal(err)
	}
	args := []string{"cancel-recovery-import", "--request-file", requestFile}
	completion := boundedLossAction(t, args...)
	if replay := boundedLossAction(t, args...); !bytes.Equal(completion, replay) {
		t.Fatal("retained cancellation replay changed its completion")
	}
	if _, err := os.Lstat(interrupted.SourceRoot + "/forks/" + interrupted.TargetID + "/recovery"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("retained imported payload remains", err)
	}
	after, err := os.ReadFile(journalPath)
	if err != nil || !bytes.Equal(after, journal) {
		t.Fatal("retained cancellation changed its permanent target reservation", err)
	}
	if err := ownedtree.ValidateComplete(context.Background(), old, plan); err != nil {
		t.Fatal("retained cancellation changed original complete backup", err)
	}
	proof["retained_inspection"], proof["cancellation_json"] = inspected, string(completion)
	proof["fenced_retained_controller_cancelled"], proof["permanent_reservation_unchanged"] = true, true
	proof["original_backing_unchanged"], proof["exact_cancellation_replay"], proof["full_original_recovery_follows"] = true, true, true
}
