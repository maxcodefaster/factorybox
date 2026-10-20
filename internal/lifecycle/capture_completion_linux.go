//go:build linux

package lifecycle

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"

	"golang.org/x/sys/unix"
)

type captureWorkerIntent struct {
	Token      string `json:"token"`
	PID        int    `json:"worker_pid"`
	Start      string `json:"worker_start"`
	SourcePID  int    `json:"source_pid"`
	CriuSHA256 string `json:"criu_sha256"`
	Scope      string `json:"scope"`
}

type captureProcessCompletion struct {
	Version        int                 `json:"version"`
	Worker         captureWorkerIntent `json:"worker"`
	SourceStart    string              `json:"source_start"`
	SourceStateSHA string              `json:"source_state_sha256"`
}

type captureAbortIntent struct {
	Version  int    `json:"version"`
	Token    string `json:"token"`
	FailedAt string `json:"failed_at"`
	Error    string `json:"error"`
}

func captureCompletionBytes(root *os.File, path string, maximum int64) ([]byte, error) {
	fd, err := unix.Openat2(int(root.Fd()), path, &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NONBLOCK,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_XDEV,
	})
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "capture completion record")
	defer file.Close()
	var info unix.Stat_t
	if err := unix.Fstat(fd, &info); err != nil {
		return nil, err
	}
	if info.Mode&unix.S_IFMT != unix.S_IFREG || info.Nlink != 1 || info.Uid != 0 ||
		info.Mode&0777 != 0600 || info.Size > maximum {
		return nil, fmt.Errorf("capture completion record has invalid ownership or size")
	}
	data, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil || int64(len(data)) > maximum {
		return nil, fmt.Errorf("capture completion record exceeds bound: %v", err)
	}
	return data, nil
}

func captureCompletionRead(root *os.File, path string, value any) error {
	data, err := captureCompletionBytes(root, path, 64<<10)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("capture completion record has trailing data: %v", err)
	}
	return nil
}

func captureGuardProcessCompleted(config captureGuardConfig, anchors captureGuardAnchors) error {
	if err := captureGuardCaptureReturned(config, anchors); err != nil {
		return err
	}
	return captureGuardProcessReceipt(config, anchors)
}

func captureGuardProcessReceipt(config captureGuardConfig, anchors captureGuardAnchors) error {
	if !digest.MatchString(config.CriuSHA256) {
		return fmt.Errorf("capture completion lacks its pinned CRIU identity")
	}
	var completion captureProcessCompletion
	if err := captureCompletionRead(anchors.evidence, "capture-process-complete.json", &completion); err != nil {
		return err
	}
	var worker captureWorkerIntent
	if err := captureCompletionRead(anchors.evidence, "capture-worker-intent.json", &worker); err != nil {
		return err
	}
	if completion.Version != 1 || completion.Worker != worker ||
		worker.Token != config.Token || worker.SourcePID != config.PID ||
		worker.CriuSHA256 != config.CriuSHA256 || worker.PID <= 1 ||
		worker.PID == config.PID || worker.PID == config.Controller || worker.Start == "" ||
		worker.Scope != "ordinary complete retained-source capture" ||
		completion.SourceStart != config.Start || !digest.MatchString(completion.SourceStateSHA) {
		return fmt.Errorf("completed process capture differs from its original worker/source")
	}
	// A completion receipt does not authorize releasing a source while the
	// writer is still active. The original worker must have been waited or
	// otherwise have exited; PID reuse cannot identify that same worker.
	if birth, err := processBirth(worker.PID); err == nil && birth == worker.Start {
		return fmt.Errorf("completed process capture still has its live worker")
	}
	object, err := objectPath(config.Source.ID)
	if err != nil {
		return err
	}
	original, err := captureCompletionBytes(anchors.root,
		filepath.Join(object, "captures", config.Token, "source-state.json"), checkpointManifestLimit)
	if err != nil {
		return err
	}
	if sum(original) != completion.SourceStateSHA {
		return fmt.Errorf("completed process capture source-state digest differs")
	}
	return nil
}

func captureGuardAbortIntent(config captureGuardConfig, anchors captureGuardAnchors) (captureAbortIntent, error) {
	var intent captureAbortIntent
	if err := captureCompletionRead(anchors.evidence, "capture-abort-intent.json", &intent); err != nil {
		return intent, err
	}
	if intent.Version != 1 || intent.Token != config.Token || intent.FailedAt == "" || intent.Error == "" {
		return intent, fmt.Errorf("capture abort intent differs from its original transaction")
	}
	return intent, nil
}

func (guard *captureGuard) captureAborted(cause error) (*checkpointCaptureError, error) {
	if !guard.unsafe || guard.stopped || cause == nil {
		return nil, fmt.Errorf("capture abort is outside its failed in-flight transaction")
	}
	if err := guard.check(); err != nil {
		return nil, err
	}
	if err := captureGuardProcessCompleted(guard.config, guard.anchor); err != nil {
		return nil, err
	}
	intent := captureAbortIntent{Version: 1, Token: guard.config.Token, FailedAt: now(), Error: cause.Error()}
	if err := captureGuardWrite(guard.anchor.evidence, "capture-abort-intent.json", intent); err != nil {
		return nil, err
	}
	if _, err := guard.socket.Write([]byte("capture-aborted:" + guard.config.Token)); err != nil {
		return nil, err
	}
	packet, err := captureGuardRead(int(guard.socket.Fd()), 2000)
	if err != nil || string(packet) != "capture-aborted-safe:"+guard.config.Token {
		return nil, fmt.Errorf("capture abort was not acknowledged: %v", err)
	}
	guard.unsafe = false
	return &checkpointCaptureError{cause: cause, failedAt: intent.FailedAt}, nil
}

// Replay a proved aborted capture without running CRIU, copying backing or
// writing the freezer. This also recovers a lost failed-pause publication.
func (e *Engine) reconcileAbortedCheckpoint(record Record, id, relative string) (resultErr error) {
	var acknowledgement struct {
		Token           string `json:"token"`
		CaptureInFlight bool   `json:"capture_in_flight"`
	}
	err := e.readCheckpointJSON(relative+"/guard/guard-capture-aborted.json", &acknowledgement)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || acknowledgement.Token != id || acknowledgement.CaptureInFlight {
		return fmt.Errorf("checkpoint abort lacks its exact safe acknowledgement: %v", err)
	}
	var config captureGuardConfig
	if err := e.readCheckpointJSON(relative+"/guard/guard-intent.json", &config); err != nil {
		return err
	}
	if config.Token != id || config.Root != e.config.Root || config.CriuSHA256 != e.config.CriuSHA256 ||
		!reflect.DeepEqual(config.Source, record) {
		return fmt.Errorf("checkpoint abort guard differs from its original source")
	}
	var intent checkpointIntent
	if err := e.readCheckpointJSON(relative+"/checkpoint-intent.json", &intent); err != nil {
		return err
	}
	if intent.Version != 1 || intent.ID != id || intent.SourceID != record.ID ||
		intent.Owner != record.Owner || intent.RuntimeID != record.RuntimeID || intent.BootID != record.BootID ||
		intent.ConfigSHA256 != record.ConfigSHA256 || intent.RuntimeSHA256 != record.RuntimeSHA256 ||
		intent.CriuSHA256 != e.config.CriuSHA256 || intent.ManagerRoot != e.config.ManagerRoot ||
		intent.StopSource != config.StopSource || intent.RequestedAt == "" {
		return fmt.Errorf("checkpoint abort has a different pinned intent")
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
		return fmt.Errorf("checkpoint abort has no proved running source outcome: %v", err)
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
	anchors.pid = os.NewFile(uintptr(fd), "aborted capture original pidfd")
	if err := captureGuardValidate(config, anchors); err != nil {
		return err
	}
	if err := captureGuardProcessReceipt(config, anchors); err != nil {
		return err
	}
	abort, err := captureGuardAbortIntent(config, anchors)
	if err != nil {
		return err
	}
	events, err := captureGuardEvents(anchors.group)
	if err != nil || events["populated"] != 1 || events["frozen"] != 0 {
		return fmt.Errorf("aborted capture source is no longer confirmed running: %v", err)
	}
	return &checkpointCaptureError{cause: errors.New(abort.Error), failedAt: abort.FailedAt, recovered: true}
}

type checkpointCaptureError struct {
	cause     error
	failedAt  string
	recovered bool
}

func (err *checkpointCaptureError) Error() string {
	return fmt.Sprintf("incomplete backing after completed process capture: %v", err.cause)
}

func (err *checkpointCaptureError) Unwrap() error { return err.cause }

var _ error = (*checkpointCaptureError)(nil)
