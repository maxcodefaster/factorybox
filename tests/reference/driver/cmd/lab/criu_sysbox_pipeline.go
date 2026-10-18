package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

func criuCopyPipelineEnabled() bool {
	return os.Getenv("CRIU_SYSBOX_COPY_PIPELINE") == "1"
}

func criuCopyPipelineGuard(root, id string) error {
	for _, name := range []string{"CRIU_SYSBOX_COPY_PIPELINE", "CRIU_SYSBOX_COPY_PIPELINE_ABORT"} {
		switch os.Getenv(name) {
		case "", "0", "1":
		default:
			return fmt.Errorf("%s must be 0 or 1", name)
		}
	}
	if !criuCopyPipelineEnabled() {
		if os.Getenv("CRIU_SYSBOX_COPY_PIPELINE_ABORT") == "1" {
			return fmt.Errorf("pipeline abort proof requires the owned copy pipeline")
		}
		return nil
	}
	if root != "/experiment" || id != "source-sysbox" {
		return fmt.Errorf("copy pipeline is outside the owned source")
	}
	for _, name := range []string{"CRIU_SYSBOX_LAZY_INHERIT", "CRIU_SYSBOX_RETAIN_SOURCE",
		"CRIU_SYSBOX_LAZY", "CRIU_SYSBOX_PROBE", "CRIU_SYSBOX_RAM_CAPTURE"} {
		if os.Getenv(name) != "1" {
			return fmt.Errorf("copy pipeline requires %s=1", name)
		}
	}
	if value := os.Getenv("CRIU_SYSBOX_PROFILE"); value != "" && value != "0" {
		return fmt.Errorf("copy pipeline excludes command-descendant CPU profiling")
	}
	return nil
}

type criuCopyPrepared struct {
	at  time.Time
	err error
}

type criuCopyPipeline struct {
	copy         criuSysboxCopy
	command      *exec.Cmd
	cancel       context.CancelFunc
	connection   *net.UnixConn
	ready        chan criuCopyPrepared
	done         chan error
	prepared     criuCopyPrepared
	observed     bool
	finished     bool
	exitErr      error
	released     bool
	logPath      string
	start        time.Time
	registeredAt time.Time
	captureStart time.Time
	captureEnd   time.Time
	releaseAt    time.Time
}

func criuSysboxStartCopyPipeline(id string) (_ *criuCopyPipeline, resultErr error) {
	if err := criuCopyPipelineGuard(criuRoot, id); err != nil || !criuCopyPipelineEnabled() {
		return nil, errors.Join(err, fmt.Errorf("copy pipeline is not enabled"))
	}
	if _, err := criuSysboxFrozenThreads(); err != nil {
		return nil, err
	}
	start := time.Now()
	copy, err := criuSysboxCopyFiles()
	if err != nil {
		return nil, err
	}
	if err := criuSysboxCopyMounts(); err != nil {
		return nil, err
	}
	sockets, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET, 0)
	if err != nil {
		return nil, err
	}
	unix.CloseOnExec(sockets[0])
	unix.CloseOnExec(sockets[1])
	parent := os.NewFile(uintptr(sockets[0]), "copy-pipeline-controller")
	child := os.NewFile(uintptr(sockets[1]), "copy-pipeline-child")
	defer child.Close()
	connection, err := net.FileConn(parent)
	parent.Close()
	if err != nil {
		return nil, err
	}
	p := &criuCopyPipeline{copy: copy, start: start, connection: connection.(*net.UnixConn),
		ready: make(chan criuCopyPrepared, 1), done: make(chan error, 1),
		logPath: "/experiment/sysbox-copy/pipeline-helper.log"}
	defer func() {
		if resultErr != nil {
			if p.command != nil && p.command.Process != nil {
				resultErr = errors.Join(resultErr, p.close())
			} else {
				p.connection.Close()
			}
		}
	}()
	if err := p.connection.SetDeadline(start.Add(40 * time.Second)); err != nil {
		return nil, err
	}
	log, err := os.OpenFile(p.logPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	p.cancel = cancel
	p.command = exec.CommandContext(ctx, "/usr/local/sbin/sysbox-runc-experiment",
		"--root", "/experiment/runtime", "experiment-restore-copy", "--image-path", copy.Images, id)
	p.command.WaitDelay = 2 * time.Second
	p.command.ExtraFiles = []*os.File{child}
	p.command.Stdout, p.command.Stderr = log, log
	p.command.Env = append(os.Environ(), "RUNTIME_FORK_COPY_PIPELINE=1", "RUNTIME_FORK_LAZY_IDENTITY=1")
	if criuParentManifestReuseEnabled() {
		p.command.Env = append(p.command.Env, "RUNTIME_FORK_PRIVATE_IMAGES=1")
	}
	if criuLazyWPEnabled() {
		p.command.Env = append(p.command.Env, "RUNTIME_FORK_LAZY_WP=1")
	}
	if err := p.command.Start(); err != nil {
		log.Close()
		cancel()
		return nil, err
	}
	go func() {
		err := p.command.Wait()
		log.Close()
		p.done <- err
	}()
	if err := criuCopyReadMessage(p.connection, 'F'); err != nil {
		return nil, err
	}
	p.registeredAt = time.Now()
	if err := criuSysboxFrozenKernel(); err != nil {
		return nil, err
	}
	go func() {
		err := criuCopyReadMessage(p.connection, 'P')
		p.ready <- criuCopyPrepared{at: time.Now(), err: err}
	}()
	record("sysbox-copy-pipeline-started", start, asJSON(map[string]any{
		"pid": p.command.Process.Pid, "log": p.logPath,
		"scope": "private upper/mounts and manager/Fs registration ready under kernel freeze; volume preparation overlaps ptrace-held RAM capture; restore waits on owned seqpacket FD",
	}), nil)
	return p, nil
}

func criuCopyReadMessage(connection *net.UnixConn, expected byte) error {
	var message [8]byte
	count, _, flags, _, err := connection.ReadMsgUnix(message[:], nil)
	if err != nil {
		return fmt.Errorf("copy pipeline %c handshake: %w", expected, err)
	}
	if count != 1 || flags&unix.MSG_TRUNC != 0 || message[0] != expected {
		return fmt.Errorf("copy pipeline %c handshake invalid", expected)
	}
	return nil
}

func (p *criuCopyPipeline) awaitReady() error {
	if !p.observed {
		p.prepared, p.observed = <-p.ready, true
	}
	return p.prepared.err
}

func (p *criuCopyPipeline) wait() error {
	if !p.finished {
		p.exitErr, p.finished = <-p.done, true
	}
	return p.exitErr
}

func (p *criuCopyPipeline) output() string {
	data, _ := os.ReadFile(p.logPath)
	return string(data)
}

func (p *criuCopyPipeline) finish(sourceImages string) (copy criuSysboxCopy, resultErr error) {
	copy = p.copy
	if err := criuSysboxCopyImages(&copy, sourceImages); err != nil {
		return copy, err
	}
	copy.RestoreStart = time.Now()
	copy.Output, resultErr = criuSysboxRestoreUsingOwned("copy-sysbox", copy.Images, func() (output string, commandErr error) {
		traceStart := time.Now()
		before, beforeErr := criuSysboxPhaseCountersRead()
		commandStart := time.Now()
		defer func() {
			criuSysboxRecordCommandCounters("restore", copy.Images, traceStart, commandStart, before, beforeErr, commandErr)
		}()
		if err := p.awaitReady(); err != nil {
			return "", err
		}
		// Image cloning and every existing RAM proof have completed. Only
		// now can the already-prepared Sysbox command launch CRIU.
		if count, err := p.connection.Write([]byte("R")); err != nil || count != 1 {
			return "", fmt.Errorf("releasing owned copy restore: %v", err)
		}
		p.released = true
		p.releaseAt = time.Now()
		err := p.wait()
		return p.output(), err
	})
	record("sysbox-copy-pipeline-complete", p.start, asJSON(map[string]any{
		"preparation_ready_us":  p.prepared.at.Sub(p.start).Microseconds(),
		"registration_ready_us": p.registeredAt.Sub(p.start).Microseconds(),
		"capture_start_us":      p.captureStart.Sub(p.start).Microseconds(),
		"capture_end_us":        p.captureEnd.Sub(p.start).Microseconds(),
		"restore_release_us":    p.releaseAt.Sub(p.start).Microseconds(),
		"restore_released":      p.released, "helper_reaped": p.finished,
		"scope": "actual overlapping span; all work remains inside charged full readiness; stage spans must not be added",
	}), resultErr)
	return copy, resultErr
}

func (p *criuCopyPipeline) close() error {
	p.connection.Close()
	defer p.cancel()
	if p.finished {
		return nil
	}
	// EOF first lets the real command run its manager/Fs cleanup. Killing
	// on the cancellation path is only a bounded failure fallback.
	select {
	case p.exitErr = <-p.done:
		p.finished = true
	case <-time.After(5 * time.Second):
		p.command.Process.Kill()
		p.wait()
		return fmt.Errorf("owned copy helper did not clean up after cancellation")
	}
	if p.released {
		return p.exitErr
	}
	var cleanup struct {
		FS      bool `json:"fs_unregistered"`
		Manager bool `json:"manager_unregistered"`
	}
	for _, line := range strings.Split(p.output(), "\n") {
		if strings.HasPrefix(line, "RUNTIME_FORK_COPY_PIPELINE_CLEANUP ") {
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "RUNTIME_FORK_COPY_PIPELINE_CLEANUP ")), &cleanup); err != nil {
				return err
			}
		}
	}
	if p.exitErr == nil || !cleanup.FS || !cleanup.Manager {
		return fmt.Errorf("copy helper cancelled without confirmed manager/Fs cleanup: %v", p.exitErr)
	}
	record("sysbox-copy-pipeline-cancelled", p.start, asJSON(map[string]any{
		"helper_reaped": p.finished, "restore_released": p.released,
		"fs_unregistered": cleanup.FS, "manager_unregistered": cleanup.Manager,
	}), nil)
	return nil
}

func criuSysboxPipelineAbortProof(p *criuCopyPipeline, id, sourceImages string, retained map[int]string,
	identity map[string]any, inners []string, before map[string]map[string]any) error {
	start := time.Now()
	if _, err := os.Stat(sourceImages + "/stats-dump"); err != nil {
		return fmt.Errorf("pipeline abort proof requires a completed RAM capture: %w", err)
	}
	if err := p.awaitReady(); err != nil {
		return err
	}
	if err := p.close(); err != nil {
		return err
	}
	for _, path := range []string{"/experiment/runtime/copy-sysbox", p.copy.Images} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			return fmt.Errorf("cancelled pipeline created restore state %s: %v", path, err)
		}
	}
	if err := criuSysboxFrozenSourceProof(retained, "after-pipeline-abort"); err != nil {
		return err
	}
	if err := criuSysboxSetSourceFrozen(id, false); err != nil {
		return err
	}
	after, err := criuRuntimeIdentity(id)
	if err != nil || asJSON(after) != asJSON(identity) {
		return fmt.Errorf("pipeline abort changed original runtime identity: %v", err)
	}
	if _, err := criuRestoredStates(id, inners, before); err != nil {
		return err
	}
	if err := criuSysboxParentManifestProof(); err != nil {
		return err
	}
	if err := criuSysboxServiceResponses(id, "pipeline-abort", start); err != nil {
		return err
	}
	if err := criuSysboxTheiaReady([]string{id}, "pipeline-abort", start); err != nil {
		return err
	}
	record("sysbox-copy-pipeline-abort-source-ready", start,
		"RAM capture complete; prepared helper reaped; manager/Fs cleanup confirmed; no child runtime or images; immutable parent and original source identity/RAM/services/warm IDE retained", nil)
	return nil
}
