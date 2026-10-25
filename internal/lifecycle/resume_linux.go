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

	"factorybox/internal/fdstore"
	"factorybox/internal/sysboxcapture"
	"golang.org/x/sys/unix"
)

type resumeRequest struct {
	Version           int    `json:"version"`
	SandboxID         string `json:"sandbox_id"`
	Owner             string `json:"owner"`
	Root              string `json:"root"`
	Filesystem        string `json:"filesystem"`
	Images            string `json:"images"`
	ManagerRoot       string `json:"manager_root"`
	SnapshotID        string `json:"snapshot_id"`
	ConfigSHA256      string `json:"config_sha256"`
	RuntimeCreated    string `json:"runtime_created"`
	HoldForActivation bool   `json:"hold_for_activation,omitempty"`
}

type resumeIntent struct {
	Version       int           `json:"version"`
	Request       resumeRequest `json:"request"`
	SnapshotSHA   string        `json:"snapshot_sha256"`
	CriuSHA256    string        `json:"criu_sha256"`
	RuntimeSHA256 string        `json:"runtime_sha256"`
}

func (e *Engine) resumeRequest(record Record) resumeRequest {
	snapshot := record.Pause.SnapshotID
	return resumeRequest{Version: 1, SandboxID: record.ID, Owner: record.Owner, Root: e.config.Root,
		Filesystem: record.InputBundle, Images: e.config.Root + "/objects/" + record.ID + "/resumes/" + snapshot + "/images",
		ManagerRoot: e.config.ManagerRoot, SnapshotID: snapshot, ConfigSHA256: record.ConfigSHA256, RuntimeCreated: record.RuntimeCreated, HoldForActivation: true}
}

func (e *Engine) Resume(ctx context.Context, id, snapshot string) (View, error) {
	lock, err := e.lock()
	if err != nil {
		return View{}, err
	}
	defer lock.Close()
	if !tokenID.MatchString(snapshot) {
		return View{}, fmt.Errorf("resume requires its immutable pause snapshot ID")
	}
	record, err := e.read(id)
	if err != nil {
		return View{}, err
	}
	if record.Pause == nil || record.Pause.SnapshotID != snapshot || record.Pause.CompletedAt == "" {
		return View{Record: record}, fmt.Errorf("resume does not match a completed pause")
	}
	if record.Phase == "running" && record.Pause.Resume != nil && record.Pause.Resume.CompletedAt != "" {
		_, state, err := e.resumeCompleted(ctx, lock, record)
		if err == nil {
			err = e.releaseSharedHistory(record)
		}
		return View{Record: record, Runtime: state}, err
	}
	if record.Phase == "paused" {
		if err := e.admitSandboxMetadata(record, true); err != nil {
			return View{Record: record}, err
		}
		active := record
		active.Phase = "resuming"
		if err := e.admitWorkerMetadata(ctx, workerMetadataDelta{Record: &active}); err != nil {
			return View{Record: record}, err
		}
		if err := e.admitStorageWrite(); err != nil {
			return View{Record: record}, err
		}
		if err := e.requireEmptyCgroup(record); err != nil {
			return View{Record: record}, err
		}
		if record.SharedSource != nil {
			if err := fdstore.VerifyRestore(e.config.FDStoreSocket, record.Owner+":"+record.Pause.SnapshotID); err != nil {
				return View{Record: record}, err
			}
		}
		if record.Network != nil {
			if err := record.Network.RequirePeerAbsent(); err != nil {
				return View{Record: record}, err
			}
		}
		record.Phase = "resuming"
		record.Pause.Resume = &Operation{RequestedAt: now()}
		if err := e.write(record); err != nil {
			return View{Record: record}, err
		}
	}
	if record.Phase != "resuming" || record.Pause.Resume == nil {
		return View{Record: record}, fmt.Errorf("resume lacks its persisted transition")
	}
	request := e.resumeRequest(record)
	relative := "objects/" + id + "/resumes/" + snapshot
	if !record.Pause.Resume.Attempted {
		if err := e.admitStorageWrite(); err != nil {
			return View{Record: record}, err
		}
		if err := e.requireEmptyCgroup(record); err != nil {
			return View{Record: record}, err
		}
		if err := e.prepareResume(ctx, lock, record, relative, request); err != nil {
			return View{Record: record}, err
		}
		if record.Network != nil {
			if err := record.Network.PrepareRestore(); err != nil {
				return View{Record: record}, err
			}
		}
		// Only empty, owned kernel groups change here. Preparation is
		// idempotent while no runtime restore has been attempted.
		if err := e.prepareEmptyResumeCgroup(record); err != nil {
			return View{Record: record}, err
		}
		record.Pause.Resume.Attempted = true
		if err := e.write(record); err != nil {
			return View{Record: record}, err
		}
		if err := e.resumeRuntime(ctx, lock, record, request); err != nil {
			record.Pause.Resume.Error = sandboxErrorSummary(err.Error())
			return View{Record: record}, errors.Join(err, e.write(record))
		}
	}
	result, state, err := e.resumeCompleted(ctx, lock, record)
	if err != nil {
		return View{Record: record, Runtime: state}, err
	}
	record.Pause.Resume.CompletedAt, record.Pause.Resume.Error, record.Phase = result.CompletedAt, "", "running"
	if err := e.write(record); err != nil {
		return View{Record: record, Runtime: state}, err
	}
	return View{Record: record, Runtime: state}, e.releaseSharedHistory(record)
}

func (e *Engine) prepareResume(ctx context.Context, lock *os.File, record Record, relative string, request resumeRequest) error {
	generation, _ := checkpointPath(record.ID, record.Pause.SnapshotID)
	saved, err := e.readCheckpoint(generation, "checkpoint.json")
	if err != nil {
		return err
	}
	if !saved.SourceStopped {
		return fmt.Errorf("resume checkpoint did not stop the original source")
	}
	if saved.Filesystem == nil {
		return fmt.Errorf("resume checkpoint has no filesystem manifest")
	}
	if !reflect.DeepEqual(saved.Filesystem.SharedSource, record.SharedSource) {
		return fmt.Errorf("resume shared source differs from its original admission")
	}
	if record.SharedSource != nil {
		if saved.Filesystem.FDStoreKey != record.Owner+":"+record.Pause.SnapshotID {
			return fmt.Errorf("shared-source descriptor generation differs")
		}
		if err := sysboxcapture.VerifyRetainedFiles(e.config.FDStoreSocket, saved.Filesystem.FDStoreKey, saved.Filesystem.RetainedFiles); err != nil {
			return err
		}
	}
	if err := e.verifyCheckpoint(ctx, record, saved.ID, generation, saved); err != nil {
		return err
	}
	if err := sysboxcapture.VerifyLiveFilesystemSnapshot(ctx, saved.Filesystem); err != nil {
		return err
	}
	digestData, err := json.Marshal(saved)
	if err != nil {
		return err
	}
	intent := resumeIntent{Version: 1, Request: request, SnapshotSHA: sum(digestData),
		CriuSHA256: e.config.CriuSHA256, RuntimeSHA256: e.config.RuntimeSHA256}
	parent := filepath.Dir(relative)
	if err := e.root.Mkdir(parent, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	var existing resumeIntent
	err = e.readCheckpointJSON(relative+"/resume-intent.json", &existing)
	if err == nil {
		if !reflect.DeepEqual(existing, intent) {
			return fmt.Errorf("resume preparation differs from its original intent")
		}
		var prepared []CheckpointFile
		if err := e.readCheckpointJSON(relative+"/resume-prepared.json", &prepared); err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return err
			}
			// No restore was attempted yet. A lost publication can leave all
			// complete input files without their preparation receipt. Verify
			// against the immutable pause generation and sync them again before
			// publishing. Never overwrite partial or damaged backing here.
			files, err := e.checkpointFiles(ctx, relative)
			if err != nil || !reflect.DeepEqual(files, saved.Files) {
				return fmt.Errorf("resume backing remains incomplete; refusing overwrite: %v", err)
			}
			if err := e.syncResumeBacking(ctx, relative, files); err != nil {
				return err
			}
			return e.publishCheckpoint(relative, "resume-prepared.json", files)
		}
		actual, err := e.checkpointFiles(ctx, relative)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(actual, prepared) || !reflect.DeepEqual(prepared, saved.Files) {
			return fmt.Errorf("prepared resume images changed")
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := e.root.Mkdir(relative, 0700); err != nil {
		return fmt.Errorf("resume preparation already exists or is uncertain: %w", err)
	}
	if err := e.publishCheckpoint(relative, "resume-intent.json", intent); err != nil {
		return err
	}
	directory, err := e.root.Open(parent)
	if err != nil {
		return err
	}
	if err := errors.Join(directory.Sync(), directory.Close()); err != nil {
		return err
	}
	if err := e.copyForkImages(ctx, lock, generation, relative); err != nil {
		return err
	}
	for _, name := range []string{"filesystem.json", "source-state.json", "sysbox-state.json", "criu.conf"} {
		data, err := e.root.ReadFile(generation + "/" + name)
		if err != nil {
			return err
		}
		if err := e.writeForkFile(relative, name, data); err != nil {
			return err
		}
	}
	files, err := e.checkpointFiles(ctx, relative)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(files, saved.Files) {
		return fmt.Errorf("private resume images differ from their complete pause checkpoint")
	}
	return e.publishCheckpoint(relative, "resume-prepared.json", files)
}

func (e *Engine) syncResumeBacking(ctx context.Context, relative string, files []CheckpointFile) error {
	for _, saved := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		file, err := e.root.Open(relative + "/" + saved.Name)
		if err != nil {
			return err
		}
		if err := errors.Join(file.Sync(), file.Close()); err != nil {
			return err
		}
	}
	for _, path := range []string{relative + "/images", relative} {
		directory, err := e.root.Open(path)
		if err != nil {
			return err
		}
		if err := errors.Join(directory.Sync(), directory.Close()); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) resumeCompleted(ctx context.Context, lock *os.File, record Record) (restoreResult, *RuntimeState, error) {
	request := e.resumeRequest(record)
	relative := strings.TrimPrefix(filepath.Dir(request.Images), e.config.Root+"/")
	var intent resumeIntent
	if err := e.readCheckpointJSON(relative+"/resume-intent.json", &intent); err != nil {
		return restoreResult{}, nil, err
	}
	if intent.Version != 1 || !reflect.DeepEqual(intent.Request, request) ||
		intent.CriuSHA256 != e.config.CriuSHA256 || intent.RuntimeSHA256 != e.config.RuntimeSHA256 {
		return restoreResult{}, nil, fmt.Errorf("resume receipt recipe differs")
	}
	return e.operationCompleted(ctx, lock, record, request, relative+"/resume-result.json")
}

func (e *Engine) resumeRuntime(ctx context.Context, lock *os.File, record Record, request resumeRequest) error {
	if record.Phase != "resuming" || record.Pause == nil || record.Pause.Resume == nil || !record.Pause.Resume.Attempted {
		return fmt.Errorf("resume requires persisted attempted intent")
	}
	if !request.HoldForActivation {
		return fmt.Errorf("resume requires its pre-activation hold")
	}
	if err := e.requireEmptyCgroup(record); err != nil {
		return err
	}
	criu, err := openRuntime(e.config.Criu, e.config.CriuSHA256)
	if err != nil {
		return err
	}
	if err := criu.Close(); err != nil {
		return err
	}
	if err := sysboxcapture.CheckExecutableParents(e.config.Criu); err != nil {
		return err
	}
	data, err := json.Marshal(request)
	if err != nil {
		return err
	}
	fd, err := unix.MemfdCreate("factorybox-resume-request", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), "sealed resume request")
	defer file.Close()
	if _, err := file.Write(data); err != nil {
		return err
	}
	if _, err := file.Seek(0, 0); err != nil {
		return err
	}
	if _, err := unix.FcntlInt(file.Fd(), unix.F_ADD_SEALS, captureRequestSeals); err != nil {
		return err
	}
	relative := strings.TrimPrefix(filepath.Dir(request.Images), e.config.Root+"/")
	return e.heldRestore(ctx, lock, record, request, file, relative, "resume")
}
