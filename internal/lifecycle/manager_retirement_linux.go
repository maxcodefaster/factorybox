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
	"golang.org/x/sys/unix"
)

// Sysbox allocates these outside the caller's OCI overlay. Retiring upper/work
// alone does not release a stopped guest's private Docker or Kubernetes data.
var retiredManagerKinds = []string{
	"buildkit", "containerd", "docker", "k0s", "kubelet", "rancher-k3s", "rancher-rke2",
}

func protectedManagerRoot(path string) error {
	if err := canonical(path); err != nil {
		return err
	}
	var stat unix.Stat_t
	if err := unix.Lstat(path, &stat); err != nil {
		return err
	}
	// Sysbox uses 0710 to traverse this root from its mapped guests. It is
	// not a controller-private 0700 directory, and must not be chmod-repaired.
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Uid != 0 || stat.Mode&0022 != 0 {
		return fmt.Errorf("Sysbox manager root has unsafe ownership or write access")
	}
	return nil
}

func managerBackingPayloads(root *os.Root, record Record) ([]string, error) {
	if !tokenID.MatchString(record.Owner) || record.RuntimeID != "fbx-"+record.Owner {
		return nil, fmt.Errorf("manager backing differs from the original native runtime owner")
	}
	var paths []string
	for _, kind := range retiredManagerKinds {
		parent, err := root.Lstat(kind)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !parent.IsDir() {
			return nil, errors.Join(err, fmt.Errorf("manager kind is not an original private directory"))
		}
		name := kind + "/" + record.RuntimeID
		info, err := root.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !info.IsDir() {
			return nil, errors.Join(err, fmt.Errorf("manager backing is not an original private directory"))
		}
		paths = append(paths, name)
	}
	sort.Strings(paths)
	return paths, nil
}

func verifiedManagerBackingPayloads(root *os.Root, record Record) ([]string, error) {
	paths, err := managerBackingPayloads(root, record)
	if err != nil {
		return nil, err
	}
	for _, path := range paths {
		// Sysbox owns both the kind and runtime directories. Guest-owned
		// content may exist below them, but cannot nominate a replacement
		// root merely by reusing the stopped runtime's name.
		for _, name := range []string{filepath.Dir(path), path} {
			if err := protectedManagerRoot(root.Name() + "/" + name); err != nil {
				return nil, err
			}
		}
	}
	return paths, nil
}

type managerBackingIntent struct {
	Version         int                   `json:"version"`
	Request         SandboxBackingRequest `json:"request"`
	Controller      string                `json:"controller"`
	RuntimeID       string                `json:"runtime_id"`
	Bundle          KernelObject          `json:"bundle"`
	Manager         KernelObject          `json:"manager"`
	Directory       KernelObject          `json:"directory"`
	InventorySHA256 string                `json:"inventory_sha256"`
	Payloads        []string              `json:"payloads"`
	StartedAt       string                `json:"started_at"`
	Before          StorageCapacity       `json:"before"`
}

// A guest may exhaust its bounded filesystem. Put this small recovery journal
// beside the original bounded image, on its already-admitted private backing
// volume, never in the guest-writable image or in canonical Source.
func (e *Engine) managerRetirementJournal(record Record) (*Engine, string, error) {
	if e.config.BoundedStore == nil {
		return nil, "", fmt.Errorf("manager retirement requires the original bounded store")
	}
	parent := filepath.Dir(e.config.BoundedStore.Image.Path)
	if err := canonical(parent); err != nil {
		return nil, "", err
	}
	if err := privateDirectory(parent); err != nil {
		return nil, "", err
	}
	journals := parent + "/manager-retirements"
	if err := os.Mkdir(journals, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, "", err
	}
	if err := privateDirectory(journals); err != nil {
		return nil, "", err
	}
	root, err := os.OpenRoot(journals)
	if err != nil {
		return nil, "", err
	}
	name := record.Owner + "-" + record.InputSHA256
	if err := root.Mkdir(name, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		root.Close()
		return nil, "", err
	}
	if err := privateDirectory(journals + "/" + name); err != nil {
		root.Close()
		return nil, "", err
	}
	for _, path := range []string{parent, journals} {
		directory, err := os.Open(path)
		if err != nil {
			root.Close()
			return nil, "", err
		}
		if err := errors.Join(directory.Sync(), directory.Close()); err != nil {
			root.Close()
			return nil, "", err
		}
	}
	// Only the existing bounded JSON/inventory I/O helpers use this metadata
	// handle. Runtime commands and admission remain on the original engine.
	return &Engine{config: Config{Root: journals}, root: root}, name, nil
}

// deletedBackingForest also locks retained epochs: manager paths are shared
// across epochs, unlike one controller's private overlay directories.
func (e *Engine) deletedBackingForest() (*epochRetirementForest, error) {
	if e.config.BoundedStore == nil {
		return nil, fmt.Errorf("Sandbox backing retirement requires the explicit bounded-store profile")
	}
	forest, err := e.retirementForest()
	if err != nil {
		return nil, err
	}
	if err := e.epochRetirementFence(); err != nil {
		forest.close()
		return nil, err
	}
	return forest, nil
}

func (e *Engine) managerBackingDependencies(ctx context.Context, forest *epochRetirementForest, record Record, paths []string) error {
	for _, controller := range forest.controllers {
		if err := ctx.Err(); err != nil {
			return err
		}
		objects, err := controller.exitEntries("objects")
		if err != nil {
			return err
		}
		for _, object := range objects {
			peer, err := controller.read(object.Name())
			if err != nil {
				return err
			}
			if peer.RuntimeID == record.RuntimeID && (controller != e || peer.ID != record.ID) {
				return fmt.Errorf("another Sandbox or retained epoch references the original manager owner")
			}
		}
		if err := controller.sandboxBackingDependencies(ctx, e.config.ManagerRoot, paths); err != nil {
			return err
		}
	}
	return nil
}

// RetireDeletedManagerBacking is also available for exact recovery of historical
// overlay-only retirements. The ordinary RetireDeletedBacking operation invokes
// the same stage automatically, including when replaying an old overlay receipt.
func (e *Engine) RetireDeletedManagerBacking(ctx context.Context, request SandboxBackingRequest) (result SandboxBackingRetirement, resultErr error) {
	forest, err := e.deletedBackingForest()
	if err != nil {
		return result, err
	}
	defer forest.close()
	return e.retireDeletedManagerBacking(ctx, request, forest)
}

func (e *Engine) retireDeletedManagerBacking(ctx context.Context, request SandboxBackingRequest, forest *epochRetirementForest) (result SandboxBackingRetirement, resultErr error) {
	record, err := e.read(request.SandboxID)
	if err != nil {
		return result, err
	}
	if err := sandboxBackingRecord(record, request); err != nil {
		return result, err
	}
	if record.RuntimeID != "fbx-"+record.Owner {
		return result, fmt.Errorf("manager backing differs from the original native runtime owner")
	}
	if err := e.sandboxBackingSafety(ctx, record, forest.locks[0]); err != nil {
		return result, err
	}
	before, err := e.storageCapacity()
	if err != nil || e.config.BoundedStore == nil {
		return result, errors.Join(err, fmt.Errorf("manager backing has no verified bounded store"))
	}
	if err := e.boundedObject(e.config.ManagerRoot); err != nil {
		return result, err
	}
	if err := protectedManagerRoot(e.config.ManagerRoot); err != nil {
		return result, err
	}
	manager, err := os.OpenRoot(e.config.ManagerRoot)
	if err != nil {
		return result, err
	}
	defer manager.Close()
	managerIdentity, err := directoryIdentity(e.config.ManagerRoot)
	if err != nil {
		return result, err
	}
	journal, relative, err := e.managerRetirementJournal(record)
	if err != nil {
		return result, err
	}
	defer journal.root.Close()
	directory, err := directoryIdentity(journal.config.Root + "/" + relative)
	if err != nil {
		return result, err
	}
	var intent managerBackingIntent
	err = journal.readCheckpointJSON(relative+"/retirement-intent.json", &intent)
	first := errors.Is(err, os.ErrNotExist)
	if err != nil && !first {
		return result, err
	}
	if first {
		if err := journal.resetUnacceptedRetirementPreparation(ctx, relative); err != nil {
			return result, err
		}
		paths, err := verifiedManagerBackingPayloads(manager, record)
		if err != nil {
			return result, err
		}
		if err := e.managerBackingDependencies(ctx, forest, record, paths); err != nil {
			return result, err
		}
		plan := ownedtree.Plan{Version: 1}
		if len(paths) != 0 {
			plan, err = ownedtree.Capture(ctx, manager, paths)
			if err != nil {
				return result, err
			}
		}
		hash, err := journal.writeRetirementInventory(relative, plan)
		if err != nil {
			return result, err
		}
		intent = managerBackingIntent{Version: 1, Request: request, Controller: e.config.Root,
			RuntimeID: record.RuntimeID, Bundle: *record.InputDirectory, Manager: managerIdentity,
			Directory: directory, InventorySHA256: hash, Payloads: paths, StartedAt: now(), Before: before}
		if err := journal.publishCheckpoint(relative, "retirement-intent.json", intent); err != nil {
			return result, err
		}
	}
	if intent.Version != 1 || intent.Request != request || intent.Controller != e.config.Root ||
		intent.RuntimeID != record.RuntimeID || intent.Bundle != *record.InputDirectory ||
		intent.Manager != managerIdentity || intent.Directory != directory ||
		!digest.MatchString(intent.InventorySHA256) || intent.StartedAt == "" {
		return result, fmt.Errorf("manager retirement differs from its original native ownership")
	}
	allowed := map[string]bool{}
	for _, kind := range retiredManagerKinds {
		allowed[kind+"/"+record.RuntimeID] = true
	}
	seen := map[string]bool{}
	for _, path := range intent.Payloads {
		if !allowed[path] || seen[path] {
			return result, fmt.Errorf("manager retirement contains a foreign payload")
		}
		seen[path] = true
	}
	absent := func() error {
		current, err := verifiedManagerBackingPayloads(manager, record)
		if err != nil || len(current) != 0 {
			return errors.Join(err, fmt.Errorf("retired manager backing has remaining or successor data"))
		}
		return nil
	}
	data, _ := json.Marshal(intent)
	if err := journal.readCheckpointJSON(relative+"/retirement-complete.json", &result); err == nil {
		if result.Version != 1 || result.PayloadKind != "sandbox-manager-backing" ||
			result.Request != request || result.IntentSHA256 != sum(data) || result.CompletedAt == "" ||
			!reflect.DeepEqual(result.Before, intent.Before) {
			return result, fmt.Errorf("manager completion differs from its original intent")
		}
		return result, absent()
	} else if !errors.Is(err, os.ErrNotExist) {
		return result, err
	}
	plan, err := journal.readRetirementInventory(relative, intent.InventorySHA256)
	if err != nil || !reflect.DeepEqual(plan.Paths, intent.Payloads) {
		return result, errors.Join(err, fmt.Errorf("manager inventory differs from its accepted paths"))
	}
	if err := e.managerBackingDependencies(ctx, forest, record, intent.Payloads); err != nil {
		return result, err
	}
	current, err := verifiedManagerBackingPayloads(manager, record)
	if err != nil {
		return result, err
	}
	for _, path := range current {
		if !seen[path] {
			return result, fmt.Errorf("manager retirement found an unadmitted successor payload")
		}
	}
	if len(intent.Payloads) != 0 {
		if err := ownedtree.RemoveRemaining(ctx, manager, plan); err != nil {
			return result, err
		}
	}
	if err := absent(); err != nil {
		return result, err
	}
	after, err := e.storageCapacity()
	if err != nil {
		return result, err
	}
	result = SandboxBackingRetirement{Version: 1, PayloadKind: "sandbox-manager-backing", Request: request,
		IntentSHA256: sum(data), CompletedAt: now(), Before: intent.Before, After: after}
	return result, journal.publishCheckpoint(relative, "retirement-complete.json", result)
}
