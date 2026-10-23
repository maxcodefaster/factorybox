//go:build linux

package lifecycle

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RetainedCheckpointRequest names an exact generation in an earlier lifetime
// of the current bounded store. It supplies no authority over old processes.
type RetainedCheckpointRequest struct {
	SourceRoot string `json:"source_root"`
	CheckpointRequest
}

func retainedControllerPath(config Config, source string) error {
	store := filepath.Dir(config.ManagerRoot)
	prefix := store + "/epochs/"
	epoch := strings.TrimSuffix(strings.TrimPrefix(source, prefix), "/controller")
	current := strings.TrimSuffix(strings.TrimPrefix(config.Root, prefix), "/controller")
	if config.BoundedStore == nil || !tokenID.MatchString(epoch) || !tokenID.MatchString(current) ||
		source != prefix+epoch+"/controller" || config.Root != prefix+current+"/controller" || epoch == current {
		return fmt.Errorf("retained selection requires a distinct exact controller epoch in the current bounded store")
	}
	return nil
}

func (e *Engine) openRetainedController(source string) (_ *Engine, resultErr error) {
	if err := retainedControllerPath(e.config, source); err != nil {
		return nil, err
	}
	parent := filepath.Dir(source)
	for _, path := range []string{parent, source} {
		if err := canonical(path); err != nil {
			return nil, err
		}
		if err := privateDirectory(path); err != nil {
			return nil, err
		}
		if err := e.boundedObject(path); err != nil {
			return nil, err
		}
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		return nil, err
	}
	epoch, readErr := readRecoveryStoreEpoch(root)
	if err := root.Close(); err != nil {
		return nil, err
	}
	if readErr != nil {
		return nil, readErr
	}
	config := e.config
	config.Root = source
	if _, err := validateRetainedStoreEpoch(config, e.boot, epoch); err != nil {
		return nil, err
	}
	retained, err := os.OpenRoot(source)
	if err != nil {
		return nil, err
	}
	old := &Engine{config: config, boot: e.boot, root: retained}
	defer func() {
		if resultErr != nil {
			old.Close()
		}
	}()
	// Never initialize a missing controller or lock in an uncertain old epoch.
	if _, err := old.root.Lstat(".lock"); err != nil {
		return nil, fmt.Errorf("retained controller has no existing operation lock: %w", err)
	}
	return old, nil
}

// ReleaseRetainedCheckpointSelection withdraws one named protection through
// the current worker. It does not remove any saved state, resume a source,
// release a committed pause, or reopen a fenced lifecycle. Both controllers
// stay locked in the same current-then-source order as persisted recovery.
func (e *Engine) ReleaseRetainedCheckpointSelection(ctx context.Context, request RetainedCheckpointRequest, id string) (CheckpointSelection, error) {
	if err := ctx.Err(); err != nil {
		return CheckpointSelection{}, err
	}
	if _, err := request.CheckpointRequest.path(); err != nil {
		return CheckpointSelection{}, err
	}
	lock, err := e.lock()
	if err != nil {
		return CheckpointSelection{}, err
	}
	defer lock.Close()
	old, err := e.openRetainedController(request.SourceRoot)
	if err != nil {
		return CheckpointSelection{}, err
	}
	defer old.Close()
	oldLock, err := old.lockRoot()
	if err != nil {
		return CheckpointSelection{}, err
	}
	defer oldLock.Close()
	return old.releaseCheckpointSelection(ctx, request.CheckpointRequest, id)
}
