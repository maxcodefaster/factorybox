//go:build linux

package lifecycle

import (
	"context"
	"fmt"
	"math"
	"reflect"

	"factorybox/internal/sysboxcapture"
)

// RetainedCheckpointInspection is a fresh read observation, not a durable
// deletion authorization or a replacement for dependency/ownership admission.
type RetainedCheckpointInspection struct {
	Version             int                       `json:"version"`
	Request             RetainedCheckpointRequest `json:"request"`
	BootID              string                    `json:"boot_id"`
	Files               int                       `json:"files"`
	FileBytes           int64                     `json:"file_bytes"`
	FilesystemTrees     int                       `json:"filesystem_trees"`
	FullContentVerified bool                      `json:"full_content_verified"`
	VerifiedAt          string                    `json:"verified_at"`
}

func (e *Engine) inspectRetainedCheckpoint(ctx context.Context, request CheckpointRequest) (Checkpoint, error) {
	record, saved, relative, err := e.checkpointRequest(request)
	if err != nil {
		return Checkpoint{}, err
	}
	published, err := e.publishedCheckpointMetadata(ctx, record, request.SnapshotID, relative)
	if err != nil {
		return Checkpoint{}, err
	}
	if !reflect.DeepEqual(published, saved) {
		return Checkpoint{}, fmt.Errorf("retained checkpoint changed during inspection")
	}
	verify, err := e.recoveryBaseVerifier(record, saved.Filesystem)
	if err != nil {
		return Checkpoint{}, err
	}
	if err := sysboxcapture.VerifyRetainedFilesystemSnapshot(ctx, e.config.Root+"/"+relative, saved.Filesystem, verify); err != nil {
		return Checkpoint{}, err
	}
	return saved, nil
}

// InspectRetainedCheckpoint verifies complete RAM, metadata and filesystem
// backing after worker replacement. The caller uses the validated current
// worker's store lease; this operation never adopts an old runtime or mount.
func (e *Engine) InspectRetainedCheckpoint(ctx context.Context, request RetainedCheckpointRequest) (RetainedCheckpointInspection, error) {
	var report RetainedCheckpointInspection
	if err := ctx.Err(); err != nil {
		return report, err
	}
	if _, err := request.CheckpointRequest.path(); err != nil {
		return report, err
	}
	lock, err := e.lock()
	if err != nil {
		return report, err
	}
	defer lock.Close()
	old, err := e.openRetainedController(request.SourceRoot)
	if err != nil {
		return report, err
	}
	defer old.Close()
	oldLock, err := old.lockRoot()
	if err != nil {
		return report, err
	}
	defer oldLock.Close()
	saved, err := old.inspectRetainedCheckpoint(ctx, request.CheckpointRequest)
	if err != nil {
		return report, err
	}
	var bytes int64
	for _, file := range saved.Files {
		if file.Bytes < 0 || file.Bytes > math.MaxInt64-bytes {
			return report, fmt.Errorf("retained checkpoint byte inventory exceeds its numeric bound")
		}
		bytes += file.Bytes
	}
	return RetainedCheckpointInspection{Version: 1, Request: request, BootID: e.boot, Files: len(saved.Files),
		FileBytes: bytes, FilesystemTrees: len(saved.Filesystem.Roots), FullContentVerified: true, VerifiedAt: now()}, nil
}
