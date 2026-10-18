package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestSnapshotHashCoalescingUsesBoundedNativeReads(t *testing.T) {
	if os.Getenv("RUNTIME_FORK_GRAPH_NATIVE") != "1" {
		t.Skip("real existing-worker snapshot file and kernel read counters only")
	}
	directory, err := os.MkdirTemp("/experiment", "hash-read-regression-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(directory) })
	path := filepath.Join(directory, "known-payload.img")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	block := make([]byte, 1<<20)
	for index := range block {
		block[index] = byte((index*73+index/4096*19+29)%255 + 1)
	}
	expected := sha256.New()
	for index := 0; index < 64; index++ {
		if _, err := file.Write(block); err != nil {
			t.Fatal(err)
		}
		expected.Write(block)
	}
	if err := file.Sync(); err != nil {
		t.Fatal(err)
	}
	digest := fmt.Sprintf("%x", expected.Sum(nil))
	counters := func() map[string]uint64 {
		data, err := os.ReadFile("/proc/self/io")
		if err != nil {
			t.Fatal(err)
		}
		values, err := descendantResourceCounters(string(data))
		if err != nil {
			t.Fatal(err)
		}
		return values
	}
	for _, kind := range []string{"leased", "logical"} {
		hash := func() (string, error) {
			if kind == "leased" {
				return descendantLeaseHash(file)
			}
			return criuRAMHashFile(path)
		}
		var reads []uint64
		for _, mode := range []string{"0", "1"} {
			t.Setenv("RUNTIME_FORK_COALESCED_HASHES", mode)
			before := counters()
			got, err := hash()
			after := counters()
			if err != nil || got != digest {
				t.Fatal("complete independent known-byte digest changed", kind, mode, err, got)
			}
			if after["rchar:"]-before["rchar:"] < 64<<20 {
				t.Fatal("hash did not read the complete real file")
			}
			reads = append(reads, after["syscr:"]-before["syscr:"])
		}
		t.Logf("%s kernel read calls: reference=%d coalesced=%d", kind, reads[0], reads[1])
		if reads[0] < 1000 || reads[1]*8 >= reads[0] {
			t.Fatal("snapshot hash still uses small generic file reads", kind, reads)
		}
	}
}
