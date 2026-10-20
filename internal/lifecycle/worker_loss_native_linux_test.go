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

func TestCaptureWorkerLossNative(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_WORKER_LOSS") != "1" {
		t.Skip("requires the owned capture-worker interruption fixture")
	}
	for _, scenario := range []struct {
		name, operation string
		forged          bool
	}{{"checkpoint", "checkpoint", false}, {"pause", "pause", false}, {"forged-completion", "checkpoint", true}} {
		t.Run(scenario.name, func(t *testing.T) {
			operation := scenario.operation
			engine, bundle := nativeEngine(t,
				"/bin/env FACTORYBOX_CAPTURE_ACTOR=1 FACTORYBOX_CAPTURE_BALLAST=1 /bin/capture-actor -test.run=^TestCaptureActor$ -test.timeout=0")
			binary, err := os.ReadFile(os.Args[0])
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(bundle+"/lower/bin/capture-actor", binary, 0755); err != nil {
				t.Fatal(err)
			}
			engine.config.Criu, engine.config.CriuSHA256 = "/out/criu", os.Getenv("FACTORYBOX_CRIU_SHA256")
			engine.config.ManagerRoot = "/var/lib/sysbox/manager"
			ctx := context.Background()
			source, err := engine.Start(ctx, "worker-loss", bundle)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if _, err := engine.Delete(ctx, source.Record.ID); err != nil {
					t.Error("delete exact worker-loss source", err)
				}
			})
			deadline := time.Now().Add(5 * time.Second)
			var before captureActorState
			for {
				before, err = readCaptureActor(source.Runtime.PID, "get")
				if err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("worker-loss actor did not become ready", err)
				}
				time.Sleep(time.Millisecond)
			}
			committed := strings.Repeat("a", 32)
			if _, err := engine.Checkpoint(ctx, source.Record.ID, committed); err != nil {
				t.Fatal("create prior committed recovery point", err)
			}
			// Small files make the real filesystem copy observable without a
			// synthetic pause hook or a large disk/memory payload.
			directory := bundle + "/rootfs/copy-window"
			if err := os.Mkdir(directory, 0755); err != nil {
				t.Fatal(err)
			}
			for index := 0; index < 4096; index++ {
				if err := os.WriteFile(filepath.Join(directory, fmt.Sprint(index)), []byte("retained\n"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			snapshot := strings.Repeat("d", 32)
			relative, _ := checkpointPath(source.Record.ID, snapshot)
			command := exec.Command("/factorybox", "--root", engine.config.Root,
				"--runtime", engine.config.Runtime, "--runtime-sha256", engine.config.RuntimeSHA256,
				"--criu", engine.config.Criu, "--criu-sha256", engine.config.CriuSHA256,
				"--manager-root", engine.config.ManagerRoot,
				operation, "--snapshot-id", snapshot, source.Record.ID)
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
			var worker struct {
				Token      string `json:"token"`
				PID        int    `json:"worker_pid"`
				Start      string `json:"worker_start"`
				SourcePID  int    `json:"source_pid"`
				CriuSHA256 string `json:"criu_sha256"`
				Scope      string `json:"scope"`
			}
			deadline = time.Now().Add(15 * time.Second)
			interrupted := false
			for !interrupted {
				_, copying := os.Stat(filepath.Join(engine.config.Root, relative, "filesystem"))
				if copying == nil &&
					engine.readCheckpointJSON(relative+"/guard/capture-worker-intent.json", &worker) == nil &&
					worker.Token == snapshot && worker.SourcePID == source.Runtime.PID &&
					worker.CriuSHA256 == engine.config.CriuSHA256 {
					birth, birthErr := processBirth(worker.PID)
					status, statusErr := os.ReadFile(fmt.Sprintf("/proc/%d/status", worker.PID))
					executable, executableErr := os.Stat(fmt.Sprintf("/proc/%d/exe", worker.PID))
					expected, expectedErr := os.Stat("/factorybox")
					parentMatches := false
					for _, line := range strings.Split(string(status), "\n") {
						fields := strings.Fields(line)
						parentMatches = parentMatches || len(fields) == 2 && fields[0] == "PPid:" &&
							fields[1] == strconv.Itoa(command.Process.Pid)
					}
					if birthErr == nil && birth == worker.Start && statusErr == nil &&
						executableErr == nil && expectedErr == nil && os.SameFile(executable, expected) && parentMatches {
						fd, err := unix.PidfdOpen(worker.PID, 0)
						if err != nil {
							t.Fatal(err)
						}
						copyFD, err := stopCaptureCopyChild(worker.PID)
						if err != nil {
							unix.Close(fd)
							t.Fatal(err)
						}
						if copyFD < 0 {
							unix.Close(fd)
							if time.Now().After(deadline) {
								t.Fatal("did not observe the exact worker's GNU cp child")
							}
							time.Sleep(time.Millisecond)
							continue
						}
						t.Cleanup(func() {
							unix.PidfdSendSignal(copyFD, unix.SIGKILL, nil, 0)
							unix.Close(copyFD)
						})
						cleanupFD, err := unix.Dup(fd)
						if err != nil {
							unix.Close(fd)
							t.Fatal(err)
						}
						t.Cleanup(func() {
							unix.PidfdSendSignal(cleanupFD, unix.SIGKILL, nil, 0)
							unix.Close(cleanupFD)
						})
						// Keep the real copy worker alive but stopped while
						// checking that its receipt alone cannot release RAM.
						err = unix.PidfdSendSignal(fd, unix.SIGSTOP, nil, 0)
						if err != nil {
							unix.Close(fd)
							t.Fatal(err)
						}
						var config captureGuardConfig
						if err := engine.readCheckpointJSON(relative+"/guard/guard-intent.json", &config); err != nil {
							unix.Close(fd)
							t.Fatal(err)
						}
						anchors := captureGuardAnchors{}
						anchors.group, err = captureGuardOpen(cgroupRoot + "/" + config.Source.RuntimeID)
						if err == nil {
							anchors.evidence, err = engine.root.Open(relative + "/guard")
						}
						if err == nil {
							anchors.root, err = engine.root.Open(".")
						}
						if err == nil {
							var pidfd int
							pidfd, err = unix.PidfdOpen(config.PID, 0)
							if err == nil {
								anchors.pid = os.NewFile(uintptr(pidfd), "worker-loss original source")
							}
						}
						if err != nil {
							anchors.close()
							unix.Close(fd)
							t.Fatal(err)
						}
						liveErr := captureGuardProcessCompleted(config, anchors)
						anchors.close()
						if liveErr == nil || !strings.Contains(liveErr.Error(), "live worker") {
							unix.Close(fd)
							t.Fatal("completed receipt did not reject its still-live worker", liveErr)
						}
						if scenario.forged {
							path := filepath.Join(engine.config.Root, relative, "guard/capture-process-complete.json")
							data, err := os.ReadFile(path)
							if err != nil {
								unix.Close(fd)
								t.Fatal(err)
							}
							var completion captureProcessCompletion
							if err := json.Unmarshal(data, &completion); err != nil {
								unix.Close(fd)
								t.Fatal(err)
							}
							completion.Worker.SourcePID++
							data, err = json.Marshal(completion)
							if err != nil {
								unix.Close(fd)
								t.Fatal(err)
							}
							if err := os.WriteFile(path, data, 0600); err != nil {
								unix.Close(fd)
								t.Fatal(err)
							}
						}
						err = unix.PidfdSendSignal(fd, unix.SIGKILL, nil, 0)
						unix.Close(fd)
						if err != nil {
							t.Fatal(err)
						}
						exitDeadline := time.Now().Add(2 * time.Second)
						for {
							remaining := time.Until(exitDeadline)
							if remaining <= 0 {
								t.Fatal("copy helper survived its dead capture worker")
							}
							poll := []unix.PollFd{{Fd: int32(copyFD), Events: unix.POLLIN}}
							count, err := unix.Poll(poll, int(remaining.Milliseconds())+1)
							if errors.Is(err, unix.EINTR) && time.Now().Before(exitDeadline) {
								continue
							}
							if err != nil || count != 1 || poll[0].Revents&unix.POLLIN == 0 {
								t.Fatal("copy helper survived its dead capture worker", err)
							}
							break
						}
						interrupted = true
					}
				}
				if time.Now().After(deadline) {
					t.Fatal("did not observe the exact capture worker copying the filesystem")
				}
				if !interrupted {
					time.Sleep(time.Millisecond)
				}
			}
			if err := command.Wait(); err == nil {
				t.Fatal("killed capture worker reported success")
			}
			waited = true
			if _, err := os.Stat(filepath.Join(engine.config.Root, relative, "checkpoint.json")); !os.IsNotExist(err) {
				t.Fatal("incomplete capture published a checkpoint", err)
			}
			if _, err := os.Stat(filepath.Join(engine.config.Root, relative, "guard/capture-worker-outcome.json")); !os.IsNotExist(err) {
				t.Fatal("fault did not kill the capture worker before its outcome", err)
			}
			current, err := engine.Inspect(ctx, source.Record.ID)
			if scenario.forged {
				if err != nil || current.Runtime == nil || current.Runtime.Status != "paused" ||
					current.Runtime.PID != source.Runtime.PID {
					t.Fatal("forged completion released an unproved source", err)
				}
				if _, err := os.Stat(filepath.Join(engine.config.Root, relative, "guard/guard-capture-aborted.json")); !os.IsNotExist(err) {
					t.Fatal("forged completion received a safe-abort acknowledgement", err)
				}
				t.Log("mismatched source completion remained held without a safe-abort acknowledgement")
				return
			}
			if err != nil || current.Runtime == nil || current.Runtime.Status != "running" ||
				current.Runtime.PID != source.Runtime.PID || current.Runtime.Created != source.Runtime.Created ||
				current.Record.Phase != "running" {
				t.Fatalf("%s worker loss did not recover the original source: %+v: %v; stderr=%s",
					operation, current, err, stderr.String())
			}
			after, err := readCaptureActor(source.Runtime.PID, "get")
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("worker loss changed original private RAM", err)
			}
			if _, err := readCaptureActor(source.Runtime.PID, "advance"); err != nil {
				t.Fatal("worker-loss source cannot continue useful work", err)
			}
			if operation == "pause" {
				if current.Record.Pause != nil || len(current.Record.PauseHistory) != 1 ||
					current.Record.PauseHistory[0].RejectedAt == "" {
					t.Fatal("failed pause did not retain its recovered failure in history")
				}
				// Lose only the final product-state publication. The guard's
				// original persisted pausing record is the exact crash state.
				var config captureGuardConfig
				if err := engine.readCheckpointJSON(relative+"/guard/guard-intent.json", &config); err != nil {
					t.Fatal(err)
				}
				if err := engine.write(config.Source); err != nil {
					t.Fatal(err)
				}
				for _, path := range []string{
					"guard/guard-capture-aborted.json",
					"guard/guard-outcome.json",
					"guard/capture-process-complete.json",
					"source-state.json",
				} {
					file := filepath.Join(engine.config.Root, relative, path)
					original, err := os.ReadFile(file)
					if err != nil {
						t.Fatal(err)
					}
					var changed []byte
					if path == "source-state.json" {
						changed = append(append([]byte(nil), original...), ' ')
					} else {
						var fields map[string]any
						if err := json.Unmarshal(original, &fields); err != nil {
							t.Fatal(err)
						}
						if path == "guard/capture-process-complete.json" {
							fields["source_start"] = "forged"
						} else {
							fields["capture_in_flight"] = true
						}
						changed, err = json.Marshal(fields)
						if err != nil {
							t.Fatal(err)
						}
					}
					if err := os.WriteFile(file, changed, 0600); err != nil {
						t.Fatal(err)
					}
					_, replayErr := engine.Pause(ctx, source.Record.ID, snapshot)
					refused, inspectErr := engine.Inspect(ctx, source.Record.ID)
					restoreErr := os.WriteFile(file, original, 0600)
					if replayErr == nil || inspectErr != nil || restoreErr != nil ||
						refused.Record.Phase != "pausing" || refused.Runtime == nil ||
						refused.Runtime.PID != source.Runtime.PID || refused.Runtime.Status != "running" {
						t.Fatalf("unsafe %s replay changed the original state: %v %v %v", path, replayErr, inspectErr, restoreErr)
					}
				}
				if _, err := engine.Pause(ctx, source.Record.ID, snapshot); err == nil {
					t.Fatal("failed pause replay reported a successful snapshot")
				}
				replayed, err := engine.Inspect(ctx, source.Record.ID)
				if err != nil || replayed.Record.Phase != "running" || replayed.Record.Pause != nil ||
					!reflect.DeepEqual(replayed.Record.PauseHistory, current.Record.PauseHistory) || replayed.Runtime == nil ||
					replayed.Runtime.PID != source.Runtime.PID || replayed.Runtime.Status != "running" {
					t.Fatal("failed pause did not reconcile its lost final publication", err)
				}
			}
			recovered, err := forkNativeCLI(engine, source.Record.ID, committed, "worker-recovery")
			if err != nil {
				t.Fatal("worker loss damaged its prior committed checkpoint", err)
			}
			t.Cleanup(func() {
				if _, err := engine.Delete(ctx, "worker-recovery"); err != nil {
					t.Error("delete exact committed recovery fork", err)
				}
			})
			recoveredRAM, err := readCaptureActor(recovered.Runtime.PID, "get")
			if err != nil || !reflect.DeepEqual(before, recoveredRAM) {
				t.Fatal("prior checkpoint lost its independent RAM", err)
			}
			if _, err := readCaptureActor(recovered.Runtime.PID, "advance"); err != nil {
				t.Fatal("prior checkpoint cannot continue useful work", err)
			}
			t.Logf("actual %s worker PID %d was killed after successful CRIU and before complete backing; original and committed fork remain useful", operation, worker.PID)
		})
	}
}

// Stop only an observed, pidfd-pinned cp child of this exact capture worker.
// Its arguments must be the production descriptor-based backing copy.
func stopCaptureCopyChild(worker int) (int, error) {
	expectedPath, err := exec.LookPath("cp")
	if err != nil {
		return -1, err
	}
	expected, err := os.Stat(expectedPath)
	if err != nil {
		return -1, err
	}
	paths, err := filepath.Glob(fmt.Sprintf("/proc/%d/task/*/children", worker))
	if err != nil {
		return -1, err
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, field := range strings.Fields(string(data)) {
			pid, err := strconv.Atoi(field)
			if err != nil || pid <= 1 {
				return -1, fmt.Errorf("copy worker has an invalid child identity")
			}
			fd, err := unix.PidfdOpen(pid, 0)
			if err != nil {
				continue
			}
			actual, statErr := os.Stat(fmt.Sprintf("/proc/%d/exe", pid))
			arguments, argsErr := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
			if statErr == nil && argsErr == nil && os.SameFile(actual, expected) &&
				string(arguments) == "cp\x00-a\x00--reflink=auto\x00--\x00/proc/self/fd/3/.\x00/proc/self/fd/4/\x00" {
				if err := unix.PidfdSendSignal(fd, unix.SIGSTOP, nil, 0); err == nil {
					return fd, nil
				}
			}
			unix.Close(fd)
		}
	}
	return -1, nil
}
