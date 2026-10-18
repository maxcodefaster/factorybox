package main

import (
	"io"
	"os"
	"sync"
)

var descendantHashBuffers = sync.Pool{New: func() any { return new([1 << 20]byte) }}

func descendantCoalescedHashesEnabled() bool {
	return os.Getenv("RUNTIME_FORK_COALESCED_HASHES") == "1"
}

func descendantCopySnapshotHash(destination io.Writer, source io.Reader) (int64, error) {
	buffer := descendantHashBuffers.Get().(*[1 << 20]byte)
	defer descendantHashBuffers.Put(buffer)
	// *os.File.WriteTo otherwise overrides CopyBuffer's supplied buffer and
	// falls back to generic small reads when the destination is a digest.
	return io.CopyBuffer(destination, struct{ io.Reader }{source}, buffer[:])
}
