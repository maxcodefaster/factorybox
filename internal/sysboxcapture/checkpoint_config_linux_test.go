//go:build linux

package sysboxcapture

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCheckpointWatchSearchAdmission(t *testing.T) {
	for name, encoded := range map[string]string{
		"not-json": "/workspace", "null": "null", "wrong-type": `[1]`,
		"root": `["/"]`, "relative": `["workspace"]`, "traversal": `["/work/../etc"]`,
		"double-slash": `["/work//data"]`, "newline": `["/work\nlog-level 4"]`,
		"space": `["/work space"]`, "quote": `["/work\"x"]`, "comment": `["/work#x"]`,
		"escape": `["/work\\x"]`, "nul": `["/work\u0000x"]`, "unicode-space": `["/work\u00a0x"]`,
		"proc": `["/proc/1/root"]`, "sys": `["/sys"]`, "dev": `["/dev/shm"]`,
		"duplicate": `["/data","/data"]`, "too-large": strings.Repeat(" ", (16<<10)+1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := CheckpointConfiguration(map[string]string{IRMapScanPathsAnnotation: encoded}); err == nil {
				t.Fatal("unsafe or malformed search roots were admitted")
			}
		})
	}
	tooMany, _ := json.Marshal(make([]string, 33))
	if _, err := CheckpointConfiguration(map[string]string{IRMapScanPathsAnnotation: string(tooMany)}); err == nil {
		t.Fatal("unbounded search root count admitted")
	}
	labels := []string{"bundle=/owned/bundle", IRMapScanPathsAnnotation + `=["/application/state","/home/project"]`}
	got, err := checkpointConfigurationFromLabels(labels)
	if err != nil || string(got) != "nested-ns\nsysbox-oom-floor\nirmap-scan-path /application/state\nirmap-scan-path /home/project\n" {
		t.Fatalf("bounded generic paths did not reach the private CRIU configuration: %s: %v", got, err)
	}
	if _, err := checkpointConfigurationFromLabels(append(labels, labels[1])); err == nil {
		t.Fatal("duplicate saved annotation admitted")
	}
	got, err = CheckpointConfiguration(nil)
	if err != nil || string(got) != "nested-ns\nsysbox-oom-floor\n" {
		t.Fatalf("unconfigured capture changed: %s: %v", got, err)
	}
}
