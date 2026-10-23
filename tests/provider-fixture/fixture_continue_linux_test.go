//go:build linux

package providerfixture

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestContinuationLeaseKeepsItsOperationAndOriginalRelease(t *testing.T) {
	directory := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := operator(ctx, directory, false)
	target := Target{RunOwner: strings.Repeat("a", 32), Sandbox: "branch", Owner: strings.Repeat("b", 32),
		Runtime: "owned-runtime", PID: 123, Created: "synthetic"}
	lease, err := Acquire(ctx, directory, "continue", target)
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	paths, _ := filepath.Glob(filepath.Join(directory, "provider-request-*.json"))
	if len(paths) != 2 {
		t.Fatal("continuation reissued a gateway request", paths)
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		var request Request
		if err != nil || json.Unmarshal(data, &request) != nil || request.Operation != "continue" || request.Target != target {
			t.Fatal("continuation gateway changed its operation or target", err, request)
		}
	}
}
