package main

import (
	"crypto/sha256"
	"fmt"
	"os"
)

const capturePreimageLimit = 16 << 20

// Diagnostic only: retain the first page of each private VMA so the
// already-observed VMA-start mismatch has a byte-for-byte capture oracle
// even when the raw image deliberately contains a hole.
func (store *captureStore) capturePreimages(image *capturedImage) error {
	mode := os.Getenv("RUNTIME_FORK_PIPE_PREIMAGES")
	if mode == "" || mode == "0" {
		return nil
	}
	if mode != "1" {
		return fmt.Errorf("unknown capture preimage diagnostic mode")
	}
	offsets := map[int64]bool{}
	for _, offset := range image.privateVMAStarts {
		offsets[offset] = true
	}
	bytes := int64(len(offsets)) * 4096
	store.mu.Lock()
	if bytes > capturePreimageLimit-store.preimageBytes {
		store.mu.Unlock()
		return fmt.Errorf("diagnostic capture preimages exceed the fixed 16MiB bound")
	}
	store.preimageBytes += bytes
	store.peakPreimageBytes = max(store.peakPreimageBytes, store.preimageBytes)
	store.mu.Unlock()
	image.preimages = make(map[int64][]byte, len(offsets))
	image.preimageBytes = bytes
	for offset := range offsets {
		page := make([]byte, 4096)
		if n, err := image.reader.ReadAt(page, offset); err != nil || n != len(page) {
			return fmt.Errorf("diagnostic capture preimage read: %d %v", n, err)
		}
		if sha256.Sum256(page) != image.pageHashes[offset/4096] {
			return fmt.Errorf("diagnostic preimage already differs from the captured page digest")
		}
		image.preimages[offset] = page
	}
	return nil
}

func (store *captureStore) releasePreimages(image *capturedImage) {
	store.mu.Lock()
	store.preimageBytes -= image.preimageBytes
	image.preimageBytes = 0
	image.preimages = nil
	store.mu.Unlock()
}
