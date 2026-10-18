//go:build linux

package lifecycle

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

type captureGuardConfig struct {
	Version    int    `json:"version"`
	Source     Record `json:"source"`
	PID        int    `json:"pid"`
	Start      string `json:"start"`
	Token      string `json:"token"`
	Evidence   string `json:"evidence"`
	Controller int    `json:"controller"`
	Root       string `json:"root"`
	RecordSHA  string `json:"record_sha256"`
}

type captureGuardAnchors struct {
	group, pid, evidence, root *os.File
}

type captureGuard struct {
	config captureGuardConfig
	anchor captureGuardAnchors
	socket *os.File
	cmd    *exec.Cmd
	done   chan error
	exit   error
	exited bool
	unsafe bool
}

// CaptureGuardCommand is an internal re-exec entry point. The CLI and native
// test executable dispatch it explicitly before parsing ordinary arguments.
// The helper receives only its exclusive socket and original kernel handles.
func CaptureGuardCommand(args []string) (bool, error) {
	if len(args) == 1 && args[0] == "_capture-guard" {
		return true, captureGuardServe()
	}
	return false, nil
}

func captureGuardOpen(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

func (anchor captureGuardAnchors) close() error {
	var result error
	for _, file := range []*os.File{anchor.group, anchor.pid, anchor.evidence, anchor.root} {
		if file != nil {
			result = errors.Join(result, file.Close())
		}
	}
	return result
}

func captureGuardValidate(config captureGuardConfig, anchor captureGuardAnchors) error {
	object, err := objectPath(config.Source.ID)
	if err != nil || config.Version != 1 || config.Source.Version != 1 ||
		config.PID <= 1 || config.Controller <= 1 || config.PID == config.Controller ||
		!tokenID.MatchString(config.Token) || !tokenID.MatchString(config.Source.Owner) ||
		config.Source.RuntimeID != "fbx-"+config.Source.Owner || config.Source.Cgroup == nil ||
		!digest.MatchString(config.RecordSHA) ||
		config.Evidence != filepath.Join(config.Root, object, "captures", config.Token, "guard") {
		return fmt.Errorf("guard intent is outside its persisted sandbox operation")
	}
	if anchor.group == nil || anchor.pid == nil || anchor.evidence == nil || anchor.root == nil {
		return fmt.Errorf("guard lacks original kernel anchors")
	}
	if err := canonical(config.Root); err != nil {
		return err
	}
	if err := privateDirectory(config.Root); err != nil {
		return err
	}
	groupPath := cgroupRoot + "/" + config.Source.RuntimeID
	for path, file := range map[string]*os.File{
		groupPath: anchor.group, config.Evidence: anchor.evidence, config.Root: anchor.root,
	} {
		current, err := os.Lstat(path)
		saved, savedErr := file.Stat()
		if err != nil || savedErr != nil || !current.IsDir() || !os.SameFile(current, saved) {
			return fmt.Errorf("guard directory identity changed: %s: %v %v", path, err, savedErr)
		}
	}
	identity, err := cgroupFDIdentity(int(anchor.group.Fd()))
	if err != nil || identity != *config.Source.Cgroup {
		return fmt.Errorf("guard cgroup kernel identity changed: %v", err)
	}
	current, err := cgroupIdentity(groupPath)
	if err != nil || current != identity {
		return fmt.Errorf("guard cgroup path changed its mount identity: %v", err)
	}
	parent, err := cgroupIdentity(cgroupRoot)
	if err != nil || parent != config.Source.CgroupParent {
		return fmt.Errorf("guard worker cgroup root changed: %v", err)
	}
	fd, err := unix.Openat2(int(anchor.root.Fd()), object+"/record.json", &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS,
	})
	if err != nil {
		return err
	}
	recordFile := os.NewFile(uintptr(fd), "guard original sandbox record")
	data, readErr := io.ReadAll(io.LimitReader(recordFile, (1<<20)+1))
	if err := errors.Join(readErr, recordFile.Close()); err != nil {
		return err
	}
	expected, err := json.Marshal(config.Source)
	var saved Record
	if err != nil || len(data) > 1<<20 || sum(data) != config.RecordSHA ||
		json.Unmarshal(data, &saved) != nil {
		return fmt.Errorf("guard source record changed")
	}
	actual, err := json.Marshal(saved)
	if err != nil || !bytes.Equal(expected, actual) {
		return fmt.Errorf("guard source differs from its recorded request")
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil || strings.TrimSpace(string(boot)) != config.Source.BootID {
		return fmt.Errorf("guard source belongs to a different worker boot: %v", err)
	}
	poll := []unix.PollFd{{Fd: int32(anchor.pid.Fd()), Events: unix.POLLIN}}
	if n, err := unix.Poll(poll, 0); err != nil || n != 0 {
		return fmt.Errorf("guard source pidfd is exited or invalid: %v", err)
	}
	descriptor, err := os.ReadFile(fmt.Sprintf("/proc/self/fdinfo/%d", anchor.pid.Fd()))
	if err != nil {
		return err
	}
	pidMatches := false
	for _, line := range strings.Split(string(descriptor), "\n") {
		fields := strings.Fields(line)
		pidMatches = pidMatches || (len(fields) == 2 && fields[0] == "Pid:" && fields[1] == fmt.Sprint(config.PID))
	}
	if !pidMatches {
		return fmt.Errorf("guard pidfd does not identify the saved source PID")
	}
	stamp, err := processBirth(config.PID)
	if err != nil || stamp != config.Start {
		return fmt.Errorf("guard source process identity changed: %v", err)
	}
	membership, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", config.PID))
	root := "/" + config.Source.RuntimeID
	found := false
	for _, line := range strings.Split(string(membership), "\n") {
		if strings.HasPrefix(line, "0::") {
			group := strings.TrimPrefix(line, "0::")
			found = group == root || strings.HasPrefix(group, root+"/")
		}
	}
	if err != nil || !found {
		return fmt.Errorf("guard source left its original cgroup: %v", err)
	}
	return nil
}

func captureGuardWrite(anchor *os.File, name string, value any) error {
	if filepath.Base(name) != name {
		return fmt.Errorf("guard record name is not local")
	}
	fd, err := unix.Openat(int(anchor.Fd()), name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), name)
	data, err := json.Marshal(value)
	if err == nil {
		_, err = file.Write(append(data, '\n'))
	}
	return errors.Join(err, file.Sync(), file.Close(), anchor.Sync())
}

func captureGuardEvents(anchor *os.File) (map[string]uint64, error) {
	fd, err := unix.Openat(int(anchor.Fd()), "cgroup.events", unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "cgroup.events")
	data, readErr := io.ReadAll(io.LimitReader(file, 4097))
	closeErr := file.Close()
	if len(data) > 4096 {
		return nil, fmt.Errorf("guard cgroup event record exceeds bound")
	}
	if err := errors.Join(readErr, closeErr); err != nil {
		return nil, err
	}
	return parseCgroupCounters(string(data))
}

func captureGuardRecover(config captureGuardConfig, anchor captureGuardAnchors) (string, error) {
	if err := captureGuardValidate(config, anchor); err != nil {
		return "", err
	}
	events, err := captureGuardEvents(anchor.group)
	frozen, hasFrozen := events["frozen"]
	populated, hasPopulated := events["populated"]
	if err != nil || !hasFrozen || !hasPopulated || populated != 1 || frozen > 1 {
		return "", fmt.Errorf("guard source is not populated: %v", err)
	}
	if frozen == 0 {
		return "source_already_running", nil
	}
	fd, err := unix.Openat(int(anchor.group.Fd()), "cgroup.freeze", unix.O_WRONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return "", err
	}
	file := os.NewFile(uintptr(fd), "original source cgroup.freeze")
	_, writeErr := file.WriteString("0")
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return "", err
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		if err := captureGuardValidate(config, anchor); err != nil {
			return "", err
		}
		events, err := captureGuardEvents(anchor.group)
		if err == nil && events["populated"] == 1 && events["frozen"] == 0 {
			return "original_source_thawed", nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("guard source thaw did not complete: %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func captureGuardHold(config captureGuardConfig, anchor captureGuardAnchors) (string, error) {
	if err := captureGuardValidate(config, anchor); err != nil {
		return "", err
	}
	events, err := captureGuardEvents(anchor.group)
	frozen, hasFrozen := events["frozen"]
	populated, hasPopulated := events["populated"]
	if err != nil || !hasFrozen || !hasPopulated || populated != 1 || frozen > 1 {
		return "", fmt.Errorf("guard cannot hold an invalid original source: %v", err)
	}
	if frozen != 1 {
		fd, err := unix.Openat(int(anchor.group.Fd()), "cgroup.freeze",
			unix.O_WRONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			return "", err
		}
		file := os.NewFile(uintptr(fd), "original unsafe source cgroup.freeze")
		_, writeErr := file.WriteString("1")
		if err := errors.Join(writeErr, file.Close()); err != nil {
			return "", err
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		if err := captureGuardValidate(config, anchor); err != nil {
			return "", err
		}
		events, err := captureGuardEvents(anchor.group)
		if err == nil && events["populated"] == 1 && events["frozen"] == 1 {
			return "original_source_held_for_capture_reconciliation", nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("original unsafe source freeze was not confirmed: %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func captureGuardRead(fd int, timeoutMS int) ([]byte, error) {
	if timeoutMS >= 0 {
		deadline := time.Now().Add(time.Duration(timeoutMS) * time.Millisecond)
		for {
			remaining := time.Until(deadline).Milliseconds()
			if remaining < 0 {
				return nil, fmt.Errorf("guard handshake timed out")
			}
			poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
			n, err := unix.Poll(poll, int(remaining)+1)
			if errors.Is(err, unix.EINTR) {
				continue
			}
			if err != nil || n == 0 {
				return nil, fmt.Errorf("guard handshake did not arrive: %v", err)
			}
			break
		}
	}
	data := make([]byte, 16<<10)
	for {
		n, _, flags, _, err := unix.Recvmsg(fd, data, nil, 0)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil || flags&unix.MSG_TRUNC != 0 {
			return nil, fmt.Errorf("invalid guard packet: %v", err)
		}
		return data[:n], nil
	}
}

func captureGuardCaptureReturned(config captureGuardConfig, anchor captureGuardAnchors) error {
	if err := captureGuardValidate(config, anchor); err != nil {
		return err
	}
	events, err := captureGuardEvents(anchor.group)
	if err != nil || events["frozen"] != 1 || events["populated"] != 1 {
		return fmt.Errorf("capture completion requires the original frozen source: %v", err)
	}
	threads, err := captureGuardThreads(config, anchor)
	if err != nil || threads[config.PID] != config.Start {
		return fmt.Errorf("capture completion lost the original source: %v", err)
	}
	for tid := range threads {
		status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", tid))
		if err != nil {
			return err
		}
		untraced := false
		for _, line := range strings.Split(string(status), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[0] == "TracerPid:" {
				untraced = fields[1] == "0"
			}
		}
		if !untraced {
			return fmt.Errorf("capture completion still has a live tracer")
		}
	}
	return nil
}

func captureGuardServe() (resultErr error) {
	socket := os.NewFile(3, "exclusive controller socket")
	anchor := captureGuardAnchors{os.NewFile(4, "original source cgroup"),
		os.NewFile(5, "original source pidfd"), os.NewFile(6, "owned evidence directory"),
		os.NewFile(7, "original controller root")}
	defer socket.Close()
	defer anchor.close()
	data, err := captureGuardRead(3, 2000)
	if err != nil || len(data) == 0 {
		return fmt.Errorf("guard initial intent is absent: %v", err)
	}
	var config captureGuardConfig
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return err
	}
	if err := captureGuardValidate(config, anchor); err != nil {
		return err
	}
	selfGroup, err := os.ReadFile("/proc/self/cgroup")
	if err != nil || strings.Contains(string(selfGroup), "/"+config.Source.RuntimeID) {
		return fmt.Errorf("guard is inside the captured source cgroup")
	}
	if _, err := socket.Write([]byte("armed:" + config.Token)); err != nil {
		return err
	}
	unsafe := false
	var request []byte
	for {
		request, err = captureGuardRead(3, -1)
		if err != nil {
			return err
		}
		name, acknowledgement := "", ""
		switch string(request) {
		case "capture-enter:" + config.Token:
			if unsafe {
				return fmt.Errorf("guard capture is already in flight")
			}
			unsafe, name, acknowledgement = true, "guard-capture-entered.json", "capture-held:"
		case "capture-returned:" + config.Token:
			if !unsafe {
				return fmt.Errorf("guard capture completion has no matching entry")
			}
			if err := captureGuardCaptureReturned(config, anchor); err != nil {
				return errors.Join(err, captureGuardWrite(anchor.evidence, "guard-capture-rejected.json",
					map[string]any{"token": config.Token, "capture_in_flight": true, "error": err.Error()}))
			}
			unsafe, name, acknowledgement = false, "guard-capture-returned.json", "capture-safe:"
		}
		if name == "" {
			break
		}
		if err := captureGuardValidate(config, anchor); err != nil {
			return err
		}
		if err := captureGuardWrite(anchor.evidence, name, map[string]any{
			"token": config.Token, "capture_in_flight": unsafe,
		}); err != nil {
			return err
		}
		if _, err := socket.Write([]byte(acknowledgement + config.Token)); err != nil {
			return err
		}
	}
	reason := "controller_disconnected"
	status := ""
	switch string(request) {
	case "disarm:" + config.Token:
		if unsafe {
			return fmt.Errorf("cannot disarm while capture completion is unproved")
		}
		reason = "controller_disarmed"
		if err := captureGuardValidate(config, anchor); err != nil {
			return err
		}
		events, err := captureGuardEvents(anchor.group)
		if err != nil || events["populated"] != 1 || events["frozen"] != 0 {
			return fmt.Errorf("cannot disarm guard before source is running: %v", err)
		}
		status = "source_running_guard_disarmed"
	case "recover:" + config.Token:
		reason = "controller_requested_recovery"
	case "":
	default:
		return fmt.Errorf("guard request does not match its exact intent")
	}
	if status == "" && unsafe {
		// An orphaned ramctl/CRIU child may still be capturing. Preserve the
		// original frozen source until a new controller reconciles that
		// command and validates tracer/thread state. EOF is not a safe point.
		status, resultErr = captureGuardHold(config, anchor)
	} else if status == "" {
		status, resultErr = captureGuardRecover(config, anchor)
	}
	record := map[string]any{"config": config, "reason": reason, "status": status,
		"capture_in_flight": unsafe, "passed": resultErr == nil}
	if resultErr != nil {
		record["error"] = resultErr.Error()
	}
	return errors.Join(resultErr, captureGuardWrite(anchor.evidence, "guard-outcome.json", record))
}

func (e *Engine) startCaptureGuard(source Record, pid int) (_ *captureGuard, resultErr error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return e.startCaptureGuardWithExecutable(source, pid, executable)
}

func (e *Engine) startCaptureGuardWithExecutable(source Record, pid int, executable string) (_ *captureGuard, resultErr error) {
	return e.startCheckpointGuard(source, pid, executable, "", nil)
}

func (e *Engine) startCheckpointGuard(source Record, pid int, executable, captureID string, intent *checkpointIntent) (_ *captureGuard, resultErr error) {
	var token [16]byte
	if captureID == "" {
		if _, err := rand.Read(token[:]); err != nil {
			return nil, err
		}
		captureID = hex.EncodeToString(token[:])
	} else if !tokenID.MatchString(captureID) {
		return nil, fmt.Errorf("checkpoint identity must be 32 lowercase hexadecimal characters")
	}
	start, err := processBirth(pid)
	if err != nil {
		return nil, err
	}
	object, err := objectPath(source.ID)
	if err != nil {
		return nil, err
	}
	captures := object + "/captures"
	if err := e.root.Mkdir(captures, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	if err := privateDirectory(filepath.Join(e.config.Root, captures)); err != nil {
		return nil, err
	}
	generation := captures + "/" + captureID
	if err := e.root.Mkdir(generation, 0700); err != nil {
		return nil, err
	}
	if err := e.root.Mkdir(generation+"/guard", 0700); err != nil {
		return nil, err
	}
	sourceData, err := e.root.ReadFile(object + "/record.json")
	if err != nil {
		return nil, err
	}
	config := captureGuardConfig{Version: 1, Source: source, PID: pid, Start: start,
		Token: captureID, Evidence: filepath.Join(e.config.Root, generation, "guard"),
		Controller: os.Getpid(), Root: e.config.Root, RecordSHA: sum(sourceData)}
	if intent != nil {
		directory, err := e.root.Open(generation)
		if err != nil {
			return nil, err
		}
		writeErr := captureGuardWrite(directory, "checkpoint-intent.json", intent)
		closeErr := directory.Close()
		if err := errors.Join(writeErr, closeErr); err != nil {
			return nil, err
		}
		for _, path := range []string{captures, object} {
			parent, err := e.root.Open(path)
			if err != nil {
				return nil, err
			}
			if err := errors.Join(parent.Sync(), parent.Close()); err != nil {
				return nil, err
			}
		}
	}
	guard := &captureGuard{config: config}
	defer func() {
		if resultErr != nil {
			guard.anchor.close()
			if guard.socket != nil {
				guard.socket.Close()
			}
			if guard.cmd != nil && guard.cmd.Process != nil {
				guard.cmd.Process.Kill()
				guard.cmd.Wait()
			}
		}
	}()
	guard.anchor.group, err = captureGuardOpen(cgroupRoot + "/" + source.RuntimeID)
	if err != nil {
		return nil, err
	}
	guard.anchor.evidence, err = captureGuardOpen(config.Evidence)
	if err != nil {
		return nil, err
	}
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		return nil, err
	}
	guard.anchor.pid = os.NewFile(uintptr(fd), "original source pidfd")
	guard.anchor.root, err = e.root.Open(".")
	if err != nil {
		return nil, err
	}
	if err := captureGuardValidate(config, guard.anchor); err != nil {
		return nil, err
	}
	if err := captureGuardWrite(guard.anchor.evidence, "guard-intent.json", config); err != nil {
		return nil, err
	}
	sockets, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	guard.socket = os.NewFile(uintptr(sockets[0]), "controller guard socket")
	child := os.NewFile(uintptr(sockets[1]), "guard control socket")
	defer child.Close()
	guard.cmd = exec.Command(executable, "_capture-guard")
	guard.cmd.Env = []string{"GOMAXPROCS=1", "GOMEMLIMIT=32MiB", "LANG=C"}
	guard.cmd.ExtraFiles = []*os.File{child, guard.anchor.group, guard.anchor.pid,
		guard.anchor.evidence, guard.anchor.root}
	guard.cmd.Stdout, guard.cmd.Stderr = os.Stderr, os.Stderr
	if err := guard.cmd.Start(); err != nil {
		return nil, err
	}
	data, _ := json.Marshal(config)
	if _, err := guard.socket.Write(data); err != nil {
		return nil, err
	}
	ready, err := captureGuardRead(int(guard.socket.Fd()), 2000)
	if err != nil || string(ready) != "armed:"+config.Token {
		return nil, fmt.Errorf("guard failed to arm: %s: %v", ready, err)
	}
	if err := captureGuardWrite(guard.anchor.evidence, "guard-armed.json",
		map[string]any{"token": config.Token, "helper_pid": guard.cmd.Process.Pid}); err != nil {
		return nil, err
	}
	guard.done = make(chan error, 1)
	go func() { guard.done <- guard.cmd.Wait(); close(guard.done) }()
	return guard, nil
}

func (guard *captureGuard) check() error {
	if !guard.exited {
		select {
		case guard.exit = <-guard.done:
			guard.exited = true
		default:
			return nil
		}
	}
	return fmt.Errorf("source guard exited before disarm: %v", guard.exit)
}

func (guard *captureGuard) capturePhase(enter bool) error {
	if err := guard.check(); err != nil {
		return err
	}
	request, response := "capture-enter:", "capture-held:"
	if !enter {
		if !guard.unsafe {
			return fmt.Errorf("guard has no in-flight capture")
		}
		request, response = "capture-returned:", "capture-safe:"
	}
	// Retain the conservative state if the helper persisted entry but its
	// acknowledgement is lost. A completion is safe only after its ACK.
	if enter {
		guard.unsafe = true
	}
	if _, err := guard.socket.Write([]byte(request + guard.config.Token)); err != nil {
		return err
	}
	packet, err := captureGuardRead(int(guard.socket.Fd()), 2000)
	if err != nil || string(packet) != response+guard.config.Token {
		return fmt.Errorf("guard capture phase was not acknowledged: %v", err)
	}
	guard.unsafe = enter
	return nil
}

// One guarded transaction may contain several capture operations. Checking an
// intermediate return must not publish a safe point before the final writer.
func (guard *captureGuard) captureOperationReturned() error {
	if err := guard.check(); err != nil {
		return err
	}
	if !guard.unsafe {
		return fmt.Errorf("capture operation is outside its guarded transaction")
	}
	return captureGuardCaptureReturned(guard.config, guard.anchor)
}

func (guard *captureGuard) finish(recoverSource bool) (resultErr error) {
	defer func() { resultErr = errors.Join(resultErr, guard.anchor.close(), guard.socket.Close()) }()
	request := "disarm:"
	if recoverSource {
		request = "recover:"
	}
	if _, err := guard.socket.Write([]byte(request + guard.config.Token)); err != nil {
		resultErr = err
	}
	if !guard.exited {
		select {
		case guard.exit = <-guard.done:
			guard.exited = true
		case <-time.After(3 * time.Second):
			guard.cmd.Process.Kill()
			select {
			case guard.exit = <-guard.done:
				guard.exited = true
			case <-time.After(time.Second):
				return errors.Join(resultErr, fmt.Errorf("owned guard did not terminate"))
			}
			resultErr = errors.Join(resultErr, fmt.Errorf("owned guard completion timed out"))
		}
	}
	resultErr = errors.Join(resultErr, guard.exit)
	if resultErr != nil {
		status := "original_source_held_for_capture_reconciliation"
		var err error
		if guard.unsafe {
			status, err = captureGuardHold(guard.config, guard.anchor)
		} else {
			status, err = captureGuardRecover(guard.config, guard.anchor)
		}
		record := map[string]any{"config": guard.config, "status": status, "passed": err == nil,
			"capture_in_flight": guard.unsafe, "reason": "helper_failed_controller_handled_original_source"}
		if err != nil {
			record["error"] = err.Error()
		}
		resultErr = errors.Join(resultErr, err, captureGuardWrite(guard.anchor.evidence, "guard-controller-outcome.json", record))
	}
	return resultErr
}
