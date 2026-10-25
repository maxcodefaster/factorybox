//go:build factorybox_candidate_amd64 || factorybox_candidate_arm64

package platform

import (
	"fmt"
	"strings"
)

// Only the explicit native candidate runner links this value. An ordinary
// build has no candidate admission path, and a tag without a kernel pin fails.
var candidateKernel string

func (info Info) CheckExecution() error {
	if candidateKernel == "" || info.OS != "linux" || info.Architecture != candidateArchitecture ||
		info.PageSize != 4096 || info.Kernel != candidateKernel {
		return fmt.Errorf("unqualified %s test build requires its exact linked Linux/%s/4096/kernel tuple",
			strings.ToUpper(candidateArchitecture), candidateArchitecture)
	}
	return nil
}
