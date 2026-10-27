//go:build linux

// Package lifecycle owns local sandbox intent and reconciles the pinned Sysbox
// process with that intent. Applications are not part of this package.
package lifecycle

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"factorybox/internal/fdstore"
	"factorybox/internal/platform"
	"factorybox/internal/sandboxnetwork"
	"factorybox/internal/storefs"
	"factorybox/internal/sysboxcapture"
	"golang.org/x/sys/unix"
)

const ownerAnnotation = "io.factorybox.owner"

var sandboxID = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
var digest = regexp.MustCompile(`^[0-9a-f]{64}$`)
var tokenID = regexp.MustCompile(`^[0-9a-f]{32}$`)

type Config struct {
	Root          string          `json:"root"`
	Runtime       string          `json:"runtime"`
	RuntimeSHA256 string          `json:"runtime_sha256"`
	Criu          string          `json:"criu"`
	CriuSHA256    string          `json:"criu_sha256"`
	ManagerRoot   string          `json:"manager_root"`
	MinFreeBytes  uint64          `json:"min_free_bytes,omitempty"`
	WriteReserve  uint64          `json:"write_reserve_bytes,omitempty"`
	BoundedStore  *storefs.Active `json:"bounded_store,omitempty"`
	FDStoreSocket string          `json:"fd_store_socket,omitempty"`
}

type Operation struct {
	RequestedAt string `json:"requested_at"`
	Attempted   bool   `json:"attempted"`
	CompletedAt string `json:"completed_at,omitempty"`
	Error       string `json:"error,omitempty"`
}

// Record retains the original start request even after deletion. An attempted
// create with no runtime state has an uncertain outcome and is never reissued.
type Record struct {
	Version        int                           `json:"version"`
	ID             string                        `json:"id"`
	Owner          string                        `json:"owner"`
	RuntimeID      string                        `json:"runtime_id"`
	RuntimeSHA256  string                        `json:"runtime_sha256"`
	BootID         string                        `json:"boot_id"`
	Phase          string                        `json:"phase"`
	InputBundle    string                        `json:"input_bundle"`
	InputSHA256    string                        `json:"input_sha256"`
	InputDirectory *KernelObject                 `json:"input_directory,omitempty"`
	ConfigSHA256   string                        `json:"config_sha256"`
	Rootfs         string                        `json:"rootfs"`
	RootfsDevice   uint64                        `json:"rootfs_device"`
	RootfsInode    uint64                        `json:"rootfs_inode"`
	RootfsMountID  uint64                        `json:"rootfs_mount_id,omitempty"`
	RuntimeCreated string                        `json:"runtime_created,omitempty"`
	CgroupParent   KernelObject                  `json:"cgroup_parent"`
	Cgroup         *KernelObject                 `json:"cgroup,omitempty"`
	Start          Operation                     `json:"start"`
	Delete         *Operation                    `json:"delete,omitempty"`
	Fork           *ForkOrigin                   `json:"fork,omitempty"`
	Pause          *PauseOperation               `json:"pause,omitempty"`
	PauseHistory   []PauseOperation              `json:"pause_history,omitempty"`
	Base           *sysboxcapture.FilesystemTree `json:"base,omitempty"`
	SharedSource   *sysboxcapture.SharedSource   `json:"shared_source,omitempty"`
	Network        *sandboxnetwork.Config        `json:"network,omitempty"`
}

type View struct {
	Record  Record        `json:"record"`
	Runtime *RuntimeState `json:"runtime,omitempty"`
}

type Engine struct {
	config Config
	boot   string
	root   *os.Root
}

func Open(config Config) (_ *Engine, resultErr error) {
	if err := validateStoragePolicy(config); err != nil {
		return nil, err
	}
	if os.Geteuid() != 0 {
		return nil, fmt.Errorf("the local Sysbox controller requires worker root")
	}
	if config.BoundedStore != nil {
		if err := storefs.Verify(*config.BoundedStore); err != nil {
			return nil, err
		}
	}
	info, err := platform.Current()
	if err == nil {
		err = info.CheckExecution()
	}
	if err != nil {
		return nil, err
	}
	if err := canonical(config.Root); err != nil {
		return nil, err
	}
	if config.BoundedStore != nil {
		engine := &Engine{config: config}
		for _, path := range []string{filepath.Dir(config.Root), config.ManagerRoot} {
			if err := engine.boundedObject(path); err != nil {
				return nil, err
			}
		}
	}
	if err := os.Mkdir(config.Root, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	if err := privateDirectory(config.Root); err != nil {
		return nil, err
	}
	if !digest.MatchString(config.RuntimeSHA256) {
		return nil, fmt.Errorf("an exact runtime SHA-256 is required")
	}
	file, err := openRuntime(config.Runtime, config.RuntimeSHA256)
	if err != nil {
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	if _, err := boundedCgroupRoot(); err != nil {
		return nil, err
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(config.Root)
	if err != nil {
		return nil, err
	}
	defer func() {
		if resultErr != nil {
			root.Close()
		}
	}()
	for _, name := range []string{"objects", "runtime", "forks", "bases"} {
		if err := root.Mkdir(name, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if err := privateDirectory(filepath.Join(config.Root, name)); err != nil {
			return nil, err
		}
	}
	return &Engine{config: config, boot: strings.TrimSpace(string(boot)), root: root}, nil
}

func (e *Engine) Close() error { return e.root.Close() }

func canonical(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("expected a clean absolute path: %q", path)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if errors.Is(err, os.ErrNotExist) {
		resolvedParent, parentErr := filepath.EvalSymlinks(filepath.Dir(path))
		if parentErr == nil && resolvedParent == filepath.Dir(path) {
			return nil
		}
	}
	if err != nil || resolved != path {
		return fmt.Errorf("path contains a symlink or is inaccessible: %q: %v", path, err)
	}
	return nil
}

func privateDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || info.Mode().Perm() != 0700 || stat.Uid != 0 {
		return fmt.Errorf("controller directory must be root-owned mode 0700: %s", path)
	}
	return nil
}

func (e *Engine) lock() (*os.File, error) {
	file, err := e.lockRoot()
	if err != nil {
		return nil, err
	}
	for _, path := range []string{"worker-exit.json", "worker-exit-complete.json"} {
		_, exitErr := e.root.Lstat(path)
		if !errors.Is(exitErr, os.ErrNotExist) {
			file.Close()
			return nil, fmt.Errorf("controller epoch is closed for worker exit: %v", exitErr)
		}
	}
	if err := e.epochRetirementFence(); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}

// lockRoot only serializes access. Lifecycle mutations must use lock, which
// also enforces the exit fence. A recovery reader validates its exact selection.
func (e *Engine) lockRoot() (*os.File, error) {
	if err := e.validateRoot(); err != nil {
		return nil, err
	}
	directory, err := e.root.Open(".")
	if err != nil {
		return nil, err
	}
	defer directory.Close()
	fd, err := unix.Openat(int(directory.Fd()), ".lock",
		unix.O_RDWR|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "factorybox-operation-lock")
	var stat unix.Stat_t
	if err = unix.Fstat(fd, &stat); err == nil {
		if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Mode&0777 != 0600 ||
			stat.Uid != 0 || stat.Nlink != 1 {
			err = fmt.Errorf("invalid controller lock identity")
		}
	}
	if err == nil {
		err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
	}
	if err == nil {
		err = e.validateRoot()
	}
	if err != nil {
		file.Close()
		return nil, err
	}
	// Close releases the lock. Do not explicitly LOCK_UN: an in-flight Sysbox
	// launcher inherits this open description if its controller is terminated.
	return file, nil
}

func (e *Engine) validateRoot() error {
	if e.config.BoundedStore != nil {
		if err := storefs.Verify(*e.config.BoundedStore); err != nil {
			return err
		}
		for _, path := range []string{e.config.Root, e.config.ManagerRoot} {
			if err := e.boundedObject(path); err != nil {
				return err
			}
		}
	}
	anchored, err := e.root.Stat(".")
	if err != nil {
		return err
	}
	current, err := os.Lstat(e.config.Root)
	if err != nil || !os.SameFile(anchored, current) {
		return fmt.Errorf("controller root identity changed: %v", err)
	}
	for _, path := range []string{e.config.Root, e.config.Root + "/objects", e.config.Root + "/runtime", e.config.Root + "/forks", e.config.Root + "/bases"} {
		if err := privateDirectory(path); err != nil {
			return err
		}
	}
	return nil
}

func objectPath(id string) (string, error) {
	if !sandboxID.MatchString(id) {
		return "", fmt.Errorf("invalid sandbox ID %q", id)
	}
	return "objects/" + id, nil
}

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func sum(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func (e *Engine) read(id string) (Record, error) {
	var record Record
	object, err := objectPath(id)
	if err != nil {
		return record, err
	}
	if err := privateDirectory(filepath.Join(e.config.Root, object)); err != nil {
		return record, err
	}
	file, err := e.root.Open(object + "/record.json")
	if err != nil {
		return record, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() > sandboxRecordLimit {
		return record, fmt.Errorf("sandbox record has an invalid size: %v", err)
	}
	if err := json.NewDecoder(io.LimitReader(file, sandboxRecordLimit)).Decode(&record); err != nil {
		return record, err
	}
	if record.Version != 1 || record.ID != id || !tokenID.MatchString(record.Owner) ||
		record.RuntimeID != "fbx-"+record.Owner || record.RuntimeSHA256 != e.config.RuntimeSHA256 ||
		record.BootID != e.boot || !digest.MatchString(record.ConfigSHA256) {
		return record, fmt.Errorf("saved sandbox identity or worker boot differs")
	}
	if record.Phase != "starting" && record.Phase != "running" &&
		record.Phase != "deleting" && record.Phase != "deleted" && record.Phase != "pausing" && record.Phase != "paused" && record.Phase != "resuming" {
		return record, fmt.Errorf("invalid saved lifecycle phase")
	}
	if record.Fork != nil && (!sandboxID.MatchString(record.Fork.SourceID) ||
		!tokenID.MatchString(record.Fork.CheckpointID) || !digest.MatchString(record.Fork.CheckpointSHA256) ||
		record.Fork.RequestedAt == "" || record.Fork.SourceID == record.ID ||
		record.InputBundle != e.config.Root+"/forks/"+record.ID+"/materialized/filesystem" ||
		record.Rootfs != record.InputBundle+"/rootfs" || record.RootfsMountID == 0) {
		return record, fmt.Errorf("saved fork ownership or backing layout differs")
	}
	config, err := e.root.ReadFile(object + "/config.json")
	if err != nil || sum(config) != record.ConfigSHA256 {
		return record, fmt.Errorf("saved OCI configuration changed: %v", err)
	}
	return record, nil
}

func (e *Engine) write(record Record) error {
	object, err := objectPath(record.ID)
	if err != nil {
		return err
	}
	data, err := sandboxRecordBytes(record)
	if err != nil {
		return err
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	temporary := object + "/.record-" + hex.EncodeToString(random[:])
	file, err := e.root.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer e.root.Remove(temporary)
	_, writeErr := file.Write(append(data, '\n'))
	if err := errors.Join(writeErr, file.Sync(), file.Close()); err != nil {
		return err
	}
	if err := e.root.Rename(temporary, object+"/record.json"); err != nil {
		return err
	}
	directory, err := e.root.Open(object)
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}

// Start accepts an administrator-prepared OCI bundle. Factorybox owns its copy
// of the configuration and runtime identity; it never deletes the input rootfs.
func (e *Engine) Start(ctx context.Context, id, bundle string) (View, error) {
	lock, err := e.lock()
	if err != nil {
		return View{}, err
	}
	defer lock.Close()
	object, err := objectPath(id)
	if err != nil {
		return View{}, err
	}
	if _, err := e.root.Lstat("forks/" + id); err == nil {
		return View{}, fmt.Errorf("sandbox ID belongs to a persisted fork; repeat the fork command")
	} else if !errors.Is(err, os.ErrNotExist) {
		return View{}, err
	}
	if err := canonical(bundle); err != nil {
		return View{}, err
	}
	input, err := os.ReadFile(filepath.Join(bundle, "config.json"))
	if err != nil {
		return View{}, err
	}
	record, err := e.read(id)
	if errors.Is(err, os.ErrNotExist) {
		if err := e.admitStorageWrite(); err != nil {
			return View{}, err
		}
		record, err = e.prepareRecordContext(ctx, id, bundle, object, input, "", nil)
	}
	if err != nil {
		return View{}, err
	}
	if record.InputBundle != bundle || record.InputSHA256 != sum(input) {
		return View{Record: record}, fmt.Errorf("sandbox ID already belongs to a different start request")
	}
	if err := sysboxcapture.VerifySharedSource(record.SharedSource); err != nil {
		return View{Record: record}, err
	}
	if record.Phase != "starting" && record.Phase != "running" {
		return View{Record: record}, fmt.Errorf("cannot start a %s sandbox", record.Phase)
	}
	state, err := e.state(ctx, record, lock)
	if err != nil {
		return View{Record: record}, err
	}
	if state == nil && record.Start.Attempted {
		return View{Record: record}, fmt.Errorf("start outcome is uncertain; refusing a second create")
	}
	if state == nil {
		if err := e.admitStorageWrite(); err != nil {
			return View{Record: record}, err
		}
		if err := e.prepareCgroup(&record); err != nil {
			return View{Record: record}, err
		}
		record.Start.Attempted = true
		if err := e.write(record); err != nil {
			return View{Record: record}, err
		}
		arguments := []string{"run", "--detach", "--bundle", filepath.Join(e.config.Root, object), record.RuntimeID}
		if record.Network != nil {
			arguments = []string{"create", "--bundle", filepath.Join(e.config.Root, object), record.RuntimeID}
		}
		_, runErr := e.command(ctx, lock, arguments...)
		if runErr != nil {
			record.Start.Error = sandboxErrorSummary(runErr.Error())
			return View{Record: record}, errors.Join(runErr, e.write(record))
		}
		state, err = e.state(ctx, record, lock)
		if err != nil {
			return View{Record: record}, err
		}
	}
	if record.Network != nil && state != nil && state.Status == "created" {
		if err := record.Network.Configure(ctx, state.PID, false); err != nil {
			return View{Record: record, Runtime: state}, err
		}
		if _, err := e.command(ctx, lock, "start", record.RuntimeID); err != nil {
			return View{Record: record, Runtime: state}, err
		}
		state, err = e.state(ctx, record, lock)
		if err != nil {
			return View{Record: record}, err
		}
	}
	if state == nil || state.Status != "running" {
		return View{Record: record, Runtime: state}, fmt.Errorf("start has no running owned runtime")
	}
	record.RuntimeCreated, record.Phase = state.Created, "running"
	if record.Start.CompletedAt == "" {
		record.Start.CompletedAt = now()
	}
	record.Start.Error = ""
	return View{Record: record, Runtime: state}, e.write(record)
}

func (e *Engine) prepare(id, bundle, object string, input []byte) (Record, error) {
	return e.prepareRecord(id, bundle, object, input, "", nil)
}

func (e *Engine) prepareRecord(id, bundle, object string, input []byte, owner string, origin *ForkOrigin) (Record, error) {
	return e.prepareRecordContext(context.Background(), id, bundle, object, input, owner, origin)
}

func (e *Engine) prepareRecordContext(ctx context.Context, id, bundle, object string, input []byte, owner string, origin *ForkOrigin) (Record, error) {
	var record Record
	var spec map[string]json.RawMessage
	if err := json.Unmarshal(input, &spec); err != nil {
		return record, err
	}
	var root struct {
		Path     string `json:"path"`
		Readonly bool   `json:"readonly"`
	}
	if err := json.Unmarshal(spec["root"], &root); err != nil || root.Path == "" {
		return record, fmt.Errorf("missing OCI rootfs")
	}
	if !filepath.IsAbs(root.Path) {
		root.Path = filepath.Join(bundle, root.Path)
	}
	if err := canonical(root.Path); err != nil {
		return record, err
	}
	info, err := os.Lstat(root.Path)
	if err != nil || !info.IsDir() {
		return record, fmt.Errorf("rootfs is not an accessible directory: %v", err)
	}
	stat := info.Sys().(*syscall.Stat_t)
	rootIdentity, err := directoryIdentity(root.Path)
	if err != nil {
		return record, err
	}
	shared, err := e.admitSharedSource(spec)
	if err != nil {
		return record, err
	}
	if err := e.boundedBundle(bundle, root.Path, spec); err != nil {
		return record, err
	}
	if err := e.exclusiveRootfs(root.Path, uint64(stat.Dev), stat.Ino); err != nil {
		return record, err
	}
	if owner == "" {
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			return record, err
		}
		owner = hex.EncodeToString(random[:])
	} else if !tokenID.MatchString(owner) {
		return record, fmt.Errorf("invalid persisted runtime owner")
	}
	parent, err := boundedCgroupRoot()
	if err != nil {
		return record, err
	}
	var linux map[string]json.RawMessage
	if err := json.Unmarshal(spec["linux"], &linux); err != nil || linux == nil {
		return record, fmt.Errorf("missing Linux OCI configuration: %v", err)
	}
	// The controller owns the cgroup name. Caller-supplied paths must never
	// direct a later force-delete into another sandbox or a worker cgroup.
	linux["cgroupsPath"], _ = json.Marshal("/fbx-" + owner)
	spec["linux"], _ = json.Marshal(linux)
	annotations := map[string]string{}
	if data := spec["annotations"]; len(data) != 0 {
		if err := json.Unmarshal(data, &annotations); err != nil || annotations == nil {
			return record, fmt.Errorf("invalid OCI annotations: %v", err)
		}
	}
	if _, exists := annotations[ownerAnnotation]; exists {
		return record, fmt.Errorf("reserved Factorybox ownership annotation")
	}
	annotations[ownerAnnotation] = owner
	if _, err := sysboxcapture.CheckpointConfiguration(annotations); err != nil {
		return record, err
	}
	base, err := e.recordBase(id, bundle, origin, annotations)
	if err != nil {
		return record, err
	}
	var network *sandboxnetwork.Config
	requested, err := sandboxnetwork.Requested(annotations)
	if err != nil {
		return record, err
	}
	if requested {
		if e.config.FDStoreSocket == "" {
			return record, fmt.Errorf("outbound networking requires the original worker epoch")
		}
		if err := fdstore.Ping(e.config.FDStoreSocket); err != nil {
			return record, err
		}
		if origin != nil {
			network, err = e.forkNetwork(*origin, id, owner)
		} else {
			network, err = sandboxnetwork.Plan(owner)
		}
		if err != nil {
			return record, err
		}
		if err := e.admitNetwork(*network, id); err != nil {
			return record, err
		}
	}
	spec["root"], _ = json.Marshal(root)
	spec["annotations"], _ = json.Marshal(annotations)
	config, err := json.Marshal(spec)
	if err != nil {
		return record, err
	}
	inputDirectory, err := directoryIdentity(bundle)
	if err != nil {
		return record, err
	}
	record = Record{Version: 1, ID: id, Owner: owner, RuntimeID: "fbx-" + owner,
		RuntimeSHA256: e.config.RuntimeSHA256, BootID: e.boot, Phase: "starting",
		InputBundle: bundle, InputSHA256: sum(input), InputDirectory: &inputDirectory, ConfigSHA256: sum(config),
		Rootfs: root.Path, RootfsDevice: uint64(stat.Dev), RootfsInode: stat.Ino, RootfsMountID: rootIdentity.MountID,
		CgroupParent: parent,
		Start:        Operation{RequestedAt: now()}, Fork: origin, Base: base, SharedSource: shared, Network: network}
	if origin != nil {
		record.Start.RequestedAt = origin.RequestedAt
	}
	if err := e.admitSandboxMetadata(record, false); err != nil {
		return record, err
	}
	if err := e.admitWorkerMetadata(ctx, workerMetadataDelta{Record: &record}); err != nil {
		return record, err
	}
	if err := e.root.Mkdir(object, 0700); err != nil {
		return record, fmt.Errorf("sandbox storage already exists or cannot be created: %w", err)
	}
	file, err := e.root.OpenFile(object+"/config.json", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return record, err
	}
	_, writeErr := file.Write(config)
	if err := errors.Join(writeErr, file.Sync(), file.Close()); err != nil {
		return record, err
	}
	if err := e.write(record); err != nil {
		return record, err
	}
	objects, err := e.root.Open("objects")
	if err != nil {
		return record, err
	}
	return record, errors.Join(objects.Sync(), objects.Close())
}

func (e *Engine) exclusiveRootfs(path string, device, inode uint64) error {
	directory, err := e.root.Open("objects")
	if err != nil {
		return err
	}
	defer directory.Close()
	names, err := directory.Readdirnames(-1)
	if err != nil {
		return err
	}
	for _, id := range names {
		record, err := e.read(id)
		if err != nil {
			return fmt.Errorf("existing sandbox ownership cannot be checked: %w", err)
		}
		if record.Phase != "deleted" && (record.Rootfs == path ||
			(record.RootfsDevice == device && record.RootfsInode == inode)) {
			return fmt.Errorf("writable rootfs already belongs to sandbox %s", id)
		}
	}
	return nil
}

func (e *Engine) Inspect(ctx context.Context, id string) (View, error) {
	lock, err := e.lock()
	if err != nil {
		return View{}, err
	}
	defer lock.Close()
	record, err := e.read(id)
	if err != nil {
		return View{}, err
	}
	state, err := e.state(ctx, record, lock)
	return View{Record: record, Runtime: state}, err
}

// Delete removes only a runtime whose saved ownership and creation identity
// still match. It keeps the request, configuration and deletion outcome.
func (e *Engine) Delete(ctx context.Context, id string) (View, error) {
	lock, err := e.lock()
	if err != nil {
		return View{}, err
	}
	defer lock.Close()
	record, err := e.read(id)
	if err != nil {
		return View{}, err
	}
	if record.Phase == "pausing" || record.Phase == "resuming" {
		return View{Record: record}, fmt.Errorf("pause/resume is unresolved; repeat its exact request before deleting")
	}
	state, err := e.state(ctx, record, lock)
	if err != nil {
		return View{Record: record}, err
	}
	if record.Phase == "deleted" {
		if state != nil {
			return View{Record: record}, fmt.Errorf("deleted identity unexpectedly exists")
		}
		return View{Record: record}, nil
	}
	if state == nil && record.RuntimeCreated == "" && record.Start.Attempted {
		return View{Record: record}, fmt.Errorf("unresolved create; refusing to discard its evidence")
	}
	if record.Delete == nil {
		record.Delete = &Operation{RequestedAt: now()}
	}
	record.Phase = "deleting"
	if state != nil {
		record.RuntimeCreated = state.Created
		record.Delete.Attempted = true
	}
	if err := e.write(record); err != nil {
		return View{Record: record}, err
	}
	if state != nil {
		if _, err := e.command(ctx, lock, "delete", "--force", record.RuntimeID); err != nil {
			record.Delete.Error = sandboxErrorSummary(err.Error())
			return View{Record: record}, errors.Join(err, e.write(record))
		}
	}
	remaining, err := e.state(ctx, record, lock)
	if err != nil || remaining != nil {
		return View{Record: record}, errors.Join(err, fmt.Errorf("runtime deletion is not confirmed"))
	}
	if err := e.retireEmptyCgroup(record); err != nil {
		return View{Record: record}, err
	}
	if err := e.retireForkOverlay(record); err != nil {
		return View{Record: record}, err
	}
	if err := e.releaseAllSharedFiles(record); err != nil {
		return View{Record: record}, err
	}
	if record.Network != nil {
		if err := record.Network.Delete(ctx); err != nil {
			return View{Record: record}, err
		}
	}
	record.Phase, record.Delete.CompletedAt, record.Delete.Error = "deleted", now(), ""
	return View{Record: record}, e.write(record)
}
