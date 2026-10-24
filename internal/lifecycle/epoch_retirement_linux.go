//go:build linux

package lifecycle

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"factorybox/internal/ownedtree"
	"factorybox/internal/sysboxcapture"
	"golang.org/x/sys/unix"
)

// EpochRetirement reports withdrawal of controller-owned backing. Caller-owned
// input bundles, vendor manager storage and every request/provenance record are
// retained. Capacity is observed; unlinking names does not certify free blocks.
type EpochRetirement struct {
	Version          int                    `json:"version"`
	Request          epochRetirementRequest `json:"request"`
	AcceptanceSHA256 string                 `json:"acceptance_sha256"`
	CompletedAt      string                 `json:"completed_at"`
	AcceptedAt       string                 `json:"accepted_at,omitempty"`
	Before           StorageCapacity        `json:"before"`
	After            StorageCapacity        `json:"after"`
}

type epochRetirementForest struct {
	controllers map[string]*Engine
	locks       []*os.File
	opened      []*Engine
}

func (f *epochRetirementForest) close() {
	for i := len(f.locks) - 1; i >= 0; i-- {
		f.locks[i].Close()
	}
	for i := len(f.opened) - 1; i >= 0; i-- {
		f.opened[i].Close()
	}
}

func (e *Engine) retirementForest() (_ *epochRetirementForest, resultErr error) {
	forest := &epochRetirementForest{controllers: map[string]*Engine{e.config.Root: e}}
	defer func() {
		if resultErr != nil {
			forest.close()
		}
	}()
	lock, err := e.lockRoot()
	if err != nil {
		return nil, err
	}
	forest.locks = append(forest.locks, lock)
	for _, name := range []string{"worker-exit.json", "worker-exit-complete.json", epochRetirementDirectory} {
		if _, err := e.root.Lstat(name); !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("current retirement coordinator epoch is closed: %s: %v", name, err)
		}
	}
	parent := epochStoreRoot(e.config.Root) + "/epochs"
	if err := canonical(parent); err != nil {
		return nil, err
	}
	if err := privateDirectory(parent); err != nil {
		return nil, err
	}
	if err := e.boundedObject(parent); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) > 65536 {
		return nil, fmt.Errorf("retained epoch inventory unavailable or excessive: %v", err)
	}
	for _, entry := range entries {
		root := parent + "/" + entry.Name() + "/controller"
		if !tokenID.MatchString(entry.Name()) || !entry.IsDir() {
			return nil, fmt.Errorf("unknown retained epoch entry")
		}
		if root == e.config.Root {
			continue
		}
		old, err := e.openRetainedController(root)
		if err != nil {
			return nil, err
		}
		forest.opened = append(forest.opened, old)
		lock, err := old.lockRoot()
		if err != nil {
			return nil, err
		}
		forest.locks = append(forest.locks, lock)
		forest.controllers[root] = old
	}
	return forest, nil
}

func (e *Engine) normalizeEpochRetirement(request epochRetirementRequest) (epochRetirementRequest, error) {
	if err := retainedControllerPath(e.config, request.SourceRoot); err != nil {
		return request, err
	}
	request.Handovers = append([]epochRecoveryHandover(nil), request.Handovers...)
	sort.Slice(request.Handovers, func(i, j int) bool {
		return request.Handovers[i].Source.SourceID < request.Handovers[j].Source.SourceID
	})
	for i, handover := range request.Handovers {
		if _, err := handover.Source.path(); err != nil {
			return request, err
		}
		if _, err := handover.Successor.CheckpointRequest.path(); err != nil {
			return request, err
		}
		if handover.Successor.SourceRoot == request.SourceRoot || handover.Successor.SourceRoot != e.config.Root && retainedControllerPath(e.config, handover.Successor.SourceRoot) != nil || i > 0 && request.Handovers[i-1].Source.SourceID == handover.Source.SourceID {
			return request, fmt.Errorf("epoch retirement handovers require distinct exact successors and one entry per source")
		}
	}
	if _, err := checkpointRecordBytes(request); err != nil {
		return request, err
	}
	return request, nil
}

func usesEpoch(path, source string) bool {
	return filepath.IsAbs(path) && below(filepath.Clean(path), source)
}

func epochRecordReferences(record Record, config []byte, source string) error {
	if usesEpoch(record.Rootfs, source) || usesEpoch(record.InputBundle, source) || record.Base != nil && record.Base.Immutable != nil && usesEpoch(record.Base.Immutable.Root, source) {
		return fmt.Errorf("sandbox %s still requires retiring epoch backing", record.ID)
	}
	var spec struct {
		Root struct {
			Path string `json:"path"`
		} `json:"root"`
		Mounts []struct {
			Source string `json:"source"`
		} `json:"mounts"`
	}
	if err := json.Unmarshal(config, &spec); err != nil {
		return err
	}
	if usesEpoch(spec.Root.Path, source) {
		return fmt.Errorf("sandbox OCI root requires retiring epoch")
	}
	for _, mount := range spec.Mounts {
		if usesEpoch(mount.Source, source) {
			return fmt.Errorf("sandbox bind source requires retiring epoch")
		}
	}
	return nil
}

func epochSnapshotReferences(snapshot *sysboxcapture.FilesystemSnapshot, source string) error {
	if snapshot == nil {
		return fmt.Errorf("dependent preparation has no complete filesystem")
	}
	for _, tree := range snapshot.Roots {
		if usesEpoch(tree.Saved, source) || tree.Immutable != nil && usesEpoch(tree.Immutable.Root, source) {
			return fmt.Errorf("prepared filesystem still requires retiring epoch")
		}
	}
	return nil
}

func (e *Engine) handoverOrigin(target Record, request RecoveryRequest) error {
	seen := map[string]bool{}
	for {
		if seen[target.ID] || target.Fork == nil {
			return fmt.Errorf("successor has no acyclic recovery provenance to the exact old pause")
		}
		seen[target.ID] = true
		intent, err := e.readForkIntent("forks/"+target.ID, target.Fork.SourceID, target.Fork.CheckpointID, target.ID)
		if err != nil {
			return err
		}
		if intent.Owner != target.Owner || !reflect.DeepEqual(&intent.Origin, target.Fork) {
			return fmt.Errorf("handover provenance owner changed")
		}
		if intent.Recovery != nil {
			if intent.Recovery.Request != request {
				return fmt.Errorf("successor recovered a different old pause")
			}
			return nil
		}
		parent, err := e.read(intent.Source.ID)
		if err != nil || parent.Owner != intent.Source.Owner {
			return fmt.Errorf("handover ancestor identity differs: %v", err)
		}
		if _, _, _, err := e.checkpointRequest(CheckpointRequest{parent.ID, intent.Checkpoint.ID, parent.Owner, intent.Origin.CheckpointSHA256}); err != nil {
			return err
		}
		target = parent
	}
}

func (e *Engine) verifyEpochHandovers(ctx context.Context, forest *epochRetirementForest, request epochRetirementRequest, sourceRecords map[string]Record) error {
	protected := map[string]bool{}
	for _, handover := range request.Handovers {
		source, found := sourceRecords[handover.Source.SourceID]
		if !found || committedExitPause(source) != nil || source.Owner != handover.Source.SourceOwner || source.Pause.SnapshotID != handover.Source.SnapshotID || source.Pause.CheckpointSHA256 != handover.Source.CheckpointSHA256 {
			return fmt.Errorf("handover does not name the exact committed source pause")
		}
		target := forest.controllers[handover.Successor.SourceRoot]
		if target == nil {
			return fmt.Errorf("handover successor is outside the exact locked store")
		}
		record, saved, relative, err := target.checkpointRequest(handover.Successor.CheckpointRequest)
		if err != nil {
			return err
		}
		if err := committedExitPause(record); err != nil {
			return err
		}
		if record.Pause.SnapshotID != saved.ID || record.Pause.CheckpointSHA256 != handover.Successor.CheckpointSHA256 {
			return fmt.Errorf("handover successor is not its exact current committed pause")
		}
		if err := epochSnapshotReferences(saved.Filesystem, request.SourceRoot); err != nil {
			return err
		}
		if err := target.handoverOrigin(record, RecoveryRequest{SourceRoot: request.SourceRoot, SourceID: source.ID, SnapshotID: source.Pause.SnapshotID, SourceOwner: source.Owner, CheckpointSHA256: source.Pause.CheckpointSHA256}); err != nil {
			return err
		}
		if target == e {
			published, err := target.publishedCheckpointMetadata(ctx, record, saved.ID, relative)
			if err != nil || !reflect.DeepEqual(saved, published) {
				return fmt.Errorf("current successor checkpoint is incomplete or changed: %v", err)
			}
			if err := sysboxcapture.VerifyFilesystemSnapshot(ctx, target.config.Root+"/"+relative, saved.Filesystem); err != nil {
				return err
			}
		} else {
			if _, err := target.inspectRetainedCheckpoint(ctx, handover.Successor.CheckpointRequest); err != nil {
				return err
			}
		}
		protected[source.ID] = true
	}
	for _, source := range sourceRecords {
		if source.Phase == "paused" && !protected[source.ID] {
			return fmt.Errorf("committed source pause has no explicit independently verified successor: %s", source.ID)
		}
	}
	return nil
}

func (e *Engine) epochDependencies(ctx context.Context, forest *epochRetirementForest, request epochRetirementRequest) error {
	for root, controller := range forest.controllers {
		if root == request.SourceRoot {
			continue
		}
		if _, err := controller.root.Lstat(epochRetirementDirectory); err == nil {
			if _, err := e.completedEpochRetirement(ctx, controller); err != nil {
				return fmt.Errorf("another retained epoch retirement is incomplete: %w", err)
			}
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		entries, err := controller.exitEntries("objects")
		if err != nil {
			return err
		}
		records := map[string]Record{}
		metadata := epochInventoryBuilder{engine: controller, ctx: ctx, allowed: map[string]uint32{}}
		for _, entry := range entries {
			record, err := controller.read(entry.Name())
			if err != nil {
				return err
			}
			if err := epochClosedRecord(record); err != nil {
				return err
			}
			records[record.ID] = record
			captures, err := controller.exitEntries("objects/" + record.ID + "/captures")
			if err != nil && !(errors.Is(err, os.ErrNotExist) && record.Phase == "deleted") {
				return err
			}
			for _, capture := range captures {
				relative, err := checkpointPath(record.ID, capture.Name())
				if err != nil {
					return err
				}
				if internal, err := controller.exitCompletedTimerGuard(record, capture.Name(), relative); err != nil {
					return err
				} else if internal {
					continue
				}
				var saved Checkpoint
				if err := controller.readCheckpointJSON(relative+"/checkpoint.json", &saved); err != nil {
					return fmt.Errorf("incomplete dependent capture blocks epoch retirement: %w", err)
				}
				if err := epochSnapshotReferences(saved.Filesystem, request.SourceRoot); err != nil {
					return err
				}
				if _, err := controller.root.Lstat(relative + "/retirement-intent.json"); errors.Is(err, os.ErrNotExist) {
					if _, err := controller.publishedCheckpointMetadata(ctx, record, capture.Name(), relative); err != nil {
						return err
					}
				} else if err != nil {
					return err
				} else {
					var retirement checkpointRetirementIntent
					var complete CheckpointRetirement
					if err := controller.readCheckpointJSON(relative+"/retirement-intent.json", &retirement); err != nil {
						return err
					}
					if err := controller.readCheckpointJSON(relative+"/retirement-complete.json", &complete); err != nil {
						return err
					}
					identity, _ := json.Marshal(retirement)
					checkpoint, _ := json.Marshal(saved)
					if complete.Version != 1 || complete.CompletedAt == "" || complete.IntentSHA256 != sum(identity) || complete.Request != retirement.Request || retirement.Request.CheckpointSHA256 != sum(checkpoint) {
						return fmt.Errorf("dependent checkpoint retirement is incomplete")
					}
					if err := controller.retirementAbsent(retirement); err != nil {
						return err
					}
				}
			}
			if err := metadata.resumes(record); err != nil {
				return err
			}
			if record.Phase == "deleted" {
				continue
			}
			if controller == e {
				if err := controller.exitPausedRuntime(ctx, forest.locks[0], record); err != nil {
					return err
				}
			}
			config, err := controller.root.ReadFile("objects/" + record.ID + "/config.json")
			if err != nil {
				return err
			}
			if err := epochRecordReferences(record, config, request.SourceRoot); err != nil {
				return err
			}
		}
		bases, err := controller.exitEntries("bases")
		if err != nil {
			return err
		}
		for _, base := range bases {
			if _, err := controller.readBase(base.Name()); err != nil {
				return fmt.Errorf("incomplete dependent base blocks retirement: %w", err)
			}
		}
		forks, err := controller.exitEntries("forks")
		if err != nil {
			return err
		}
		for _, entry := range forks {
			record, found := records[entry.Name()]
			if !found || record.Fork == nil {
				if controller.cancelledRecoveryImport(ctx, entry.Name()) {
					continue
				}
				return fmt.Errorf("incomplete descendant fork/import blocks epoch retirement")
			}
			intent, err := controller.readForkIntent("forks/"+record.ID, record.Fork.SourceID, record.Fork.CheckpointID, record.ID)
			if err != nil {
				return err
			}
			var prepared forkPreparation
			if err := controller.readCheckpointJSON("forks/"+record.ID+"/preparation.json", &prepared); err != nil {
				return err
			}
			data, _ := json.Marshal(intent)
			if prepared.Version != 1 || prepared.Owner != record.Owner || prepared.IntentSHA256 != sum(data) || prepared.PreparedAt == "" {
				return fmt.Errorf("descendant private preparation is incomplete")
			}
			if record.Phase == "deleted" {
				continue
			}
			if err := epochSnapshotReferences(prepared.Filesystem, request.SourceRoot); err != nil {
				return err
			}
			if intent.Recovery != nil {
				if err := epochSnapshotReferences(intent.Checkpoint.Filesystem, request.SourceRoot); err != nil {
					return err
				}
			}
			actual, err := controller.checkpointFiles(ctx, "forks/"+record.ID+"/materialized")
			if err != nil || !checkpointPreparationIntact(prepared.Files, actual) {
				return fmt.Errorf("descendant private RAM images changed: %v", err)
			}
		}
	}
	return e.epochMountDependencies(request.SourceRoot)
}

func (e *Engine) epochRetirementMetadata(ctx context.Context, source *Engine, prepared epochRetirementPreparation) error {
	directory, err := directoryIdentity(source.config.Root + "/" + epochRetirementDirectory)
	if err != nil || directory.Inode != prepared.Directory.Inode || directory.Device != e.config.BoundedStore.BlockDevice ||
		directory.MountID != e.config.BoundedStore.Mount.MountID || prepared.Directory.Device != prepared.Attachment.BlockDevice ||
		prepared.Directory.MountID != prepared.Attachment.Mount.MountID {
		return fmt.Errorf("epoch retirement directory changed: %v", err)
	}
	plan, err := ownedtree.Capture(ctx, source.root, []string{epochRetirementDirectory})
	if err != nil {
		return err
	}
	allowed := map[string]uint32{epochRetirementDirectory: unix.S_IFDIR | 0700, epochRetirementDirectory + "/witness": unix.S_IFDIR | 0700}
	for _, name := range []string{"prepare.json", "intent.json", "complete.json", "retirement-payload.json.gz", ".retirement-reserve", "witness/retirement-payload.json.gz"} {
		allowed[epochRetirementDirectory+"/"+name] = unix.S_IFREG | 0600
	}
	for _, entry := range plan.Entries {
		mode, found := allowed[entry.Path]
		if !found || mode != entry.Object.Mode || entry.Object.UID != 0 || entry.Object.GID != 0 {
			return fmt.Errorf("unknown or foreign retirement coordination metadata: %s", entry.Path)
		}
	}
	return nil
}

func epochRetirementCompletionBound(prepared epochRetirementPreparation) error {
	stamp := "9999-12-31T23:59:59.999999999Z"
	report := EpochRetirement{Version: 1, Request: prepared.Request, AcceptanceSHA256: strings.Repeat("f", 64), CompletedAt: stamp, AcceptedAt: stamp, Before: prepared.Before, After: prepared.Before}
	value := epochRetirementCompletion{Version: 1, SourceRoot: prepared.Request.SourceRoot, PreparationSHA256: strings.Repeat("f", 64), AcceptanceSHA256: strings.Repeat("f", 64), CompletedAt: stamp, Report: &report}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var widened any
	if err := decoder.Decode(&widened); err != nil {
		return err
	}
	var expand func(any) any
	expand = func(value any) any {
		switch v := value.(type) {
		case json.Number:
			return json.Number("18446744073709551615")
		case bool:
			return false
		case map[string]any:
			for key, item := range v {
				if strings.HasSuffix(key, "_at") {
					if _, ok := item.(string); ok {
						v[key] = stamp
						continue
					}
				}
				v[key] = expand(item)
			}
			return v
		case []any:
			for i, item := range v {
				v[i] = expand(item)
			}
			return v
		default:
			return value
		}
	}
	maximum, err := checkpointRecordBytes(expand(widened))
	if err != nil {
		return err
	}
	// The general record limit is larger than the actual allocated reserve.
	// Retain half the reserve for publication and filesystem metadata even if
	// unlinking the authorized payload has not yet released any data blocks.
	if len(maximum) > retirementReserveBytes/2 {
		return fmt.Errorf("epoch retirement completion reserve cannot hold the bounded report")
	}
	return nil
}

func mountUnescape(value string) (string, error) {
	var out strings.Builder
	for i := 0; i < len(value); i++ {
		if value[i] != '\\' {
			out.WriteByte(value[i])
			continue
		}
		if i+3 >= len(value) {
			return "", fmt.Errorf("incomplete mountinfo escape")
		}
		n, err := strconv.ParseUint(value[i+1:i+4], 8, 8)
		if err != nil {
			return "", err
		}
		out.WriteByte(byte(n))
		i += 3
	}
	return out.String(), nil
}

func (e *Engine) epochMountDependencies(source string) error {
	file, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 4<<20)
	device := fmt.Sprintf("%d:%d", unix.Major(e.config.BoundedStore.BlockDevice), unix.Minor(e.config.BoundedStore.BlockDevice))
	relative := "/" + strings.TrimPrefix(source, e.config.BoundedStore.Target+"/")
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		dash := -1
		for i, field := range fields {
			if field == "-" {
				dash = i
				break
			}
		}
		if len(fields) < 10 || dash < 6 || dash+3 >= len(fields) {
			return fmt.Errorf("unrecognized current mount inventory")
		}
		root, err := mountUnescape(fields[3])
		if err != nil {
			return err
		}
		point, err := mountUnescape(fields[4])
		if err != nil {
			return err
		}
		if usesEpoch(point, source) || fields[2] == device && below(root, relative) {
			return fmt.Errorf("current mount still references retiring epoch")
		}
		if fields[dash+1] != "overlay" {
			continue
		}
		for _, option := range strings.Split(fields[dash+3], ",") {
			key, value, found := strings.Cut(option, "=")
			if !found || key != "lowerdir" && key != "upperdir" && key != "workdir" {
				continue
			}
			for _, part := range strings.Split(value, ":") {
				name, err := mountUnescape(part)
				if err != nil {
					return err
				}
				if usesEpoch(name, source) {
					return fmt.Errorf("current overlay still references retiring epoch")
				}
			}
		}
	}
	return scanner.Err()
}

func (e *Engine) epochWitness(ctx context.Context, source *Engine, prepared epochRetirementPreparation, complete bool) (ownedtree.Plan, error) {
	witness, err := source.readRetirementInventory(epochRetirementDirectory+"/witness", prepared.WitnessSHA256)
	if err != nil {
		return witness, err
	}
	if err := e.epochRetirementMetadata(ctx, source, prepared); err != nil {
		return witness, err
	}
	// The saved plan covers each original top-level name. It cannot discover
	// a later sibling outside those paths, so check the directory itself too.
	allowed := map[string]bool{epochRetirementDirectory: true}
	for _, name := range witness.Paths {
		allowed[name] = true
	}
	entries, err := source.exitEntries(".")
	if err != nil {
		return witness, err
	}
	for _, entry := range entries {
		if !allowed[entry.Name()] {
			return witness, fmt.Errorf("unknown retiring epoch root entry: %s", entry.Name())
		}
	}
	witness, err = rebindEpochRetirementPlan(witness, prepared.Attachment, *e.config.BoundedStore)
	if err != nil {
		return witness, err
	}
	if complete {
		return witness, ownedtree.ValidateComplete(ctx, source.root, witness)
	}
	if err := ownedtree.ValidateRemaining(ctx, source.root, witness); err != nil {
		return witness, err
	}
	payload, err := source.readRetirementInventory(epochRetirementDirectory, prepared.InventorySHA256)
	if err != nil {
		return witness, err
	}
	for _, entry := range witness.Entries {
		inside := false
		for _, root := range payload.Paths {
			inside = inside || below(entry.Path, root)
		}
		if !inside {
			if _, err := source.root.Lstat(entry.Path); err != nil {
				return witness, fmt.Errorf("retirement metadata disappeared: %s: %w", entry.Path, err)
			}
		}
	}
	return witness, nil
}

func (e *Engine) completedEpochRetirement(ctx context.Context, source *Engine) (EpochRetirement, error) {
	var complete epochRetirementCompletion
	if err := source.readCheckpointJSON(epochRetirementDirectory+"/complete.json", &complete); err != nil {
		return EpochRetirement{}, err
	}
	var accepted epochRetirementAcceptance
	if err := source.readCheckpointJSON(epochRetirementDirectory+"/intent.json", &accepted); err != nil {
		return EpochRetirement{}, err
	}
	prepared, err := e.epochRetirementPreparation(source, accepted.Fence.PreparationSHA256)
	if err != nil {
		return EpochRetirement{}, err
	}
	data, _ := json.Marshal(accepted)
	if complete.Version != 1 || complete.SourceRoot != source.config.Root || complete.AcceptanceSHA256 != sum(data) || complete.PreparationSHA256 != accepted.Fence.PreparationSHA256 || complete.CompletedAt == "" || complete.Report == nil {
		return EpochRetirement{}, fmt.Errorf("epoch retirement lacks a complete public protocol result")
	}
	report := *complete.Report
	if report.Version != 1 || !reflect.DeepEqual(report.Request, prepared.Request) || report.AcceptanceSHA256 != complete.AcceptanceSHA256 || report.CompletedAt != complete.CompletedAt || report.AcceptedAt != accepted.AcceptedAt || !reflect.DeepEqual(report.Before, prepared.Before) {
		return report, fmt.Errorf("epoch retirement report differs from its accepted request")
	}
	if _, err := e.epochWitness(ctx, source, prepared, false); err != nil {
		return report, err
	}
	payload, err := source.readRetirementInventory(epochRetirementDirectory, prepared.InventorySHA256)
	if err != nil {
		return report, err
	}
	for _, path := range payload.Paths {
		if _, err := source.root.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			return report, fmt.Errorf("retired epoch has payload or a foreign successor: %s: %v", path, err)
		}
	}
	return report, nil
}

// RetireEpoch requires a current validated bounded worker, an old exclusively
// owned epoch and an independently committed successor for every old pause.
// All store controllers remain locked throughout admission and mutation.
func (e *Engine) RetireEpoch(ctx context.Context, request epochRetirementRequest) (EpochRetirement, error) {
	return e.retireEpoch(ctx, request, false)
}

// BeginEpochRetirement performs the complete admission and durably accepts
// maintenance without removing payloads. RetireEpoch completes that same exact
// request. An accepted store remains fenced across a worker loss.
func (e *Engine) BeginEpochRetirement(ctx context.Context, request epochRetirementRequest) (EpochRetirement, error) {
	return e.retireEpoch(ctx, request, true)
}

func (e *Engine) retireEpoch(ctx context.Context, request epochRetirementRequest, beginOnly bool) (EpochRetirement, error) {
	var report EpochRetirement
	if err := ctx.Err(); err != nil {
		return report, err
	}
	request, err := e.normalizeEpochRetirement(request)
	if err != nil {
		return report, err
	}
	forest, err := e.retirementForest()
	if err != nil {
		return report, err
	}
	defer forest.close()
	source := forest.controllers[request.SourceRoot]
	if source == nil {
		return report, fmt.Errorf("requested source epoch is missing")
	}
	if _, err := source.root.Lstat(epochRetirementDirectory + "/complete.json"); err == nil {
		completed, err := e.completedEpochRetirement(ctx, source)
		if err != nil {
			return report, err
		}
		if !reflect.DeepEqual(completed.Request, request) {
			return report, fmt.Errorf("retired epoch belongs to another request")
		}
		var accepted epochRetirementAcceptance
		if err := source.readCheckpointJSON(epochRetirementDirectory+"/intent.json", &accepted); err != nil {
			return report, err
		}
		return completed, e.finishEpochRetirementFence(ctx, source, accepted)
	} else if !errors.Is(err, os.ErrNotExist) {
		return report, err
	}
	if err := e.epochDependencies(ctx, forest, request); err != nil {
		return report, err
	}
	var prepared epochRetirementPreparation
	err = source.readCheckpointJSON(epochRetirementDirectory+"/prepare.json", &prepared)
	if errors.Is(err, os.ErrNotExist) {
		if err := e.epochRetirementReadFence(); err != nil {
			return report, err
		}
		inventory, err := source.inventoryEpochPayloads(ctx)
		if err != nil {
			return report, err
		}
		if err := e.verifyEpochHandovers(ctx, forest, request, inventory.Records); err != nil {
			return report, err
		}
		before, err := e.storageCapacity()
		if err != nil {
			return report, err
		}
		if err := source.root.Mkdir(epochRetirementDirectory, 0700); err != nil {
			return report, fmt.Errorf("retirement preparation is partial or already owned: %w", err)
		}
		if err := source.root.Mkdir(epochRetirementDirectory+"/witness", 0700); err != nil {
			return report, err
		}
		directory, err := directoryIdentity(source.config.Root + "/" + epochRetirementDirectory)
		if err != nil {
			return report, err
		}
		payloadSHA, err := source.writeRetirementInventory(epochRetirementDirectory, inventory.Payload)
		if err != nil {
			return report, err
		}
		witnessSHA, err := source.writeRetirementInventory(epochRetirementDirectory+"/witness", inventory.Witness)
		if err != nil {
			return report, err
		}
		reserve, err := source.prepareRetirementReserve(epochRetirementDirectory)
		if err != nil {
			return report, err
		}
		prepared = epochRetirementPreparation{Version: 1, Request: request, BootID: e.boot, Image: e.config.BoundedStore.Image, Attachment: *e.config.BoundedStore,
			InventorySHA256: payloadSHA, WitnessSHA256: witnessSHA, Directory: directory, Reserve: reserve, Before: before, PreparedAt: now()}
		if err := source.publishCheckpoint(epochRetirementDirectory, "prepare.json", prepared); err != nil {
			return report, err
		}
		parent, err := source.root.Open(".")
		if err != nil {
			return report, err
		}
		syncErr := parent.Sync()
		closeErr := parent.Close()
		if err := errors.Join(syncErr, closeErr); err != nil {
			return report, err
		}
	} else if err != nil {
		return report, err
	}
	if !reflect.DeepEqual(prepared.Request, request) || !digest.MatchString(prepared.WitnessSHA256) {
		return report, fmt.Errorf("epoch preparation differs from the exact request or lacks its metadata witness")
	}
	if err := e.epochRetirementMetadata(ctx, source, prepared); err != nil {
		return report, err
	}
	if err := epochRetirementCompletionBound(prepared); err != nil {
		return report, err
	}
	acceptedAlready := false
	if _, err := source.root.Lstat(epochRetirementDirectory + "/intent.json"); err == nil {
		acceptedAlready = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return report, err
	}
	if _, err := e.epochWitness(ctx, source, prepared, !acceptedAlready); err != nil {
		return report, err
	}
	records := map[string]Record{}
	entries, err := source.exitEntries("objects")
	if err != nil {
		return report, err
	}
	for _, entry := range entries {
		record, err := source.read(entry.Name())
		if err != nil {
			return report, err
		}
		records[record.ID] = record
	}
	if err := e.verifyEpochHandovers(ctx, forest, request, records); err != nil {
		return report, err
	}
	data, _ := json.Marshal(prepared)
	preparationSHA := sum(data)
	accepted, err := e.acceptEpochRetirement(ctx, source, preparationSHA)
	if err != nil {
		return report, err
	}
	if beginOnly {
		data, _ := json.Marshal(accepted)
		return EpochRetirement{Version: 1, Request: request, AcceptanceSHA256: sum(data), AcceptedAt: accepted.AcceptedAt, Before: prepared.Before}, nil
	}
	if _, err := e.epochWitness(ctx, source, prepared, false); err != nil {
		return report, err
	}
	plan, err := source.readRetirementInventory(epochRetirementDirectory, prepared.InventorySHA256)
	if err != nil {
		return report, err
	}
	plan, err = rebindEpochRetirementPlan(plan, prepared.Attachment, *e.config.BoundedStore)
	if err != nil {
		return report, err
	}
	if err := ownedtree.RemoveRemaining(ctx, source.root, plan); err != nil {
		return report, err
	}
	reserve := prepared.Reserve
	if reserve.Device != prepared.Attachment.BlockDevice {
		return report, fmt.Errorf("epoch retirement reserve has a foreign original device")
	}
	reserve.Device = e.config.BoundedStore.BlockDevice
	if err := source.releaseRetirementReserve(epochRetirementDirectory, reserve); err != nil {
		return report, err
	}
	after, err := e.storageCapacity()
	if err != nil {
		return report, err
	}
	data, _ = json.Marshal(accepted)
	report = EpochRetirement{Version: 1, Request: request, AcceptanceSHA256: sum(data), CompletedAt: now(), AcceptedAt: accepted.AcceptedAt, Before: prepared.Before, After: after}
	complete := epochRetirementCompletion{Version: 1, SourceRoot: source.config.Root, PreparationSHA256: preparationSHA, AcceptanceSHA256: report.AcceptanceSHA256, CompletedAt: report.CompletedAt, Report: &report}
	if err := source.publishCheckpoint(epochRetirementDirectory, "complete.json", complete); err != nil {
		return report, err
	}
	return report, e.finishEpochRetirementFence(ctx, source, accepted)
}
