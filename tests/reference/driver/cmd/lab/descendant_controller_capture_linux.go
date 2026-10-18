package main

import (
	"errors"
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

type controllerLossCapture struct {
	journal             captureCrashJournal
	images, group       *os.File
	commandFD, tracerFD int
}

type controllerCaptureResult struct {
	Version           int                 `json:"version"`
	Journal           captureCrashJournal `json:"journal"`
	Recovery          captureCrashOutcome `json:"source_recovery"`
	CommandAdopted    bool                `json:"capture_command_adopted_alive_after_controller_death"`
	TracerSurvived    bool                `json:"actual_criu_survived_controller_death_stopped"`
	CompletionBlocked bool                `json:"live_tracer_blocked_completion_before_abort"`
	TracerKilled      bool                `json:"actual_criu_sigkilled_by_surviving_worker"`
	CommandReaped     bool                `json:"adopted_capture_command_reaped"`
	CommandExit       int                 `json:"capture_command_exit_code"`
	TracerReaped      bool                `json:"original_criu_reaped"`
	TracerReapedHere  bool                `json:"criu_reaped_by_surviving_worker"`
	Committed         bool                `json:"whole_checkpoint_committed"`
}

func (capture *controllerLossCapture) completedResult(guard *descendantGuard) (*controllerCaptureResult, error) {
	if guard == nil || capture.journal.Source != guard.config.Source ||
		capture.journal.Init.PID != guard.config.PID || capture.journal.Init.Start != guard.config.Start ||
		capture.journal.Token != guard.config.Token {
		return nil, fmt.Errorf("completed capture replay has a different original source/operation")
	}
	if err := descendantGuardValidate(guard.config, guard.anchor); err != nil {
		return nil, err
	}
	var result controllerCaptureResult
	err := captureCrashRead(guard, "worker-capture-reconciled.json", &result)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("completed capture result is uncertain: %w", err)
	}
	recovered := result.Recovery
	if result.Version != 1 || asJSON(result.Journal) != asJSON(capture.journal) ||
		asJSON(recovered.Journal) != asJSON(capture.journal) || !recovered.ImagesRemoved ||
		!recovered.GroupRemoved || recovered.Recovery.Status != "passed" || !recovered.Recovery.Frozen ||
		recovered.Recovery.PID != capture.journal.Parent.PID ||
		recovered.Recovery.Start != capture.journal.Parent.Start ||
		recovered.Recovery.Child != capture.journal.Holder.PID ||
		recovered.Recovery.ChildStart != capture.journal.Holder.Start ||
		recovered.Recovery.Result != capture.journal.Holder.NSPID || recovered.Recovery.Threads <= 0 ||
		!result.CommandAdopted || !result.TracerSurvived || !result.CompletionBlocked ||
		!result.TracerKilled || !result.CommandReaped || result.CommandExit <= 0 ||
		!result.TracerReaped || result.Committed {
		return nil, fmt.Errorf("completed capture result does not certify this exact aborted operation")
	}
	var retired captureCrashOutcome
	if err := captureCrashRead(guard, "crash-cleaned.json", &retired); err != nil {
		return nil, err
	}
	if asJSON(retired) != asJSON(result.Recovery) {
		return nil, fmt.Errorf("completed capture result differs from its durable exact cleanup")
	}
	// This is a historical outcome. In particular, never inspect or mutate a
	// later object at the retired path/PID, or freeze/thaw a now-running source.
	return &result, nil
}

func (capture *controllerLossCapture) close(abort bool) error {
	var result error
	for _, fd := range []int{capture.tracerFD, capture.commandFD} {
		if fd >= 0 {
			if abort {
				_ = unix.PidfdSendSignal(fd, unix.SIGKILL, nil, 0)
			}
			result = errors.Join(result, unix.Close(fd))
		}
	}
	for _, file := range []*os.File{capture.images, capture.group} {
		if file != nil {
			result = errors.Join(result, file.Close())
		}
	}
	return result
}

func controllerLossCaptureOpen(guard *descendantGuard, ready controllerLossReady,
	network *captureNetwork) (_ *controllerLossCapture, resultErr error) {
	if ready.Mode != "armed-capture" || len(ready.JournalSHA) != 64 || network == nil {
		return nil, fmt.Errorf("controller loss has no trusted armed capture journal")
	}
	var journal captureCrashJournal
	if err := captureCrashRead(guard, "crash-armed-journal.json", &journal); err != nil {
		return nil, err
	}
	checksum, err := captureFileSHA(guard.config.Evidence + "/crash-armed-journal.json")
	if err != nil || checksum != ready.JournalSHA {
		return nil, fmt.Errorf("controller armed journal digest differs: %v", err)
	}
	if journal.Version != 1 || journal.Attempt != 1 || journal.Source != guard.config.Source ||
		journal.Init.PID != guard.config.PID || journal.Init.Start != guard.config.Start ||
		journal.Token != guard.config.Token || journal.Command.PPID != ready.Controller.PID ||
		journal.Tracer.PPID != journal.Command.PID ||
		asJSON(journal.Network) != asJSON(network.before) {
		return nil, fmt.Errorf("armed capture journal is outside the original controller/source operation")
	}
	if err := descendantRAMPath(journal.Source, journal.Images, true); err != nil {
		return nil, err
	}
	var prepare captureCrashJournal
	if err := captureCrashRead(guard, "crash-prepare-intent.json", &prepare); err != nil {
		return nil, err
	}
	if prepare.Source != journal.Source || prepare.Images != journal.Images ||
		prepare.Token != journal.Token || prepare.Init.PID != journal.Init.PID ||
		prepare.Init.Start != journal.Init.Start {
		return nil, fmt.Errorf("armed operation has no matching pre-create intent")
	}
	threads, err := descendantThreads(journal.Source)
	if err != nil || asJSON(threads) != asJSON(journal.Threads) {
		return nil, fmt.Errorf("armed journal task identities differ before controller loss: %v", err)
	}
	command, err := captureProcess(journal.Command.PID)
	if err != nil || command.Start != journal.Command.Start || command.PPID != ready.Controller.PID {
		return nil, fmt.Errorf("owned capture command changed before adoption: %v", err)
	}
	tracer, err := captureProcess(journal.Tracer.PID)
	if err != nil || tracer.Start != journal.Tracer.Start || tracer.PPID != command.PID || tracer.State != "T" {
		return nil, fmt.Errorf("owned actual CRIU is not at its stopped armed point: %v", err)
	}
	parent, err := captureProcess(journal.Parent.PID)
	if err != nil || journal.Threads[parent.PID] != parent.Start ||
		parent.Start != journal.Parent.Start {
		return nil, fmt.Errorf("armed capture parent is outside the original source: %v", err)
	}
	actualTracer, err := captureTracer(parent.PID)
	if err != nil || actualTracer != tracer.PID {
		return nil, fmt.Errorf("journal does not identify the actual source tracer: %v", err)
	}
	holder, err := captureProcess(journal.Holder.PID)
	if err != nil || holder.Start != journal.Holder.Start || holder.PPID != parent.PID ||
		holder.NSPID != journal.Holder.NSPID {
		return nil, fmt.Errorf("journal does not identify the exact owned COW holder: %v", err)
	}
	for _, binary := range []struct{ process, expected string }{
		{fmt.Sprintf("/proc/%d/exe", command.PID), "/ramctl"},
		{fmt.Sprintf("/proc/%d/exe", tracer.PID), "/usr/local/sbin/criu"},
	} {
		actual, err := captureFileSHA(binary.process)
		expected, expectedErr := captureFileSHA(binary.expected)
		if err != nil || expectedErr != nil || actual != expected {
			return nil, fmt.Errorf("orphan adoption binary differs from its pin: %v %v", err, expectedErr)
		}
	}
	capture := &controllerLossCapture{journal: journal, commandFD: -1, tracerFD: -1}
	defer func() {
		if resultErr != nil {
			capture.close(false)
		}
	}()
	capture.commandFD, err = unix.PidfdOpen(command.PID, 0)
	if err != nil {
		return nil, err
	}
	capture.tracerFD, err = unix.PidfdOpen(tracer.PID, 0)
	if err != nil {
		return nil, err
	}
	for _, saved := range []captureProcessStamp{command, tracer} {
		current, err := captureProcess(saved.PID)
		if err != nil || current.Start != saved.Start || current.PPID != saved.PPID {
			return nil, fmt.Errorf("capture process changed while opening adoption pidfds: %v", err)
		}
	}
	capture.images, err = descendantGuardOpen(journal.Images)
	if err != nil {
		return nil, err
	}
	capture.group, err = descendantGuardOpen(fmt.Sprintf("/sys/fs/cgroup/criu-probe-cow-holder-%d", tracer.PID))
	if err != nil {
		return nil, err
	}
	if err := descendantGuardWrite(guard.anchor.evidence, "worker-capture-observed.json", map[string]any{
		"journal": journal, "command": command, "tracer": tracer, "holder": holder,
		"original_network_policy_preserved_in_worker": true,
	}); err != nil {
		return nil, err
	}
	return capture, nil
}

func (capture *controllerLossCapture) abortAndRecover(guard *descendantGuard,
	network *captureNetwork) (*controllerCaptureResult, error) {
	completed, err := capture.completedResult(guard)
	if err != nil || completed != nil {
		return completed, err
	}
	journal := capture.journal
	command, err := captureProcess(journal.Command.PID)
	if err != nil || command.Start != journal.Command.Start || command.PPID != os.Getpid() {
		return nil, fmt.Errorf("surviving worker did not adopt its exact live ramctl command: %v", err)
	}
	tracer, err := captureProcess(journal.Tracer.PID)
	if err != nil || tracer.Start != journal.Tracer.Start || tracer.PPID != command.PID || tracer.State != "T" {
		return nil, fmt.Errorf("orphaned actual CRIU did not survive controller death at the armed point: %v", err)
	}
	rejected := descendantGuardCaptureReturned(guard.config, guard.anchor)
	if rejected == nil || rejected.Error() != "capture completion still has a live tracer" {
		return nil, fmt.Errorf("live orphaned CRIU was not rejected as incomplete: %v", rejected)
	}
	if err := descendantGuardWrite(guard.anchor.evidence, "worker-capture-abort-intent.json", map[string]any{
		"journal": journal, "adopted_command": command, "still_live_tracer": tracer,
		"completion_rejected": rejected.Error(), "whole_checkpoint_committed": false,
	}); err != nil {
		return nil, err
	}
	if err := unix.PidfdSendSignal(capture.tracerFD, unix.SIGKILL, nil, 0); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(5 * time.Second)
	var commandStatus unix.WaitStatus
	for {
		current, err := captureProcess(command.PID)
		if err != nil || current.Start != command.Start || current.PPID != os.Getpid() {
			return nil, fmt.Errorf("adopted capture command changed before exact reap: %v", err)
		}
		found, err := unix.Wait4(command.PID, &commandStatus, unix.WNOHANG, nil)
		if err != nil {
			return nil, err
		}
		if found == command.PID {
			if !commandStatus.Exited() || commandStatus.ExitStatus() == 0 {
				return nil, fmt.Errorf("aborted orphan capture did not report a failed checkpoint: %v", commandStatus)
			}
			break
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("adopted capture command did not return after exact CRIU kill")
		}
		time.Sleep(time.Millisecond)
	}
	poll := []unix.PollFd{{Fd: int32(capture.tracerFD), Events: unix.POLLIN}}
	if count, err := unix.Poll(poll, 0); err != nil || count != 1 || poll[0].Revents&unix.POLLIN == 0 {
		return nil, fmt.Errorf("original CRIU pidfd has not confirmed its exit: %v", err)
	}
	criuReapedByWorker := false
	if remaining, err := captureProcess(tracer.PID); err == nil && remaining.Start == tracer.Start {
		if remaining.PPID != os.Getpid() || remaining.State != "Z" {
			return nil, fmt.Errorf("original CRIU retained an unexpected live owner/state after command exit")
		}
		var status unix.WaitStatus
		found, err := unix.Wait4(tracer.PID, &status, unix.WNOHANG, nil)
		if err != nil || found != tracer.PID || !status.Signaled() || status.Signal() != unix.SIGKILL {
			return nil, fmt.Errorf("surviving worker could not reap its exact killed CRIU: %v", err)
		}
		criuReapedByWorker = true
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	var recovered captureCrashOutcome
	if err := captureRecoverArmed(guard, journal, network, capture.images, capture.group, "", &recovered); err != nil {
		return nil, err
	}
	result := &controllerCaptureResult{
		Version: 1, Journal: journal, Recovery: recovered,
		CommandAdopted: true, TracerSurvived: true, CompletionBlocked: true,
		TracerKilled: true, CommandReaped: true, CommandExit: commandStatus.ExitStatus(),
		TracerReaped: true, TracerReapedHere: criuReapedByWorker, Committed: false,
	}
	if err := descendantGuardWrite(guard.anchor.evidence, "worker-capture-reconciled.json", result); err != nil {
		return nil, err
	}
	return result, nil
}
