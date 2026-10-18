package platform

import "testing"

// Retains the prototype's six unsupported tuple cases and adds the host-OS guard.
func TestRejectsUntestedPlatforms(t *testing.T) {
	if err := (Info{"linux", "arm64", 4096, ReferenceKernel}).CheckReference(); err != nil {
		t.Fatal(err)
	}
	for _, info := range []Info{
		{"linux", "amd64", 4096, ReferenceKernel},
		{"linux", "arm", 4096, ReferenceKernel},
		{"linux", "arm64", 16384, ReferenceKernel},
		{"linux", "arm64", 65536, ReferenceKernel},
		{"linux", "arm64", 4096, "7.0.15"},
		{"linux", "arm64", 4096, ""},
		{"darwin", "arm64", 4096, ReferenceKernel},
	} {
		if info.CheckReference() == nil {
			t.Fatal("untested platform accepted", info)
		}
	}
}
