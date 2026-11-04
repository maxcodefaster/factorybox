package platform

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

const (
	btfPath       = "/sys/kernel/btf/vmlinux"
	btfLimit      = 128 << 20
	bootIDPath    = "/proc/sys/kernel/random/boot_id"
	cgroupRoot    = "/sys/fs/cgroup"
	maxUserNSPath = "/proc/sys/user/max_user_namespaces"
)

// Inspect reads the running kernel. It reports every capability the runtime
// opens or loads at startup, and never depends on the release string beyond
// the documented minimum.
func Inspect() (Report, error) {
	info, err := Current()
	return Host{}.Inspect(info), err
}

// Inspect probes the host's facilities for the given tuple.
func (host Host) Inspect(info Info) Report {
	report := Report{Platform: info, Qualification: info.Qualification(),
		MinimumKernel: fmt.Sprintf("%d.%d", MinimumKernelMajor, MinimumKernelMinor), KernelConfig: "unavailable"}
	if info.OS != "linux" {
		return report
	}
	if boot, err := host.read(bootIDPath, 128); err == nil {
		report.BootID = strings.TrimSpace(string(boot))
	}
	add := func(capability Capability) { report.Capabilities = append(report.Capabilities, capability) }
	add(host.probeKernelRelease(info))
	add(host.probeCgroup())
	add(host.probeNamespaces())
	add(host.probeCheckpointRestore())
	add(host.probeBTF())
	add(host.probeSeccomp())
	add(host.probeLoopDevice())
	add(host.probeFilesystems())
	add(host.probeFuse())
	add(host.probeNetwork())
	if options, source := host.kernelConfig(info.Kernel); options != nil {
		report.KernelConfig = source
		for _, capability := range configCapabilities(options, source) {
			add(capability)
		}
	}
	return report
}

func (host Host) probeKernelRelease(info Info) Capability {
	capability := Capability{Name: "kernel-release", Status: OK,
		Detail: fmt.Sprintf("%s (%s) meets the minimum %d.%d", info.Kernel, info.Architecture, MinimumKernelMajor, MinimumKernelMinor)}
	if _, _, ok := ParseRelease(info.Kernel); !ok {
		capability.Status = Unknown
		capability.Detail = "release " + info.Kernel + " has no major.minor prefix; the minimum is decided by the capabilities below"
	} else if info.CheckExecution() != nil {
		capability.Status = Failed
		capability.Detail = fmt.Sprintf("%s is older than the minimum %d.%d (BPF task iterator helper bpf_task_pt_regs)", info.Kernel, MinimumKernelMajor, MinimumKernelMinor)
		capability.Remediation = fmt.Sprintf("run the worker on a node image with Linux >= %d.%d", MinimumKernelMajor, MinimumKernelMinor)
	}
	return capability
}

func (host Host) probeCgroup() Capability {
	capability := Capability{Name: "cgroup-v2", Status: OK, Detail: "unified hierarchy at " + cgroupRoot}
	controllers, err := host.read(cgroupRoot+"/cgroup.controllers", 4096)
	if err != nil {
		capability.Status = Failed
		capability.Detail = cgroupRoot + "/cgroup.controllers is absent, so cgroup v2 is not mounted"
		capability.Remediation = "boot the node with cgroup v2 (systemd.unified_cgroup_hierarchy=1) and run the worker in a private cgroup namespace"
		return capability
	}
	if !contains(strings.Fields(string(controllers)), "memory") {
		capability.Status = Failed
		capability.Detail = "the memory controller is not enabled for the worker cgroup (controllers: " + strings.TrimSpace(string(controllers)) + ")"
		capability.Remediation = "give the worker a finite memory limit so its cgroup delegates the memory controller"
	}
	return capability
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func (host Host) probeNamespaces() Capability {
	capability := Capability{Name: "namespaces", Status: OK, Detail: "pid, user, net, mnt, uts, ipc, cgroup and time namespaces"}
	var missing []string
	for _, name := range []string{"pid", "user", "net", "mnt", "uts", "ipc", "cgroup", "time"} {
		if !host.exists("/proc/self/ns/" + name) {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		capability.Status = Failed
		capability.Detail = "namespace types absent from /proc/self/ns: " + strings.Join(missing, ", ")
		capability.Remediation = "use a kernel with every namespace type enabled; time namespaces need Linux 5.6 and CONFIG_TIME_NS"
		return capability
	}
	if data, err := host.read(maxUserNSPath, 64); err == nil {
		if limit, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && limit == 0 {
			capability.Status = Failed
			capability.Detail = maxUserNSPath + " is 0, so user namespaces are disabled"
			capability.Remediation = "allow user namespaces on the node (kernel.max_user_namespaces > 0)"
		}
	}
	return capability
}

func (host Host) probeCheckpointRestore() Capability {
	capability := Capability{Name: "criu-ns-last-pid", Status: OK, Detail: "/proc/sys/kernel/ns_last_pid is present"}
	if !host.exists("/proc/sys/kernel/ns_last_pid") {
		capability.Status = Failed
		capability.Detail = "/proc/sys/kernel/ns_last_pid is absent; CRIU restores process IDs through it"
		capability.Remediation = "use a kernel built with CONFIG_CHECKPOINT_RESTORE=y and mount /proc read-write for the worker"
	}
	return capability
}

func (host Host) probeBTF() Capability {
	capability := Capability{Name: "bpf-btf", Status: OK, Detail: btfPath + " describes the BPF task iterator"}
	data, err := host.read(btfPath, btfLimit)
	switch {
	case err != nil:
		capability.Status = Failed
		capability.Detail = btfPath + " is unreadable: " + err.Error()
		capability.Remediation = "use a kernel with CONFIG_DEBUG_INFO_BTF=y and mount /sys for the worker"
	case !bytes.Contains(data, []byte("bpf_iter__task\x00")) || !bytes.Contains(data, []byte("bpf_iter_meta\x00")):
		capability.Status = Failed
		capability.Detail = "the kernel BTF has no bpf_iter__task context; BPF task iterators are not built"
		capability.Remediation = "use a kernel with CONFIG_BPF_SYSCALL=y, CONFIG_DEBUG_INFO_BTF=y and task iterators (Linux >= 5.8)"
	}
	return capability
}

func (host Host) probeSeccomp() Capability {
	capability := Capability{Name: "seccomp", Status: OK, Detail: "seccomp filters are available"}
	status, err := host.read("/proc/self/status", 1<<16)
	if err != nil {
		capability.Status = Unknown
		capability.Detail = "process status is unreadable: " + err.Error()
	} else if !bytes.Contains(status, []byte("\nSeccomp:")) {
		capability.Status = Failed
		capability.Detail = "/proc/self/status reports no Seccomp field"
		capability.Remediation = "use a kernel with CONFIG_SECCOMP_FILTER=y; Sysbox emulates system calls through seccomp notification"
	}
	return capability
}

func (host Host) probeLoopDevice() Capability {
	capability := Capability{Name: "loop-device", Status: OK, Detail: "/dev/loop-control is present"}
	if !host.exists("/dev/loop-control") {
		capability.Status = Failed
		capability.Detail = "/dev/loop-control is absent; the bounded store attaches its image through it"
		capability.Remediation = "run the worker privileged with host devices and load the loop module (modprobe loop)"
	}
	return capability
}

func (host Host) probeFilesystems() Capability {
	capability := Capability{Name: "filesystems", Status: OK, Detail: "ext4 and overlay are registered"}
	data, err := host.read("/proc/filesystems", 1<<16)
	if err != nil {
		capability.Status = Unknown
		capability.Detail = "/proc/filesystems is unreadable: " + err.Error()
		return capability
	}
	var missing []string
	for _, name := range []string{"ext4", "overlay"} {
		if !bytes.Contains(data, []byte("\t"+name+"\n")) {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		capability.Status = Unknown
		capability.Detail = strings.Join(missing, ", ") + " not registered; a loadable module may load on first mount"
	}
	return capability
}

func (host Host) probeFuse() Capability {
	capability := Capability{Name: "fuse", Status: OK, Detail: "/dev/fuse is present; sysbox-fs mounts its emulation tree through it"}
	if !host.exists("/dev/fuse") {
		capability.Status = Failed
		capability.Detail = "/dev/fuse is absent; sysbox-fs cannot mount its filesystem"
		capability.Remediation = "run the worker privileged with host devices and load the fuse module (modprobe fuse)"
	}
	return capability
}

func (host Host) probeNetwork() Capability {
	capability := Capability{Name: "network", Status: OK, Detail: "veth, conntrack and NAT are loaded and IPv4 forwarding is controllable"}
	if !host.exists("/proc/sys/net/ipv4/ip_forward") {
		capability.Status = Failed
		capability.Detail = "/proc/sys/net/ipv4/ip_forward is absent; the sandbox network must enable forwarding in its own network namespace"
		capability.Remediation = "run the worker in a private network namespace with a writable /proc/sys/net"
		return capability
	}
	var unloaded []string
	for _, module := range []string{"veth", "nf_conntrack", "nf_nat"} {
		if !host.exists("/sys/module/" + module) {
			unloaded = append(unloaded, module)
		}
	}
	if len(unloaded) > 0 {
		capability.Status = Unknown
		capability.Detail = strings.Join(unloaded, ", ") + " not loaded or built in; they may load on first use"
	}
	return capability
}

// CheckHost inspects the running kernel and refuses only on failed capabilities.
func CheckHost() (Report, error) {
	report, err := Inspect()
	if err != nil {
		return report, err
	}
	return report, report.Err()
}
