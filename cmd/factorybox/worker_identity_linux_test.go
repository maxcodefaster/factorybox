//go:build linux

package main

import (
	"strings"
	"testing"
)

func TestWorkerIdentityMustBeCompleteBeforeReadingWorker(t *testing.T) {
	token := strings.Repeat("a", 32)
	for _, args := range [][]string{
		{"--worker-store-id", token, "capacity"},
		{"--worker-epoch", token, "capacity"},
		{"--worker-store-id", token, "--worker-epoch", token, "capacity"},
		{"--worker-run-root", "/nonexistent", "--worker-store-id", "bad", "--worker-epoch", token, "capacity"},
		{"--worker-run-root", "/nonexistent", "--worker-store-id", token, "--worker-epoch", strings.Repeat("G", 32), "capacity"},
	} {
		if err := runLifecycle(args); err == nil || !strings.Contains(err.Error(), "worker identity requires") {
			t.Fatal("incomplete worker expectation was not rejected before inspection", args, err)
		}
	}
}
