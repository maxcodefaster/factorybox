//go:build !factorybox_candidate_amd64 && !factorybox_candidate_arm64

package platform

import "fmt"

const ExecutionKernel = "7.2.8-9-omarchy"
const ARM64ExecutionKernel = ReferenceKernel

func (info Info) CheckExecution() error {
	qualified := info.Architecture == "amd64" && info.Kernel == ExecutionKernel ||
		info.Architecture == "arm64" && info.Kernel == ARM64ExecutionKernel
	if info.OS != "linux" || info.PageSize != 4096 || !qualified {
		return fmt.Errorf("unsupported execution platform: os=%s architecture=%s page_size=%d kernel=%s; supported tuples are linux/amd64/4096/%s and linux/arm64/4096/%s",
			info.OS, info.Architecture, info.PageSize, info.Kernel, ExecutionKernel, ARM64ExecutionKernel)
	}
	return nil
}
