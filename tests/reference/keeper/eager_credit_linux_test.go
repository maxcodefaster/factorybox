package main

import (
	"bytes"
	"io"
	"testing"

	"golang.org/x/sys/unix"
)

func TestEagerFallbackAcknowledgesOriginalPipeBeforeWriterDrains(t *testing.T) {
	store, connection, done, file, pipe, source := captureFixture(t)
	expected := append(bytes.Repeat([]byte{17}, 4096), bytes.Repeat([]byte{29}, 4096)...)
	if err := file.Truncate(0); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if err := sendFixture(connection, captureMessage{op: 1, a: 7, b: uint64(source.Process.Pid), fds: []int{int(file.Fd())}}); err != nil {
		t.Fatal(err)
	}
	// ENOMEM in the producer sends its original read descriptor with only
	// eager spans. The keeper must release that duplicate before its ACK.
	if err := sendFixture(connection, captureMessage{op: 2, b: 8192, c: 1, fds: []int{pipe[0]},
		spans: []captureSpan{{offset: 0, bytes: 8192, pipeOffset: 0, eager: 1}}}); err != nil {
		t.Fatal(err)
	}
	available, err := unix.IoctlGetInt(pipe[0], unix.TIOCINQ)
	if err != nil || available != len(expected) {
		t.Fatal("keeper consumed the original writer's bytes", available, err)
	}
	if store.heldBytes != 0 || store.heldFDs != 0 {
		t.Fatal("eager fallback retained original pipe credits")
	}
	if n, err := unix.Splice(pipe[0], nil, int(file.Fd()), nil, len(expected), 0); err != nil || n != int64(len(expected)) {
		t.Fatal("unchanged original writer failed", n, err)
	}
	if err := sendFixture(connection, captureMessage{op: 3, a: 8192}); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := source.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = source.Wait()
	if _, err := store.finish(); err != nil {
		t.Fatal("eager fallback did not preserve the complete capture", err)
	}
	actual := make([]byte, len(expected))
	if n, err := file.ReadAt(actual, 0); err != nil || n != len(actual) || !bytes.Equal(actual, expected) {
		t.Fatal("original writer's complete bytes differ after commit", n, err)
	}
}

func TestEagerOnlyCaptureDoesNotRetainPipeCredits(t *testing.T) {
	store, connection, done, file, pipe, source := captureFixture(t)
	if err := sendFixture(connection, captureMessage{op: 1, a: 7, b: uint64(source.Process.Pid), fds: []int{int(file.Fd())}}); err != nil {
		t.Fatal(err)
	}
	if err := sendFixture(connection, captureMessage{op: 2, b: 8192, c: 1, fds: []int{pipe[0]},
		spans: []captureSpan{{offset: 0, bytes: 8192, pipeOffset: 0, eager: 1}}}); err != nil {
		t.Fatal(err)
	}
	store.mu.RLock()
	heldBytes, heldFDs := store.heldBytes, store.heldFDs
	logicalBytes := store.pending[file.Name()].bytes
	store.mu.RUnlock()
	if heldBytes != 0 || heldFDs != 0 || logicalBytes != 8192 {
		t.Fatalf("eager-only data kept unused pipe credits: held_bytes=%d held_fds=%d logical_bytes=%d", heldBytes, heldFDs, logicalBytes)
	}
	if err := sendFixture(connection, captureMessage{op: 3, a: 8192}); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := source.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	source.Wait()
	proof, err := store.finish()
	if err != nil {
		t.Fatal("complete original-writer data did not survive eager-pipe release", err)
	}
	images := proof["images"].([]map[string]any)
	if len(images) != 1 || images[0]["logical_bytes"] != int64(8192) ||
		images[0]["sha256_logical_after_capture"] != images[0]["sha256_after_source_exit"] ||
		proof["active_capture_credit_bytes"] != int64(0) || proof["active_capture_credit_descriptors"] != 0 {
		t.Fatal("complete bytes/retirement differ after releasing unused eager credits")
	}
}
