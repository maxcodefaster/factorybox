package main

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
	"regexp"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const descendantGuardChild = "RUNTIME_FORK_DESCENDANT_GUARD_CHILD"

var descendantGuardEvidence = regexp.MustCompile(`^/experiment/descendant(/generation-[12]|/post-resume-fork)?(/source-guard|/controller-loss)?$`)

type descendantGuardConfig struct {
	Version    int              `json:"version"`
	Source     descendantLayout `json:"source"`
	PID        int              `json:"pid"`
	Start      string           `json:"start"`
	Token      string           `json:"token"`
	Evidence   string           `json:"evidence"`
	Controller int              `json:"controller"`
}

type descendantGuardAnchors struct {
	group, pid, evidence *os.File
}

type descendantGuard struct {
	config descendantGuardConfig
	anchor descendantGuardAnchors
	socket *os.File
	cmd    *exec.Cmd
	done   chan error
	exit   error
	exited bool
	unsafe bool
}

func init() {
	if os.Getenv(descendantGuardChild) == "1" {
		if len(os.Args) != 1 {
			os.Exit(2)
		}
		if err := descendantGuardServe(); err != nil {
			fmt.Fprintln(os.Stderr, "owned descendant guard:", err)
			os.Exit(1)
		}
		os.Exit(0)
	}
}

func descendantGuardOpen(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

func (anchor descendantGuardAnchors) close() error {
	var result error
	for _, file := range []*os.File{anchor.group, anchor.pid, anchor.evidence} {
		if file != nil {
			result = errors.Join(result, file.Close())
		}
	}
	return result
}

func descendantGuardValidate(config descendantGuardConfig, anchor descendantGuardAnchors) error {
	layout, err := descendantOwnedLayout(config.Source.ID)
	if err != nil || layout != config.Source || config.Version != 1 ||
		config.PID <= 1 || config.Controller <= 1 || config.PID == config.Controller ||
		!descendantGuardEvidence.MatchString(config.Evidence) ||
		!regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(config.Token) {
		return fmt.Errorf("guard intent is outside the canonical owned fork")
	}
	if anchor.group == nil || anchor.pid == nil || anchor.evidence == nil {
		return fmt.Errorf("guard lacks original kernel anchors")
	}
	for path, file := range map[string]*os.File{layout.Cgroup: anchor.group, config.Evidence: anchor.evidence} {
		current, err := os.Lstat(path)
		saved, savedErr := file.Stat()
		if err != nil || savedErr != nil || !current.IsDir() || !os.SameFile(current, saved) {
			return fmt.Errorf("guard directory identity changed: %s: %v %v", path, err, savedErr)
		}
	}
	var fs unix.Statfs_t
	if err := unix.Fstatfs(int(anchor.group.Fd()), &fs); err != nil || fs.Type != unix.CGROUP2_SUPER_MAGIC {
		return fmt.Errorf("guard requires the original cgroup2 directory: %v", err)
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
	stamp, err := criuSysboxThreadStart(config.PID)
	if err != nil || stamp != config.Start {
		return fmt.Errorf("guard source process identity changed: %v", err)
	}
	membership, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", config.PID))
	root := strings.TrimPrefix(layout.Cgroup, "/sys/fs/cgroup")
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

func descendantGuardWrite(anchor *os.File, name string, value any) error {
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

func descendantGuardEvents(anchor *os.File) (map[string]uint64, error) {
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
	return criuRAMCounters(string(data))
}

func descendantGuardRecover(config descendantGuardConfig, anchor descendantGuardAnchors) (string, error) {
	if err := descendantGuardValidate(config, anchor); err != nil {
		return "", err
	}
	events, err := descendantGuardEvents(anchor.group)
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
		if err := descendantGuardValidate(config, anchor); err != nil {
			return "", err
		}
		events, err := descendantGuardEvents(anchor.group)
		if err == nil && events["populated"] == 1 && events["frozen"] == 0 {
			return "original_source_thawed", nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("guard source thaw did not complete: %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func descendantGuardHold(config descendantGuardConfig, anchor descendantGuardAnchors) (string, error) {
	if err := descendantGuardValidate(config, anchor); err != nil {
		return "", err
	}
	events, err := descendantGuardEvents(anchor.group)
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
		if err := descendantGuardValidate(config, anchor); err != nil {
			return "", err
		}
		events, err := descendantGuardEvents(anchor.group)
		if err == nil && events["populated"] == 1 && events["frozen"] == 1 {
			return "original_source_held_for_capture_reconciliation", nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("original unsafe source freeze was not confirmed: %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func descendantGuardRead(fd int, timeoutMS int) ([]byte, error) {
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

func descendantGuardCaptureReturned(config descendantGuardConfig, anchor descendantGuardAnchors) error {
	if err := descendantGuardValidate(config, anchor); err != nil {
		return err
	}
	events, err := descendantGuardEvents(anchor.group)
	if err != nil || events["frozen"] != 1 || events["populated"] != 1 {
		return fmt.Errorf("capture completion requires the original frozen source: %v", err)
	}
	threads, err := descendantThreads(config.Source)
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

func descendantGuardServe() (resultErr error) {
	socket := os.NewFile(3, "exclusive controller socket")
	anchor := descendantGuardAnchors{os.NewFile(4, "original source cgroup"),
		os.NewFile(5, "original source pidfd"), os.NewFile(6, "owned evidence directory")}
	defer socket.Close()
	defer anchor.close()
	data, err := descendantGuardRead(3, 2000)
	if err != nil || len(data) == 0 {
		return fmt.Errorf("guard initial intent is absent: %v", err)
	}
	var config descendantGuardConfig
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return err
	}
	if err := descendantGuardValidate(config, anchor); err != nil {
		return err
	}
	selfGroup, err := os.ReadFile("/proc/self/cgroup")
	if err != nil || strings.Contains(string(selfGroup), strings.TrimPrefix(config.Source.Cgroup, "/sys/fs/cgroup")) {
		return fmt.Errorf("guard is inside the captured source cgroup")
	}
	if _, err := socket.Write([]byte("armed:" + config.Token)); err != nil {
		return err
	}
	unsafe := false
	var request []byte
	for {
		request, err = descendantGuardRead(3, -1)
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
			if err := descendantGuardCaptureReturned(config, anchor); err != nil {
				return errors.Join(err, descendantGuardWrite(anchor.evidence, "guard-capture-rejected.json",
					map[string]any{"token": config.Token, "capture_in_flight": true, "error": err.Error()}))
			}
			unsafe, name, acknowledgement = false, "guard-capture-returned.json", "capture-safe:"
		}
		if name == "" {
			break
		}
		if err := descendantGuardValidate(config, anchor); err != nil {
			return err
		}
		if err := descendantGuardWrite(anchor.evidence, name, map[string]any{
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
		if err := descendantGuardValidate(config, anchor); err != nil {
			return err
		}
		events, err := descendantGuardEvents(anchor.group)
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
		status, resultErr = descendantGuardHold(config, anchor)
	} else if status == "" {
		status, resultErr = descendantGuardRecover(config, anchor)
	}
	record := map[string]any{"config": config, "reason": reason, "status": status,
		"capture_in_flight": unsafe, "passed": resultErr == nil}
	if resultErr != nil {
		record["error"] = resultErr.Error()
	}
	return errors.Join(resultErr, descendantGuardWrite(anchor.evidence, "guard-outcome.json", record))
}

func descendantStartGuard(source descendantLayout, pid int, evidence string) (_ *descendantGuard, resultErr error) {
	owned, err := descendantOwnedLayout(source.ID)
	if err != nil || source != owned || !descendantGuardEvidence.MatchString(evidence) {
		return nil, fmt.Errorf("source guard request is outside one owned fork")
	}
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return nil, err
	}
	start, err := criuSysboxThreadStart(pid)
	if err != nil {
		return nil, err
	}
	config := descendantGuardConfig{1, source, pid, start, hex.EncodeToString(token[:]), evidence, os.Getpid()}
	guard := &descendantGuard{config: config}
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
	guard.anchor.group, err = descendantGuardOpen(source.Cgroup)
	if err != nil {
		return nil, err
	}
	guard.anchor.evidence, err = descendantGuardOpen(evidence)
	if err != nil {
		return nil, err
	}
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		return nil, err
	}
	guard.anchor.pid = os.NewFile(uintptr(fd), "original source pidfd")
	if err := descendantGuardValidate(config, guard.anchor); err != nil {
		return nil, err
	}
	if err := descendantGuardWrite(guard.anchor.evidence, "guard-intent.json", config); err != nil {
		return nil, err
	}
	sockets, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	guard.socket = os.NewFile(uintptr(sockets[0]), "controller guard socket")
	child := os.NewFile(uintptr(sockets[1]), "guard control socket")
	defer child.Close()
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	guard.cmd = exec.Command(executable)
	guard.cmd.Env = append(os.Environ(), descendantGuardChild+"=1", "GOMAXPROCS=1", "GOMEMLIMIT=32MiB")
	guard.cmd.ExtraFiles = []*os.File{child, guard.anchor.group, guard.anchor.pid, guard.anchor.evidence}
	guard.cmd.Stdout, guard.cmd.Stderr = os.Stderr, os.Stderr
	if err := guard.cmd.Start(); err != nil {
		return nil, err
	}
	data, _ := json.Marshal(config)
	if _, err := guard.socket.Write(data); err != nil {
		return nil, err
	}
	ready, err := descendantGuardRead(int(guard.socket.Fd()), 2000)
	if err != nil || string(ready) != "armed:"+config.Token {
		return nil, fmt.Errorf("guard failed to arm: %s: %v", ready, err)
	}
	if err := descendantGuardWrite(guard.anchor.evidence, "guard-armed.json",
		map[string]any{"token": config.Token, "helper_pid": guard.cmd.Process.Pid}); err != nil {
		return nil, err
	}
	guard.done = make(chan error, 1)
	go func() { guard.done <- guard.cmd.Wait(); close(guard.done) }()
	return guard, nil
}

func (guard *descendantGuard) check() error {
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

func (guard *descendantGuard) capturePhase(enter bool) error {
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
	packet, err := descendantGuardRead(int(guard.socket.Fd()), 2000)
	if err != nil || string(packet) != response+guard.config.Token {
		return fmt.Errorf("guard capture phase was not acknowledged: %v", err)
	}
	guard.unsafe = enter
	return nil
}

// One guarded transaction may contain several capture operations. Checking an
// intermediate return must not publish a safe point before the final writer.
func (guard *descendantGuard) captureOperationReturned() error {
	if err := guard.check(); err != nil {
		return err
	}
	if !guard.unsafe {
		return fmt.Errorf("capture operation is outside its guarded transaction")
	}
	return descendantGuardCaptureReturned(guard.config, guard.anchor)
}

func (guard *descendantGuard) finish(recoverSource bool) (resultErr error) {
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
			status, err = descendantGuardHold(guard.config, guard.anchor)
		} else {
			status, err = descendantGuardRecover(guard.config, guard.anchor)
		}
		record := map[string]any{"config": guard.config, "status": status, "passed": err == nil,
			"capture_in_flight": guard.unsafe, "reason": "helper_failed_controller_handled_original_source"}
		if err != nil {
			record["error"] = err.Error()
		}
		resultErr = errors.Join(resultErr, err, descendantGuardWrite(guard.anchor.evidence, "guard-controller-outcome.json", record))
	}
	return resultErr
}
