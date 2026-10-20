//go:build linux

package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"factorybox/internal/sysboxcapture"
	"golang.org/x/sys/unix"
)

type captureRequest struct {
	Guard       captureGuardConfig `json:"guard"`
	Criu        string             `json:"criu"`
	CriuSHA256  string             `json:"criu_sha256"`
	ManagerRoot string             `json:"manager_root"`
}

const captureRequestSeals = unix.F_SEAL_WRITE | unix.F_SEAL_GROW | unix.F_SEAL_SHRINK | unix.F_SEAL_SEAL

func CaptureWorkerCommand(args []string) (bool, error) {
	if len(args) == 1 && args[0] == "_capture" {
		return true, captureWorkerServe()
	}
	return false, nil
}

func captureWorkerServe() (resultErr error) {
	requestFile := os.NewFile(3, "sealed capture request")
	defer requestFile.Close()
	seals, err := unix.FcntlInt(requestFile.Fd(), unix.F_GET_SEALS, 0)
	if err != nil || seals&captureRequestSeals != captureRequestSeals {
		return fmt.Errorf("capture request is not sealed: %v", err)
	}
	data, err := io.ReadAll(io.LimitReader(requestFile, (64<<10)+1))
	if err != nil || len(data) > 64<<10 {
		return fmt.Errorf("capture request exceeds bound or cannot be read: %v", err)
	}
	var request captureRequest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return err
	}
	anchors := captureGuardAnchors{os.NewFile(4, "capture original source cgroup"),
		os.NewFile(5, "capture original pidfd"), os.NewFile(6, "capture evidence"),
		os.NewFile(7, "capture original controller root")}
	defer anchors.close()
	if err := captureGuardValidate(request.Guard, anchors); err != nil {
		return err
	}
	events, err := captureGuardEvents(anchors.group)
	if err != nil || events["frozen"] != 1 || events["populated"] != 1 {
		return fmt.Errorf("capture worker requires the original frozen source: %v", err)
	}
	if !digest.MatchString(request.CriuSHA256) {
		return fmt.Errorf("capture requires an exact CRIU SHA-256")
	}
	criu, err := openRuntime(request.Criu, request.CriuSHA256)
	if err != nil {
		return err
	}
	defer criu.Close()
	if err := sysboxcapture.CheckExecutableParents(request.Criu); err != nil {
		return err
	}
	birth, err := processBirth(os.Getpid())
	if err != nil {
		return err
	}
	worker := captureWorkerIntent{Token: request.Guard.Token, PID: os.Getpid(), Start: birth,
		SourcePID: request.Guard.PID, CriuSHA256: request.CriuSHA256,
		Scope: "ordinary complete retained-source capture"}
	if request.Guard.CriuSHA256 != request.CriuSHA256 {
		return fmt.Errorf("capture worker CRIU identity differs from its guard")
	}
	if err := captureGuardWrite(anchors.evidence, "capture-worker-intent.json", worker); err != nil {
		return err
	}
	var captured sysboxcapture.Result
	defer func() {
		outcome := map[string]any{"token": request.Guard.Token, "passed": resultErr == nil,
			"capture": captured}
		if resultErr != nil {
			outcome["error"] = resultErr.Error()
		}
		resultErr = errors.Join(resultErr,
			captureGuardWrite(anchors.evidence, "capture-worker-outcome.json", outcome))
	}()
	network, err := sysboxcapture.BeginCaptureNetwork(request.Guard.PID, request.Guard.Start,
		filepath.Dir(request.Guard.Evidence))
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, network.Close()) }()
	threads, err := captureGuardThreads(request.Guard, anchors)
	if err != nil {
		return err
	}
	if err := network.TrackSockets(threads); err != nil {
		return err
	}
	captured, err = sysboxcapture.Capture(sysboxcapture.Request{
		RuntimeRoot: request.Guard.Root + "/runtime", RuntimeID: request.Guard.Source.RuntimeID,
		Owner: request.Guard.Source.Owner, Rootfs: request.Guard.Source.Rootfs,
		RuntimeCreated: request.Guard.Source.RuntimeCreated, PID: request.Guard.PID,
		Generation: filepath.Dir(request.Guard.Evidence), Criu: request.Criu,
		Bundle: request.Guard.Source.InputBundle, ManagerRoot: request.ManagerRoot,
		Base: request.Guard.Source.Base,
		ProcessCaptured: func(result sysboxcapture.Result) error {
			if result.SourcePID != request.Guard.PID || !result.CompleteWriter || !result.LeaveRunning {
				return fmt.Errorf("process completion lacks its retained original source")
			}
			if err := captureGuardCaptureReturned(request.Guard, anchors); err != nil {
				return err
			}
			if err := network.Rollback(); err != nil {
				return err
			}
			return captureGuardWrite(anchors.evidence, "capture-process-complete.json",
				captureProcessCompletion{Version: 1, Worker: worker,
					SourceStart: request.Guard.Start, SourceStateSHA: result.SourceStateSHA})
		},
	})
	if err != nil {
		// Capture() returns only after its CRIU subprocess has been waited.
		// Repair only the known network delta while the guarded original
		// remains frozen and untraced. A repaired network is not permission
		// to declare complete process rollback or to thaw the source.
		eventsBefore, _ := captureGuardEvents(anchors.group)
		_, held := captureGuardHold(request.Guard, anchors)
		validation := held
		if validation == nil {
			validation = captureGuardCaptureReturned(request.Guard, anchors)
		}
		var rollback error
		if validation == nil {
			rollback = network.Rollback()
		}
		receipt := captureGuardWrite(anchors.evidence, "capture-network-rollback.json", map[string]any{
			"token": request.Guard.Token, "source_pid": request.Guard.PID,
			"source_start": request.Guard.Start, "source_frozen": held == nil,
			"events_before": eventsBefore,
			"passed":        validation == nil && rollback == nil,
		})
		return errors.Join(err, validation, rollback, receipt)
	}
	if request.Guard.StopSource {
		return captureStopSource(request.Guard, anchors, captured)
	}
	return captureGuardCaptureReturned(request.Guard, anchors)
}

func captureStopSource(config captureGuardConfig, anchors captureGuardAnchors, captured sysboxcapture.Result) error {
	if !config.StopSource || captured.SourcePID != config.PID || !captured.CompleteWriter ||
		!captured.LeaveRunning || captured.Filesystem == nil {
		return fmt.Errorf("source stop lacks its complete retained-source snapshot")
	}
	if err := captureGuardCaptureReturned(config, anchors); err != nil {
		return err
	}
	if err := captureGuardWrite(anchors.evidence, "capture-stop-intent.json", map[string]any{
		"token": config.Token, "source_pid": config.PID, "source_start": config.Start,
		"capture": captured,
	}); err != nil {
		return err
	}
	fd, err := unix.Openat(int(anchors.group.Fd()), "cgroup.kill", unix.O_WRONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), "original captured source cgroup.kill")
	_, writeErr := file.WriteString("1")
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return err
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if err := captureGuardValidateStopped(config, anchors); err == nil {
			// Old Sysbox checks its freezer before reporting process exit.
			// Clear only this already-empty original cgroup's freezer.
			fd, err := unix.Openat(int(anchors.group.Fd()), "cgroup.freeze",
				unix.O_WRONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
			if err != nil {
				return err
			}
			freezer := os.NewFile(uintptr(fd), "empty captured source freezer")
			_, writeErr := freezer.WriteString("0")
			if err := errors.Join(writeErr, freezer.Close()); err != nil {
				return err
			}
			return captureGuardValidateStopped(config, anchors)
		} else if time.Now().After(deadline) {
			return fmt.Errorf("captured original source did not stop: %w", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (e *Engine) capture(ctx context.Context, guard *captureGuard, lock *os.File, executable string) error {
	if !guard.unsafe {
		return fmt.Errorf("capture command requires an acknowledged guarded transaction")
	}
	if err := guard.check(); err != nil {
		return err
	}
	if err := captureGuardValidate(guard.config, guard.anchor); err != nil {
		return err
	}
	if !digest.MatchString(e.config.CriuSHA256) {
		return fmt.Errorf("capture requires an exact CRIU SHA-256")
	}
	criu, err := openRuntime(e.config.Criu, e.config.CriuSHA256)
	if err != nil {
		return err
	}
	if err := criu.Close(); err != nil {
		return err
	}
	// Capture the FUSE values at the same frozen point as RAM. Forking from
	// this generation must not consult the source's later mutable datastore.
	if _, err := e.command(ctx, lock, "factorybox-fs-snapshot",
		"--file", filepath.Dir(guard.config.Evidence)+"/sysbox-state.json",
		"--owner", guard.config.Source.Owner, guard.config.Source.RuntimeID); err != nil {
		return fmt.Errorf("capture frozen FUSE state: %w", err)
	}
	data, err := json.Marshal(captureRequest{Guard: guard.config, Criu: e.config.Criu,
		CriuSHA256: e.config.CriuSHA256, ManagerRoot: e.config.ManagerRoot})
	if err != nil {
		return err
	}
	fd, err := unix.MemfdCreate("factorybox-capture-request", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		return err
	}
	request := os.NewFile(uintptr(fd), "sealed capture request")
	defer request.Close()
	if _, err := request.Write(data); err != nil {
		return err
	}
	if _, err := request.Seek(0, 0); err != nil {
		return err
	}
	if _, err := unix.FcntlInt(request.Fd(), unix.F_ADD_SEALS, captureRequestSeals); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "_capture")
	command.Env = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=/root", "LANG=C", "GOMAXPROCS=2", "GOMEMLIMIT=256MiB"}
	command.ExtraFiles = []*os.File{request, guard.anchor.group, guard.anchor.pid,
		guard.anchor.evidence, guard.anchor.root, lock}
	var stdout, stderr boundedOutput
	command.Stdout, command.Stderr = &stdout, &stderr
	command.WaitDelay = 2 * time.Second
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("retained source capture worker context ended: %w: %s", ctx.Err(), stderr.String())
		}
		return fmt.Errorf("retained source capture: %w: %s", err, stderr.String())
	}
	if err := guard.captureOperationReturned(); err != nil {
		return err
	}
	return nil
}
