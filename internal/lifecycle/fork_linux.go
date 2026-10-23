//go:build linux

package lifecycle

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"syscall"

	"factorybox/internal/sysboxcapture"
	"golang.org/x/sys/unix"
)

type ForkOrigin struct {
	SourceID         string `json:"source_id"`
	CheckpointID     string `json:"checkpoint_id"`
	CheckpointSHA256 string `json:"checkpoint_sha256"`
	RequestedAt      string `json:"requested_at"`
}

type forkIntent struct {
	Version       int             `json:"version"`
	TargetID      string          `json:"target_id"`
	Owner         string          `json:"owner"`
	BootID        string          `json:"boot_id"`
	RuntimeSHA256 string          `json:"runtime_sha256"`
	CriuSHA256    string          `json:"criu_sha256"`
	ManagerRoot   string          `json:"manager_root"`
	Origin        ForkOrigin      `json:"origin"`
	Source        Record          `json:"source"`
	Checkpoint    Checkpoint      `json:"checkpoint"`
	OCI           []byte          `json:"oci"`
	State         []byte          `json:"state"`
	FsState       []byte          `json:"fs_state"`
	CriuConfig    []byte          `json:"criu_config"`
	Recovery      *RecoveryOrigin `json:"recovery,omitempty"`
}

type forkPreparation struct {
	Version      int                               `json:"version"`
	Owner        string                            `json:"owner"`
	IntentSHA256 string                            `json:"intent_sha256"`
	Filesystem   *sysboxcapture.FilesystemSnapshot `json:"filesystem"`
	Files        []CheckpointFile                  `json:"files"`
	PreparedAt   string                            `json:"prepared_at"`
}

// Save the uncovered mountpoint before overlay mount. After a lost final
// deletion write, its exact identity proves that the original unmount finished.
// An absent mount alone cannot distinguish this directory from a replacement.
type forkOverlayOrigin struct {
	Version          int          `json:"version"`
	Owner            string       `json:"owner"`
	Rootfs           string       `json:"rootfs"`
	InputSHA256      string       `json:"input_sha256"`
	CheckpointSHA256 string       `json:"checkpoint_sha256"`
	Mountpoint       KernelObject `json:"mountpoint"`
}

func (origin forkOverlayOrigin) validate(owner, rootfs, inputSHA, checkpointSHA string) error {
	if origin.Version != 1 || origin.Owner != owner || origin.Rootfs != rootfs ||
		origin.InputSHA256 != inputSHA || origin.CheckpointSHA256 != checkpointSHA ||
		origin.Mountpoint.Device == 0 || origin.Mountpoint.Inode == 0 || origin.Mountpoint.MountID == 0 {
		return fmt.Errorf("fork overlay origin differs from its original request")
	}
	return nil
}

// Fork starts an independent target from an already published checkpoint.
// The immutable target ID is also the request's idempotency identity.
func (e *Engine) Fork(ctx context.Context, sourceID, snapshotID, targetID string) (View, error) {
	lock, err := e.lock()
	if err != nil {
		return View{}, err
	}
	defer lock.Close()
	object, err := objectPath(targetID)
	if err != nil {
		return View{}, err
	}
	if _, err := checkpointPath(sourceID, snapshotID); err != nil || sourceID == targetID {
		return View{}, errors.Join(err, fmt.Errorf("fork requires distinct source and target IDs"))
	}
	if !digest.MatchString(e.config.CriuSHA256) || canonical(e.config.ManagerRoot) != nil {
		return View{}, fmt.Errorf("fork requires pinned CRIU and a canonical manager root")
	}
	operation := "forks/" + targetID
	metadataVerified := false
	intent, err := e.readForkIntent(operation, sourceID, snapshotID, targetID)
	if errors.Is(err, os.ErrNotExist) {
		if _, exists := e.root.Lstat(operation); exists == nil {
			return View{}, fmt.Errorf("fork intent is incomplete; refusing to reuse its target identity")
		} else if !errors.Is(exists, os.ErrNotExist) {
			return View{}, exists
		}
		if _, exists := e.root.Lstat(object); exists == nil {
			return View{}, fmt.Errorf("fork target ID already belongs to another sandbox")
		} else if !errors.Is(exists, os.ErrNotExist) {
			return View{}, exists
		}
		if err := e.admitStorageWrite(); err != nil {
			return View{}, err
		}
		intent, err = e.newForkIntent(ctx, sourceID, snapshotID, targetID)
		metadataVerified = err == nil
		if err == nil {
			err = e.admitWorkerMetadata(ctx, workerMetadataDelta{Fork: &intent})
		}
		if err == nil {
			err = e.root.Mkdir(operation, 0700)
		}
		if err == nil {
			err = e.publishCheckpoint(operation, "intent.json", intent)
		}
		if err == nil {
			parent, openErr := e.root.Open("forks")
			if openErr != nil {
				err = openErr
			} else {
				err = errors.Join(parent.Sync(), parent.Close())
			}
		}
	}
	if err != nil {
		return View{}, err
	}
	if intent.Recovery != nil {
		return View{}, fmt.Errorf("target belongs to a persisted-pause recovery request")
	}
	return e.finishFork(ctx, lock, operation, object, intent, metadataVerified)
}

func (e *Engine) finishFork(ctx context.Context, lock *os.File, operation, object string, intent forkIntent, metadataVerified bool) (View, error) {
	record, err := e.read(intent.TargetID)
	if err == nil {
		if record.Owner != intent.Owner || !reflect.DeepEqual(record.Fork, &intent.Origin) ||
			record.InputSHA256 != sum(intent.OCI) {
			return View{Record: record}, fmt.Errorf("fork target belongs to a different persisted request")
		}
		return e.reconcileFork(ctx, lock, intent, record)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return View{}, err
	}
	if err := e.admitStorageWrite(); err != nil {
		return View{}, err
	}
	if err := e.prepareForkBacking(ctx, lock, operation, intent, metadataVerified); err != nil {
		return View{}, err
	}
	bundle := e.config.Root + "/" + operation + "/materialized/filesystem"
	if err := e.forkOverlay(bundle, operation, intent); err != nil {
		return View{}, err
	}
	record, err = e.prepareRecordContext(ctx, intent.TargetID, bundle, object, intent.OCI, intent.Owner, &intent.Origin)
	if err != nil {
		return View{Record: record}, err
	}
	return e.reconcileFork(ctx, lock, intent, record)
}

func (e *Engine) readForkIntent(operation, sourceID, snapshotID, targetID string) (forkIntent, error) {
	var intent forkIntent
	if err := e.readCheckpointJSON(operation+"/intent.json", &intent); err != nil {
		return intent, err
	}
	data, err := json.Marshal(intent.Checkpoint)
	if err != nil {
		return intent, err
	}
	if intent.Version != 1 || intent.TargetID != targetID || !tokenID.MatchString(intent.Owner) ||
		intent.BootID != e.boot || intent.RuntimeSHA256 != e.config.RuntimeSHA256 ||
		intent.CriuSHA256 != e.config.CriuSHA256 || intent.ManagerRoot != e.config.ManagerRoot ||
		intent.Origin.SourceID != sourceID || intent.Origin.CheckpointID != snapshotID ||
		intent.Origin.CheckpointSHA256 != sum(data) || intent.Origin.RequestedAt == "" ||
		intent.Source.ID != sourceID || intent.Checkpoint.SourceID != sourceID ||
		intent.Checkpoint.ID != snapshotID || intent.Checkpoint.ReleasedAt == "" ||
		intent.Source.Owner != intent.Checkpoint.Owner || intent.Source.RuntimeID != intent.Checkpoint.RuntimeID ||
		intent.Source.ConfigSHA256 != intent.Checkpoint.ConfigSHA256 ||
		intent.Source.BootID != intent.BootID || intent.Checkpoint.BootID != intent.BootID ||
		intent.Checkpoint.ManagerRoot != intent.ManagerRoot ||
		intent.Checkpoint.RuntimeSHA256 != intent.RuntimeSHA256 || intent.Checkpoint.CriuSHA256 != intent.CriuSHA256 ||
		len(intent.OCI) == 0 || len(intent.State) == 0 || len(intent.FsState) == 0 {
		return intent, fmt.Errorf("fork intent differs from its original request or pinned recipe")
	}
	if intent.Recovery != nil {
		if err := e.validateRecoveryRequest(intent.Recovery.Request, targetID); err != nil ||
			intent.Recovery.Request.SourceID != sourceID || intent.Recovery.Request.SnapshotID != snapshotID ||
			intent.Recovery.Request.SourceOwner != intent.Source.Owner || intent.Recovery.ImportedAt == "" ||
			!intent.Checkpoint.SourceStopped {
			return intent, fmt.Errorf("recovery intent differs from its file-only source: %v", err)
		}
	}
	for name, blob := range map[string][]byte{
		"source-state.json": intent.State, "sysbox-state.json": intent.FsState, "criu.conf": intent.CriuConfig,
	} {
		found := false
		for _, file := range intent.Checkpoint.Files {
			found = found || (file.Name == name && file.Bytes == int64(len(blob)) && file.SHA256 == sum(blob))
		}
		if !found {
			return intent, fmt.Errorf("fork intent changed saved metadata: %s", name)
		}
	}
	return intent, nil
}

func (e *Engine) newForkIntent(ctx context.Context, sourceID, snapshotID, targetID string) (forkIntent, error) {
	var intent forkIntent
	source, err := e.read(sourceID)
	if err != nil {
		return intent, err
	}
	generation, _ := checkpointPath(sourceID, snapshotID)
	if _, err := e.readCheckpoint(generation, "checkpoint.json"); err != nil {
		return intent, fmt.Errorf("fork requires an already published checkpoint: %w", err)
	}
	checkpoint, err := e.publishedCheckpointMetadata(ctx, source, snapshotID, generation)
	if err != nil {
		return intent, err
	}
	oci, err := e.root.ReadFile("objects/" + sourceID + "/config.json")
	if err != nil {
		return intent, err
	}
	bundle := e.config.Root + "/forks/" + targetID + "/materialized/filesystem"
	oci, err = forkOCI(oci, bundle)
	if err != nil {
		return intent, err
	}
	state, err := e.root.ReadFile(generation + "/source-state.json")
	if err != nil {
		return intent, err
	}
	fsState, err := e.root.ReadFile(generation + "/sysbox-state.json")
	if err != nil {
		return intent, err
	}
	criuConfig, err := e.root.ReadFile(generation + "/criu.conf")
	if err != nil {
		return intent, err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return intent, err
	}
	identity, err := json.Marshal(checkpoint)
	if err != nil {
		return intent, err
	}
	return forkIntent{Version: 1, TargetID: targetID, Owner: hex.EncodeToString(nonce[:]),
		BootID: e.boot, RuntimeSHA256: e.config.RuntimeSHA256, CriuSHA256: e.config.CriuSHA256,
		ManagerRoot: e.config.ManagerRoot, Source: source, Checkpoint: checkpoint, OCI: oci, State: state,
		FsState: fsState, CriuConfig: criuConfig, Origin: ForkOrigin{SourceID: sourceID, CheckpointID: snapshotID,
			CheckpointSHA256: sum(identity), RequestedAt: now()}}, nil
}

func forkOCI(data []byte, bundle string) ([]byte, error) {
	var spec map[string]json.RawMessage
	if err := json.Unmarshal(data, &spec); err != nil {
		return nil, err
	}
	var root map[string]any
	if err := json.Unmarshal(spec["root"], &root); err != nil || root == nil {
		return nil, fmt.Errorf("missing saved OCI root: %v", err)
	}
	root["path"] = bundle + "/rootfs"
	spec["root"], _ = json.Marshal(root)
	var annotations map[string]string
	if err := json.Unmarshal(spec["annotations"], &annotations); err != nil {
		return nil, err
	}
	delete(annotations, ownerAnnotation)
	delete(annotations, baseAnnotation)
	spec["annotations"], _ = json.Marshal(annotations)
	return json.Marshal(spec)
}

func (e *Engine) prepareForkBacking(ctx context.Context, lock *os.File, operation string, intent forkIntent, metadataVerified bool) error {
	materialized := operation + "/materialized"
	destination := e.config.Root + "/" + materialized
	expected, err := json.Marshal(intent)
	if err != nil {
		return err
	}
	var prepared forkPreparation
	err = e.readCheckpointJSON(operation+"/preparation.json", &prepared)
	if err == nil {
		if prepared.Version != 1 || prepared.Owner != intent.Owner ||
			prepared.IntentSHA256 != sum(expected) || prepared.PreparedAt == "" {
			return fmt.Errorf("fork preparation belongs to another intent")
		}
		files, err := e.checkpointFiles(ctx, materialized)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(files, prepared.Files) {
			return fmt.Errorf("prepared fork image or metadata changed")
		}
		return sysboxcapture.VerifyFilesystemSnapshot(ctx, destination, prepared.Filesystem)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err := e.root.Lstat(materialized); !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("fork preparation is incomplete or uncertain; refusing to overwrite its backing: %v", err)
	}
	generation := forkInputGeneration(intent)
	// A new intent's complete images and metadata were verified under this
	// same exclusive lock. Retry verifies them again. Materialization below
	// admits every source mount and verifies each independent target against
	// the complete saved content/metadata digest before runtime creation.
	// No persisted marker substitutes for fresh verification in a later call.
	if !metadataVerified {
		if err := e.verifyCheckpointMetadata(ctx, intent.Source, intent.Checkpoint.ID, generation, intent.Checkpoint); err != nil {
			return err
		}
	}
	filesystem, err := sysboxcapture.MaterializeFilesystemSnapshot(ctx,
		e.config.Root+"/"+generation, destination, intent.Checkpoint.Filesystem)
	if err != nil {
		return err
	}
	if err := e.copyForkImages(ctx, lock, generation, materialized); err != nil {
		return err
	}
	metadata, err := json.Marshal(filesystem)
	if err != nil {
		return err
	}
	for name, data := range map[string][]byte{
		"filesystem.json": metadata, "source-state.json": intent.State,
		"sysbox-state.json": intent.FsState, "criu.conf": intent.CriuConfig,
	} {
		if err := e.writeForkFile(materialized, name, data); err != nil {
			return err
		}
	}
	files, err := e.checkpointFiles(ctx, materialized)
	if err != nil {
		return err
	}
	originals := make(map[string]CheckpointFile, len(intent.Checkpoint.Files))
	for _, file := range intent.Checkpoint.Files {
		originals[file.Name] = file
	}
	for _, file := range files {
		if file.Name == "filesystem.json" {
			continue
		}
		original, found := originals[file.Name]
		if !found || file != original {
			return fmt.Errorf("private fork file differs from its checkpoint: %s", file.Name)
		}
	}
	if len(files) != len(intent.Checkpoint.Files) {
		return fmt.Errorf("fork image set differs from its complete checkpoint")
	}
	prepared = forkPreparation{Version: 1, Owner: intent.Owner, IntentSHA256: sum(expected),
		Filesystem: filesystem, Files: files, PreparedAt: now()}
	return e.publishCheckpoint(operation, "preparation.json", prepared)
}

func (e *Engine) copyForkImages(ctx context.Context, lock *os.File, generation, destination string) error {
	return e.copyForkImagesTo(ctx, lock, generation, e, lock, destination)
}

func (e *Engine) copyForkImagesTo(ctx context.Context, sourceLock *os.File, generation string, targetEngine *Engine, targetLock *os.File, destination string) error {
	source, err := e.root.Open(generation + "/images")
	if err != nil {
		return err
	}
	defer source.Close()
	if err := targetEngine.root.Mkdir(destination+"/images", 0700); err != nil {
		return err
	}
	target, err := targetEngine.root.Open(destination + "/images")
	if err != nil {
		return err
	}
	defer target.Close()
	command := exec.CommandContext(ctx, "cp", "-a", "--reflink=auto", "--", "/proc/self/fd/3/.", "/proc/self/fd/4/")
	command.ExtraFiles = []*os.File{source, target, sourceLock, targetLock}
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("copy private fork images: %w: %.4096s", err, output)
	}
	entries, err := target.ReadDir(-1)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		file, err := targetEngine.root.Open(destination + "/images/" + entry.Name())
		if err != nil {
			return err
		}
		if err := errors.Join(file.Sync(), file.Close()); err != nil {
			return err
		}
	}
	return target.Sync()
}

func (e *Engine) writeForkFile(relative, name string, data []byte) error {
	if filepath.Base(name) != name {
		return fmt.Errorf("fork file name is not local")
	}
	file, err := e.root.OpenFile(relative+"/"+name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	if err := errors.Join(writeErr, file.Sync(), file.Close()); err != nil {
		return err
	}
	directory, err := e.root.Open(relative)
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}

func (e *Engine) forkOverlay(bundle, operation string, intent forkIntent) error {
	for _, name := range []string{"work", "rootfs"} {
		if err := os.Mkdir(bundle+"/"+name, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
	}
	if _, err := os.Lstat(bundle + "/config.json"); errors.Is(err, os.ErrNotExist) {
		if err := e.writeForkFile(operation+"/materialized/filesystem", "config.json", intent.OCI); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if data, err := os.ReadFile(bundle + "/config.json"); err != nil || sum(data) != sum(intent.OCI) {
		return fmt.Errorf("prepared fork OCI input changed: %v", err)
	}
	mounted := sysboxcapture.CheckOverlayLayout(bundle)
	if mounted != nil && !errors.Is(mounted, sysboxcapture.ErrOverlayAbsent) {
		return fmt.Errorf("fork rootfs has an unexpected existing mount: %w", mounted)
	}
	var origin forkOverlayOrigin
	err := e.readCheckpointJSON(operation+"/overlay-origin.json", &origin)
	if errors.Is(err, os.ErrNotExist) {
		if mounted == nil {
			return fmt.Errorf("fork overlay exists without its original mountpoint intent")
		}
		base, err := directoryIdentity(bundle + "/rootfs")
		if err != nil {
			return err
		}
		origin = forkOverlayOrigin{Version: 1, Owner: intent.Owner, Rootfs: bundle + "/rootfs",
			InputSHA256: sum(intent.OCI), CheckpointSHA256: intent.Origin.CheckpointSHA256, Mountpoint: base}
		if err := e.publishCheckpoint(operation, "overlay-origin.json", origin); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if err := origin.validate(intent.Owner, bundle+"/rootfs", sum(intent.OCI), intent.Origin.CheckpointSHA256); err != nil {
		return err
	}
	// The target intent, backing manifest and uncovered mountpoint are durable.
	// A matching existing overlay can only be reused before runtime creation.
	if mounted == nil {
		return nil
	}
	base, err := directoryIdentity(bundle + "/rootfs")
	if err != nil || base != origin.Mountpoint {
		return fmt.Errorf("fork original mountpoint identity changed: %v", err)
	}
	entries, err := os.ReadDir(bundle + "/rootfs")
	if err != nil || len(entries) != 0 {
		return fmt.Errorf("fork rootfs mountpoint is not empty: %v", err)
	}
	// Sysbox remounts this upper layer with mapped lowers in the child's mount
	// namespace. Require the mount-local setting even when the worker's overlay
	// module enables exclusive indexed uppers by default.
	if err := unix.Mount("overlay", bundle+"/rootfs", "overlay", 0,
		"index=off,lowerdir="+bundle+"/lower,upperdir="+bundle+"/upper,workdir="+bundle+"/work"); err != nil {
		return err
	}
	return sysboxcapture.CheckOverlayLayout(bundle)
}

func (e *Engine) forkRestoreRequest(intent forkIntent, target Record) restoreRequest {
	return restoreRequest{Version: 1, SandboxID: target.ID, Owner: target.Owner,
		SourceID: intent.Source.RuntimeID, SourceRootfs: intent.Source.Rootfs, Root: e.config.Root,
		Filesystem: target.InputBundle, Images: filepath.Dir(target.InputBundle) + "/images",
		ManagerRoot: e.config.ManagerRoot, ConfigSHA256: target.ConfigSHA256, State: intent.State, FsState: intent.FsState, HoldForActivation: true}
}

func (e *Engine) reconcileFork(ctx context.Context, lock *os.File, intent forkIntent, record Record) (View, error) {
	if record.Start.CompletedAt != "" && record.Pause != nil {
		if record.Phase == "running" && record.Pause.Resume != nil && record.Pause.Resume.CompletedAt != "" {
			_, state, err := e.resumeCompleted(ctx, lock, record)
			return View{Record: record, Runtime: state}, err
		}
		if record.Phase == "paused" {
			state, err := e.state(ctx, record, lock)
			return View{Record: record, Runtime: state}, err
		}
	}
	if record.Phase != "starting" && record.Phase != "running" {
		return View{Record: record}, fmt.Errorf("cannot fork into a %s target identity", record.Phase)
	}
	request := e.forkRestoreRequest(intent, record)
	if !record.Start.Attempted {
		if err := e.admitStorageWrite(); err != nil {
			return View{Record: record}, err
		}
		if err := e.prepareCgroup(&record); err != nil {
			return View{Record: record}, err
		}
		record.Start.Attempted = true
		if err := e.write(record); err != nil {
			return View{Record: record}, err
		}
		if err := e.restore(ctx, lock, record, request); err != nil {
			record.Start.Error = sandboxErrorSummary(err.Error())
			return View{Record: record}, errors.Join(err, e.write(record))
		}
	}
	result, state, err := e.restoreCompleted(ctx, lock, record, request)
	if err != nil {
		return View{Record: record, Runtime: state}, err
	}
	if record.Phase == "running" && record.Start.CompletedAt == result.CompletedAt &&
		record.RuntimeCreated == state.Created && record.Start.Error == "" {
		return View{Record: record, Runtime: state}, nil
	}
	record.Phase, record.RuntimeCreated = "running", state.Created
	record.Start.CompletedAt, record.Start.Error = result.CompletedAt, ""
	return View{Record: record, Runtime: state}, e.write(record)
}

func (e *Engine) retireForkOverlay(record Record) error {
	if record.Fork != nil && (record.Phase != "deleting" || record.Delete == nil) {
		return fmt.Errorf("fork deletion lacks its persisted delete intent")
	}
	return e.retireForkOverlayBacking(record)
}

// The caller holds the operation lock and has either a delete intent or a
// durable epoch exit fence for a committed pause whose runtime is now absent.
func (e *Engine) retireForkOverlayBacking(record Record) error {
	if record.Fork == nil {
		return nil
	}
	if record.InputBundle != e.config.Root+"/forks/"+record.ID+"/materialized/filesystem" ||
		record.Rootfs != record.InputBundle+"/rootfs" {
		return fmt.Errorf("fork deletion overlay is outside its persisted target")
	}
	var origin forkOverlayOrigin
	if err := e.readCheckpointJSON("forks/"+record.ID+"/overlay-origin.json", &origin); err != nil {
		return fmt.Errorf("fork deletion original mountpoint is unconfirmed: %w", err)
	}
	if err := origin.validate(record.Owner, record.Rootfs, record.InputSHA256, record.Fork.CheckpointSHA256); err != nil {
		return err
	}
	if err := sysboxcapture.CheckOverlayLayout(record.InputBundle); err != nil {
		if errors.Is(err, sysboxcapture.ErrOverlayAbsent) {
			identity, err := directoryIdentity(record.Rootfs)
			if err != nil || identity != origin.Mountpoint {
				return fmt.Errorf("fork deletion unmounted rootfs identity changed: %v", err)
			}
			entries, err := os.ReadDir(record.Rootfs)
			if err != nil || len(entries) != 0 {
				return fmt.Errorf("fork deletion original mountpoint is not empty: %v", err)
			}
			return e.retireForkBaseAlias(record)
		}
		return err
	}
	info, err := os.Lstat(record.Rootfs)
	if err != nil {
		return err
	}
	stat := info.Sys().(*syscall.Stat_t)
	if uint64(stat.Dev) != record.RootfsDevice || stat.Ino != record.RootfsInode {
		return fmt.Errorf("fork deletion overlay identity changed")
	}
	identity, err := directoryIdentity(record.Rootfs)
	if err != nil || identity.MountID != record.RootfsMountID {
		return fmt.Errorf("fork deletion overlay mount identity changed: %v", err)
	}
	if err := unix.Unmount(record.Rootfs, 0); err != nil {
		return err
	}
	return e.retireForkBaseAlias(record)
}

func (e *Engine) retireForkBaseAlias(record Record) error {
	if record.Base == nil {
		return nil
	}
	return sysboxcapture.RetireImmutableAlias(record.InputBundle+"/lower", *record.Base)
}
