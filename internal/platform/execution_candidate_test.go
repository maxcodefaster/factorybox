//go:build factorybox_candidate_amd64 || factorybox_candidate_arm64

package platform

import "testing"

// Candidate builds use the same capability gate as ordinary builds. Only the
// kernel linked by the native candidate runner is labelled verified.
func TestCandidateLinkedKernelIsVerifiedButAdmissionIsUnchanged(t *testing.T) {
	original := candidateKernel
	t.Cleanup(func() { candidateKernel = original })
	info := Info{"linux", candidateArchitecture, 4096, "test-only-kernel"}
	candidateKernel = ""
	if info.CheckExecution() != nil || info.Qualification() != Unverified {
		t.Fatal("unlinked candidate changed admission or qualification")
	}
	candidateKernel = info.Kernel
	if err := info.CheckExecution(); err != nil || info.Qualification() != Verified {
		t.Fatal("linked native kernel is not verified", err)
	}
	if info.CheckReference() == nil {
		t.Fatal("candidate was promoted to the historical reference")
	}
	other := "amd64"
	if candidateArchitecture == "amd64" {
		other = "arm64"
	}
	for _, info := range []Info{
		{"linux", other, 4096, candidateKernel},
	} {
		if info.Qualification() != Unverified {
			t.Fatal("candidate verified another architecture", info)
		}
	}
	for _, info := range []Info{
		{"darwin", candidateArchitecture, 4096, candidateKernel},
		{"linux", candidateArchitecture, 65536, candidateKernel},
		{"linux", candidateArchitecture, 4096, "4.19.0"},
	} {
		if info.CheckExecution() == nil {
			t.Fatal("candidate admitted an unsupported tuple", info)
		}
	}
}
