//go:build linux

package lifecycle

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"

	"factorybox/internal/ownedtree"
)

// This is a reservation and provenance record, never a published fork intent.
// Persist it before copying payloads so ENOSPC cannot turn a known operation
// into an unknown fork directory. Its original target remains reserved.
type recoveryImportPreparation struct {
	Version       int             `json:"version"`
	Root          string          `json:"root"`
	TargetID      string          `json:"target_id"`
	Owner         string          `json:"owner"`
	BootID        string          `json:"boot_id"`
	RuntimeSHA256 string          `json:"runtime_sha256"`
	CriuSHA256    string          `json:"criu_sha256"`
	ManagerRoot   string          `json:"manager_root"`
	Request       RecoveryRequest `json:"request"`
	Source        Record          `json:"source"`
	Checkpoint    Checkpoint      `json:"checkpoint"`
	RequestedAt   string          `json:"requested_at"`
}

func (p recoveryImportPreparation) proposal() forkIntent {
	return forkIntent{TargetID: p.TargetID, Checkpoint: p.Checkpoint, Recovery: &RecoveryOrigin{Request: p.Request}}
}

func (e *Engine) beginRecoveryImport(operation string, request RecoveryRequest, targetID string, source Record, checkpoint Checkpoint) (recoveryImportPreparation, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return recoveryImportPreparation{}, err
	}
	p := recoveryImportPreparation{Version: 1, Root: e.config.Root, TargetID: targetID,
		Owner: hex.EncodeToString(nonce[:]), BootID: e.boot, RuntimeSHA256: e.config.RuntimeSHA256,
		CriuSHA256: e.config.CriuSHA256, ManagerRoot: e.config.ManagerRoot,
		Request: request, Source: source, Checkpoint: checkpoint, RequestedAt: now()}
	// Refuse an oversized journal before creating even the operation directory.
	if _, err := checkpointRecordBytes(p); err != nil {
		return p, err
	}
	if err := e.root.Mkdir(operation, 0700); err != nil {
		return p, err
	}
	if err := e.publishCheckpoint(operation, "import.json", p); err != nil {
		return p, err
	}
	parent, err := e.root.Open("forks")
	if err != nil {
		return p, err
	}
	return p, errors.Join(parent.Sync(), parent.Close())
}

func (e *Engine) readRecoveryImportPreparation(operation, targetID string) (recoveryImportPreparation, error) {
	var p recoveryImportPreparation
	if operation != "forks/"+targetID || !sandboxID.MatchString(targetID) {
		return p, fmt.Errorf("recovery import escaped its target identity")
	}
	if err := e.readCheckpointJSON(operation+"/import.json", &p); err != nil {
		return p, err
	}
	if err := e.validateRecoveryRequest(p.Request, targetID); err != nil {
		return p, err
	}
	source, saved := p.Source, p.Checkpoint
	identity, err := json.Marshal(saved)
	if err != nil || p.Version != 1 || p.Root != e.config.Root || p.TargetID != targetID ||
		!tokenID.MatchString(p.Owner) || p.BootID != e.boot || p.RuntimeSHA256 != e.config.RuntimeSHA256 ||
		p.CriuSHA256 != e.config.CriuSHA256 || p.ManagerRoot != e.config.ManagerRoot || p.RequestedAt == "" ||
		source.Version != 1 || source.ID != p.Request.SourceID || source.Owner != p.Request.SourceOwner ||
		source.BootID != p.BootID || source.RuntimeSHA256 != p.RuntimeSHA256 || source.Phase != "paused" ||
		source.Pause == nil || source.Pause.SnapshotID != p.Request.SnapshotID || source.Pause.CompletedAt == "" ||
		source.Pause.Error != "" || source.Pause.RejectedAt != "" || source.Pause.Resume != nil ||
		source.Pause.After.Anon != 0 || source.Pause.Snapshot.ResidentBytesAfter != 0 ||
		(source.Pause.CheckpointSHA256 != "" && source.Pause.CheckpointSHA256 != p.Request.CheckpointSHA256) ||
		saved.Version != 1 || saved.SourceID != source.ID || saved.ID != p.Request.SnapshotID ||
		saved.Owner != source.Owner || saved.RuntimeID != source.RuntimeID || saved.BootID != p.BootID ||
		saved.ConfigSHA256 != source.ConfigSHA256 || saved.RuntimeSHA256 != p.RuntimeSHA256 ||
		saved.CriuSHA256 != p.CriuSHA256 || saved.ManagerRoot != p.ManagerRoot ||
		!saved.SourceStopped || saved.CapturedAt == "" || saved.ReleasedAt == "" ||
		saved.Filesystem == nil || saved.Filesystem.Version != 1 || len(saved.Filesystem.Roots) == 0 ||
		sum(identity) != p.Request.CheckpointSHA256 {
		return p, fmt.Errorf("recovery import differs from its original verified pause or pinned recipe: %v", err)
	}
	return p, nil
}

func (e *Engine) readUnpublishedRecoveryImport(operation, targetID string) (recoveryImportPreparation, error) {
	p, err := e.readRecoveryImportPreparation(operation, targetID)
	if err != nil {
		return p, err
	}
	if err := privateDirectory(e.config.Root + "/" + operation); err != nil {
		return p, err
	}
	if _, err := e.root.Lstat("objects/" + targetID); !errors.Is(err, os.ErrNotExist) {
		return p, fmt.Errorf("unpublished recovery import already owns a sandbox: %v", err)
	}
	entries, err := e.exitEntries(operation)
	if err != nil {
		return p, err
	}
	temporaries := 0
	for _, entry := range entries {
		switch entry.Name() {
		case "import.json", "import-cancelled.json":
			if !entry.Type().IsRegular() {
				return p, fmt.Errorf("recovery import journal is not a private regular file")
			}
		case "recovery":
			if !entry.IsDir() || privateDirectory(e.config.Root+"/"+operation+"/recovery") != nil {
				return p, fmt.Errorf("recovery import copy directory changed")
			}
		default:
			if err := e.recoveryImportTemporary(operation, entry.Name()); err != nil {
				return p, err
			}
			temporaries++
		}
	}
	completed, err := e.recoveryImportCancellation(operation, p)
	if err != nil {
		return p, err
	}
	if temporaries > 1 || completed != nil && temporaries != 0 {
		return p, fmt.Errorf("recovery import contains repeated or post-cancellation publication")
	}
	return p, nil
}

// A killed publication can leave one partial private temporary file. Its
// contents confer no intent or completion authority. Cancellation requires a
// fresh inventory of every byte before removing this exact known control file.
func (e *Engine) recoveryImportTemporary(operation, name string) error {
	if !strings.HasPrefix(name, ".checkpoint-") || !tokenID.MatchString(strings.TrimPrefix(name, ".checkpoint-")) {
		return fmt.Errorf("unpublished recovery import contains unrecognized entry: %s", name)
	}
	info, err := e.root.Lstat(operation + "/" + name)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > checkpointManifestLimit {
		return fmt.Errorf("recovery import publication temporary changed: %s", name)
	}
	// Capture verifies links, mounts and complete content, including truncated
	// non-JSON publications. Control metadata also requires exact root ownership.
	plan, err := ownedtree.Capture(context.Background(), e.root, []string{operation + "/" + name})
	if err != nil {
		return err
	}
	if len(plan.Entries) != 1 || plan.Entries[0].Object.UID != 0 || plan.Entries[0].Object.GID != 0 || plan.Entries[0].Links != 1 {
		return fmt.Errorf("recovery import publication temporary is not exclusively root owned")
	}
	return nil
}

func (e *Engine) verifyCompletedRecoveryImport(operation string, intent forkIntent) (bool, error) {
	p, err := e.readRecoveryImportPreparation(operation, intent.TargetID)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil // Completed imports from the preceding package have no preparation.
	}
	if err != nil {
		return false, err
	}
	if _, err := e.root.Lstat(operation + "/import-cancelled.json"); !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("completed recovery has a cancelled import reservation: %v", err)
	}
	if intent.Recovery == nil || p.Owner != intent.Owner ||
		!reflect.DeepEqual(p.Request, intent.Recovery.Request) || !reflect.DeepEqual(p.Source, intent.Source) {
		return false, fmt.Errorf("completed recovery differs from its import reservation")
	}
	return true, nil
}

func (e *Engine) inspectUnpublishedRecoveryExit(ctx context.Context, operation, targetID string) error {
	if _, err := e.readUnpublishedRecoveryImport(operation, targetID); err != nil {
		return err
	}
	// This path grants no unmount or deletion authority. A pure interrupted
	// copy needs neither. Any covering/descendant mount or external hardlink
	// refuses exit; later import stages require their own proven reconciliation.
	_, err := ownedtree.Capture(ctx, e.root, []string{operation})
	return err
}
