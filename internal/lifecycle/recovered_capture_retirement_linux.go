//go:build linux

package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"

	"factorybox/internal/ownedtree"
	"factorybox/internal/sysboxcapture"
)

type captureAliasReleaseIntent struct {
	Version    int                          `json:"version"`
	Request    SandboxBackingRequest        `json:"request"`
	SnapshotID string                       `json:"snapshot_id"`
	ProofSHA   string                       `json:"proof_sha256"`
	Capture    KernelObject                 `json:"capture"`
	Tree       sysboxcapture.FilesystemTree `json:"tree"`
}

type captureAliasReleaseComplete struct {
	Version     int    `json:"version"`
	IntentSHA   string `json:"intent_sha256"`
	CompletedAt string `json:"completed_at"`
}

func (e *Engine) recoveredCaptureProofSHA(record Record, snapshot, relative string) (string, error) {
	proved, err := e.recoveredFailedCapture(record, snapshot, relative)
	if err != nil || !proved {
		return "", errors.Join(err, fmt.Errorf("capture has no exact completed source rollback"))
	}
	root, err := e.root.Open(".")
	if err != nil {
		return "", err
	}
	defer root.Close()
	evidence := map[string]string{}
	for _, name := range []string{
		"checkpoint-intent.json", "source-state.json", sysboxcapture.CaptureLowerAliasIntent,
		"guard/guard-intent.json", "guard/guard-capture-aborted.json", "guard/capture-abort-intent.json",
		"guard/capture-worker-intent.json", "guard/capture-process-complete.json",
		"guard/capture-process-restored.json", "guard/criu-retained-arm.json",
		"guard/criu-retained-request.json", "guard/criu-retained-result.json",
		"guard/guard-outcome.json", "guard/guard-controller-outcome.json",
	} {
		data, err := captureCompletionBytes(root, relative+"/"+name, checkpointManifestLimit)
		if errors.Is(err, os.ErrNotExist) {
			continue // The proof above requires the exact applicable receipt variant.
		}
		if err != nil {
			return "", err
		}
		evidence[name] = sum(data)
	}
	data, err := json.Marshal(evidence)
	return sum(data), err
}

func (e *Engine) privateJournalChild(relative, name string) (string, error) {
	path := relative + "/" + name
	if err := e.root.Mkdir(path, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return "", err
	}
	if err := privateDirectory(e.config.Root + "/" + path); err != nil {
		return "", err
	}
	parent, err := e.root.Open(relative)
	if err != nil {
		return "", err
	}
	return path, errors.Join(parent.Sync(), parent.Close())
}

// Persist the original underlying mountpoint before uncovering it. Completed
// release never permits a new mount at that path. Older captures without this
// writer receipt remain reclaimable only after their original mounts are gone.
func (e *Engine) uncoverRecoveredCapture(journal *Engine, relative string, record Record,
	request SandboxBackingRequest, snapshot, generation, proof string, capture KernelObject) error {
	var tree sysboxcapture.FilesystemTree
	err := e.readCheckpointJSON(generation+"/"+sysboxcapture.CaptureLowerAliasIntent, &tree)
	if errors.Is(err, os.ErrNotExist) {
		if _, err := e.root.Lstat(generation + "/filesystem"); errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return noInitialDescendantMounts(e.config.Root + "/" + generation + "/filesystem")
	}
	if err != nil {
		return err
	}
	if record.Base == nil || tree.Name != "lower" || tree.Alias == nil || tree.Immutable == nil ||
		tree.Source != record.InputBundle+"/lower" ||
		tree.Saved != e.config.Root+"/"+generation+"/filesystem/lower" ||
		!reflect.DeepEqual(tree.Immutable, record.Base.Immutable) {
		return fmt.Errorf("failed capture alias intent differs from its original source/base")
	}
	intent := captureAliasReleaseIntent{1, request, snapshot, proof, capture, tree}
	var previous captureAliasReleaseIntent
	err = journal.readCheckpointJSON(relative+"/release-intent.json", &previous)
	first := errors.Is(err, os.ErrNotExist)
	if err == nil && !reflect.DeepEqual(previous, intent) {
		return fmt.Errorf("failed capture alias release intent changed")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	data, err := json.Marshal(intent)
	if err != nil {
		return err
	}
	var complete captureAliasReleaseComplete
	if err := journal.readCheckpointJSON(relative+"/release-complete.json", &complete); err == nil {
		if first || complete.Version != 1 || complete.IntentSHA != sum(data) || complete.CompletedAt == "" {
			return fmt.Errorf("failed capture alias release completion changed")
		}
		mounted, err := sysboxcapture.CheckImmutableAliasRetirement(tree.Saved, tree)
		if err != nil || mounted {
			return errors.Join(err, fmt.Errorf("released failed capture alias has a successor"))
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err := sysboxcapture.CheckImmutableAliasRetirement(tree.Saved, tree); err != nil {
		return err
	}
	if first {
		if err := journal.publishCheckpoint(relative, "release-intent.json", intent); err != nil {
			return err
		}
	}
	if err := sysboxcapture.RetireImmutableAlias(tree.Saved, tree); err != nil {
		return err
	}
	return journal.publishCheckpoint(relative, "release-complete.json",
		captureAliasReleaseComplete{1, sum(data), now()})
}

// Retire every proved recovered failure after explicit native Delete, including
// failures already moved into history before a later Pause. Canonical Source,
// request/rollback evidence, usable checkpoints and registered bases stay intact.
func (e *Engine) retireRecoveredCaptureBacking(ctx context.Context, record Record,
	request SandboxBackingRequest, forest *epochRetirementForest) error {
	if err := sandboxBackingRecord(record, request); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, pause := range record.PauseHistory {
		if pause.CompletedAt != "" || pause.RejectedAt == "" ||
			!strings.HasPrefix(pause.Error, "incomplete checkpoint after source process recovery:") {
			continue
		}
		if !tokenID.MatchString(pause.SnapshotID) || seen[pause.SnapshotID] {
			return fmt.Errorf("recovered capture retirement has an invalid or duplicate generation")
		}
		seen[pause.SnapshotID] = true
		if err := e.retireRecoveredCapture(ctx, record, request, forest, pause.SnapshotID); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) retireRecoveredCapture(ctx context.Context, record Record, request SandboxBackingRequest,
	forest *epochRetirementForest, snapshot string) error {
	generation, err := checkpointPath(record.ID, snapshot)
	if err != nil {
		return err
	}
	proof, err := e.recoveredCaptureProofSHA(record, snapshot, generation)
	if err != nil {
		return err
	}
	if err := e.boundedObject(e.config.Root + "/" + generation); err != nil {
		return err
	}
	if err := privateDirectory(e.config.Root + "/" + generation); err != nil {
		return err
	}
	capture, err := directoryIdentity(e.config.Root + "/" + generation)
	if err != nil {
		return err
	}
	journal, relative, err := e.privateBackingJournal(record, "recovered-capture")
	if err != nil {
		return err
	}
	defer journal.Close()
	relative, err = journal.privateJournalChild(relative, snapshot)
	if err != nil {
		return err
	}
	alias, err := journal.privateJournalChild(relative, "alias")
	if err != nil {
		return err
	}
	relative, err = journal.privateJournalChild(relative, "payload")
	if err != nil {
		return err
	}
	directory, err := directoryIdentity(journal.config.Root + "/" + relative)
	if err != nil {
		return err
	}
	allowed := []string{generation + "/images", generation + "/filesystem"}
	absent := func() error {
		for _, name := range allowed {
			if _, err := e.root.Lstat(name); !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("retired recovered capture has remaining or successor backing: %s: %v", name, err)
			}
		}
		return nil
	}
	dependencies := func() error {
		for _, controller := range forest.controllers {
			if err := controller.sandboxBackingDependencies(ctx, e.config.Root, allowed); err != nil {
				return err
			}
		}
		return nil
	}
	var intent failedCaptureBackingIntent
	err = journal.readCheckpointJSON(relative+"/retirement-intent.json", &intent)
	if errors.Is(err, os.ErrNotExist) {
		if err := journal.resetUnacceptedRetirementPreparation(ctx, relative); err != nil {
			return err
		}
		if err := dependencies(); err != nil {
			return err
		}
		if err := e.uncoverRecoveredCapture(journal, alias, record, request, snapshot, generation, proof, capture); err != nil {
			return err
		}
		paths := []string{}
		for _, name := range allowed {
			if info, err := e.root.Lstat(name); err == nil {
				if !info.IsDir() {
					return fmt.Errorf("recovered capture payload is not its original directory")
				}
				if err := e.boundedObject(e.config.Root + "/" + name); err != nil {
					return err
				}
				paths = append(paths, name)
			} else if !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		sort.Strings(paths)
		plan := ownedtree.Plan{Version: 1, Paths: paths}
		if len(paths) != 0 {
			plan, err = ownedtree.Capture(ctx, e.root, paths)
			if err != nil {
				return err
			}
		}
		before, err := e.storageCapacity()
		if err != nil {
			return err
		}
		hash, err := journal.writeRetirementInventory(relative, plan)
		if err != nil {
			return err
		}
		intent = failedCaptureBackingIntent{Version: 1, Request: request, SnapshotID: snapshot,
			AdmissionSHA256: proof, Controller: e.config.Root, Capture: capture, Directory: directory,
			Bundle: *record.InputDirectory, InventorySHA256: hash, Payloads: paths, StartedAt: now(), Before: before}
		if err := journal.publishCheckpoint(relative, "retirement-intent.json", intent); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if intent.Version != 1 || intent.Request != request || intent.SnapshotID != snapshot ||
		intent.AdmissionSHA256 != proof || intent.Controller != e.config.Root || intent.Capture != capture ||
		intent.Directory != directory || intent.Bundle != *record.InputDirectory ||
		!digest.MatchString(intent.InventorySHA256) || intent.StartedAt == "" || len(intent.Payloads) > len(allowed) {
		return fmt.Errorf("recovered capture retirement differs from its original owner/proof")
	}
	for i, name := range intent.Payloads {
		if name != allowed[0] && name != allowed[1] || i > 0 && name <= intent.Payloads[i-1] {
			return fmt.Errorf("recovered capture retirement gained an unauthorized payload")
		}
	}
	data, _ := json.Marshal(intent)
	var result SandboxBackingRetirement
	if err := journal.readCheckpointJSON(relative+"/retirement-complete.json", &result); err == nil {
		if result.Version != 1 || result.PayloadKind != "recovered-capture-backing" ||
			result.Request != request || result.IntentSHA256 != sum(data) || result.CompletedAt == "" ||
			!reflect.DeepEqual(result.Before, intent.Before) {
			return fmt.Errorf("recovered capture retirement completion changed")
		}
		return absent()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	plan, err := journal.readRetirementInventory(relative, intent.InventorySHA256)
	if err != nil || !reflect.DeepEqual(plan.Paths, intent.Payloads) {
		return errors.Join(err, fmt.Errorf("recovered capture inventory differs from its original paths"))
	}
	if err := dependencies(); err != nil {
		return err
	}
	if checked, err := e.recoveredCaptureProofSHA(record, snapshot, generation); err != nil || checked != proof {
		return errors.Join(err, fmt.Errorf("recovered capture proof changed before retirement"))
	}
	if len(intent.Payloads) != 0 {
		if err := e.retireCaptureNativePayload(ctx, generation, plan); err != nil {
			return err
		}
		if err := ownedtree.RemoveRemaining(ctx, e.root, plan); err != nil {
			return err
		}
	}
	if err := absent(); err != nil {
		return err
	}
	after, err := e.storageCapacity()
	if err != nil {
		return err
	}
	result = SandboxBackingRetirement{Version: 1, PayloadKind: "recovered-capture-backing",
		Request: request, IntentSHA256: sum(data), CompletedAt: now(), Before: intent.Before, After: after}
	return journal.publishCheckpoint(relative, "retirement-complete.json", result)
}
