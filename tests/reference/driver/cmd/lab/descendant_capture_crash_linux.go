package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

type captureProcessStamp struct {
	PID   int    `json:"pid"`
	Start string `json:"start"`
	PPID  int    `json:"ppid"`
	State string `json:"state"`
	NSPID int    `json:"namespace_pid"`
}

type captureCrashJournal struct {
	Version int                 `json:"version"`
	Attempt int                 `json:"attempt"`
	Source  descendantLayout    `json:"source"`
	Init    captureProcessStamp `json:"original_init"`
	Threads map[int]string      `json:"original_threads"`
	Images  string              `json:"images"`
	Command captureProcessStamp `json:"command"`
	Tracer  captureProcessStamp `json:"tracer"`
	Parent  captureProcessStamp `json:"parent"`
	Holder  captureProcessStamp `json:"holder"`
	Token   string              `json:"guard_token"`
	Network map[string][]string `json:"original_network_filter"`
}

type captureCrashRecovery struct {
	Status     string `json:"status"`
	PID        int    `json:"source_pid"`
	Start      string `json:"source_start"`
	Child      int    `json:"reaped_host_pid"`
	ChildStart string `json:"reaped_start"`
	Result     int    `json:"exact_namespace_wait4_result"`
	Frozen     bool   `json:"source_left_frozen"`
	Threads    int    `json:"original_threads"`
}

type captureCrashOutcome struct {
	Journal       captureCrashJournal  `json:"journal"`
	Recovery      captureCrashRecovery `json:"recovery"`
	ImagesRemoved bool                 `json:"failed_generation_removed"`
	GroupRemoved  bool                 `json:"exact_empty_holder_cgroup_removed"`
}

type captureCrashContinuation struct {
	Version int              `json:"version"`
	Attempt int              `json:"attempt"`
	Source  descendantLayout `json:"source"`
	PID     int              `json:"original_init_pid"`
	Start   string           `json:"original_init_start"`
	Token   string           `json:"guard_token"`
	Passed  bool             `json:"full_source_services_passed"`
}

func captureCrashPrefix(attempt int) string {
	if attempt == 1 {
		return ""
	}
	return fmt.Sprintf("repeat-%d-", attempt)
}

func captureCrashRead(guard *descendantGuard, name string, value any) error {
	fd, err := unix.Openat(int(guard.anchor.evidence.Fd()), name,
		unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), name)
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return fmt.Errorf("capture recovery record is not a bounded regular file: %v", err)
	}
	decoder := json.NewDecoder(io.LimitReader(file, (1<<20)+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return fmt.Errorf("capture recovery record has trailing data: %v", err)
	}
	return nil
}

func captureProcess(pid int) (captureProcessStamp, error) {
	result := captureProcessStamp{PID: pid}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return result, err
	}
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 {
		return result, fmt.Errorf("capture process stat malformed")
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) < 20 {
		return result, fmt.Errorf("capture process stat incomplete")
	}
	result.State, result.Start = fields[0], fields[19]
	result.PPID, err = strconv.Atoi(fields[1])
	if err != nil {
		return result, err
	}
	status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return result, err
	}
	for _, line := range strings.Split(string(status), "\n") {
		values := strings.Fields(line)
		if len(values) > 1 && values[0] == "NSpid:" {
			result.NSPID, err = strconv.Atoi(values[len(values)-1])
			if err != nil {
				return result, err
			}
		}
	}
	if result.NSPID <= 0 {
		return result, fmt.Errorf("capture process namespace PID absent")
	}
	return result, nil
}

func captureFileSHA(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(file, (64<<20)+1))
	if err != nil {
		return "", err
	}
	if n > 64<<20 {
		return "", fmt.Errorf("capture binary exceeds its hash bound")
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func captureOwnedDescendant(pid int, command captureProcessStamp) error {
	for depth := 0; depth < 16 && pid > 1; depth++ {
		current, err := captureProcess(pid)
		if err != nil {
			return err
		}
		if current.PID == command.PID && current.Start == command.Start {
			return nil
		}
		pid = current.PPID
	}
	return fmt.Errorf("capturer is outside the exact owned ramctl command")
}

func captureTracer(pid int) (int, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "TracerPid:" {
			return strconv.Atoi(fields[1])
		}
	}
	return 0, fmt.Errorf("source tracer field absent")
}

func captureSourceDiagnostic(guard *descendantGuard, name string) error {
	tasks, err := os.ReadDir(fmt.Sprintf("/proc/%d/task", guard.config.PID))
	if err != nil {
		return err
	}
	details := map[string]map[string]string{}
	for _, task := range tasks {
		fields := map[string]string{}
		for _, name := range []string{"stat", "status", "wchan", "syscall", "stack", "children"} {
			file, err := os.Open(fmt.Sprintf("/proc/%d/task/%s/%s", guard.config.PID, task.Name(), name))
			if err != nil {
				fields[name] = err.Error()
				continue
			}
			data, readErr := io.ReadAll(io.LimitReader(file, 4097))
			file.Close()
			if readErr != nil {
				fields[name] = readErr.Error()
			} else {
				fields[name] = string(data)
			}
		}
		details[task.Name()] = fields
	}
	groups := map[string]string{}
	err = filepath.Walk(guard.config.Source.Cgroup, func(path string, info os.FileInfo, err error) error {
		if err != nil || !info.IsDir() {
			return err
		}
		for _, name := range []string{"cgroup.freeze", "cgroup.events"} {
			data, err := os.ReadFile(filepath.Join(path, name))
			if err != nil {
				return err
			}
			groups[filepath.Join(path, name)] = string(data)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return descendantGuardWrite(guard.anchor.evidence, name, map[string]any{
		"original_init": guard.config.PID, "tasks": details, "freezers": groups,
	})
}

// The guard stays in its conservative in-flight state across the failed
// attempt and its subsequent real capture. This never treats EOF as completion.
func descendantCaptureCrashProof(guard *descendantGuard, source descendantLayout,
	threads map[int]string, proof map[string]any, attempt int,
	armedBarrier func(captureCrashJournal) error) (resultErr error) {
	if guard == nil || attempt < 1 || attempt > 3 || guard.unsafe != (attempt > 1) || source != guard.config.Source ||
		os.Getenv("RUNTIME_FORK_CAPTURE_CRASH_PROOF") != "1" {
		return fmt.Errorf("armed crash proof lacks its exact original source guard")
	}
	if err := descendantGuardValidate(guard.config, guard.anchor); err != nil {
		return err
	}
	previousOutcomes, validOutcomes := proof["armed_capture_crash_recoveries"].([]captureCrashOutcome)
	if (attempt > 1 && !validOutcomes) || len(previousOutcomes) != attempt-1 {
		return fmt.Errorf("capture recovery attempt has no exact preceding outcome")
	}
	if attempt > 1 {
		previousPrefix := captureCrashPrefix(attempt - 1)
		var previous captureCrashOutcome
		var continuation captureCrashContinuation
		if err := captureCrashRead(guard, previousPrefix+"crash-cleaned.json", &previous); err != nil {
			return err
		}
		if err := captureCrashRead(guard, previousPrefix+"crash-source-continuation.json", &continuation); err != nil {
			return err
		}
		if !previous.ImagesRemoved || !previous.GroupRemoved ||
			previous.Journal.Attempt != attempt-1 || previous.Journal.Source != source ||
			previous.Journal.Token != guard.config.Token || previous.Journal.Init.PID != guard.config.PID ||
			previous.Journal.Init.Start != guard.config.Start ||
			asJSON(previous) != asJSON(previousOutcomes[attempt-2]) ||
			continuation.Version != 1 || continuation.Attempt != attempt-1 ||
			continuation.Source != source || continuation.PID != guard.config.PID ||
			continuation.Start != guard.config.Start || continuation.Token != guard.config.Token || !continuation.Passed {
			return fmt.Errorf("previous recovery lacks its exact cleanup and full source continuation")
		}
	}
	prefix := captureCrashPrefix(attempt)
	write := func(name string, value any) error {
		return descendantGuardWrite(guard.anchor.evidence, prefix+name, value)
	}
	for _, binary := range []struct{ path, env string }{
		{"/capture-reaper", "RUNTIME_FORK_CAPTURE_REAPER_SHA256"},
		{"/capture-stop.so", "RUNTIME_FORK_CAPTURE_STOP_SHA256"},
		{"/usr/local/sbin/criu", "RUNTIME_FORK_CAPTURE_CRIU_SHA256"},
	} {
		expected := os.Getenv(binary.env)
		actual, err := captureFileSHA(binary.path)
		if err != nil || len(expected) != 64 || actual != expected {
			return fmt.Errorf("capture recovery binary is not exactly pinned: %s %v", binary.path, err)
		}
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	images := source.Root + "/images-" + hex.EncodeToString(random[:])
	if err := descendantRAMPath(source, images, true); err != nil {
		return err
	}
	if _, err := os.Lstat(images); !os.IsNotExist(err) {
		return fmt.Errorf("crash attempt requires a fresh owned image generation")
	}
	init, err := captureProcess(guard.config.PID)
	if err != nil || init.Start != guard.config.Start || threads[init.PID] != init.Start {
		return fmt.Errorf("crash attempt lost original source init: %v", err)
	}
	network, err := captureNetworkOpen(guard)
	if err != nil {
		return err
	}
	defer network.anchor.Close()
	if attempt > 1 && asJSON(network.before) != asJSON(previousOutcomes[0].Journal.Network) {
		return fmt.Errorf("original network policy changed between repeated recoveries")
	}
	journal := captureCrashJournal{Version: 1, Attempt: attempt, Source: source, Init: init,
		Threads: threads, Images: images, Token: guard.config.Token, Network: network.before}
	if err := write("crash-prepare-intent.json", journal); err != nil {
		return err
	}
	if err := captureSourceDiagnostic(guard, prefix+"crash-source-before.json"); err != nil {
		return err
	}
	if err := os.Mkdir(images, 0700); err != nil {
		return err
	}
	imageAnchor, err := descendantGuardOpen(images)
	if err != nil {
		return err
	}
	defer imageAnchor.Close()
	if attempt == 1 {
		if err := guard.capturePhase(true); err != nil {
			return err
		}
	}
	log, err := os.OpenFile(guard.config.Evidence+"/"+prefix+"crash-command.log",
		os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer log.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	args := []string{"LD_PRELOAD=/capture-stop.so", "CAPTURER_CRASH_STOP_ARMED=1",
		"RUNTIME_FORK_PIPE_CAPTURE=0", "RUNTIME_FORK_COW_CAPTURE=1",
		"CRIU_SYSBOX_LAZY_INHERIT=0", "CRIU_SYSBOX_ASYNC_TRACK=0", "CRIU_SYSBOX_PRE_DUMP=0",
		"CRIU_SYSBOX_DELTA=0", "RUNTIME_FORK_LAZY_WP=0", "RUNTIME_FORK_LAZY_IDENTITY=0",
		"/ramctl", "checkpoint-retain-lineage", "/experiment/runtime", source.ID, images}
	command := exec.CommandContext(ctx, "env", args...)
	command.Stdout, command.Stderr, command.WaitDelay = log, log, 2*time.Second
	if err := command.Start(); err != nil {
		return err
	}
	journal.Command, err = captureProcess(command.Process.Pid)
	if err != nil {
		command.Process.Kill()
		command.Wait()
		return err
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	exited := false
	tracerFD := -1
	var holderGroup *os.File
	defer func() {
		if holderGroup != nil {
			holderGroup.Close()
		}
	}()
	defer func() {
		if tracerFD >= 0 {
			if resultErr != nil {
				_ = unix.PidfdSendSignal(tracerFD, unix.SIGKILL, nil, 0)
			}
			unix.Close(tracerFD)
		}
		if !exited {
			command.Process.Kill()
			<-done
		}
	}()
	if err := write("crash-command-started.json", journal); err != nil {
		return err
	}
	armed := regexp.MustCompile(`COW_CAPTURE_ARMED source=(\d+) child=(\d+) namespace_child=(\d+) advice_ranges=\d+ skipped_droppable=\d+ result=0`)
	deadline := time.Now().Add(15 * time.Second)
	for {
		select {
		case err := <-done:
			exited = true
			return fmt.Errorf("capture did not stop at the real armed point: %v", err)
		default:
		}
		data, err := os.ReadFile(images + "/dump.log")
		if err == nil {
			match := armed.FindStringSubmatch(string(data))
			if len(match) == 4 {
				parent, _ := strconv.Atoi(match[1])
				holder, _ := strconv.Atoi(match[2])
				childNS, _ := strconv.Atoi(match[3])
				journal.Parent, err = captureProcess(parent)
				if err != nil || threads[parent] != journal.Parent.Start {
					return fmt.Errorf("armed parent is outside the original source task set: %v", err)
				}
				journal.Holder, err = captureProcess(holder)
				if err != nil || journal.Holder.PPID != parent || journal.Holder.NSPID != childNS {
					return fmt.Errorf("armed helper identity does not match its actual parent/namespace: %v", err)
				}
				tracer, err := captureTracer(parent)
				if err != nil || tracer <= 1 {
					return fmt.Errorf("armed source has no actual capturer: %v", err)
				}
				journal.Tracer, err = captureProcess(tracer)
				if err == nil && journal.Tracer.State != "T" {
					time.Sleep(time.Millisecond)
					continue
				}
				if err != nil {
					return err
				}
				if err := captureOwnedDescendant(tracer, journal.Command); err != nil {
					return err
				}
				hash, err := captureFileSHA(fmt.Sprintf("/proc/%d/exe", tracer))
				if err != nil || hash != os.Getenv("RUNTIME_FORK_CAPTURE_CRIU_SHA256") {
					return fmt.Errorf("actual source tracer is not the pinned CRIU: %v", err)
				}
				tracerFD, err = unix.PidfdOpen(tracer, 0)
				if err != nil {
					return err
				}
				anchored, err := captureProcess(tracer)
				if err != nil || anchored != journal.Tracer {
					return fmt.Errorf("capturer changed while opening its pidfd: %v", err)
				}
				holderGroup, err = descendantGuardOpen(fmt.Sprintf("/sys/fs/cgroup/criu-probe-cow-holder-%d", tracer))
				if err != nil {
					return err
				}
				break
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("real armed capturer stop did not arrive: %v", err)
		}
		time.Sleep(time.Millisecond)
	}
	if err := descendantFreeze(source, true); err != nil {
		return err
	}
	if err := write("crash-armed-journal.json", journal); err != nil {
		return err
	}
	if armedBarrier != nil {
		if err := armedBarrier(journal); err != nil {
			return err
		}
		return fmt.Errorf("owned armed barrier returned without its expected controller death")
	}
	if err := unix.PidfdSendSignal(tracerFD, unix.SIGKILL, nil, 0); err != nil {
		return err
	}
	select {
	case err := <-done:
		exited = true
		if err == nil {
			return fmt.Errorf("deliberately killed capture unexpectedly reported success")
		}
	case <-time.After(8 * time.Second):
		return fmt.Errorf("owned ramctl did not return after its actual CRIU was killed")
	}
	var cleanup captureCrashOutcome
	if err := captureRecoverArmed(guard, journal, network, imageAnchor, holderGroup, prefix, &cleanup); err != nil {
		return err
	}
	if attempt == 1 {
		proof["armed_capture_crash_recovery"] = cleanup
	}
	proof["armed_capture_crash_recoveries"] = append(previousOutcomes, cleanup)
	return nil
}

// Reconciliation uses the original source/network/directory anchors and the
// durable armed journal. It starts no second source guard and commits no image.
func captureRecoverArmed(guard *descendantGuard, journal captureCrashJournal, network *captureNetwork,
	imageAnchor, holderGroup *os.File, prefix string, output *captureCrashOutcome) error {
	if guard == nil || !guard.unsafe || journal.Version != 1 ||
		journal.Source != guard.config.Source || journal.Init.PID != guard.config.PID ||
		journal.Init.Start != guard.config.Start || journal.Token != guard.config.Token ||
		network == nil || imageAnchor == nil || holderGroup == nil || output == nil {
		return fmt.Errorf("armed recovery lacks its exact original operation anchors")
	}
	source, threads, images := journal.Source, journal.Threads, journal.Images
	write := func(name string, value any) error {
		return descendantGuardWrite(guard.anchor.evidence, prefix+name, value)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		helper, err := captureProcess(journal.Holder.PID)
		if err == nil && helper.Start == journal.Holder.Start && helper.PPID == journal.Parent.PID && helper.State == "Z" {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("EXITKILL did not leave the exact expected zombie: %v", err)
		}
		time.Sleep(time.Millisecond)
	}
	if err := descendantGuardCaptureReturned(guard.config, guard.anchor); err != nil {
		return err
	}
	if err := captureSourceDiagnostic(guard, prefix+"crash-source-after-kill.json"); err != nil {
		return err
	}
	recoveryThreads, err := descendantThreads(source)
	if err != nil || asJSON(recoveryThreads) != asJSON(threads) {
		return fmt.Errorf("actual capturer death changed original frozen task identities: %v", err)
	}
	if err := network.recover(guard, prefix); err != nil {
		return err
	}
	if os.Getenv("RUNTIME_FORK_CAPTURE_CRASH_SETTLE") == "1" {
		if err := descendantFreeze(source, false); err != nil {
			return err
		}
		data, healthErr := criuCommand(3*time.Second, "nsenter",
			"--net=/proc/"+strconv.Itoa(guard.config.PID)+"/ns/net", "--", "/lab", "get", "/state")
		diagnosticErr := captureSourceDiagnostic(guard, prefix+"crash-source-after-kill-running.json")
		freezeErr := descendantFreeze(source, true)
		if err := write("crash-health-before-reaper.json",
			map[string]any{"output": data, "passed": healthErr == nil}); err != nil {
			return err
		}
		if err := errors.Join(healthErr, diagnosticErr, freezeErr); err != nil {
			return fmt.Errorf("source did not continue before exact zombie reaping: %w", err)
		}
		recoveryThreads, err = descendantThreads(source)
		if err != nil || recoveryThreads[guard.config.PID] != guard.config.Start {
			return fmt.Errorf("source identity changed during ordinary work before reaping: %v", err)
		}
	}
	data, err := criuCommand(8*time.Second, "env", "SYSBOX_FORK_COLLECT_SCOPE=1", "/capture-reaper",
		strconv.Itoa(journal.Parent.PID), journal.Parent.Start,
		strconv.Itoa(journal.Holder.PID), journal.Holder.Start,
		strconv.Itoa(journal.Holder.NSPID), strconv.Itoa(journal.Parent.NSPID), source.Cgroup)
	if err != nil {
		return fmt.Errorf("armed helper recovery failed: %s %w", data, err)
	}
	var recovered captureCrashRecovery
	if err := json.Unmarshal([]byte(data), &recovered); err != nil || recovered.Status != "passed" ||
		recovered.PID != journal.Parent.PID || recovered.Start != journal.Parent.Start ||
		recovered.Child != journal.Holder.PID || recovered.ChildStart != journal.Holder.Start ||
		recovered.Result != journal.Holder.NSPID || !recovered.Frozen || recovered.Threads != len(recoveryThreads) {
		return fmt.Errorf("exact helper recovery outcome differs: %s %v", data, err)
	}
	if err := descendantGuardCaptureReturned(guard.config, guard.anchor); err != nil {
		return err
	}
	if err := captureSourceDiagnostic(guard, prefix+"crash-source-after-reaper.json"); err != nil {
		return err
	}
	current, err := descendantThreads(source)
	if err != nil || asJSON(current) != asJSON(recoveryThreads) {
		return fmt.Errorf("recovered source task identities changed: %v", err)
	}
	if err := write("crash-recovered.json", map[string]any{
		"journal": journal, "recovery": recovered, "original_tasks_preserved": true,
		"reaper_threads_before": recoveryThreads, "reaper_threads_after": current,
		"whole_checkpoint_committed": false,
	}); err != nil {
		return err
	}
	// Preserve the failed attempt before removing only its anchored generation.
	for _, name := range []string{"dump.log", "runtime-config.json", "descriptors.json"} {
		data, err := os.ReadFile(filepath.Join(images, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if err := write("crash-failed-"+name+".json",
			map[string]any{"name": name, "content": string(data)}); err != nil {
			return err
		}
	}
	holderPath := holderGroup.Name()
	for path, anchor := range map[string]*os.File{images: imageAnchor, holderPath: holderGroup} {
		current, err := os.Lstat(path)
		original, savedErr := anchor.Stat()
		if err != nil || savedErr != nil || !current.IsDir() || !os.SameFile(current, original) {
			return fmt.Errorf("failed capture cleanup lost its directory anchor: %s %v %v", path, err, savedErr)
		}
	}
	events, err := descendantGuardEvents(holderGroup)
	if err != nil || events["populated"] != 0 {
		return fmt.Errorf("failed capture holder cgroup remains populated: %v", err)
	}
	if _, err := os.Stat(fmt.Sprintf("/proc/%d", journal.Holder.PID)); !os.IsNotExist(err) {
		return fmt.Errorf("reaped exact holder is still present: %v", err)
	}
	if err := errors.Join(os.Remove(holderPath), os.RemoveAll(images)); err != nil {
		return err
	}
	cleanup := captureCrashOutcome{Journal: journal, Recovery: recovered, ImagesRemoved: true, GroupRemoved: true}
	if err := write("crash-cleaned.json", cleanup); err != nil {
		return err
	}
	*output = cleanup
	return nil
}
