//go:build linux

package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"factorybox/internal/sysboxcapture"
)

// RecoveryRequest names a fully committed pause in an exclusively owned
// persistent store. The caller must have fenced the old worker before use.
// SourceRoot supplies files only; no old runtime, PID, cgroup or mount is adopted.
type RecoveryRequest struct {
	SourceRoot       string `json:"source_root"`
	SourceID         string `json:"source_id"`
	SnapshotID       string `json:"snapshot_id"`
	SourceOwner      string `json:"source_owner"`
	CheckpointSHA256 string `json:"checkpoint_sha256"`
}

type RecoveryOrigin struct {
	Request    RecoveryRequest `json:"request"`
	ImportedAt string          `json:"imported_at"`
}

func (e *Engine) validateRecoveryRequest(request RecoveryRequest, targetID string) error {
	root := request.SourceRoot
	if !filepath.IsAbs(root) || root == "/" || filepath.Clean(root) != root || strings.ContainsAny(root, "\n,:\\") ||
		root == e.config.Root || strings.HasPrefix(root, e.config.Root+"/") || strings.HasPrefix(e.config.Root, root+"/") ||
		!sandboxID.MatchString(request.SourceID) || !sandboxID.MatchString(targetID) || request.SourceID == targetID ||
		!tokenID.MatchString(request.SnapshotID) || !tokenID.MatchString(request.SourceOwner) ||
		!digest.MatchString(request.CheckpointSHA256) {
		return fmt.Errorf("recovery requires disjoint controller roots, distinct IDs and exact saved pause identities")
	}
	return nil
}

func forkInputGeneration(intent forkIntent) string {
	if intent.Recovery != nil {
		return "forks/" + intent.TargetID + "/recovery/input"
	}
	generation, _ := checkpointPath(intent.Origin.SourceID, intent.Origin.CheckpointID)
	return generation
}

// RecoverPaused restores a completed persisted pause into a new sandbox in a
// fresh controller. It never opens old runtime state or invokes a lifecycle
// command for the source. Its immutable target ID is the idempotency key. An
// interrupted import or uncertain restore is retained, never blindly repeated.
func (e *Engine) RecoverPaused(ctx context.Context, request RecoveryRequest, targetID string) (View, error) {
	if err := e.validateRecoveryRequest(request, targetID); err != nil {
		return View{}, err
	}
	if !digest.MatchString(e.config.CriuSHA256) || canonical(e.config.ManagerRoot) != nil {
		return View{}, fmt.Errorf("recovery requires pinned CRIU and a canonical manager root")
	}
	lock, err := e.lock()
	if err != nil {
		return View{}, err
	}
	defer lock.Close()
	operation := "forks/" + targetID
	object, _ := objectPath(targetID)
	intent, err := e.readForkIntent(operation, request.SourceID, request.SnapshotID, targetID)
	if err == nil {
		if intent.Recovery == nil || !reflect.DeepEqual(intent.Recovery.Request, request) {
			return View{}, fmt.Errorf("target belongs to a different recovery request")
		}
		return e.finishFork(ctx, lock, operation, object, intent, false)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return View{}, err
	}
	for _, path := range []string{operation, object} {
		if _, err := e.root.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			return View{}, fmt.Errorf("recovery target is already owned or its import is incomplete: %s: %v", path, err)
		}
	}
	if err := e.admitStorageWrite(); err != nil {
		return View{}, err
	}
	intent, err = e.importPaused(ctx, lock, request, targetID)
	if err != nil {
		return View{}, err
	}
	// The intent is the publication point. All imported RAM, filesystem and
	// metadata have already been independently verified and synced.
	if err := e.publishCheckpoint(operation, "intent.json", intent); err != nil {
		return View{}, err
	}
	parent, err := e.root.Open("forks")
	if err != nil {
		return View{}, err
	}
	if err := errors.Join(parent.Sync(), parent.Close()); err != nil {
		return View{}, err
	}
	return e.finishFork(ctx, lock, operation, object, intent, true)
}

func (e *Engine) importPaused(ctx context.Context, lock *os.File, request RecoveryRequest, targetID string) (forkIntent, error) {
	var intent forkIntent
	if err := canonical(request.SourceRoot); err != nil {
		return intent, err
	}
	if err := privateDirectory(request.SourceRoot); err != nil {
		return intent, err
	}
	root, err := os.OpenRoot(request.SourceRoot)
	if err != nil {
		return intent, err
	}
	defer root.Close()
	oldConfig := e.config
	oldConfig.Root = request.SourceRoot
	old := &Engine{config: oldConfig, boot: e.boot, root: root}
	// A read-only view avoids Open's directory initialization. The source
	// lock serializes cooperating file readers/writers; worker fencing is a
	// separate prerequisite, not inferred from this lock or absent old PIDs.
	oldLock, err := old.recoveryLock(request)
	if err != nil {
		return intent, err
	}
	defer oldLock.Close()
	source, err := old.read(request.SourceID)
	if err != nil {
		return intent, err
	}
	if source.SharedSource != nil {
		return intent, sysboxcapture.ErrSharedSourceCopy
	}
	if source.Network != nil {
		return intent, fmt.Errorf("outbound network requires a separate worker recovery contract")
	}
	if source.Owner != request.SourceOwner || source.Phase != "paused" || source.Pause == nil ||
		source.Pause.SnapshotID != request.SnapshotID || source.Pause.CompletedAt == "" ||
		source.Pause.Error != "" || source.Pause.RejectedAt != "" || source.Pause.Resume != nil ||
		source.Pause.After.Anon != 0 || source.Pause.Snapshot.ResidentBytesAfter != 0 {
		return intent, fmt.Errorf("source is not the exact completed, released and unresumed pause")
	}
	if source.Pause.CheckpointSHA256 != "" && source.Pause.CheckpointSHA256 != request.CheckpointSHA256 {
		return intent, fmt.Errorf("recovery checkpoint differs from the committed pause receipt")
	}
	oldGeneration, _ := checkpointPath(source.ID, request.SnapshotID)
	saved, err := old.publishedCheckpointMetadata(ctx, source, request.SnapshotID, oldGeneration)
	if err != nil {
		return intent, err
	}
	identity, err := json.Marshal(saved)
	if err != nil || sum(identity) != request.CheckpointSHA256 || !saved.SourceStopped {
		return intent, fmt.Errorf("saved pause differs from its requested complete checkpoint: %v", err)
	}
	baseVerifier, err := old.recoveryBaseVerifier(source, saved.Filesystem)
	if err != nil {
		return intent, err
	}
	operation := "forks/" + targetID
	proposal := forkIntent{TargetID: targetID, Checkpoint: saved,
		Recovery: &RecoveryOrigin{Request: request}}
	if err := e.admitWorkerMetadata(ctx, workerMetadataDelta{Fork: &proposal}); err != nil {
		return intent, err
	}
	prepared, err := e.beginRecoveryImport(operation, request, targetID, source, saved)
	if err != nil {
		return intent, err
	}
	if err := e.root.Mkdir(operation+"/recovery", 0700); err != nil {
		return intent, err
	}
	generation := operation + "/recovery/input"
	filesystem, err := sysboxcapture.RecoverFilesystemSnapshotWithBaseVerifier(ctx,
		old.config.Root+"/"+oldGeneration, e.config.Root+"/"+generation, saved.Filesystem, baseVerifier)
	if err != nil {
		return intent, err
	}
	if err := old.copyForkImagesTo(ctx, oldLock, oldGeneration, e, lock, generation); err != nil {
		return intent, err
	}
	blobs := map[string][]byte{}
	for _, name := range []string{"source-state.json", "sysbox-state.json", "criu.conf"} {
		blobs[name], err = old.root.ReadFile(oldGeneration + "/" + name)
		if err != nil {
			return intent, err
		}
	}
	blobs["filesystem.json"], err = json.Marshal(filesystem)
	if err != nil {
		return intent, err
	}
	for name, data := range blobs {
		if err := e.writeForkFile(generation, name, data); err != nil {
			return intent, err
		}
	}
	files, err := e.checkpointFiles(ctx, generation)
	if err != nil {
		return intent, err
	}
	if len(files) != len(saved.Files) {
		return intent, fmt.Errorf("recovered checkpoint omitted saved files")
	}
	for index, file := range files {
		if file.Name != "filesystem.json" && file != saved.Files[index] {
			return intent, fmt.Errorf("recovered RAM or opaque metadata changed: %s", file.Name)
		}
	}
	saved.Files, saved.Filesystem = files, filesystem
	original := checkpointIntent{Version: 1, ID: saved.ID, SourceID: source.ID, Owner: source.Owner,
		RuntimeID: source.RuntimeID, BootID: source.BootID, ConfigSHA256: source.ConfigSHA256,
		RuntimeSHA256: source.RuntimeSHA256, CriuSHA256: e.config.CriuSHA256,
		ManagerRoot: e.config.ManagerRoot, RequestedAt: now(), StopSource: true}
	captured := saved
	captured.ReleasedAt = ""
	for _, entry := range []struct {
		name  string
		value any
	}{
		{"checkpoint-intent.json", original}, {"capture-result.json", captured}, {"checkpoint.json", saved},
	} {
		if err := e.publishCheckpoint(generation, entry.name, entry.value); err != nil {
			return intent, err
		}
	}
	parent, err := e.root.Open(operation + "/recovery")
	if err != nil {
		return intent, err
	}
	if err := errors.Join(parent.Sync(), parent.Close()); err != nil {
		return intent, err
	}
	if err := e.verifyCheckpoint(ctx, source, saved.ID, generation, saved); err != nil {
		return intent, err
	}
	oci, err := old.root.ReadFile("objects/" + source.ID + "/config.json")
	if err != nil || sum(oci) != source.ConfigSHA256 {
		return intent, fmt.Errorf("saved source OCI changed during import: %v", err)
	}
	oci, err = forkOCI(oci, e.config.Root+"/"+operation+"/materialized/filesystem")
	if err != nil {
		return intent, err
	}
	identity, err = json.Marshal(saved)
	if err != nil {
		return intent, err
	}
	return forkIntent{Version: 1, TargetID: targetID, Owner: prepared.Owner,
		BootID: e.boot, RuntimeSHA256: e.config.RuntimeSHA256, CriuSHA256: e.config.CriuSHA256,
		ManagerRoot: e.config.ManagerRoot, Source: source, Checkpoint: saved, OCI: oci,
		State: blobs["source-state.json"], FsState: blobs["sysbox-state.json"], CriuConfig: blobs["criu.conf"],
		Origin:   ForkOrigin{SourceID: source.ID, CheckpointID: saved.ID, CheckpointSHA256: sum(identity), RequestedAt: now()},
		Recovery: &RecoveryOrigin{Request: request, ImportedAt: now()}}, nil
}
