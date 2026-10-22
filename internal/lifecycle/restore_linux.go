//go:build linux

package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"factorybox/internal/sysboxcapture"
	"golang.org/x/sys/unix"
)

// The Sysbox adapter decodes its vendor config inside the pinned executable.
// The lifecycle controller passes only owned identities and saved opaque bytes.
type restoreRequest struct {
	Version           int    `json:"version"`
	SandboxID         string `json:"sandbox_id"`
	Owner             string `json:"owner"`
	SourceID          string `json:"source_id"`
	SourceRootfs      string `json:"source_rootfs"`
	Root              string `json:"root"`
	Filesystem        string `json:"filesystem"`
	Images            string `json:"images"`
	ManagerRoot       string `json:"manager_root"`
	ConfigSHA256      string `json:"config_sha256"`
	State             []byte `json:"state"`
	FsState           []byte `json:"fs_state"`
	HoldForActivation bool   `json:"hold_for_activation,omitempty"`
}

type restoreResult struct {
	Version       int    `json:"version"`
	Owner         string `json:"owner"`
	RuntimeID     string `json:"runtime_id"`
	RequestSHA256 string `json:"request_sha256"`
	PID           int    `json:"pid"`
	ProcessStart  string `json:"process_start"`
	Created       string `json:"created"`
	CompletedAt   string `json:"completed_at"`
}

func (e *Engine) restoreCompleted(ctx context.Context, lock *os.File, target Record, request restoreRequest) (restoreResult, *RuntimeState, error) {
	object, err := objectPath(target.ID)
	if err != nil {
		return restoreResult{}, nil, err
	}
	return e.operationCompleted(ctx, lock, target, request, object+"/restore-result.json")
}

func (e *Engine) operationCompleted(ctx context.Context, lock *os.File, target Record, request any, receipt string) (restoreResult, *RuntimeState, error) {
	var result restoreResult
	if err := e.readCheckpointJSON(receipt, &result); err != nil {
		return result, nil, fmt.Errorf("restore completion is unconfirmed; refusing another restore: %w", err)
	}
	data, err := json.Marshal(request)
	if err != nil {
		return result, nil, err
	}
	if result.Version != 1 || result.Owner != target.Owner || result.RuntimeID != target.RuntimeID ||
		result.RequestSHA256 != sum(data) || result.PID <= 1 || result.ProcessStart == "" || result.CompletedAt == "" {
		return result, nil, fmt.Errorf("restore receipt differs from its exact request")
	}
	state, err := e.state(ctx, target, lock)
	if err != nil || state == nil || state.Status != "running" || state.PID != result.PID {
		return result, state, errors.Join(err, fmt.Errorf("restore receipt does not identify its running original target"))
	}
	created, createdErr := time.Parse(time.RFC3339Nano, state.Created)
	recorded, recordedErr := time.Parse(time.RFC3339Nano, result.Created)
	start, startErr := processBirth(state.PID)
	if createdErr != nil || recordedErr != nil || !created.Equal(recorded) ||
		startErr != nil || start != result.ProcessStart {
		return result, state, fmt.Errorf("restored process birth or runtime creation differs from its receipt")
	}
	return result, state, nil
}

func (e *Engine) restore(ctx context.Context, lock *os.File, target Record, request restoreRequest) error {
	if target.Phase != "starting" || !target.Start.Attempted ||
		request.Root != e.config.Root || request.SandboxID != target.ID ||
		request.Owner != target.Owner || request.Filesystem != target.InputBundle ||
		request.Filesystem+"/rootfs" != target.Rootfs || request.ConfigSHA256 != target.ConfigSHA256 ||
		request.ManagerRoot != e.config.ManagerRoot || !digest.MatchString(e.config.CriuSHA256) {
		return fmt.Errorf("restore requires its exact persisted target creation")
	}
	if err := e.validateCgroup(target); err != nil {
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
	if len(data) > 2<<20 {
		return fmt.Errorf("restore request exceeds bound")
	}
	fd, err := unix.MemfdCreate("factorybox-restore-request", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), "factorybox sealed restore request")
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
	if _, err := e.commandFiles(ctx, lock, []*os.File{file}, "--criu", e.config.Criu,
		"factorybox-restore", target.RuntimeID); err != nil {
		return err
	}
	_, _, err = e.restoreCompleted(ctx, lock, target, request)
	return err
}
