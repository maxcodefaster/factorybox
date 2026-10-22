//go:build linux

package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"factorybox/internal/sysboxcapture"
	"golang.org/x/sys/unix"
)

func retirementArgs(command string, request CheckpointRequest, selection string) []string {
	args := []string{command, "--snapshot-id", request.SnapshotID, "--source-owner", request.SourceOwner, "--checkpoint-sha256", request.CheckpointSHA256}
	if selection != "" {
		args = append(args, "--selection-id", selection)
	}
	return append(args, request.SourceID)
}

func retirementRefused(t *testing.T, request CheckpointRequest, contains string) {
	t.Helper()
	_, err := boundedLossCommand(append([]string{"--worker-run-root", "/run/factorybox"}, retirementArgs("retire-checkpoint", request, "")...)...)
	if err == nil || !strings.Contains(err.Error(), contains) {
		t.Fatal("retirement did not refuse safely", contains, err)
	}
}

func TestNativeCheckpointRetirement(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_RETIREMENT") != "1" {
		t.Skip("owned delivered retirement fixture only")
	}
	TestNativeBoundedLossPrepare(t)
	status := boundedLossStatusNow(t)
	data, err := os.ReadFile("/output/bounded-loss-pause.json")
	if err != nil {
		t.Fatal(err)
	}
	var pause boundedLossPause
	if err := json.Unmarshal(data, &pause); err != nil {
		t.Fatal(err)
	}
	request := CheckpointRequest{"source", pause.Request.SnapshotID, pause.Request.SourceOwner, pause.Request.CheckpointSHA256}
	retirementRefused(t, request, "unresumed pause")
	selected := boundedLossAction(t, retirementArgs("select-checkpoint", request, "recovery")...)
	if replay := boundedLossAction(t, retirementArgs("select-checkpoint", request, "recovery")...); !bytes.Equal(replay, selected) {
		t.Fatal("selection replay changed")
	}
	source := boundedLossView(t, "resume", "--snapshot-id", request.SnapshotID, "source")
	boundedLossActor(t, source, pause.State)
	retirementRefused(t, request, "protected by recovery selection")
	released := boundedLossAction(t, retirementArgs("release-checkpoint", request, "recovery")...)
	if replay := boundedLossAction(t, retirementArgs("release-checkpoint", request, "recovery")...); !bytes.Equal(replay, released) {
		t.Fatal("release replay changed")
	}
	if _, err := boundedLossCommand(append([]string{"--worker-run-root", "/run/factorybox"}, retirementArgs("select-checkpoint", request, "recovery")...)...); err == nil {
		t.Fatal("released selection was silently reactivated")
	}
	child := boundedLossView(t, "fork", "--source", "source", "--snapshot-id", request.SnapshotID, "child")
	t.Cleanup(func() {
		for _, id := range []string{"grandchild", "child", "source"} {
			if _, err := os.Stat(status.Lifecycle.Root + "/objects/" + id); os.IsNotExist(err) {
				continue
			}
			if _, err := boundedLossCommand("--worker-run-root", "/run/factorybox", "delete", id); err != nil {
				t.Error("delete exact retirement fixture sandbox", id, err)
			}
		}
	})
	boundedLossActor(t, child, pause.State)
	if _, err := readCaptureActor(source.Runtime.PID, "advance"); err != nil {
		t.Fatal(err)
	}
	generation := status.Lifecycle.Root + "/objects/source/captures/" + request.SnapshotID
	if err := unix.Mount("tmpfs", generation+"/images", "tmpfs", unix.MS_NODEV|unix.MS_NOSUID, "size=1m"); err != nil {
		t.Fatal(err)
	}
	foreign, err := directoryIdentity(generation + "/images")
	if err != nil {
		t.Fatal(err)
	}
	foreignMounted := true
	t.Cleanup(func() {
		if !foreignMounted {
			return
		}
		actual, err := directoryIdentity(generation + "/images")
		if err != nil || actual != foreign {
			t.Error("refuse cleanup of changed fixture mount", err)
			return
		}
		if err := unix.Unmount(generation+"/images", 0); err != nil {
			t.Error("release exact negative fixture mount", err)
		}
	})
	retirementRefused(t, request, "checkpoint image count")
	actual, err := directoryIdentity(generation + "/images")
	if err != nil || actual != foreign {
		t.Fatal("retirement changed foreign mount", err)
	}
	if err := unix.Unmount(generation+"/images", 0); err != nil {
		t.Fatal(err)
	}
	foreignMounted = false
	var first CheckpointRetirement
	retired := boundedLossAction(t, retirementArgs("retire-checkpoint", request, "")...)
	if err := json.Unmarshal(retired, &first); err != nil || first.CompletedAt == "" || first.After.AvailableBytes <= first.Before.AvailableBytes {
		t.Fatal("retirement did not observe returned capacity", first, err)
	}
	if replay := boundedLossAction(t, retirementArgs("retire-checkpoint", request, "")...); !bytes.Equal(replay, retired) {
		t.Fatal("retirement replay changed")
	}
	boundedLossActor(t, child, pause.State)
	if replay := boundedLossView(t, "fork", "--source", "source", "--snapshot-id", request.SnapshotID, "child"); !reflect.DeepEqual(replay, child) {
		t.Fatal("independent completed fork still required retired input")
	}
	if _, err := boundedLossCommand("--worker-run-root", "/run/factorybox", "fork", "--source", "source", "--snapshot-id", request.SnapshotID, "retired-new-fork"); err == nil {
		t.Fatal("retired checkpoint accepted a new fork")
	}
	if _, err := os.Lstat(status.Lifecycle.Root + "/forks/retired-new-fork"); !os.IsNotExist(err) {
		t.Fatal("retired checkpoint published new fork backing", err)
	}
	boundedLossView(t, "resume", "--snapshot-id", request.SnapshotID, "source")
	if err := os.Mkdir(generation+"/images", 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(generation+"/images/foreign-successor", []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	retirementRefused(t, request, "unexplained successor")
	if data, err := os.ReadFile(generation + "/images/foreign-successor"); err != nil || string(data) != "preserve" {
		t.Fatal("successor was changed", err)
	}
	if err := os.Remove(generation + "/images/foreign-successor"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(generation + "/images"); err != nil {
		t.Fatal(err)
	}
	boundedLossAction(t, "delete", "source")
	window := child.Record.Rootfs + "/retirement-window"
	if err := os.Mkdir(window, 0755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5000; i++ {
		if err := os.WriteFile(window+"/"+strconv.Itoa(i), []byte("owned retirement window\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := strings.Repeat("b", 32)
	var checkpoint Checkpoint
	if err := json.Unmarshal(boundedLossAction(t, "checkpoint", "--snapshot-id", snapshot, "child"), &checkpoint); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(checkpoint)
	second := CheckpointRequest{"child", snapshot, child.Record.Owner, sum(encoded)}
	grandchild := boundedLossView(t, "fork", "--source", "child", "--snapshot-id", snapshot, "grandchild")
	boundedLossActor(t, grandchild, pause.State)
	args := append([]string{"--worker-run-root", "/run/factorybox"}, retirementArgs("retire-checkpoint", second, "")...)
	command := exec.Command("/opt/factorybox/factorybox", args...)
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
	secondGeneration := status.Lifecycle.Root + "/objects/child/captures/" + snapshot
	removed := secondGeneration + "/filesystem/upper/retirement-window/999"
	for deadline := time.Now().Add(30 * time.Second); ; {
		var intent checkpointRetirementIntent
		data, readErr := os.ReadFile(secondGeneration + "/retirement-intent.json")
		_, missing := os.Lstat(removed)
		if readErr == nil && json.Unmarshal(data, &intent) == nil && intent.Request == second && os.IsNotExist(missing) {
			poll := []unix.PollFd{{Fd: int32(pidfd), Events: unix.POLLIN}}
			if n, err := unix.Poll(poll, 0); err != nil || n != 0 {
				t.Fatal("retirement finished before exact interruption window", err, stderr.String())
			}
			if err := unix.PidfdSendSignal(pidfd, unix.SIGSTOP, nil, 0); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Lstat(secondGeneration + "/retirement-complete.json"); !os.IsNotExist(err) {
				t.Fatal("interruption was after completed retirement", err)
			}
			if err := unix.PidfdSendSignal(pidfd, unix.SIGKILL, nil, 0); err != nil {
				t.Fatal(err)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("actual partial deletion was not observed", readErr, missing, stderr.String())
		}
		time.Sleep(time.Millisecond)
	}
	if err := command.Wait(); err == nil {
		t.Fatal("killed retirement returned success")
	}
	waited = true
	foreignPath := secondGeneration + "/filesystem/upper/retirement-window/foreign-added"
	if err := os.WriteFile(foreignPath, []byte("leave unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	retirementRefused(t, second, "unknown entry")
	if data, err := os.ReadFile(foreignPath); err != nil || string(data) != "leave unchanged" {
		t.Fatal("partial retry changed foreign data", err)
	}
	if err := os.Remove(foreignPath); err != nil {
		t.Fatal(err)
	}
	var completed CheckpointRetirement
	if err := json.Unmarshal(boundedLossAction(t, retirementArgs("retire-checkpoint", second, "")...), &completed); err != nil || completed.CompletedAt == "" {
		t.Fatal("partial retirement could not complete", err)
	}
	boundedLossActor(t, grandchild, pause.State)
	boundedLossAction(t, "delete", "child")
	if continued, err := readCaptureActor(grandchild.Runtime.PID, "advance"); err != nil || continued.Counter != 1 {
		t.Fatal("retirement damaged independent useful descendant", err)
	}
	boundedLossAction(t, "delete", "grandchild")
	boundedLossSave(t, "retirement.json", map[string]any{"schema": 1, "passed": true, "production_qualified": false,
		"unresumed_pause_protected": true, "selection_protected": true, "explicit_release_replayed": true, "released_selection_not_reactivated": true,
		"foreign_mount_preserved": true, "source_resume_preserved": true, "independent_fork_preserved": true, "new_fork_refused": true,
		"exact_completion_replayed": true, "foreign_successor_preserved": true, "actual_controller_sigkill": command.Process.Pid,
		"partial_retry_preserved_foreign": true, "partial_retry_completed": true, "descendant_useful_after_source_delete": true,
		"first": first, "interrupted": completed})
}

func TestNativeCheckpointRetirementImmutableAlias(t *testing.T) {
	testNativeCheckpointRetirementImmutableAlias(t, false)
}

func TestNativeCheckpointRetirementBaseActor(t *testing.T) {
	testNativeCheckpointRetirementImmutableAlias(t, true)
}

func testNativeCheckpointRetirementImmutableAlias(t *testing.T, actor bool) {
	if os.Getenv("FACTORYBOX_NATIVE_RETIREMENT") != "1" {
		t.Skip("owned delivered retirement fixture only")
	}
	status := boundedLossStatusNow(t)
	baseID, sourceID, bundleName, proofName := "retirement-base", "base-source", "base-bundle", "retirement-base.json"
	if actor {
		baseID, sourceID, bundleName, proofName = "retirement-actor-base", "base-actor", "base-actor-bundle", "retirement-base-actor.json"
	}
	var base Base
	if err := json.Unmarshal(boundedLossAction(t, "prepare-base", "--source", "/busybox", baseID), &base); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := sysboxcapture.VerifyImmutableTree(base.Tree.Saved, base.Tree); err != nil {
			t.Error(err)
			return
		}
		if err := unix.Unmount(base.Tree.Saved, 0); err != nil {
			t.Error("unmount only the exact fixture base after all assertions", err)
		}
	})
	bundle := status.Store + "/" + bundleName
	if err := os.Mkdir(bundle, 0700); err != nil {
		t.Fatal(err)
	}
	bound, err := sysboxcapture.BindFilesystemBase(base.Tree, bundle+"/lower")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := sysboxcapture.RetireImmutableAlias(bundle+"/lower", bound); err != nil {
			t.Error(err)
		}
	})
	for _, name := range []string{"upper", "work", "rootfs"} {
		if err := os.Mkdir(bundle+"/"+name, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := unix.Mount("overlay", bundle+"/rootfs", "overlay", 0, "index=off,lowerdir="+bundle+"/lower,upperdir="+bundle+"/upper,workdir="+bundle+"/work"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := unix.Unmount(bundle+"/rootfs", 0); err != nil {
			t.Error(err)
		}
	})
	if out, err := exec.Command(status.Lifecycle.Runtime, "spec", "--bundle", bundle).CombinedOutput(); err != nil {
		t.Fatal(string(out), err)
	}
	data, err := os.ReadFile(bundle + "/config.json")
	if err != nil {
		t.Fatal(err)
	}
	var spec map[string]any
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatal(err)
	}
	process := spec["process"].(map[string]any)
	process["terminal"] = false
	process["cwd"] = "/"
	process["args"] = []string{"/bin/sh", "-c", "exec sleep 300 </dev/null >/dev/null 2>&1"}
	if actor {
		binary, err := os.ReadFile(os.Args[0])
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(bundle+"/rootfs/bin/capture-actor", binary, 0755); err != nil {
			t.Fatal(err)
		}
		process["args"] = []string{"/bin/sh", "-c", "exec /bin/env FACTORYBOX_CAPTURE_ACTOR=1 /bin/capture-actor -test.run=^TestCaptureActor$ -test.timeout=0 </dev/null >/dev/null 2>&1"}
	}
	process["env"] = []string{"PATH=/bin:/sbin:/usr/bin:/usr/sbin", "HOME=/root"}
	spec["root"].(map[string]any)["readonly"] = false
	spec["annotations"] = map[string]string{baseAnnotation: base.ID}
	spec["linux"].(map[string]any)["resources"] = map[string]any{"pids": map[string]any{"limit": 64}, "devices": []map[string]any{{"allow": true, "access": "rwm"}}}
	data, err = json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundle+"/config.json", data, 0600); err != nil {
		t.Fatal(err)
	}
	source := boundedLossView(t, "start", "--bundle", bundle, sourceID)
	t.Cleanup(func() {
		if _, err := boundedLossCommand("--worker-run-root", "/run/factorybox", "delete", sourceID); err != nil {
			t.Error(err)
		}
	})
	watch, cancelWatch := context.WithCancel(context.Background())
	defer cancelWatch()
	exitStat := make(chan string, 1)
	go func() {
		for watch.Err() == nil {
			data, err := os.ReadFile("/proc/" + strconv.Itoa(source.Runtime.PID) + "/stat")
			if err != nil {
				return
			}
			end := strings.LastIndexByte(string(data), ')')
			if end >= 0 && strings.HasPrefix(string(data[end+1:]), " Z ") {
				exitStat <- string(data)
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()
	time.Sleep(100 * time.Millisecond)
	beforeCheckpoint := boundedLossView(t, "state", sourceID)
	commandLine, commandErr := os.ReadFile("/proc/" + strconv.Itoa(source.Runtime.PID) + "/cmdline")
	t.Logf("[DEBUG-retirement-base] before-checkpoint=%+v cmdline=%q err=%v", beforeCheckpoint.Runtime, commandLine, commandErr)
	if beforeCheckpoint.Runtime == nil || beforeCheckpoint.Runtime.PID != source.Runtime.PID || beforeCheckpoint.Runtime.Status != "running" {
		t.Fatalf("base consumer died before checkpoint: start=%+v before-checkpoint=%+v", source.Runtime, beforeCheckpoint.Runtime)
	}
	beforeSyscall, _ := os.ReadFile("/proc/" + strconv.Itoa(source.Runtime.PID) + "/syscall")
	if err := captureNativeFreeze(source.Record, true); err != nil {
		t.Fatal(err)
	}
	controlFrozen := true
	defer func() {
		if controlFrozen {
			if err := captureNativeFreeze(source.Record, false); err != nil {
				t.Error("release exact fixture control freeze", err)
			}
		}
	}()
	if !actor {
		stack, stackErr := os.ReadFile("/proc/" + strconv.Itoa(source.Runtime.PID) + "/stack")
		t.Logf("[DEBUG-retirement-base] frozen-stack=%s err=%v", stack, stackErr)
		if _, err := os.Stat("/timer-probe.py"); err == nil {
			probe := exec.Command("python3", "/timer-probe.py", strconv.Itoa(source.Runtime.PID))
			output, probeErr := probe.CombinedOutput()
			t.Logf("[DEBUG-retirement-base] read-only-restart-probe=%s err=%v", output, probeErr)
			if probeErr != nil {
				t.Fatal("read-only kernel restart diagnostic failed")
			}
			var observed struct {
				Names        []string `json:"restart_function_names"`
				Clock        int      `json:"clock_id"`
				BirthMatches bool     `json:"birth_matches"`
				Expires      int64    `json:"expires_ns"`
				Now          int64    `json:"observed_monotonic_ns"`
			}
			if err := json.Unmarshal(output, &observed); err != nil || !observed.BirthMatches ||
				!reflect.DeepEqual(observed.Names, []string{"hrtimer_nanosleep_restart"}) || observed.Clock != 1 ||
				observed.Expires-observed.Now < int64(290*time.Second) || observed.Expires-observed.Now > int64(300*time.Second) {
				t.Fatal("restart diagnostic did not identify the exact sleeping task and remaining deadline", err)
			}
			nested := exec.Command("unshare", "--pid", "--fork", "--mount-proc", "python3", "/timer-probe.py", "--own-init-control", "1")
			other, nestedErr := nested.CombinedOutput()
			t.Logf("[DEBUG-retirement-base] same-pid-different-namespace-control=%s err=%v", other, nestedErr)
			if err := json.Unmarshal(other, &observed); err != nil || nestedErr != nil || !observed.BirthMatches ||
				reflect.DeepEqual(observed.Names, []string{"hrtimer_nanosleep_restart"}) {
				t.Fatal("restart diagnostic adopted the other namespace's sleeping PID 1", err, nestedErr)
			}
		}
	}
	time.Sleep(100 * time.Millisecond)
	if err := captureNativeFreeze(source.Record, false); err != nil {
		t.Fatal(err)
	}
	controlFrozen = false
	time.Sleep(100 * time.Millisecond)
	control := boundedLossView(t, "state", sourceID)
	afterFreezeSyscall, _ := os.ReadFile("/proc/" + strconv.Itoa(source.Runtime.PID) + "/syscall")
	t.Logf("[DEBUG-retirement-base] freeze-control=%+v syscall-before=%s syscall-after=%s", control.Runtime, beforeSyscall, afterFreezeSyscall)
	if control.Runtime == nil || control.Runtime.PID != source.Runtime.PID || control.Runtime.Status != "running" {
		t.Fatal("freeze-only control changed sleep")
	}
	if !actor {
		probe := exec.Command("chroot", bundle+"/rootfs", "/bin/sleep", "0.01")
		output, err := probe.CombinedOutput()
		t.Logf("[DEBUG-retirement-base] decimal-sleep-control=%q err=%v", output, err)
	}
	observedTimespec := make(chan string, 32)
	if fields := strings.Fields(string(afterFreezeSyscall)); !actor && len(fields) >= 5 {
		address, err := strconv.ParseUint(fields[3], 0, 64)
		if err != nil {
			t.Fatal(err)
		}
		memory, err := os.Open("/proc/" + strconv.Itoa(source.Runtime.PID) + "/mem")
		if err != nil {
			t.Fatal(err)
		}
		defer memory.Close()
		go func() {
			var last string
			for watch.Err() == nil {
				data := make([]byte, 16)
				if _, err := memory.ReadAt(data, int64(address)); err != nil {
					return
				}
				value := fmt.Sprintf("%x", data)
				if value != last {
					select {
					case observedTimespec <- value:
					default:
					}
					last = value
				}
				time.Sleep(time.Millisecond)
			}
		}()
	}
	snapshot := strings.Repeat("c", 32)
	var actorState captureActorState
	if actor {
		snapshot = strings.Repeat("d", 32)
		var err error
		actorState, err = readCaptureActor(source.Runtime.PID, "get")
		if err != nil {
			t.Fatal(err)
		}
	}
	var checkpoint Checkpoint
	if err := json.Unmarshal(boundedLossAction(t, "checkpoint", "--snapshot-id", snapshot, sourceID), &checkpoint); err != nil {
		t.Fatal(err)
	}
	identity, _ := json.Marshal(checkpoint)
	request := CheckpointRequest{sourceID, snapshot, source.Record.Owner, sum(identity)}
	beforeRetirement := boundedLossView(t, "state", sourceID)
	dumpLog, dumpErr := os.ReadFile(status.Lifecycle.Root + "/objects/" + sourceID + "/captures/" + snapshot + "/images/dump.log")
	if dumpErr != nil {
		t.Fatal(dumpErr)
	}
	for _, line := range strings.Split(string(dumpLog), "\n") {
		if strings.Contains(line, "interrupted") || strings.Contains(line, "Warn") {
			t.Logf("[DEBUG-retirement-base] criu=%s", line)
		}
	}
	t.Logf("[DEBUG-retirement-base] start=%+v after-checkpoint=%+v", source.Runtime, beforeRetirement.Runtime)
	for len(observedTimespec) > 0 {
		t.Logf("[DEBUG-retirement-base] requested-remaining-timespec=%s", <-observedTimespec)
	}
	select {
	case stat := <-exitStat:
		t.Logf("[DEBUG-retirement-base] exact-source-exit-stat=%s", stat)
	default:
	}
	if beforeRetirement.Runtime == nil || beforeRetirement.Runtime.PID != source.Runtime.PID || beforeRetirement.Runtime.Status != "running" {
		t.Fatalf("checkpoint already changed live base consumer: start=%+v after-checkpoint=%+v", source.Runtime, beforeRetirement.Runtime)
	}
	var completed CheckpointRetirement
	if err := json.Unmarshal(boundedLossAction(t, retirementArgs("retire-checkpoint", request, "")...), &completed); err != nil || completed.CompletedAt == "" {
		t.Fatal("immutable alias retirement failed", err)
	}
	if err := sysboxcapture.VerifyImmutableTree(base.Tree.Saved, base.Tree); err != nil {
		t.Fatal("retirement damaged shared immutable base", err)
	}
	current := boundedLossView(t, "state", sourceID)
	if current.Runtime == nil || current.Runtime.PID != source.Runtime.PID || current.Runtime.Status != "running" {
		t.Fatalf("retirement changed live base consumer: before=%+v after=%+v", source.Runtime, current.Runtime)
	}
	if actor {
		actual, err := readCaptureActor(current.Runtime.PID, "get")
		if err != nil || !reflect.DeepEqual(actual, actorState) {
			t.Fatal("base retirement changed private actor RAM", err)
		}
		if advanced, err := readCaptureActor(current.Runtime.PID, "advance"); err != nil || advanced.Counter != actorState.Counter+1 {
			t.Fatal("base retirement damaged useful actor", err)
		}
	}
	boundedLossSave(t, proofName, map[string]any{"schema": 1, "passed": true, "exact_alias_retired": true, "shared_base_preserved": true,
		"source_running": true, "retirement": completed})
}
