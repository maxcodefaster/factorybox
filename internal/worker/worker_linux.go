//go:build linux

// Package worker owns the pinned Sysbox services inside one bounded Docker
// worker. It has no consumer, IDE, network orchestrator or agent dependency.
package worker

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"time"

	"factorybox/internal/fdstore"
	"factorybox/internal/lifecycle"
	"factorybox/internal/platform"
	"factorybox/internal/storefs"
	"factorybox/internal/sysboxcapture"
	"golang.org/x/sys/unix"
)

type Config struct {
	Store               string
	RunRoot             string
	Components          string
	Manifest            string
	StoreID             string
	BoundedRoot         string
	BoundedBytes        int64
	MinimumBoundedBytes int64
	HealthListen        string
}

const (
	localMinFreeBytes = 12 << 30
	localWriteReserve = 4 << 30
)

type Process struct {
	PID   int    `json:"pid"`
	Birth string `json:"birth"`
}

type Status struct {
	Schema           int                   `json:"schema"`
	StoreID          string                `json:"store_id"`
	Store            string                `json:"store"`
	Epoch            string                `json:"epoch"`
	BootID           string                `json:"boot_id"`
	Supervisor       Process               `json:"supervisor"`
	SupervisorSHA256 string                `json:"supervisor_sha256"`
	Components       map[string]string     `json:"component_sha256"`
	Manager          Process               `json:"manager"`
	Filesystem       Process               `json:"filesystem"`
	ManagerPeer      Process               `json:"manager_peer"`
	FilesystemPeer   Process               `json:"filesystem_peer"`
	ReadyAt          string                `json:"ready_at"`
	Lifecycle        lifecycle.Config      `json:"lifecycle"`
	BoundedCapacity  *boundedCapacityRange `json:"bounded_capacity,omitempty"`
}

type child struct {
	command  *exec.Cmd
	identity Process
	done     chan error
	pidfd    int
}

func nonce() (string, error) {
	var value [16]byte
	_, err := rand.Read(value[:])
	return hex.EncodeToString(value[:]), err
}

func private(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" {
		return fmt.Errorf("requires a canonical private path: %s", path)
	}
	for parent := path; ; parent = filepath.Dir(parent) {
		info, err := os.Lstat(parent)
		if err != nil {
			return err
		}
		stat := info.Sys().(*syscall.Stat_t)
		if !info.IsDir() || stat.Uid != 0 || info.Mode().Perm()&0022 != 0 ||
			(parent == path && info.Mode().Perm() != 0700) {
			return fmt.Errorf("unsafe worker directory: %s", parent)
		}
		if parent == "/" {
			return nil
		}
	}
}

func makePrivate(path string) error {
	if err := os.Mkdir(path, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	return private(path)
}

func openLease(store string, create bool) (*os.File, error) {
	flags := unix.O_RDWR | unix.O_NOFOLLOW | unix.O_CLOEXEC
	if create {
		flags |= unix.O_CREAT
	}
	fd, err := unix.Open(store+"/.worker.lock", flags, 0600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "worker-store-lease")
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Mode&0777 != 0600 || stat.Uid != 0 || stat.Nlink != 1 {
		file.Close()
		return nil, fmt.Errorf("invalid worker store lease: %v", err)
	}
	return file, nil
}

func hashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	_, err = io.Copy(hash, file)
	return hex.EncodeToString(hash.Sum(nil)), err
}

func birth(pid int) (Process, error) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return Process{}, err
	}
	index := strings.LastIndex(string(data), ")")
	if index < 0 {
		return Process{}, fmt.Errorf("invalid process identity")
	}
	fields := strings.Fields(string(data)[index+1:])
	if len(fields) < 20 || fields[0] == "Z" || fields[0] == "X" {
		return Process{}, fmt.Errorf("process is absent or exited")
	}
	return Process{PID: pid, Birth: fields[19]}, nil
}

func live(saved Process) error {
	current, err := birth(saved.PID)
	if err != nil || current != saved || saved.PID <= 0 || saved.Birth == "" {
		return fmt.Errorf("worker process identity changed: %v", err)
	}
	return nil
}

func readJSON(path string, value any) error {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	stat := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || info.Size() > 1<<20 || stat.Uid != 0 || stat.Nlink != 1 || info.Mode().Perm() != 0600 {
		return fmt.Errorf("invalid private worker record")
	}
	decoder := json.NewDecoder(io.LimitReader(file, 1<<20))
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("worker record has trailing data")
	}
	return nil
}

func writeNew(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = file.Write(append(data, '\n'))
	if err := errors.Join(err, file.Sync(), file.Close()); err != nil {
		return err
	}
	parent, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	return errors.Join(parent.Sync(), parent.Close())
}

func componentManifest(config Config) (map[string]string, error) {
	if err := private(config.Components); err != nil {
		return nil, err
	}
	if err := private(filepath.Dir(config.Manifest)); err != nil {
		return nil, err
	}
	var manifest struct {
		Schema   int               `json:"schema"`
		Binaries map[string]string `json:"binary_sha256"`
	}
	if err := readJSON(config.Manifest, &manifest); err != nil {
		return nil, err
	}
	if manifest.Schema != 1 || len(manifest.Binaries) != 4 {
		return nil, fmt.Errorf("incomplete pinned worker component manifest")
	}
	for _, name := range []string{"sysbox-runc", "sysbox-mgr", "sysbox-fs", "criu"} {
		path := config.Components + "/" + name
		if name == "criu" {
			if err := sysboxcapture.CheckExecutableParents(path); err != nil {
				return nil, err
			}
		}
		info, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		expected := manifest.Binaries[name]
		actual, err := hashFile(path)
		if !info.Mode().IsRegular() || info.Sys().(*syscall.Stat_t).Uid != 0 || info.Mode().Perm()&0022 != 0 || info.Mode().Perm()&0111 == 0 || len(expected) != 64 || actual != expected || err != nil {
			return nil, fmt.Errorf("worker component changed: %s: %v", name, err)
		}
	}
	return manifest.Binaries, nil
}

func delegate() error {
	if os.Getpid() != 1 {
		return fmt.Errorf("worker must be PID 1 in its private bounded Docker PID/cgroup namespaces")
	}
	group, err := os.ReadFile("/proc/1/cgroup")
	if err != nil || strings.TrimSpace(string(group)) != "0::/" {
		return fmt.Errorf("worker lacks a private cgroup namespace: %v", err)
	}
	memory, err := os.ReadFile("/sys/fs/cgroup/memory.max")
	if err != nil {
		return err
	}
	maximum, err := strconv.ParseUint(strings.TrimSpace(string(memory)), 10, 64)
	if err != nil || maximum == 0 || maximum > 3<<30 {
		return fmt.Errorf("worker requires at most 3 GiB of finite cgroup memory")
	}
	cpu, err := os.ReadFile("/sys/fs/cgroup/cpu.max")
	if err != nil {
		return err
	}
	parts := strings.Fields(string(cpu))
	if len(parts) != 2 {
		return fmt.Errorf("worker CPU limit missing")
	}
	quota, e1 := strconv.ParseUint(parts[0], 10, 64)
	period, e2 := strconv.ParseUint(parts[1], 10, 64)
	if e1 != nil || e2 != nil || quota == 0 || period == 0 || quota/period > 3 || quota > 3*period {
		return fmt.Errorf("worker requires at most 3 CPUs")
	}
	processes, err := os.ReadFile("/sys/fs/cgroup/cgroup.procs")
	if err != nil || strings.TrimSpace(string(processes)) != "1" {
		return fmt.Errorf("private cgroup root has an unexpected process: %v", err)
	}
	if err := os.Mkdir("/sys/fs/cgroup/factorybox-worker", 0755); err != nil {
		return err
	}
	if err := os.WriteFile("/sys/fs/cgroup/factorybox-worker/cgroup.procs", []byte("1\n"), 0600); err != nil {
		return err
	}
	controllers, err := os.ReadFile("/sys/fs/cgroup/cgroup.controllers")
	if err != nil {
		return err
	}
	var enable []string
	for _, name := range strings.Fields(string(controllers)) {
		enable = append(enable, "+"+name)
	}
	if len(enable) == 0 {
		return fmt.Errorf("worker has no delegated cgroup controllers")
	}
	return os.WriteFile("/sys/fs/cgroup/cgroup.subtree_control", []byte(strings.Join(enable, " ")), 0600)
}

func startChild(path string, args []string, lease *os.File, log *os.File) (*child, error) {
	command := exec.Command(path, args...)
	command.Stdout, command.Stderr = log, log
	command.ExtraFiles = []*os.File{lease}
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	if err := command.Start(); err != nil {
		return nil, err
	}
	identity, err := birth(command.Process.Pid)
	if err != nil {
		command.Process.Kill()
		command.Wait()
		return nil, err
	}
	pidfd, err := unix.PidfdOpen(command.Process.Pid, 0)
	if err != nil {
		command.Process.Kill()
		command.Wait()
		return nil, err
	}
	service := &child{command: command, identity: identity, done: make(chan error, 1), pidfd: pidfd}
	go func() { service.done <- command.Wait(); close(service.done) }()
	return service, nil
}

func (c *child) alive() error {
	select {
	case err := <-c.done:
		return fmt.Errorf("owned Sysbox service exited: %v", err)
	default:
	}
	return live(c.identity)
}

func (c *child) stop() {
	defer unix.Close(c.pidfd)
	// The process handle was created here and remains pinned by pidfd. Group
	// signals are issued only while this original leader is still live.
	if c.alive() != nil {
		return
	}
	unix.Kill(-c.identity.PID, unix.SIGTERM)
	select {
	case <-c.done:
		return
	case <-time.After(5 * time.Second):
	}
	if c.alive() == nil {
		unix.Kill(-c.identity.PID, unix.SIGKILL)
	}
	select {
	case <-c.done:
	case <-time.After(2 * time.Second):
	}
}

func peer(socket string) (Process, error) {
	connection, err := net.DialTimeout("unix", socket, 100*time.Millisecond)
	if err != nil {
		return Process{}, err
	}
	defer connection.Close()
	raw, err := connection.(*net.UnixConn).SyscallConn()
	if err != nil {
		return Process{}, err
	}
	var credential *unix.Ucred
	var inner error
	if err := raw.Control(func(fd uintptr) { credential, inner = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED) }); err != nil {
		return Process{}, err
	}
	if inner != nil || credential == nil || credential.Uid != 0 {
		return Process{}, fmt.Errorf("unowned worker service socket: %v", inner)
	}
	return birth(int(credential.Pid))
}

func descendant(process, leader Process) bool {
	if live(leader) != nil {
		return false
	}
	for depth := 0; depth < 32; depth++ {
		if process == leader {
			return true
		}
		if process.PID <= 1 || live(process) != nil {
			return false
		}
		data, err := os.ReadFile("/proc/" + strconv.Itoa(process.PID) + "/stat")
		if err != nil {
			return false
		}
		index := strings.LastIndex(string(data), ")")
		if index < 0 {
			return false
		}
		fields := strings.Fields(string(data)[index+1:])
		if len(fields) < 2 {
			return false
		}
		parent, err := strconv.Atoi(fields[1])
		if err != nil {
			return false
		}
		process, err = birth(parent)
		if err != nil {
			return false
		}
	}
	return false
}

// Current validates a live worker, the held store lease, exact service births
// and socket peers. A stored ready marker alone never authorizes a CLI call.
func Current(runRoot string) (Status, error) {
	var status Status
	if err := private(runRoot); err != nil {
		return status, err
	}
	if err := readJSON(runRoot+"/worker.json", &status); err != nil {
		return status, err
	}
	if status.Schema != 1 || status.ReadyAt == "" || len(status.Epoch) != 32 || len(status.StoreID) != 32 {
		return status, fmt.Errorf("invalid worker readiness identity")
	}
	if status.Lifecycle.FDStoreSocket != runRoot+"/retained-fds.sock" {
		return status, fmt.Errorf("worker retained-file endpoint changed")
	}
	if err := fdstore.Ping(status.Lifecycle.FDStoreSocket); err != nil {
		return status, err
	}
	if strings.Trim(status.Epoch, "0123456789abcdef") != "" || strings.Trim(status.StoreID, "0123456789abcdef") != "" ||
		status.Lifecycle.Root != status.Store+"/epochs/"+status.Epoch+"/controller" || status.Lifecycle.ManagerRoot != status.Store+"/manager" ||
		len(status.Components) != 4 || status.Lifecycle.RuntimeSHA256 != status.Components["sysbox-runc"] || status.Lifecycle.CriuSHA256 != status.Components["criu"] {
		return status, fmt.Errorf("worker controller epoch or component recipe changed")
	}
	if err := verifyBoundedStatus(status); err != nil {
		return status, err
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil || strings.TrimSpace(string(boot)) != status.BootID {
		return status, fmt.Errorf("worker boot changed: %v", err)
	}
	if err := private(status.Store); err != nil {
		return status, err
	}
	for _, process := range []Process{status.Supervisor, status.Manager, status.Filesystem} {
		if err := live(process); err != nil {
			return status, err
		}
	}
	for name, process := range map[string]Process{"sysbox-mgr": status.Manager, "sysbox-fs": status.Filesystem} {
		hash, err := hashFile("/proc/" + strconv.Itoa(process.PID) + "/exe")
		if err != nil || hash != status.Components[name] {
			return status, fmt.Errorf("worker service executable changed: %s: %v", name, err)
		}
	}
	hash, err := hashFile("/proc/" + strconv.Itoa(status.Supervisor.PID) + "/exe")
	if err != nil || hash != status.SupervisorSHA256 {
		return status, fmt.Errorf("worker supervisor executable changed: %v", err)
	}
	lease, err := openLease(status.Store, false)
	if err != nil {
		return status, err
	}
	defer lease.Close()
	if err := unix.Flock(int(lease.Fd()), unix.LOCK_EX|unix.LOCK_NB); err == nil {
		return status, fmt.Errorf("worker no longer holds its persistent-store lease")
	} else if !errors.Is(err, unix.EWOULDBLOCK) {
		return status, err
	}
	var identity struct {
		Schema int    `json:"schema"`
		ID     string `json:"id"`
	}
	if err := readJSON(status.Store+"/store.json", &identity); err != nil || identity.Schema != 1 || identity.ID != status.StoreID {
		return status, fmt.Errorf("worker store identity changed: %v", err)
	}
	for _, service := range []struct {
		socket        string
		saved, leader Process
	}{
		{"/run/sysbox/sysmgr.sock", status.ManagerPeer, status.Manager},
		{"/run/sysbox/sysfs-seccomp.sock", status.FilesystemPeer, status.Filesystem},
	} {
		actual, err := peer(service.socket)
		if err != nil || actual != service.saved || !descendant(actual, service.leader) {
			return status, fmt.Errorf("worker socket peer changed: %v", err)
		}
	}
	return status, nil
}

// Run must be the init process in a fresh bounded Docker worker. Its lifetime
// owns the store lease; every recreation creates a new controller epoch.
func Run(ctx context.Context, config Config) (runErr error) {
	if os.Geteuid() != 0 || os.Getpid() != 1 {
		return fmt.Errorf("worker requires root as PID 1 in a fresh bounded Docker container")
	}
	if len(config.StoreID) != 32 || strings.Trim(config.StoreID, "0123456789abcdef") != "" {
		return fmt.Errorf("worker requires an explicit 32-character hexadecimal store ID")
	}
	if err := validateHealthListen(config.HealthListen); err != nil {
		return err
	}
	info, err := platform.Current()
	if err == nil {
		err = info.CheckExecution()
	}
	if err != nil {
		return err
	}
	if err := boundedConfig(&config); err != nil {
		return err
	}
	var bounded *storefs.Active
	if config.BoundedRoot != "" {
		attachment, outerLease, err := attachBounded(ctx, config)
		if err != nil {
			return err
		}
		defer outerLease.Close()
		defer func() { runErr = errors.Join(runErr, attachment.Close()) }()
		bounded = &attachment.Active
	}
	for _, path := range []string{config.Store, config.RunRoot} {
		if err := makePrivate(path); err != nil {
			return err
		}
	}
	if _, err := os.Lstat(config.RunRoot + "/worker.json"); !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("worker readiness path is already owned: %v", err)
	}
	binaries, err := componentManifest(config)
	if err != nil {
		return err
	}
	lease, err := openLease(config.Store, true)
	if err != nil {
		return err
	}
	defer lease.Close()
	if err := unix.Flock(int(lease.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return fmt.Errorf("persistent store already has a worker: %w", err)
	}
	var identity struct {
		Schema int    `json:"schema"`
		ID     string `json:"id"`
	}
	if err := readJSON(config.Store+"/store.json", &identity); errors.Is(err, os.ErrNotExist) {
		entries, err := os.ReadDir(config.Store)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.Name() != ".worker.lock" {
				return fmt.Errorf("refusing unidentified nonempty persistent store")
			}
		}
		identity.Schema = 1
		identity.ID = config.StoreID
		if err := writeNew(config.Store+"/store.json", identity); err != nil {
			return err
		}
	} else if err != nil || identity.Schema != 1 || identity.ID != config.StoreID {
		return fmt.Errorf("invalid persistent store identity: %v", err)
	}
	epoch, err := nonce()
	if err != nil {
		return err
	}
	for _, path := range []string{config.Store + "/epochs", config.Store + "/epochs/" + epoch} {
		if err := makePrivate(path); err != nil {
			return err
		}
	}
	// The pinned manager creates its data root as 0710, including after an
	// abrupt exit. It sits inside the separately protected 0700 store and
	// may be traversable by its root group, never writable by another user.
	managerRoot := config.Store + "/manager"
	if err := os.Mkdir(managerRoot, 0710); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	managerInfo, err := os.Lstat(managerRoot)
	if err != nil {
		return err
	}
	managerStat := managerInfo.Sys().(*syscall.Stat_t)
	if !managerInfo.IsDir() || managerStat.Uid != 0 || managerStat.Gid != 0 ||
		(managerInfo.Mode().Perm() != 0700 && managerInfo.Mode().Perm() != 0710) {
		return fmt.Errorf("unsafe Sysbox manager data root")
	}
	root := config.Store + "/epochs/" + epoch
	if err := delegate(); err != nil {
		return err
	}
	if err := os.Mkdir("/run/sysbox", 0700); err != nil {
		return fmt.Errorf("refusing existing Sysbox service directory: %w", err)
	}
	log, err := os.OpenFile(root+"/services.log", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer log.Close()
	manager, err := startChild(config.Components+"/sysbox-mgr", []string{"--disable-shiftfs", "--disable-inner-image-preload", "--disable-binfmt-misc", "--data-root", config.Store + "/manager", "--log", root + "/manager.log"}, lease, log)
	if err != nil {
		return err
	}
	defer manager.stop()
	status := Status{Schema: 1, Store: config.Store, StoreID: identity.ID, Epoch: epoch, Components: binaries,
		Manager:         manager.identity,
		BoundedCapacity: boundedRange(config),
		Lifecycle: lifecycle.Config{Root: root + "/controller", Runtime: config.Components + "/sysbox-runc", RuntimeSHA256: binaries["sysbox-runc"],
			Criu: config.Components + "/criu", CriuSHA256: binaries["criu"], ManagerRoot: config.Store + "/manager",
			MinFreeBytes: localMinFreeBytes, WriteReserve: localWriteReserve, BoundedStore: bounded,
			FDStoreSocket: config.RunRoot + "/retained-fds.sock"}}
	if bounded != nil {
		status.Lifecycle.MinFreeBytes, status.Lifecycle.WriteReserve = boundedMinFreeBytes, boundedWriteReserve
	}
	ready := false
	// Register after each service cleanup so failure collection runs first.
	collectFailure := func() {
		if !ready && runErr != nil {
			if err := recordStartupFailure(root, status, runErr); err != nil {
				fmt.Fprintf(log, "startup diagnostic unavailable: %v\n", err)
			}
		}
	}
	filesystem, err := startChild(config.Components+"/sysbox-fs", []string{"--mountpoint", config.Store + "/fs", "--log", root + "/filesystem.log"}, lease, log)
	if err != nil {
		// The manager is still owned and live when filesystem exec fails.
		runErr = err
		collectFailure()
		return err
	}
	defer filesystem.stop()
	defer collectFailure()
	status.Filesystem = filesystem.identity
	status.Supervisor, err = birth(os.Getpid())
	if err != nil {
		return err
	}
	status.SupervisorSHA256, err = hashFile("/proc/self/exe")
	if err != nil {
		return err
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return err
	}
	status.BootID = strings.TrimSpace(string(boot))
	deadline := time.Now().Add(60 * time.Second)
	for {
		if err := manager.alive(); err != nil {
			return err
		}
		if err := filesystem.alive(); err != nil {
			return err
		}
		status.ManagerPeer, err = peer("/run/sysbox/sysmgr.sock")
		if err == nil {
			status.FilesystemPeer, err = peer("/run/sysbox/sysfs-seccomp.sock")
		}
		if err == nil && descendant(status.ManagerPeer, status.Manager) && descendant(status.FilesystemPeer, status.Filesystem) {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("owned Sysbox services were not ready within 60 seconds: %v", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	retained, err := fdstore.Start(status.Lifecycle.FDStoreSocket)
	if err != nil {
		return err
	}
	defer retained.Close()
	engine, err := lifecycle.Open(status.Lifecycle)
	if err != nil {
		return err
	}
	engine.Close()
	status.ReadyAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := writeNew(config.RunRoot+"/worker.json", status); err != nil {
		return err
	}
	if err := writeNew(root+"/worker.json", status); err != nil {
		return err
	}
	if _, err := Current(config.RunRoot); err != nil {
		return err
	}
	// Start the probe inside PID 1 only after delegation and live verification.
	// Kubernetes exec probes would create tasks in the initially empty root.
	health, err := startHealth(config.HealthListen, func() error {
		current, err := Current(config.RunRoot)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(current, status) {
			return fmt.Errorf("worker readiness identity changed")
		}
		return nil
	})
	if err != nil {
		return err
	}
	defer health.Close()
	if err := json.NewEncoder(os.Stdout).Encode(status); err != nil {
		return err
	}
	ready = true
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			if bounded != nil {
				var current Status
				if err := readJSON(config.RunRoot+"/worker.json", &current); err != nil || !reflect.DeepEqual(current, status) {
					return fmt.Errorf("worker exit readiness identity changed: %v", err)
				}
				if err := os.Remove(config.RunRoot + "/worker.json"); err != nil {
					return err
				}
				engine, err := lifecycle.Open(status.Lifecycle)
				if err != nil {
					return err
				}
				// Full consumers retain several large RAM generations. Preserve
				// their complete verification; callers provide a 45-second Stop grace
				// for this bounded verification and both service shutdowns.
				exitContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				return errors.Join(engine.ReleaseDeletedEpochBases(exitContext), engine.Close())
			}
			return nil
		case <-ticker.C:
			if err := manager.alive(); err != nil {
				return err
			}
			if err := filesystem.alive(); err != nil {
				return err
			}
			reapOrphans(manager.identity.PID, filesystem.identity.PID)
		}
	}
}

func reapOrphans(owned ...int) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 1 {
			continue
		}
		skip := false
		for _, child := range owned {
			skip = skip || child == pid
		}
		if skip {
			continue
		}
		data, err := os.ReadFile("/proc/" + entry.Name() + "/stat")
		if err != nil {
			continue
		}
		index := strings.LastIndex(string(data), ")")
		if index < 0 {
			continue
		}
		fields := strings.Fields(string(data)[index+1:])
		if len(fields) < 2 || fields[1] != "1" || fields[0] != "Z" {
			continue
		}
		var status unix.WaitStatus
		unix.Wait4(pid, &status, unix.WNOHANG, nil)
	}
}
