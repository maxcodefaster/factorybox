//go:build linux

package lifecycle

import (
	"errors"
	"fmt"
	"os"
	"reflect"
)

// A sealed epoch admits only this file reader, never a lifecycle mutation. A
// complete pause remains recoverable if the old worker died during drain. The
// caller must still fence that worker independently before importing any files.
func (e *Engine) recoveryLock(request RecoveryRequest) (_ *os.File, resultErr error) {
	lock, err := e.lockRoot()
	if err != nil {
		return nil, err
	}
	defer func() {
		if resultErr != nil {
			lock.Close()
		}
	}()
	if err := e.epochRetirementFence(); err != nil {
		return nil, err
	}
	var intent workerBaseRelease
	err = e.readCheckpointJSON("worker-exit.json", &intent)
	if errors.Is(err, os.ErrNotExist) {
		if _, err := e.root.Lstat("worker-exit-complete.json"); !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("worker exit completion lacks its original intent: %v", err)
		}
		return lock, nil
	}
	if err != nil {
		return nil, err
	}
	source, err := e.read(request.SourceID)
	if err != nil {
		return nil, err
	}
	var persisted Record
	if err := e.readCheckpointJSON("objects/"+request.SourceID+"/record.json", &persisted); err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(persisted, source) {
		return nil, fmt.Errorf("worker exit recovery record changed during its read")
	}
	if err := intent.recoverySelection(e.config, e.boot, request, source); err != nil {
		return nil, err
	}
	// Reject a changed completion even though an interrupted drain needs no
	// completion. Its selected complete checkpoint is verified again by import.
	var complete workerBaseRelease
	err = e.readCheckpointJSON("worker-exit-complete.json", &complete)
	if err == nil {
		if complete.CompletedAt == "" {
			return nil, fmt.Errorf("worker exit completion is incomplete")
		}
		complete.CompletedAt = ""
		if !reflect.DeepEqual(complete, intent) {
			return nil, fmt.Errorf("worker exit completion differs from its fenced intent")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return lock, nil
}

func (intent workerBaseRelease) recoverySelection(config Config, boot string, request RecoveryRequest, source Record) error {
	if intent.Version != 1 || intent.Root != config.Root || intent.Root != request.SourceRoot ||
		intent.BootID != boot || intent.RuntimeSHA256 != config.RuntimeSHA256 ||
		intent.CriuSHA256 != config.CriuSHA256 || intent.ManagerRoot != config.ManagerRoot ||
		intent.RequestedAt == "" || intent.CompletedAt != "" {
		return fmt.Errorf("worker exit fence differs from the exact recovery epoch or pinned recipe")
	}
	seen := make(map[string]bool)
	found := false
	for _, paused := range intent.Paused {
		if seen[paused.ID] || committedExitPause(paused) != nil || paused.BootID != boot ||
			paused.RuntimeSHA256 != config.RuntimeSHA256 || !sandboxID.MatchString(paused.ID) ||
			!tokenID.MatchString(paused.Owner) || paused.RuntimeID != "fbx-"+paused.Owner {
			return fmt.Errorf("worker exit fence contains an invalid or duplicate paused identity")
		}
		seen[paused.ID] = true
		if paused.ID == request.SourceID {
			if paused.Owner != request.SourceOwner || paused.Pause.SnapshotID != request.SnapshotID ||
				paused.Pause.CheckpointSHA256 != request.CheckpointSHA256 || !reflect.DeepEqual(paused, source) {
				return fmt.Errorf("recovery differs from the exact pause retained by the worker exit fence")
			}
			found = true
		}
	}
	if !found {
		return fmt.Errorf("worker exit fence does not retain the requested pause")
	}
	return nil
}
