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
	Guard      captureGuardConfig `json:"guard"`
	Criu       string             `json:"criu"`
	CriuSHA256 string             `json:"criu_sha256"`
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
	if err := captureGuardWrite(anchors.evidence, "capture-worker-intent.json",
		map[string]any{"token": request.Guard.Token, "worker_pid": os.Getpid(),
			"worker_start": birth, "source_pid": request.Guard.PID,
			"criu_sha256": request.CriuSHA256, "scope": "ordinary complete retained-source capture"}); err != nil {
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
	captured, err = sysboxcapture.Capture(sysboxcapture.Request{
		RuntimeRoot: request.Guard.Root + "/runtime", RuntimeID: request.Guard.Source.RuntimeID,
		Owner: request.Guard.Source.Owner, Rootfs: request.Guard.Source.Rootfs,
		RuntimeCreated: request.Guard.Source.RuntimeCreated, PID: request.Guard.PID,
		Generation: filepath.Dir(request.Guard.Evidence), Criu: request.Criu,
	})
	if err != nil {
		return err
	}
	return captureGuardCaptureReturned(request.Guard, anchors)
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
	data, err := json.Marshal(captureRequest{Guard: guard.config, Criu: e.config.Criu,
		CriuSHA256: e.config.CriuSHA256})
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
		return fmt.Errorf("retained source capture: %w: %s", err, stderr.String())
	}
	if err := guard.captureOperationReturned(); err != nil {
		return err
	}
	return nil
}
