//go:build linux

package devx_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"time"

	"factorybox/internal/lifecycle"
	"golang.org/x/sys/unix"
)

type idePeer struct {
	Version  int    `json:"version"`
	RunOwner string `json:"run_owner"`
	PID      int    `json:"pid"`
	Start    string `json:"start"`
	ClientID string `json:"client_id"`
	UID      uint32 `json:"uid"`
	GID      uint32 `json:"gid"`
}

type ideRPC struct {
	OK       bool     `json:"ok"`
	ClientID string   `json:"client_id"`
	PID      int      `json:"pid"`
	Markers  []string `json:"markers"`
	Values   map[string]struct {
		Marker string `json:"marker"`
		Probe  string `json:"probe"`
	} `json:"values"`
}

type ideProjection struct {
	target, identity, empty, probe string
}

type nativeIDE struct {
	fixture   *fixture
	peer      idePeer
	namespace *os.File
	root      *os.File
	pidfd     *os.File
	mounts    map[string]ideProjection
	samples   []map[string]any
	closed    bool
}

func ideProcessStart(pid int) (string, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", err
	}
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 {
		return "", fmt.Errorf("IDE process lacks its command boundary")
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) < 20 || fields[0] == "Z" || fields[0] == "X" {
		return "", fmt.Errorf("original IDE process is absent or dead")
	}
	return fields[19], nil
}

func (f *fixture) attachIDE(source lifecycle.View) {
	f.t.Helper()
	if os.Getenv("FACTORYBOX_DEVX_THEIA") != "1" {
		return
	}
	processRoot := fmt.Sprintf("/proc/%d/root", source.Runtime.PID)
	deadline := time.Now().Add(15 * time.Second)
	for {
		_, err := os.Stat(processRoot + "/run/factorybox-workspace-ready")
		if err == nil {
			break
		}
		if !os.IsNotExist(err) || time.Now().After(deadline) {
			f.t.Fatal("owned workspace preparation was not completed", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	root := processRoot + "/workspace/project"
	info, err := os.Stat(root)
	if err != nil {
		f.t.Fatal(err)
	}
	stat := info.Sys().(*syscall.Stat_t)
	request, err := json.Marshal(map[string]any{
		"version": 1, "run_owner": os.Getenv("FACTORYBOX_DEVX_RUN"),
		"uid": stat.Uid, "gid": stat.Gid,
	})
	if err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile("/output/theia-request.json.partial", request, 0600); err != nil {
		f.t.Fatal(err)
	}
	outputInfo, err := os.Stat("/output")
	if err != nil {
		f.t.Fatal(err)
	}
	outputOwner := outputInfo.Sys().(*syscall.Stat_t)
	if err := os.Chown("/output/theia-request.json.partial", int(outputOwner.Uid), int(outputOwner.Gid)); err != nil {
		f.t.Fatal(err)
	}
	if err := os.Rename("/output/theia-request.json.partial", "/output/theia-request.json"); err != nil {
		f.t.Fatal(err)
	}
	ide := &nativeIDE{fixture: f, mounts: map[string]ideProjection{}}
	deadline = time.Now().Add(60 * time.Second)
	for {
		data, err := os.ReadFile("/output/theia-peer.json")
		if err == nil {
			if err := json.Unmarshal(data, &ide.peer); err != nil {
				f.t.Fatal(err)
			}
			break
		}
		if !os.IsNotExist(err) || time.Now().After(deadline) {
			f.t.Fatal("separate Theia peer was not published", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if ide.peer.Version != 1 || ide.peer.RunOwner != os.Getenv("FACTORYBOX_DEVX_RUN") ||
		ide.peer.RunOwner == "" || ide.peer.UID != stat.Uid || ide.peer.GID != stat.Gid ||
		ide.peer.PID <= 1 || len(ide.peer.ClientID) != 36 {
		f.t.Fatal("IDE peer differs from its original worker and project owner")
	}
	fd, err := unix.PidfdOpen(ide.peer.PID, 0)
	if err != nil {
		f.t.Fatal(err)
	}
	ide.pidfd = os.NewFile(uintptr(fd), "original native IDE client")
	ide.namespace, err = os.Open(fmt.Sprintf("/proc/%d/ns/mnt", ide.peer.PID))
	if err != nil {
		ide.pidfd.Close()
		f.t.Fatal(err)
	}
	ide.root, err = os.Open(fmt.Sprintf("/proc/%d/root", ide.peer.PID))
	if err != nil {
		ide.namespace.Close()
		ide.pidfd.Close()
		f.t.Fatal(err)
	}
	f.ide = ide
	f.t.Cleanup(func() {
		if err := ide.close(); err != nil {
			f.t.Error("retire original IDE workspace projections and namespace handles", err)
		}
	})
	if err := ide.validate(); err != nil {
		f.t.Fatal(err)
	}
	if rejected := ide.rpc("/ready", map[string]any{
		"branches": []string{"unknown"}, "phase": "rejected", "expected_marker": "source",
	}, http.StatusServiceUnavailable); rejected.OK {
		f.t.Fatal("unknown IDE fixture was accepted")
	}
}

func (ide *nativeIDE) close() error {
	if ide.closed {
		return nil
	}
	for id := range ide.mounts {
		if err := ide.release(id); err != nil {
			return err
		}
	}
	ide.closed = true
	return errors.Join(ide.namespace.Close(), ide.root.Close(), ide.pidfd.Close())
}

func (ide *nativeIDE) finish() {
	ide.fixture.t.Helper()
	if err := ide.close(); err != nil {
		ide.fixture.t.Fatal("release IDE before final sandbox teardown", err)
	}
	if err := os.WriteFile("/output/theia-operations-complete", []byte("done\n"), 0600); err != nil {
		ide.fixture.t.Fatal(err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		data, err := os.ReadFile("/output/theia-released")
		if err == nil {
			if strings.TrimSpace(string(data)) != "ok" {
				ide.fixture.t.Fatal("separate IDE peer failed before final sandbox teardown")
			}
			return
		}
		if !os.IsNotExist(err) || time.Now().After(deadline) {
			ide.fixture.t.Fatal("separate IDE peer was not released", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (ide *nativeIDE) validate() error {
	if ide.closed {
		return fmt.Errorf("original IDE consumer has already been released")
	}
	start, err := ideProcessStart(ide.peer.PID)
	if err != nil || start != ide.peer.Start {
		return fmt.Errorf("original IDE client process changed: %v", err)
	}
	poll := []unix.PollFd{{Fd: int32(ide.pidfd.Fd()), Events: unix.POLLIN}}
	if n, err := unix.Poll(poll, 0); err != nil || n != 0 {
		return fmt.Errorf("original IDE client pidfd is exited or invalid: %v", err)
	}
	current, err := os.Stat(fmt.Sprintf("/proc/%d/ns/mnt", ide.peer.PID))
	original, originalErr := ide.namespace.Stat()
	if err != nil || originalErr != nil || !os.SameFile(current, original) {
		return fmt.Errorf("original IDE mount namespace changed")
	}
	current, err = os.Stat(fmt.Sprintf("/proc/%d/root", ide.peer.PID))
	original, originalErr = ide.root.Stat()
	if err != nil || originalErr != nil || !os.SameFile(current, original) {
		return fmt.Errorf("original IDE filesystem root changed")
	}
	return nil
}

func (ide *nativeIDE) command(args ...string) (string, error) {
	helper, err := os.Open("/ide-filesystem")
	if err != nil {
		return "", err
	}
	defer helper.Close()
	extra := []*os.File{ide.namespace, ide.root, helper}
	if len(args) == 4 && args[0] == "mount" && args[1] == "--bind" {
		tree, err := cloneIDEWorkspace(args[2], helper)
		if err != nil {
			return "", fmt.Errorf("clone original workspace mount: %w", err)
		}
		defer tree.Close()
		extra = append(extra, tree)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "nsenter", append([]string{
		"--mount=/proc/self/fd/3", "--root=/proc/self/fd/4", "--wd=/proc/self/fd/4", "--",
		"/proc/self/fd/5",
	}, args...)...)
	command.Env = append(os.Environ(), "FACTORYBOX_IDE_FILESYSTEM=1")
	command.ExtraFiles = extra
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("owned IDE namespace command %v: %w: %s", args, err, output)
	}
	return strings.TrimSpace(string(output)), nil
}

func cloneIDEWorkspace(source string, helper *os.File) (*os.File, error) {
	rest, ok := strings.CutPrefix(source, "/proc/")
	pid, relative, found := strings.Cut(rest, "/root")
	number, err := strconv.Atoi(pid)
	if !ok || !found || err != nil || number <= 1 || !strings.HasPrefix(relative, "/") {
		return nil, fmt.Errorf("project source is outside its original process root")
	}
	var anchors []*os.File
	defer func() {
		for _, file := range anchors {
			file.Close()
		}
	}()
	for _, path := range []string{"/proc/" + pid + "/ns/mnt", "/proc/" + pid + "/root", "/proc/" + pid + "/ns/pid"} {
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		anchors = append(anchors, file)
	}
	pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	receiver := os.NewFile(uintptr(pair[0]), "owned original mount receiver")
	sender := os.NewFile(uintptr(pair[1]), "owned original mount sender")
	defer receiver.Close()
	defer sender.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "nsenter",
		"--mount=/proc/self/fd/3", "--root=/proc/self/fd/4", "--wd=/proc/self/fd/4",
		"--pid=/proc/self/fd/7", "--", "/proc/self/fd/5", "clone", relative)
	command.Env = append(os.Environ(), "FACTORYBOX_IDE_FILESYSTEM=1")
	command.ExtraFiles = []*os.File{anchors[0], anchors[1], helper, sender, anchors[2]}
	if output, err := command.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("original project namespace clone: %w: %s", err, output)
	}
	poll := []unix.PollFd{{Fd: int32(receiver.Fd()), Events: unix.POLLIN}}
	if n, err := unix.Poll(poll, 1000); err != nil || n != 1 {
		return nil, fmt.Errorf("original mount descriptor did not arrive: %v", err)
	}
	var payload [2]byte
	control := make([]byte, unix.CmsgSpace(4))
	n, oob, flags, _, err := unix.Recvmsg(int(receiver.Fd()), payload[:], control, unix.MSG_CMSG_CLOEXEC)
	var descriptors []int
	messages, parseErr := unix.ParseSocketControlMessage(control[:oob])
	if parseErr == nil {
		for _, message := range messages {
			files, rightsErr := unix.ParseUnixRights(&message)
			if rightsErr != nil {
				parseErr = rightsErr
				break
			}
			descriptors = append(descriptors, files...)
		}
	}
	if err != nil || parseErr != nil || n != 1 || payload[0] != 'M' ||
		flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 || len(descriptors) != 1 {
		for _, fd := range descriptors {
			unix.Close(fd)
		}
		return nil, fmt.Errorf("original mount handoff has an invalid descriptor record: %v %v", err, parseErr)
	}
	return os.NewFile(uintptr(descriptors[0]), "original detached project mount"), nil
}

func (ide *nativeIDE) project(view lifecycle.View) (string, string) {
	ide.fixture.t.Helper()
	if err := ide.validate(); err != nil {
		ide.fixture.t.Fatal(err)
	}
	id := "branch-" + view.Record.Owner
	if len(view.Record.Owner) != 32 || view.Runtime == nil || view.Runtime.ID != "fbx-"+view.Record.Owner {
		ide.fixture.t.Fatal("IDE selection lacks a complete original runtime identity")
	}
	if _, err := strconv.ParseUint(view.Record.Owner[:16], 16, 64); err != nil {
		ide.fixture.t.Fatal(err)
	}
	if _, err := strconv.ParseUint(view.Record.Owner[16:], 16, 64); err != nil {
		ide.fixture.t.Fatal(err)
	}
	target := "/experiment/branches/" + id + "/upper/workspace/fixture"
	if projection, ok := ide.mounts[view.Record.ID]; ok {
		if projection.target != target {
			ide.fixture.t.Fatal("IDE workspace identity changed")
		}
		return id, target
	}
	pidfd, err := unix.PidfdOpen(view.Runtime.PID, 0)
	if err != nil {
		ide.fixture.t.Fatal(err)
	}
	defer unix.Close(pidfd)
	cgroups, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", view.Runtime.PID))
	membership := false
	for _, line := range strings.Split(string(cgroups), "\n") {
		group := strings.TrimPrefix(line, "0::")
		membership = membership || group == "/"+view.Runtime.ID || strings.HasPrefix(group, "/"+view.Runtime.ID+"/")
	}
	if err != nil || !membership {
		ide.fixture.t.Fatal("IDE workspace process left its original runtime cgroup", err)
	}
	source := fmt.Sprintf("/proc/%d/root/workspace/project", view.Runtime.PID)
	info, err := os.Stat(source)
	if err != nil {
		ide.fixture.t.Fatal(err)
	}
	stat := info.Sys().(*syscall.Stat_t)
	if stat.Uid != ide.peer.UID || stat.Gid != ide.peer.GID {
		ide.fixture.t.Fatal("restored project ownership differs from the original IDE user")
	}
	if _, err := ide.command("mkdir", "-p", "--", target); err != nil {
		ide.fixture.t.Fatal(err)
	}
	empty, err := ide.command("stat", "-c", "%d:%i", "--", target)
	if err != nil {
		ide.fixture.t.Fatal(err)
	}
	identity := fmt.Sprintf("%d:%d", stat.Dev, stat.Ino)
	ide.mounts[view.Record.ID] = ideProjection{target: target, identity: identity, empty: empty}
	if _, err := ide.command("mount", "--bind", source, target); err != nil {
		ide.fixture.t.Fatal(err)
	}
	current, err := ide.command("stat", "-c", "%d:%i", "--", target)
	if err != nil || current != identity {
		ide.fixture.t.Fatal("IDE binding does not identify the selected live project", err, current, identity)
	}
	return id, target
}

func (ide *nativeIDE) release(id string) error {
	projection, ok := ide.mounts[id]
	if !ok {
		return nil
	}
	current, err := ide.command("stat", "-c", "%d:%i", "--", projection.target)
	if err == nil && current == projection.empty {
		delete(ide.mounts, id)
		return nil
	}
	if err != nil || current != projection.identity {
		return fmt.Errorf("IDE workspace binding changed before exact removal: %v", err)
	}
	if _, err := ide.command("umount", "--", projection.target); err != nil {
		return err
	}
	delete(ide.mounts, id)
	return nil
}

func (ide *nativeIDE) rpc(operation string, input any, status int) ideRPC {
	ide.fixture.t.Helper()
	if err := ide.validate(); err != nil {
		ide.fixture.t.Fatal(err)
	}
	payload, err := json.Marshal(input)
	if err != nil {
		ide.fixture.t.Fatal(err)
	}
	client := http.Client{Timeout: 15 * time.Second}
	response, err := client.Post("http://127.0.0.1:4401"+operation, "application/json", bytes.NewReader(payload))
	if err != nil {
		ide.fixture.t.Fatal("native Theia RPC request", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, (16<<10)+1))
	var result ideRPC
	if err != nil || len(data) > 16<<10 || response.StatusCode != status || json.Unmarshal(data, &result) != nil {
		ide.fixture.t.Fatalf("native Theia RPC response: HTTP %d: %v: %s", response.StatusCode, err, data)
	}
	if result.PID != ide.peer.PID || result.ClientID != ide.peer.ClientID {
		ide.fixture.t.Fatal("native Theia connection restarted or reconnected")
	}
	return result
}

func (ide *nativeIDE) record(phase string, view lifecycle.View, start time.Time, result ideRPC) {
	ide.samples = append(ide.samples, map[string]any{
		"phase": phase, "sandbox": view.Record.ID, "runtime_id": view.Record.RuntimeID,
		"client_id": result.ClientID, "pid": result.PID,
		"milliseconds": float64(time.Since(start)) / float64(time.Millisecond),
	})
	data, err := json.MarshalIndent(ide.samples, "", "  ")
	if err != nil {
		ide.fixture.t.Fatal(err)
	}
	if err := os.WriteFile("/output/theia-observations.json", append(data, '\n'), 0600); err != nil {
		ide.fixture.t.Fatal(err)
	}
}

func (ide *nativeIDE) ready(phase string, view lifecycle.View, start time.Time, marker string) {
	ide.fixture.t.Helper()
	id, target := ide.project(view)
	root := fmt.Sprintf("/proc/%d/root/workspace/project", view.Runtime.PID)
	raw, err := os.ReadFile(root + "/marker.txt")
	if err != nil || strings.TrimSpace(string(raw)) != marker {
		ide.fixture.t.Fatal("selected IDE project differs from the real Connector app", err)
	}
	result := ide.rpc("/ready", map[string]any{
		"branches": []string{id}, "phase": phase, "expected_marker": string(raw),
	}, http.StatusOK)
	expected := phase + ":" + id
	if !result.OK || len(result.Values) != 1 || result.Values[id].Probe != expected ||
		result.Values[id].Marker != string(raw) {
		ide.fixture.t.Fatal("native Theia read/edit differs from the selected live runtime")
	}
	probe, err := os.ReadFile(root + "/theia-readiness.txt")
	if err != nil || string(probe) != expected {
		ide.fixture.t.Fatal("native Theia write did not reach the live sandbox project", err)
	}
	for other, projection := range ide.mounts {
		if other == view.Record.ID || projection.probe == "" {
			continue
		}
		current, err := ide.command("cat", projection.target+"/theia-readiness.txt")
		if err != nil || current != projection.probe {
			ide.fixture.t.Fatal("native Theia edit changed another sandbox's project", err)
		}
	}
	projection := ide.mounts[view.Record.ID]
	projection.target, projection.probe = target, expected
	ide.mounts[view.Record.ID] = projection
	ide.record(phase, view, start, result)
}

func (ide *nativeIDE) diverge(first, second lifecycle.View) {
	ide.fixture.t.Helper()
	start := time.Now()
	views := []lifecycle.View{first, second}
	ids := []string{}
	before := []observedState{}
	for index, view := range views {
		state, err := readApp(view.Runtime.PID, "")
		if err != nil {
			ide.fixture.t.Fatal(err)
		}
		id, target := ide.project(view)
		marker := fmt.Sprintf("devcontainer-branch-%d", index)
		// Prepare the sealed client's control value through the owned view.
		// The example's POST contract requires a counter increment and a
		// different marker alphabet; no application assertion is relaxed.
		if _, err := ide.command("write", target+"/marker.txt", marker); err != nil {
			ide.fixture.t.Fatal(err)
		}
		expected := state
		expected.App.Marker = marker
		state, err = readApp(view.Runtime.PID, "")
		if err != nil || !reflect.DeepEqual(state, expected) {
			ide.fixture.t.Fatal("controlled workspace preparation changed app RAM or missed its Connector", err)
		}
		before = append(before, state)
		ids = append(ids, id)
	}
	result := ide.rpc("/diverge", map[string]any{"branches": ids}, http.StatusOK)
	if !result.OK || len(result.Markers) != 2 || result.Markers[0] == result.Markers[1] {
		ide.fixture.t.Fatal("Theia selected-sandbox edits did not diverge")
	}
	for index, view := range views {
		state, err := readApp(view.Runtime.PID, "")
		expected := before[index]
		expected.App.Marker = result.Markers[index]
		if err != nil || !reflect.DeepEqual(state, expected) {
			ide.fixture.t.Fatal("Theia edit did not reach only the selected Dev Container through its real Connector", err)
		}
	}
	ide.record("selected-sandbox-edit-readback", first, start, result)
	ide.fixture.t.Log("one external native Theia connection retained across checkpoint/forks/pause/resume; independent edits reached both real Dev Containers through their Connectors")
}
