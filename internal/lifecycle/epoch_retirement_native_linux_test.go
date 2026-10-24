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
	"golang.org/x/sys/unix"
)

type epochRetirementNativeState struct {
	Request       EpochRetirementRequest `json:"request"`
	Accepted      EpochRetirement        `json:"accepted"`
	State         captureActorState      `json:"state"`
	RemovedRoot   string                 `json:"removed_root"`
	WrongRequests int                    `json:"wrong_requests"`
	Base          Base                   `json:"base"`
}

const epochRetirementWindowFiles = 5000

var epochRetirementWindowContent = []byte("owned epoch retirement window\n")

func epochRetirementNativeEnabled(t *testing.T) {
	t.Helper()
	if os.Getenv("FACTORYBOX_NATIVE_EPOCH_RETIREMENT") != "1" {
		t.Skip("owned public epoch retirement fixture only")
	}
}

func epochRetirementCLI(t *testing.T, operation string, request any, name string) ([]byte, error) {
	t.Helper()
	path := "/output/" + name
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	return boundedLossCommand("--worker-run-root", "/run/factorybox", operation, "--request-file", path)
}

func TestNativeEpochRetirementPrepare(t *testing.T) {
	epochRetirementNativeEnabled(t)
	TestNativeRetainedSelectionPrepare(t)
	epochAdmissionNativePrepare(t)
	status := boundedLossStatusNow(t)
	input := status.Store + "/epoch-retirement-base-input"
	for _, directory := range []string{input, input + "/retirement-window"} {
		if err := os.Mkdir(directory, 0755); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < epochRetirementWindowFiles; i++ {
		if err := os.WriteFile(fmt.Sprintf("%s/retirement-window/%04d", input, i), epochRetirementWindowContent, 0644); err != nil {
			t.Fatal(err)
		}
	}
	var base Base
	if err := json.Unmarshal(boundedLossAction(t, "prepare-base", "--source", input, "retirement-base"), &base); err != nil ||
		base.Version != 1 || base.ID != "retirement-base" || base.PreparedAt == "" || base.Tree.Immutable == nil || base.Tree.Entries < epochRetirementWindowFiles {
		t.Fatal("registered base has no complete private input tree", base, err)
	}
	boundedLossSave(t, "epoch-retirement-base.json", base)
}

func TestNativeEpochRetirementRecover(t *testing.T) {
	epochRetirementNativeEnabled(t)
	if !t.Run("full-original-recovery", TestNativeRetainedSelectionRecover) {
		t.FailNow()
	}
	var pause boundedLossPause
	epochFenceRead(t, "bounded-loss-pause.json", &pause)
	var base Base
	epochFenceRead(t, "epoch-retirement-base.json", &base)
	status := boundedLossStatusNow(t)
	engine, err := Open(status.Lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	old, err := engine.openRetainedController(pause.Request.SourceRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	// Keep the exact original 1-GiB profile. The completed original scenario's
	// three obsolete snapshots can be explicitly retired before extending it.
	for _, generation := range []struct{ id, snapshot string }{{"recovered", strings.Repeat("b", 32)}, {"child", strings.Repeat("c", 32)}, {"child", strings.Repeat("d", 32)}} {
		record, err := engine.read(generation.id)
		if err != nil || record.Phase != "deleted" {
			t.Fatal("original descendant not deleted", err)
		}
		relative, _ := checkpointPath(generation.id, generation.snapshot)
		var saved Checkpoint
		if err := engine.readCheckpointJSON(relative+"/checkpoint.json", &saved); err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(saved)
		boundedLossAction(t, retirementArgs("retire-checkpoint", CheckpointRequest{record.ID, saved.ID, record.Owner, sum(data)}, "")...)
	}
	// A private unaccepted preparation marker must not turn an intact pause
	// into an unreadable recovery source when preparation cannot finish.
	if err := old.root.Mkdir(epochRetirementDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	reader, err := old.recoveryLock(pause.Request)
	if err != nil {
		t.Fatal("unaccepted preparation fenced the old recovery reader", err)
	}
	reader.Close()
	oldRequest := CheckpointRequest{pause.Request.SourceID, pause.Request.SnapshotID, pause.Request.SourceOwner, pause.Request.CheckpointSHA256}
	inspect := retirementArgs("inspect-recovery-checkpoint", oldRequest, "")
	inspect = append([]string{inspect[0], "--source-root", old.config.Root}, inspect[1:]...)
	boundedLossAction(t, inspect...)
	if err := old.root.Remove(epochRetirementDirectory); err != nil {
		t.Fatal(err)
	}
	view := boundedLossView(t, "recover-paused", "--source-root", pause.Request.SourceRoot, "--source", pause.Request.SourceID,
		"--snapshot-id", pause.Request.SnapshotID, "--source-owner", pause.Request.SourceOwner, "--checkpoint-sha256", pause.Request.CheckpointSHA256, "handover")
	boundedLossActor(t, view, pause.State)
	state, err := readCaptureActor(view.Runtime.PID, "advance")
	if err != nil || state.Counter != pause.State.Counter+1 {
		t.Fatal("handover did not perform independent useful work", err)
	}
	boundedLossActor(t, view, state)
	request := EpochRetirementRequest{SourceRoot: old.config.Root}
	wrong := 0
	refuse := func(req EpochRetirementRequest, label string) {
		t.Helper()
		before, err := ownedtree.Capture(context.Background(), old.root, []string{"objects", "forks", "bases", "runtime"})
		if err != nil {
			t.Fatal(err)
		}
		if output, err := epochRetirementCLI(t, "begin-epoch-retirement", req, "epoch-wrong-"+label+".json"); err == nil {
			t.Fatal("invalid public epoch retirement accepted", label, string(output))
		}
		after, err := ownedtree.Capture(context.Background(), old.root, before.Paths)
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatal("refused epoch retirement changed old inputs", label, err)
		}
		if _, err := old.root.Lstat(epochRetirementDirectory); !os.IsNotExist(err) {
			t.Fatal("refusal published source preparation", label, err)
		}
		if _, err := os.Lstat(status.Store + "/" + epochRetirementFenceName); !os.IsNotExist(err) {
			t.Fatal("refusal published global maintenance fence", label, err)
		}
		wrong++
	}
	refuse(request, "running-successor")
	snapshot := strings.Repeat("e", 32)
	paused := boundedLossView(t, "pause", "--snapshot-id", snapshot, "handover")
	if err := committedExitPause(paused.Record); err != nil {
		t.Fatal(err)
	}
	parentCheckpoint := CheckpointRequest{"handover", snapshot, paused.Record.Owner, paused.Record.Pause.CheckpointSHA256}
	child := boundedLossView(t, "fork", "--source", "handover", "--snapshot-id", snapshot, "descendant")
	boundedLossActor(t, child, state)
	state, err = readCaptureActor(child.Runtime.PID, "advance")
	if err != nil || state.Counter != pause.State.Counter+2 {
		t.Fatal("handover descendant did not advance independently", err)
	}
	boundedLossActor(t, child, state)
	boundedLossAction(t, "delete", "handover")
	snapshot = strings.Repeat("f", 32)
	paused = boundedLossView(t, "pause", "--snapshot-id", snapshot, "descendant")
	if err := committedExitPause(paused.Record); err != nil {
		t.Fatal(err)
	}
	boundedLossAction(t, retirementArgs("retire-checkpoint", parentCheckpoint, "")...)
	successor := RetainedCheckpointRequest{SourceRoot: status.Lifecycle.Root, CheckpointRequest: CheckpointRequest{"descendant", snapshot, paused.Record.Owner, paused.Record.Pause.CheckpointSHA256}}
	request.Handovers = []EpochRecoveryHandover{{Source: oldRequest, Successor: successor}}
	missing := request
	missing.Handovers = nil
	refuse(missing, "no-handover")
	changed := request
	changed.Handovers = append([]EpochRecoveryHandover(nil), request.Handovers...)
	changed.Handovers[0].Source.SourceOwner = strings.Repeat("0", 32)
	refuse(changed, "wrong-source-owner")
	changed = request
	changed.Handovers = append([]EpochRecoveryHandover(nil), request.Handovers...)
	changed.Handovers[0].Successor.CheckpointSHA256 = strings.Repeat("0", 64)
	refuse(changed, "wrong-successor-hash")
	// A valid active selection still protects the exact old checkpoint.
	relative, _ := oldRequest.path()
	selected := CheckpointSelection{Version: 1, ID: "retirement-protected", Request: oldRequest, SelectedAt: now()}
	if err := old.publishCheckpoint(relative, "selection-retirement-protected.json", selected); err != nil {
		t.Fatal(err)
	}
	refuse(request, "active-selection")
	release := retirementArgs("release-recovery-selection", oldRequest, selected.ID)
	release = append([]string{release[0], "--source-root", old.config.Root}, release[1:]...)
	boundedLossAction(t, release...)
	foreign := relative + "/unrecognized.json"
	if err := old.root.WriteFile(foreign, []byte("keep this unknown input\n"), 0600); err != nil {
		t.Fatal(err)
	}
	refuse(request, "unknown-entry")
	if err := old.root.Remove(foreign); err != nil {
		t.Fatal(err)
	}
	// A current covering mount is foreign even when its complete bytes match.
	images := old.config.Root + "/" + relative + "/images"
	before, err := ownedtree.Capture(context.Background(), old.root, []string{"objects", "forks", "bases", "runtime"})
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Mount(images, images, "", unix.MS_BIND, ""); err != nil {
		t.Fatal(err)
	}
	_, rejected := epochRetirementCLI(t, "begin-epoch-retirement", request, "epoch-wrong-covering-mount.json")
	if err := unix.Unmount(images, 0); err != nil {
		t.Fatal(err)
	}
	if rejected == nil {
		t.Fatal("foreign self-bind over retained RAM images was accepted")
	}
	after, err := ownedtree.Capture(context.Background(), old.root, before.Paths)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("foreign-mount refusal changed original inputs", err)
	}
	wrong++
	foreignOverlay := "/var/lib/sysbox/epoch-retirement-foreign-overlay"
	if err := os.Mkdir(foreignOverlay, 0700); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{"mount", "upper", "work"} {
		if err := os.Mkdir(foreignOverlay+"/"+directory, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := unix.Mount("overlay", foreignOverlay+"/mount", "overlay", 0, "index=off,lowerdir="+old.config.Root+"/"+relative+"/filesystem/lower,upperdir="+foreignOverlay+"/upper,workdir="+foreignOverlay+"/work"); err != nil {
		t.Fatal(err)
	}
	_, rejected = epochRetirementCLI(t, "begin-epoch-retirement", request, "epoch-wrong-foreign-overlay.json")
	if err := unix.Unmount(foreignOverlay+"/mount", 0); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{"mount", "upper", "work/work", "work"} {
		if err := os.Remove(foreignOverlay + "/" + directory); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	if err := os.Remove(foreignOverlay); err != nil {
		t.Fatal(err)
	}
	if rejected == nil {
		t.Fatal("an overlay outside the epoch still using its lower was accepted")
	}
	wrong++
	epochRetirementPreparationENOSPC(t, engine, old, pause.Request, request)
	acceptedBytes, err := epochRetirementCLI(t, "begin-epoch-retirement", request, "epoch-retirement-request.json")
	if err != nil {
		t.Fatal(err, string(acceptedBytes))
	}
	var accepted EpochRetirement
	if err := json.Unmarshal(acceptedBytes, &accepted); err != nil || accepted.Version != 1 || accepted.AcceptedAt == "" || accepted.CompletedAt != "" || !reflect.DeepEqual(accepted.Request, request) {
		t.Fatal("public begin did not durably accept the exact request", err)
	}
	if replay, err := epochRetirementCLI(t, "begin-epoch-retirement", request, "epoch-retirement-request.json"); err != nil || !bytes.Equal(replay, acceptedBytes) {
		t.Fatal("public acceptance replay changed", err)
	}
	acceptedStage, err := ownedtree.Capture(context.Background(), old.root, []string{epochRetirementDirectory})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := boundedLossCommand("--worker-run-root", "/run/factorybox", "inspect-epoch-preparation", "--source-root", old.config.Root); err == nil || !strings.Contains(err.Error(), "accepted") {
		t.Fatal("accepted retirement allowed cancellation inspection", err)
	}
	if _, err := boundedLossCommand("--worker-run-root", "/run/factorybox", "cancel-epoch-preparation", "--request-file", "/output/epoch-preparation-cancellation.json"); err == nil || !strings.Contains(err.Error(), "accepted") {
		t.Fatal("accepted retirement allowed stale preparation cancellation", err)
	}
	if err := ownedtree.ValidateComplete(context.Background(), old.root, acceptedStage); err != nil {
		t.Fatal("accepted cancellation refusal changed metadata", err)
	}
	boundedLossSave(t, "epoch-preparation-accepted-refusals.json", map[string]any{"passed": true, "request": request, "acceptance_sha256": accepted.AcceptanceSHA256,
		"inspection_refused": true, "cancellation_refused": true, "accepted_metadata_unchanged": true})
	// Observe the actual public unlink loop, then stop only this exact child.
	// The registered base supplies enough real owned files for a checked
	// interruption window without changing any original lifecycle assertion.
	removed := base.Tree.Saved + "/retirement-window/4999"
	survivor := base.Tree.Saved + "/retirement-window/0000"
	for _, name := range []string{removed, survivor} {
		if data, err := os.ReadFile(name); err != nil || !bytes.Equal(data, epochRetirementWindowContent) {
			t.Fatal("interruption marker is not an exact original base file", name, err)
		}
	}
	command := exec.Command("/opt/factorybox/factorybox", "--worker-run-root", "/run/factorybox", "retire-epoch", "--request-file", "/output/epoch-retirement-request.json")
	command.WaitDelay = 2 * time.Second
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
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
	pidfd, err := unix.PidfdOpen(command.Process.Pid, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(pidfd)
	start, err := processBirth(command.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(30 * time.Second); ; {
		_, missing := os.Lstat(removed)
		if os.IsNotExist(missing) {
			poll := []unix.PollFd{{Fd: int32(pidfd), Events: unix.POLLIN}}
			if n, err := unix.Poll(poll, 0); err != nil || n != 0 {
				t.Fatal("public retirement exited before its actual unlink interruption", err, stderr.String())
			}
			if err := unix.PidfdSendSignal(pidfd, unix.SIGSTOP, nil, 0); err != nil {
				t.Fatal(err)
			}
			if err := unix.PidfdSendSignal(pidfd, unix.SIGKILL, nil, 0); err != nil {
				t.Fatal(err)
			}
			break
		}
		if missing != nil || time.Now().After(deadline) {
			t.Fatal("actual public unlink window was not observed", missing, stderr.String())
		}
		time.Sleep(time.Millisecond)
	}
	if err := command.Wait(); err == nil {
		t.Fatal("interrupted public retirement returned success", stdout.String())
	}
	waited = true
	if status, ok := command.ProcessState.Sys().(syscall.WaitStatus); !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatal("public retirement did not finish the exact requested SIGKILL", command.ProcessState)
	}
	if data, err := os.ReadFile(survivor); err != nil || !bytes.Equal(data, epochRetirementWindowContent) {
		t.Fatal("public unlink completed before its interruption", err)
	}
	if _, err := old.root.Lstat(epochRetirementDirectory + "/complete.json"); !os.IsNotExist(err) {
		t.Fatal("interruption was after public completion", err)
	}
	if _, err := old.root.Lstat(relative + "/images"); !os.IsNotExist(err) {
		t.Fatal("partial retirement did not remove its exact original RAM root", err)
	}
	if _, err := boundedLossCommand("--worker-run-root", "/run/factorybox", "resume", "--snapshot-id", snapshot, "descendant"); err == nil || !strings.Contains(err.Error(), "epoch retirement") {
		t.Fatal("accepted partial retirement failed to protect its successor", err)
	}
	boundedLossSave(t, "epoch-retirement-interruption.json", map[string]any{"passed": true, "request": request, "acceptance_sha256": accepted.AcceptanceSHA256,
		"pid": command.Process.Pid, "process_start": start, "removed_original_file": removed, "surviving_original_file": survivor,
		"public_unlink_interrupted": true, "exact_child_sigkill": true, "completion_absent": true})
	boundedLossSave(t, "epoch-retirement-accepted.json", epochRetirementNativeState{Request: request, Accepted: accepted, State: state, RemovedRoot: relative + "/images", WrongRequests: wrong, Base: base})
}

func epochRetirementPreparationENOSPC(t *testing.T, current, old *Engine, recovery RecoveryRequest, request EpochRetirementRequest) {
	t.Helper()
	store := epochStoreRoot(current.config.Root)
	before, err := ownedtree.Capture(context.Background(), old.root, []string{"objects", "forks", "bases", "runtime"})
	if err != nil {
		t.Fatal(err)
	}
	pressurePath := store + "/epoch-retirement-owned-pressure"
	pressure, err := os.OpenFile(pressurePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer pressure.Close()
	var capacity unix.Statfs_t
	if err := unix.Fstatfs(int(pressure.Fd()), &capacity); err != nil {
		t.Fatal(err)
	}
	var identity unix.Stat_t
	if err := unix.Fstat(int(pressure.Fd()), &identity); err != nil || current.config.BoundedStore == nil ||
		current.config.BoundedStore.Image.Bytes != 1<<30 || uint64(identity.Dev) != current.config.BoundedStore.BlockDevice ||
		epochStoreRoot(old.config.Root) != store || capacity.Bsize <= 0 || capacity.Blocks == 0 ||
		capacity.Blocks > (1<<30)/uint64(capacity.Bsize) {
		t.Fatal("pressure descriptor is not the exact private 1-GiB image; refusing allocation", err)
	}
	if err := unix.Syncfs(int(pressure.Fd())); err != nil {
		t.Fatal(err)
	}
	if err := unix.Fstatfs(int(pressure.Fd()), &capacity); err != nil {
		t.Fatal(err)
	}
	// Follow actual available blocks, including extent metadata and delayed
	// allocation. Reserved free blocks need not be allocatable by this worker.
	// Leave space for a partial preparation but less than its 1-MiB reserve.
	const remaining = 512 << 10
	if int64(capacity.Bavail)*capacity.Bsize <= remaining {
		t.Fatal("ENOSPC fixture lacks space for its private bounded pressure")
	}
	initial := capacity
	chunk := int64(4 << 20)
	allocations := 0
	for int64(capacity.Bavail)*capacity.Bsize > remaining {
		allocations++
		if allocations > 1024 {
			t.Fatal("bounded pressure allocation did not converge", capacity)
		}
		size := min(chunk, int64(capacity.Bavail)*capacity.Bsize-remaining)
		info, err := pressure.Stat()
		if err != nil {
			t.Fatal(err)
		}
		beforeAvailable := capacity.Bavail
		err = unix.Fallocate(int(pressure.Fd()), 0, info.Size(), size)
		if err != nil && !errors.Is(err, unix.ENOSPC) {
			t.Fatal("allocate only the exact bounded image's private pressure", err)
		}
		if statErr := unix.Fstatfs(int(pressure.Fd()), &capacity); statErr != nil {
			t.Fatal(statErr)
		}
		if errors.Is(err, unix.ENOSPC) && capacity.Bavail >= beforeAvailable {
			if chunk <= capacity.Bsize {
				t.Fatal("reported available blocks are not allocatable", capacity, err)
			}
			chunk = max(capacity.Bsize, chunk/2)
		}
	}
	if err := pressure.Sync(); err != nil {
		t.Fatal(err)
	}
	if err := unix.Fstatfs(int(pressure.Fd()), &capacity); err != nil || int64(capacity.Bavail)*capacity.Bsize > remaining {
		t.Fatal("preparation pressure did not reach its actual physical bound", capacity, err)
	}
	output, rejected := epochRetirementCLI(t, "begin-epoch-retirement", request, "epoch-enospc-request.json")
	if rejected == nil || !strings.Contains(rejected.Error(), unix.ENOSPC.Error()) {
		t.Fatal("real partial preparation did not fail with ENOSPC", rejected, string(output))
	}
	for _, name := range []string{"prepare.json", "intent.json", "complete.json"} {
		if _, err := old.root.Lstat(epochRetirementDirectory + "/" + name); !os.IsNotExist(err) {
			t.Fatal("ENOSPC preparation published an accepted or completed retirement", name, err)
		}
	}
	if _, err := os.Lstat(store + "/" + epochRetirementFenceName); !os.IsNotExist(err) {
		t.Fatal("unaccepted ENOSPC preparation fenced successor work globally", err)
	}
	after, err := ownedtree.Capture(context.Background(), old.root, before.Paths)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("ENOSPC preparation changed the complete original backing", err)
	}
	reader, err := old.recoveryLock(recovery)
	if err != nil {
		t.Fatal("actual unaccepted ENOSPC preparation lost its intact recovery reader", err)
	}
	reader.Close()
	oldRequest := CheckpointRequest{recovery.SourceID, recovery.SnapshotID, recovery.SourceOwner, recovery.CheckpointSHA256}
	inspect := retirementArgs("inspect-recovery-checkpoint", oldRequest, "")
	inspect = append([]string{inspect[0], "--source-root", old.config.Root}, inspect[1:]...)
	boundedLossAction(t, inspect...)
	// Inspect and cancel through the delivered public CLI. The test keeps its
	// independent full inventory only to verify that the public operation
	// removes this stage and preserves every original payload.
	stage, err := ownedtree.Capture(context.Background(), old.root, []string{epochRetirementDirectory})
	if err != nil || len(stage.Entries) < 3 {
		t.Fatal("real ENOSPC did not leave its observed partial preparation", err)
	}
	inspected := boundedLossAction(t, "inspect-epoch-preparation", "--source-root", old.config.Root)
	var inspection map[string]any
	if err := json.Unmarshal(inspected, &inspection); err != nil {
		t.Fatal("public partial-preparation inspection", err)
	}
	var projected struct {
		Version    int            `json:"version"`
		SourceRoot string         `json:"source_root"`
		Inventory  ownedtree.Plan `json:"inventory"`
	}
	if err := json.Unmarshal(inspected, &projected); err != nil || projected.Version != 1 || projected.SourceRoot != old.config.Root || !reflect.DeepEqual(projected.Inventory, stage) {
		t.Fatal("public inspection differs from independently captured exact stage", err)
	}
	refusals := 0
	for _, field := range []string{"inventory_sha256", "source_root", "directory_birth"} {
		var changed map[string]any
		if err := json.Unmarshal(inspected, &changed); err != nil {
			t.Fatal(err)
		}
		changed[field] = strings.Repeat("f", 64)
		if field == "source_root" {
			changed[field] = current.config.Root
		}
		if _, err := epochRetirementCLI(t, "cancel-epoch-preparation", changed, "epoch-preparation-wrong.json"); err == nil {
			t.Fatal("public cancellation accepted altered inspection", field)
		}
		if err := ownedtree.ValidateComplete(context.Background(), old.root, stage); err != nil {
			t.Fatal("wrong cancellation changed original stage", err)
		}
		refusals++
	}
	metadata := epochRetirementDirectory + "/retirement-payload.json.gz"
	originalMetadata, err := old.root.ReadFile(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if err := old.root.WriteFile(metadata, append(append([]byte(nil), originalMetadata...), byte('x')), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := epochRetirementCLI(t, "cancel-epoch-preparation", inspection, "epoch-preparation-wrong.json"); err == nil {
		t.Fatal("changed metadata was cancelled using its old content hash")
	}
	if err := old.root.WriteFile(metadata, originalMetadata, 0600); err != nil {
		t.Fatal(err)
	}
	if err := ownedtree.ValidateComplete(context.Background(), old.root, stage); err != nil {
		t.Fatal("restore only the test's exact unaccepted metadata", err)
	}
	refusals++
	foreign := epochRetirementDirectory + "/foreign-test-input"
	// The deliberately failed reserve allocation can consume every available
	// block. Return one block from this fixture's exact still-open pressure
	// file so the nonempty foreign-input control can exist. Cancellation must
	// still satisfy the original physical pressure bound below.
	pressureInfo, err := pressure.Stat()
	if err != nil || pressureInfo.Size() < capacity.Bsize {
		t.Fatal("foreign-input control has no exact owned pressure block", err)
	}
	if err := pressure.Truncate(pressureInfo.Size() - capacity.Bsize); err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(pressure.Sync(), unix.Syncfs(int(pressure.Fd()))); err != nil {
		t.Fatal(err)
	}
	var controlCapacity unix.Statfs_t
	if err := unix.Fstatfs(int(pressure.Fd()), &controlCapacity); err != nil || controlCapacity.Bavail == 0 ||
		int64(controlCapacity.Bavail)*controlCapacity.Bsize > remaining {
		t.Fatal("foreign-input control lost its bounded pressure or available block", controlCapacity, err)
	}
	file, err := old.root.OpenFile(foreign, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := file.Write([]byte("preserve foreign cancellation input\n"))
	if err := errors.Join(writeErr, file.Sync(), file.Close()); err != nil {
		t.Fatal(err)
	}
	foreignPlan, err := ownedtree.Capture(context.Background(), old.root, []string{foreign})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := boundedLossCommand("--worker-run-root", "/run/factorybox", "inspect-epoch-preparation", "--source-root", old.config.Root); err == nil {
		t.Fatal("public inspection accepted foreign metadata")
	}
	if _, err := epochRetirementCLI(t, "cancel-epoch-preparation", inspection, "epoch-preparation-wrong.json"); err == nil {
		t.Fatal("old inspection removed a stage with foreign input")
	}
	if err := ownedtree.ValidateComplete(context.Background(), old.root, foreignPlan); err != nil {
		t.Fatal("foreign input changed on cancellation refusal", err)
	}
	if err := ownedtree.RemoveRemaining(context.Background(), old.root, foreignPlan); err != nil {
		t.Fatal("remove only this exact test-created foreign input", err)
	}
	refusals++
	// The directory changed during the negative control, so the operator must
	// obtain a fresh complete inspection. Keep the real pressure allocated.
	inspected = boundedLossAction(t, "inspect-epoch-preparation", "--source-root", old.config.Root)
	if err := json.Unmarshal(inspected, &inspection); err != nil {
		t.Fatal(err)
	}
	var atCancellation unix.Statfs_t
	if err := unix.Fstatfs(int(pressure.Fd()), &atCancellation); err != nil || int64(atCancellation.Bavail)*atCancellation.Bsize > remaining {
		t.Fatal("public cancellation no longer has actual bounded pressure", atCancellation, err)
	}
	if output, err := epochRetirementCLI(t, "cancel-epoch-preparation", inspection, "epoch-preparation-cancellation.json"); err != nil {
		t.Fatal("public cancellation of the exact unaccepted stage under actual pressure", err, string(output))
	}
	if _, err := old.root.Lstat(epochRetirementDirectory); !os.IsNotExist(err) {
		t.Fatal("public cancellation left its preparation directory", err)
	}
	if err := pressure.Truncate(0); err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(pressure.Sync(), pressure.Close()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(pressurePath); err != nil {
		t.Fatal(err)
	}
	after, err = ownedtree.Capture(context.Background(), old.root, before.Paths)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("public cancellation changed original saved data", err)
	}
	boundedLossSave(t, "epoch-retirement-enospc.json", map[string]any{"passed": true, "request": request,
		"actual_preparation_enospc": true, "error": rejected.Error(), "free_blocks_after_pressure": capacity.Bfree,
		"available_blocks_before_pressure": initial.Bavail, "available_blocks_after_pressure": capacity.Bavail, "pressure_allocations": allocations,
		"block_bytes": capacity.Bsize, "original_backing_unchanged": true, "intact_recovery_reader_available": true,
		"retained_checkpoint_inspected": true, "no_acceptance_or_completion": true, "global_acceptance_fence_absent": true,
		"partial_stage": stage, "test_owned_partial_stage_cleanup": false, "public_preparation_inspected": true,
		"public_preparation_cancelled": true, "public_cancellation_preserved_original_backing": true,
		"public_cancellation_under_pressure": true, "public_cancellation_refusals": refusals,
		"negative_control_pressure_bytes_released": capacity.Bsize,
		"available_blocks_at_cancellation":         atCancellation.Bavail})
}

func TestNativeEpochRetirementFinish(t *testing.T) {
	epochRetirementNativeEnabled(t)
	var evidence epochRetirementNativeState
	epochFenceRead(t, "epoch-retirement-accepted.json", &evidence)
	status := boundedLossStatusNow(t)
	if status.Lifecycle.Root == evidence.Request.Handovers[0].Successor.SourceRoot {
		t.Fatal("accepted worker was not replaced")
	}
	engine, err := Open(status.Lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	old, err := engine.openRetainedController(evidence.Request.SourceRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	for _, name := range []string{"foreign-after-acceptance.json", epochRetirementDirectory + "/foreign-after-acceptance.json"} {
		before, err := ownedtree.Capture(context.Background(), old.root, []string{"objects", "forks", "bases", "runtime"})
		if err != nil {
			t.Fatal(err)
		}
		content := []byte("preserve unknown input after accepted partial deletion\n")
		if err := old.root.WriteFile(name, content, 0600); err != nil {
			t.Fatal(err)
		}
		output, rejected := epochRetirementCLI(t, "retire-epoch", evidence.Request, "epoch-retirement-request.json")
		if rejected == nil || !strings.Contains(rejected.Error(), "unknown") {
			t.Fatal("unknown accepted-retirement input was not refused", name, rejected, string(output))
		}
		after, err := ownedtree.Capture(context.Background(), old.root, before.Paths)
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatal("refused accepted retirement changed original backing", name, err)
		}
		if saved, err := old.root.ReadFile(name); err != nil || !bytes.Equal(saved, content) {
			t.Fatal("refused retirement changed foreign input", name, err)
		}
		if _, err := os.Lstat(status.Store + "/" + epochRetirementFenceName); err != nil {
			t.Fatal("refused retirement lost its accepted fence", err)
		}
		if err := old.root.Remove(name); err != nil {
			t.Fatal(err)
		}
		evidence.WrongRequests++
	}
	data, err := epochRetirementCLI(t, "retire-epoch", evidence.Request, "epoch-retirement-request.json")
	if err != nil {
		t.Fatal(err, string(data))
	}
	var completed EpochRetirement
	if err := json.Unmarshal(data, &completed); err != nil || completed.Version != 1 || completed.CompletedAt == "" || completed.AcceptanceSHA256 != evidence.Accepted.AcceptanceSHA256 || completed.AcceptedAt != evidence.Accepted.AcceptedAt {
		t.Fatal("public retirement did not complete its original accepted request", err)
	}
	if replay, err := epochRetirementCLI(t, "retire-epoch", evidence.Request, "epoch-retirement-request.json"); err != nil || !bytes.Equal(replay, data) {
		t.Fatal("completed epoch retirement replay changed", err)
	}
	var prepared epochRetirementPreparation
	if err := old.readCheckpointJSON(epochRetirementDirectory+"/prepare.json", &prepared); err != nil {
		t.Fatal(err)
	}
	plan, err := old.readRetirementInventory(epochRetirementDirectory, prepared.InventorySHA256)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Paths) < 3 {
		t.Fatal("public epoch inventory omitted filesystem and RAM payload roots")
	}
	for _, name := range plan.Paths {
		if _, err := old.root.Lstat(name); !os.IsNotExist(err) {
			t.Fatal("retired epoch payload or successor remains", name, err)
		}
	}
	basePath := "bases/" + evidence.Base.ID + "/rootfs"
	foundBase := false
	for _, name := range plan.Paths {
		foundBase = foundBase || name == basePath
	}
	if !foundBase || evidence.Base.Tree.Saved != old.config.Root+"/"+basePath {
		t.Fatal("registered base was not in the exact retired payload inventory")
	}
	for i := 0; i < epochRetirementWindowFiles; i++ {
		name := fmt.Sprintf("%s/retirement-window/%04d", evidence.Base.Source, i)
		if data, err := os.ReadFile(name); err != nil || !bytes.Equal(data, epochRetirementWindowContent) {
			t.Fatal("retirement changed caller-owned base input", name, err)
		}
	}
	var interrupted struct {
		Passed         bool                   `json:"passed"`
		Request        EpochRetirementRequest `json:"request"`
		AcceptanceSHA  string                 `json:"acceptance_sha256"`
		PublicUnlink   bool                   `json:"public_unlink_interrupted"`
		ExactSIGKILL   bool                   `json:"exact_child_sigkill"`
		CompleteAbsent bool                   `json:"completion_absent"`
	}
	epochFenceRead(t, "epoch-retirement-interruption.json", &interrupted)
	if !interrupted.Passed || !interrupted.PublicUnlink || !interrupted.ExactSIGKILL || !interrupted.CompleteAbsent ||
		!reflect.DeepEqual(interrupted.Request, evidence.Request) || interrupted.AcceptanceSHA != completed.AcceptanceSHA256 {
		t.Fatal("public completion lost the exact interrupted unlink request")
	}
	if _, err := os.Lstat(status.Store + "/" + epochRetirementFenceName); !os.IsNotExist(err) {
		t.Fatal("global maintenance fence remains", err)
	}
	if err := old.epochRetirementReadFence(); err == nil {
		t.Fatal("source tombstone no longer fences old recovery")
	}
	successor := evidence.Request.Handovers[0].Successor
	view := boundedLossView(t, "recover-paused", "--source-root", successor.SourceRoot, "--source", successor.SourceID, "--snapshot-id", successor.SnapshotID,
		"--source-owner", successor.SourceOwner, "--checkpoint-sha256", successor.CheckpointSHA256, "successor")
	boundedLossActor(t, view, evidence.State)
	state, err := readCaptureActor(view.Runtime.PID, "advance")
	if err != nil || state.Counter != evidence.State.Counter+1 {
		t.Fatal("independently preserved successor cannot do useful work", err)
	}
	// Retire the next obsolete epoch too. It contains completed imports,
	// forks, resume backing, retired checkpoints and a cold committed pause.
	paused := boundedLossView(t, "pause", "--snapshot-id", strings.Repeat("1", 32), "successor")
	if err := committedExitPause(paused.Record); err != nil {
		t.Fatal(err)
	}
	next := EpochRetirementRequest{SourceRoot: successor.SourceRoot, Handovers: []EpochRecoveryHandover{{Source: successor.CheckpointRequest,
		Successor: RetainedCheckpointRequest{SourceRoot: status.Lifecycle.Root, CheckpointRequest: CheckpointRequest{"successor", strings.Repeat("1", 32), paused.Record.Owner, paused.Record.Pause.CheckpointSHA256}}}}}
	second, err := epochRetirementCLI(t, "retire-epoch", next, "epoch-retirement-second.json")
	if err != nil {
		t.Fatal("second epoch with fork/resume history", err, string(second))
	}
	if replay, err := epochRetirementCLI(t, "retire-epoch", next, "epoch-retirement-second.json"); err != nil || !bytes.Equal(replay, second) {
		t.Fatal("second epoch replay changed", err)
	}
	view = boundedLossView(t, "resume", "--snapshot-id", strings.Repeat("1", 32), "successor")
	boundedLossActor(t, view, state)
	if after, err := readCaptureActor(view.Runtime.PID, "advance"); err != nil || after.Counter != state.Counter+1 {
		t.Fatal("successor cannot work after second epoch deletion", err)
	}
	boundedLossAction(t, "delete", "successor")
	epochAdmissionNativeVerify(t, old)
	boundedLossSave(t, "epoch-retirement-finished.json", map[string]any{"passed": true, "public_epoch_retirement_passed": true, "partial_payloads_removed_before_worker_loss": true,
		"all_authorized_epoch_payloads_absent": true, "source_metadata_preserved": true, "request_replay_exact": true, "independent_successor_recovered_and_useful": true,
		"source_tombstone_retained": true, "global_fence_cleared_exactly": true, "wrong_requests_refused": evidence.WrongRequests,
		"prepared_source_reader_available": true, "unknown_source_entry_refused": true, "foreign_covering_mount_refused": true, "active_selection_preserved": true,
		"full_original_lifecycle_assertions_passed": true, "fork_resume_epoch_retired": true, "descendant_handover_verified": true,
		"foreign_entries_after_acceptance_refused": true, "public_unlink_interrupted": true, "registered_base_retired": true,
		"caller_base_input_preserved": true, "full_epoch_retirement_qualified": false, "report": completed})
}
