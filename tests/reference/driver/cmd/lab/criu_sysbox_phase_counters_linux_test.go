//go:build linux

package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestSysboxPhaseCountersNative(t *testing.T) {
	if os.Getenv("SYSBOX_PHASE_COUNTER_NATIVE") != "1" {
		t.Skip("requires a separately owned native Linux fixture")
	}
	before, err := criuSysboxPhaseCountersRead()
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	path := filepath.Join("/work", "owned-phase-counter-payload")
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	defer file.Close()
	buffer := make([]byte, 128*1024)
	for index := range buffer {
		buffer[index] = byte(index*37 + index/256)
	}
	expected := sha256.New()
	for repeat := 0; repeat < 256; repeat++ {
		if _, err := file.Write(buffer); err != nil {
			t.Fatal(err)
		}
		expected.Write(buffer)
	}
	if err := file.Sync(); err != nil {
		t.Fatal(err)
	}
	if err := unix.Fadvise(int(file.Fd()), 0, 0, unix.FADV_DONTNEED); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	actual := sha256.New()
	count, err := io.CopyBuffer(actual, struct{ io.Reader }{file}, buffer)
	if err != nil || count != 32*1024*1024 || string(actual.Sum(nil)) != string(expected.Sum(nil)) {
		t.Fatalf("owned native file bytes: %d %v", count, err)
	}
	wallUS := time.Since(start).Microseconds()
	after, err := criuSysboxPhaseCountersRead()
	if err != nil {
		t.Fatal(err)
	}
	delta, err := criuPhaseDifference(before, after)
	_, hasOOMKillCounter := delta.MemoryEvents["oom_kill"]
	if err != nil || delta.CPU["usage_usec"] == 0 || after.Current == 0 || !hasOOMKillCounter || delta.MemoryEvents["oom_kill"] != 0 || len(after.Pressure)+len(after.PressureUnavailable) != 3 {
		t.Fatalf("native measured cgroup: %+v %v", delta, err)
	}
	data, err := json.Marshal(map[string]any{"status": "passed", "verified_bytes": count, "workload_wall_us": wallUS,
		"before": before, "after": after, "delta": delta,
		"scope": "only the separately owned native test cgroup; actual CPU and synced Btrfs write/read; no global settings or production workload"})
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(string(data))
}
