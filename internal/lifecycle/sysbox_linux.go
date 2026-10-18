//go:build linux

package lifecycle

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

type RuntimeState struct {
	ID          string            `json:"id"`
	Status      string            `json:"status"`
	PID         int               `json:"pid"`
	Bundle      string            `json:"bundle"`
	Rootfs      string            `json:"rootfs"`
	Created     string            `json:"created"`
	Annotations map[string]string `json:"annotations"`
}

func openRuntime(path, expected string) (_ *os.File, resultErr error) {
	if err := canonical(path); err != nil {
		return nil, err
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "pinned-sysbox-runc")
	defer func() {
		if resultErr != nil {
			file.Close()
		}
	}()
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return nil, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Mode&0022 != 0 ||
		stat.Mode&0111 == 0 || stat.Uid != 0 {
		return nil, fmt.Errorf("runtime must be a root-owned executable without group/other write access")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return nil, err
	}
	if hex.EncodeToString(hash.Sum(nil)) != expected {
		return nil, fmt.Errorf("runtime SHA-256 differs")
	}
	return file, nil
}

type boundedOutput struct {
	bytes.Buffer
}

func (b *boundedOutput) Write(data []byte) (int, error) {
	n := len(data)
	remaining := (64 << 10) - b.Len()
	if remaining > 0 {
		b.Buffer.Write(data[:min(remaining, n)])
	}
	return n, nil
}

func (e *Engine) command(ctx context.Context, lock *os.File, args ...string) ([]byte, error) {
	return e.commandFiles(ctx, lock, nil, args...)
}

func (e *Engine) commandFiles(ctx context.Context, lock *os.File, files []*os.File, args ...string) ([]byte, error) {
	if err := e.validateRoot(); err != nil {
		return nil, err
	}
	binary, err := openRuntime(e.config.Runtime, e.config.RuntimeSHA256)
	if err != nil {
		return nil, err
	}
	defer binary.Close()
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/proc/self/fd/3",
		append([]string{"--root", e.config.Root + "/runtime"}, args...)...)
	// Execute the verified descriptor. Keep the operation lock in the Sysbox
	// launcher if this controller exits before the launcher has completed.
	command.ExtraFiles = append([]*os.File{binary, lock}, files...)
	command.Env = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=/root", "LANG=C", "GOMAXPROCS=2"}
	var stdout, stderr boundedOutput
	command.Stdout, command.Stderr = &stdout, &stderr
	command.WaitDelay = 2 * time.Second
	if err := command.Run(); err != nil {
		return stdout.Bytes(), fmt.Errorf("Sysbox %s: %w: %s", args[0], err, stderr.String())
	}
	return stdout.Bytes(), nil
}

func (e *Engine) state(ctx context.Context, record Record, lock *os.File) (*RuntimeState, error) {
	path := e.config.Root + "/runtime/" + record.RuntimeID
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("runtime directory is invalid: %v", err)
	}
	if err := e.validateCgroup(record); err != nil {
		return nil, err
	}
	data, err := e.command(ctx, lock, "state", record.RuntimeID)
	if err != nil {
		return nil, err
	}
	var state RuntimeState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	object, err := objectPath(record.ID)
	if err != nil {
		return nil, err
	}
	if state.ID != record.RuntimeID || state.Created == "" ||
		state.Annotations[ownerAnnotation] != record.Owner ||
		state.Rootfs != record.Rootfs || state.Bundle != filepath.Join(e.config.Root, object) ||
		(record.RuntimeCreated != "" && state.Created != record.RuntimeCreated) {
		return nil, fmt.Errorf("runtime ownership or immutable identity differs")
	}
	rootfs, err := os.Lstat(record.Rootfs)
	if err != nil {
		return nil, err
	}
	stat := rootfs.Sys().(*syscall.Stat_t)
	if !rootfs.IsDir() || uint64(stat.Dev) != record.RootfsDevice || stat.Ino != record.RootfsInode {
		return nil, fmt.Errorf("input rootfs identity changed")
	}
	return &state, nil
}
