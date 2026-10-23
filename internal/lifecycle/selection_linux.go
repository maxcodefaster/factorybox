//go:build linux

package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"

	"factorybox/internal/sysboxcapture"
)

type CheckpointRequest struct {
	SourceID         string `json:"source_id"`
	SnapshotID       string `json:"snapshot_id"`
	SourceOwner      string `json:"source_owner"`
	CheckpointSHA256 string `json:"checkpoint_sha256"`
}

type CheckpointSelection struct {
	Version    int               `json:"version"`
	ID         string            `json:"id"`
	Request    CheckpointRequest `json:"request"`
	SelectedAt string            `json:"selected_at"`
	ReleasedAt string            `json:"released_at,omitempty"`
}

func (request CheckpointRequest) path() (string, error) {
	if !tokenID.MatchString(request.SourceOwner) || !digest.MatchString(request.CheckpointSHA256) {
		return "", fmt.Errorf("checkpoint operation requires its exact source owner and complete checkpoint SHA-256")
	}
	return checkpointPath(request.SourceID, request.SnapshotID)
}

func (e *Engine) checkpointRequest(request CheckpointRequest) (Record, Checkpoint, string, error) {
	var saved Checkpoint
	relative, err := request.path()
	if err != nil {
		return Record{}, saved, "", err
	}
	record, err := e.read(request.SourceID)
	if err != nil {
		return record, saved, relative, err
	}
	if record.Owner != request.SourceOwner {
		return record, saved, relative, fmt.Errorf("checkpoint operation source owner differs")
	}
	if err := e.readCheckpointJSON(relative+"/checkpoint.json", &saved); err != nil {
		return record, saved, relative, err
	}
	encoded, err := json.Marshal(saved)
	if err != nil || sum(encoded) != request.CheckpointSHA256 || saved.SourceID != request.SourceID || saved.ID != request.SnapshotID ||
		saved.Owner != request.SourceOwner || saved.ReleasedAt == "" {
		return record, saved, relative, fmt.Errorf("checkpoint operation does not name the exact published generation")
	}
	return record, saved, relative, nil
}

func (e *Engine) checkpointAvailable(relative string) error {
	for _, name := range []string{"retirement-intent.json", "retirement-complete.json"} {
		if _, err := e.root.Lstat(relative + "/" + name); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("checkpoint retirement has begun; generation cannot be reused: %s: %v", relative, err)
		}
	}
	return nil
}

func (e *Engine) selection(relative, id string) (CheckpointSelection, error) {
	var selected CheckpointSelection
	if !sandboxID.MatchString(id) {
		return selected, fmt.Errorf("invalid immutable recovery selection ID")
	}
	if err := e.readCheckpointJSON(relative+"/selection-"+id+".json", &selected); err != nil {
		return selected, err
	}
	if selected.Version != 1 || selected.ID != id || selected.SelectedAt == "" || selected.ReleasedAt != "" {
		return selected, fmt.Errorf("invalid persisted recovery selection")
	}
	if savedPath, err := selected.Request.path(); err != nil || savedPath != relative {
		return selected, fmt.Errorf("recovery selection names a different generation")
	}
	released := selected
	if err := e.readCheckpointJSON(relative+"/selection-"+id+"-released.json", &released); err == nil {
		stamp := released.ReleasedAt
		released.ReleasedAt = ""
		if stamp == "" || released != selected {
			return selected, fmt.Errorf("recovery selection release differs from its original request")
		}
		selected.ReleasedAt = stamp
	} else if !errors.Is(err, os.ErrNotExist) {
		return selected, err
	}
	return selected, nil
}

// SelectCheckpoint protects a fully verified generation until explicit release.
// Selection and release records remain after retirement and worker replacement.
func (e *Engine) SelectCheckpoint(ctx context.Context, request CheckpointRequest, id string) (CheckpointSelection, error) {
	lock, err := e.lock()
	if err != nil {
		return CheckpointSelection{}, err
	}
	defer lock.Close()
	_, saved, relative, err := e.checkpointRequest(request)
	if err != nil {
		return CheckpointSelection{}, err
	}
	if !sandboxID.MatchString(id) || strings.HasSuffix(id, "-released") {
		return CheckpointSelection{}, fmt.Errorf("invalid or reserved immutable recovery selection ID")
	}
	selected, err := e.selection(relative, id)
	if err == nil {
		if selected.Request != request || selected.ReleasedAt != "" {
			return selected, fmt.Errorf("selection ID already belongs to another or released request")
		}
		return selected, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return selected, err
	}
	if err := e.checkpointAvailable(relative); err != nil {
		return selected, err
	}
	record, err := e.read(request.SourceID)
	if err != nil {
		return selected, err
	}
	published, err := e.publishedCheckpointMetadata(ctx, record, request.SnapshotID, relative)
	if err != nil {
		return selected, err
	}
	if !reflect.DeepEqual(published, saved) {
		return selected, fmt.Errorf("published checkpoint changed before selection")
	}
	if err := sysboxcapture.VerifyFilesystemSnapshot(ctx, e.config.Root+"/"+relative, saved.Filesystem); err != nil {
		return selected, err
	}
	selected = CheckpointSelection{Version: 1, ID: id, Request: request, SelectedAt: now()}
	return selected, e.publishCheckpoint(relative, "selection-"+id+".json", selected)
}

func (e *Engine) ReleaseCheckpointSelection(ctx context.Context, request CheckpointRequest, id string) (CheckpointSelection, error) {
	lock, err := e.lock()
	if err != nil {
		return CheckpointSelection{}, err
	}
	defer lock.Close()
	return e.releaseCheckpointSelection(ctx, request, id)
}

// The caller must hold this controller's lock. Retained-epoch maintenance may
// release one exact selection without reopening the closed lifecycle.
func (e *Engine) releaseCheckpointSelection(ctx context.Context, request CheckpointRequest, id string) (CheckpointSelection, error) {
	if err := ctx.Err(); err != nil {
		return CheckpointSelection{}, err
	}
	_, _, relative, err := e.checkpointRequest(request)
	if err != nil {
		return CheckpointSelection{}, err
	}
	selected, err := e.selection(relative, id)
	if err != nil {
		return selected, err
	}
	if selected.Request != request {
		return selected, fmt.Errorf("recovery selection belongs to another exact checkpoint")
	}
	if selected.ReleasedAt != "" {
		return selected, nil
	}
	selected.ReleasedAt = now()
	return selected, e.publishCheckpoint(relative, "selection-"+id+"-released.json", selected)
}

func (e *Engine) checkpointSelections(relative string, request CheckpointRequest) error {
	directory, err := e.root.Open(relative)
	if err != nil {
		return err
	}
	defer directory.Close()
	entries, err := directory.ReadDir(65537)
	if err != nil && !errors.Is(err, io.EOF) || len(entries) > 65536 {
		return fmt.Errorf("checkpoint selection inventory unavailable or excessive: %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "selection-") {
			continue
		}
		if !strings.HasSuffix(name, ".json") || !entry.Type().IsRegular() {
			return fmt.Errorf("unrecognized recovery selection record")
		}
		id := strings.TrimSuffix(strings.TrimPrefix(name, "selection-"), ".json")
		if strings.HasSuffix(id, "-released") {
			id = strings.TrimSuffix(id, "-released")
		}
		selected, err := e.selection(relative, id)
		if err != nil {
			return err
		}
		if selected.Request != request {
			return fmt.Errorf("recovery selection changed its exact checkpoint identity")
		}
		if selected.ReleasedAt == "" {
			return fmt.Errorf("checkpoint is protected by recovery selection %s", id)
		}
	}
	return nil
}

// Check only actual backing dependencies. Completed forks use their verified
// private image copies; their ancestry remains provenance after retirement.
func (e *Engine) checkpointDependencies(ctx context.Context, request CheckpointRequest, lock *os.File) error {
	objects, err := e.root.Open("objects")
	if err != nil {
		return err
	}
	defer objects.Close()
	entries, err := objects.ReadDir(65537)
	if err != nil && !errors.Is(err, io.EOF) || len(entries) > 65536 {
		return fmt.Errorf("sandbox dependency inventory unavailable: %v", err)
	}
	retired, _ := request.path()
	prefix := e.config.Root + "/" + retired
	uses := func(name string) bool { return name == prefix || strings.HasPrefix(name, prefix+"/") }
	for _, entry := range entries {
		record, err := e.read(entry.Name())
		if err != nil {
			return err
		}
		if record.Phase != "running" && record.Phase != "paused" && record.Phase != "deleted" {
			return fmt.Errorf("incomplete sandbox operation blocks checkpoint retirement: %s", record.ID)
		}
		if record.Phase == "deleted" {
			if record.Delete == nil || record.Delete.CompletedAt == "" {
				return fmt.Errorf("sandbox deletion is unconfirmed")
			}
			continue
		}
		if record.Base != nil && record.Base.Immutable != nil && uses(record.Base.Immutable.Root) {
			return fmt.Errorf("live sandbox still requires retiring immutable backing")
		}
		var spec struct {
			Mounts []struct {
				Source string `json:"source"`
			} `json:"mounts"`
		}
		data, err := e.root.ReadFile("objects/" + record.ID + "/config.json")
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, &spec); err != nil {
			return err
		}
		for _, mount := range spec.Mounts {
			if uses(mount.Source) {
				return fmt.Errorf("live sandbox binds retiring checkpoint backing")
			}
		}
		if record.ID == request.SourceID && record.Pause != nil && record.Pause.SnapshotID == request.SnapshotID {
			if record.Phase != "running" || record.Pause.Resume == nil || record.Pause.Resume.CompletedAt == "" {
				return fmt.Errorf("unresumed pause protects its recovery checkpoint")
			}
			relative := "objects/" + record.ID + "/resumes/" + request.SnapshotID
			var intent resumeIntent
			var prepared []CheckpointFile
			if err := e.readCheckpointJSON(relative+"/resume-intent.json", &intent); err != nil {
				return err
			}
			if intent.SnapshotSHA != request.CheckpointSHA256 || intent.Request != e.resumeRequest(record) {
				return fmt.Errorf("resume still lacks exact independent preparation")
			}
			if err := e.readCheckpointJSON(relative+"/resume-prepared.json", &prepared); err != nil {
				return err
			}
			actual, err := e.checkpointFiles(ctx, relative)
			if err != nil {
				return err
			}
			if !checkpointPreparationIntact(prepared, actual) {
				return fmt.Errorf("independent resume images changed")
			}
			if _, _, err := e.resumeCompleted(ctx, lock, record); err != nil {
				return err
			}
		}
	}
	forks, err := e.root.Open("forks")
	if err != nil {
		return err
	}
	defer forks.Close()
	entries, err = forks.ReadDir(65537)
	if err != nil && !errors.Is(err, io.EOF) || len(entries) > 65536 {
		return fmt.Errorf("fork dependency inventory unavailable: %v", err)
	}
	for _, entry := range entries {
		var intent forkIntent
		if err := e.readCheckpointJSON("forks/"+entry.Name()+"/intent.json", &intent); err != nil {
			return fmt.Errorf("incomplete fork/import blocks retirement: %w", err)
		}
		if _, err := e.readForkIntent("forks/"+entry.Name(), intent.Origin.SourceID, intent.Origin.CheckpointID, entry.Name()); err != nil {
			return err
		}
		if intent.Origin.SourceID != request.SourceID || intent.Origin.CheckpointID != request.SnapshotID {
			continue
		}
		record, err := e.read(entry.Name())
		if err != nil {
			return err
		}
		if record.Start.CompletedAt == "" {
			return fmt.Errorf("incomplete descendant still needs source checkpoint")
		}
		if record.Phase == "deleted" && record.Delete != nil && record.Delete.CompletedAt != "" {
			continue
		}
		var prepared forkPreparation
		if err := e.readCheckpointJSON("forks/"+record.ID+"/preparation.json", &prepared); err != nil {
			return err
		}
		identity, _ := json.Marshal(intent)
		if prepared.Owner != record.Owner || prepared.IntentSHA256 != sum(identity) || prepared.PreparedAt == "" || prepared.Filesystem == nil {
			return fmt.Errorf("descendant preparation is not independently bound")
		}
		actual, err := e.checkpointFiles(ctx, "forks/"+record.ID+"/materialized")
		if err != nil {
			return err
		}
		if !checkpointPreparationIntact(prepared.Files, actual) {
			return fmt.Errorf("descendant private images changed")
		}
		for _, tree := range prepared.Filesystem.Roots {
			if uses(tree.Saved) || tree.Immutable != nil && uses(tree.Immutable.Root) {
				return fmt.Errorf("descendant still references retiring filesystem backing")
			}
		}
	}
	return nil
}

func checkpointPreparationIntact(prepared, actual []CheckpointFile) bool {
	if len(prepared) == 0 {
		return false
	}
	originals := make(map[string]CheckpointFile, len(prepared))
	for _, file := range prepared {
		if _, exists := originals[file.Name]; exists {
			return false
		}
		originals[file.Name] = file
	}
	seen := make(map[string]bool, len(actual))
	for _, file := range actual {
		if seen[file.Name] {
			return false
		}
		seen[file.Name] = true
		if original, exists := originals[file.Name]; exists {
			if original != file {
				return false
			}
			delete(originals, file.Name)
			continue
		}
		// The pinned CRIU restore writes these two diagnostics beside the
		// independent prepared images. Never permit a changed original file,
		// an additional image, or an unbounded diagnostic to prove independence.
		limit := int64(0)
		switch file.Name {
		case "images/restore.log":
			limit = 1 << 20
		case "images/stats-restore":
			limit = 64 << 10
		}
		if limit == 0 || file.Bytes < 0 || file.Bytes > limit || !digest.MatchString(file.SHA256) {
			return false
		}
	}
	return len(originals) == 0
}
