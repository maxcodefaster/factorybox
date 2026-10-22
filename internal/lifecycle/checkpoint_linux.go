//go:build linux

package lifecycle

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"

	"factorybox/internal/sysboxcapture"
	"golang.org/x/sys/unix"
)

type checkpointIntent struct {
	Version       int    `json:"version"`
	ID            string `json:"id"`
	SourceID      string `json:"source_id"`
	Owner         string `json:"owner"`
	RuntimeID     string `json:"runtime_id"`
	BootID        string `json:"boot_id"`
	ConfigSHA256  string `json:"config_sha256"`
	RuntimeSHA256 string `json:"runtime_sha256"`
	CriuSHA256    string `json:"criu_sha256"`
	ManagerRoot   string `json:"manager_root"`
	RequestedAt   string `json:"requested_at"`
	StopSource    bool   `json:"stop_source,omitempty"`
}

type CheckpointFile struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

type Checkpoint struct {
	Version       int                               `json:"version"`
	ID            string                            `json:"id"`
	SourceID      string                            `json:"source_id"`
	RuntimeID     string                            `json:"runtime_id"`
	Owner         string                            `json:"owner"`
	BootID        string                            `json:"boot_id"`
	ConfigSHA256  string                            `json:"config_sha256"`
	RuntimeSHA256 string                            `json:"runtime_sha256"`
	CriuSHA256    string                            `json:"criu_sha256"`
	ManagerRoot   string                            `json:"manager_root"`
	CapturedAt    string                            `json:"captured_at"`
	ReleasedAt    string                            `json:"released_at,omitempty"`
	SourcePID     int                               `json:"source_pid"`
	SourceStopped bool                              `json:"source_stopped,omitempty"`
	Files         []CheckpointFile                  `json:"files"`
	Filesystem    *sysboxcapture.FilesystemSnapshot `json:"filesystem"`
}

type checkpointRecovery struct {
	Version       int                `json:"version"`
	Config        captureGuardConfig `json:"config"`
	CaptureSHA256 string             `json:"capture_sha256"`
	ReleasedAt    string             `json:"released_at"`
	Status        string             `json:"status"`
}

const checkpointManifestLimit = 4 << 20

func checkpointPath(id, snapshot string) (string, error) {
	object, err := objectPath(id)
	if err != nil {
		return "", err
	}
	if !tokenID.MatchString(snapshot) {
		return "", fmt.Errorf("checkpoint ID must be 32 lowercase hexadecimal characters")
	}
	return object + "/captures/" + snapshot, nil
}

// Checkpoint publishes an immutable generation only after its full capture and
// source release are durable. Repeating its ID verifies and returns that same
// generation. Unfinished capture is never silently repeated.
func (e *Engine) Checkpoint(ctx context.Context, id, snapshot string) (_ Checkpoint, resultErr error) {
	lock, err := e.lock()
	if err != nil {
		return Checkpoint{}, err
	}
	defer lock.Close()
	record, err := e.read(id)
	if err != nil {
		return Checkpoint{}, err
	}
	relative, err := checkpointPath(id, snapshot)
	if err != nil {
		return Checkpoint{}, err
	}
	if _, err := e.root.Lstat(relative); err == nil {
		return e.reconcileCheckpoint(ctx, record, snapshot, relative)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Checkpoint{}, err
	}
	if record.Phase != "running" {
		return Checkpoint{}, fmt.Errorf("checkpoint requires a running source")
	}
	return e.captureCheckpoint(ctx, lock, record, snapshot, relative, false)
}

func (e *Engine) captureCheckpoint(ctx context.Context, lock *os.File, record Record, snapshot, relative string, stopSource bool) (_ Checkpoint, resultErr error) {
	if err := e.admitStorageWrite(); err != nil {
		return Checkpoint{}, err
	}
	if !digest.MatchString(e.config.CriuSHA256) || !filepath.IsAbs(e.config.ManagerRoot) {
		return Checkpoint{}, fmt.Errorf("checkpoint requires pinned CRIU and an explicit manager root")
	}
	binary, err := openRuntime(e.config.Criu, e.config.CriuSHA256)
	if err != nil {
		return Checkpoint{}, err
	}
	features := sysboxcapture.InspectFeatures(ctx, binary)
	if err := binary.Close(); err != nil {
		return Checkpoint{}, err
	}
	if err := sysboxcapture.CheckExecutableParents(e.config.Criu); err != nil {
		return Checkpoint{}, err
	}
	state, err := e.state(ctx, record, lock)
	if err != nil || state == nil || state.Status != "running" {
		return Checkpoint{}, errors.Join(err, fmt.Errorf("checkpoint source is not running"))
	}
	executable, err := os.Executable()
	if err != nil {
		return Checkpoint{}, err
	}
	intent := checkpointIntent{Version: 1, ID: snapshot, SourceID: record.ID, Owner: record.Owner, RuntimeID: record.RuntimeID,
		BootID: record.BootID, ConfigSHA256: record.ConfigSHA256, RuntimeSHA256: record.RuntimeSHA256,
		CriuSHA256: e.config.CriuSHA256, ManagerRoot: e.config.ManagerRoot, RequestedAt: now(), StopSource: stopSource}
	guard, err := e.startCheckpointGuard(record, state.PID, executable, snapshot, &intent)
	if err != nil {
		return Checkpoint{}, err
	}
	finished := false
	var admission *checkpointAdmissionError
	var captureFailure *checkpointCaptureError
	defer func() {
		if !finished {
			// A killed worker cannot release its own reserve. Make space for
			// the validated abort receipts before asking the guard to recover.
			resultErr = errors.Join(resultErr, releaseCaptureReserve(guard.anchor.evidence, guard.config))
			if guard.unsafe {
				if failure, err := guard.captureAborted(resultErr); err == nil {
					captureFailure, resultErr = failure, failure
				}
			}
			recoveryErr := guard.finish(true)
			if admission != nil && recoveryErr == nil {
				admission.recovered = true
			}
			if captureFailure != nil && recoveryErr == nil {
				captureFailure.recovered = true
			}
			resultErr = errors.Join(resultErr, recoveryErr)
		}
	}()
	if err := guard.freeze(true); err != nil {
		return Checkpoint{}, err
	}
	kind := "descriptors"
	admissionErr := guard.admitCheckpointDescriptors(ctx)
	if admissionErr == nil {
		kind = ""
		admissionErr = guard.admitCheckpointNamespaces(features.RootOwnedTimeNamespaces)
	}
	if admissionErr == nil {
		kind = "filesystem"
		admissionErr = sysboxcapture.AdmitFilesystem(sysboxcapture.Request{
			RuntimeRoot: e.config.Root + "/runtime", RuntimeID: record.RuntimeID,
			Owner: record.Owner, Rootfs: record.Rootfs, Bundle: record.InputBundle,
			ManagerRoot: e.config.ManagerRoot, Base: record.Base,
		})
	}
	if admissionErr == nil {
		kind = "timers"
		admissionErr = guard.admitCheckpointTimers()
	}
	if admissionErr != nil {
		admission = &checkpointAdmissionError{kind: kind, cause: admissionErr, rejectedAt: now()}
		evidenceErr := e.publishCheckpoint(relative, "admission-rejected.json", checkpointAdmissionRejection{
			Version: 1, SourceID: record.ID, SnapshotID: snapshot,
			SourcePID: state.PID, Kind: kind, RejectedAt: admission.rejectedAt, Error: admission.Error(),
		})
		return Checkpoint{}, errors.Join(admission, evidenceErr)
	}
	if err := guard.capturePhase(true); err != nil {
		return Checkpoint{}, err
	}
	if err := e.capture(ctx, guard, lock, executable); err != nil {
		return Checkpoint{}, err
	}
	captured, err := e.checkpointResult(ctx, record, snapshot, relative, guard.config.PID)
	if err != nil {
		return Checkpoint{}, err
	}
	captured.SourceStopped = stopSource
	if err := e.publishCheckpoint(relative, "capture-result.json", captured); err != nil {
		return captured, err
	}
	if stopSource {
		if err := guard.captureStopped(); err != nil {
			return captured, err
		}
	} else {
		if err := guard.capturePhase(false); err != nil {
			return captured, err
		}
		if err := guard.freeze(false); err != nil {
			return captured, err
		}
	}
	err = guard.finish(false)
	finished = true
	if err != nil {
		return captured, err
	}
	captured.ReleasedAt = now()
	if err := e.publishCheckpoint(relative, "checkpoint.json", captured); err != nil {
		return captured, err
	}
	return captured, nil
}

func (e *Engine) checkpointResult(ctx context.Context, record Record, id, relative string, pid int) (Checkpoint, error) {
	generation := filepath.Join(e.config.Root, relative)
	var filesystem sysboxcapture.FilesystemSnapshot
	if err := e.readCheckpointJSON(relative+"/filesystem.json", &filesystem); err != nil {
		return Checkpoint{}, err
	}
	if err := sysboxcapture.VerifyFilesystemSnapshot(ctx, generation, &filesystem); err != nil {
		return Checkpoint{}, err
	}
	files, err := e.checkpointFiles(ctx, relative)
	if err != nil {
		return Checkpoint{}, err
	}
	return Checkpoint{Version: 1, ID: id, SourceID: record.ID, RuntimeID: record.RuntimeID, Owner: record.Owner,
		BootID: record.BootID, ConfigSHA256: record.ConfigSHA256, RuntimeSHA256: record.RuntimeSHA256,
		CriuSHA256: e.config.CriuSHA256, ManagerRoot: e.config.ManagerRoot, CapturedAt: now(), SourcePID: pid,
		Files: files, Filesystem: &filesystem}, nil
}

func (e *Engine) publishCheckpoint(relative, name string, result any) error {
	if filepath.Base(name) != name {
		return fmt.Errorf("checkpoint record name is not local")
	}
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	if len(data)+1 > checkpointManifestLimit {
		return fmt.Errorf("checkpoint record exceeds size limit")
	}
	directory, err := e.root.Open(relative)
	if err != nil {
		return err
	}
	defer directory.Close()
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	temporary := ".checkpoint-" + hex.EncodeToString(random[:])
	fd, err := unix.Openat(int(directory.Fd()), temporary,
		unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return err
	}
	defer unix.Unlinkat(int(directory.Fd()), temporary, 0)
	file := os.NewFile(uintptr(fd), temporary)
	_, writeErr := file.Write(append(data, '\n'))
	if err := errors.Join(writeErr, file.Sync(), file.Close()); err != nil {
		return err
	}
	if err := unix.Renameat2(int(directory.Fd()), temporary, int(directory.Fd()), name, unix.RENAME_NOREPLACE); err != nil {
		return err
	}
	return directory.Sync()
}

func (e *Engine) readCheckpoint(relative, name string) (Checkpoint, error) {
	var result Checkpoint
	if err := e.checkpointAvailable(relative); err != nil {
		return result, err
	}
	err := e.readCheckpointJSON(relative+"/"+name, &result)
	return result, err
}

func (e *Engine) readCheckpointJSON(path string, value any) error {
	directory, err := e.root.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()
	fd, err := unix.Openat2(int(directory.Fd()), path, &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NONBLOCK,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS})
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Nlink != 1 ||
		stat.Uid != 0 || stat.Mode&0777 != 0600 || stat.Size > checkpointManifestLimit {
		return fmt.Errorf("checkpoint record has invalid file identity or size")
	}
	data, err := io.ReadAll(io.LimitReader(file, checkpointManifestLimit+1))
	if err != nil {
		return err
	}
	if len(data) > checkpointManifestLimit {
		return fmt.Errorf("checkpoint record exceeds size limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("checkpoint manifest contains trailing data")
	}
	return nil
}

func (e *Engine) verifyCheckpoint(ctx context.Context, record Record, id, relative string, result Checkpoint) error {
	if err := e.verifyCheckpointMetadata(ctx, record, id, relative, result); err != nil {
		return err
	}
	return sysboxcapture.VerifyFilesystemSnapshot(ctx, filepath.Join(e.config.Root, relative), result.Filesystem)
}

func (e *Engine) verifyCheckpointMetadata(ctx context.Context, record Record, id, relative string, result Checkpoint) error {
	if result.Version != 1 || result.ID != id || result.SourceID != record.ID || result.RuntimeID != record.RuntimeID ||
		result.Owner != record.Owner || result.BootID != record.BootID || result.ConfigSHA256 != record.ConfigSHA256 ||
		result.RuntimeSHA256 != record.RuntimeSHA256 || result.CriuSHA256 != e.config.CriuSHA256 ||
		result.ManagerRoot != e.config.ManagerRoot || result.CapturedAt == "" || result.SourcePID <= 1 {
		return fmt.Errorf("checkpoint manifest identity or pinned recipe differs")
	}
	files, err := e.checkpointFiles(ctx, relative)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(files, result.Files) {
		return fmt.Errorf("checkpoint metadata or RAM payload changed")
	}
	var filesystem sysboxcapture.FilesystemSnapshot
	if err := e.readCheckpointJSON(relative+"/filesystem.json", &filesystem); err != nil {
		return err
	}
	if !reflect.DeepEqual(result.Filesystem, &filesystem) {
		return fmt.Errorf("checkpoint manifest differs from its saved filesystem metadata")
	}
	var intent checkpointIntent
	if err := e.readCheckpointJSON(relative+"/checkpoint-intent.json", &intent); err != nil {
		return err
	}
	if intent.Version != 1 || intent.ID != result.ID || intent.SourceID != result.SourceID ||
		intent.RuntimeID != result.RuntimeID || intent.Owner != result.Owner ||
		intent.BootID != result.BootID || intent.ConfigSHA256 != result.ConfigSHA256 ||
		intent.RuntimeSHA256 != result.RuntimeSHA256 || intent.CriuSHA256 != result.CriuSHA256 ||
		intent.ManagerRoot != result.ManagerRoot || intent.RequestedAt == "" {
		return fmt.Errorf("checkpoint manifest differs from its original intent")
	}
	if intent.StopSource != result.SourceStopped {
		return fmt.Errorf("checkpoint source-stop mode differs from its persisted intent")
	}
	return nil
}

// A fork verifies filesystem payloads at the independent materialization
// boundary, before runtime creation. Its intent binds the complete published RAM,
// filesystem manifest and source-release outcome before any external create.
func (e *Engine) publishedCheckpointMetadata(ctx context.Context, record Record, id, relative string) (Checkpoint, error) {
	if err := privateDirectory(filepath.Join(e.config.Root, relative)); err != nil {
		return Checkpoint{}, err
	}
	ready, err := e.readCheckpoint(relative, "checkpoint.json")
	if err != nil {
		return ready, err
	}
	if ready.ReleasedAt == "" {
		return ready, fmt.Errorf("ready checkpoint has no confirmed source release")
	}
	captured, err := e.readCheckpoint(relative, "capture-result.json")
	if err != nil {
		return ready, err
	}
	unreleased := ready
	unreleased.ReleasedAt = ""
	if !reflect.DeepEqual(unreleased, captured) {
		return ready, fmt.Errorf("ready checkpoint differs from its completed capture")
	}
	return ready, e.verifyCheckpointMetadata(ctx, record, id, relative, ready)
}

func (e *Engine) reconcileCheckpoint(ctx context.Context, record Record, id, relative string) (Checkpoint, error) {
	if err := privateDirectory(filepath.Join(e.config.Root, relative)); err != nil {
		return Checkpoint{}, err
	}
	ready, err := e.readCheckpoint(relative, "checkpoint.json")
	if err == nil {
		if ready.ReleasedAt == "" {
			return ready, fmt.Errorf("ready checkpoint has no confirmed source release")
		}
		captured, err := e.readCheckpoint(relative, "capture-result.json")
		if err != nil {
			return ready, err
		}
		unreleased := ready
		unreleased.ReleasedAt = ""
		if !reflect.DeepEqual(unreleased, captured) {
			return ready, fmt.Errorf("ready checkpoint differs from its completed capture")
		}
		return ready, e.verifyCheckpoint(ctx, record, id, relative, ready)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Checkpoint{}, err
	}
	if err := e.reconcileRejectedCheckpoint(record, id, relative); err != nil {
		return Checkpoint{}, err
	}
	if err := e.reconcileAbortedCheckpoint(record, id, relative); err != nil {
		return Checkpoint{}, err
	}
	captured, err := e.readCheckpoint(relative, "capture-result.json")
	if err != nil {
		return Checkpoint{}, fmt.Errorf("checkpoint capture is unfinished or uncertain; refusing a second capture: %w", err)
	}
	if captured.ReleasedAt != "" {
		return captured, fmt.Errorf("unfinished checkpoint has invalid source-release metadata")
	}
	if err := e.verifyCheckpoint(ctx, record, id, relative, captured); err != nil {
		return captured, err
	}
	released, err := e.recoverCheckpointSource(relative, record, id, captured)
	if err != nil {
		return captured, err
	}
	captured.ReleasedAt = released
	return captured, e.publishCheckpoint(relative, "checkpoint.json", captured)
}

func (e *Engine) recoverCheckpointSource(relative string, record Record, id string, captured Checkpoint) (_ string, resultErr error) {
	if captured.SourceStopped {
		return e.recoverStoppedCheckpoint(relative, record, id, captured)
	}
	var config captureGuardConfig
	if err := e.readCheckpointJSON(relative+"/guard/guard-intent.json", &config); err != nil {
		return "", err
	}
	if config.Token != id || config.Source.ID != record.ID || config.Source.Owner != record.Owner ||
		config.Source.RuntimeID != record.RuntimeID || config.Source.ConfigSHA256 != record.ConfigSHA256 ||
		config.Source.BootID != record.BootID || config.Source.RuntimeSHA256 != record.RuntimeSHA256 ||
		config.PID != captured.SourcePID || config.Root != e.config.Root {
		return "", fmt.Errorf("checkpoint recovery guard identity differs")
	}
	captureData, err := json.Marshal(captured)
	if err != nil {
		return "", err
	}
	var recovered checkpointRecovery
	err = e.readCheckpointJSON(relative+"/checkpoint-reconciled.json", &recovered)
	if err == nil {
		if recovered.Version != 1 || !reflect.DeepEqual(recovered.Config, config) ||
			recovered.CaptureSHA256 != sum(captureData) || recovered.ReleasedAt == "" ||
			(recovered.Status != "original_source_thawed" && recovered.Status != "source_already_running") {
			return "", fmt.Errorf("checkpoint recovery record differs from its original capture")
		}
		return recovered.ReleasedAt, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	// A completed guard outcome proves the old helper will perform no later
	// freezer write. Its absence is not permission to race a surviving helper.
	var outcomeData []byte
	for _, name := range []string{"guard-outcome.json", "guard-controller-outcome.json"} {
		outcomeData, err = e.root.ReadFile(relative + "/guard/" + name)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	if err != nil {
		return "", fmt.Errorf("checkpoint source guard outcome is unresolved: %w", err)
	}
	var outcome struct {
		Config captureGuardConfig `json:"config"`
		Passed bool               `json:"passed"`
		Status string             `json:"status"`
	}
	if json.Unmarshal(outcomeData, &outcome) != nil || !outcome.Passed || !reflect.DeepEqual(outcome.Config, config) {
		return "", fmt.Errorf("checkpoint guard outcome is not bound to its exact operation")
	}
	switch outcome.Status {
	case "source_running_guard_disarmed", "original_source_thawed", "source_already_running",
		"original_source_held_for_capture_reconciliation":
	default:
		return "", fmt.Errorf("checkpoint guard outcome is not a recognized source state")
	}
	anchors := captureGuardAnchors{}
	defer func() { resultErr = errors.Join(resultErr, anchors.close()) }()
	anchors.group, err = captureGuardOpen(cgroupRoot + "/" + record.RuntimeID)
	if err != nil {
		return "", err
	}
	anchors.evidence, err = e.root.Open(relative + "/guard")
	if err != nil {
		return "", err
	}
	anchors.root, err = e.root.Open(".")
	if err != nil {
		return "", err
	}
	fd, err := unix.PidfdOpen(config.PID, 0)
	if err != nil {
		return "", err
	}
	anchors.pid = os.NewFile(uintptr(fd), "checkpoint recovery original pidfd")
	if err := captureGuardValidate(config, anchors); err != nil {
		return "", err
	}
	if _, err := captureGuardHold(config, anchors); err != nil {
		return "", err
	}
	if err := captureGuardCaptureReturned(config, anchors); err != nil {
		return "", err
	}
	status, err := captureGuardRecover(config, anchors)
	if err != nil {
		return "", err
	}
	recovered = checkpointRecovery{Version: 1, Config: config, CaptureSHA256: sum(captureData),
		Status: status, ReleasedAt: now()}
	return recovered.ReleasedAt, e.publishCheckpoint(relative, "checkpoint-reconciled.json", recovered)
}

func (e *Engine) checkpointFiles(ctx context.Context, relative string) ([]CheckpointFile, error) {
	names := []string{"criu.conf", "filesystem.json", "source-state.json", "sysbox-state.json"}
	images, err := e.root.Open(relative + "/images")
	if err != nil {
		return nil, err
	}
	entries, readErr := images.ReadDir(65537)
	closeErr := images.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return nil, errors.Join(readErr, closeErr)
	}
	if closeErr != nil || len(entries) == 0 || len(entries) > 65536 {
		return nil, errors.Join(closeErr, fmt.Errorf("invalid checkpoint image count"))
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeType != 0 {
			return nil, fmt.Errorf("checkpoint image is not a regular independent file")
		}
		names = append(names, "images/"+entry.Name())
	}
	// ReadDir returns sorted entries for an ordinary File only when requested
	// through os.ReadDir. Sort explicitly for stable manifest comparisons.
	sort.Strings(names)
	result := make([]CheckpointFile, 0, len(names))
	root, err := e.root.Open(".")
	if err != nil {
		return nil, err
	}
	defer root.Close()
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		fd, err := unix.Openat2(int(root.Fd()), relative+"/"+name, &unix.OpenHow{
			Flags: unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOATIME, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS})
		if err != nil {
			return nil, err
		}
		file := os.NewFile(uintptr(fd), name)
		var stat unix.Stat_t
		statErr := unix.Fstat(fd, &stat)
		if statErr != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Nlink != 1 {
			file.Close()
			return nil, fmt.Errorf("checkpoint file changed its regular-file identity: %s: %v", name, statErr)
		}
		hash := sha256.New()
		_, readErr := io.Copy(hash, checkpointReader{ctx, file})
		closeErr := file.Close()
		if err := errors.Join(readErr, closeErr); err != nil {
			return nil, err
		}
		result = append(result, CheckpointFile{Name: name, SHA256: hex.EncodeToString(hash.Sum(nil)), Bytes: stat.Size})
	}
	return result, nil
}

type checkpointReader struct {
	context context.Context
	reader  io.Reader
}

func (r checkpointReader) Read(data []byte) (int, error) {
	if err := r.context.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(data)
}
