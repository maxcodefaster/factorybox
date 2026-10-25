//go:build !factorybox_candidate_amd64 && !factorybox_candidate_arm64

package platform

import "fmt"

const ExecutionKernel = "7.2.8-9-omarchy"

func (info Info) CheckExecution() error {
	if info.OS != "linux" || info.Architecture != "amd64" ||
		info.PageSize != 4096 || info.Kernel != ExecutionKernel {
		return fmt.Errorf("unsupported execution platform: os=%s architecture=%s page_size=%d kernel=%s; reference is linux/amd64/4096/%s",
			info.OS, info.Architecture, info.PageSize, info.Kernel, ExecutionKernel)
	}
	return nil
}
