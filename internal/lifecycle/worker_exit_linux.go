//go:build linux

package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"

	"factorybox/internal/sysboxcapture"
)

type workerBaseRelease struct {
	Version       int                            `json:"version"`
	Root          string                         `json:"root"`
	BootID        string                         `json:"boot_id"`
	RuntimeSHA256 string                         `json:"runtime_sha256"`
	RequestedAt   string                         `json:"requested_at"`
	CompletedAt   string                         `json:"completed_at,omitempty"`
	Bases         []Base                         `json:"bases"`
	Aliases       []sysboxcapture.FilesystemTree `json:"aliases,omitempty"`
}

func (e *Engine) exitEntries(path string) ([]os.DirEntry, error) {
	directory, err := e.root.Open(path)
	if err != nil {
		return nil, err
	}
	defer directory.Close()
	entries, err := directory.ReadDir(65537)
	if err != nil && !errors.Is(err, io.EOF) || len(entries) > 65536 {
		return nil, fmt.Errorf("worker exit inventory unavailable or excessive: %s: %v", path, err)
	}
	return entries, nil
}

// ReleaseDeletedEpochBases fences further controller operations and releases
// completed canonical base mounts only after every sandbox was confirmed
// deleted. Completed capture aliases are uncovered before their canonical
// bases. No backing is deleted and no incomplete operation is repaired.
// Paused epochs need a separate drain qualification.
func (e *Engine) ReleaseDeletedEpochBases(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	lock, err := e.lock()
	if err != nil {
		return err
	}
	defer lock.Close()
	entries, err := e.exitEntries("bases")
	if err != nil || len(entries) == 0 {
		return err
	}
	intent := workerBaseRelease{Version: 1, Root: e.config.Root, BootID: e.boot,
		RuntimeSHA256: e.config.RuntimeSHA256, RequestedAt: now()}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		base, err := e.readBase(entry.Name())
		if err != nil {
			return fmt.Errorf("worker exit refuses an unconfirmed base: %w", err)
		}
		if err := sysboxcapture.VerifyImmutableTree(base.Tree.Saved, base.Tree); err != nil {
			return err
		}
		intent.Bases = append(intent.Bases, base)
	}
	objects, err := e.exitEntries("objects")
	if err != nil {
		return err
	}
	for _, entry := range objects {
		if err := ctx.Err(); err != nil {
			return err
		}
		record, err := e.read(entry.Name())
		if err != nil {
			return fmt.Errorf("worker exit refuses an unconfirmed sandbox: %w", err)
		}
		if record.Phase != "deleted" || record.Delete == nil ||
			record.Delete.CompletedAt == "" || record.Delete.Error != "" {
			return fmt.Errorf("base release requires confirmed deletion of sandbox %s", record.ID)
		}
		if _, err := e.root.Lstat("runtime/" + record.RuntimeID); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("base release refuses remaining runtime %s: %v", record.ID, err)
		}
		if _, err := os.Lstat(cgroupRoot + "/" + record.RuntimeID); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("base release refuses remaining cgroup %s: %v", record.ID, err)
		}
		aliases, err := e.exitCaptureAliases(ctx, record, intent.Bases)
		if err != nil {
			return err
		}
		intent.Aliases = append(intent.Aliases, aliases...)
	}
	// Existing controllers must acquire this same lock before every operation.
	// Publish the irreversible epoch fence while holding it, before unmounting.
	if err := e.publishCheckpoint(".", "worker-exit.json", intent); err != nil {
		return err
	}
	for _, tree := range intent.Aliases {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := sysboxcapture.RetireImmutableAlias(tree.Saved, tree); err != nil {
			return fmt.Errorf("release capture base alias: %w", err)
		}
	}
	for _, base := range intent.Bases {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := sysboxcapture.ReleaseFilesystemBaseMount(base.Tree); err != nil {
			return fmt.Errorf("release canonical base %s: %w", base.ID, err)
		}
	}
	intent.CompletedAt = now()
	return e.publishCheckpoint(".", "worker-exit-complete.json", intent)
}

func (e *Engine) exitCaptureAliases(ctx context.Context, record Record, bases []Base) ([]sysboxcapture.FilesystemTree, error) {
	entries, err := e.exitEntries("objects/" + record.ID + "/captures")
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var aliases []sysboxcapture.FilesystemTree
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		relative, err := checkpointPath(record.ID, entry.Name())
		if err != nil {
			return nil, err
		}
		retired, err := e.exitRetiredCapture(record, relative)
		if err != nil {
			return nil, err
		}
		if retired {
			continue
		}
		internal, err := e.exitCompletedTimerGuard(record, entry.Name(), relative)
		if err != nil {
			return nil, fmt.Errorf("worker exit refuses an unconfirmed internal guard %s: %w", relative, err)
		}
		if internal {
			continue
		}
		saved, err := e.publishedCheckpointMetadata(ctx, record, entry.Name(), relative)
		if err != nil {
			return nil, fmt.Errorf("worker exit refuses an unconfirmed capture %s: %w", relative, err)
		}
		if saved.Filesystem == nil {
			return nil, fmt.Errorf("worker exit capture has no filesystem manifest")
		}
		for _, tree := range saved.Filesystem.Roots {
			if tree.Immutable == nil {
				continue
			}
			if tree.Saved != e.config.Root+"/"+relative+"/filesystem/"+tree.Name {
				return nil, fmt.Errorf("worker exit capture alias is outside its exact generation")
			}
			matched := false
			for _, base := range bases {
				matched = matched || reflect.DeepEqual(tree.Immutable, base.Tree.Immutable)
			}
			if !matched {
				return nil, fmt.Errorf("worker exit capture alias lacks its exact completed base")
			}
			if _, err := sysboxcapture.CheckImmutableAliasRetirement(tree.Saved, tree); err != nil {
				return nil, err
			}
			aliases = append(aliases, tree)
		}
	}
	return aliases, nil
}

// Restoring a fork or pause starts a separate timer activation guard. Its
// generation contains only guard evidence, never checkpoint RAM or filesystem
// backing. Require that exact layout and both completed transactions; a missing
// checkpoint alone is never permission to overlook an unfinished capture.
func (e *Engine) exitCompletedTimerGuard(record Record, id, relative string) (bool, error) {
	entries, err := e.exitEntries(relative)
	if err != nil {
		return false, err
	}
	if len(entries) != 1 || entries[0].Name() != "guard" || !entries[0].IsDir() {
		return false, nil
	}
	evidence := filepath.Join(e.config.Root, relative, "guard")
	for _, path := range []string{filepath.Dir(evidence), evidence} {
		if err := privateDirectory(path); err != nil {
			return false, err
		}
	}
	var config captureGuardConfig
	if err := e.readCheckpointJSON(relative+"/guard/guard-intent.json", &config); err != nil {
		return false, err
	}
	source := config.Source
	if config.Version != 1 || config.Root != e.config.Root || config.Evidence != evidence ||
		config.Token != id || config.StopSource || config.Criu != "" || config.CriuSHA256 != "" ||
		config.PID <= 1 || config.Controller <= 1 || config.PID == config.Controller || config.Start == "" ||
		source.Version != 1 || source.ID != record.ID || source.Owner != record.Owner ||
		source.RuntimeID != record.RuntimeID || source.RuntimeSHA256 != record.RuntimeSHA256 ||
		source.BootID != record.BootID || source.BootID != e.boot || source.Cgroup == nil ||
		source.ConfigSHA256 != record.ConfigSHA256 || source.InputSHA256 != record.InputSHA256 ||
		source.InputBundle != record.InputBundle || source.Rootfs != record.Rootfs {
		return false, fmt.Errorf("internal timer guard differs from its exact source and generation")
	}
	data, err := json.MarshalIndent(source, "", "  ")
	if err != nil || sum(append(data, '\n')) != config.RecordSHA {
		return false, fmt.Errorf("internal timer guard original source record changed: %v", err)
	}
	var outcome struct {
		Config          captureGuardConfig `json:"config"`
		Reason          string             `json:"reason"`
		Status          string             `json:"status"`
		CaptureInFlight bool               `json:"capture_in_flight"`
		Passed          bool               `json:"passed"`
		Error           string             `json:"error,omitempty"`
	}
	if err := e.readCheckpointJSON(relative+"/guard/guard-outcome.json", &outcome); err != nil {
		return false, err
	}
	if !reflect.DeepEqual(outcome.Config, config) || !outcome.Passed || outcome.CaptureInFlight ||
		outcome.Error != "" || outcome.Reason != "controller_disarmed" || outcome.Status != "source_running_guard_disarmed" {
		return false, fmt.Errorf("internal timer guard has no confirmed disarm")
	}
	directory, err := e.root.Open(relative + "/guard")
	if err != nil {
		return false, err
	}
	defer directory.Close()
	completed, err := captureTimerActivationComplete(config, captureGuardAnchors{evidence: directory})
	if err != nil || !completed {
		return false, fmt.Errorf("internal timer guard has no confirmed activation: %v", err)
	}
	return true, nil
}

func (e *Engine) exitRetiredCapture(record Record, relative string) (bool, error) {
	var intent checkpointRetirementIntent
	if err := e.readCheckpointJSON(relative+"/retirement-intent.json", &intent); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	path, err := intent.Request.path()
	if err != nil || path != relative || intent.Request.SourceOwner != record.Owner {
		return false, fmt.Errorf("worker exit retirement request differs from its source: %v", err)
	}
	directory, err := directoryIdentity(e.config.Root + "/" + relative)
	if err != nil || intent.Version != 1 || intent.Directory != directory ||
		!digest.MatchString(intent.InventorySHA256) || intent.StartedAt == "" {
		return false, fmt.Errorf("worker exit retirement identity differs: %v", err)
	}
	var saved Checkpoint
	if err := e.readCheckpointJSON(relative+"/checkpoint.json", &saved); err != nil {
		return false, err
	}
	identity, err := json.Marshal(saved)
	if err != nil || sum(identity) != intent.Request.CheckpointSHA256 || saved.Version != 1 ||
		saved.ID != intent.Request.SnapshotID || saved.Owner != record.Owner || saved.SourceID != record.ID ||
		saved.RuntimeID != record.RuntimeID || saved.ConfigSHA256 != record.ConfigSHA256 ||
		saved.BootID != record.BootID || saved.RuntimeSHA256 != record.RuntimeSHA256 ||
		saved.CriuSHA256 != e.config.CriuSHA256 || saved.ManagerRoot != e.config.ManagerRoot ||
		saved.CapturedAt == "" || saved.ReleasedAt == "" || saved.SourcePID <= 1 {
		return false, fmt.Errorf("worker exit retired checkpoint differs from its exact request: %v", err)
	}
	identity, err = json.Marshal(intent)
	if err != nil {
		return false, err
	}
	var complete CheckpointRetirement
	if err := e.readCheckpointJSON(relative+"/retirement-complete.json", &complete); err != nil {
		return false, fmt.Errorf("worker exit refuses incomplete checkpoint retirement: %w", err)
	}
	if complete.Version != 1 || complete.Request != intent.Request || complete.IntentSHA256 != sum(identity) ||
		complete.CompletedAt == "" || !reflect.DeepEqual(complete.Before, intent.Before) {
		return false, fmt.Errorf("worker exit retirement completion differs from its original intent")
	}
	return true, e.retirementAbsent(intent)
}
