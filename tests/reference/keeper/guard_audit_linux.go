package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

const guardAuditByteLimit = 16 << 20
const guardAuditFDLimit = 64

// This optional diagnostic reader deliberately observes the unprotected
// spans rejected by the real snapshot. It is never leased, restored, hashed
// as snapshot content or exported. Its bounded references end before ACK.
func (store *captureStore) openGuardAudit(image *capturedImage) error {
	if !image.guardAuditEnabled || image.unsharedEagerBytes+image.unknownEagerBytes == 0 {
		return nil
	}
	reader, err := newPipeFile(int(image.golden.Fd()), image.bytes, image.guardAuditSpecs)
	if err != nil {
		return err
	}
	var bytes int64
	for _, pipe := range reader.pipes {
		bytes += int64(pipe.available)
	}
	fds := len(reader.pipes) + 1
	store.mu.Lock()
	defer store.mu.Unlock()
	if bytes > guardAuditByteLimit-store.auditBytes || fds > guardAuditFDLimit-store.auditFDs {
		return errors.Join(fmt.Errorf("guard audit exceeds fixed16MiB/64FD bounds"), reader.Close())
	}
	image.guardAudit = reader
	image.guardAuditBytes, image.guardAuditFDs = bytes, fds
	store.auditBytes += bytes
	store.auditFDs += fds
	store.peakAuditBytes = max(store.peakAuditBytes, store.auditBytes)
	store.peakAuditFDs = max(store.peakAuditFDs, store.auditFDs)
	return nil
}

func (store *captureStore) closeGuardAudit(image *capturedImage) error {
	if image.guardAudit == nil {
		return nil
	}
	err := image.guardAudit.Close()
	store.mu.Lock()
	store.auditBytes -= image.guardAuditBytes
	store.auditFDs -= image.guardAuditFDs
	image.guardAuditBytes, image.guardAuditFDs = 0, 0
	image.guardAudit = nil
	store.mu.Unlock()
	return err
}

func (store *captureStore) inspectGuardAudit(image *capturedImage) (map[string]any, error) {
	if !image.guardAuditEnabled {
		return nil, nil
	}
	if image.guardAuditProof != nil {
		return image.guardAuditProof, nil
	}
	var checked, changed int64
	proof := map[string]any{"guard_audit_enabled": true}
	if image.guardAudit != nil {
		var unprotected, captured [4096]byte
		for _, span := range image.guardAudit.spans {
			if span.pipe == nil {
				continue
			}
			for offset := span.offset; offset < span.offset+span.bytes; offset += 4096 {
				n, err := image.guardAudit.ReadAt(unprotected[:], offset)
				if err != nil || n != len(unprotected) {
					return nil, fmt.Errorf("unprotected audit read: %d %v", n, err)
				}
				checked += 4096
				expected := image.pageHashes[offset/4096]
				current := sha256.Sum256(unprotected[:])
				if current == expected {
					continue
				}
				changed++
				if changed != 1 {
					continue
				}
				if n, err := unix.Pread(image.guardAudit.baseFD, captured[:], offset); err != nil || n != len(captured) ||
					sha256.Sum256(captured[:]) != expected {
					return nil, fmt.Errorf("guard audit original writer no longer matches capture: %d %v", n, err)
				}
				var positions []int
				for index := range captured {
					if captured[index] != unprotected[index] {
						positions = append(positions, index)
					}
				}
				source, sourceErr := sourceAt(image.source.PID)
				record := map[string]any{
					"image": image.name, "source": image.source, "comm": image.comm,
					"page_offset": offset, "captured_page_sha256": hex.EncodeToString(expected[:]),
					"unprotected_page_sha256":         hex.EncodeToString(current[:]),
					"changed_byte_offsets_in_page":    positions,
					"original_source_alive":           sourceErr == nil && source == image.source,
					"actual_snapshot_matches_capture": true,
					"scope":                           "Same actual capture. Real snapshot uses the exact original-writer copy. This default-off diagnostic retains only rejected unprotected pipe spans and observes whether the old retention policy would later return different bytes. No application memory is printed.",
				}
				stem := fmt.Sprintf("guard-audit-%x-%x-%s", image.signature.device, image.signature.inode, filepath.Base(image.name))
				for suffix, data := range map[string][]byte{"-capture.bin": captured[:], "-unprotected.bin": unprotected[:]} {
					file, err := os.OpenFile(filepath.Join(store.output, stem+suffix), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
					if err != nil {
						return nil, err
					}
					_, writeErr := file.Write(data)
					if err := errors.Join(writeErr, file.Sync(), file.Close()); err != nil {
						return nil, err
					}
				}
				if err := writeJSON(filepath.Join(store.output, stem+".json"), record); err != nil {
					return nil, err
				}
			}
		}
	}
	proof["guard_audit_checked_bytes"] = checked
	proof["guard_audit_changed_pages"] = changed
	if err := store.closeGuardAudit(image); err != nil {
		return nil, err
	}
	image.guardAuditProof = proof
	return proof, nil
}
