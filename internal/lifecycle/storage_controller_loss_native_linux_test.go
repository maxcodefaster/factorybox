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
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// Stop only the exact controller while its observed copy is held, kill the
// real capture worker, and fill this private filesystem before the controller
// handles that exit. Neither the failed operation nor its recovery receives
// diagnostic free space or a diagnostic thaw.
func TestNativeStorageControllerLossENOSPC(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_STORAGE_CONTROLLER_LOSS") != "1" {
		t.Skip("requires the owned bounded capture-worker-loss pressure fixture")
	}
	for _, operation := range []string{"checkpoint", "pause"} {
		if !t.Run(operation, func(t *testing.T) {
			original, bundle := nativeEngine(t, "/bin/env FACTORYBOX_CAPTURE_ACTOR=1 /bin/capture-actor -test.run=^TestCaptureActor$ -test.timeout=0")
			binary, err := os.ReadFile(os.Args[0])
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(bundle+"/lower/bin/capture-actor", binary, 0755); err != nil {
				t.Fatal(err)
			}
			config := original.config
			if err := unix.Mount("tmpfs", config.Root, "tmpfs", unix.MS_NOSUID|unix.MS_NODEV, "size=128m,mode=0700"); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if t.Failed() {
					command := exec.Command("tar", "--sparse", "-C", config.Root, "-cpf", filepath.Dir(config.Root)+"/failed-controller-pressure-store.tar", ".")
					if out, err := command.CombinedOutput(); err != nil {
						t.Errorf("retain exact failed bounded store: %v: %s", err, out)
					}
				}
				if err := unix.Unmount(config.Root, 0); err != nil {
					t.Error("unmount exact owned pressure filesystem", err)
				}
			})
			config.Criu, config.CriuSHA256, config.ManagerRoot = "/out/criu", os.Getenv("FACTORYBOX_CRIU_SHA256"), "/var/lib/sysbox/store/manager"
			config.MinFreeBytes, config.WriteReserve = 8<<20, 8<<20
			engine, err := Open(config)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { engine.Close() })
			ctx, cancel := context.WithTimeout(context.Background(), 190*time.Second)
			defer cancel()
			source, err := engine.Start(ctx, "source", bundle)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				// Fixture cleanup happens after the recovery assertions. It does
				// not provide any space to the tested failed operation.
				if err := unix.Mount("tmpfs", config.Root, "tmpfs", unix.MS_REMOUNT|unix.MS_NOSUID|unix.MS_NODEV, "size=144m,mode=0700"); err != nil {
					t.Error("allow bounded cleanup metadata", err)
				}
				if _, err := engine.Delete(context.Background(), "source"); err != nil {
					t.Error("delete exact pressure source", err)
				}
			})
			var before captureActorState
			for deadline := time.Now().Add(10 * time.Second); ; {
				before, err = readCaptureActor(source.Runtime.PID, "get")
				if err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("private actor did not become ready", err)
				}
				time.Sleep(time.Millisecond)
			}
			committed := strings.Repeat("a", 32)
			prior, err := engine.Checkpoint(ctx, "source", committed)
			if err != nil {
				t.Fatal("prior complete checkpoint", err)
			}
			directory := bundle + "/rootfs/copy-window"
			if err := os.Mkdir(directory, 0755); err != nil {
				t.Fatal(err)
			}
			for index := range 4096 {
				if err := os.WriteFile(filepath.Join(directory, strconv.Itoa(index)), []byte("retained\n"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			snapshot := strings.Repeat("d", 32)
			relative, _ := checkpointPath("source", snapshot)
			command := exec.CommandContext(ctx, "/factorybox", "--root", config.Root,
				"--runtime", config.Runtime, "--runtime-sha256", config.RuntimeSHA256,
				"--criu", config.Criu, "--criu-sha256", config.CriuSHA256, "--manager-root", config.ManagerRoot,
				"--min-free-bytes", fmt.Sprint(config.MinFreeBytes), "--write-reserve-bytes", fmt.Sprint(config.WriteReserve),
				operation, "--snapshot-id", snapshot, "source")
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
			controllerFD, err := unix.PidfdOpen(command.Process.Pid, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer unix.Close(controllerFD)
			var worker captureWorkerIntent
			copyFD, workerFD := -1, -1
			for deadline := time.Now().Add(15 * time.Second); ; {
				if engine.readCheckpointJSON(relative+"/guard/capture-worker-intent.json", &worker) == nil &&
					worker.Token == snapshot && worker.SourcePID == source.Runtime.PID && worker.CriuSHA256 == config.CriuSHA256 {
					birth, birthErr := processBirth(worker.PID)
					status, statusErr := os.ReadFile(fmt.Sprintf("/proc/%d/status", worker.PID))
					actual, actualErr := os.Stat(fmt.Sprintf("/proc/%d/exe", worker.PID))
					expected, expectedErr := os.Stat("/factorybox")
					parentMatches := false
					for _, line := range strings.Split(string(status), "\n") {
						fields := strings.Fields(line)
						parentMatches = parentMatches || (len(fields) == 2 && fields[0] == "PPid:" && fields[1] == strconv.Itoa(command.Process.Pid))
					}
					if birthErr == nil && birth == worker.Start && statusErr == nil && actualErr == nil && expectedErr == nil &&
						os.SameFile(actual, expected) && parentMatches {
						workerFD, err = unix.PidfdOpen(worker.PID, 0)
						if err != nil {
							t.Fatal(err)
						}
						copyFD, err = stopCaptureCopyChild(worker.PID)
						if err != nil {
							unix.Close(workerFD)
							t.Fatal(err)
						}
						if copyFD >= 0 {
							break
						}
						unix.Close(workerFD)
					}
				}
				if time.Now().After(deadline) {
					t.Fatal("did not observe the owned capture worker's exact GNU cp child")
				}
				time.Sleep(time.Millisecond)
			}
			defer unix.Close(workerFD)
			defer unix.Close(copyFD)
			defer unix.PidfdSendSignal(copyFD, unix.SIGKILL, nil, 0)
			defer unix.PidfdSendSignal(workerFD, unix.SIGKILL, nil, 0)
			var completion captureProcessCompletion
			if err := engine.readCheckpointJSON(relative+"/guard/capture-process-complete.json", &completion); err != nil ||
				completion.Worker != worker || completion.SourceStart == "" {
				t.Fatal("copy interruption lacks the actual completed CRIU receipt", err)
			}
			if err := unix.PidfdSendSignal(controllerFD, unix.SIGSTOP, nil, 0); err != nil {
				t.Fatal(err)
			}
			stopped := func() bool {
				threads, err := os.ReadDir(fmt.Sprintf("/proc/%d/task", command.Process.Pid))
				if err != nil || len(threads) == 0 {
					return false
				}
				for _, thread := range threads {
					status, err := os.ReadFile(fmt.Sprintf("/proc/%d/task/%s/status", command.Process.Pid, thread.Name()))
					if err != nil || !bytes.Contains(status, []byte("\nState:\tT ")) {
						return false
					}
				}
				return true
			}
			for deadline := time.Now().Add(2 * time.Second); !stopped(); {
				if time.Now().After(deadline) {
					t.Fatal("owned controller did not enter its complete observed group stop")
				}
				time.Sleep(time.Millisecond)
			}
			if err := unix.PidfdSendSignal(workerFD, unix.SIGKILL, nil, 0); err != nil {
				t.Fatal(err)
			}
			for deadline := time.Now().Add(2 * time.Second); ; {
				poll := []unix.PollFd{{Fd: int32(copyFD), Events: unix.POLLIN}}
				count, err := unix.Poll(poll, 50)
				if err == nil && count == 1 && poll[0].Revents&unix.POLLIN != 0 {
					break
				}
				if (err != nil && !errors.Is(err, unix.EINTR)) || time.Now().After(deadline) {
					t.Fatal("owned copy survived its dead capture worker", err)
				}
			}
			var capacity unix.Statfs_t
			if err := unix.Statfs(config.Root, &capacity); err != nil || capacity.Bavail == 0 {
				t.Fatal("fault did not leave pressure space to allocate", err)
			}
			pressure, err := os.OpenFile(config.Root+"/owned-pressure", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				t.Fatal(err)
			}
			err = unix.Fallocate(int(pressure.Fd()), 0, 0, int64(capacity.Bavail)*capacity.Bsize)
			closeErr := pressure.Close()
			if err != nil || closeErr != nil {
				t.Fatal("fill only the bounded private pressure filesystem", err, closeErr)
			}
			if err := unix.Statfs(config.Root, &capacity); err != nil || capacity.Bavail != 0 {
				t.Fatal("controller was not tested with actual zero available bytes", capacity.Bavail, err)
			}
			canary, err := os.OpenFile(config.Root+"/enospc-canary", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				t.Fatal(err)
			}
			_, writeErr := canary.Write([]byte("must fail with actual ENOSPC"))
			closeErr = canary.Close()
			if !errors.Is(writeErr, unix.ENOSPC) || closeErr != nil {
				t.Fatal("bounded store did not return actual ENOSPC", writeErr, closeErr)
			}
			reserve, err := os.Stat(filepath.Join(config.Root, relative, "guard/.rollback-reserve"))
			if err != nil || reserve.Size() != captureRollbackBytes || !stopped() {
				t.Fatal("controller handled the failed capture before actual zero-space pressure", err)
			}
			group, err := captureGuardOpen(cgroupRoot + "/" + source.Record.RuntimeID)
			if err != nil {
				t.Fatal(err)
			}
			events, eventErr := captureGuardEvents(group)
			closeErr = group.Close()
			if eventErr != nil || closeErr != nil || events["frozen"] != 1 {
				t.Fatal("capture source was not still held at the actual full-disk boundary", eventErr, closeErr, events)
			}
			t.Logf("exact controller %d stopped; capture worker %d killed after real CRIU; private filesystem has %d available bytes", command.Process.Pid, worker.PID, capacity.Bavail)
			if err := unix.PidfdSendSignal(controllerFD, unix.SIGCONT, nil, 0); err != nil {
				t.Fatal(err)
			}
			failure := command.Wait()
			waited = true
			if err := os.WriteFile("/output/storage-controller-loss-"+operation+"-cli.txt", append(stdout.Bytes(), stderr.Bytes()...), 0600); err != nil {
				t.Fatal(err)
			}
			if failure == nil {
				t.Fatal("killed incomplete capture reported success")
			}
			for _, name := range []string{"checkpoint.json", "guard/capture-worker-outcome.json"} {
				if _, err := os.Stat(filepath.Join(config.Root, relative, name)); !os.IsNotExist(err) {
					t.Fatal("fault published a checkpoint or reached the killed worker's defer", name, err)
				}
			}
			current, err := engine.Inspect(ctx, "source")
			if err != nil || current.Runtime == nil || current.Runtime.PID != source.Runtime.PID ||
				current.Runtime.Created != source.Runtime.Created || current.Runtime.Status != "running" || current.Record.Phase != "running" {
				t.Fatalf("%s full-disk worker loss lost/froze the original source before pressure removal: %+v: %v; stderr=%s", operation, current, err, stderr.String())
			}
			after, err := readCaptureActor(source.Runtime.PID, "get")
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("full-disk worker loss changed original private RAM/files/watch", err)
			}
			if operation == "pause" && (current.Record.Pause != nil || len(current.Record.PauseHistory) != 1 || current.Record.PauseHistory[0].RejectedAt == "") {
				t.Fatal("failed pause lost its original recovered history")
			}
			if _, err := readCaptureActor(source.Runtime.PID, "advance"); err != nil {
				t.Fatal("recovered source cannot continue useful work before pressure removal", err)
			}
			priorPath, _ := checkpointPath("source", committed)
			if err := engine.verifyCheckpoint(ctx, source.Record, committed, priorPath, prior); err != nil {
				t.Fatal("older complete checkpoint changed under pressure", err)
			}
			// Only now clear this test's pressure to admit a new independent
			// fork. The failed operation already recovered its source unaided.
			if err := os.Remove(config.Root + "/owned-pressure"); err != nil {
				t.Fatal(err)
			}
			fork, err := forkNativeCLI(engine, "source", committed, "recovery")
			if err != nil {
				t.Fatal("older checkpoint cannot make an independent recovery fork", err)
			}
			t.Cleanup(func() {
				if _, err := engine.Delete(context.Background(), "recovery"); err != nil {
					t.Error("delete exact independent recovery fork", err)
				}
			})
			forked, err := readCaptureActor(fork.Runtime.PID, "get")
			if err != nil || !reflect.DeepEqual(before, forked) {
				t.Fatal("older checkpoint lost its independent original RAM/files/watch", err)
			}
			data, err := json.MarshalIndent(map[string]any{"passed": true, "operation": operation,
				"source_pid": source.Runtime.PID, "worker_pid": worker.PID, "controller_pid": command.Process.Pid,
				"available_before_controller_continue": capacity.Bavail, "actual_enospc": true,
				"source_running_before_pressure_removal": true, "private_ram_sha256": sum(before.Payload),
				"prior_checkpoint_independent": true, "prior_checkpoint": committed}, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile("/output/storage-controller-loss-"+operation+".json", data, 0644); err != nil {
				t.Fatal(err)
			}
			t.Log("full-disk capture-worker loss preserved the running source and independently useful prior checkpoint")
		}) {
			return
		}
	}
}
