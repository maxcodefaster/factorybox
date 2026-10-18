package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

const liveCaptureImageLimit = 128
const retiredCaptureHistoryLimit = 128
const completedReaderHistoryLimit = 512

type archivedRetirement struct {
	Schema   int            `json:"schema"`
	Name     string         `json:"name"`
	Source   sourceIdentity `json:"source"`
	Bytes    int64          `json:"bytes"`
	Device   uint64         `json:"device"`
	Inode    uint64         `json:"inode"`
	MtimeSec int64          `json:"mtime_sec"`
	MtimeNS  int64          `json:"mtime_ns"`
	SHA256   string         `json:"sha256"`
	Evidence map[string]any `json:"evidence"`
}

func retiredArchiveName(name string) string {
	sum := sha256.Sum256([]byte(name))
	return hex.EncodeToString(sum[:]) + ".json"
}

func readRetirementArchive(path string) ([]byte, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 16384 {
		return nil, fmt.Errorf("invalid owned retirement archive file: %v", err)
	}
	data, err := io.ReadAll(io.LimitReader(file, 16385))
	if err != nil || len(data) > 16384 {
		return nil, fmt.Errorf("bounded retirement archive read failed: %v", err)
	}
	return data, nil
}

func (store *captureStore) persistRetiredImageLocked(image *capturedImage) ([]byte, error) {
	directory := filepath.Join(store.output, "retired-images")
	if !store.archiveReady {
		if err := os.Mkdir(directory, 0700); err != nil {
			return nil, fmt.Errorf("create owned retirement archive: %w", err)
		}
		store.archiveReady = true
	}
	record := archivedRetirement{
		Schema: 1, Name: image.name, Source: image.source, Bytes: image.bytes,
		Device: image.signature.device, Inode: image.signature.inode,
		MtimeSec: image.signature.mtimeSec, MtimeNS: image.signature.mtimeNsec,
		SHA256: image.goldenHash, Evidence: image.retirement,
	}
	path := filepath.Join(directory, retiredArchiveName(image.name))
	if err := writeJSON(path, record); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, fmt.Errorf("persist completed image before catalog pruning: %w", err)
	}
	data, err := readRetirementArchive(path)
	if err != nil {
		return nil, fmt.Errorf("retirement archive readback failed: %w", err)
	}
	expected, err := json.MarshalIndent(record, "", "  ")
	if err != nil || !bytes.Equal(data, append(expected, '\n')) {
		return nil, fmt.Errorf("retirement archive differs after sync: %v", err)
	}
	// An identical record may survive an interrupted persistence/pruning
	// transition. Re-sync it and its directory before completing that retry.
	owned, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	if err := errors.Join(owned.Sync(), owned.Close()); err != nil {
		return nil, err
	}
	parent, err := os.Open(directory)
	if err != nil {
		return nil, err
	}
	if err := errors.Join(parent.Sync(), parent.Close()); err != nil {
		return nil, err
	}
	return data, nil
}

// The bounded catalog retains recent retirement retries. Older identities
// keep an exclusive file-backed record, so pruning cannot allow reuse of
// a previously committed path or lose its immutable capture evidence.
// store.mu is held by the caller. No image lock is acquired here.
func (store *captureStore) pruneRetiredLocked() error {
	retired := 0
	for _, image := range store.images {
		if image.retired {
			retired++
		}
	}
	for retired >= retiredCaptureHistoryLimit {
		index := -1
		for i, image := range store.images {
			if image.retired {
				index = i
				break
			}
		}
		image := store.images[index]
		data, err := store.persistRetiredImageLocked(image)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		for i := range sum {
			store.archiveSum[i] ^= sum[i]
		}
		store.archivedImages++
		store.archivedBytes += image.bytes
		copy(store.images[index:], store.images[index+1:])
		store.images[len(store.images)-1] = nil
		store.images = store.images[:len(store.images)-1]
		retired--
	}
	return nil
}

func (store *captureStore) admit(image *capturedImage) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.liveImages >= liveCaptureImageLimit {
		return fmt.Errorf("active capture image bound reached")
	}
	for _, existing := range store.images {
		if existing.name == image.name {
			return fmt.Errorf("duplicate immutable image identity")
		}
	}
	if _, pending := store.pending[image.name]; pending {
		return fmt.Errorf("duplicate pending image identity")
	}
	if _, err := os.Lstat(filepath.Join(store.output, "retired-images", retiredArchiveName(image.name))); err == nil {
		return fmt.Errorf("previously retired immutable image identity")
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := store.pruneRetiredLocked(); err != nil {
		return err
	}
	if store.pending == nil {
		store.pending = make(map[string]*capturedImage)
	}
	store.pending[image.name] = image
	image.creditsHeld = true
	store.liveImages++
	store.peakLiveImages = max(store.peakLiveImages, store.liveImages)
	return nil
}

func (store *captureStore) publish(image *capturedImage) {
	store.mu.Lock()
	defer store.mu.Unlock()
	delete(store.pending, image.name)
	store.images = append(store.images, image)
	store.registeredBytes += image.pipeBytes
	store.registeredFDs += image.buffers
	store.registeredImages++
}

func (store *captureStore) releaseCaptureCredits(image *capturedImage) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.releaseCaptureCreditsLocked(image)
}

func (store *captureStore) releaseCaptureCreditsLocked(image *capturedImage) {
	if image.creditsHeld {
		store.liveImages--
		store.heldBytes -= image.pipeBytes
		store.heldFDs -= image.buffers
		delete(store.pending, image.name)
		image.creditsHeld = false
	}
}

func (store *captureStore) completeRetirement(image *capturedImage) {
	// No client can read an image while its retirement holds image.mu.
	// Keep the closed reader identity for bounded exact retirement retries.
	image.pageHashes, image.specs, image.privateVMAStarts, image.guardAuditSpecs = nil, nil, nil, nil
	image.maps, image.comm = "", ""
	store.mu.Lock()
	defer store.mu.Unlock()
	image.retired = true
	store.releaseCaptureCreditsLocked(image)
}

func (store *captureStore) verifyArchivedRetirements() (map[string]any, error) {
	proof := map[string]any{
		"images": store.archivedImages, "logical_bytes": store.archivedBytes,
		"directory": "retired-images", "records_verified": uint64(0),
	}
	if !store.archiveReady {
		return proof, nil
	}
	directory, err := os.Open(filepath.Join(store.output, "retired-images"))
	if err != nil {
		return proof, err
	}
	defer directory.Close()
	var count uint64
	var total int64
	var sum [32]byte
	for {
		entries, readErr := directory.ReadDir(32)
		if readErr != nil && readErr != io.EOF {
			return proof, readErr
		}
		for _, entry := range entries {
			if !entry.Type().IsRegular() {
				return proof, fmt.Errorf("retirement archive contains a nonregular entry")
			}
			path := filepath.Join(directory.Name(), entry.Name())
			data, err := readRetirementArchive(path)
			if err != nil {
				return proof, err
			}
			var record archivedRetirement
			if err := json.Unmarshal(data, &record); err != nil || record.Schema != 1 ||
				entry.Name() != retiredArchiveName(record.Name) || record.Bytes < 0 ||
				record.Bytes > maxFileBytes || len(record.SHA256) != 64 {
				return proof, fmt.Errorf("retirement archive identity differs: %v", err)
			}
			current, err := sourceAt(record.Source.PID)
			if err == nil && current == record.Source {
				return proof, fmt.Errorf("archived captured source still alive: %d", current.PID)
			}
			if err != nil && !os.IsNotExist(err) {
				return proof, err
			}
			hash := sha256.Sum256(data)
			for i := range hash {
				sum[i] ^= hash[i]
			}
			count++
			total += record.Bytes
		}
		if readErr == io.EOF {
			break
		}
	}
	if count != store.archivedImages || total != store.archivedBytes || sum != store.archiveSum {
		return proof, fmt.Errorf("completed retirement archive changed")
	}
	proof["records_verified"] = count
	proof["record_sha256_xor"] = hex.EncodeToString(sum[:])
	proof["source_identity_checks"] = count
	return proof, nil
}
