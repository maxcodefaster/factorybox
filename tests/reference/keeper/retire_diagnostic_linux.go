package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func (store *captureStore) retirementMismatch(image *capturedImage, backing *os.File, backingHash string) error {
	original, err := os.OpenFile(image.name, os.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("diagnostic original image: %w", err)
	}
	defer original.Close()
	signature, err := signatureForFD(int(original.Fd()))
	if err != nil || signature != image.signature {
		return fmt.Errorf("diagnostic original image identity changed: %v", err)
	}
	var disk, logical, raw [4096]byte
	for offset := int64(0); offset < image.bytes; offset += 4096 {
		n, err := backing.ReadAt(disk[:], offset)
		if err != nil || n != len(disk) {
			return fmt.Errorf("diagnostic backing read: %d %v", n, err)
		}
		expected := image.pageHashes[offset/4096]
		diskHash := sha256.Sum256(disk[:])
		logicalCount, logicalErr := image.reader.ReadAt(logical[:], offset)
		logicalHash := sha256.Sum256(logical[:])
		if diskHash == expected && logicalErr == nil && logicalCount == 4096 && logicalHash == expected {
			continue
		}
		rawCount, rawErr := unix.Pread(int(original.Fd()), raw[:], offset)
		rawHash := sha256.Sum256(raw[:])
		var changed []int
		rawIsGolden := rawErr == nil && rawCount == 4096 && rawHash == expected
		captured := image.preimages[offset]
		captureAvailable := len(captured) == 4096 && sha256.Sum256(captured) == expected
		comparison := captured
		if !captureAvailable {
			comparison = nil
			if rawIsGolden {
				comparison = raw[:]
			}
		}
		if len(comparison) == 4096 && logicalErr == nil && logicalCount == 4096 {
			for index := range comparison {
				if comparison[index] != logical[index] {
					changed = append(changed, index)
				}
			}
		}
		record := map[string]any{
			"image": image.name, "source": image.source, "comm": image.comm,
			"page_offset": offset, "logical_bytes": image.bytes, "raw_eager_base": image.usesRawBase(),
			"expected_capture_sha256": image.goldenHash, "backing_sha256": backingHash,
			"expected_page_sha256":        hex.EncodeToString(expected[:]),
			"backing_page_sha256":         hex.EncodeToString(diskHash[:]),
			"current_logical_page_sha256": hex.EncodeToString(logicalHash[:]),
			"current_raw_page_sha256":     hex.EncodeToString(rawHash[:]),
			"current_logical_bytes":       logicalCount, "current_raw_bytes": rawCount,
			"logical_still_matches_capture":   logicalErr == nil && logicalCount == 4096 && logicalHash == expected,
			"backing_matches_current_logical": logicalErr == nil && logicalCount == 4096 && logicalHash == diskHash,
			"backing_still_matches_capture":   diskHash == expected,
			"raw_still_matches_capture":       rawIsGolden,
			"changed_byte_offsets_in_page":    changed,
			"capture_preimage_available":      captureAvailable,
			"scope":                           "First actual retirement page mismatch. Raw diagnostic pages are owned fixture evidence; no memory bytes are emitted to logs. This is failure evidence, never a speed sample.",
		}
		if logicalErr != nil {
			record["logical_read_error"] = logicalErr.Error()
		}
		if rawErr != nil {
			record["raw_read_error"] = rawErr.Error()
		}
		for _, span := range image.reader.spans {
			if offset >= span.offset && offset < span.offset+span.bytes {
				record["span"] = map[string]any{"offset": span.offset, "bytes": span.bytes, "pipe_offset": span.pipeOffset, "eager": span.pipe == nil}
				break
			}
		}
		var st unix.Stat_t
		if err := unix.Fstat(int(original.Fd()), &st); err == nil {
			record["current_raw_identity"] = map[string]any{"device": st.Dev, "inode": st.Ino, "size": st.Size, "mtime_sec": st.Mtim.Sec, "mtime_nsec": st.Mtim.Nsec}
		}
		record["captured_image_identity"] = map[string]any{"device": image.signature.device, "inode": image.signature.inode,
			"size": image.signature.size, "mtime_sec": image.signature.mtimeSec, "mtime_nsec": image.signature.mtimeNsec}
		stem := fmt.Sprintf("retirement-mismatch-%x-%x-%s", image.signature.device, image.signature.inode, filepath.Base(image.name))
		for suffix, data := range map[string][]byte{"-backing.bin": disk[:], "-logical.bin": logical[:], "-raw.bin": raw[:]} {
			path := filepath.Join(store.output, stem+suffix)
			file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				return err
			}
			if _, err := file.Write(data); err != nil {
				file.Close()
				return err
			}
			if err := file.Close(); err != nil {
				return err
			}
		}
		if captureAvailable {
			if err := os.WriteFile(filepath.Join(store.output, stem+"-capture.bin"), captured, 0600); err != nil {
				return err
			}
		}
		if image.maps != "" {
			if err := os.WriteFile(filepath.Join(store.output, stem+"-maps.txt"), []byte(image.maps), 0600); err != nil {
				return err
			}
		}
		return writeJSON(filepath.Join(store.output, stem+".json"), record)
	}
	return fmt.Errorf("retirement hash differed but no mismatching page remained")
}
