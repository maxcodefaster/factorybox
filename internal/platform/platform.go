// Package platform describes the reference platform. Passing this check alone
// does not qualify filesystem, userfaultfd, runtime installation or recovery.
package platform

import (
	"fmt"
	"os"
	"runtime"
)

const ReferenceKernel = "7.0.14-orbstack-00380-ga7e0a2dc9535"

type Info struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	PageSize     int    `json:"page_size"`
	Kernel       string `json:"kernel"`
}

func Current() (Info, error) {
	info := Info{OS: runtime.GOOS, Architecture: runtime.GOARCH, PageSize: os.Getpagesize()}
	kernel, err := kernelRelease()
	info.Kernel = kernel
	return info, err
}

func (info Info) CheckReference() error {
	if info.OS != "linux" || info.Architecture != "arm64" ||
		info.PageSize != 4096 || info.Kernel != ReferenceKernel {
		return fmt.Errorf("unsupported snapshot platform: os=%s architecture=%s page_size=%d kernel=%s; reference is linux/arm64/4096/%s",
			info.OS, info.Architecture, info.PageSize, info.Kernel, ReferenceKernel)
	}
	return nil
}
