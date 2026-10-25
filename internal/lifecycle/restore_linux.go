//go:build linux

package lifecycle

import (
	"bytes"
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
	return e.operationCompletedStatus(ctx, lock, target, request, receipt, "running")
}

func (e *Engine) operationCompletedStatus(ctx context.Context, lock *os.File, target Record, request any, receipt, expectedStatus string) (restoreResult, *RuntimeState, error) {
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
	if err != nil || state == nil || state.Status != expectedStatus || state.PID != result.PID {
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
	if !request.HoldForActivation {
		return fmt.Errorf("restore requires its pre-activation hold")
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
	object, _ := objectPath(target.ID)
	if err := e.heldRestore(ctx, lock, target, request, file, object, "restore"); err != nil {
		return err
	}
	_, _, err = e.restoreCompleted(ctx, lock, target, request)
	return err
}

type restoreHoldReady struct {
	Version    int    `json:"version"`
	RequestSHA string `json:"request_sha256"`
	RuntimeID  string `json:"runtime_id"`
	PID        int    `json:"pid"`
	Start      string `json:"process_start"`
	Tracer     int    `json:"criu_tracer_pid"`
	Frozen     bool   `json:"frozen_before_resume"`
	CRIUOrigin bool   `json:"criu_returned_to_original_worker"`
}

// The independent restore worker owns this handshake through activation and
// durable publication. Its public controller may stop or die in the meantime.
func (e *Engine) heldRestoreWorker(ctx context.Context, lock *os.File, target Record, request any, sealed *os.File, relative, operation string) error {
	data, err := json.Marshal(request)
	if err != nil {
		return err
	}
	sockets, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return err
	}
	client, child := os.NewFile(uintptr(sockets[0]), "restore activation controller"), os.NewFile(uintptr(sockets[1]), "restore activation runtime")
	defer client.Close()
	defer child.Close()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := e.commandFiles(ctx, lock, []*os.File{sealed, child}, "--criu", e.config.Criu, "factorybox-"+operation, target.RuntimeID)
		done <- err
	}()
	waited := false
	defer func() {
		if !waited {
			cancel()
			<-done
		}
	}()
	packet, err := captureGuardRead(int(client.Fd()), 15000)
	if err != nil {
		return fmt.Errorf("restore did not enter its pre-resume hold: %w", err)
	}
	child.Close()
	var ready restoreHoldReady
	decoder := json.NewDecoder(bytes.NewReader(packet))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&ready); err != nil || ready.Version != 1 || ready.RequestSHA != sum(data) || ready.RuntimeID != target.RuntimeID || ready.PID <= 1 || ready.Tracer <= 1 || !ready.Frozen || !ready.CRIUOrigin {
		return fmt.Errorf("restore pre-resume identity differs: %v", err)
	}
	if err := e.validateCgroup(target); err != nil {
		return err
	}
	group, err := captureGuardOpen(cgroupRoot + "/" + target.RuntimeID)
	if err != nil {
		return err
	}
	defer group.Close()
	events, err := captureGuardEvents(group)
	if err != nil || events["frozen"] != 1 || events["populated"] != 1 {
		return fmt.Errorf("restored group is not held: %v", err)
	}
	birth, err := processBirth(ready.PID)
	if err != nil || birth != ready.Start {
		return fmt.Errorf("held restored task birth changed: %v", err)
	}
	pidfd, err := unix.PidfdOpen(ready.PID, 0)
	if err != nil {
		return err
	}
	defer unix.Close(pidfd)
	status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", ready.PID))
	if err != nil || !bytes.Contains(status, []byte(fmt.Sprintf("TracerPid:\t%d\n", ready.Tracer))) {
		return fmt.Errorf("restored task lost its original CRIU tracer: %v", err)
	}
	actual, err := os.Stat(fmt.Sprintf("/proc/%d/exe", ready.Tracer))
	expected, expectedErr := os.Stat(e.config.Criu)
	if err != nil || expectedErr != nil || !os.SameFile(actual, expected) {
		return fmt.Errorf("held restore tracer differs from pinned CRIU")
	}
	if _, err := client.Write([]byte("held:" + ready.RequestSHA)); err != nil {
		return err
	}
	err = <-done
	waited = true
	if err != nil {
		return err
	}
	result, state, err := e.operationCompletedStatus(ctx, lock, target, request, relative+"/"+operation+"-held.json", "paused")
	if err != nil {
		return err
	}
	if result.PID != ready.PID || result.ProcessStart != ready.Start || state.PID != ready.PID {
		return fmt.Errorf("held restore receipt changed its originally pinned process")
	}
	events, err = captureGuardEvents(group)
	if err != nil || events["frozen"] != 1 || events["populated"] != 1 {
		return fmt.Errorf("restore detached without retaining its exact freeze: %v", err)
	}
	if target.Network != nil {
		if err := target.Network.Configure(ctx, ready.PID, true); err != nil {
			return err
		}
	}
	if err := e.activateRestoredTimers(target, ready.PID); err != nil {
		return err
	}
	return e.publishCheckpoint(relative, operation+"-result.json", result)
}
