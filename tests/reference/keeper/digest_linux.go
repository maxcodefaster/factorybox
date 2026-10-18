package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
)

type captureDigest struct {
	whole                       string
	pages                       [][32]byte
	logicalBytes, retainedBytes int64
	complete, different         bool
}

type digestReads struct {
	reader io.ReaderAt
	bytes  int64
}

func (reader *digestReads) ReadAt(data []byte, offset int64) (int, error) {
	n, err := reader.reader.ReadAt(data, offset)
	reader.bytes += int64(n)
	return n, err
}

// The legacy schedule is retained as the matched control. Both schedules
// produce the complete image digest and every page digest, and reject an
// independent original-writer/retained mismatch.
func captureDigests(logical, retained io.ReaderAt, size int64, same, combined bool) (result captureDigest, resultErr error) {
	if size <= 0 || size > maxFileBytes || size%4096 != 0 {
		return result, fmt.Errorf("invalid capture digest size")
	}
	original := &digestReads{reader: logical}
	saved := &digestReads{reader: retained}
	defer func() {
		result.logicalBytes, result.retainedBytes = original.bytes, saved.bytes
	}()
	if combined {
		whole := sha256.New()
		result.pages = make([][32]byte, size/4096)
		buffer := make([]byte, readChunk)
		var comparison []byte
		if !same {
			comparison = make([]byte, readChunk)
		}
		for offset := int64(0); offset < size; offset += int64(len(buffer)) {
			count := int(min(int64(len(buffer)), size-offset))
			if n, err := original.ReadAt(buffer[:count], offset); err != nil || n != count {
				return result, fmt.Errorf("complete capture digest read failed: %d %v", n, err)
			}
			_, _ = whole.Write(buffer[:count])
			for page := 0; page < count; page += 4096 {
				result.pages[(offset+int64(page))/4096] = sha256.Sum256(buffer[page : page+4096])
			}
			if !same {
				if n, err := saved.ReadAt(comparison[:count], offset); err != nil || n != count {
					return result, fmt.Errorf("independent retained comparison read failed: %d %v", n, err)
				}
				result.different = result.different || !bytes.Equal(buffer[:count], comparison[:count])
			}
		}
		result.whole = hex.EncodeToString(whole.Sum(nil))
		result.complete = true
		if result.different {
			return result, fmt.Errorf("retained capture differs from original writer")
		}
		return result, nil
	}
	var err error
	result.whole, err = hashReader(original, size)
	if err != nil {
		return result, err
	}
	result.pages = make([][32]byte, size/4096)
	buffer := make([]byte, readChunk)
	for offset := int64(0); offset < size; offset += int64(len(buffer)) {
		count := int(min(int64(len(buffer)), size-offset))
		if n, err := original.ReadAt(buffer[:count], offset); err != nil || n != count {
			return result, fmt.Errorf("golden page digest read failed: %d %v", n, err)
		}
		for page := 0; page < count; page += 4096 {
			result.pages[(offset+int64(page))/4096] = sha256.Sum256(buffer[page : page+4096])
		}
	}
	result.complete = true
	got, err := hashReader(saved, size)
	if err != nil {
		return result, err
	}
	result.different = got != result.whole
	if result.different {
		return result, fmt.Errorf("retained capture differs from original writer")
	}
	return result, nil
}
