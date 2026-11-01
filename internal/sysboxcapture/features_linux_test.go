//go:build linux

package sysboxcapture

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCriuFeatures(t *testing.T) {
	for _, test := range []struct {
		name       string
		body       string
		exit       int
		want       bool
		wantShared bool
	}{
		{"legacy", "unknown command", 1, false, false},
		{"supported", `{"schema":1,"root_owned_time_namespaces":true}`, 0, true, false},
		{"packet-protocol", `{"schema":1,"root_owned_time_namespaces":true,"shared_file_descriptions":true,"shared_source_directories":true,"shared_file_transfer_version":2}`, 0, true, true},
		{"missing-packet-protocol", `{"schema":1,"root_owned_time_namespaces":true,"shared_file_descriptions":true,"shared_source_directories":true}`, 0, true, false},
		{"different-packet-protocol", `{"schema":1,"root_owned_time_namespaces":true,"shared_file_descriptions":true,"shared_source_directories":true,"shared_file_transfer_version":3}`, 0, true, false},
		{"invalid-packet-protocol-type", `{"schema":1,"root_owned_time_namespaces":true,"shared_file_transfer_version":true}`, 0, false, false},
		{"unknown-schema", `{"schema":2,"root_owned_time_namespaces":true}`, 0, false, false},
		{"unknown-field", `{"schema":1,"root_owned_time_namespaces":true,"future":true}`, 0, false, false},
		{"trailing-data", `{"schema":1,"root_owned_time_namespaces":true}{}`, 0, false, false},
		{"oversized", strings.Repeat("x", 5000), 0, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "probe")
			script := fmt.Sprintf("#!/bin/sh\nprintf '%%s' '%s'\nexit %d\n", test.body, test.exit)
			if err := os.WriteFile(path, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			file, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			result := InspectFeatures(context.Background(), file)
			if result.RootOwnedTimeNamespaces != test.want ||
				result.SharedFileDescriptions != test.wantShared || result.SharedSourceDirectories != test.wantShared {
				t.Fatalf("unexpected feature admission: %+v", result)
			}
		})
	}
}
