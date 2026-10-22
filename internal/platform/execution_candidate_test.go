//go:build factorybox_candidate_amd64

package platform

import "testing"

func TestCandidateExecutionRequiresExactLinkedTuple(t *testing.T) {
	original := candidateKernel
	t.Cleanup(func() { candidateKernel = original })
	info := Info{"linux", "amd64", 4096, "test-only-kernel"}
	candidateKernel = ""
	if info.CheckExecution() == nil {
		t.Fatal("candidate tag without kernel pin admitted execution")
	}
	candidateKernel = info.Kernel
	if err := info.CheckExecution(); err != nil {
		t.Fatal(err)
	}
	if info.CheckReference() == nil {
		t.Fatal("candidate was promoted to the reference")
	}
	for _, other := range []Info{
		{"linux", "arm64", 4096, candidateKernel},
		{"darwin", "amd64", 4096, candidateKernel},
		{"linux", "amd64", 65536, candidateKernel},
		{"linux", "amd64", 4096, "different-kernel"},
	} {
		if other.CheckExecution() == nil {
			t.Fatal("candidate admitted an unpinned tuple", other)
		}
	}
}
