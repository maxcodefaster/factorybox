//go:build linux

package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"factorybox/internal/ownedtree"
	"factorybox/internal/storefs"
	"factorybox/internal/sysboxcapture"
	"golang.org/x/sys/unix"
)

type epochPayloadInventory struct {
	Payload ownedtree.Plan
	Witness ownedtree.Plan
	Records map[string]Record
}

type epochInventoryBuilder struct {
	engine   *Engine
	ctx      context.Context
	previous storefs.Active
	allowed  map[string]uint32
	payloads []string
	records  map[string]Record
	bases    map[string]sysboxcapture.FilesystemTree
}

func below(name, root string) bool { return name == root || strings.HasPrefix(name, root+"/") }

func (b *epochInventoryBuilder) keep(name string, mode uint32) {
	b.allowed[name] = mode
	for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
		b.allowed[parent] = unix.S_IFDIR | 0700
	}
}

func (b *epochInventoryBuilder) file(name string) { b.keep(name, unix.S_IFREG|0600) }

func (b *epochInventoryBuilder) payload(absolute string) error {
	root := b.engine.config.Root
	if !strings.HasPrefix(absolute, root+"/") || filepath.Clean(absolute) != absolute {
		return fmt.Errorf("epoch payload escapes its exact controller: %s", absolute)
	}
	relative := strings.TrimPrefix(absolute, root+"/")
	for _, previous := range b.payloads {
		if below(relative, previous) || below(previous, relative) {
			return fmt.Errorf("epoch payload roots overlap: %s", relative)
		}
	}
	b.payloads = append(b.payloads, relative)
	for parent := path.Dir(relative); parent != "."; parent = path.Dir(parent) {
		b.allowed[parent] = unix.S_IFDIR | 0700
	}
	return nil
}

func (b *epochInventoryBuilder) translate(tree sysboxcapture.FilesystemTree, device uint64) error {
	if tree.Immutable == nil {
		return fmt.Errorf("epoch translation has no canonical base identity")
	}
	current := b.engine.config.BoundedStore
	base, found := b.bases[tree.Immutable.Root]
	if !found || !reflect.DeepEqual(base.Immutable, tree.Immutable) || tree.Immutable.Device != b.previous.BlockDevice ||
		device != current.BlockDevice || !below(tree.Immutable.Root, b.engine.config.Root) {
		return fmt.Errorf("epoch tree differs from its independently registered retained base")
	}
	if err := storefs.Verify(*current); err != nil {
		return err
	}
	return b.engine.boundedObject(tree.Immutable.Root)
}

func (b *epochInventoryBuilder) snapshot(generation string, snapshot *sysboxcapture.FilesystemSnapshot) error {
	if err := sysboxcapture.VerifyRetainedFilesystemSnapshot(b.ctx, b.engine.config.Root+"/"+generation, snapshot, b.translate); err != nil {
		return err
	}
	for _, tree := range snapshot.Roots {
		if err := b.payload(tree.Saved); err != nil {
			return err
		}
	}
	return nil
}

func epochClosedRecord(record Record) error {
	if record.Start.RequestedAt == "" || !record.Start.Attempted || record.Start.CompletedAt == "" || record.Start.Error != "" {
		return fmt.Errorf("epoch sandbox start is incomplete: %s", record.ID)
	}
	if record.Phase == "paused" {
		return committedExitPause(record)
	}
	if record.Phase != "deleted" || record.Delete == nil || !record.Delete.Attempted || record.Delete.CompletedAt == "" || record.Delete.Error != "" {
		return fmt.Errorf("epoch sandbox is neither committed paused nor confirmed deleted: %s", record.ID)
	}
	return nil
}

func (b *epochInventoryBuilder) runtime(relative string, record Record, required bool) error {
	entries, err := b.engine.exitEntries(relative)
	if errors.Is(err, os.ErrNotExist) && !required {
		return nil
	}
	if err != nil {
		return err
	}
	b.keep(relative, unix.S_IFDIR|0700)
	for _, entry := range entries {
		if entry.Name() != record.RuntimeID || !entry.IsDir() {
			if relative == "runtime" && entry.Name() != record.RuntimeID {
				continue
			}
			return fmt.Errorf("epoch contains an unknown runtime identity")
		}
		parent := relative + "/" + entry.Name()
		children, err := b.engine.exitEntries(parent)
		if err != nil {
			return err
		}
		for _, child := range children {
			name := parent + "/" + child.Name()
			if child.Name() == "checkpoint" && relative != "runtime" {
				info, err := b.engine.root.Lstat(name)
				if err != nil || info.Size() != 0 {
					return fmt.Errorf("capture runtime checkpoint marker is not empty: %v", err)
				}
				b.keep(name, unix.S_IFREG|0644)
				continue
			}
			if child.Name() != "state.json" {
				return fmt.Errorf("epoch contains unknown runtime metadata: %s", name)
			}
			var state struct {
				ID     string `json:"id"`
				Config struct {
					Rootfs string `json:"rootfs"`
				} `json:"config"`
			}
			var fields map[string]json.RawMessage
			if err := b.engine.readCheckpointJSON(name, &fields); err != nil {
				return err
			}
			data, _ := json.Marshal(fields)
			if err := json.Unmarshal(data, &state); err != nil || state.ID != record.RuntimeID || state.Config.Rootfs != record.Rootfs {
				return fmt.Errorf("retained runtime metadata differs from its owned sandbox: %s", name)
			}
			b.file(name)
		}
		mode := uint32(unix.S_IFDIR | 0700)
		if relative == "runtime" {
			mode = unix.S_IFDIR | 0711
		}
		b.allowed[parent] = mode
	}
	return nil
}

func epochRetainedLogNames(arm retainedCaptureArm) (map[string]bool, error) {
	if arm.Version != 3 || arm.Mode != "process_tree_retained" || len(arm.Tree) == 0 || len(arm.Tree) > 512 {
		return nil, fmt.Errorf("retained log inventory lacks its complete bounded original tree")
	}
	tree := map[int]retainedCaptureThread{}
	previous := 1
	for _, thread := range arm.Tree {
		if thread.PID <= previous || thread.Process <= 1 || thread.Start == "" {
			return nil, fmt.Errorf("retained log inventory has incomplete or unordered original threads")
		}
		tree[thread.PID], previous = thread, thread.PID
	}
	if tree[arm.SourcePID].Process != arm.SourcePID || tree[arm.SourcePID].Start != arm.SourceStart || arm.SourceStart == "" ||
		tree[arm.ActivePID].Process != arm.ActivePID || tree[arm.ActivePID].Start != arm.ActiveStart || arm.ActiveStart == "" || arm.ActiveVirtualPID <= 0 {
		return nil, fmt.Errorf("retained log inventory lost its original source or active process")
	}
	logs := map[string]bool{}
	for _, thread := range arm.Tree {
		if tree[thread.Process].Process != thread.Process {
			return nil, fmt.Errorf("retained log inventory lacks an original process leader")
		}
		// The pinned CRIU producer opens one log for each serially active
		// process. Its final ARM retains the complete original tree, even when
		// a different process was active last. Thread IDs grant no log name.
		if thread.PID == thread.Process {
			logs["criu-retained-"+strconv.Itoa(thread.PID)+".log"] = true
		}
	}
	return logs, nil
}

func (b *epochInventoryBuilder) guard(relative string, record Record) error {
	entries, err := b.engine.exitEntries(relative)
	if err != nil {
		return err
	}
	known := strings.Fields(`.rollback-reserve capture-abort-intent.json capture-network-rollback.json
capture-process-complete.json capture-process-restored.json capture-stop-intent.json capture-worker-intent.json
capture-worker-outcome.json criu-retained-arm.json criu-retained-request.json criu-retained-result.json
guard-armed.json guard-capture-aborted.json guard-capture-cured.json guard-capture-entered.json
guard-capture-rejected.json guard-capture-returned.json guard-capture-stopped.json guard-controller-outcome.json
guard-delegated-recovery.json guard-intent.json guard-orphan-recovery.json guard-outcome.json
guard-timer-activation-watched.json guard-timer-watched.json rollback-reserve.json timer-state-plan.json
timer-activation-complete.json timer-activation-intent.json timer-activation-mutator-ready.json
timer-activation-worker-intent.json timer-mutator-ready.json timer-prepare-complete.json timer-prepare-intent.json`)
	allowed := map[string]bool{}
	for _, name := range known {
		allowed[name] = true
	}
	var arm retainedCaptureArm
	if err := b.engine.readCheckpointJSON(relative+"/criu-retained-arm.json", &arm); err == nil {
		var config captureGuardConfig
		if err := b.engine.readCheckpointJSON(relative+"/guard-intent.json", &config); err != nil {
			return err
		}
		if arm.Version != 3 || arm.Mode != "process_tree_retained" || arm.SourcePID <= 1 || arm.SourcePID != config.PID || arm.SourceStart != config.Start ||
			config.Version != 1 || config.Root != b.engine.config.Root || config.Evidence != b.engine.config.Root+"/"+relative || config.Token != path.Base(path.Dir(relative)) ||
			config.Source.ID != record.ID || config.Source.Owner != record.Owner || config.Source.RuntimeID != record.RuntimeID || config.Source.Rootfs != record.Rootfs {
			return fmt.Errorf("retained capture log has no exact original arm identity")
		}
		logs, err := epochRetainedLogNames(arm)
		if err != nil {
			return err
		}
		for name := range logs {
			allowed[name] = true
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, prefix := range []string{"timer-", "timer-activation-"} {
		for _, name := range []string{"exit-hold-unconfirmed.json", "mutator-exit-held.json", "mutator-watch-error.json"} {
			allowed[prefix+name] = true
		}
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !allowed[entry.Name()] {
			return fmt.Errorf("unknown epoch capture guard entry: %s/%s", relative, entry.Name())
		}
		b.file(relative + "/" + entry.Name())
	}
	b.keep(relative, unix.S_IFDIR|0700)
	return nil
}

func (b *epochInventoryBuilder) retired(record Record, relative string) (bool, error) {
	var intent checkpointRetirementIntent
	if err := b.engine.readCheckpointJSON(relative+"/retirement-intent.json", &intent); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	directory, err := directoryIdentity(b.engine.config.Root + "/" + relative)
	if err != nil || intent.Version != 1 || intent.Directory.Inode != directory.Inode ||
		intent.Directory.Device != b.previous.BlockDevice || intent.Directory.MountID != b.previous.Mount.MountID {
		return false, fmt.Errorf("retired epoch checkpoint original directory identity differs: %v", err)
	}
	_, _, expected, err := b.engine.checkpointRequest(intent.Request)
	if err != nil || expected != relative || intent.Request.SourceOwner != record.Owner {
		return false, fmt.Errorf("retired epoch checkpoint request differs: %v", err)
	}
	data, _ := json.Marshal(intent)
	var complete CheckpointRetirement
	if err := b.engine.readCheckpointJSON(relative+"/retirement-complete.json", &complete); err != nil {
		return false, err
	}
	if complete.Version != 1 || complete.Request != intent.Request || complete.IntentSHA256 != sum(data) || complete.CompletedAt == "" || !reflect.DeepEqual(complete.Before, intent.Before) {
		return false, fmt.Errorf("epoch checkpoint retirement lacks its exact completion")
	}
	if err := b.engine.retirementAbsent(intent); err != nil {
		return false, err
	}
	for _, name := range []string{"retirement-intent.json", "retirement-complete.json", "retirement-payload.json.gz", ".retirement-reserve"} {
		b.file(relative + "/" + name)
	}
	return true, nil
}

func (b *epochInventoryBuilder) captures(record Record) error {
	parent := "objects/" + record.ID + "/captures"
	entries, err := b.engine.exitEntries(parent)
	if errors.Is(err, os.ErrNotExist) && record.Phase == "deleted" {
		return nil
	}
	if err != nil {
		return err
	}
	b.keep(parent, unix.S_IFDIR|0700)
	pauseFound := record.Phase != "paused"
	for _, entry := range entries {
		relative, err := checkpointPath(record.ID, entry.Name())
		if err != nil || !entry.IsDir() {
			return fmt.Errorf("unknown epoch capture generation: %v", err)
		}
		retired, err := b.retired(record, relative)
		if err != nil {
			return err
		}
		internal := false
		if !retired {
			internal, err = b.engine.exitCompletedTimerGuard(record, entry.Name(), relative)
			if err != nil {
				return err
			}
		}
		if internal {
			if err := b.guard(relative+"/guard", record); err != nil {
				return err
			}
			continue
		}
		var saved Checkpoint
		if retired {
			if err := b.engine.readCheckpointJSON(relative+"/checkpoint.json", &saved); err != nil {
				return err
			}
		} else {
			var err error
			saved, err = b.engine.publishedCheckpointMetadata(b.ctx, record, entry.Name(), relative)
			if err != nil {
				return fmt.Errorf("unfinished epoch capture %s: %w", relative, err)
			}
			request := CheckpointRequest{record.ID, saved.ID, record.Owner, ""}
			data, _ := json.Marshal(saved)
			request.CheckpointSHA256 = sum(data)
			if err := b.engine.checkpointSelections(relative, request); err != nil {
				return err
			}
			if err := b.snapshot(relative, saved.Filesystem); err != nil {
				return err
			}
			if err := b.payload(b.engine.config.Root + "/" + relative + "/images"); err != nil {
				return err
			}
		}
		b.keep(relative+"/filesystem", unix.S_IFDIR|0700)
		b.keep(relative+"/filesystem/volumes", unix.S_IFDIR|0700)
		request := CheckpointRequest{record.ID, saved.ID, record.Owner, ""}
		identity, _ := json.Marshal(saved)
		request.CheckpointSHA256 = sum(identity)
		if err := b.engine.checkpointSelections(relative, request); err != nil {
			return err
		}
		if record.Phase == "paused" && entry.Name() == record.Pause.SnapshotID {
			data, _ := json.Marshal(saved)
			if retired || !saved.SourceStopped || saved.SourcePID != record.Pause.SourcePID || sum(data) != record.Pause.CheckpointSHA256 {
				return fmt.Errorf("epoch pause differs from its protected complete checkpoint")
			}
			pauseFound = true
		}
		for _, name := range []string{"checkpoint.json", "capture-result.json", "checkpoint-intent.json", "filesystem.json", "source-state.json", "sysbox-state.json", "criu.conf", "network-before.json", "sockets-before.json", "checkpoint-reconciled.json"} {
			b.file(relative + "/" + name)
		}
		children, err := b.engine.exitEntries(relative)
		if err != nil {
			return err
		}
		for _, child := range children {
			if strings.HasPrefix(child.Name(), "selection-") {
				b.file(relative + "/" + child.Name())
			}
		}
		if err := b.guard(relative+"/guard", record); err != nil {
			return err
		}
		if err := b.runtime(relative+"/runtime", record, false); err != nil {
			return err
		}
	}
	if !pauseFound {
		return fmt.Errorf("epoch has no complete published pause")
	}
	return nil
}

func epochRestoreReceipt(engine *Engine, relative string, record Record, request any, completed string) error {
	var result, held restoreResult
	if err := engine.readCheckpointJSON(relative+"-result.json", &result); err != nil {
		return err
	}
	if err := engine.readCheckpointJSON(relative+"-held.json", &held); err != nil {
		return err
	}
	data, _ := json.Marshal(request)
	if result.Version != 1 || !reflect.DeepEqual(held, result) || result.Owner != record.Owner || result.RuntimeID != record.RuntimeID ||
		result.RequestSHA256 != sum(data) || result.CompletedAt != completed || result.PID <= 1 || result.ProcessStart == "" || result.Created == "" {
		return fmt.Errorf("epoch restore lacks its exact completed request: %s", relative)
	}
	return nil
}

func (b *epochInventoryBuilder) resumes(record Record) error {
	parent := "objects/" + record.ID + "/resumes"
	pauses := map[string]PauseOperation{}
	for _, pause := range record.PauseHistory {
		pauses[pause.SnapshotID] = pause
	}
	if record.Pause != nil {
		pauses[record.Pause.SnapshotID] = *record.Pause
	}
	entries, err := b.engine.exitEntries(parent)
	if errors.Is(err, os.ErrNotExist) {
		for _, pause := range pauses {
			if pause.Resume != nil {
				return fmt.Errorf("epoch resume backing is missing from its original pause")
			}
		}
		return nil
	}
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	b.keep(parent, unix.S_IFDIR|0700)
	for _, entry := range entries {
		pause, found := pauses[entry.Name()]
		if !found || !tokenID.MatchString(entry.Name()) || pause.Resume == nil || !pause.Resume.Attempted || pause.Resume.CompletedAt == "" || pause.Resume.Error != "" {
			return fmt.Errorf("epoch resume is unfinished or has no original pause")
		}
		seen[entry.Name()] = true
		relative := parent + "/" + entry.Name()
		original := record
		original.Pause = &pause
		request := b.engine.resumeRequest(original)
		var intent resumeIntent
		if err := b.engine.readCheckpointJSON(relative+"/resume-intent.json", &intent); err != nil {
			return err
		}
		if intent.Version != 1 || intent.Request != request || intent.SnapshotSHA != pause.CheckpointSHA256 || intent.CriuSHA256 != b.engine.config.CriuSHA256 || intent.RuntimeSHA256 != b.engine.config.RuntimeSHA256 {
			return fmt.Errorf("epoch resume intent differs from its immutable request")
		}
		if err := epochRestoreReceipt(b.engine, relative+"/resume", record, request, pause.Resume.CompletedAt); err != nil {
			return err
		}
		var prepared []CheckpointFile
		if err := b.engine.readCheckpointJSON(relative+"/resume-prepared.json", &prepared); err != nil {
			return err
		}
		actual, err := b.engine.checkpointFiles(b.ctx, relative)
		if err != nil || !checkpointPreparationIntact(prepared, actual) {
			return fmt.Errorf("epoch resume images differ: %v", err)
		}
		if err := b.payload(b.engine.config.Root + "/" + relative + "/images"); err != nil {
			return err
		}
		for _, name := range []string{"resume-intent.json", "resume-prepared.json", "resume-held.json", "resume-result.json", "filesystem.json", "source-state.json", "sysbox-state.json", "criu.conf"} {
			b.file(relative + "/" + name)
		}
	}
	for snapshot, pause := range pauses {
		if pause.Resume != nil && !seen[snapshot] {
			return fmt.Errorf("epoch resume has lost its original private preparation")
		}
	}
	return nil
}

func (b *epochInventoryBuilder) fork(record Record) error {
	operation := "forks/" + record.ID
	intent, err := b.engine.readForkIntent(operation, record.Fork.SourceID, record.Fork.CheckpointID, record.ID)
	if err != nil {
		return err
	}
	if intent.Owner != record.Owner || !reflect.DeepEqual(&intent.Origin, record.Fork) || sum(intent.OCI) != record.InputSHA256 {
		return fmt.Errorf("epoch fork differs from its original complete intent")
	}
	if err := epochRestoreReceipt(b.engine, "objects/"+record.ID+"/restore", record, b.engine.forkRestoreRequest(intent, record), record.Start.CompletedAt); err != nil {
		return err
	}
	var prepared forkPreparation
	if err := b.engine.readCheckpointJSON(operation+"/preparation.json", &prepared); err != nil {
		return err
	}
	data, _ := json.Marshal(intent)
	if prepared.Version != 1 || prepared.Owner != record.Owner || prepared.IntentSHA256 != sum(data) || prepared.PreparedAt == "" || prepared.Filesystem == nil {
		return fmt.Errorf("epoch fork preparation is incomplete")
	}
	materialized := operation + "/materialized"
	config, err := b.engine.root.ReadFile(materialized + "/filesystem/config.json")
	if err != nil || !reflect.DeepEqual(config, intent.OCI) {
		return fmt.Errorf("epoch fork bundle configuration differs from its exact original intent: %v", err)
	}
	b.file(materialized + "/filesystem/config.json")
	actual, err := b.engine.checkpointFiles(b.ctx, materialized)
	if err != nil || !checkpointPreparationIntact(prepared.Files, actual) {
		return fmt.Errorf("epoch fork private images changed: %v", err)
	}
	var filesystem sysboxcapture.FilesystemSnapshot
	if err := b.engine.readCheckpointJSON(materialized+"/filesystem.json", &filesystem); err != nil {
		return err
	}
	if !reflect.DeepEqual(&filesystem, prepared.Filesystem) || len(filesystem.Roots) < 2 || len(filesystem.Roots) > 9 {
		return fmt.Errorf("epoch fork filesystem preparation differs")
	}
	seen := map[string]bool{}
	for _, tree := range filesystem.Roots {
		if tree.Saved != b.engine.config.Root+"/"+materialized+"/filesystem/"+tree.Name || !epochFilesystemName(tree.Name) || seen[tree.Name] {
			return fmt.Errorf("epoch fork filesystem escaped its exact private preparation")
		}
		seen[tree.Name] = true
		if tree.Name == "lower" {
			if err := sysboxcapture.VerifyRetainedFilesystemTree(b.ctx, tree, b.translate); err != nil {
				return err
			}
		}
		if tree.Immutable != nil && tree.Name != "lower" {
			return fmt.Errorf("epoch fork has an unexpected immutable tree")
		}
		if err := b.payload(tree.Saved); err != nil {
			return err
		}
	}
	if !seen["lower"] || !seen["upper"] {
		return fmt.Errorf("epoch fork lacks its complete overlay layer set")
	}
	var origin forkOverlayOrigin
	if err := b.engine.readCheckpointJSON(operation+"/overlay-origin.json", &origin); err != nil {
		return err
	}
	if err := origin.validate(record.Owner, record.Rootfs, record.InputSHA256, intent.Origin.CheckpointSHA256); err != nil {
		return err
	}
	actualRoot, err := directoryIdentity(record.Rootfs)
	if err != nil || actualRoot.Inode != origin.Mountpoint.Inode || origin.Mountpoint.Device != b.previous.BlockDevice || actualRoot.Device != b.engine.config.BoundedStore.BlockDevice || actualRoot.MountID != b.engine.config.BoundedStore.Mount.MountID {
		return fmt.Errorf("epoch fork overlay has a foreign or changed uncovered mountpoint: %v", err)
	}
	for _, name := range []string{"rootfs", "work", "restore-work"} {
		if err := privateDirectory(record.InputBundle + "/" + name); err != nil {
			return err
		}
		if err := b.payload(record.InputBundle + "/" + name); err != nil {
			return err
		}
	}
	pauses := append([]PauseOperation(nil), record.PauseHistory...)
	if record.Pause != nil {
		pauses = append(pauses, *record.Pause)
	}
	for _, pause := range pauses {
		if pause.Resume == nil {
			continue
		}
		// The pinned FactoryboxResumeRoot creates exactly this private
		// overlay workdir for the already verified completed resume token.
		work := record.InputBundle + "/resume-work-" + pause.SnapshotID
		if err := privateDirectory(work); err != nil {
			return err
		}
		if err := b.payload(work); err != nil {
			return err
		}
	}
	if err := b.payload(b.engine.config.Root + "/" + materialized + "/images"); err != nil {
		return err
	}
	for _, name := range []string{"intent.json", "preparation.json", "overlay-origin.json"} {
		b.file(operation + "/" + name)
	}
	for _, name := range []string{"filesystem.json", "source-state.json", "sysbox-state.json", "criu.conf"} {
		b.file(materialized + "/" + name)
	}
	if intent.Recovery != nil {
		prepared, err := b.engine.verifyCompletedRecoveryImport(operation, intent)
		if err != nil {
			return err
		}
		if prepared {
			b.file(operation + "/import.json")
		}
		generation := forkInputGeneration(intent)
		if err := b.engine.verifyCheckpointMetadata(b.ctx, intent.Source, intent.Checkpoint.ID, generation, intent.Checkpoint); err != nil {
			return err
		}
		if err := b.snapshot(generation, intent.Checkpoint.Filesystem); err != nil {
			return err
		}
		if err := b.payload(b.engine.config.Root + "/" + generation + "/images"); err != nil {
			return err
		}
		for _, name := range []string{"checkpoint.json", "capture-result.json", "checkpoint-intent.json", "filesystem.json", "source-state.json", "sysbox-state.json", "criu.conf"} {
			b.file(generation + "/" + name)
		}
		for _, tree := range intent.Checkpoint.Filesystem.Roots {
			if tree.Immutable == nil {
				continue
			}
			if tree.Immutable.Root != b.engine.config.Root+"/"+generation+"/base/rootfs" {
				return fmt.Errorf("epoch recovery canonical base escaped its exact input")
			}
			if err := b.payload(tree.Immutable.Root); err != nil {
				return err
			}
		}
	}
	return nil
}

func epochFilesystemName(name string) bool {
	return name == "lower" || name == "upper" || name == "volumes/docker" || name == "volumes/kubelet" || name == "volumes/k0s" ||
		name == "volumes/rancher-k3s" || name == "volumes/rancher-rke2" || name == "volumes/buildkit" || name == "volumes/containerd"
}

// inventoryEpochPayloads authorizes complete cold epoch backing. It does not
// resolve cross-epoch dependencies or withdraw a committed pause. The caller
// must hold the source lock under the independently verified current store lease.
func (e *Engine) inventoryEpochPayloads(ctx context.Context) (epochPayloadInventory, error) {
	var result epochPayloadInventory
	if e.config.BoundedStore == nil {
		return result, fmt.Errorf("epoch inventory requires a verified bounded attachment")
	}
	parent, err := os.OpenRoot(filepath.Dir(e.config.Root))
	if err != nil {
		return result, err
	}
	epoch, readErr := readRecoveryStoreEpoch(parent)
	if err := parent.Close(); err != nil {
		return result, err
	}
	if readErr != nil {
		return result, readErr
	}
	previous, err := validateRetainedStoreEpoch(e.config, e.boot, epoch)
	if err != nil {
		return result, err
	}
	b := epochInventoryBuilder{engine: e, ctx: ctx, previous: previous, allowed: map[string]uint32{}, records: map[string]Record{}, bases: map[string]sysboxcapture.FilesystemTree{}}
	for _, name := range []string{"objects", "forks", "bases", "runtime"} {
		b.keep(name, unix.S_IFDIR|0700)
	}
	b.file(".lock")
	objects, err := e.exitEntries("objects")
	if err != nil {
		return result, err
	}
	for _, entry := range objects {
		record, err := e.read(entry.Name())
		if err != nil {
			return result, err
		}
		var strict Record
		if err := e.readCheckpointJSON("objects/"+record.ID+"/record.json", &strict); err != nil || !reflect.DeepEqual(strict, record) {
			return result, fmt.Errorf("epoch sandbox record changed or is unrecognized: %v", err)
		}
		if err := epochClosedRecord(record); err != nil {
			return result, err
		}
		b.records[record.ID] = record
		for _, name := range []string{"record.json", "config.json", "restore-held.json", "restore-result.json"} {
			b.file("objects/" + record.ID + "/" + name)
		}
	}
	bases, err := e.exitEntries("bases")
	if err != nil {
		return result, err
	}
	for _, entry := range bases {
		base, err := e.readBase(entry.Name())
		if err != nil {
			return result, err
		}
		b.bases[base.Tree.Immutable.Root] = base.Tree
		b.file("bases/" + base.ID + "/intent.json")
		b.file("bases/" + base.ID + "/base.json")
		if err := b.payload(base.Tree.Saved); err != nil {
			return result, err
		}
	}
	forks, err := e.exitEntries("forks")
	if err != nil {
		return result, err
	}
	for _, entry := range forks {
		record, found := b.records[entry.Name()]
		if !found || record.Fork == nil {
			if e.cancelledRecoveryImport(ctx, entry.Name()) {
				b.file("forks/" + entry.Name() + "/import.json")
				b.file("forks/" + entry.Name() + "/import-cancelled.json")
				continue
			}
			return result, fmt.Errorf("epoch contains an incomplete or unknown fork/import")
		}
		intent, err := e.readForkIntent("forks/"+record.ID, record.Fork.SourceID, record.Fork.CheckpointID, record.ID)
		if err != nil {
			return result, err
		}
		if intent.Recovery != nil {
			if intent.Checkpoint.Filesystem == nil {
				return result, fmt.Errorf("epoch recovery input filesystem is incomplete")
			}
			for _, tree := range intent.Checkpoint.Filesystem.Roots {
				if tree.Immutable != nil {
					expected := e.config.Root + "/" + forkInputGeneration(intent) + "/base/rootfs"
					if tree.Name != "lower" || tree.Immutable.Root != expected {
						return result, fmt.Errorf("unknown recovered epoch base layout")
					}
					b.bases[expected] = tree
				}
			}
		}
	}
	for _, tree := range b.bases {
		if err := sysboxcapture.VerifyRetainedFilesystemBase(ctx, tree, b.translate); err != nil {
			return result, err
		}
	}
	for _, record := range b.records {
		if err := b.captures(record); err != nil {
			return result, err
		}
		if err := b.resumes(record); err != nil {
			return result, err
		}
		if record.Fork != nil {
			if err := b.fork(record); err != nil {
				return result, err
			}
		}
	}
	runtimes, err := e.exitEntries("runtime")
	if err != nil {
		return result, err
	}
	for _, entry := range runtimes {
		found := false
		for _, record := range b.records {
			if entry.Name() == record.RuntimeID {
				found = true
				if record.Phase == "deleted" {
					return result, fmt.Errorf("deleted epoch sandbox retains runtime state")
				}
			}
		}
		if !found {
			return result, fmt.Errorf("epoch runtime has no exact owned sandbox")
		}
	}
	for _, record := range b.records {
		if err := b.runtime("runtime", record, false); err != nil {
			return result, err
		}
	}
	rootEntries, err := e.exitEntries(".")
	if err != nil {
		return result, err
	}
	var exit workerBaseRelease
	err = e.readCheckpointJSON("worker-exit.json", &exit)
	if err == nil {
		if exit.Version != 1 || exit.Root != e.config.Root || exit.BootID != e.boot || exit.RuntimeSHA256 != e.config.RuntimeSHA256 || exit.CriuSHA256 != e.config.CriuSHA256 || exit.ManagerRoot != e.config.ManagerRoot || exit.RequestedAt == "" || exit.CompletedAt != "" {
			return result, fmt.Errorf("retained worker exit intent differs from its exact epoch")
		}
		seen := map[string]bool{}
		for _, paused := range exit.Paused {
			known, found := b.records[paused.ID]
			if !found || seen[paused.ID] || !reflect.DeepEqual(known, paused) || committedExitPause(paused) != nil {
				return result, fmt.Errorf("retained worker exit paused ledger differs")
			}
			seen[paused.ID] = true
		}
		var complete workerBaseRelease
		if err := e.readCheckpointJSON("worker-exit-complete.json", &complete); err == nil {
			stamp := complete.CompletedAt
			complete.CompletedAt = ""
			if stamp == "" || !reflect.DeepEqual(exit, complete) {
				return result, fmt.Errorf("retained worker exit completion differs")
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return result, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return result, err
	} else if _, err := e.root.Lstat("worker-exit-complete.json"); !errors.Is(err, os.ErrNotExist) {
		return result, fmt.Errorf("retained worker exit completion has no original intent")
	}
	paths := []string{}
	for _, entry := range rootEntries {
		name := entry.Name()
		if name == "worker-exit.json" || name == "worker-exit-complete.json" {
			b.file(name)
		}
		if _, allowed := b.allowed[name]; !allowed {
			return result, fmt.Errorf("unknown controller epoch entry: %s", name)
		}
		paths = append(paths, name)
	}
	full, err := ownedtree.Capture(ctx, e.root, paths)
	if err != nil {
		return result, err
	}
	for _, entry := range full.Entries {
		payload := false
		for _, root := range b.payloads {
			payload = payload || below(entry.Path, root)
		}
		if payload {
			continue
		}
		mode, found := b.allowed[entry.Path]
		if !found || entry.Object.Mode != mode || entry.Object.UID != 0 || entry.Object.GID != 0 {
			return result, fmt.Errorf("unknown or foreign epoch metadata: %s", entry.Path)
		}
	}
	sort.Strings(b.payloads)
	if len(b.payloads) == 0 {
		return result, fmt.Errorf("epoch inventory has no complete payload roots")
	}
	plan, err := ownedtree.Capture(ctx, e.root, b.payloads)
	if err != nil {
		return result, err
	}
	return epochPayloadInventory{Payload: plan, Witness: full, Records: b.records}, nil
}
