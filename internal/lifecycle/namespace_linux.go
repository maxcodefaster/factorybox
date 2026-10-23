//go:build linux

package lifecycle

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"

	"golang.org/x/sys/unix"
)

type checkpointAdmissionError struct {
	kind       string
	cause      error
	recovered  bool
	rejectedAt string
}

func (failure *checkpointAdmissionError) Error() string {
	return checkpointAdmissionPrefix(failure.kind) + failure.cause.Error()
}

func (failure *checkpointAdmissionError) Unwrap() error { return failure.cause }

func checkpointAdmissionPrefix(kind string) string {
	if kind == "descriptors" {
		return "checkpoint descriptor admission: "
	}
	if kind == "filesystem" {
		return "checkpoint filesystem admission: "
	}
	return "checkpoint namespace admission: "
}

type checkpointAdmissionRejection struct {
	Version    int    `json:"version"`
	SourceID   string `json:"source_id"`
	SnapshotID string `json:"snapshot_id"`
	SourcePID  int    `json:"source_pid"`
	Kind       string `json:"kind,omitempty"`
	RejectedAt string `json:"rejected_at"`
	Error      string `json:"error"`
}

// Reconcile only a proved pre-capture rejection. No freezer write, CRIU
// invocation or second snapshot is permitted while replaying this outcome.
func (e *Engine) reconcileRejectedCheckpoint(record Record, id, relative string) (resultErr error) {
	var rejection checkpointAdmissionRejection
	err := e.readCheckpointJSON(relative+"/admission-rejected.json", &rejection)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if rejection.Version != 1 || rejection.SourceID != record.ID || rejection.SnapshotID != id ||
		rejection.SourcePID <= 1 || rejection.RejectedAt == "" ||
		(rejection.Kind != "" && rejection.Kind != "filesystem" && rejection.Kind != "descriptors" && rejection.Kind != "timers") ||
		!strings.HasPrefix(rejection.Error, checkpointAdmissionPrefix(rejection.Kind)) {
		return fmt.Errorf("checkpoint admission rejection differs from its original operation")
	}
	var intent checkpointIntent
	if err := e.readCheckpointJSON(relative+"/checkpoint-intent.json", &intent); err != nil {
		return err
	}
	if intent.Version != 1 || intent.ID != id || intent.SourceID != record.ID ||
		intent.Owner != record.Owner || intent.RuntimeID != record.RuntimeID || intent.BootID != record.BootID ||
		intent.ConfigSHA256 != record.ConfigSHA256 || intent.RuntimeSHA256 != record.RuntimeSHA256 ||
		intent.CriuSHA256 != e.config.CriuSHA256 || intent.ManagerRoot != e.config.ManagerRoot || intent.RequestedAt == "" {
		return fmt.Errorf("checkpoint admission rejection has a different pinned intent")
	}
	var config captureGuardConfig
	if err := e.readCheckpointJSON(relative+"/guard/guard-intent.json", &config); err != nil {
		return err
	}
	if config.Token != id || config.PID != rejection.SourcePID || config.Root != e.config.Root ||
		config.StopSource != intent.StopSource || !reflect.DeepEqual(config.Source, record) {
		return fmt.Errorf("checkpoint admission guard differs from its original source")
	}
	if rejection.Kind == "descriptors" {
		var observation captureDescriptorAdmission
		err := e.readCheckpointJSON(relative+"/guard/descriptor-admission.json", &observation)
		if err != nil || observation.Version != 1 || observation.Token != config.Token ||
			observation.SourcePID != config.PID || observation.SourceStart != config.Start || observation.Passed {
			return fmt.Errorf("descriptor admission observation differs from its original rejection: %v", err)
		}
	}
	for _, path := range []string{"/guard/guard-capture-entered.json", "/capture-result.json", "/checkpoint.json"} {
		if _, err := e.root.Lstat(relative + path); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("checkpoint admission conflicts with a capture or inaccessible record: %s: %v", path, err)
		}
	}
	var outcome struct {
		Config          captureGuardConfig `json:"config"`
		Reason          string             `json:"reason"`
		Status          string             `json:"status"`
		CaptureInFlight bool               `json:"capture_in_flight"`
		Passed          bool               `json:"passed"`
		Error           string             `json:"error,omitempty"`
	}
	err = e.readCheckpointJSON(relative+"/guard/guard-outcome.json", &outcome)
	if errors.Is(err, os.ErrNotExist) {
		err = e.readCheckpointJSON(relative+"/guard/guard-controller-outcome.json", &outcome)
	}
	if err != nil || !outcome.Passed || outcome.CaptureInFlight || !reflect.DeepEqual(outcome.Config, config) ||
		(outcome.Status != "original_source_thawed" && outcome.Status != "source_already_running") {
		return fmt.Errorf("checkpoint admission has no proved safe source outcome: %v", err)
	}
	anchors := captureGuardAnchors{}
	defer func() { resultErr = errors.Join(resultErr, anchors.close()) }()
	anchors.group, err = captureGuardOpen(cgroupRoot + "/" + record.RuntimeID)
	if err != nil {
		return err
	}
	anchors.evidence, err = e.root.Open(relative + "/guard")
	if err != nil {
		return err
	}
	anchors.root, err = e.root.Open(".")
	if err != nil {
		return err
	}
	fd, err := unix.PidfdOpen(config.PID, 0)
	if err != nil {
		return err
	}
	anchors.pid = os.NewFile(uintptr(fd), "admission recovery original pidfd")
	if err := captureGuardValidate(config, anchors); err != nil {
		return err
	}
	events, err := captureGuardEvents(anchors.group)
	if err != nil || events["populated"] != 1 || events["frozen"] != 0 {
		return fmt.Errorf("rejected source is no longer confirmed running: %v", err)
	}
	return &checkpointAdmissionError{
		kind:      rejection.Kind,
		cause:     errors.New(strings.TrimPrefix(rejection.Error, checkpointAdmissionPrefix(rejection.Kind))),
		recovered: true, rejectedAt: rejection.RejectedAt,
	}
}

type timeNamespaceID struct {
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
}

func openTimeNamespace(tid int, name string) (*os.File, timeNamespaceID, error) {
	if tid <= 0 || (name != "time" && name != "time_for_children") {
		return nil, timeNamespaceID{}, fmt.Errorf("invalid time namespace target")
	}
	path := fmt.Sprintf("/proc/%d/ns/%s", tid, name)
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, timeNamespaceID{}, err
	}
	file := os.NewFile(uintptr(fd), path)
	var stat unix.Stat_t
	var filesystem unix.Statfs_t
	kind, kindErr := unix.IoctlRetInt(fd, unix.NS_GET_NSTYPE)
	err = errors.Join(unix.Fstat(fd, &stat), unix.Fstatfs(fd, &filesystem), kindErr)
	if err != nil || filesystem.Type != unix.NSFS_MAGIC || kind != unix.CLONE_NEWTIME {
		file.Close()
		return nil, timeNamespaceID{}, fmt.Errorf("task %d has no verified %s namespace handle: %v", tid, name, err)
	}
	return file, timeNamespaceID{Device: uint64(stat.Dev), Inode: stat.Ino}, nil
}

// Every thread's time_for_children must match its current time namespace.
// Additional root-user-owned clocks require the verified CRIU feature ABI.
// Check the exact frozen tree before CRIU's network/ptrace hooks can run.
func (guard *captureGuard) admitCheckpointNamespaces(rootOwnedTime bool) error {
	if err := guard.check(); err != nil {
		return err
	}
	if guard.unsafe || guard.stopped {
		return fmt.Errorf("namespace admission is outside the pre-capture phase")
	}
	if err := captureGuardValidate(guard.config, guard.anchor); err != nil {
		return err
	}
	events, err := captureGuardEvents(guard.anchor.group)
	if err != nil || events["frozen"] != 1 || events["populated"] != 1 {
		return fmt.Errorf("namespace admission requires the frozen original tree: %v", err)
	}
	root, expected, err := openTimeNamespace(guard.config.PID, "time")
	if err != nil {
		return err
	}
	// Retain this handle throughout the scan so its namespace identity cannot
	// be recycled if the original source is killed concurrently.
	defer root.Close()
	var rootUser timeNamespaceID
	if rootOwnedTime {
		fd, err := unix.Open(fmt.Sprintf("/proc/%d/ns/user", guard.config.PID), unix.O_RDONLY|unix.O_CLOEXEC, 0)
		if err != nil {
			return err
		}
		defer unix.Close(fd)
		var stat unix.Stat_t
		if err := unix.Fstat(fd, &stat); err != nil {
			return err
		}
		rootUser = timeNamespaceID{Device: uint64(stat.Dev), Inode: stat.Ino}
	}
	threads, err := captureGuardThreads(guard.config, guard.anchor)
	if err != nil || len(threads) == 0 {
		return fmt.Errorf("namespace admission lacks the original task tree: %v", err)
	}
	tids := make([]int, 0, len(threads))
	for tid := range threads {
		tids = append(tids, tid)
	}
	slices.Sort(tids)
	for _, tid := range tids {
		var current timeNamespaceID
		for _, name := range []string{"time", "time_for_children"} {
			file, observed, err := openTimeNamespace(tid, name)
			if err != nil {
				return err
			}
			if name == "time" {
				current = observed
			}
			if (name == "time" && observed != expected && !rootOwnedTime) ||
				(name == "time_for_children" && observed != current) {
				file.Close()
				return fmt.Errorf("nested time namespace is not supported by the selected CRIU: task %d %s=%+v root=%+v",
					tid, name, observed, expected)
			}
			if name == "time" && observed != expected && rootOwnedTime {
				ownerFD, err := unix.IoctlRetInt(int(file.Fd()), unix.NS_GET_USERNS)
				if err != nil {
					file.Close()
					return err
				}
				var owner unix.Stat_t
				statErr := unix.Fstat(ownerFD, &owner)
				closeErr := unix.Close(ownerFD)
				if statErr != nil || closeErr != nil ||
					(timeNamespaceID{Device: uint64(owner.Dev), Inode: owner.Ino}) != rootUser {
					file.Close()
					return fmt.Errorf("nested time namespace is not owned by the source root user namespace: %v %v",
						statErr, closeErr)
				}
				memberFD, err := unix.Open(fmt.Sprintf("/proc/%d/ns/user", tid), unix.O_RDONLY|unix.O_CLOEXEC, 0)
				if err != nil {
					file.Close()
					return err
				}
				var member unix.Stat_t
				statErr = unix.Fstat(memberFD, &member)
				closeErr = unix.Close(memberFD)
				if statErr != nil || closeErr != nil ||
					(timeNamespaceID{Device: uint64(member.Dev), Inode: member.Ino}) != rootUser {
					file.Close()
					return fmt.Errorf("nested time namespace task is not in the source root user namespace: %v %v",
						statErr, closeErr)
				}
			}
			if err := file.Close(); err != nil {
				return err
			}
		}
		birth, err := processBirth(tid)
		if err != nil || birth != threads[tid] {
			return fmt.Errorf("task %d changed during namespace admission: %v", tid, err)
		}
	}
	return captureGuardValidate(guard.config, guard.anchor)
}
