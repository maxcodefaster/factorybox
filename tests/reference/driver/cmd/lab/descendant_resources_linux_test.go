package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDescendantResourceSampleNative(t *testing.T) {
	if os.Getenv("RUNTIME_FORK_GRAPH_NATIVE") != "1" {
		t.Skip("existing-worker native resource accounting only")
	}
	start := time.Now()
	before := descendantReadResources(start, "before")
	if len(before.Errors) != 0 || before.CPUMax == "" || before.CPU["usage_usec"] == 0 {
		t.Fatal("native cgroup/worker observations unavailable", asJSON(before))
	}
	until, checksum := time.Now().Add(60*time.Millisecond), uint64(7)
	for time.Now().Before(until) {
		for i := 0; i < 10000; i++ {
			checksum = checksum*6364136223846793005 + 1
		}
	}
	directory, err := os.MkdirTemp("/experiment", "resource-observation-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(directory) })
	path := filepath.Join(directory, "owned-resource-write")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(make([]byte, 1<<20)); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		t.Fatal(err)
	}
	file.Close()
	after := descendantReadResources(start, "after")
	if len(after.Errors) != 0 || after.CPUMax != before.CPUMax ||
		after.ObservedUS <= before.ObservedUS || after.CPU["usage_usec"] <= before.CPU["usage_usec"] {
		t.Fatal("real native CPU/I/O workload was not observed", asJSON(after), checksum)
	}
	for _, resource := range []string{"cpu", "io", "memory"} {
		if (after.Pressure[resource] == nil) != (before.Pressure[resource] == nil) ||
			(after.WorkerPressure[resource] == nil) != (before.WorkerPressure[resource] == nil) {
			t.Fatal("pressure capability changed")
		}
		if after.Pressure[resource] != nil && after.Pressure[resource]["some"] < before.Pressure[resource]["some"] {
			t.Fatal("native pressure counters moved backwards")
		}
		if after.WorkerPressure[resource] != nil && after.WorkerPressure[resource]["some"] < before.WorkerPressure[resource]["some"] {
			t.Fatal("native worker pressure counters moved backwards")
		}
	}
	if after.WorkerCPU["idle"] < before.WorkerCPU["idle"] ||
		after.Self["user_us"] <= before.Self["user_us"] ||
		after.WorkerMemory["MemTotal"] == 0 {
		t.Fatal("native CPU work or worker memory was not observed")
	}
	t.Log(asJSON(map[string]any{"before": before, "after": after, "checksum": checksum}))
}
