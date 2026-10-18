//go:build linux

package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"factorybox/internal/sysboxcapture"
	"golang.org/x/sys/unix"
)

// The Sysbox adapter decodes its vendor config inside the pinned executable.
// The lifecycle controller passes only owned identities and saved opaque bytes.
type restoreRequest struct {
	Version      int    `json:"version"`
	SandboxID    string `json:"sandbox_id"`
	Owner        string `json:"owner"`
	SourceID     string `json:"source_id"`
	SourceRootfs string `json:"source_rootfs"`
	Root         string `json:"root"`
	Filesystem   string `json:"filesystem"`
	Images       string `json:"images"`
	ManagerRoot  string `json:"manager_root"`
	ConfigSHA256 string `json:"config_sha256"`
	State        []byte `json:"state"`
	FsState      []byte `json:"fs_state"`
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
	_, err = e.commandFiles(ctx, lock, []*os.File{file}, "--criu", e.config.Criu,
		"factorybox-restore", target.RuntimeID)
	return err
}
