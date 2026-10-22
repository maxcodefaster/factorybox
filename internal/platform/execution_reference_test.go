//go:build !factorybox_candidate_amd64

package platform

import "testing"

func TestOrdinaryExecutionRequiresReference(t *testing.T) {
	if err := (Info{"linux", "amd64", 4096, ExecutionKernel}).CheckExecution(); err != nil {
		t.Fatal(err)
	}
	for _, info := range []Info{
		{"linux", "arm64", 4096, ReferenceKernel},
		{"linux", "amd64", 4096, ReferenceKernel},
		{"linux", "arm64", 4096, ExecutionKernel},
		{"darwin", "amd64", 4096, ExecutionKernel},
		{"linux", "amd64", 16384, ExecutionKernel},
		{"linux", "amd64", 65536, ExecutionKernel},
		{"linux", "amd64", 4096, "unqualified"},
		{"linux", "amd64", 4096, ""},
	} {
		if info.CheckExecution() == nil {
			t.Fatal("ordinary execution admitted an unqualified tuple", info)
		}
	}
}
