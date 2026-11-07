//go:build linux

package worker

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

const reexecMarker = "FACTORYBOX_CGROUPNS_REEXEC"

// checkPrivateCgroupRoot reports why the worker's /proc/1/cgroup is not the
// root of a private cgroup namespace, naming the value it read.
func checkPrivateCgroupRoot(group []byte, readErr error) error {
	if readErr != nil {
		return fmt.Errorf("worker cannot read its cgroup: %w", readErr)
	}
	if value := strings.TrimSpace(string(group)); value != "0::/" {
		return fmt.Errorf("worker lacks a private cgroup namespace: /proc/1/cgroup is %q", value)
	}
	return nil
}

// EnsurePrivateCgroupNamespace gives PID 1 a cgroup namespace rooted at its own
// cgroup. Docker creates one for a privileged container on cgroup v2, but
// containerd shares the host namespace with every privileged Pod, so the worker
// would see its full host path instead of "0::/". A privileged worker creates
// the namespace itself, replaces the cgroup2 view (detached lazily because the
// Go runtime keeps cgroup files open) and executes its own image again. Docker
// and Kubernetes then present the same private root. It does nothing when the
// root is already private.
func EnsurePrivateCgroupNamespace() error {
	if os.Getpid() != 1 {
		return nil
	}
	group, err := os.ReadFile("/proc/1/cgroup")
	if err == nil && checkPrivateCgroupRoot(group, nil) == nil {
		return nil
	}
	if os.Getenv(reexecMarker) != "" {
		return fmt.Errorf("worker could not enter a private cgroup namespace")
	}
	runtime.LockOSThread()
	if err := unix.Unshare(unix.CLONE_NEWCGROUP); err != nil {
		return fmt.Errorf("worker cannot create a private cgroup namespace: %w", err)
	}
	if err := unix.Unmount("/sys/fs/cgroup", unix.MNT_DETACH); err != nil {
		return fmt.Errorf("worker cannot replace the shared cgroup view: %w", err)
	}
	if err := unix.Mount("cgroup2", "/sys/fs/cgroup", "cgroup2", unix.MS_NOSUID|unix.MS_NODEV|unix.MS_NOEXEC, ""); err != nil {
		return fmt.Errorf("worker cannot mount its private cgroup view: %w", err)
	}
	return reexecute()
}

// JoinWorkerCgroupNamespace moves a process started by `exec` into the cgroup
// namespace of the worker (PID 1). Docker enters it automatically. runc does
// not when the container was created without a cgroup namespace entry, and the
// lifecycle commands compare cgroup paths relative to the worker root. A caller
// without the privilege keeps its own view; the lifecycle checks then report
// the mismatch by name.
func JoinWorkerCgroupNamespace() error {
	if os.Getpid() == 1 {
		return nil
	}
	want, err := os.Readlink("/proc/1/ns/cgroup")
	if err != nil {
		return nil
	}
	have, err := os.Readlink("/proc/self/ns/cgroup")
	if err != nil || have == want {
		return nil
	}
	if os.Getenv(reexecMarker) != "" {
		return fmt.Errorf("process could not join the worker cgroup namespace")
	}
	runtime.LockOSThread()
	fd, err := unix.Open("/proc/1/ns/cgroup", unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil
	}
	defer unix.Close(fd)
	if err := unix.Setns(fd, unix.CLONE_NEWCGROUP); err != nil {
		return nil
	}
	return reexecute()
}

// reexecute replaces the process with itself so every thread, including the Go
// runtime's helper threads, runs in the namespaces of the calling thread.
func reexecute() error {
	executable, err := os.Readlink("/proc/self/exe")
	if err != nil {
		return err
	}
	return syscall.Exec(executable, os.Args, append(os.Environ(), reexecMarker+"=1"))
}
