//go:build linux

package devx_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"factorybox/internal/lifecycle"
	"factorybox/internal/testtrace"
	"golang.org/x/sys/unix"
)

type faultThread struct {
	PID     int    `json:"tid"`
	Process int    `json:"tgid"`
	Start   string `json:"start"`
}

type faultArm struct {
	Version          int           `json:"version"`
	SourcePID        int           `json:"source_pid"`
	TracerPID        int           `json:"tracer_pid"`
	WorkerPID        int           `json:"worker_pid"`
	ControllerPID    int           `json:"controller_pid"`
	ControllerStart  string        `json:"controller_start"`
	GuardPID         int           `json:"guard_pid"`
	GuardStart       string        `json:"guard_start"`
	HelperPID        int           `json:"helper_pid"`
	HelperStart      string        `json:"helper_start"`
	ActivePID        int           `json:"active_pid"`
	ActiveVirtualPID int           `json:"active_vpid"`
	ExitHoldArmed    bool          `json:"exit_hold_armed"`
	Nonce            string        `json:"nonce"`
	Tree             []faultThread `json:"tree"`
}

func faultProcessBirth(pid int) (string, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", err
	}
	end := bytes.LastIndexByte(data, ')')
	if end < 0 {
		return "", fmt.Errorf("invalid original process stat")
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) < 20 {
		return "", fmt.Errorf("short original process stat")
	}
	return fields[19], nil
}

func faultPageBytes(images string, pid int) int64 {
	file, err := os.Open(filepath.Join(images, fmt.Sprintf("pagemap-%d.img", pid)))
	if err != nil {
		return 0
	}
	var prefix [64]byte
	n, _ := file.ReadAt(prefix[:], 0)
	file.Close()
	if n < 14 || binary.LittleEndian.Uint32(prefix[:4]) != 0x54564319 ||
		binary.LittleEndian.Uint32(prefix[4:8]) != 0x56084025 {
		return 0
	}
	size := int(binary.LittleEndian.Uint32(prefix[8:12]))
	if size < 2 || size > 16 || n < 12+size || prefix[12] != 8 {
		return 0
	}
	id, length := binary.Uvarint(prefix[13 : 12+size])
	if length <= 0 || length+1 != size || id == 0 {
		return 0
	}
	info, err := os.Stat(filepath.Join(images, fmt.Sprintf("pages-%d.img", id)))
	if err != nil {
		return 0
	}
	return info.Size()
}

// An accepted AF_UNIX server socket inherits the connecting socket's network
// namespace. Create the test client in the captured namespace so that CRIU can
// enumerate a server FD even while Docker finishes closing a hijacked exec.
func dialDockerUnix(ctx context.Context, pid int, path string) (net.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	target, err := os.Open(fmt.Sprintf("/proc/%d/ns/net", pid))
	if err != nil {
		return nil, err
	}
	defer target.Close()
	runtime.LockOSThread()
	original, err := os.Open("/proc/thread-self/ns/net")
	if err != nil {
		runtime.UnlockOSThread()
		return nil, err
	}
	defer original.Close()
	if err := unix.Setns(int(target.Fd()), unix.CLONE_NEWNET); err != nil {
		runtime.UnlockOSThread()
		return nil, err
	}
	fd, socketErr := unix.Socket(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	restored := unix.Setns(int(original.Fd()), unix.CLONE_NEWNET)
	for errors.Is(restored, unix.EINTR) {
		restored = unix.Setns(int(original.Fd()), unix.CLONE_NEWNET)
	}
	if restored != nil {
		if fd >= 0 {
			unix.Close(fd)
		}
		// Never put a thread with a changed namespace back into Go's pool.
		panic(fmt.Errorf("restore the diagnostic client's original namespace: %w", restored))
	}
	runtime.UnlockOSThread()
	if socketErr != nil {
		return nil, socketErr
	}
	file := os.NewFile(uintptr(fd), "owned source-namespace Docker client")
	// Bound connect even if the owned server stops consuming its backlog.
	if err := unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_SNDTIMEO,
		&unix.Timeval{Sec: 15}); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	if err := unix.Connect(fd, &unix.SockaddrUnix{Name: path}); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	connection, err := net.FileConn(file)
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		if connection != nil {
			connection.Close()
		}
		return nil, errors.Join(err, closeErr)
	}
	return connection, nil
}

func (fixture *fixture) dockerRequest(pid int, method, path string, body []byte) []byte {
	fixture.t.Helper()
	transport := &http.Transport{
		DisableKeepAlives: true,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialDockerUnix(ctx, pid, fmt.Sprintf("/proc/%d/root/var/run/docker.sock", pid))
		},
	}
	defer transport.CloseIdleConnections()
	client := http.Client{Transport: transport, Timeout: 15 * time.Second}
	request, err := http.NewRequest(method, "http://owned-docker"+path, bytes.NewReader(body))
	if err != nil {
		fixture.t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		fixture.t.Fatal("original private Docker daemon stopped responding", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, (64<<10)+1))
	if err != nil || len(data) > 64<<10 || response.StatusCode < 200 || response.StatusCode >= 300 {
		fixture.t.Fatal("original private Docker API failed", method, path, response.StatusCode, err)
	}
	return data
}

func (fixture *fixture) interruptDockerCapture(source lifecycle.View, initial observedState) {
	fixture.t.Helper()
	target := os.Getenv("FACTORYBOX_DEVX_CRIU_FAULT")
	if target != "root" && target != "dockerd" {
		fixture.t.Fatal("Docker fault requires an exact root or dockerd process probe")
	}
	operation := os.Getenv("FACTORYBOX_DEVX_CRIU_FAULT_OPERATION")
	if operation == "" {
		operation = "checkpoint"
	}
	if operation != "checkpoint" && operation != "pause" {
		fixture.t.Fatal("Docker fault requires a checkpoint or pause operation")
	}
	component := os.Getenv("FACTORYBOX_DEVX_CRIU_FAULT_COMPONENT")
	if component == "" {
		component = "tracer"
	}
	if component != "tracer" && component != "helper" && component != "controller" && component != "guard" ||
		component == "helper" && operation != "checkpoint" {
		fixture.t.Fatal("Docker helper loss requires a checkpoint; tracer loss admits checkpoint or pause")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	waiter := testtrace.New("owned CRIU trace deadline expired")
	defer waiter.Close()
	type container struct {
		ID    string
		Image string
		State string
	}
	containers := func() []container {
		var result []container
		if err := json.Unmarshal(fixture.dockerRequest(source.Runtime.PID, "GET", "/containers/json", nil), &result); err != nil {
			fixture.t.Fatal(err)
		}
		sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
		if len(result) < 2 {
			fixture.t.Fatal("Docker fault lacks real running workspace/dependency containers")
		}
		for _, item := range result {
			if len(item.ID) != 64 || item.State != "running" {
				fixture.t.Fatal("Docker fault lacks stable running containers")
			}
		}
		return result
	}
	before := containers()
	exitHold := os.Getenv("FACTORYBOX_CRIU_EXIT_HOLD") == "1"
	requestedVirtualPID := 1
	if exitHold && target == "dockerd" {
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/root/run/private-docker.pid", source.Runtime.PID))
		if err != nil {
			fixture.t.Fatal("read only the original private Docker daemon's PID", err)
		}
		requestedVirtualPID, err = strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil || requestedVirtualPID <= 1 {
			fixture.t.Fatal("invalid original private Docker daemon PID", err)
		}
	}
	snapshot := strings.Repeat("e", 32)
	relative := filepath.Join(fixture.root, "objects/source/captures", snapshot)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/factorybox",
		"--root", fixture.root, "--runtime", "/out/sysbox-runc",
		"--runtime-sha256", os.Getenv("FACTORYBOX_RUNTIME_SHA256"),
		"--criu", "/out/criu", "--criu-sha256", os.Getenv("FACTORYBOX_CRIU_SHA256"),
		"--manager-root", "/var/lib/sysbox/manager",
		operation, "--snapshot-id", snapshot, "source")
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Start(); err != nil {
		fixture.t.Fatal(err)
	}
	waited := false
	defer func() {
		if !waited {
			command.Process.Kill()
			command.Wait()
		}
		os.WriteFile("/output/docker-fault-cli.json", stdout.Bytes(), 0600)
		os.WriteFile("/output/docker-fault-cli.stderr", stderr.Bytes(), 0600)
	}()
	var worker struct {
		Token     string `json:"token"`
		PID       int    `json:"worker_pid"`
		Start     string `json:"worker_start"`
		SourcePID int    `json:"source_pid"`
	}
	readJSON := func(name string, target any) error {
		data, err := os.ReadFile(filepath.Join(relative, "guard", name))
		if err != nil {
			return err
		}
		return json.Unmarshal(data, target)
	}
	deadline := time.Now().Add(30 * time.Second)
	tracer, tracerFD := 0, -1
	holdRequested := false
	for tracer == 0 && time.Now().Before(deadline) {
		if readJSON("capture-worker-intent.json", &worker) == nil &&
			worker.SourcePID == source.Runtime.PID && worker.Token == snapshot {
			birth, birthErr := faultProcessBirth(worker.PID)
			if exitHold && !holdRequested && birthErr == nil && birth == worker.Start {
				if err := testtrace.RequestPageHold(filepath.Join(relative, "guard"), snapshot, requestedVirtualPID); err != nil {
					fixture.t.Fatal("request only the owned Docker page boundary", err)
				}
				holdRequested = true
			}
			status, statusErr := os.ReadFile(fmt.Sprintf("/proc/%d/status", source.Runtime.PID))
			if birthErr == nil && birth == worker.Start && statusErr == nil {
				for _, line := range strings.Split(string(status), "\n") {
					fields := strings.Fields(line)
					if len(fields) == 2 && fields[0] == "TracerPid:" {
						candidate, _ := strconv.Atoi(fields[1])
						if candidate <= 1 {
							continue
						}
						fd, err := unix.PidfdOpen(candidate, 0)
						if err != nil {
							continue
						}
						actual, actualErr := os.Stat(fmt.Sprintf("/proc/%d/exe", candidate))
						expected, expectedErr := os.Stat("/out/criu")
						state, stateErr := os.ReadFile(fmt.Sprintf("/proc/%d/status", candidate))
						if actualErr == nil && expectedErr == nil && stateErr == nil &&
							os.SameFile(actual, expected) &&
							strings.Contains(string(state), fmt.Sprintf("PPid:\t%d\n", worker.PID)) {
							tracer, tracerFD = candidate, fd
							break
						}
						unix.Close(fd)
					}
				}
			}
		}
		if tracer == 0 {
			time.Sleep(time.Millisecond)
		}
	}
	if tracerFD < 0 {
		fixture.t.Fatal("did not observe the exact owned CRIU tracing real Docker")
	}
	defer unix.Close(tracerFD)
	if !exitHold {
		if err := unix.PtraceSeize(tracer); err != nil {
			fixture.t.Fatal(err)
		}
	}
	tracing := true
	defer func() {
		if tracing {
			unix.PidfdSendSignal(tracerFD, unix.SIGKILL, nil, 0)
			if exitHold {
				testtrace.WaitDead(tracerFD, time.Now().Add(2*time.Second))
			} else {
				waiter.Wait(tracer, time.Now().Add(2*time.Second))
			}
		}
	}()
	var status unix.WaitStatus
	var err error
	if !exitHold {
		if err := unix.PtraceInterrupt(tracer); err != nil {
			fixture.t.Fatal(err)
		}
		status, err = waiter.Wait(tracer, deadline)
		if err != nil || !status.Stopped() {
			fixture.t.Fatal("owned CRIU did not stop for injection", err, status)
		}
		if err := unix.PtraceSetOptions(tracer, unix.PTRACE_O_TRACESYSGOOD); err != nil {
			fixture.t.Fatal(err)
		}
	}
	deadline = time.Now().Add(30 * time.Second)
	var arm faultArm
	var pageBytes int64
	var activeComm string
	for {
		if readJSON("criu-retained-arm.json", &arm) == nil &&
			arm.Version == 3 && arm.TracerPID == tracer && arm.WorkerPID == worker.PID &&
			arm.ControllerPID == command.Process.Pid && arm.SourcePID == source.Runtime.PID &&
			arm.ActivePID > 1 && arm.ActiveVirtualPID > 0 {
			comm, commErr := os.ReadFile(fmt.Sprintf("/proc/%d/comm", arm.ActivePID))
			activeComm = strings.TrimSpace(string(comm))
			matches := target == "root" && arm.ActivePID == source.Runtime.PID && arm.ActiveVirtualPID == 1
			if target == "dockerd" {
				matches = commErr == nil && activeComm == "dockerd" &&
					arm.ActivePID != source.Runtime.PID && arm.ActiveVirtualPID > 1
			}
			if matches {
				pageBytes = faultPageBytes(filepath.Join(relative, "images"), arm.ActiveVirtualPID)
				if pageBytes >= 4096 {
					if !exitHold {
						break
					}
					if !arm.ExitHoldArmed || arm.ActiveVirtualPID != requestedVirtualPID {
						fixture.t.Fatal("the requested Docker fault has no armed exit observer")
					}
					held, err := testtrace.PageHeld(filepath.Join(relative, "guard"), snapshot,
						tracer, arm.HelperPID, arm.ActivePID, arm.ActiveVirtualPID, arm.Nonce)
					if err != nil {
						fixture.t.Fatal(err)
					}
					if held {
						break
					}
				}
			}
		}
		if exitHold {
			if time.Now().After(deadline) {
				fixture.t.Fatal("CRIU did not reach the exact acknowledged Docker page-dump boundary")
			}
			time.Sleep(time.Millisecond)
			continue
		}
		signal := int(status.StopSignal())
		if signal == int(unix.SIGTRAP) || signal == int(unix.SIGTRAP)|0x80 {
			signal = 0
		}
		if err := unix.PtraceSyscall(tracer, signal); err != nil {
			fixture.t.Fatal(err)
		}
		status, err = waiter.Wait(tracer, deadline)
		if err != nil || !status.Stopped() {
			fixture.t.Fatal("CRIU exited before the real Docker "+target+" page-dump boundary", err, status)
		}
	}
	processes, retained := map[int]bool{}, map[int]faultThread{}
	for _, thread := range arm.Tree {
		processes[thread.Process] = true
		if thread.PID != thread.Process {
			continue
		}
		comm, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", thread.PID))
		if err != nil {
			fixture.t.Fatal(err)
		}
		switch strings.TrimSpace(string(comm)) {
		case "docker-init", "dockerd", "containerd", "devx-actor", "devx-registry", "node":
			retained[thread.PID] = thread
		}
	}
	if len(processes) < 4 || len(retained) < 4 {
		fixture.t.Fatal("fault did not cover a real Docker process tree", len(processes), len(retained))
	}
	if target == "dockerd" {
		thread, found := retained[arm.ActivePID]
		birth, birthErr := faultProcessBirth(arm.ActivePID)
		if !found || thread.Process != arm.ActivePID || birthErr != nil || birth != thread.Start {
			fixture.t.Fatal("fault did not reach the exact original Docker daemon child", birthErr)
		}
	}
	armed, err := json.MarshalIndent(arm, "", "  ")
	if err != nil || os.WriteFile("/output/docker-fault-arm.json", append(armed, '\n'), 0600) != nil {
		fixture.t.Fatal("save the exact armed identities before injecting the fault", err)
	}
	var cure struct {
		Passed       bool   `json:"passed"`
		Authorized   bool   `json:"guard_authorized"`
		Nonce        string `json:"nonce"`
		Processes    int    `json:"processes"`
		Threads      int    `json:"threads"`
		Registers    bool   `json:"registers_match"`
		SignalMask   bool   `json:"signal_mask_match"`
		Maps         int    `json:"injected_maps_after"`
		Held         bool   `json:"source_held"`
		ExitStopHeld bool   `json:"exit_stop_held"`
	}
	var cureProof any
	helperKilled := false
	controllerKilled := false
	guardKilled := false
	controllerWaited := false
	identitiesPreserved := false
	var resumeGuard func()
	workerFD := -1
	if component == "controller" || component == "guard" {
		if component == "guard" && !exitHold {
			fixture.t.Fatal("guard-loss proof requires the pinned exit-observer candidate")
		}
		resumeGuard = fixture.stopOriginalFaultGuard(arm)
		defer resumeGuard()
		workerFD, err = unix.PidfdOpen(worker.PID, 0)
		if err != nil {
			fixture.t.Fatal("pin the original live capture worker", err)
		}
		defer unix.Close(workerFD)
		if birth, err := faultProcessBirth(worker.PID); err != nil || birth != worker.Start {
			fixture.t.Fatal("capture worker changed before recovery-owner loss injection", err)
		}
	}
	if component == "helper" {
		helperKilled = fixture.killRetainedHelper(arm, tracer, source.Runtime.PID)
		if exitHold {
			if err := unix.PidfdSendSignal(tracerFD, unix.SIGCONT, nil, 0); err != nil {
				fixture.t.Fatal("resume only the original healthy tracer after helper death", err)
			}
		} else if err := unix.PtraceDetach(tracer); err != nil {
			fixture.t.Fatal("resume only the original healthy tracer", err)
		}
		tracing = false
		err = command.Wait()
		waited = true
		if err != nil {
			fixture.t.Fatal("healthy tracer failed after its retained helper died", err, stderr.String())
		}
		var checkpoint lifecycle.Checkpoint
		if err := json.Unmarshal(stdout.Bytes(), &checkpoint); err != nil ||
			checkpoint.ID != snapshot || checkpoint.SourceID != source.Record.ID ||
			checkpoint.RuntimeID != source.Runtime.ID || checkpoint.Owner != source.Record.Owner ||
			checkpoint.SourcePID != source.Runtime.PID || checkpoint.ReleasedAt == "" ||
			checkpoint.SourceStopped || checkpoint.Filesystem == nil || len(checkpoint.Files) == 0 {
			fixture.t.Fatal("helper loss has no complete published source checkpoint", err)
		}
	} else {
		if err := unix.PidfdSendSignal(tracerFD, unix.SIGKILL, nil, 0); err != nil {
			fixture.t.Fatal(err)
		}
		if exitHold {
			if err := testtrace.WaitDead(tracerFD, time.Now().Add(2*time.Second)); err != nil {
				fixture.t.Fatal(err)
			}
		} else {
			status, err = waiter.Wait(tracer, time.Now().Add(2*time.Second))
			if err != nil || (!status.Signaled() && !status.Exited()) {
				fixture.t.Fatal("exact CRIU trace was not reaped", err, status)
			}
		}
		tracing = false
		if component == "controller" || component == "guard" {
			if err := testtrace.WaitDead(workerFD, time.Now().Add(5*time.Second)); err != nil {
				fixture.t.Fatal("original worker did not finish before recovery-owner loss", err)
			}
			var rollback struct {
				Token  string `json:"token"`
				PID    int    `json:"source_pid"`
				Passed bool   `json:"passed"`
				Frozen bool   `json:"source_frozen"`
			}
			if err := readJSON("capture-network-rollback.json", &rollback); err != nil ||
				rollback.Token != snapshot || rollback.PID != source.Runtime.PID || !rollback.Passed || !rollback.Frozen {
				fixture.t.Fatal("recovery-owner loss lacks checked original network rollback", err)
			}
		}
		if component == "controller" {
			fd, err := unix.PidfdOpen(command.Process.Pid, 0)
			if err != nil {
				fixture.t.Fatal(err)
			}
			birth, birthErr := faultProcessBirth(command.Process.Pid)
			actual, actualErr := os.Stat(fmt.Sprintf("/proc/%d/exe", command.Process.Pid))
			expected, expectedErr := os.Stat("/factorybox")
			if command.Process.Pid != arm.ControllerPID || birthErr != nil || birth != arm.ControllerStart ||
				actualErr != nil || expectedErr != nil || !os.SameFile(actual, expected) {
				unix.Close(fd)
				fixture.t.Fatal("controller loss cannot pin its original CLI identity")
			}
			killErr := unix.PidfdSendSignal(fd, unix.SIGKILL, nil, 0)
			exitErr := testtrace.WaitDead(fd, time.Now().Add(2*time.Second))
			unix.Close(fd)
			if err := errors.Join(killErr, exitErr); err != nil {
				fixture.t.Fatal("kill only the original CLI after tracer/worker rollback", err)
			}
			controllerKilled = true
			resumeGuard()
		} else if component == "guard" {
			guardFD, err := unix.PidfdOpen(arm.GuardPID, 0)
			if err != nil {
				fixture.t.Fatal("pin original guard before accepted-grant loss", err)
			}
			defer unix.Close(guardFD)
			if birth, err := faultProcessBirth(arm.GuardPID); err != nil || birth != arm.GuardStart {
				fixture.t.Fatal("original guard changed before accepted-grant loss", err)
			}
			resumeCure := fixture.holdOriginalFaultCure(arm, source, resumeGuard)
			defer resumeCure()
			killErr := unix.PidfdSendSignal(guardFD, unix.SIGKILL, nil, 0)
			exitErr := testtrace.WaitDead(guardFD, time.Now().Add(2*time.Second))
			if err := errors.Join(killErr, exitErr); err != nil {
				fixture.t.Fatal("kill only the original guard after its accepted grant", err)
			}
			guardKilled = true
			controllerFD, err := unix.PidfdOpen(command.Process.Pid, 0)
			if err != nil {
				fixture.t.Fatal("original controller exited before retained cure", err)
			}
			defer unix.Close(controllerFD)
			if birth, err := faultProcessBirth(command.Process.Pid); err != nil ||
				command.Process.Pid != arm.ControllerPID || birth != arm.ControllerStart {
				fixture.t.Fatal("original controller changed during accepted-grant loss", err)
			}
			for deadline := time.Now().Add(100 * time.Millisecond); time.Now().Before(deadline); {
				poll := []unix.PollFd{{Fd: int32(controllerFD), Events: unix.POLLIN}}
				count, err := unix.Poll(poll, int(time.Until(deadline).Milliseconds())+1)
				if errors.Is(err, unix.EINTR) {
					continue
				}
				if err != nil || count != 0 {
					fixture.t.Fatal("original controller did not wait for retained cure", err, count)
				}
				controllerWaited = true
			}
			if !controllerWaited {
				fixture.t.Fatal("original controller wait was not observed")
			}
			resumeCure()
		}
		err = command.Wait()
		waited = true
		if err == nil {
			fixture.t.Fatal("killed CRIU published a successful developer " + operation)
		}
		if controllerKilled {
			replay := exec.CommandContext(ctx, command.Path, command.Args[1:]...)
			replay.WaitDelay = 2 * time.Second
			output, replayErr := replay.CombinedOutput()
			if err := os.WriteFile("/output/docker-controller-replay.stderr", output, 0600); err != nil {
				fixture.t.Fatal(err)
			}
			if replayErr == nil || !bytes.Contains(output, []byte("after source process recovery")) {
				fixture.t.Fatal("controller loss has no proved replay of its original failed transaction", replayErr, string(output))
			}
		}
		if controllerKilled || guardKilled {
			var current faultArm
			if err := readJSON("criu-retained-arm.json", &current); err != nil || !reflect.DeepEqual(current, arm) {
				fixture.t.Fatal("recovery-owner loss replaced original armed identities", err)
			}
			var grant struct {
				Guard      int    `json:"guard_pid"`
				Controller int    `json:"controller_pid"`
				Nonce      string `json:"nonce"`
			}
			if err := readJSON("criu-retained-request.json", &grant); err != nil ||
				grant.Guard != arm.GuardPID || grant.Controller != arm.ControllerPID || grant.Nonce != arm.Nonce {
				fixture.t.Fatal("recovery-owner loss replaced the original recovery authority", err)
			}
			identitiesPreserved = true
		}
		if err := readJSON("criu-retained-result.json", &cure); err != nil ||
			!cure.Passed || !cure.Authorized || cure.Nonce != arm.Nonce ||
			cure.Processes != len(processes) || cure.Threads != len(arm.Tree) ||
			!cure.Registers || !cure.SignalMask || cure.Maps != 0 || !cure.Held ||
			cure.ExitStopHeld != arm.ExitHoldArmed {
			fixture.t.Fatal("real Docker source has no complete authorized cure", err, cure)
		}
		cureProof = cure
	}
	for pid, thread := range retained {
		birth, err := faultProcessBirth(pid)
		maps, mapsErr := os.ReadFile(fmt.Sprintf("/proc/%d/maps", pid))
		if err != nil || birth != thread.Start || mapsErr != nil || bytes.Contains(maps, []byte("CRIUMFD")) {
			fixture.t.Fatal("Docker recovery replaced or failed to cure an original process", pid, err, mapsErr)
		}
	}
	view := fixture.view("state", "source")
	if view.Runtime == nil || view.Runtime.PID != source.Runtime.PID || view.Record.Phase != "running" {
		fixture.t.Fatal("Docker recovery did not retain its exact running source")
	}
	failedPauseHistory, failedIdentityReplay := false, false
	if operation == "pause" {
		history := view.Record.PauseHistory
		if view.Record.Pause != nil || len(history) != len(source.Record.PauseHistory)+1 ||
			len(source.Record.PauseHistory) != 0 && !reflect.DeepEqual(history[:len(history)-1], source.Record.PauseHistory) {
			fixture.t.Fatal("failed real-Docker pause did not retain exactly its rejected transition")
		}
		failed := history[len(history)-1]
		if failed.SnapshotID != snapshot || failed.SourcePID != source.Runtime.PID ||
			failed.RequestedAt == "" || failed.RejectedAt == "" || failed.Error == "" ||
			failed.CompletedAt != "" || failed.Resume != nil {
			fixture.t.Fatal("failed real-Docker pause has no complete rejection history")
		}
		failedPauseHistory = true
		// Reusing the same failed identity must stop before another capture,
		// and must not append another history entry or change the source.
		replay := exec.CommandContext(ctx, command.Path, command.Args[1:]...)
		replay.WaitDelay = 2 * time.Second
		replayOutput, replayErr := replay.CombinedOutput()
		if err := os.WriteFile("/output/docker-fault-replay.stderr", replayOutput, 0600); err != nil {
			fixture.t.Fatal(err)
		}
		if replayErr == nil || !bytes.Contains(replayOutput, []byte("pause snapshot identity already exists")) {
			fixture.t.Fatal("failed pause identity was not refused before recapture", replayErr)
		}
		again := fixture.view("state", "source")
		if !reflect.DeepEqual(again, view) {
			fixture.t.Fatal("failed pause identity replay changed the original record or runtime")
		}
		failedIdentityReplay = true
	}
	after, err := readApp(source.Runtime.PID, "")
	if err != nil || !reflect.DeepEqual(after, initial) || !reflect.DeepEqual(containers(), before) {
		fixture.t.Fatal("Docker recovery changed app RAM, Connector or running containers", err)
	}
	var execution struct{ ID string }
	nonce := "factorybox-after-criu-loss-" + arm.Nonce
	body, err := json.Marshal(map[string]any{
		"AttachStdout": true, "AttachStderr": true,
		"Cmd": []string{"/bin/sh", "-c", "printf '%s' \"$1\"", "factorybox-fault", nonce},
	})
	if err != nil {
		fixture.t.Fatal(err)
	}
	if err := json.Unmarshal(fixture.dockerRequest(source.Runtime.PID, "POST",
		"/containers/"+before[0].ID+"/exec", body), &execution); err != nil || len(execution.ID) != 64 {
		fixture.t.Fatal("original Docker cannot create a new exec after CRIU loss", err)
	}
	output := fixture.dockerRequest(source.Runtime.PID, "POST", "/exec/"+execution.ID+"/start",
		[]byte(`{"Detach":false,"Tty":false}`))
	var finished struct {
		ID       string
		Running  *bool
		ExitCode *int
	}
	if err := json.Unmarshal(fixture.dockerRequest(source.Runtime.PID, "GET",
		"/exec/"+execution.ID+"/json", nil), &finished); err != nil ||
		finished.ID != execution.ID || finished.Running == nil || *finished.Running ||
		finished.ExitCode == nil || *finished.ExitCode != 0 || !bytes.Contains(output, []byte(nonce)) {
		fixture.t.Fatal("original Docker cannot complete useful new work after CRIU loss", err)
	}
	fault := "root-process-page-dump"
	if target == "dockerd" {
		fault = "docker-daemon-child-page-dump"
	}
	if component == "helper" {
		fault = "retained-helper-loss-during-" + target + "-page-dump"
	} else if component == "controller" {
		fault = "controller-loss-after-" + target + "-page-dump"
	} else if component == "guard" {
		fault = "guard-loss-after-accepted-grant-" + target + "-page-dump"
	}
	boundary := target + " page-dump " + component + " death during " + operation
	if controllerKilled {
		boundary = target + " page-dump tracer death, then original CLI death after worker/network rollback before cure"
	} else if guardKilled {
		boundary = target + " page-dump tracer death, then original guard death at the helper's source-freezer open after grant acceptance"
	}
	record, err := json.MarshalIndent(map[string]any{
		"passed": true, "fault": fault, "operation": operation, "component": component, "active_page_bytes": pageBytes,
		"retained_helper_killed": helperKilled, "checkpoint_published_after_helper_loss": helperKilled,
		"controller_killed": controllerKilled, "same_original_guard": controllerKilled,
		"guard_killed": guardKilled, "controller_waited_for_cure": controllerWaited,
		"original_recovery_identities_preserved": identitiesPreserved,
		"controller_pid":                         arm.ControllerPID, "guard_pid": arm.GuardPID,
		"helper_pid": arm.HelperPID, "helper_start": arm.HelperStart,
		"failed_pause_history_retained": failedPauseHistory, "failed_identity_replay_refused": failedIdentityReplay,
		"active_pid": arm.ActivePID, "active_vpid": arm.ActiveVirtualPID, "active_comm": activeComm,
		"source_pid": source.Runtime.PID, "tracer_pid": tracer, "processes": len(processes),
		"threads": len(arm.Tree), "retained_processes": retained, "cure": cureProof,
		"unchanged_app": after, "unchanged_containers": before, "docker_exec_exit": *finished.ExitCode,
		"scope": "Real Docker/Compose/Dev Container process tree, " + boundary + ", " +
			"original stable process identities/app state and new Docker exec. " +
			"No other process target, before-grant guard death, earlier CLI loss, " +
			"simultaneous tracer/helper loss or external agent inference qualification.",
	}, "", "  ")
	if err != nil || os.WriteFile("/output/devx-criu-interruption.json", append(record, '\n'), 0600) != nil {
		fixture.t.Fatal("save complete developer fault result", err)
	}
	fixture.t.Logf("real Docker %s page-dump %s loss preserved %d processes/%d threads; new Docker exec passed",
		target, component, len(processes), len(arm.Tree))
}

func (fixture *fixture) stopOriginalFaultGuard(arm faultArm) func() {
	fixture.t.Helper()
	if arm.GuardPID <= 1 || arm.GuardStart == "" || arm.GuardPID == arm.ControllerPID ||
		arm.GuardPID == arm.WorkerPID || arm.GuardPID == arm.TracerPID || arm.GuardPID == arm.HelperPID {
		fixture.t.Fatal("original guard identity overlaps another capture role")
	}
	for _, thread := range arm.Tree {
		if thread.PID == arm.GuardPID {
			fixture.t.Fatal("original guard is in the captured process tree")
		}
	}
	fd, err := unix.PidfdOpen(arm.GuardPID, 0)
	if err != nil {
		fixture.t.Fatal(err)
	}
	birth, birthErr := faultProcessBirth(arm.GuardPID)
	actual, actualErr := os.Stat(fmt.Sprintf("/proc/%d/exe", arm.GuardPID))
	expected, expectedErr := os.Stat("/factorybox")
	status, statusErr := os.ReadFile(fmt.Sprintf("/proc/%d/status", arm.GuardPID))
	if birthErr != nil || birth != arm.GuardStart || actualErr != nil || expectedErr != nil ||
		!os.SameFile(actual, expected) || statusErr != nil ||
		!strings.Contains(string(status), fmt.Sprintf("PPid:\t%d\n", arm.ControllerPID)) {
		unix.Close(fd)
		fixture.t.Fatal("original guard birth, executable or parent changed")
	}
	resume := func() {
		if fd >= 0 {
			if err := unix.PidfdSendSignal(fd, unix.SIGCONT, nil, 0); err != nil && !errors.Is(err, unix.ESRCH) {
				fixture.t.Error("resume only the original guard", err)
			}
			unix.Close(fd)
			fd = -1
		}
	}
	fixture.t.Cleanup(resume)
	if err := unix.PidfdSendSignal(fd, unix.SIGSTOP, nil, 0); err != nil {
		fixture.t.Fatal(err)
	}
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", arm.GuardPID))
		if err == nil && strings.Contains(string(status), "State:\tT") {
			return resume
		}
		time.Sleep(time.Millisecond)
	}
	fixture.t.Fatal("original guard did not stop at the recovery-owner loss boundary")
	return resume
}

func (fixture *fixture) holdOriginalFaultCure(arm faultArm, source lifecycle.View, resumeGuard func()) func() {
	fixture.t.Helper()
	if arm.HelperPID <= 1 || arm.HelperStart == "" || source.Record.Cgroup == nil ||
		arm.HelperPID == arm.GuardPID || arm.HelperPID == arm.ControllerPID ||
		arm.HelperPID == arm.WorkerPID || arm.HelperPID == arm.TracerPID {
		fixture.t.Fatal("retained cure helper identity overlaps another capture role")
	}
	for _, thread := range arm.Tree {
		if thread.PID == arm.HelperPID {
			fixture.t.Fatal("retained cure helper is in the captured source tree")
		}
	}
	fd, err := unix.PidfdOpen(arm.HelperPID, 0)
	if err != nil {
		fixture.t.Fatal("pin original retained cure helper", err)
	}
	defer unix.Close(fd)
	birth, birthErr := faultProcessBirth(arm.HelperPID)
	actual, actualErr := os.Stat(fmt.Sprintf("/proc/%d/exe", arm.HelperPID))
	expected, expectedErr := os.Stat("/out/criu")
	if birthErr != nil || birth != arm.HelperStart || actualErr != nil || expectedErr != nil ||
		!os.SameFile(actual, expected) {
		fixture.t.Fatal("original retained cure helper birth or executable changed")
	}
	group := source.Record.Cgroup
	return testtrace.HoldCure(fixture.t, arm.HelperPID, fd,
		testtrace.Cgroup{Device: group.Device, Inode: group.Inode, MountID: group.MountID}, resumeGuard)
}

func (fixture *fixture) killRetainedHelper(arm faultArm, tracer, source int) bool {
	fixture.t.Helper()
	if arm.HelperPID <= 1 || arm.HelperPID == tracer || arm.HelperPID == source ||
		arm.HelperPID == arm.WorkerPID || arm.HelperPID == arm.ControllerPID || arm.HelperStart == "" {
		fixture.t.Fatal("retained helper identity overlaps another original process")
	}
	for _, thread := range arm.Tree {
		if thread.PID == arm.HelperPID {
			fixture.t.Fatal("retained helper is in the captured source tree")
		}
	}
	fd, err := unix.PidfdOpen(arm.HelperPID, 0)
	if err != nil {
		fixture.t.Fatal("pin exact retained helper", err)
	}
	defer unix.Close(fd)
	birth, birthErr := faultProcessBirth(arm.HelperPID)
	actual, actualErr := os.Stat(fmt.Sprintf("/proc/%d/exe", arm.HelperPID))
	expected, expectedErr := os.Stat("/out/criu")
	state, stateErr := os.ReadFile(fmt.Sprintf("/proc/%d/status", arm.HelperPID))
	if birthErr != nil || birth != arm.HelperStart || actualErr != nil || expectedErr != nil ||
		!os.SameFile(actual, expected) || stateErr != nil ||
		!strings.Contains(string(state), fmt.Sprintf("PPid:\t%d\n", tracer)) {
		fixture.t.Fatal("retained helper birth, parent or pinned executable changed")
	}
	if ready, err := unix.Poll([]unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}, 0); err != nil || ready != 0 {
		fixture.t.Fatal("retained helper already exited before injection", ready, err)
	}
	if err := unix.PidfdSendSignal(fd, unix.SIGKILL, nil, 0); err != nil {
		fixture.t.Fatal("kill only the pinned retained helper", err)
	}
	if ready, err := unix.Poll([]unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}, 2000); err != nil || ready != 1 {
		fixture.t.Fatal("retained helper death was not observed", ready, err)
	}
	return true
}

func TestNativeDockerCaptureInterruption(t *testing.T) {
	if target := os.Getenv("FACTORYBOX_DEVX_CRIU_FAULT"); target != "root" && target != "dockerd" {
		t.Skip("requires the owned real-Docker root or dockerd page-dump fault fixture")
	}
	fixture, source, initial := prepareDeveloperFixture(t)
	prior := strings.Repeat("a", 32)
	fixture.command("checkpoint", "--snapshot-id", prior, "source")
	fixture.interruptDockerCapture(source, initial)
	if os.Getenv("FACTORYBOX_DEVX_CRIU_FAULT_COMPONENT") == "helper" {
		start := time.Now()
		branch := fixture.view("fork", "--source", "source", "--snapshot-id",
			strings.Repeat("e", 32), "helper-loss-snapshot")
		fixture.cleanup("helper-loss-snapshot")
		cloned := fixture.useful("fork-helper-loss-first-use", branch, start, "")
		if !reflect.DeepEqual(initial, cloned) || branch.Runtime.ID == source.Runtime.ID {
			t.Fatal("checkpoint completed under helper loss has no independent complete fork")
		}
		mutated := fixture.useful("helper-loss-branch-advance", branch, time.Now(),
			`{"increment":1,"marker":"helper-loss-branch"}`)
		sourceAgain := fixture.useful("helper-loss-source-independent", source, time.Now(), "")
		if mutated.App.Counter != 1 || mutated.App.Marker != "helper-loss-branch" ||
			!reflect.DeepEqual(sourceAgain, initial) {
			t.Fatal("helper-loss checkpoint fork changed the original source")
		}
		fixture.view("delete", "helper-loss-snapshot")
	}
	sourceAfter := fixture.useful("recovered-source-advance", source, time.Now(),
		`{"increment":1,"marker":"recovered-source"}`)
	if sourceAfter.App.Counter != 1 || sourceAfter.App.Marker != "recovered-source" {
		t.Fatal("recovered source cannot mutate its original RAM and workspace")
	}
	// Capture the cured original, rather than only restoring a pre-fault
	// checkpoint. Leftover parasite state must not survive a second capture.
	current := strings.Repeat("f", 32)
	fixture.command("checkpoint", "--snapshot-id", current, "source")
	start := time.Now()
	branch := fixture.view("fork", "--source", "source", "--snapshot-id", current, "after-interruption")
	fixture.cleanup("after-interruption")
	cloned := fixture.useful("fork-after-interruption-first-use", branch, start, "")
	if !reflect.DeepEqual(sourceAfter, cloned) || branch.Runtime.ID == source.Runtime.ID {
		t.Fatal("cured source cannot produce a complete independent current-state fork")
	}
	branchAfter := fixture.useful("recovered-branch-advance", branch, time.Now(),
		`{"increment":1,"marker":"recovered-branch"}`)
	sourceAgain := fixture.useful("recovered-source-independent", source, time.Now(), "")
	if branchAfter.App.Counter != 2 || branchAfter.App.Marker != "recovered-branch" ||
		!reflect.DeepEqual(sourceAfter, sourceAgain) {
		t.Fatal("fork of the cured source lost RAM/filesystem independence")
	}
	fixture.view("delete", "after-interruption")
	start = time.Now()
	previous := fixture.view("fork", "--source", "source", "--snapshot-id", prior, "before-interruption")
	fixture.cleanup("before-interruption")
	previousState := fixture.useful("fork-before-interruption-first-use", previous, start, "")
	if !reflect.DeepEqual(initial, previousState) || previous.Runtime.ID == source.Runtime.ID {
		t.Fatal("CRIU loss or subsequent capture damaged the previous immutable checkpoint")
	}
}
