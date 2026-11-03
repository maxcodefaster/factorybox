//go:build linux

package lifecycle

import (
	"math"
	"testing"
)

func TestCheckpointMemoryStorageBytes(t *testing.T) {
	for _, test := range []struct {
		name   string
		memory SandboxMemory
		bytes  uint64
	}{
		{"anonymous and shared", SandboxMemory{Anon: 2 << 30, Shmem: 19 << 20, File: 6 << 30}, 2<<30 + 19<<20},
		{"shared counted once", SandboxMemory{Shmem: 32 << 20, File: 32 << 20}, 32 << 20},
		{"ordinary cache is not RAM output", SandboxMemory{Current: 3 << 30, File: 3 << 30}, 0},
		{"empty", SandboxMemory{}, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			actual, err := checkpointMemoryBytes(test.memory)
			if err != nil || actual != test.bytes {
				t.Fatal("checkpoint RAM storage allocation differs", actual, test.bytes, err)
			}
		})
	}
	if _, err := checkpointMemoryBytes(SandboxMemory{Anon: math.MaxUint64, Shmem: 1}); err == nil {
		t.Fatal("checkpoint storage accepted overflowed memory counters")
	}
}
