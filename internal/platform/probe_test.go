package platform

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T) Host {
	t.Helper()
	root := t.TempDir()
	write := func(name, content string) {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("sys/fs/cgroup/cgroup.controllers", "cpuset cpu io memory pids\n")
	for _, name := range []string{"pid", "user", "net", "mnt", "uts", "ipc", "cgroup", "time"} {
		write("proc/self/ns/"+name, "")
	}
	write("proc/sys/user/max_user_namespaces", "63704\n")
	write("proc/sys/kernel/ns_last_pid", "100\n")
	write("proc/sys/kernel/random/boot_id", "4a1b0c2d-0000-0000-0000-000000000001\n")
	write("proc/sys/net/ipv4/ip_forward", "0\n")
	write("sys/kernel/btf/vmlinux", "\x00bpf_iter__task\x00bpf_iter_meta\x00")
	write("proc/self/status", "Name:\tx\nSeccomp:\t0\nSeccomp_filters:\t0\n")
	write("proc/filesystems", "nodev\tproc\n\text4\nnodev\toverlay\nnodev\tfuse\n")
	write("dev/loop-control", "")
	write("dev/fuse", "")
	for _, module := range []string{"veth", "nf_conntrack", "nf_nat"} {
		write("sys/module/"+module+"/refcnt", "0\n")
	}
	return Host{Root: root}
}

func (host Host) remove(t *testing.T, name string) {
	t.Helper()
	if err := os.RemoveAll(host.path(name)); err != nil {
		t.Fatal(err)
	}
}

func status(report Report, name string) string {
	for _, capability := range report.Capabilities {
		if capability.Name == name {
			return capability.Status
		}
	}
	return "missing"
}

// Ubuntu linux-azure on AKS: not one of the two natively run kernels.
var aks = Info{"linux", "amd64", 4096, "5.15.0-1070-azure"}

func TestCapableUnknownKernelIsAdmittedAndLabelledUnverified(t *testing.T) {
	report := fixture(t).Inspect(aks)
	if err := report.Err(); err != nil {
		t.Fatal(err)
	}
	if report.Qualification != Unverified || report.BootID == "" || report.MinimumKernel != "5.15" {
		t.Fatalf("unexpected report %+v", report)
	}
	if status(report, "kernel-release") != OK || status(report, "cgroup-v2") != OK || status(report, "bpf-btf") != OK {
		t.Fatalf("capable fixture failed %+v", report.Capabilities)
	}
	if report.Summary().Qualification != Unverified || report.Summary().Kernel != aks.Kernel {
		t.Fatal("summary does not retain the exact kernel and qualification")
	}
}

func TestEveryMissingCapabilityIsReportedWithRemediation(t *testing.T) {
	host := fixture(t)
	for _, name := range []string{"sys/fs/cgroup/cgroup.controllers", "proc/self/ns/time", "proc/sys/kernel/ns_last_pid",
		"sys/kernel/btf/vmlinux", "dev/loop-control", "dev/fuse", "proc/sys/net/ipv4/ip_forward"} {
		host.remove(t, name)
	}
	report := host.Inspect(aks)
	err := report.Err()
	if err == nil {
		t.Fatal("a host without required facilities was admitted")
	}
	for _, name := range []string{"cgroup-v2", "namespaces", "criu-ns-last-pid", "bpf-btf", "loop-device", "fuse", "network"} {
		if status(report, name) != Failed || !strings.Contains(err.Error(), "missing "+name+":") {
			t.Fatal("failing capability not reported", name, err)
		}
	}
	for _, capability := range report.Failures() {
		if capability.Remediation == "" {
			t.Fatal("failing capability has no remediation", capability.Name)
		}
	}
}

func TestBTFWithoutTaskIteratorFailsAndOldKernelReportsMinimum(t *testing.T) {
	host := fixture(t)
	if err := os.WriteFile(host.path("sys/kernel/btf/vmlinux"), []byte("\x00task_struct\x00"), 0644); err != nil {
		t.Fatal(err)
	}
	report := host.Inspect(Info{"linux", "amd64", 4096, "5.10.0-1000-azure"})
	if status(report, "bpf-btf") != Failed || status(report, "kernel-release") != Failed {
		t.Fatalf("old kernel without iterators was admitted %+v", report.Capabilities)
	}
	if err := report.Err(); err == nil || !strings.Contains(err.Error(), "older than the minimum 5.15") {
		t.Fatal(err)
	}
}

func TestUnloadedModulesAndUnknownReleaseDoNotRefuse(t *testing.T) {
	host := fixture(t)
	host.remove(t, "sys/module")
	host.remove(t, "proc/filesystems")
	report := host.Inspect(Info{"linux", "arm64", 4096, "custom-build"})
	if err := report.Err(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"kernel-release", "network", "filesystems"} {
		if status(report, name) != Unknown {
			t.Fatal("missing evidence was not reported as unknown", name, report.Capabilities)
		}
	}
	if len(report.Summary().Unknown) != 3 {
		t.Fatal("summary lost the unknown capabilities", report.Summary())
	}
}

func TestUserNamespacesDisabledFails(t *testing.T) {
	host := fixture(t)
	if err := os.WriteFile(host.path("proc/sys/user/max_user_namespaces"), []byte("0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if status(host.Inspect(aks), "namespaces") != Failed {
		t.Fatal("disabled user namespaces were admitted")
	}
}

func TestKernelConfigFailsOnlyExplicitlyDisabledOptions(t *testing.T) {
	host := fixture(t)
	config := "CONFIG_CHECKPOINT_RESTORE=y\n# CONFIG_VETH is not set\nCONFIG_BLK_DEV_LOOP=m\nCONFIG_NF_NAT=m\n"
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	writer.Write([]byte(config))
	writer.Close()
	if err := os.WriteFile(host.path("proc/config.gz"), compressed.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	report := host.Inspect(aks)
	if report.KernelConfig != "/proc/config.gz" {
		t.Fatal("kernel config was not read", report.KernelConfig)
	}
	if status(report, "config:network-veth") != Failed || status(report, "config:block-loop") != OK ||
		status(report, "config:criu-checkpoint-restore") != OK || status(report, "config:criu-namespaces") != OK {
		t.Fatalf("config decisions are wrong %+v", report.Capabilities)
	}
	if err := report.Err(); err == nil || !strings.Contains(err.Error(), "CONFIG_VETH is disabled") {
		t.Fatal(err)
	}
}

func TestBootConfigIsFoundByRelease(t *testing.T) {
	host := fixture(t)
	if err := os.MkdirAll(host.path("boot"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(host.path("boot/config-"+aks.Kernel), []byte("# CONFIG_EXT4_FS is not set\n"), 0644); err != nil {
		t.Fatal(err)
	}
	report := host.Inspect(aks)
	if status(report, "config:filesystem-ext4") != Failed {
		t.Fatal("boot config was not used", report.Capabilities)
	}
}

func TestNonLinuxReportOnlyHasTheTuple(t *testing.T) {
	report := fixture(t).Inspect(Info{"darwin", "arm64", 16384, "24.0.0"})
	if len(report.Capabilities) != 0 || report.Err() == nil {
		t.Fatal("non-Linux host was probed or admitted")
	}
}
