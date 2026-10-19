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
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"factorybox/internal/sysboxcapture"
	"golang.org/x/sys/unix"
)

func forkNativeCLI(engine *Engine, source, snapshot, target string) (View, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/factorybox", "--root", engine.config.Root,
		"--runtime", engine.config.Runtime, "--runtime-sha256", engine.config.RuntimeSHA256,
		"--criu", engine.config.Criu, "--criu-sha256", engine.config.CriuSHA256,
		"--manager-root", engine.config.ManagerRoot, "fork", "--source", source, "--snapshot-id", snapshot, target)
	command.WaitDelay = 2 * time.Second
	output, err := command.CombinedOutput()
	var view View
	if err != nil {
		return view, fmt.Errorf("public fork: %w: %s", err, output)
	}
	err = json.Unmarshal(output, &view)
	return view, err
}

func forkNative(t *testing.T, engine *Engine, source Record, snapshot, target string, expected captureActorState) View {
	t.Helper()
	started := time.Now()
	view, err := forkNativeCLI(engine, source.ID, snapshot, target)
	if err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(started)
	t.Cleanup(func() {
		if _, err := engine.Delete(context.Background(), target); err != nil {
			t.Error("delete exact public fork", err)
		}
	})
	if view.Runtime == nil || view.Runtime.PID <= 1 || view.Runtime.ID == source.RuntimeID ||
		view.Record.Fork == nil || view.Record.Fork.SourceID != source.ID ||
		view.Record.Fork.CheckpointID != snapshot || view.Record.Start.CompletedAt == "" {
		t.Fatal("public fork did not publish its independent identity", view.Record.ID)
	}
	state, err := readCaptureActor(view.Runtime.PID, "get")
	if err != nil || !reflect.DeepEqual(state, expected) {
		t.Fatal("first useful RAM request did not observe the complete captured state", err)
	}
	t.Logf("individual full public CLI fork for %s=%s; first useful RAM response=%s; includes checkpoint verification, complete filesystem/image copies, mount, persisted intent, restore and receipt reconciliation; not a developer-workload or p95 benchmark",
		target, elapsed, time.Since(started))
	replayed, err := forkNativeCLI(engine, source.ID, snapshot, target)
	if err != nil || !reflect.DeepEqual(replayed, view) {
		t.Fatal("public fork replay replaced its target or changed its original result", err)
	}
	t.Run(target+"-without-source-checkpoint", func(t *testing.T) {
		generation := engine.config.Root + "/objects/" + source.ID + "/captures/" + snapshot
		offline := generation + "-offline"
		if err := os.Rename(generation, offline); err != nil {
			t.Fatal(err)
		}
		moved := true
		defer func() {
			if moved {
				if err := os.Rename(offline, generation); err != nil {
					t.Error(err)
				}
			}
		}()
		replayed, err := forkNativeCLI(engine, source.ID, snapshot, target)
		if err != nil || !reflect.DeepEqual(replayed, view) {
			t.Fatal("completed fork still required its source checkpoint", err)
		}
		assertRestoredNative(t, engine, view, expected)
		if err := os.Rename(offline, generation); err != nil {
			t.Fatal(err)
		}
		moved = false
	})
	t.Run(target+"-foreign-rootfs-mount", func(t *testing.T) {
		path := view.Record.Rootfs
		if err := unix.Mount(path, path, "", unix.MS_BIND, ""); err != nil {
			t.Fatal(err)
		}
		mounted := true
		defer func() {
			if mounted {
				if err := unix.Unmount(path, 0); err != nil {
					t.Error(err)
				}
			}
		}()
		_, rejected := engine.Delete(context.Background(), target)
		if err := unix.Unmount(path, 0); err != nil {
			t.Fatal(err)
		}
		mounted = false
		if rejected == nil || !strings.Contains(rejected.Error(), "mount identity changed") {
			t.Fatal("fork deletion accepted a foreign mount of its same rootfs inode", rejected)
		}
		assertRestoredNative(t, engine, view, expected)
	})
	t.Run(target+"-lost-controller-outcome", func(t *testing.T) {
		lost := view.Record
		lost.Phase, lost.RuntimeCreated = "starting", ""
		lost.Start.CompletedAt, lost.Start.Error = "", "controller response lost"
		if err := engine.write(lost); err != nil {
			t.Fatal(err)
		}
		recovered, err := forkNativeCLI(engine, source.ID, snapshot, target)
		if err != nil || !reflect.DeepEqual(recovered, view) {
			t.Fatal("fork receipt did not recover the original completed target", err)
		}
	})
	t.Run(target+"-reject-missing-or-wrong-receipt", func(t *testing.T) {
		path := engine.config.Root + "/objects/" + target + "/restore-result.json"
		original, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(path, path+".held"); err != nil {
			t.Fatal(err)
		}
		_, missing := forkNativeCLI(engine, source.ID, snapshot, target)
		if err := os.Rename(path+".held", path); err != nil {
			t.Fatal(err)
		}
		var result restoreResult
		if err := json.Unmarshal(original, &result); err != nil {
			t.Fatal(err)
		}
		result.ProcessStart = "1"
		changed, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, changed, 0600); err != nil {
			t.Fatal(err)
		}
		_, wrong := forkNativeCLI(engine, source.ID, snapshot, target)
		if err := os.WriteFile(path, original, 0600); err != nil {
			t.Fatal(err)
		}
		if missing == nil || wrong == nil {
			t.Fatal("fork replay accepted unconfirmed or foreign process completion", missing, wrong)
		}
		assertRestoredNative(t, engine, view, expected)
	})
	if _, err := forkNativeCLI(engine, source.ID, strings.Repeat("9", 32), target); err == nil {
		t.Fatal("fork target ID accepted a different checkpoint request")
	}
	if _, err := engine.Start(context.Background(), target, view.Record.InputBundle); err == nil {
		t.Fatal("ordinary start replaced a fork request")
	}
	return view
}

func forkFailuresNative(t *testing.T, engine *Engine, source, snapshot string) {
	t.Helper()
	ctx := context.Background()
	t.Run("partial-preparation-no-reuse", func(t *testing.T) {
		lock, err := engine.lock()
		if err != nil {
			t.Fatal(err)
		}
		intent, err := engine.newForkIntent(ctx, source, snapshot, "partial")
		if err != nil {
			lock.Close()
			t.Fatal(err)
		}
		if err := engine.root.Mkdir("forks/partial", 0700); err != nil {
			lock.Close()
			t.Fatal(err)
		}
		if err := engine.publishCheckpoint("forks/partial", "intent.json", intent); err != nil {
			lock.Close()
			t.Fatal(err)
		}
		if err := engine.root.Mkdir("forks/partial/materialized", 0700); err != nil {
			lock.Close()
			t.Fatal(err)
		}
		if err := engine.writeForkFile("forks/partial/materialized", "retained-partial-input", []byte("preserve\n")); err != nil {
			lock.Close()
			t.Fatal(err)
		}
		if err := lock.Close(); err != nil {
			t.Fatal(err)
		}
		_, rejected := forkNativeCLI(engine, source, snapshot, "partial")
		if rejected == nil || !strings.Contains(rejected.Error(), "refusing to overwrite") {
			t.Fatal("partial fork preparation was silently repeated", rejected)
		}
		data, err := engine.root.ReadFile("forks/partial/materialized/retained-partial-input")
		if err != nil || string(data) != "preserve\n" {
			t.Fatal("partial fork evidence was replaced", err)
		}
		if _, err := engine.root.Lstat("runtime/fbx-" + intent.Owner); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("partial preparation created a runtime", err)
		}
		if _, err := engine.Start(ctx, "partial", engine.config.Root+"/forks/partial/materialized"); err == nil {
			t.Fatal("ordinary start reused an incomplete fork's reserved identity")
		}
	})
	t.Run("unconfirmed-restore-no-second-create", func(t *testing.T) {
		lock, err := engine.lock()
		if err != nil {
			t.Fatal(err)
		}
		intent, err := engine.newForkIntent(ctx, source, snapshot, "uncertain-fork")
		if err != nil {
			lock.Close()
			t.Fatal(err)
		}
		operation := "forks/uncertain-fork"
		if err := engine.root.Mkdir(operation, 0700); err != nil {
			lock.Close()
			t.Fatal(err)
		}
		if err := engine.publishCheckpoint(operation, "intent.json", intent); err != nil {
			lock.Close()
			t.Fatal(err)
		}
		bundle := engine.config.Root + "/" + operation + "/materialized/filesystem"
		if err := os.MkdirAll(bundle+"/rootfs", 0700); err != nil {
			lock.Close()
			t.Fatal(err)
		}
		record, err := engine.prepareRecord("uncertain-fork", bundle, "objects/uncertain-fork",
			intent.OCI, intent.Owner, &intent.Origin)
		if err != nil {
			lock.Close()
			t.Fatal(err)
		}
		if err := engine.prepareCgroup(&record); err != nil {
			lock.Close()
			t.Fatal(err)
		}
		record.Start.Attempted, record.Start.Error = true, "restore worker outcome lost"
		if err := engine.write(record); err != nil {
			lock.Close()
			t.Fatal(err)
		}
		if err := lock.Close(); err != nil {
			t.Fatal(err)
		}
		_, rejected := forkNativeCLI(engine, source, snapshot, record.ID)
		if rejected == nil || !strings.Contains(rejected.Error(), "refusing another restore") {
			t.Fatal("unconfirmed restore was issued again", rejected)
		}
		if _, err := engine.root.Lstat("runtime/" + record.RuntimeID); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("uncertain replay created a runtime", err)
		}
		after, err := engine.read(record.ID)
		if err != nil || !reflect.DeepEqual(after, record) {
			t.Fatal("uncertain replay discarded its original evidence", err)
		}
	})
}

func forkControllerLossNative(t *testing.T, engine *Engine, source Record, snapshot string, expected captureActorState) {
	t.Helper()
	t.Run("actual-controller-loss-after-restore", func(t *testing.T) {
		target := "controller-loss"
		command := exec.Command("/factorybox", "--root", engine.config.Root,
			"--runtime", engine.config.Runtime, "--runtime-sha256", engine.config.RuntimeSHA256,
			"--criu", engine.config.Criu, "--criu-sha256", engine.config.CriuSHA256,
			"--manager-root", engine.config.ManagerRoot, "fork", "--source", source.ID, "--snapshot-id", snapshot, target)
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
		controller, err := unix.PidfdOpen(command.Process.Pid, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer unix.Close(controller)
		deadline := time.Now().Add(20 * time.Second)
		backend := -1
		var intent Record
		for backend < 0 {
			intent, err = engine.read(target)
			if err == nil && intent.Start.Attempted && intent.Phase == "starting" {
				tasks, readErr := os.ReadDir(fmt.Sprintf("/proc/%d/task", command.Process.Pid))
				if readErr != nil {
					t.Fatal(readErr)
				}
				var fields []string
				for _, task := range tasks {
					children, readErr := os.ReadFile(fmt.Sprintf("/proc/%d/task/%s/children", command.Process.Pid, task.Name()))
					if readErr == nil {
						fields = append(fields, strings.Fields(string(children))...)
					}
				}
				for _, field := range fields {
					pid, parseErr := strconv.Atoi(field)
					if parseErr != nil {
						t.Fatal(parseErr)
					}
					args, readErr := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
					if readErr != nil {
						continue
					}
					parts := strings.Split(string(args), "\x00")
					if !slices.Contains(parts, "factorybox-restore") || !slices.Contains(parts, intent.RuntimeID) {
						continue
					}
					backend, err = unix.PidfdOpen(pid, 0)
					if err != nil {
						backend = -1
					}
				}
			}
			if time.Now().After(deadline) {
				t.Fatal("did not observe the exact owned in-flight restore backend")
			}
			if backend < 0 {
				time.Sleep(5 * time.Millisecond)
			}
		}
		defer unix.Close(backend)
		if err := unix.PidfdSendSignal(controller, unix.SIGSTOP, nil, 0); err != nil {
			t.Fatal(err)
		}
		for {
			poll := []unix.PollFd{{Fd: int32(backend), Events: unix.POLLIN}}
			n, err := unix.Poll(poll, 10)
			if err != nil && !errors.Is(err, unix.EINTR) {
				t.Fatal(err)
			}
			if n > 0 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("owned restore backend did not finish while its controller was stopped")
			}
		}
		receiptPath := "objects/" + target + "/restore-result.json"
		var completed restoreResult
		if err := engine.readCheckpointJSON(receiptPath, &completed); err != nil {
			t.Fatal("backend did not persist completion independently of its stopped controller", err)
		}
		before, err := engine.read(target)
		if err != nil || before.Phase != "starting" || before.Start.CompletedAt != "" {
			t.Fatal("controller published its outcome before the loss window", err)
		}
		if err := unix.PidfdSendSignal(controller, unix.SIGKILL, nil, 0); err != nil {
			t.Fatal(err)
		}
		waitErr := command.Wait()
		waited = true
		if waitErr == nil {
			t.Fatal("owned controller unexpectedly exited successfully")
		}
		recovered, err := forkNativeCLI(engine, source.ID, snapshot, target)
		if err != nil {
			t.Fatal("reconcile after actual controller death", err, stderr.String())
		}
		t.Cleanup(func() {
			if _, err := engine.Delete(context.Background(), target); err != nil {
				t.Error(err)
			}
		})
		if recovered.Runtime == nil || recovered.Runtime.PID != completed.PID ||
			recovered.Record.Start.CompletedAt != completed.CompletedAt || recovered.Record.Owner != before.Owner {
			t.Fatal("controller-loss recovery replaced its original restored target")
		}
		assertRestoredNative(t, engine, recovered, expected)
		if _, err := engine.Delete(context.Background(), target); err != nil {
			t.Fatal("delete exact controller-loss target before the next live fork", err)
		}
		t.Log("stopped the actual public controller while its exact backend finished, killed it after durable completion, recovered the same PID/owner/RAM from its receipt and deleted only that target")
	})
}

func assertRestoredNative(t *testing.T, engine *Engine, view View, expected captureActorState) {
	t.Helper()
	state, err := readCaptureActor(view.Runtime.PID, "get")
	if err != nil || !reflect.DeepEqual(state, expected) {
		t.Fatal("restored process lost its exact private RAM payload/counter", state.Counter, expected.Counter, err)
	}
	lock, err := engine.lock()
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	for path, expected := range map[string]string{
		"/proc/sys/kernel/panic":         "17",
		"/state/marker":                  "retained-source-file",
		"/var/lib/docker/capture-marker": "private-daemon-before",
	} {
		output, err := engine.command(context.Background(), lock, "exec", view.Record.RuntimeID, "/bin/cat", path)
		if err != nil || strings.TrimSpace(string(output)) != expected {
			t.Fatal("restored private virtual/file/volume state differs", path, string(output), err)
		}
	}
}

func forkFromRestoredNative(t *testing.T, engine *Engine, restored View, expected captureActorState) {
	t.Helper()
	ctx := context.Background()
	snapshot := strings.Repeat("e", 32)
	checkpoint, err := engine.Checkpoint(ctx, restored.Record.ID, snapshot)
	if err != nil {
		t.Fatal("capture a previously forked independent source", err)
	}
	generation := engine.config.Root + "/objects/" + restored.Record.ID + "/captures/" + snapshot
	descendant := forkNative(t, engine, restored.Record, snapshot, "descendant", expected)
	if descendant.Runtime.PID == restored.Runtime.PID {
		t.Fatal("descendant replaced its source process")
	}
	assertRestoredNative(t, engine, descendant, expected)
	if _, err := readCaptureActor(descendant.Runtime.PID, "advance"); err != nil {
		t.Fatal(err)
	}
	lock, err := engine.lock()
	if err != nil {
		t.Fatal(err)
	}
	_, virtualErr := engine.command(ctx, lock, "exec", descendant.Record.RuntimeID,
		"/bin/sh", "-c", "printf '41\\n' > /proc/sys/kernel/panic")
	closeErr := lock.Close()
	if virtualErr != nil || closeErr != nil {
		t.Fatal("mutate descendant's private virtual setting", virtualErr, closeErr)
	}
	assertRestoredNative(t, engine, restored, expected)
	if err := sysboxcapture.VerifyFilesystemSnapshot(ctx, generation, checkpoint.Filesystem); err != nil {
		t.Fatal("descendant mutation changed its source checkpoint", err)
	}
	deleted, err := engine.Delete(ctx, descendant.Record.ID)
	if err != nil {
		t.Fatal("delete public fork with its owned overlay", err)
	}
	if err := sysboxcapture.CheckOverlayLayout(descendant.Record.InputBundle); !errors.Is(err, sysboxcapture.ErrOverlayAbsent) {
		t.Fatal("public fork deletion retained or changed its host overlay", err)
	}
	t.Run("lost-delete-outcome-after-unmount", func(t *testing.T) {
		lost := deleted.Record
		operation := *lost.Delete
		operation.CompletedAt, operation.Error = "", "controller deletion outcome lost"
		lost.Phase, lost.Delete = "deleting", &operation
		if err := engine.write(lost); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := engine.write(deleted.Record); err != nil {
				t.Error(err)
			}
		}()
		rootfs := lost.Rootfs
		if err := unix.Mount(rootfs, rootfs, "", unix.MS_BIND, ""); err != nil {
			t.Fatal(err)
		}
		_, foreignMount := engine.Delete(ctx, lost.ID)
		unmountErr := unix.Unmount(rootfs, 0)
		if foreignMount == nil || unmountErr != nil {
			t.Fatal("lost deletion accepted or removed a foreign bind mount", foreignMount, unmountErr)
		}
		if err := os.Rename(rootfs, rootfs+".held"); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(rootfs, 0700); err != nil {
			os.Rename(rootfs+".held", rootfs)
			t.Fatal(err)
		}
		_, foreignDirectory := engine.Delete(ctx, lost.ID)
		removeErr := os.Remove(rootfs)
		renameErr := os.Rename(rootfs+".held", rootfs)
		if foreignDirectory == nil || removeErr != nil || renameErr != nil {
			t.Fatal("lost deletion accepted a replacement mountpoint directory",
				foreignDirectory, removeErr, renameErr)
		}
		recovered, err := engine.Delete(ctx, lost.ID)
		if err != nil || recovered.Record.Phase != "deleted" || recovered.Runtime != nil ||
			recovered.Record.Owner != lost.Owner || recovered.Record.RuntimeCreated != lost.RuntimeCreated ||
			recovered.Record.Delete.CompletedAt == "" {
			t.Fatal("lost final deletion outcome did not reconcile its original unmounted rootfs", err)
		}
		if _, err := os.Stat(lost.InputBundle + "/upper"); err != nil {
			t.Fatal("reconciled deletion removed retained fork backing", err)
		}
	})
	if _, err := forkNativeCLI(engine, restored.Record.ID, snapshot, "descendant"); err == nil {
		t.Fatal("fork replay recreated a deleted target identity")
	}
}
