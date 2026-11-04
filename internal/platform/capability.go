package platform

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Capability states. Only Failed refuses execution. Unknown means that this
// process cannot see the evidence (for example a kernel config or an unloaded
// module). The runtime then relies on the exact error of the first real use.
const (
	OK      = "ok"
	Failed  = "failed"
	Unknown = "unknown"
)

type Capability struct {
	Name        string `json:"name"`
	Status      string `json:"status"`
	Detail      string `json:"detail,omitempty"`
	Remediation string `json:"remediation,omitempty"`
}

type Report struct {
	Platform      Info         `json:"platform"`
	Qualification string       `json:"qualification"`
	MinimumKernel string       `json:"minimum_kernel"`
	BootID        string       `json:"boot_id,omitempty"`
	KernelConfig  string       `json:"kernel_config"`
	Capabilities  []Capability `json:"capabilities"`
}

// Summary is the compact form retained in worker status.
type Summary struct {
	Info
	Qualification string   `json:"qualification"`
	Unknown       []string `json:"unknown_capabilities,omitempty"`
}

func (report Report) Summary() Summary {
	summary := Summary{Info: report.Platform, Qualification: report.Qualification}
	for _, capability := range report.Capabilities {
		if capability.Status == Unknown {
			summary.Unknown = append(summary.Unknown, capability.Name)
		}
	}
	return summary
}

func (report Report) Failures() []Capability {
	var failed []Capability
	for _, capability := range report.Capabilities {
		if capability.Status == Failed {
			failed = append(failed, capability)
		}
	}
	return failed
}

// Err lists every failing capability with its remediation. A tuple failure
// from Info.CheckExecution is reported first.
func (report Report) Err() error {
	err := report.Platform.CheckExecution()
	failed := report.Failures()
	if err == nil && len(failed) == 0 {
		return nil
	}
	var message strings.Builder
	if err != nil {
		message.WriteString(err.Error())
	} else {
		fmt.Fprintf(&message, "kernel %s lacks required capabilities", report.Platform.Kernel)
	}
	for _, capability := range failed {
		fmt.Fprintf(&message, "\n  missing %s: %s; %s", capability.Name, capability.Detail, capability.Remediation)
	}
	return fmt.Errorf("%s", message.String())
}

// Host reads kernel facts below Root ("" means "/"). Tests point it at a
// fixture tree.
type Host struct{ Root string }

func (host Host) path(name string) string { return filepath.Join(host.Root, name) }

func (host Host) exists(name string) bool {
	_, err := os.Stat(host.path(name))
	return err == nil
}

func (host Host) read(name string, limit int64) ([]byte, error) {
	file, err := os.Open(host.path(name))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(io.LimitReader(file, limit))
}

// kernelConfig returns the option table and the file it came from. Worker
// containers usually see no config. Then every config-only decision is Unknown.
func (host Host) kernelConfig(release string) (map[string]string, string) {
	candidates := []string{"/proc/config.gz", "/boot/config-" + release, "/lib/modules/" + release + "/build/.config", "/usr/lib/modules/" + release + "/config"}
	for _, name := range candidates {
		data, err := host.read(name, 32<<20)
		if err != nil {
			continue
		}
		if strings.HasSuffix(name, ".gz") {
			reader, err := gzip.NewReader(bytes.NewReader(data))
			if err != nil {
				continue
			}
			data, err = io.ReadAll(io.LimitReader(reader, 32<<20))
			if err != nil {
				continue
			}
		}
		return parseConfig(string(data)), name
	}
	return nil, ""
}

func parseConfig(text string) map[string]string {
	options := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		switch {
		case strings.HasPrefix(line, "CONFIG_"):
			if name, value, ok := strings.Cut(line, "="); ok {
				options[name] = value
			}
		case strings.HasPrefix(line, "# CONFIG_") && strings.HasSuffix(line, " is not set"):
			options[strings.TrimSuffix(strings.TrimPrefix(line, "# "), " is not set")] = "n"
		}
	}
	return options
}

// configGroup is one capability and every kernel option behind it. An option
// explicitly disabled in the config fails the capability. An option the config
// does not mention (removed or renamed in a newer kernel) is not evidence.
type configGroup struct {
	name, purpose, remediation string
	options                    []string
}

var configGroups = []configGroup{
	{"criu-checkpoint-restore", "CRIU needs ns_last_pid, kcmp and PID control", "use a kernel built with CONFIG_CHECKPOINT_RESTORE=y",
		[]string{"CONFIG_CHECKPOINT_RESTORE"}},
	{"criu-namespaces", "CRIU checkpoints PID, NET, IPC, UTS, USER and time namespaces", "enable CONFIG_NAMESPACES, PID_NS, NET_NS, IPC_NS, UTS_NS, USER_NS and TIME_NS",
		[]string{"CONFIG_NAMESPACES", "CONFIG_PID_NS", "CONFIG_NET_NS", "CONFIG_IPC_NS", "CONFIG_UTS_NS", "CONFIG_USER_NS", "CONFIG_TIME_NS"}},
	{"criu-file-descriptors", "CRIU restores epoll, eventfd, inotify, file handles and reads page maps", "enable CONFIG_FHANDLE, EPOLL, EVENTFD, INOTIFY_USER and PROC_PAGE_MONITOR",
		[]string{"CONFIG_FHANDLE", "CONFIG_EPOLL", "CONFIG_EVENTFD", "CONFIG_INOTIFY_USER", "CONFIG_PROC_PAGE_MONITOR"}},
	{"criu-socket-diagnostics", "CRIU enumerates sockets through the *_DIAG netlink families", "enable CONFIG_UNIX_DIAG, INET_DIAG, PACKET_DIAG and NETLINK_DIAG as y or m",
		[]string{"CONFIG_UNIX_DIAG", "CONFIG_INET_DIAG", "CONFIG_PACKET_DIAG", "CONFIG_NETLINK_DIAG"}},
	{"bpf-task-iterator", "the task timer reader loads a BPF task iterator that reads BTF", "enable CONFIG_BPF_SYSCALL=y and CONFIG_DEBUG_INFO_BTF=y",
		[]string{"CONFIG_BPF_SYSCALL", "CONFIG_DEBUG_INFO_BTF"}},
	{"block-loop", "the bounded store attaches its ext4 image through a loop device", "enable CONFIG_BLK_DEV_LOOP as y or m",
		[]string{"CONFIG_BLK_DEV_LOOP"}},
	{"filesystem-ext4", "the bounded store mounts ext4", "enable CONFIG_EXT4_FS as y or m",
		[]string{"CONFIG_EXT4_FS"}},
	{"network-veth", "outbound sandbox networking uses an owned veth pair", "enable CONFIG_VETH as y or m",
		[]string{"CONFIG_VETH"}},
	{"network-nat", "outbound sandbox networking uses conntrack and MASQUERADE rules", "enable CONFIG_NF_CONNTRACK and CONFIG_NF_NAT as y or m",
		[]string{"CONFIG_NF_CONNTRACK", "CONFIG_NF_NAT"}},
	{"sysbox-seccomp-notify", "Sysbox emulates system calls through seccomp user notification", "enable CONFIG_SECCOMP_FILTER=y",
		[]string{"CONFIG_SECCOMP_FILTER"}},
}

func configCapabilities(options map[string]string, source string) []Capability {
	var capabilities []Capability
	for _, group := range configGroups {
		capability := Capability{Name: "config:" + group.name, Status: OK, Detail: "from " + source}
		var disabled []string
		for _, option := range group.options {
			if options[option] == "n" {
				disabled = append(disabled, option)
			}
		}
		if len(disabled) > 0 {
			capability.Status = Failed
			capability.Detail = group.purpose + "; " + strings.Join(disabled, ", ") + " is disabled in " + source
			capability.Remediation = group.remediation
		}
		capabilities = append(capabilities, capability)
	}
	return capabilities
}
