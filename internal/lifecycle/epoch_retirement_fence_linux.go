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
	"path/filepath"
	"reflect"

	"factorybox/internal/ownedtree"
	"factorybox/internal/storefs"
	"golang.org/x/sys/unix"
)

const epochRetirementDirectory = ".epoch-retirement"
const epochRetirementFenceName = "epoch-retirement.json"

// These are internal coordination records. A caller must first authorize the
// complete epoch inventory, every dependency and each recovery handover while
// holding all affected controller locks. Publishing a fence is not admission.
type epochRecoveryHandover struct {
	Source    CheckpointRequest         `json:"source"`
	Successor RetainedCheckpointRequest `json:"successor"`
}

type epochRetirementRequest struct {
	SourceRoot string                  `json:"source_root"`
	Handovers  []epochRecoveryHandover `json:"handovers"`
}

type epochRetirementPreparation struct {
	Version         int                    `json:"version"`
	Request         epochRetirementRequest `json:"request"`
	BootID          string                 `json:"boot_id"`
	Image           storefs.Image          `json:"image"`
	Attachment      storefs.Active         `json:"attachment"`
	InventorySHA256 string                 `json:"inventory_sha256"`
	PreparedAt      string                 `json:"prepared_at"`
}

type epochRetirementFence struct {
	Version           int           `json:"version"`
	SourceRoot        string        `json:"source_root"`
	BootID            string        `json:"boot_id"`
	Image             storefs.Image `json:"image"`
	PreparationSHA256 string        `json:"preparation_sha256"`
	Token             string        `json:"token"`
	StartedAt         string        `json:"started_at"`
}

type epochRetirementAcceptance struct {
	Version    int                  `json:"version"`
	Fence      epochRetirementFence `json:"fence"`
	Attachment storefs.Active       `json:"attachment"`
	FencePlan  ownedtree.Plan       `json:"fence_plan"`
	AcceptedAt string               `json:"accepted_at"`
}

type epochRetirementCompletion struct {
	Version           int    `json:"version"`
	SourceRoot        string `json:"source_root"`
	PreparationSHA256 string `json:"preparation_sha256"`
	AcceptanceSHA256  string `json:"acceptance_sha256"`
	CompletedAt       string `json:"completed_at"`
}

// Derive the location from the controller layout even for explicit unbounded
// CLI options. Otherwise omitting --worker-run-root could bypass the durable
// store fence protecting a successor in this same controller.
func epochStoreRoot(root string) string {
	epoch := filepath.Dir(root)
	epochs := filepath.Dir(epoch)
	if filepath.Base(root) != "controller" || !tokenID.MatchString(filepath.Base(epoch)) ||
		filepath.Base(epochs) != "epochs" || !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return ""
	}
	return filepath.Dir(epochs)
}

func (e *Engine) epochRetirementFence() error {
	if _, err := e.root.Lstat(epochRetirementDirectory); !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("controller is closed for epoch retirement: %v", err)
	}
	store := epochStoreRoot(e.config.Root)
	if e.config.BoundedStore != nil {
		bounded := filepath.Dir(e.config.ManagerRoot)
		if store != "" && store != bounded {
			return fmt.Errorf("epoch retirement fence cannot resolve the bounded controller store")
		}
		store = bounded
	}
	if store != "" {
		if _, err := os.Lstat(store + "/" + epochRetirementFenceName); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("store is closed for epoch retirement: %v", err)
		}
	}
	return nil
}

func (e *Engine) epochRetirementStore(source *Engine) (*Engine, error) {
	if err := retainedControllerPath(e.config, source.config.Root); err != nil {
		return nil, err
	}
	if source.boot != e.boot || source.config.RuntimeSHA256 != e.config.RuntimeSHA256 ||
		source.config.CriuSHA256 != e.config.CriuSHA256 {
		return nil, fmt.Errorf("epoch retirement source recipe or boot differs")
	}
	store := epochStoreRoot(e.config.Root)
	if canonical(store) != nil || privateDirectory(store) != nil {
		return nil, fmt.Errorf("epoch retirement store is not canonical and private")
	}
	if err := e.boundedObject(store); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(store)
	if err != nil {
		return nil, err
	}
	return &Engine{root: root}, nil
}

func (e *Engine) epochRetirementPreparation(source *Engine, expected string) (epochRetirementPreparation, error) {
	var prepared epochRetirementPreparation
	if err := source.readCheckpointJSON(epochRetirementDirectory+"/prepare.json", &prepared); err != nil {
		return prepared, err
	}
	data, err := json.Marshal(prepared)
	if err != nil || !digest.MatchString(expected) || sum(data) != expected || prepared.Version != 1 ||
		prepared.Request.SourceRoot != source.config.Root || prepared.BootID != e.boot ||
		prepared.Image != e.config.BoundedStore.Image || prepared.Attachment.Image != prepared.Image ||
		!digest.MatchString(prepared.InventorySHA256) || prepared.PreparedAt == "" {
		return prepared, fmt.Errorf("epoch retirement preparation differs from its exact saved request")
	}
	return prepared, nil
}

// Only attachment-relative device and mount IDs can change. Inode, owner,
// mode, complete content hashes and every original path remain exact. This is
// used only after independent same-image verification under the store lease.
func rebindEpochRetirementPlan(plan ownedtree.Plan, previous, current storefs.Active) (ownedtree.Plan, error) {
	if previous.Image != current.Image || previous.Target != current.Target || !previous.DirectIO || !current.DirectIO ||
		previous.FilesystemMax != current.FilesystemMax || previous.LoopNumber < 0 ||
		previous.BlockDevice != uint64(unix.Mkdev(7, uint32(previous.LoopNumber))) || previous.Mount.Device != previous.BlockDevice ||
		previous.Mount.Inode != current.Mount.Inode || previous.Mount.MountID == 0 || current.Mount.MountID == 0 ||
		current.LoopNumber < 0 || current.BlockDevice != uint64(unix.Mkdev(7, uint32(current.LoopNumber))) || current.Mount.Device != current.BlockDevice {
		return ownedtree.Plan{}, fmt.Errorf("epoch retirement attachment changed its persistent identity")
	}
	translate := func(object *ownedtree.Object) error {
		if object.Device != previous.BlockDevice || object.MountID != previous.Mount.MountID {
			return fmt.Errorf("epoch retirement inventory crosses its original attachment")
		}
		object.Device, object.MountID = current.BlockDevice, current.Mount.MountID
		return nil
	}
	// Never modify the archived plan or share its mutable backing arrays.
	result := plan
	result.Paths = append([]string(nil), plan.Paths...)
	result.Anchors = append([]ownedtree.Entry(nil), plan.Anchors...)
	result.Entries = append([]ownedtree.Entry(nil), plan.Entries...)
	if err := translate(&result.Root); err != nil {
		return ownedtree.Plan{}, err
	}
	for _, entries := range [][]ownedtree.Entry{result.Anchors, result.Entries} {
		for i := range entries {
			if err := translate(&entries[i].Object); err != nil {
				return ownedtree.Plan{}, err
			}
		}
	}
	return result, nil
}

func (e *Engine) verifyEpochRetirementFence(ctx context.Context, store *Engine, accepted epochRetirementAcceptance) (ownedtree.Plan, error) {
	if accepted.Version != 1 || accepted.AcceptedAt == "" || accepted.Attachment.Image != e.config.BoundedStore.Image ||
		accepted.Fence.Version != 1 || retainedControllerPath(e.config, accepted.Fence.SourceRoot) != nil ||
		accepted.Fence.BootID != e.boot || accepted.Fence.Image != e.config.BoundedStore.Image ||
		!digest.MatchString(accepted.Fence.PreparationSHA256) || !tokenID.MatchString(accepted.Fence.Token) || accepted.Fence.StartedAt == "" ||
		accepted.FencePlan.Version != 1 || len(accepted.FencePlan.Paths) != 1 || accepted.FencePlan.Paths[0] != epochRetirementFenceName ||
		len(accepted.FencePlan.Anchors) != 0 || len(accepted.FencePlan.Entries) != 1 || accepted.FencePlan.Entries[0].Path != epochRetirementFenceName {
		return ownedtree.Plan{}, fmt.Errorf("epoch retirement acceptance has no exact original fence inventory")
	}
	plan, err := rebindEpochRetirementPlan(accepted.FencePlan, accepted.Attachment, *e.config.BoundedStore)
	if err != nil {
		return plan, err
	}
	actual, err := ownedtree.Capture(ctx, store.root, []string{epochRetirementFenceName})
	if err != nil {
		return plan, err
	}
	if !reflect.DeepEqual(plan, actual) {
		return plan, fmt.Errorf("epoch retirement fence object or content changed")
	}
	var fence epochRetirementFence
	if err := store.readCheckpointJSON(epochRetirementFenceName, &fence); err != nil {
		return plan, err
	}
	if fence != accepted.Fence {
		return plan, fmt.Errorf("epoch retirement fence differs from its accepted request")
	}
	return plan, nil
}

// acceptEpochRetirement publishes the store-wide fence before acknowledging
// acceptance. A crash between fence publication and the source intent leaves
// all operations blocked and permits only an exact, independently revalidated
// retry. The caller must hold current/source/dependency locks and must not
// remove payloads until this returns an accepted record.
func (e *Engine) acceptEpochRetirement(ctx context.Context, source *Engine, preparationSHA string) (epochRetirementAcceptance, error) {
	var accepted epochRetirementAcceptance
	if err := ctx.Err(); err != nil {
		return accepted, err
	}
	store, err := e.epochRetirementStore(source)
	if err != nil {
		return accepted, err
	}
	defer store.Close()
	if _, err := e.epochRetirementPreparation(source, preparationSHA); err != nil {
		return accepted, err
	}
	var fence epochRetirementFence
	err = store.readCheckpointJSON(epochRetirementFenceName, &fence)
	if errors.Is(err, os.ErrNotExist) {
		// An existing source acceptance can never create a replacement fence.
		if _, err := source.root.Lstat(epochRetirementDirectory + "/intent.json"); !errors.Is(err, os.ErrNotExist) {
			return accepted, fmt.Errorf("epoch retirement acceptance lost its original fence: %v", err)
		}
		var token [16]byte
		if _, err := rand.Read(token[:]); err != nil {
			return accepted, err
		}
		fence = epochRetirementFence{Version: 1, SourceRoot: source.config.Root, BootID: e.boot,
			Image: e.config.BoundedStore.Image, PreparationSHA256: preparationSHA, Token: hex.EncodeToString(token[:]), StartedAt: now()}
		if err := store.publishCheckpoint(".", epochRetirementFenceName, fence); err != nil {
			return accepted, err
		}
	} else if err != nil {
		return accepted, err
	}
	if fence.Version != 1 || fence.SourceRoot != source.config.Root || fence.BootID != e.boot ||
		fence.Image != e.config.BoundedStore.Image || fence.PreparationSHA256 != preparationSHA || !tokenID.MatchString(fence.Token) || fence.StartedAt == "" {
		return accepted, fmt.Errorf("another or changed epoch retirement already owns the store fence")
	}
	err = source.readCheckpointJSON(epochRetirementDirectory+"/intent.json", &accepted)
	if errors.Is(err, os.ErrNotExist) {
		plan, err := ownedtree.Capture(ctx, store.root, []string{epochRetirementFenceName})
		if err != nil {
			return accepted, err
		}
		accepted = epochRetirementAcceptance{Version: 1, Fence: fence, Attachment: *e.config.BoundedStore,
			FencePlan: plan, AcceptedAt: now()}
		if err := source.publishCheckpoint(epochRetirementDirectory, "intent.json", accepted); err != nil {
			return accepted, err
		}
	} else if err != nil {
		return accepted, err
	}
	if accepted.Fence != fence {
		return accepted, fmt.Errorf("epoch retirement acceptance differs from the active store fence")
	}
	_, err = e.verifyEpochRetirementFence(ctx, store, accepted)
	return accepted, err
}

// A completion never authorizes removal of a successor occupying a retired
// path. Every payload root must be absent and the original fence inode/content
// must still match. Source preparation, acceptance and completion remain.
func (e *Engine) finishEpochRetirementFence(ctx context.Context, source *Engine, accepted epochRetirementAcceptance) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	store, err := e.epochRetirementStore(source)
	if err != nil {
		return err
	}
	defer store.Close()
	prepared, err := e.epochRetirementPreparation(source, accepted.Fence.PreparationSHA256)
	if err != nil {
		return err
	}
	var original epochRetirementAcceptance
	if err := source.readCheckpointJSON(epochRetirementDirectory+"/intent.json", &original); err != nil {
		return err
	}
	if !reflect.DeepEqual(original, accepted) || accepted.Fence.SourceRoot != source.config.Root {
		return fmt.Errorf("epoch retirement completion lacks its original acceptance")
	}
	var complete epochRetirementCompletion
	if err := source.readCheckpointJSON(epochRetirementDirectory+"/complete.json", &complete); err != nil {
		return err
	}
	data, err := json.Marshal(accepted)
	if err != nil || complete.Version != 1 || complete.SourceRoot != source.config.Root ||
		complete.PreparationSHA256 != accepted.Fence.PreparationSHA256 || complete.AcceptanceSHA256 != sum(data) || complete.CompletedAt == "" {
		return fmt.Errorf("epoch retirement completion differs from its accepted operation")
	}
	payload, err := source.readRetirementInventory(epochRetirementDirectory, prepared.InventorySHA256)
	if err != nil {
		return err
	}
	payload, err = rebindEpochRetirementPlan(payload, prepared.Attachment, *e.config.BoundedStore)
	if err != nil {
		return err
	}
	if err := ownedtree.ValidateRemaining(ctx, source.root, payload); err != nil {
		return err
	}
	for _, path := range payload.Paths {
		if _, err := source.root.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("epoch retirement still has payload or a successor: %s: %v", path, err)
		}
	}
	plan, err := e.verifyEpochRetirementFence(ctx, store, accepted)
	if errors.Is(err, os.ErrNotExist) {
		// The complete source tombstone proves an exact finished retry. A
		// present foreign fence, including a symlink, never reaches this path.
		if _, exists := store.root.Lstat(epochRetirementFenceName); errors.Is(exists, os.ErrNotExist) {
			return nil
		}
	}
	if err != nil {
		return err
	}
	return ownedtree.RemoveRemaining(ctx, store.root, plan)
}
