//go:build linux

package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
)

// An unattempted Resume never started a restore: the runtime stayed stopped
// under the exclusive operation lock. When preparation cannot continue, for
// example because the paused filesystem changed after the committed Pause, the
// owner must still be able to remove that Sandbox. Deleting it discards no
// restored state. It needs the same positive facts as a failed Resume, minus
// an Error that no restore could have written: the complete original Pause,
// the original stopped runtime with an empty cgroup, and no held or completed
// restore.
func (e *Engine) unattemptedResumeDeleteEvidence(ctx context.Context, record Record) error {
	if !unattemptedResumeInputs(record) || record.RuntimeCreated == "" {
		return fmt.Errorf("Delete requires an original unattempted Resume of a committed Pause")
	}
	generation, err := checkpointPath(record.ID, record.Pause.SnapshotID)
	if err != nil {
		return err
	}
	checkpoint, err := e.publishedCheckpointMetadata(ctx, record, record.Pause.SnapshotID, generation)
	if err != nil {
		return err
	}
	data, _ := json.Marshal(checkpoint)
	if !checkpoint.SourceStopped || checkpoint.Filesystem == nil ||
		sum(data) != record.Pause.CheckpointSHA256 ||
		!reflect.DeepEqual(checkpoint.Filesystem.SharedSource, record.SharedSource) {
		return fmt.Errorf("unattempted Resume Delete differs from its complete original Pause")
	}
	relative := "objects/" + record.ID + "/resumes/" + record.Pause.SnapshotID
	for _, name := range []string{"resume-held.json", "resume-result.json"} {
		if _, err := e.root.Lstat(relative + "/" + name); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("unattempted Resume Delete cannot discard a held or completed restore: %s: %v", name, err)
		}
	}
	return nil
}

func (e *Engine) admitUnattemptedResumeDelete(ctx context.Context, lock *os.File, record Record) error {
	if err := e.unattemptedResumeDeleteEvidence(ctx, record); err != nil {
		return err
	}
	state, err := e.state(ctx, record, lock)
	if err != nil || state == nil || state.Status != "stopped" || state.PID != 0 {
		return fmt.Errorf("unattempted Resume Delete requires its original stopped runtime: %v", err)
	}
	return e.requireEmptyCgroup(record)
}
