//go:build linux

package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"reflect"
	"strings"

	"factorybox/internal/sysboxcapture"
)

// The caller holds the ordinary controller lock. A proposal replaces its own
// record or adds its new request before any backing/intent is created. These
// byte allowances are not authority to release or adopt any kernel object.
type workerMetadataDelta struct {
	Record        *Record
	Fork          *forkIntent
	CaptureSource *Record
	CaptureID     string
	BaseBytes     int
}

type workerMetadataBudget struct{ bytes int }

func (b *workerMetadataBudget) grow(bytes int) error {
	if bytes < 0 || bytes > checkpointManifestLimit-b.bytes {
		return fmt.Errorf("exit completion metadata=%d bytes, next bound=%d, limit=%d; retain existing data and drain to a new worker epoch",
			b.bytes, bytes, checkpointManifestLimit)
	}
	b.bytes += bytes
	return nil
}

func (b *workerMetadataBudget) item(value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return b.grow(len(data) + 1) // A comma, or the envelope's final newline.
}

func workerMetadataActive(record Record) bool {
	return record.Phase != "paused" && !(record.Phase == "deleted" && record.Delete != nil &&
		record.Delete.CompletedAt != "" && record.Delete.Error == "")
}

func (b *workerMetadataBudget) record(record Record) error {
	if workerMetadataActive(record) {
		// Existing history is immutable. Every new operation rechecks its next
		// successful pause, including bounded errors and maximum field widths.
		// Charge that complete compact outcome rather than one full MiB for
		// every small unresolved record. The independent publisher still caps
		// every actual record at one MiB, including its newline/array comma.
		maximum := sandboxRecordOutcomeBound(record, true)
		data, err := json.Marshal(maximum)
		if err != nil {
			return err
		}
		return b.grow(min(len(data)+1, sandboxRecordLimit))
	}
	if record.Phase == "paused" {
		if err := committedExitPause(record); err != nil {
			return err
		}
		return b.item(record)
	}
	return nil
}

// Bound every changing field of a new immutable alias/canonical tree. Preserve
// already larger strings, including their encoded JSON escaping. Never mutate
// the source manifest or infer release authority from these projected values.
func workerTreeBound(tree sysboxcapture.FilesystemTree, source, saved, canonical string) sysboxcapture.FilesystemTree {
	tree.Source, tree.Saved = recordJSONMaximum(tree.Source, source), recordJSONMaximum(tree.Saved, saved)
	tree.Name = recordJSONMaximum(tree.Name, "lower")
	tree.SHA256 = recordJSONMaximum(tree.SHA256, strings.Repeat("f", 64))
	tree.Entries = math.MinInt
	tree.FileBytes, tree.HashedBytes, tree.CaptureUS = math.MinInt64, math.MinInt64, math.MinInt64
	tree.SourceReadUS, tree.CopyUS, tree.TargetReadUS = math.MinInt64, math.MinInt64, math.MinInt64
	if tree.Immutable != nil {
		identity := *tree.Immutable
		identity.Root = recordJSONMaximum(identity.Root, canonical)
		identity.Device, identity.Inode, identity.MountID = math.MaxUint64, math.MaxUint64, math.MaxUint64
		identity.Mode, identity.UID, identity.GID = math.MaxUint32, math.MaxUint32, math.MaxUint32
		identity.MtimeSec, identity.MtimeNano, identity.CtimeSec, identity.CtimeNano = math.MinInt64, math.MinInt64, math.MinInt64, math.MinInt64
		identity.Entries, identity.FileBytes, identity.HashedBytes = math.MinInt, math.MinInt64, math.MinInt64
		identity.SHA256 = recordJSONMaximum(identity.SHA256, strings.Repeat("f", 64))
		tree.Immutable = &identity
	}
	tree.Alias = &sysboxcapture.ImmutableAlias{Device: math.MaxUint64, Inode: math.MaxUint64, MountID: math.MaxUint64}
	return tree
}

func (e *Engine) workerCaptureAllowance(b *workerMetadataBudget, record Record, id string) error {
	if record.Base == nil || record.Base.Immutable == nil {
		return nil
	}
	saved := e.config.Root + "/objects/" + record.ID + "/captures/" + id + "/filesystem/lower"
	return b.item(workerTreeBound(*record.Base, record.InputBundle+"/lower", saved, record.Base.Immutable.Root))
}

func (e *Engine) workerRecordAllowance(b *workerMetadataBudget, record Record) error {
	if err := b.record(record); err != nil {
		return err
	}
	if workerMetadataActive(record) {
		// Retain capacity for the next committed pause of each active source.
		return e.workerCaptureAllowance(b, record, strings.Repeat("f", 32))
	}
	return nil
}

func (e *Engine) workerCaptureMetadata(ctx context.Context, b *workerMetadataBudget, record Record, delta workerMetadataDelta) error {
	entries, err := e.exitEntries("objects/" + record.ID + "/captures")
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	found := false
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !entry.IsDir() || !tokenID.MatchString(entry.Name()) {
			return fmt.Errorf("unrecognized capture inventory for %s", record.ID)
		}
		relative, _ := checkpointPath(record.ID, entry.Name())
		found = found || delta.CaptureSource != nil && record.ID == delta.CaptureSource.ID && entry.Name() == delta.CaptureID
		var checkpoint Checkpoint
		err := e.readCheckpointJSON(relative+"/checkpoint.json", &checkpoint)
		if errors.Is(err, os.ErrNotExist) {
			// An unfinished regular capture or internal timer guard can still
			// retain its prospective alias. A missing checkpoint is no discount.
			var guard captureGuardConfig
			if err := e.readCheckpointJSON(relative+"/guard/guard-intent.json", &guard); err != nil {
				return fmt.Errorf("unconfirmed capture metadata %s: %w", relative, err)
			}
			if guard.Version != 1 || guard.Root != e.config.Root || guard.Token != entry.Name() ||
				guard.Source.ID != record.ID || guard.Source.Owner != record.Owner ||
				guard.Source.RuntimeID != record.RuntimeID || guard.Source.BootID != record.BootID {
				return fmt.Errorf("capture guard metadata differs from its source")
			}
			if err := e.workerCaptureAllowance(b, guard.Source, entry.Name()); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if checkpoint.Version != 1 || checkpoint.ID != entry.Name() || checkpoint.SourceID != record.ID ||
			checkpoint.Owner != record.Owner || checkpoint.RuntimeID != record.RuntimeID ||
			checkpoint.BootID != record.BootID || checkpoint.ConfigSHA256 != record.ConfigSHA256 ||
			checkpoint.RuntimeSHA256 != e.config.RuntimeSHA256 || checkpoint.CriuSHA256 != e.config.CriuSHA256 ||
			checkpoint.ManagerRoot != e.config.ManagerRoot || checkpoint.Filesystem == nil {
			return fmt.Errorf("capture metadata differs from its source or recipe")
		}
		for _, tree := range checkpoint.Filesystem.Roots {
			if tree.Immutable != nil {
				// Retired generations remain conservatively charged until epoch
				// replacement. Admission does not authorize or repair retirement.
				if err := b.item(tree); err != nil {
					return err
				}
			}
		}
	}
	if delta.CaptureSource != nil && delta.CaptureSource.ID == record.ID && !found {
		return e.workerCaptureAllowance(b, *delta.CaptureSource, delta.CaptureID)
	}
	return nil
}

func (e *Engine) workerForkMetadata(ctx context.Context, b *workerMetadataBudget, intent forkIntent, exists bool, replacement *Record) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if intent.Checkpoint.Filesystem == nil {
		return fmt.Errorf("fork input has no filesystem metadata")
	}
	var base *sysboxcapture.FilesystemTree
	for _, tree := range intent.Checkpoint.Filesystem.Roots {
		if tree.Immutable == nil {
			continue
		}
		if base != nil || tree.Name != "lower" {
			return fmt.Errorf("fork input has an unrecognized immutable layout")
		}
		canonical := tree.Immutable.Root
		if intent.Recovery != nil {
			input := e.config.Root + "/forks/" + intent.TargetID + "/recovery/input"
			canonical = input + "/base/rootfs"
			recovered := workerTreeBound(tree, canonical, canonical, canonical)
			recovered.Alias = nil
			if err := b.item(recovered); err != nil {
				return err
			}
			alias := workerTreeBound(tree, canonical, input+"/filesystem/lower", canonical)
			if err := b.item(alias); err != nil {
				return err
			}
			tree = alias
		}
		projected := workerTreeBound(tree, tree.Saved,
			e.config.Root+"/forks/"+intent.TargetID+"/materialized/filesystem/lower", canonical)
		base = &projected
	}
	var prepared forkPreparation
	expectedBase := base != nil
	err := e.readCheckpointJSON("forks/"+intent.TargetID+"/preparation.json", &prepared)
	if err == nil {
		encoded, err := json.Marshal(intent)
		if err != nil || prepared.Version != 1 || prepared.Owner != intent.Owner ||
			prepared.IntentSHA256 != sum(encoded) || prepared.PreparedAt == "" || prepared.Filesystem == nil {
			return fmt.Errorf("fork preparation metadata differs from its intent: %v", err)
		}
		base = nil
		for _, tree := range prepared.Filesystem.Roots {
			if tree.Immutable == nil {
				continue
			}
			if base != nil || tree.Name != "lower" {
				return fmt.Errorf("prepared fork has an unrecognized immutable layout")
			}
			copy := tree
			base = &copy
		}
		if expectedBase != (base != nil) {
			return fmt.Errorf("prepared fork immutable layout differs from its input")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if base != nil {
		if err := b.item(*base); err != nil {
			return err
		}
	}
	if exists {
		record, err := e.read(intent.TargetID)
		if replacement != nil && replacement.ID == intent.TargetID {
			record, err = *replacement, nil
		}
		if err != nil || record.Owner != intent.Owner || !reflect.DeepEqual(record.Fork, &intent.Origin) ||
			record.InputSHA256 != sum(intent.OCI) || !reflect.DeepEqual(record.Base, base) {
			return fmt.Errorf("fork object metadata differs from its intent/preparation: %v", err)
		}
		return nil
	}
	// An intent can precede its object. Reserve that future record once, and
	// its next pause alias, without adopting any old runtime identity.
	if err := b.grow(sandboxRecordLimit); err != nil {
		return err
	}
	return e.workerCaptureAllowance(b, Record{ID: intent.TargetID, Phase: "starting",
		InputBundle: e.config.Root + "/forks/" + intent.TargetID + "/materialized/filesystem", Base: base}, strings.Repeat("f", 32))
}

func (e *Engine) admitWorkerMetadata(ctx context.Context, delta workerMetadataDelta) error {
	if e.config.BoundedStore == nil {
		return nil
	}
	if err := e.workerMetadataBound(ctx, delta); err != nil {
		return fmt.Errorf("new worker metadata refused: %w", err)
	}
	return nil
}

func (e *Engine) workerMetadataBound(ctx context.Context, delta workerMetadataDelta) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if delta.BaseBytes < 0 {
		return fmt.Errorf("invalid prospective base metadata allowance")
	}
	const timestamp = "9999-12-31T23:59:59.999999999Z"
	b := &workerMetadataBudget{}
	envelope := workerBaseRelease{Version: 1, Root: e.config.Root, BootID: e.boot,
		RuntimeSHA256: e.config.RuntimeSHA256, CriuSHA256: e.config.CriuSHA256,
		ManagerRoot: e.config.ManagerRoot, RequestedAt: timestamp, CompletedAt: timestamp}
	if err := b.item(envelope); err != nil {
		return err
	}
	// The base key is already present as null. Reserve every other array's
	// optional key/brackets even when currently empty. item counts all commas.
	if err := b.grow(len(`,"recovered_bases":[],"aliases":[],"paused":[]`) + delta.BaseBytes); err != nil {
		return err
	}
	bases, err := e.exitEntries("bases")
	if err != nil {
		return err
	}
	for _, entry := range bases {
		if err := ctx.Err(); err != nil {
			return err
		}
		base, err := e.readBase(entry.Name())
		if err != nil {
			return err
		}
		if err := b.item(base); err != nil {
			return err
		}
	}
	objects, err := e.exitEntries("objects")
	if err != nil {
		return err
	}
	ids := make(map[string]bool, len(objects))
	for _, entry := range objects {
		if err := ctx.Err(); err != nil {
			return err
		}
		record, err := e.read(entry.Name())
		if err != nil {
			return err
		}
		ids[record.ID] = true
		if delta.Record != nil && record.ID == delta.Record.ID {
			record = *delta.Record
		}
		if err := e.workerRecordAllowance(b, record); err != nil {
			return err
		}
		if err := e.workerCaptureMetadata(ctx, b, record, delta); err != nil {
			return err
		}
	}
	if delta.Record != nil && !ids[delta.Record.ID] {
		if err := e.workerRecordAllowance(b, *delta.Record); err != nil {
			return err
		}
		ids[delta.Record.ID] = true
	}
	forks, err := e.exitEntries("forks")
	if err != nil {
		return err
	}
	for _, entry := range forks {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !entry.IsDir() || !sandboxID.MatchString(entry.Name()) {
			return fmt.Errorf("unrecognized fork inventory")
		}
		if delta.Fork != nil && delta.Fork.TargetID == entry.Name() {
			return fmt.Errorf("new fork target already owns an intent")
		}
		var original forkIntent
		if err := e.readCheckpointJSON("forks/"+entry.Name()+"/intent.json", &original); err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return err
			}
			prepared, err := e.readUnpublishedRecoveryImport("forks/"+entry.Name(), entry.Name())
			if err != nil {
				return err
			}
			if err := e.workerForkMetadata(ctx, b, prepared.proposal(), false, nil); err != nil {
				return err
			}
			continue
		}
		intent, err := e.readForkIntent("forks/"+entry.Name(), original.Origin.SourceID, original.Origin.CheckpointID, entry.Name())
		if err != nil {
			return err
		}
		if err := e.workerForkMetadata(ctx, b, intent, ids[intent.TargetID], delta.Record); err != nil {
			return err
		}
	}
	if delta.Fork != nil {
		return e.workerForkMetadata(ctx, b, *delta.Fork, ids[delta.Fork.TargetID], delta.Record)
	}
	return nil
}
