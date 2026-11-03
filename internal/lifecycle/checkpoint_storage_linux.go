//go:build linux

package lifecycle

import (
	"fmt"
	"math"
)

func checkpointMemoryBytes(memory SandboxMemory) (uint64, error) {
	if memory.Shmem > math.MaxUint64-memory.Anon {
		return 0, fmt.Errorf("checkpoint memory accounting overflows its storage allocation")
	}
	// Shared memory belongs to memory.stat's file counter. Charge it once,
	// alongside anonymous pages, without charging ordinary file cache as RAM.
	return memory.Anon + memory.Shmem, nil
}

func (e *Engine) admitCheckpointMemory(record Record) error {
	if e.config.MinFreeBytes == 0 && e.config.WriteReserve == 0 {
		return nil
	}
	memory, err := e.sandboxMemory(record)
	if err != nil {
		return fmt.Errorf("checkpoint memory accounting is unconfirmed: %w", err)
	}
	bytes, err := checkpointMemoryBytes(memory)
	if err != nil {
		return err
	}
	// The caller holds the original source frozen, before CRIU or timer
	// mutation. This accounts for known RAM output in addition to the existing
	// floor/reserve. It is not a quota against other guests' subsequent writes.
	return e.admitStorageAllocation(bytes)
}
