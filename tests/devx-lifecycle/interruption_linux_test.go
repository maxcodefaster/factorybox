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
	"os/signal"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"factorybox/internal/lifecycle"
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
	ActivePID        int           `json:"active_pid"`
	ActiveVirtualPID int           `json:"active_vpid"`
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

type faultTraceWaiter struct {
	events chan os.Signal
}

func newFaultTraceWaiter() *faultTraceWaiter {
	waiter := &faultTraceWaiter{events: make(chan os.Signal, 1)}
	signal.Notify(waiter.events, unix.SIGCHLD)
	return waiter
}

func (waiter *faultTraceWaiter) close() {
	signal.Stop(waiter.events)
}

func (waiter *faultTraceWaiter) wait(pid int, deadline time.Time) (unix.WaitStatus, error) {
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	for time.Now().Before(deadline) {
		var status unix.WaitStatus
		actual, err := unix.Wait4(pid, &status, unix.WALL|unix.WNOHANG, nil)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil || actual == pid {
			return status, err
		}
		// Register before tracing begins and always drain the exact PID first.
		// A buffered notification covers a stop between Wait4 and this select;
		// unrelated child events only cause another exact-PID check.
		select {
		case <-waiter.events:
		case <-timer.C:
			return 0, fmt.Errorf("owned CRIU trace deadline expired")
		}
	}
	return 0, fmt.Errorf("owned CRIU trace deadline expired")
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
	if os.Getenv("FACTORYBOX_DEVX_CRIU_FAULT") != "root" {
		fixture.t.Fatal("Docker fault requires the exact root process probe")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	waiter := newFaultTraceWaiter()
	defer waiter.close()
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
	snapshot := strings.Repeat("e", 32)
	relative := filepath.Join(fixture.root, "objects/source/captures", snapshot)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/factorybox",
		"--root", fixture.root, "--runtime", "/out/sysbox-runc",
		"--runtime-sha256", os.Getenv("FACTORYBOX_RUNTIME_SHA256"),
		"--criu", "/out/criu", "--criu-sha256", os.Getenv("FACTORYBOX_CRIU_SHA256"),
		"--manager-root", "/var/lib/sysbox/manager",
		"checkpoint", "--snapshot-id", snapshot, "source")
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
	for tracer == 0 && time.Now().Before(deadline) {
		if readJSON("capture-worker-intent.json", &worker) == nil &&
			worker.SourcePID == source.Runtime.PID && worker.Token == snapshot {
			birth, birthErr := faultProcessBirth(worker.PID)
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
	if err := unix.PtraceSeize(tracer); err != nil {
		fixture.t.Fatal(err)
	}
	tracing := true
	defer func() {
		if tracing {
			unix.PidfdSendSignal(tracerFD, unix.SIGKILL, nil, 0)
			waiter.wait(tracer, time.Now().Add(2*time.Second))
		}
	}()
	if err := unix.PtraceInterrupt(tracer); err != nil {
		fixture.t.Fatal(err)
	}
	deadline = time.Now().Add(30 * time.Second)
	status, err := waiter.wait(tracer, deadline)
	if err != nil || !status.Stopped() {
		fixture.t.Fatal("owned CRIU did not stop for injection", err, status)
	}
	if err := unix.PtraceSetOptions(tracer, unix.PTRACE_O_TRACESYSGOOD); err != nil {
		fixture.t.Fatal(err)
	}
	var arm faultArm
	var pageBytes int64
	for {
		if readJSON("criu-retained-arm.json", &arm) == nil &&
			arm.Version == 3 && arm.TracerPID == tracer && arm.WorkerPID == worker.PID &&
			arm.ControllerPID == command.Process.Pid && arm.SourcePID == source.Runtime.PID &&
			arm.ActivePID == source.Runtime.PID && arm.ActiveVirtualPID == 1 {
			pageBytes = faultPageBytes(filepath.Join(relative, "images"), arm.ActiveVirtualPID)
			if pageBytes >= 4096 {
				break
			}
		}
		signal := int(status.StopSignal())
		if signal == int(unix.SIGTRAP) || signal == int(unix.SIGTRAP)|0x80 {
			signal = 0
		}
		if err := unix.PtraceSyscall(tracer, signal); err != nil {
			fixture.t.Fatal(err)
		}
		status, err = waiter.wait(tracer, deadline)
		if err != nil || !status.Stopped() {
			fixture.t.Fatal("CRIU exited before the real Docker root page-dump boundary", err, status)
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
	if err := unix.PidfdSendSignal(tracerFD, unix.SIGKILL, nil, 0); err != nil {
		fixture.t.Fatal(err)
	}
	status, err = waiter.wait(tracer, time.Now().Add(2*time.Second))
	if err != nil || (!status.Signaled() && !status.Exited()) {
		fixture.t.Fatal("exact CRIU trace was not reaped", err, status)
	}
	tracing = false
	err = command.Wait()
	waited = true
	if err == nil {
		fixture.t.Fatal("killed CRIU published a successful developer checkpoint")
	}
	var cure struct {
		Passed     bool   `json:"passed"`
		Authorized bool   `json:"guard_authorized"`
		Nonce      string `json:"nonce"`
		Processes  int    `json:"processes"`
		Threads    int    `json:"threads"`
		Registers  bool   `json:"registers_match"`
		SignalMask bool   `json:"signal_mask_match"`
		Maps       int    `json:"injected_maps_after"`
		Held       bool   `json:"source_held"`
	}
	if err := readJSON("criu-retained-result.json", &cure); err != nil ||
		!cure.Passed || !cure.Authorized || cure.Nonce != arm.Nonce ||
		cure.Processes != len(processes) || cure.Threads != len(arm.Tree) ||
		!cure.Registers || !cure.SignalMask || cure.Maps != 0 || !cure.Held {
		fixture.t.Fatal("real Docker source has no complete authorized cure", err, cure)
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
	record, err := json.MarshalIndent(map[string]any{
		"passed": true, "fault": "root-process-page-dump", "active_page_bytes": pageBytes,
		"source_pid": source.Runtime.PID, "tracer_pid": tracer, "processes": len(processes),
		"threads": len(arm.Tree), "retained_processes": retained, "cure": cure,
		"unchanged_app": after, "unchanged_containers": before, "docker_exec_exit": *finished.ExitCode,
		"scope": "Real Docker/Compose/Dev Container process tree, root page-dump tracer death, " +
			"guard-authorized recovery, original stable process identities/app state and new Docker exec. " +
			"No Docker-child dump, helper/guard loss or external agent inference qualification.",
	}, "", "  ")
	if err != nil || os.WriteFile("/output/devx-criu-interruption.json", append(record, '\n'), 0600) != nil {
		fixture.t.Fatal("save complete developer fault result", err)
	}
	fixture.t.Logf("real Docker root page-dump recovery preserved %d processes/%d threads; new Docker exec passed",
		len(processes), len(arm.Tree))
}

func TestNativeDockerCaptureInterruption(t *testing.T) {
	if os.Getenv("FACTORYBOX_DEVX_CRIU_FAULT") != "root" {
		t.Skip("requires the owned real-Docker root page-dump fault fixture")
	}
	fixture, source, initial := prepareDeveloperFixture(t)
	prior := strings.Repeat("a", 32)
	fixture.command("checkpoint", "--snapshot-id", prior, "source")
	fixture.interruptDockerCapture(source, initial)
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
