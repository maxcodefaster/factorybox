//go:build !factorybox_candidate_amd64 && !factorybox_candidate_arm64

package platform

import "testing"

func TestOrdinaryARM64RequiresExactNativeTuple(t *testing.T) {
	if err := (Info{"linux", "arm64", 4096, ReferenceKernel}).CheckExecution(); err != nil {
		t.Fatal("qualified native ARM64 tuple was rejected", err)
	}
	for _, info := range []Info{
		{"darwin", "arm64", 4096, ReferenceKernel},
		{"linux", "arm64", 16384, ReferenceKernel},
		{"linux", "arm64", 65536, ReferenceKernel},
		{"linux", "arm64", 4096, ExecutionKernel},
		{"linux", "arm64", 4096, ""},
		{"linux", "arm64", 4096, ReferenceKernel + "-different"},
		{"linux", "arm64", 4096, "unqualified"},
		{"linux", "arm", 4096, ReferenceKernel},
		{"linux", "riscv64", 4096, ReferenceKernel},
	} {
		if info.CheckExecution() == nil {
			t.Fatal("ordinary ARM64 admission accepted an unqualified tuple", info)
		}
	}
}
