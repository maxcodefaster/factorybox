//go:build linux

package lifecycle

import (
	"errors"
	"fmt"
	"math"
	"os"
)

func resumeImageBytes(files []CheckpointFile) (uint64, error) {
	if len(files) == 0 {
		return 0, fmt.Errorf("Resume has no complete checkpoint file inventory")
	}
	var bytes uint64
	for _, file := range files {
		if file.Bytes < 0 || uint64(file.Bytes) > math.MaxUint64-bytes {
			return 0, fmt.Errorf("Resume checkpoint byte inventory is invalid or overflows")
		}
		bytes += uint64(file.Bytes)
	}
	return bytes, nil
}

// Resume needs an independent writable image set. Check its complete known
// size before changing a usable Pause or copying any backing. An existing
// preparation is verified by prepareResume, never charged or copied twice.
func (e *Engine) admitResumeStorage(record Record) error {
	relative := "objects/" + record.ID + "/resumes/" + record.Pause.SnapshotID
	if _, err := e.root.Lstat(relative); err == nil {
		return e.admitStorageWrite()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	generation, err := checkpointPath(record.ID, record.Pause.SnapshotID)
	if err != nil {
		return err
	}
	saved, err := e.readCheckpoint(generation, "checkpoint.json")
	if err != nil {
		return err
	}
	if saved.SourceID != record.ID || saved.ID != record.Pause.SnapshotID ||
		saved.Owner != record.Owner || !saved.SourceStopped {
		return fmt.Errorf("Resume capacity does not identify its original completed Pause")
	}
	bytes, err := resumeImageBytes(saved.Files)
	if err != nil {
		return err
	}
	return e.admitStorageAllocation(bytes)
}
