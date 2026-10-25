//go:build linux

package lifecycle

import "factorybox/internal/fdstore"

func previousSharedFDKey(record Record) string {
	if record.SharedSource == nil {
		return ""
	}
	for index := len(record.PauseHistory) - 1; index >= 0; index-- {
		pause := record.PauseHistory[index]
		if pause.Resume != nil && pause.Resume.CompletedAt != "" {
			return record.Owner + ":" + pause.SnapshotID
		}
	}
	return ""
}

// The running source can still hold a description from an older mount
// namespace. Keep one generation until its successor is durably captured.
func (e *Engine) releaseSharedHistory(record Record) error {
	if record.SharedSource == nil {
		return nil
	}
	for _, pause := range record.PauseHistory {
		if err := fdstore.Release(e.config.FDStoreSocket, record.Owner+":"+pause.SnapshotID); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) releaseSharedFiles(record Record) error {
	if record.SharedSource == nil || record.Pause == nil {
		return nil
	}
	return fdstore.Release(e.config.FDStoreSocket, record.Owner+":"+record.Pause.SnapshotID)
}

func (e *Engine) releaseAllSharedFiles(record Record) error {
	if record.SharedSource == nil {
		return nil
	}
	if err := e.releaseSharedHistory(record); err != nil {
		return err
	}
	return e.releaseSharedFiles(record)
}
