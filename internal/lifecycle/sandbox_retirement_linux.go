//go:build linux

package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"

	"factorybox/internal/ownedtree"
)

type SandboxBackingRequest struct {
	SandboxID    string `json:"sandbox_id"`
	SandboxOwner string `json:"sandbox_owner"`
	InputSHA256  string `json:"input_sha256"`
}

type SandboxBackingRetirement struct {
	Version      int                   `json:"version"`
	PayloadKind  string                `json:"payload_kind"`
	Request      SandboxBackingRequest `json:"request"`
	IntentSHA256 string                `json:"intent_sha256"`
	CompletedAt  string                `json:"completed_at"`
	Before       StorageCapacity       `json:"before"`
	After        StorageCapacity       `json:"after"`
}

type sandboxBackingIntent struct {
	Version         int                   `json:"version"`
	Request         SandboxBackingRequest `json:"request"`
	Directory       KernelObject          `json:"directory"`
	Bundle          KernelObject          `json:"bundle"`
	Store           KernelObject          `json:"store"`
	InventorySHA256 string                `json:"inventory_sha256"`
	Payloads        []string              `json:"payloads"`
	Reserve         retirementReserve     `json:"reserve"`
	StartedAt       string                `json:"started_at"`
	Before          StorageCapacity       `json:"before"`
}

func sandboxBackingRecord(record Record, request SandboxBackingRequest) error {
	if !sandboxID.MatchString(request.SandboxID) || !tokenID.MatchString(request.SandboxOwner) || !digest.MatchString(request.InputSHA256) ||
		record.ID != request.SandboxID || record.Owner != request.SandboxOwner || record.InputSHA256 != request.InputSHA256 {
		return fmt.Errorf("Sandbox backing retirement differs from its immutable owner/input")
	}
	if record.Phase != "deleted" || record.Delete == nil || !record.Delete.Attempted ||
		record.Delete.CompletedAt == "" || record.Delete.Error != "" || record.Start.CompletedAt == "" {
		return fmt.Errorf("Sandbox backing retirement requires confirmed Delete")
	}
	if record.InputDirectory == nil || record.InputDirectory.Inode == 0 ||
		record.Rootfs != record.InputBundle+"/rootfs" {
		return fmt.Errorf("Sandbox backing has no original admitted bundle directory")
	}
	return nil
}

func (e *Engine) sandboxBackingSafety(ctx context.Context, record Record, lock *os.File) error {
	state, err := e.state(ctx, record, lock)
	if err != nil || state != nil {
		return errors.Join(err, fmt.Errorf("Sandbox backing retirement has no confirmed runtime absence"))
	}
	actual, err := directoryIdentity(record.InputBundle)
	if err != nil || actual != *record.InputDirectory {
		return errors.Join(err, fmt.Errorf("original Sandbox bundle directory changed"))
	}
	return nil
}

func (e *Engine) sandboxBackingDependencies(ctx context.Context, storePath string, paths []string) error {
	bases, err := e.exitEntries("bases")
	if err != nil {
		return err
	}
	for _, name := range paths {
		absolute := storePath + "/" + name
		if err := e.retirementPathDependencies(ctx, absolute); err != nil {
			return err
		}
		for _, baseName := range bases {
			base, err := e.readBase(baseName.Name())
			if err != nil || base.Tree.Immutable == nil {
				return errors.Join(err, fmt.Errorf("registered base identity cannot be verified"))
			}
			if below(base.Tree.Immutable.Root, absolute) || below(absolute, base.Tree.Immutable.Root) {
				return fmt.Errorf("registered base protects retiring private input")
			}
		}
	}
	return nil
}

// RetireDeletedBacking is explicitly authorized by the original Start input and
// completed Delete. It removes mutable private payloads, never the canonical
// Source, registered bases, request/configuration records or the bundle anchor.
func (e *Engine) RetireDeletedBacking(ctx context.Context, request SandboxBackingRequest) (result SandboxBackingRetirement, resultErr error) {
	lock, err := e.lock()
	if err != nil {
		return result, err
	}
	defer lock.Close()
	if e.config.BoundedStore == nil {
		return result, fmt.Errorf("Sandbox backing retirement requires the explicit bounded-store profile")
	}
	record, err := e.read(request.SandboxID)
	if err != nil {
		return result, err
	}
	if err := sandboxBackingRecord(record, request); err != nil {
		return result, err
	}
	storePath := filepath.Dir(e.config.ManagerRoot)
	if !withinStore(storePath, record.InputBundle) || !withinStore(storePath, e.config.Root) {
		return result, fmt.Errorf("Sandbox backing escapes its original worker store")
	}
	if err := e.boundedObject(storePath); err != nil {
		return result, err
	}
	if err := privateDirectory(storePath); err != nil {
		return result, err
	}
	store, err := os.OpenRoot(storePath)
	if err != nil {
		return result, err
	}
	defer store.Close()
	storeIdentity, err := directoryIdentity(storePath)
	if err != nil {
		return result, err
	}
	if err := e.sandboxBackingSafety(ctx, record, lock); err != nil {
		return result, err
	}
	bundle, err := filepath.Rel(storePath, record.InputBundle)
	if err != nil {
		return result, err
	}
	allowed := map[string]bool{bundle + "/upper": true, bundle + "/work": true}
	required := map[string]bool{bundle + "/upper": true, bundle + "/work": true}
	if record.Base == nil {
		allowed[bundle+"/lower"], required[bundle+"/lower"] = true, true
	}
	pauses := append([]PauseOperation(nil), record.PauseHistory...)
	if record.Pause != nil {
		pauses = append(pauses, *record.Pause)
	}
	for _, pause := range pauses {
		if pause.Resume != nil && pause.Resume.CompletedAt != "" && tokenID.MatchString(pause.SnapshotID) {
			allowed[bundle+"/resume-work-"+pause.SnapshotID] = true
		}
	}
	relative := "objects/" + record.ID + "/backing-retirement"
	if err := e.root.Mkdir(relative, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return result, err
	}
	if err := privateDirectory(e.config.Root + "/" + relative); err != nil {
		return result, err
	}
	directory, err := directoryIdentity(e.config.Root + "/" + relative)
	if err != nil {
		return result, err
	}
	var intent sandboxBackingIntent
	err = e.readCheckpointJSON(relative+"/retirement-intent.json", &intent)
	first := errors.Is(err, os.ErrNotExist)
	if err != nil && !first {
		return result, err
	}
	if record.Fork != nil {
		// Fork manifests reserve exact private filesystem/image roots. The
		// currently deleted fork can no longer mutate them. The common
		// immutable lower alias has already been uncovered by native Delete.
		operation := "forks/" + record.ID
		fork, err := e.readForkIntent(operation, record.Fork.SourceID, record.Fork.CheckpointID, record.ID)
		if err != nil {
			return result, err
		}
		var prepared forkPreparation
		if err := e.readCheckpointJSON(operation+"/preparation.json", &prepared); err != nil {
			return result, err
		}
		data, _ := json.Marshal(fork)
		if fork.Owner != record.Owner || !reflect.DeepEqual(&fork.Origin, record.Fork) || sum(fork.OCI) != record.InputSHA256 ||
			prepared.Version != 1 || prepared.Owner != record.Owner || prepared.IntentSHA256 != sum(data) ||
			prepared.PreparedAt == "" || prepared.Filesystem == nil || len(prepared.Filesystem.Roots) > 9 {
			return result, fmt.Errorf("deleted fork has no exact independent preparation")
		}
		if err := epochRestoreReceipt(e, "objects/"+record.ID+"/restore", record, e.forkRestoreRequest(fork, record), record.Start.CompletedAt); err != nil {
			return result, err
		}
		materialized := operation + "/materialized"
		images, _ := filepath.Rel(storePath, e.config.Root+"/"+materialized+"/images")
		allowed[images], required[images] = true, true
		allowed[bundle+"/restore-work"] = true
		seen := map[string]bool{}
		for _, tree := range prepared.Filesystem.Roots {
			if tree.Saved != record.InputBundle+"/"+tree.Name || !epochFilesystemName(tree.Name) ||
				tree.Immutable != nil && tree.Name != "lower" || seen[tree.Name] {
				return result, fmt.Errorf("deleted fork filesystem escaped its exact private input")
			}
			seen[tree.Name] = true
			if tree.Name != "lower" || record.Base == nil {
				allowed[bundle+"/"+tree.Name], required[bundle+"/"+tree.Name] = true, true
			}
		}
		if !seen["lower"] || !seen["upper"] {
			return result, fmt.Errorf("deleted fork lacks its complete private filesystem")
		}
		if first {
			actual, err := e.checkpointFiles(ctx, materialized)
			if err != nil || !checkpointPreparationIntact(prepared.Files, actual) {
				return result, errors.Join(err, fmt.Errorf("deleted fork original images changed"))
			}
		}
	}
	if first {
		for _, name := range []string{"rootfs", "lower"} {
			identity, err := directoryIdentity(record.InputBundle + "/" + name)
			if err != nil || identity.MountID != record.InputDirectory.MountID || identity.Device != record.InputDirectory.Device {
				return result, errors.Join(err, fmt.Errorf("private input mounts must be released before backing retirement"))
			}
			if name == "rootfs" || record.Base != nil {
				entries, err := os.ReadDir(record.InputBundle + "/" + name)
				if err != nil || len(entries) != 0 {
					return result, errors.Join(err, fmt.Errorf("uncovered private input mountpoint is not empty"))
				}
			}
		}
		paths := []string{}
		for name := range allowed {
			if _, err := store.Lstat(name); err == nil {
				paths = append(paths, name)
			} else if !errors.Is(err, os.ErrNotExist) || required[name] {
				return result, errors.Join(err, fmt.Errorf("required private input payload is missing"))
			}
		}
		sort.Strings(paths)
		if err := e.sandboxBackingDependencies(ctx, storePath, paths); err != nil {
			return result, err
		}
		plan, err := ownedtree.Capture(ctx, store, paths)
		if err != nil {
			return result, err
		}
		before, err := e.storageCapacity()
		if err != nil {
			return result, err
		}
		hash, err := e.writeRetirementInventory(relative, plan)
		if err != nil {
			return result, err
		}
		reserve, err := e.prepareRetirementReserve(relative)
		if err != nil {
			return result, err
		}
		intent = sandboxBackingIntent{Version: 1, Request: request, Directory: directory, Bundle: *record.InputDirectory,
			Store: storeIdentity, InventorySHA256: hash, Payloads: paths, Reserve: reserve, StartedAt: now(), Before: before}
		if err := e.publishCheckpoint(relative, "retirement-intent.json", intent); err != nil {
			return result, err
		}
	}
	if intent.Version != 1 || intent.Request != request || intent.Directory != directory ||
		intent.Bundle != *record.InputDirectory || intent.Store != storeIdentity ||
		!digest.MatchString(intent.InventorySHA256) || intent.StartedAt == "" || len(intent.Payloads) == 0 {
		return result, fmt.Errorf("Sandbox backing retirement intent differs from its original identity")
	}
	seen := map[string]bool{}
	for _, name := range intent.Payloads {
		if !allowed[name] || seen[name] {
			return result, fmt.Errorf("Sandbox backing retirement contains an unauthorized payload")
		}
		seen[name] = true
	}
	for name := range required {
		if !seen[name] {
			return result, fmt.Errorf("Sandbox backing retirement lost a required payload")
		}
	}
	absent := func() error {
		for _, name := range intent.Payloads {
			if _, err := store.Lstat(name); !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("retired private backing has a successor: %s: %v", name, err)
			}
		}
		return nil
	}
	data, _ := json.Marshal(intent)
	if err := e.readCheckpointJSON(relative+"/retirement-complete.json", &result); err == nil {
		if result.Version != 1 || result.PayloadKind != "sandbox-backing" || result.Request != request ||
			result.IntentSHA256 != sum(data) || result.CompletedAt == "" || !reflect.DeepEqual(result.Before, intent.Before) {
			return result, fmt.Errorf("Sandbox backing completion differs from its original intent")
		}
		return result, absent()
	} else if !errors.Is(err, os.ErrNotExist) {
		return result, err
	}
	plan, err := e.readRetirementInventory(relative, intent.InventorySHA256)
	if err != nil {
		return result, err
	}
	if !reflect.DeepEqual(plan.Paths, intent.Payloads) {
		return result, fmt.Errorf("Sandbox backing inventory differs from its exact accepted paths")
	}
	if err := e.sandboxBackingDependencies(ctx, storePath, intent.Payloads); err != nil {
		return result, err
	}
	if err := ownedtree.RemoveRemaining(ctx, store, plan); err != nil {
		return result, err
	}
	if err := absent(); err != nil {
		return result, err
	}
	if err := e.releaseRetirementReserve(relative, intent.Reserve); err != nil {
		return result, err
	}
	after, err := e.storageCapacity()
	if err != nil {
		return result, err
	}
	result = SandboxBackingRetirement{Version: 1, PayloadKind: "sandbox-backing", Request: request,
		IntentSHA256: sum(data), CompletedAt: now(), Before: intent.Before, After: after}
	return result, e.publishCheckpoint(relative, "retirement-complete.json", result)
}
