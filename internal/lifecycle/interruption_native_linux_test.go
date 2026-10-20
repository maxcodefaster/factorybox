//go:build linux

package lifecycle

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestInterruptedCaptureNative(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_INTERRUPTION") != "1" {
		t.Skip("requires the owned real-CRIU interruption fixture")
	}
	// The candidate fault probe traces only its exact owned CRIU process.
	// Keep ptrace requests and waits on one OS thread until that tracer exits.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pauseMode := os.Getenv("FACTORYBOX_NATIVE_INTERRUPTION_PAUSE") == "1"
	multiprocess := os.Getenv("FACTORYBOX_NATIVE_INTERRUPTION_MULTIPROCESS") == "1"
	interruptChild := os.Getenv("FACTORYBOX_NATIVE_INTERRUPTION_CHILD") == "1"
	if interruptChild && !multiprocess {
		t.Fatal("child interruption requires the two-process fixture")
	}
	grantControl := os.Getenv("FACTORYBOX_CRIU_RETAINED_GRANT_CONTROL") == "1"
	threadChurn := os.Getenv("FACTORYBOX_NATIVE_INTERRUPTION_THREAD_CHURN") == "1"
	if grantControl && os.Getenv("FACTORYBOX_CRIU_RETAINED_DIAGNOSTIC") != "1" {
		t.Fatal("retained grant controls require the retained-context candidate")
	}
	actor := "/bin/env FACTORYBOX_CAPTURE_ACTOR=1 FACTORYBOX_CAPTURE_BALLAST=1 "
	if os.Getenv("FACTORYBOX_CRIU_RETAINED_DIAGNOSTIC") == "1" {
		actor += "FACTORYBOX_CAPTURE_BALLAST_ADDRESS=1 "
	}
	engine, bundle := nativeEngine(t, actor+"/bin/capture-actor -test.run=^TestCaptureActor$ -test.timeout=0")
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
	source, err := engine.Start(ctx, "interrupted", bundle)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := engine.Delete(ctx, "interrupted"); err != nil {
			t.Error("delete exact interrupted fixture", err)
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
			t.Fatal("interruption actor is not ready", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	network, err := os.Open(fmt.Sprintf("/proc/%d/ns/net", source.Runtime.PID))
	if err != nil {
		t.Fatal(err)
	}
	defer network.Close()
	privateNetwork, err := network.Stat()
	workerNetwork, workerErr := os.Stat("/proc/self/ns/net")
	if err != nil || workerErr != nil || os.SameFile(privateNetwork, workerNetwork) {
		t.Fatal("interruption requires the source's separate private network namespace", err, workerErr)
	}
	loopback := exec.Command("nsenter", "--net=/proc/self/fd/3", "--", "ip", "link", "set", "dev", "lo", "up")
	loopback.ExtraFiles = []*os.File{network}
	if output, err := loopback.CombinedOutput(); err != nil {
		t.Fatalf("prepare only the private source loopback: %v: %s", err, output)
	}
	before, err = readCaptureActor(source.Runtime.PID, "make-tcp-pair")
	if err != nil || before.TCPClient <= 2 || before.TCPServer <= 2 || before.TCPListener <= 2 {
		t.Fatal("prepare real established TCP sockets", err)
	}
	type socketState struct {
		FD     int    `json:"fd"`
		Cookie uint64 `json:"cookie"`
		Repair int    `json:"repair"`
		Mark   int    `json:"mark"`
	}
	sockets := func() ([]socketState, error) {
		pidfd, err := unix.PidfdOpen(source.Runtime.PID, 0)
		if err != nil {
			return nil, err
		}
		defer unix.Close(pidfd)
		var result []socketState
		for _, original := range []int{before.TCPClient, before.TCPServer, before.TCPListener} {
			fd, err := unix.PidfdGetfd(pidfd, original, 0)
			if err != nil {
				return nil, err
			}
			cookie, cookieErr := unix.GetsockoptUint64(fd, unix.SOL_SOCKET, unix.SO_COOKIE)
			repair, repairErr := unix.GetsockoptInt(fd, unix.IPPROTO_TCP, unix.TCP_REPAIR)
			mark, markErr := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_MARK)
			closeErr := unix.Close(fd)
			if err := errors.Join(cookieErr, repairErr, markErr, closeErr); err != nil {
				return nil, err
			}
			result = append(result, socketState{original, cookie, repair, mark})
		}
		return result, nil
	}
	var childPID, childFD int
	var childStart string
	var childBefore captureActorState
	if multiprocess {
		before, err = readCaptureActor(source.Runtime.PID, "make-memory-child")
		if err != nil || before.NestedPID <= 1 || before.TimeMode != "memory" {
			t.Fatal("prepare the second captured process", err)
		}
		childPID, childStart, childFD = pinRetainedChildNative(t, source.Runtime.PID, before.NestedPID)
		defer unix.Close(childFD)
		deadline := time.Now().Add(5 * time.Second)
		for {
			childBefore, err = readCaptureActorAt(source.Runtime.PID, "/child-state", "get")
			if err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("second 32-MiB actor did not become ready", err)
			}
			time.Sleep(5 * time.Millisecond)
		}
		if childBefore.BallastBytes != 32<<20 || childBefore.BallastAddress == 0 {
			t.Fatal("second process lacks its complete private RAM witness")
		}
		evidence, err := json.Marshal(map[string]any{
			"passed": false, "source_pid": source.Runtime.PID, "child_pid": childPID,
			"child_start": childStart, "child_nested_pid": before.NestedPID,
		})
		if err != nil || os.WriteFile("/output/interruption-process-tree.json", append(evidence, '\n'), 0600) != nil {
			t.Fatal("save original process-tree fault boundary", err)
		}
	}
	committed := strings.Repeat("b", 32)
	if _, err := engine.Checkpoint(ctx, "interrupted", committed); err != nil {
		t.Fatal("create the committed recovery point before fault injection", err)
	}
	if threadChurn {
		armed, err := readCaptureActor(source.Runtime.PID, "arm-thread-churn")
		if err != nil || !reflect.DeepEqual(before, armed) {
			t.Fatal("arming thread churn changed original application payload", err)
		}
	}
	beforeSockets, err := sockets()
	if err != nil {
		t.Fatal("observe original TCP socket properties", err)
	}
	exchange := func() error {
		pidfd, err := unix.PidfdOpen(source.Runtime.PID, 0)
		if err != nil {
			return err
		}
		defer unix.Close(pidfd)
		client, err := unix.PidfdGetfd(pidfd, before.TCPClient, 0)
		if err != nil {
			return err
		}
		defer unix.Close(client)
		server, err := unix.PidfdGetfd(pidfd, before.TCPServer, 0)
		if err != nil {
			return err
		}
		defer unix.Close(server)
		message := []byte("owned-socket-round-trip\n")
		transfer := func(send, receive int) error {
			n, err := unix.SendmsgN(send, message, nil, nil, unix.MSG_DONTWAIT|unix.MSG_NOSIGNAL)
			if err != nil || n != len(message) {
				return fmt.Errorf("send original socket probe: %d: %w", n, err)
			}
			deadline := time.Now().Add(time.Second)
			var received []byte
			for len(received) < len(message) {
				var buffer [64]byte
				n, _, err := unix.Recvfrom(receive, buffer[:], unix.MSG_DONTWAIT)
				if err != nil && !errors.Is(err, unix.EAGAIN) {
					return err
				}
				if errors.Is(err, unix.EAGAIN) {
					n = 0
				}
				received = append(received, buffer[:n]...)
				if time.Now().After(deadline) {
					return fmt.Errorf("original socket probe timed out")
				}
				if n == 0 {
					time.Sleep(time.Millisecond)
				}
			}
			if !bytes.Equal(received, message) {
				return fmt.Errorf("original socket changed probe data")
			}
			return nil
		}
		return errors.Join(transfer(client, server), transfer(server, client))
	}
	if err := exchange(); err != nil {
		t.Fatal("committed TCP fixture is not usable before interruption", err)
	}
	if err := engine.validateCgroup(source.Record); err != nil {
		t.Fatal("pin only the original source cgroup for interruption observation", err)
	}
	originalGroup, err := captureGuardOpen(cgroupRoot + "/" + source.Record.RuntimeID)
	if err != nil {
		t.Fatal(err)
	}
	defer originalGroup.Close()
	if identity, err := cgroupFDIdentity(int(originalGroup.Fd())); err != nil ||
		source.Record.Cgroup == nil || identity != *source.Record.Cgroup {
		t.Fatal("interruption observation lost the original cgroup identity", err)
	}
	var freezerAtKill []byte
	var freezerObservationErr error
	networkRules := func() ([]byte, error) {
		command := exec.Command("nsenter", "--net=/proc/self/fd/3", "--", "iptables-save")
		command.ExtraFiles = []*os.File{network}
		data, err := command.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("read only original private network rules: %w", err)
		}
		var rules []string
		for _, line := range strings.Split(string(data), "\n") {
			if line != "" && !strings.HasPrefix(line, "#") {
				rules = append(rules, line)
			}
		}
		return []byte(regexp.MustCompile(`\[[0-9]+:[0-9]+\]`).ReplaceAllString(strings.Join(rules, "\n"), "[counters]")), nil
	}
	beforeNetwork, err := networkRules()
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("FACTORYBOX_CRIU_RETAINED_CORRUPTION_CONTROL") == "1" {
		if os.Getenv("FACTORYBOX_CRIU_RETAINED_DIAGNOSTIC") != "1" {
			t.Fatal("raw-RAM corruption control requires the held-source diagnostic")
		}
		stale, err := readCaptureActor(source.Runtime.PID, "flip-ballast-without-refresh")
		if err != nil || stale.BallastSHA != before.BallastSHA {
			t.Fatal("raw-RAM control did not retain its deliberately stale application digest", err)
		}
	}
	snapshot := strings.Repeat("c", 32)
	relative, _ := checkpointPath("interrupted", snapshot)
	operation := "checkpoint"
	if pauseMode {
		operation = "pause"
	}
	command := exec.Command("/factorybox", "--root", engine.config.Root,
		"--runtime", engine.config.Runtime, "--runtime-sha256", engine.config.RuntimeSHA256,
		"--criu", engine.config.Criu, "--criu-sha256", engine.config.CriuSHA256,
		"--manager-root", engine.config.ManagerRoot,
		operation, "--snapshot-id", snapshot, "interrupted")
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
	deadline = time.Now().Add(15 * time.Second)
	var worker struct {
		Token      string `json:"token"`
		PID        int    `json:"worker_pid"`
		Start      string `json:"worker_start"`
		SourcePID  int    `json:"source_pid"`
		CriuSHA256 string `json:"criu_sha256"`
		Scope      string `json:"scope"`
	}
	var interruptedPID int
	activePID, activeVirtualPID := source.Runtime.PID, 1
	if interruptChild {
		activePID, activeVirtualPID = childPID, before.NestedPID
	}
	var heldGuard retainedCaptureArm
	var resumeGuard func()
	for interruptedPID == 0 {
		if err := engine.readCheckpointJSON(relative+"/guard/capture-worker-intent.json", &worker); err == nil &&
			worker.Token == snapshot && worker.SourcePID == source.Runtime.PID &&
			worker.CriuSHA256 == engine.config.CriuSHA256 {
			birth, err := processBirth(worker.PID)
			if err == nil && birth == worker.Start {
				status, _ := os.ReadFile(fmt.Sprintf("/proc/%d/status", activePID))
				tracer := 0
				for _, line := range strings.Split(string(status), "\n") {
					fields := strings.Fields(line)
					if len(fields) == 2 && fields[0] == "TracerPid:" {
						tracer, _ = strconv.Atoi(fields[1])
					}
				}
				pagesWritten := false
				images, _ := os.ReadDir(filepath.Join(engine.config.Root, relative, "images"))
				for _, image := range images {
					if strings.HasPrefix(image.Name(), "pages-") && strings.HasSuffix(image.Name(), ".img") {
						if info, err := image.Info(); err == nil && info.Size() >= 8<<20 {
							pagesWritten = true
						}
					}
				}
				if os.Getenv("FACTORYBOX_CRIU_RETAINED_DIAGNOSTIC") == "1" {
					// Trace CRIU before page dumping starts, then stop it at
					// the first owned syscall boundary with enough actual pages.
					pagesWritten = true
				}
				if tracer > 1 && pagesWritten {
					fd, err := unix.PidfdOpen(tracer, 0)
					if err == nil {
						executable, statErr := os.Stat(fmt.Sprintf("/proc/%d/exe", tracer))
						expected, expectedErr := os.Stat(engine.config.Criu)
						processStatus, statusErr := os.ReadFile(fmt.Sprintf("/proc/%d/status", tracer))
						ownedParent := false
						for _, line := range strings.Split(string(processStatus), "\n") {
							fields := strings.Fields(line)
							if len(fields) == 2 && fields[0] == "PPid:" {
								ownedParent = fields[1] == strconv.Itoa(worker.PID)
							}
						}
						if statErr == nil && expectedErr == nil && statusErr == nil &&
							os.SameFile(executable, expected) && ownedParent {
							var finishTrace func()
							if os.Getenv("FACTORYBOX_CRIU_RETAINED_DIAGNOSTIC") == "1" {
								finishTrace = stopRetainedDumpNative(t, tracer, fd,
									filepath.Join(engine.config.Root, relative, "images"), activePID, activeVirtualPID)
								defer finishTrace()
							}
							if grantControl {
								heldGuard, resumeGuard = stopRetainedGuardNative(t, engine, relative, command.Process.Pid, tracer)
								defer resumeGuard()
							}
							// Read only the pinned original freezer at the actual
							// fault boundary. Write diagnostics after the kill so
							// output I/O cannot delay the injection window.
							freezerAtKill, freezerObservationErr = captureReadFile(originalGroup, "cgroup.freeze", 64)
							if threadChurn {
								if err := os.WriteFile(fmt.Sprintf("/proc/%d/root/state/thread-churn-trigger",
									source.Runtime.PID), []byte("1"), 0600); err != nil {
									t.Fatal("trigger only the owned actor after exact tracer stop", err)
								}
							}
							err = unix.PidfdSendSignal(fd, unix.SIGKILL, nil, 0)
							if err == nil {
								interruptedPID = tracer
							}
							if finishTrace != nil {
								finishTrace()
							}
						}
						unix.Close(fd)
					}
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("did not observe the pinned CRIU child actively tracing the original source")
		}
		if interruptedPID == 0 {
			time.Sleep(time.Millisecond)
		}
	}
	t.Logf("interrupted pinned CRIU PID %d after at least 8 MiB of the active process's page image while tracing original source PID %d, active PID %d", interruptedPID, source.Runtime.PID, activePID)
	if grantControl {
		probeRetainedGrantsNative(t, engine, relative, originalGroup, heldGuard)
		resumeGuard()
	}
	if err := command.Wait(); err == nil {
		t.Fatal("interrupted CRIU published a successful checkpoint")
	}
	waited = true
	for name, data := range map[string][]byte{
		"interruption-cli.json": stdout.Bytes(), "interruption-cli.stderr": stderr.Bytes(),
	} {
		if err := os.WriteFile("/output/"+name, data, 0600); err != nil {
			t.Fatal("save interrupted command outcome before validating recovery", err)
		}
	}
	if os.Getenv("FACTORYBOX_CRIU_RETAINED_DIAGNOSTIC") == "1" {
		var arm retainedCaptureArm
		if err := engine.readCheckpointJSON(relative+"/guard/criu-retained-arm.json", &arm); err != nil ||
			arm.Version != 3 || arm.Mode != "process_tree_retained" ||
			arm.SourcePID != source.Runtime.PID || arm.TracerPID != interruptedPID ||
			arm.ActivePID != activePID || arm.ActiveVirtualPID != activeVirtualPID ||
			arm.WorkerPID != worker.PID || arm.ControllerPID != command.Process.Pid {
			t.Fatal("retained-context diagnostic was not armed for its exact interrupted transaction", err, arm)
		}
		if birth, err := processBirth(source.Runtime.PID); err != nil || birth != arm.SourceStart {
			t.Fatal("retained-context diagnostic lost the original source process birth", err)
		}
		var cure retainedCaptureResult
		expectedProcesses := 1
		if multiprocess {
			expectedProcesses = 2
		}
		cureDeadline := time.Now().Add(20 * time.Second)
		for {
			err := engine.readCheckpointJSON(relative+"/guard/criu-retained-result.json", &cure)
			if err == nil {
				break
			}
			if time.Now().After(cureDeadline) {
				t.Fatal("retained-context diagnostic did not finish while preserving the source hold", err)
			}
			time.Sleep(10 * time.Millisecond)
		}
		if !cure.Passed || cure.Version != 3 || cure.SourcePID != arm.SourcePID ||
			cure.SourceStart != arm.SourceStart || cure.HelperPID != arm.HelperPID ||
			!cure.Registers || !cure.SignalMask || cure.Maps != 0 || !cure.Held ||
			cure.Threads != len(arm.Tree) || cure.ActivePID != arm.ActivePID ||
			cure.Processes != expectedProcesses || cure.Nonce != arm.Nonce {
			t.Fatal("retained-context producer did not prove its scoped cure", cure)
		}
		if err := engine.validateCgroup(source.Record); err != nil {
			t.Fatal(err)
		}
		events, err := captureGuardEvents(originalGroup)
		if err != nil || events["frozen"] > 1 || events["populated"] != 1 ||
			before.BallastBytes != 32<<20 || before.BallastAddress == 0 || before.BallastAddress > 1<<62 {
			t.Fatal("diagnostic cure did not retain its original frozen source and complete RAM bounds", events, err)
		}
		if events["frozen"] == 0 {
			if pauseMode {
				record, err := engine.read("interrupted")
				if err != nil || !cure.Authorized || record.Phase != "running" || record.Pause != nil ||
					len(record.PauseHistory) != 1 || record.PauseHistory[0].SnapshotID != snapshot ||
					record.PauseHistory[0].RejectedAt == "" || record.PauseHistory[0].Error == "" {
					t.Fatal("interrupted pause lost its confirmed running source or failed operation history", err, record)
				}
			} else {
				var recovered *checkpointCaptureError
				replayErr := engine.reconcileAbortedCheckpoint(source.Record, snapshot, relative)
				if !cure.Authorized || !errors.As(replayErr, &recovered) || !recovered.recovered {
					t.Fatal("source ran without the original guard's immutable recovery acknowledgement", replayErr)
				}
			}
		}
		maps, err := os.ReadFile(fmt.Sprintf("/proc/%d/maps", source.Runtime.PID))
		if err != nil || bytes.Contains(maps, []byte("CRIUMFD")) {
			t.Fatal("diagnostic cure left its injected mapping", err)
		}
		memory, err := os.Open(fmt.Sprintf("/proc/%d/mem", source.Runtime.PID))
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.New()
		read, hashErr := io.Copy(digest, io.NewSectionReader(memory, int64(before.BallastAddress), int64(before.BallastBytes)))
		closeErr := memory.Close()
		if hashErr != nil || closeErr != nil || read != int64(before.BallastBytes) ||
			fmt.Sprintf("%x", digest.Sum(nil)) != before.BallastSHA {
			t.Fatal("read-only diagnostic found changed original 32-MiB RAM", hashErr, closeErr, read)
		}
		encoded, err := json.Marshal(map[string]any{
			"passed": true, "source_pid": source.Runtime.PID, "source_held": events["frozen"] == 1,
			"guard_authorized": cure.Authorized,
			"ballast_bytes":    read, "ballast_sha256": fmt.Sprintf("%x", digest.Sum(nil)),
			"cure": cure, "scope": "Real page dump and retained C cure; source release requires the original guard's checked acknowledgement.",
		})
		if err != nil {
			t.Fatal("encode exact held-source diagnostic", err)
		}
		if err := os.WriteFile("/output/interruption-retained-context.json", append(encoded, '\n'), 0600); err != nil {
			t.Fatal("save exact held-source diagnostic", err)
		}
		t.Log("retained CRIU context removed its injected mapping and preserved all original 32-MiB RAM without diagnostic thaw")
	}
	freezerAfter, freezeErr := captureReadFile(originalGroup, "cgroup.freeze", 64)
	eventsAfter, eventsErr := captureReadFile(originalGroup, "cgroup.events", 4096)
	boundary, marshalErr := json.Marshal(map[string]any{
		"source_pid": source.Runtime.PID, "criu_pid": interruptedPID,
		"active_pid":      activePID,
		"freezer_at_kill": string(freezerAtKill), "freezer_after_wait": string(freezerAfter),
		"events_after_wait": string(eventsAfter),
	})
	if err := errors.Join(freezerObservationErr, freezeErr, eventsErr, marshalErr); err != nil {
		t.Fatal("read-only interruption-boundary observation failed", err)
	}
	if err := os.WriteFile("/output/interruption-boundary.json", append(boundary, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(engine.config.Root, relative, "checkpoint.json")); !os.IsNotExist(err) {
		t.Fatal("interrupted capture published complete backing", err)
	}
	for _, name := range []string{"capture-process-complete.json"} {
		if _, err := os.Stat(filepath.Join(engine.config.Root, relative, "guard", name)); !os.IsNotExist(err) {
			t.Fatal("interrupted CRIU gained a process-completion or safe-abort receipt", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(engine.config.Root, relative, "guard", "capture-process-restored.json")); err == nil {
		if !pauseMode {
			var recovered *checkpointCaptureError
			replayErr := engine.reconcileAbortedCheckpoint(source.Record, snapshot, relative)
			if !errors.As(replayErr, &recovered) || !recovered.recovered {
				t.Fatal("actual CRIU loss has an unproved recovery receipt", replayErr)
			}
		}
	} else {
		for _, name := range []string{"capture-abort-intent.json", "guard-capture-aborted.json"} {
			if _, err := os.Stat(filepath.Join(engine.config.Root, relative, "guard", name)); !os.IsNotExist(err) {
				t.Fatal("uncured CRIU loss gained an unsafe abort acknowledgement", name, err)
			}
		}
	}
	afterNetwork, networkErr := networkRules()
	for name, data := range map[string][]byte{
		"interruption-network-before.txt": beforeNetwork,
		"interruption-network-after.txt":  afterNetwork,
	} {
		if err := os.WriteFile("/output/"+name, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if networkErr != nil || !bytes.Equal(beforeNetwork, afterNetwork) {
		t.Errorf("interrupted capture did not restore its original private network rules: %v", networkErr)
	}
	afterSockets, socketErr := sockets()
	socketData, marshalErr := json.Marshal(map[string]any{"before": beforeSockets, "after": afterSockets})
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	if err := os.WriteFile("/output/interruption-sockets.json", append(socketData, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if socketErr != nil || !reflect.DeepEqual(beforeSockets, afterSockets) {
		t.Errorf("interrupted capture changed original socket identity or properties: %v; before=%+v after=%+v",
			socketErr, beforeSockets, afterSockets)
	}
	if err := exchange(); err != nil {
		t.Error("rolled-back original TCP sockets are not bidirectionally usable", err)
	}
	recovered, recoverErr := forkNativeCLI(engine, "interrupted", committed, "committed-recovery")
	if recoverErr != nil {
		t.Fatal("interruption also made the previous committed recovery point unusable", recoverErr)
	}
	t.Cleanup(func() {
		if _, err := engine.Delete(ctx, "committed-recovery"); err != nil {
			t.Error("delete exact committed recovery target", err)
		}
	})
	recoveredRAM, recoverErr := readCaptureActor(recovered.Runtime.PID, "get")
	if recoverErr != nil || !reflect.DeepEqual(before, recoveredRAM) {
		t.Fatal("previous committed checkpoint lost its complete independent RAM", recoverErr)
	}
	if _, err := readCaptureActor(recovered.Runtime.PID, "advance"); err != nil {
		t.Fatal("previous committed checkpoint cannot continue useful work", err)
	}
	t.Log("previous committed checkpoint remained independently usable after actual CRIU interruption")
	state, err := engine.Inspect(ctx, "interrupted")
	if err != nil || state.Runtime == nil || state.Runtime.Status != "running" || state.Runtime.PID != source.Runtime.PID {
		t.Fatalf("interrupted capture did not recover its original running source: %v; state=%+v; stderr=%s",
			err, state.Runtime, stderr.String())
	}
	after, err := readCaptureActor(source.Runtime.PID, "get")
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("interruption changed original private RAM", err)
	}
	advanced, err := readCaptureActor(source.Runtime.PID, "advance")
	if err != nil {
		t.Fatal("interrupted source cannot continue useful work", err)
	}
	if _, err := os.Stat(filepath.Join(engine.config.Root, relative, "guard", "capture-process-restored.json")); err == nil && !pauseMode {
		replay := func() error {
			_, err := engine.Checkpoint(ctx, "interrupted", snapshot)
			return err
		}
		var recovered *checkpointCaptureError
		if err := replay(); !errors.As(err, &recovered) || !recovered.recovered {
			t.Fatal("proved CRIU rollback did not retain its immutable failed-checkpoint outcome", err)
		}
		for name, field := range map[string]string{
			"criu-retained-arm.json":        "helper_start",
			"criu-retained-request.json":    "nonce",
			"criu-retained-result.json":     "nonce",
			"capture-process-restored.json": "arm_sha256",
			"capture-network-rollback.json": "source_start",
		} {
			path := filepath.Join(engine.config.Root, relative, "guard", name)
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var changed map[string]any
			if err := json.Unmarshal(original, &changed); err != nil {
				t.Fatal(err)
			}
			changed[field] = "tampered"
			encoded, err := json.Marshal(changed)
			if err != nil || os.WriteFile(path, encoded, 0600) != nil {
				t.Fatal("prepare owned receipt corruption control", name, err)
			}
			replayErr := replay()
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal("restore only the owned receipt control", name, err)
			}
			var unsafe *checkpointCaptureError
			if replayErr == nil || (errors.As(replayErr, &unsafe) && unsafe.recovered) {
				t.Fatal("tampered retained-context proof authorized recovery replay", name)
			}
			view, err := engine.Inspect(ctx, "interrupted")
			current, readErr := readCaptureActor(source.Runtime.PID, "get")
			if err != nil || view.Runtime == nil || view.Runtime.PID != source.Runtime.PID ||
				view.Runtime.Status != "running" || readErr != nil || !reflect.DeepEqual(current, advanced) {
				t.Fatal("refused proof replay changed the original running source", name, err, readErr)
			}
		}
		if err := replay(); !errors.As(err, &recovered) || !recovered.recovered {
			t.Fatal("restored receipt controls changed the retained original failure", err)
		}
		t.Log("immutable CRIU-loss replay passed and changed arm/grant/result/restoration/network proofs were refused without recapture")
	}
	if pauseMode {
		beforeReplay, err := engine.read("interrupted")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := engine.Pause(ctx, "interrupted", snapshot); err == nil {
			t.Fatal("failed pause identity was reused for a second capture")
		}
		afterReplay, err := engine.read("interrupted")
		current, readErr := readCaptureActor(source.Runtime.PID, "get")
		if err != nil || !reflect.DeepEqual(beforeReplay, afterReplay) ||
			readErr != nil || !reflect.DeepEqual(current, advanced) {
			t.Fatal("failed-pause replay changed retained history or original RAM", err, readErr)
		}
		t.Log("actual CRIU-loss pause restored running state, retained failure history and refused identity reuse")
	}
	if multiprocess {
		poll := []unix.PollFd{{Fd: int32(childFD), Events: unix.POLLIN}}
		n, err := unix.Poll(poll, 0)
		birth, birthErr := processBirth(childPID)
		if err != nil || n != 0 || birthErr != nil || birth != childStart {
			t.Fatal("CRIU-loss recovery replaced or lost the original second process", err, birthErr)
		}
		memory, err := os.Open(fmt.Sprintf("/proc/%d/mem", childPID))
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.New()
		read, hashErr := io.Copy(digest, io.NewSectionReader(memory, int64(childBefore.BallastAddress), int64(childBefore.BallastBytes)))
		closeErr := memory.Close()
		if hashErr != nil || closeErr != nil || read != 32<<20 ||
			fmt.Sprintf("%x", digest.Sum(nil)) != childBefore.BallastSHA {
			t.Fatal("CRIU-loss recovery changed the original child's complete 32-MiB RAM", hashErr, closeErr, read)
		}
		for _, pid := range []int{recovered.Runtime.PID, source.Runtime.PID} {
			current, err := readCaptureActorAt(pid, "/child-state", "get")
			if err != nil || !reflect.DeepEqual(current, childBefore) {
				t.Fatal("original or fork child lost its private RAM state", pid, err)
			}
			next, err := readCaptureActorAt(pid, "/child-state", "advance")
			if err != nil || next.Counter != childBefore.Counter+1 || next.BallastSHA == childBefore.BallastSHA {
				t.Fatal("original or fork child cannot independently continue useful work", pid, err)
			}
		}
		// Reap the cloned child through the restored parent, then independently
		// reap the original child. This checks both real parent-child relations.
		for _, pid := range []int{recovered.Runtime.PID, source.Runtime.PID} {
			after, err := readCaptureActor(pid, "drop-time-child")
			if err != nil || after.NestedPID != 0 || after.TimeMode != "" {
				t.Fatal("recovered process tree cannot signal and reap its own child", pid, err)
			}
		}
		evidence, err := json.Marshal(map[string]any{
			"passed": true, "source_pid": source.Runtime.PID, "child_pid": childPID,
			"child_start": childStart, "child_nested_pid": before.NestedPID,
			"original_child_preserved": true, "original_and_fork_child_reaped": true,
			"child_ballast_bytes": read, "child_ballast_sha256": childBefore.BallastSHA,
			"active_pid": activePID, "interrupted_child": interruptChild,
			"scope": "Two multithreaded Go processes in one Sysbox cgroup, each with 32 MiB of private RAM.",
		})
		if err != nil || os.WriteFile("/output/interruption-process-tree.json", append(evidence, '\n'), 0600) != nil {
			t.Fatal("save complete process-tree recovery proof", err)
		}
		t.Log("two-process CRIU-loss recovery preserved the exact original child and independent fork parent-child relations")
	}
	if threadChurn {
		deadline := time.Now().Add(2 * time.Second)
		for {
			data, err := os.ReadFile(fmt.Sprintf("/proc/%d/root/state/thread-churn-ran", source.Runtime.PID))
			if err == nil {
				tid, parseErr := strconv.Atoi(string(data))
				if parseErr != nil || tid <= 1 {
					t.Fatal("owned thread churn lacks its nonleader identity", parseErr)
				}
				evidence, err := json.Marshal(map[string]any{
					"passed": true, "source_pid": source.Runtime.PID, "retired_guest_tid": tid,
					"scope": "Owned nonleader thread exit triggered at the actual tracer stop. " +
						"All original RAM/TCP/source/recovery-fork assertions remain unchanged.",
				})
				if err != nil || os.WriteFile("/output/interruption-thread-churn.json",
					append(evidence, '\n'), 0600) != nil {
					t.Fatal("save thread-churn boundary proof", err)
				}
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("thread-churn control never executed its owned nonleader exit", err)
			}
			time.Sleep(time.Millisecond)
		}
	}
}
