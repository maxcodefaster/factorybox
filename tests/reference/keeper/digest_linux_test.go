package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"testing"
)

type countedCaptureReads struct {
	reader io.ReaderAt
	bytes  int64
}

func (reader *countedCaptureReads) ReadAt(data []byte, offset int64) (int, error) {
	n, err := reader.reader.ReadAt(data, offset)
	reader.bytes += int64(n)
	return n, err
}

func TestCombinedDigestsReadEachInputOnce(t *testing.T) {
	for _, same := range []bool{true, false} {
		name := "original-writer"
		if same {
			name = "deferred"
		}
		t.Run(name, func(t *testing.T) {
			var logical io.ReaderAt
			var retained io.ReaderAt
			if same {
				store, _ := deferredFixture(t)
				logical, retained = store.images[0].reader, store.images[0].reader
			} else {
				store, file := committedReadFixture(t)
				logical, retained = file, store.images[0].reader
			}
			original := &countedCaptureReads{reader: logical}
			saved := &countedCaptureReads{reader: retained}
			digests, err := captureDigests(original, saved, 8192, same, true)
			if err != nil || !digests.complete || digests.different {
				t.Fatal("actual logical capture digest failed", err)
			}
			expected := append(bytes.Repeat([]byte{17}, 4096), bytes.Repeat([]byte{29}, 4096)...)
			whole := sha256.Sum256(expected)
			if digests.whole != hex.EncodeToString(whole[:]) || len(digests.pages) != 2 ||
				digests.pages[0] != sha256.Sum256(expected[:4096]) ||
				digests.pages[1] != sha256.Sum256(expected[4096:]) {
				t.Fatal("complete or per-page digest differs from actual fixture bytes")
			}
			wantRetained := int64(8192)
			if same {
				wantRetained = 0
			}
			if original.bytes != 8192 || saved.bytes != wantRetained ||
				digests.logicalBytes != original.bytes || digests.retainedBytes != saved.bytes {
				t.Fatalf("duplicate full reads remain: original=%d retained=%d want=8192/%d",
					original.bytes, saved.bytes, wantRetained)
			}
		})
	}
}

func TestCombinedDigestsKeepOriginalWriterMismatchAndReadFailure(t *testing.T) {
	for _, mode := range []string{"changed", "truncated"} {
		t.Run(mode, func(t *testing.T) {
			store, file := committedReadFixture(t)
			if mode == "changed" {
				if _, err := file.WriteAt([]byte{231}, 37); err != nil {
					t.Fatal(err)
				}
			} else if err := file.Truncate(4096); err != nil {
				t.Fatal(err)
			}
			digests, err := captureDigests(file, store.images[0].reader, 8192, false, true)
			if err == nil {
				t.Fatal("independent actual original-writer error was accepted")
			}
			if mode == "changed" && (!digests.complete || !digests.different) {
				t.Fatal("actual private-page mismatch lost complete capture diagnostics")
			}
			if mode == "truncated" && digests.complete {
				t.Fatal("truncated original writer produced a complete digest")
			}
		})
	}
}
