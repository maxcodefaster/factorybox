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
	"reflect"
	"time"

	"golang.org/x/sys/unix"
)

type restoreWorkerRequest struct {
	Version    int    `json:"version"`
	Config     Config `json:"config"`
	BootID     string `json:"boot_id"`
	TargetID   string `json:"target_id"`
	TargetSHA  string `json:"target_sha256"`
	RequestSHA string `json:"request_sha256"`
	Operation  string `json:"operation"`
	Relative   string `json:"relative"`
}

func readSealedRestoreRequest(file *os.File, limit int64) ([]byte, error) {
	seals, err := unix.FcntlInt(file.Fd(), unix.F_GET_SEALS, 0)
	if err != nil || seals&captureRequestSeals != captureRequestSeals {
		return nil, fmt.Errorf("restore worker request is not sealed: %v", err)
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > limit {
		return nil, fmt.Errorf("restore worker request is not a bounded regular file: %v", err)
	}
	// ReadAt keeps the inherited runtime request at its original offset.
	return io.ReadAll(io.NewSectionReader(file, 0, info.Size()))
}

func decodeRestoreWorkerRequest(file *os.File) (restoreWorkerRequest, error) {
	var request restoreWorkerRequest
	data, err := readSealedRestoreRequest(file, captureRequestLimit)
	if err != nil {
		return request, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return request, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return request, fmt.Errorf("restore worker request has trailing data")
	}
	if request.Version != 1 || !sandboxID.MatchString(request.TargetID) ||
		!digest.MatchString(request.TargetSHA) || !digest.MatchString(request.RequestSHA) || request.BootID == "" ||
		(request.Operation != "restore" && request.Operation != "resume") {
		return request, fmt.Errorf("restore worker request identity is invalid")
	}
	return request, nil
}

func restoreWorkerLock(root, lock *os.File) error {
	var held unix.Stat_t
	if err := unix.Fstat(int(lock.Fd()), &held); err != nil {
		return err
	}
	if held.Mode&unix.S_IFMT != unix.S_IFREG || held.Mode&0777 != 0600 || held.Uid != 0 || held.Nlink != 1 {
		return fmt.Errorf("restore worker inherited an invalid operation lock")
	}
	fd, err := unix.Openat(int(root.Fd()), ".lock", unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	var current unix.Stat_t
	if err := unix.Fstat(fd, &current); err != nil {
		return err
	}
	if current.Dev != held.Dev || current.Ino != held.Ino {
		return fmt.Errorf("restore worker operation lock was replaced")
	}
	// A separately opened description must conflict, while the inherited
	// description must already own that same exclusive flock. Closing either
	// probe never explicitly unlocks the operation shared with the controller.
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); !errors.Is(err, unix.EWOULDBLOCK) {
		return fmt.Errorf("restore worker operation lock is not held: %v", err)
	}
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return fmt.Errorf("restore worker does not own its inherited lock: %w", err)
	}
	return nil
}

// RestoreWorkerCommand accepts only an inherited sealed request and exact
// root/lock descriptors. The following two arguments identify its real backend
// operation for observation; they cannot supply a new restore recipe.
func RestoreWorkerCommand(args []string) (bool, error) {
	if len(args) != 3 || args[0] != "_restore-worker" {
		return false, nil
	}
	return true, serveRestoreWorker(args[1], args[2])
}

func serveRestoreWorker(operation, runtimeID string) error {
	envelope := os.NewFile(3, "sealed restore worker request")
	lock := os.NewFile(4, "inherited restore operation lock")
	root := os.NewFile(5, "original restore controller root")
	sealed := os.NewFile(6, "original sealed runtime restore request")
	defer envelope.Close()
	defer lock.Close()
	defer root.Close()
	defer sealed.Close()
	request, err := decodeRestoreWorkerRequest(envelope)
	if err != nil {
		return err
	}
	if operation != "factorybox-"+request.Operation {
		return fmt.Errorf("restore worker command differs from its sealed operation")
	}
	anchored, err := root.Stat()
	if err != nil {
		return err
	}
	current, err := os.Lstat(request.Config.Root)
	if err != nil || !os.SameFile(anchored, current) {
		return fmt.Errorf("restore worker controller root was replaced: %v", err)
	}
	if err := restoreWorkerLock(root, lock); err != nil {
		return err
	}
	e, err := Open(request.Config)
	if err != nil {
		return err
	}
	defer e.Close()
	opened, err := e.root.Stat(".")
	if err != nil || !os.SameFile(anchored, opened) || e.boot != request.BootID {
		return fmt.Errorf("restore worker root or boot identity changed: %v", err)
	}
	for _, name := range []string{"worker-exit.json", "worker-exit-complete.json"} {
		if _, err := e.root.Lstat(name); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("restore worker epoch is closed: %v", err)
		}
	}
	if err := e.epochRetirementFence(); err != nil {
		return err
	}
	target, err := e.read(request.TargetID)
	if err != nil {
		return err
	}
	data, err := json.Marshal(target)
	if err != nil || sum(data) != request.TargetSHA || target.RuntimeID != runtimeID || target.BootID != e.boot ||
		target.RuntimeSHA256 != e.config.RuntimeSHA256 {
		return fmt.Errorf("restore worker persisted target differs: %v", err)
	}
	raw, err := readSealedRestoreRequest(sealed, 2<<20)
	if err != nil || sum(raw) != request.RequestSHA {
		return fmt.Errorf("restore worker runtime request differs: %v", err)
	}
	var expected any
	relative := "objects/" + target.ID
	if request.Operation == "restore" {
		if target.Phase != "starting" || !target.Start.Attempted || target.Start.CompletedAt != "" || target.Fork == nil {
			return fmt.Errorf("restore worker lacks its attempted fork")
		}
		var intent forkIntent
		if err := e.readCheckpointJSON("forks/"+target.ID+"/intent.json", &intent); err != nil {
			return err
		}
		if intent.Owner != target.Owner || !reflect.DeepEqual(target.Fork, &intent.Origin) {
			return fmt.Errorf("restore worker fork origin differs")
		}
		expected = e.forkRestoreRequest(intent, target)
	} else {
		if target.Phase != "resuming" || target.Pause == nil || target.Pause.Resume == nil ||
			!target.Pause.Resume.Attempted || target.Pause.Resume.CompletedAt != "" || target.Pause.CompletedAt == "" {
			return fmt.Errorf("restore worker lacks its attempted completed-pause resume")
		}
		expected = e.resumeRequest(target)
		relative += "/resumes/" + target.Pause.SnapshotID
	}
	data, err = json.Marshal(expected)
	if err != nil || !bytes.Equal(data, raw) || request.Relative != relative {
		return fmt.Errorf("restore worker request differs from its persisted recipe: %v", err)
	}
	if err := e.requireEmptyCgroup(target); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	return e.heldRestoreWorker(ctx, lock, target, expected, sealed, relative, request.Operation)
}

func (e *Engine) heldRestore(ctx context.Context, lock *os.File, target Record, request any, sealed *os.File, relative, operation string) error {
	data, err := json.Marshal(request)
	if err != nil {
		return err
	}
	record, err := json.Marshal(target)
	if err != nil {
		return err
	}
	worker := restoreWorkerRequest{Version: 1, Config: e.config, BootID: e.boot,
		TargetID: target.ID, TargetSHA: sum(record), RequestSHA: sum(data), Operation: operation, Relative: relative}
	data, err = json.Marshal(worker)
	if err != nil || len(data) > captureRequestLimit {
		return fmt.Errorf("restore worker request exceeds its bound: %v", err)
	}
	fd, err := unix.MemfdCreate("factorybox-restore-worker", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		return err
	}
	envelope := os.NewFile(uintptr(fd), "sealed restore worker request")
	defer envelope.Close()
	if _, err := envelope.Write(data); err != nil {
		return err
	}
	if _, err := unix.FcntlInt(envelope.Fd(), unix.F_ADD_SEALS, captureRequestSeals); err != nil {
		return err
	}
	root, err := e.root.Open(".")
	if err != nil {
		return err
	}
	defer root.Close()
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/proc/self/exe", "_restore-worker", "factorybox-"+operation, target.RuntimeID)
	command.ExtraFiles = []*os.File{envelope, lock, root, sealed}
	command.Env = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=/root", "LANG=C", "GOMAXPROCS=2", "GOMEMLIMIT=256MiB"}
	var stdout, stderr boundedOutput
	command.Stdout, command.Stderr = &stdout, &stderr
	command.WaitDelay = 2 * time.Second
	// No parent-death signal: the already accepted worker retains the exact
	// operation lock and can publish completion after the caller disappears.
	if err := command.Run(); err != nil {
		return fmt.Errorf("restore completion worker: %w: %s", err, stderr.String())
	}
	return nil
}
