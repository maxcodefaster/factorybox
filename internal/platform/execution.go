package platform

import (
	"fmt"
	"strconv"
	"strings"
)

// The minimum kernel is set by the owned task timer reader. It loads a BPF task
// iterator that calls bpf_task_pt_regs (helper 175, Linux 5.15). The reader,
// cgroup.freeze, openat2, pidfd_getfd, time namespaces and LOOP_CONFIGURE are
// all older. Older kernels cannot run the reader and are refused.
const (
	MinimumKernelMajor = 5
	MinimumKernelMinor = 15
)

// Qualification values reported next to every admitted platform.
const (
	Verified   = "verified"
	Unverified = "unverified"
)

type verifiedPlatform struct{ Architecture, Kernel string }

// The two kernels on which the project ran its native suites. Admission never
// depends on this list. It only labels the report.
var verifiedPlatforms = []verifiedPlatform{
	{"amd64", "7.2.8-9-omarchy"},
	{"arm64", ReferenceKernel},
}

// ParseRelease reads the leading major.minor numbers of a kernel release such as
// 5.15.0-1070-azure. It reports false when the release has another shape.
func ParseRelease(release string) (major, minor int, ok bool) {
	fields := strings.SplitN(release, ".", 3)
	if len(fields) < 2 {
		return 0, 0, false
	}
	major, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0, 0, false
	}
	digits := fields[1]
	if end := strings.IndexFunc(digits, func(r rune) bool { return r < '0' || r > '9' }); end >= 0 {
		digits = digits[:end]
	}
	minor, err = strconv.Atoi(digits)
	return major, minor, err == nil
}

// Qualification reports whether the project ran its native suites on this
// exact architecture and kernel release.
func (info Info) Qualification() string {
	if info.Kernel != "" && info.Kernel == linkedKernel() && info.Architecture == linkedArchitecture() {
		return Verified
	}
	for _, platform := range verifiedPlatforms {
		if info.Architecture == platform.Architecture && info.Kernel == platform.Kernel {
			return Verified
		}
	}
	return Unverified
}

// CheckExecution validates the facts that the release tuple alone can decide.
// An unfamiliar release string is not a reason to refuse. A release older than
// the documented minimum is. The kernel facilities are checked by Inspect.
func (info Info) CheckExecution() error {
	var reasons []string
	if info.OS != "linux" {
		reasons = append(reasons, "operating system "+info.OS+" is not linux")
	}
	if info.Architecture != "amd64" && info.Architecture != "arm64" {
		reasons = append(reasons, "architecture "+info.Architecture+" is neither amd64 nor arm64")
	}
	if info.PageSize != 4096 {
		reasons = append(reasons, fmt.Sprintf("page size %d is not 4096", info.PageSize))
	}
	if info.Kernel == "" {
		reasons = append(reasons, "the kernel release is not reported")
	} else if major, minor, ok := ParseRelease(info.Kernel); ok &&
		(major < MinimumKernelMajor || major == MinimumKernelMajor && minor < MinimumKernelMinor) {
		reasons = append(reasons, fmt.Sprintf("kernel %s is older than the minimum %d.%d", info.Kernel, MinimumKernelMajor, MinimumKernelMinor))
	}
	if len(reasons) > 0 {
		return fmt.Errorf("unsupported execution platform: os=%s architecture=%s page_size=%d kernel=%s: %s; requires Linux amd64 or arm64, 4096-byte pages and kernel >= %d.%d",
			info.OS, info.Architecture, info.PageSize, info.Kernel, strings.Join(reasons, ", "), MinimumKernelMajor, MinimumKernelMinor)
	}
	return nil
}
