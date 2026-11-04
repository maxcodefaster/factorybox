package platform

import (
	"strings"
	"testing"
)

func TestExecutionAdmitsByCapabilityNotByReleaseString(t *testing.T) {
	for _, info := range []Info{
		{"linux", "amd64", 4096, "7.2.8-9-omarchy"},
		{"linux", "arm64", 4096, ReferenceKernel},
		{"linux", "amd64", 4096, "5.15.0-1070-azure"},
		{"linux", "amd64", 4096, "6.8.0-1017-azure"},
		{"linux", "arm64", 4096, "6.1.0-18-arm64"},
		{"linux", "amd64", 4096, "5.15"},
		{"linux", "amd64", 4096, "unfamiliar-release"},
		{"linux", "arm64", 4096, ReferenceKernel + "-different"},
	} {
		if err := info.CheckExecution(); err != nil {
			t.Fatal("a capable tuple was refused by release string", info, err)
		}
	}
}

func TestExecutionRejectsUnsupportedTuples(t *testing.T) {
	for _, info := range []Info{
		{"darwin", "arm64", 4096, ReferenceKernel},
		{"darwin", "amd64", 4096, "7.2.8-9-omarchy"},
		{"linux", "arm64", 16384, ReferenceKernel},
		{"linux", "arm64", 65536, ReferenceKernel},
		{"linux", "amd64", 16384, "7.2.8-9-omarchy"},
		{"linux", "amd64", 65536, "7.2.8-9-omarchy"},
		{"linux", "arm", 4096, ReferenceKernel},
		{"linux", "riscv64", 4096, ReferenceKernel},
		{"linux", "amd64", 4096, ""},
		{"linux", "arm64", 4096, ""},
		{"linux", "amd64", 4096, "5.14.21-azure"},
		{"linux", "amd64", 4096, "4.19.0-27-amd64"},
		{"linux", "amd64", 4096, "3.10.0-1160.el7"},
	} {
		if info.CheckExecution() == nil {
			t.Fatal("execution admitted an unsupported tuple", info)
		}
	}
}

func TestExecutionRefusalNamesTheMinimum(t *testing.T) {
	err := (Info{"linux", "amd64", 4096, "5.10.0"}).CheckExecution()
	if err == nil || !strings.Contains(err.Error(), "older than the minimum 5.15") {
		t.Fatal("old kernel refusal does not state the documented minimum", err)
	}
}

func TestParseRelease(t *testing.T) {
	for release, want := range map[string][2]int{
		"5.15.0-1070-azure": {5, 15}, "6.8.0-1017-azure": {6, 8}, "7.2.8-9-omarchy": {7, 2},
		"5.15": {5, 15}, "6.10-rc1": {6, 10}, "6.1.0-18-arm64": {6, 1},
		"5.15.153.1-microsoft-standard-WSL2": {5, 15},
	} {
		major, minor, ok := ParseRelease(release)
		if !ok || major != want[0] || minor != want[1] {
			t.Fatal("release parsed incorrectly", release, major, minor, ok)
		}
	}
	for _, release := range []string{"", "unqualified", "x.y.z", "5", "5.x"} {
		if _, _, ok := ParseRelease(release); ok {
			t.Fatal("malformed release parsed", release)
		}
	}
}

func TestQualificationLabelsOnlyTheNativelyRunKernels(t *testing.T) {
	for _, info := range []Info{
		{"linux", "amd64", 4096, "7.2.8-9-omarchy"},
		{"linux", "arm64", 4096, ReferenceKernel},
	} {
		if info.Qualification() != Verified {
			t.Fatal("natively run kernel is not labelled verified", info)
		}
	}
	for _, info := range []Info{
		{"linux", "arm64", 4096, "7.2.8-9-omarchy"},
		{"linux", "amd64", 4096, ReferenceKernel},
		{"linux", "amd64", 4096, "5.15.0-1070-azure"},
		{"linux", "amd64", 4096, ""},
	} {
		if info.Qualification() != Unverified {
			t.Fatal("unfamiliar kernel is not labelled unverified", info)
		}
	}
}
