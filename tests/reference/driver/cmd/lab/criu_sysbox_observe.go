package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

var criuObservePhase = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
var criuObserveFields = []string{
	"faults", "queued_faults", "zero_faults", "read_requests", "read_pages", "dispatch_ns",
	"read_excluding_copy_ns", "copy_calls", "copy_pages", "copy_ns", "copy_errors",
	"copy_eexist", "copy_eagain", "zero_calls", "zero_pages", "zero_ns", "handler_ns",
}

type criuLazyObservation struct {
	Schema    int                         `json:"schema"`
	DaemonPID int                         `json:"daemon_pid"`
	Sequence  uint64                      `json:"sequence"`
	ClockNS   uint64                      `json:"clock_ns"`
	Final     bool                        `json:"final"`
	Total     map[string]uint64           `json:"total"`
	Active    []criuLazyObservationReader `json:"active"`
}

type criuLazyObservationReader struct {
	ID        uint64            `json:"reader_id"`
	ImagePID  int               `json:"image_pid"`
	NativePID int               `json:"native_pid"`
	Counts    map[string]uint64 `json:"counts"`
}

func criuLazyObserveEnabled() bool {
	return os.Getenv("CRIU_SYSBOX_LAZY_OBSERVE") == "1"
}

func criuLazyObserveGuard(root, id string) error {
	switch os.Getenv("CRIU_SYSBOX_LAZY_OBSERVE") {
	case "", "0":
		return nil
	case "1":
	default:
		return fmt.Errorf("CRIU_SYSBOX_LAZY_OBSERVE must be 0 or 1")
	}
	if root != "/experiment" || id != "source-sysbox" {
		return fmt.Errorf("lazy observation is outside the owned source")
	}
	for _, name := range []string{"CRIU_SYSBOX_LAZY_INHERIT", "CRIU_SYSBOX_RETAIN_SOURCE",
		"CRIU_SYSBOX_LAZY", "CRIU_SYSBOX_PROBE"} {
		if os.Getenv(name) != "1" {
			return fmt.Errorf("lazy observation requires %s=1", name)
		}
	}
	if value := os.Getenv("CRIU_SYSBOX_PROFILE"); value != "" && value != "0" {
		return fmt.Errorf("lazy observation is a separate diagnostic from CPU profiling")
	}
	return nil
}

func criuObserveValidate(value criuLazyObservation, pid int) error {
	if pid <= 0 || value.Schema != 1 || value.DaemonPID != pid || value.Sequence == 0 ||
		value.ClockNS == 0 || len(value.Active) > 4096 {
		return fmt.Errorf("invalid owned lazy observation identity/schema")
	}
	check := func(counts map[string]uint64) error {
		if len(counts) != len(criuObserveFields) {
			return fmt.Errorf("incomplete lazy observation counters")
		}
		for _, field := range criuObserveFields {
			if _, exists := counts[field]; !exists {
				return fmt.Errorf("missing lazy observation counter %s", field)
			}
		}
		return nil
	}
	if err := check(value.Total); err != nil {
		return err
	}
	seen := map[uint64]bool{}
	for _, reader := range value.Active {
		if reader.ID == 0 || seen[reader.ID] || reader.ImagePID <= 0 || reader.NativePID < -1 || reader.NativePID == 0 {
			return fmt.Errorf("invalid lazy observation reader identity")
		}
		seen[reader.ID] = true
		if err := check(reader.Counts); err != nil {
			return err
		}
	}
	return nil
}

func criuObserveRead(path string, pid int) (value criuLazyObservation, resultErr error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return value, err
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 ||
		info.Size() > 1<<20 || int(info.Sys().(*syscall.Stat_t).Uid) != os.Geteuid() {
		return value, fmt.Errorf("invalid owned lazy observation file: %v", err)
	}
	decoder := json.NewDecoder(io.LimitReader(file, (1<<20)+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return value, fmt.Errorf("trailing lazy observation data: %v", err)
	}
	return value, criuObserveValidate(value, pid)
}

func (d *criuLazyDaemon) observeSnapshot() (value criuLazyObservation, resultErr error) {
	if d.observePath == "" || d.command == nil || d.command.Process == nil {
		return value, fmt.Errorf("daemon has no owned observation path")
	}
	finished := d.pollCompletion()
	if finished && d.exitErr != nil {
		return value, d.exitErr
	}
	if !finished {
		if err := d.command.Process.Signal(syscall.SIGUSR2); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return value, err
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		value, err := criuObserveRead(d.observePath, d.command.Process.Pid)
		if err != nil && !os.IsNotExist(err) {
			return value, err
		}
		if err == nil && (value.Sequence > d.observeSequence || value.Final && value.Sequence >= d.observeSequence) {
			if value.ClockNS < d.observeClockNS {
				return value, fmt.Errorf("lazy observation clock moved backwards")
			}
			d.observeSequence, d.observeClockNS = value.Sequence, value.ClockNS
			return value, nil
		}
		if d.pollCompletion() && d.exitErr != nil {
			return value, d.exitErr
		}
		if time.Now().After(deadline) {
			return value, fmt.Errorf("lazy observation acknowledgement timed out")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func criuObserveSave(id, phase, edge string, value criuLazyObservation) (string, error) {
	path := filepath.Join(criuRoot, "lazy-observe-"+id+"-"+phase+"-"+edge+".json")
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	_, writeErr := file.Write(append(data, '\n'))
	return path, errors.Join(writeErr, file.Close())
}

func criuBeginServiceObservation(id, phase string) (func() error, error) {
	if !criuLazyObserveEnabled() {
		return func() error { return nil }, nil
	}
	if (id != "source-sysbox" && id != "copy-sysbox") || !criuObservePhase.MatchString(phase) {
		return nil, fmt.Errorf("service observation is outside owned runtime/phase")
	}
	imageDirectory := "/experiment/sysbox/images"
	if id == "copy-sysbox" {
		imageDirectory = "/experiment/sysbox-copy/images"
	}
	var daemon *criuLazyDaemon
	for _, candidate := range criuLazyDaemons {
		if filepath.Dir(candidate.logPath) == imageDirectory {
			if daemon != nil {
				return nil, fmt.Errorf("multiple daemons own an observation")
			}
			daemon = candidate
		}
	}
	if daemon == nil && phase == "source" { // Before the first checkpoint.
		return func() error { return nil }, nil
	}
	if daemon == nil {
		return nil, fmt.Errorf("no lazy daemon owns service observation")
	}
	before, err := daemon.observeSnapshot()
	if err != nil {
		return nil, err
	}
	beforePath, err := criuObserveSave(id, phase, "before", before)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	return func() (resultErr error) {
		defer func() {
			if resultErr != nil {
				record("sysbox-lazy-observe-"+id+"-"+phase, start,
					asJSON(map[string]any{"runtime_id": id, "phase": phase, "before_path": beforePath}), resultErr)
			}
		}()
		after, err := daemon.observeSnapshot()
		if err != nil {
			return err
		}
		afterPath, err := criuObserveSave(id, phase, "after", after)
		if err != nil {
			return err
		}
		delta := map[string]uint64{}
		for _, field := range criuObserveFields {
			if after.Total[field] < before.Total[field] {
				return fmt.Errorf("lazy observation counter moved backwards: %s", field)
			}
			delta[field] = after.Total[field] - before.Total[field]
		}
		record("sysbox-lazy-observe-"+id+"-"+phase, start, asJSON(map[string]any{
			"runtime_id": id, "phase": phase, "daemon_pid": after.DaemonPID,
			"before_path": beforePath, "after_path": afterPath, "delta": delta,
			"interval_ns": after.ClockNS - before.ClockNS,
			"scope":       "diagnostic only; all owned sandbox paging during actual service requests, including concurrent activity; dispatch includes COPY and handler includes nested work; raw reader snapshots retained separately; exited-reader work remains in total",
		}), nil)
		return nil
	}, nil
}
