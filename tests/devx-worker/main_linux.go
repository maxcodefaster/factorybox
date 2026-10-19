//go:build linux

// Test-only entry point. Drop global mutation capabilities before the existing
// image's init and tools run. It is not part of the Factorybox runtime command.
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if os.Geteuid() != 0 || os.Getpid() != 1 || os.Getenv("FACTORYBOX_DEVX_WORKER") != "1" ||
		len(os.Args) < 2 || os.Args[1] != "/usr/local/bin/docker-init" {
		return fmt.Errorf("requires the exact owned developer-proof init invocation")
	}
	group, err := os.ReadFile("/proc/1/cgroup")
	if err != nil || strings.TrimSpace(string(group)) != "0::/" {
		return fmt.Errorf("requires the owned private cgroup namespace: %v", err)
	}
	data, err := os.ReadFile("/sys/fs/cgroup/memory.max")
	if err != nil {
		return err
	}
	memory, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	if err != nil || memory == 0 || memory > 3<<30 {
		return fmt.Errorf("requires its declared finite memory ceiling")
	}
	// Leave the private root empty before enabling domain controllers. Nested
	// Docker creates sibling workload cgroups under this root.
	leaf := "/sys/fs/cgroup/factorybox-devx-controller"
	if err := os.Mkdir(leaf, 0755); err != nil {
		return err
	}
	if err := os.WriteFile(leaf+"/cgroup.procs", []byte("1\n"), 0644); err != nil {
		return err
	}
	data, err = os.ReadFile("/sys/fs/cgroup/cgroup.procs")
	if err != nil || strings.TrimSpace(string(data)) != "" {
		return fmt.Errorf("private cgroup root still populated: %v", err)
	}
	data, err = os.ReadFile("/sys/fs/cgroup/cgroup.controllers")
	if err != nil || len(strings.Fields(string(data))) == 0 {
		return fmt.Errorf("private cgroup controllers unavailable: %v", err)
	}
	var enable []string
	for _, controller := range strings.Fields(string(data)) {
		enable = append(enable, "+"+controller)
	}
	if err := os.WriteFile("/sys/fs/cgroup/cgroup.subtree_control", []byte(strings.Join(enable, " ")), 0644); err != nil {
		return err
	}
	// SYS_MODULE, SYS_RAWIO, SYS_BOOT, SYS_TIME, SYSLOG. Exec applies the
	// reduced bounding set to effective and permitted capabilities too.
	for _, capability := range []uintptr{16, 17, 22, 25, 34} {
		if _, _, errno := syscall.RawSyscall(syscall.SYS_PRCTL, 24, capability, 0); errno != 0 {
			return fmt.Errorf("drop global capability %d: %w", capability, errno)
		}
	}
	return syscall.Exec(os.Args[1], os.Args[1:], os.Environ())
}
