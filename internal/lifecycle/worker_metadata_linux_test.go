//go:build linux

package lifecycle

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"factorybox/internal/storefs"
	"factorybox/internal/sysboxcapture"
)

func workerMetadataPauseFixture() Record {
	return Record{Version: 1, ID: "source", Phase: "paused", Start: Operation{CompletedAt: "started"},
		RuntimeCreated: "created", Pause: &PauseOperation{SnapshotID: strings.Repeat("a", 32),
			CheckpointSHA256: strings.Repeat("b", 64), RequestedAt: "requested", CompletedAt: "completed", SourcePID: 123}}
}

func TestWorkerMetadataReservationCoversLargestPublishablePause(t *testing.T) {
	record := workerMetadataPauseFixture()
	record.PauseHistory = []PauseOperation{{Error: "x"}}
	data, err := sandboxRecordBytes(record)
	if err != nil {
		t.Fatal(err)
	}
	record.PauseHistory[0].Error += strings.Repeat("x", sandboxRecordLimit-len(data)-1)
	data, err = sandboxRecordBytes(record)
	if err != nil || len(data)+1 != sandboxRecordLimit {
		t.Fatal("fixture must reach the real indented publisher's boundary", len(data), err)
	}
	b := &workerMetadataBudget{}
	active := record
	active.Phase = "running"
	if err := b.record(active); err != nil || b.bytes != sandboxRecordLimit {
		t.Fatal("active record lacks its full future pause reservation", b.bytes, err)
	}
	completed := &workerMetadataBudget{}
	if err := completed.record(record); err != nil || completed.bytes > b.bytes {
		t.Fatal("admitted active reservation cannot contain a publishable committed pause", completed.bytes, b.bytes, err)
	}
	deleted := record
	deleted.Phase = "deleted"
	before := completed.bytes
	if err := completed.record(deleted); err != nil || completed.bytes != before+sandboxRecordLimit {
		t.Fatal("unconfirmed deletion silently discounted a record", err)
	}
	deleted.Delete = &Operation{CompletedAt: "deleted"}
	before = completed.bytes
	if err := completed.record(deleted); err != nil || completed.bytes != before {
		t.Fatal("confirmed deletion incorrectly retains a paused array entry", err)
	}
}

func TestWorkerTreeBoundPreservesInputAndEncodedWidths(t *testing.T) {
	original := sysboxcapture.FilesystemTree{Name: "lower", Source: strings.Repeat("\x00", 3000),
		Saved: "/old/saved", SHA256: strings.Repeat("c", 64),
		Immutable: &sysboxcapture.ImmutableTree{Root: "/old/base", SHA256: strings.Repeat("c", 64)}}
	before, _ := json.Marshal(original)
	bound := workerTreeBound(original, "/new/source", "/new/saved", "/new/canonical")
	after, _ := json.Marshal(original)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("projected immutable metadata mutated its input")
	}
	upper, _ := json.Marshal(bound)
	for _, source := range []string{original.Source, "/new/source"} {
		actual := original
		actual.Source, actual.Saved = source, "/new/saved"
		identity := *original.Immutable
		identity.Root = "/new/canonical"
		identity.Device, identity.Inode, identity.MountID = math.MaxUint64, math.MaxUint64, math.MaxUint64
		identity.Mode, identity.UID, identity.GID = math.MaxUint32, math.MaxUint32, math.MaxUint32
		identity.MtimeSec, identity.CtimeSec = math.MinInt64, math.MinInt64
		actual.Immutable = &identity
		actual.Alias = &sysboxcapture.ImmutableAlias{Device: math.MaxUint64, Inode: math.MaxUint64, MountID: math.MaxUint64}
		encoded, _ := json.Marshal(actual)
		if len(encoded) > len(upper) {
			t.Fatal("projected tree undercounts a valid serialized field width", len(encoded), len(upper))
		}
	}
}

func TestWorkerMetadataPendingRecoveryCoversWholeExitRecord(t *testing.T) {
	engine, _ := recordMetadataEngine(t)
	engine.config.Root = "/owned/" + strings.Repeat("x", 3000) + "\troot"
	tree := sysboxcapture.FilesystemTree{Name: "lower", Source: "/old/input", Saved: "/old/capture/filesystem/lower",
		SHA256: strings.Repeat("d", 64), Immutable: &sysboxcapture.ImmutableTree{
			Root: "/old/base", SHA256: strings.Repeat("d", 64)}}
	intent := forkIntent{TargetID: "target", Checkpoint: Checkpoint{Filesystem: &sysboxcapture.FilesystemSnapshot{
		Roots: []sysboxcapture.FilesystemTree{tree}}}, Recovery: &RecoveryOrigin{}}
	b := &workerMetadataBudget{}
	if err := engine.workerForkMetadata(context.Background(), b, intent, false, nil); err != nil {
		t.Fatal(err)
	}
	record := workerMetadataPauseFixture()
	input := engine.config.Root + "/forks/target/recovery/input"
	canonical := tree
	canonical.Source, canonical.Saved = input+"/base/rootfs", input+"/base/rootfs"
	identity := *tree.Immutable
	identity.Root = canonical.Saved
	canonical.Immutable = &identity
	alias := canonical
	alias.Saved, alias.Alias = input+"/filesystem/lower", &sysboxcapture.ImmutableAlias{Device: 1, Inode: 2, MountID: 3}
	materialized := alias
	materialized.Source, materialized.Saved = alias.Saved, engine.config.Root+"/forks/target/materialized/filesystem/lower"
	paused := materialized
	paused.Source, paused.Saved = materialized.Saved, engine.config.Root+"/objects/target/captures/"+strings.Repeat("f", 32)+"/filesystem/lower"
	record.Base = &materialized
	encoded, _ := json.MarshalIndent(record, "", "  ")
	record.PauseHistory = []PauseOperation{{Error: strings.Repeat("x", sandboxRecordLimit-len(encoded)-2048)}}
	if _, err := sandboxRecordBytes(record); err != nil {
		t.Fatal("large future pause does not fit the real writer", err)
	}
	ledger := workerBaseRelease{RecoveredBases: []sysboxcapture.FilesystemTree{canonical},
		Aliases: []sysboxcapture.FilesystemTree{alias, materialized, paused}, Paused: []Record{record}}
	actual, err := checkpointRecordBytes(ledger)
	if err != nil || len(actual)+1 > b.bytes {
		t.Fatal("pending recovery omitted an eventual exit dependency", len(actual)+1, b.bytes, err)
	}
}

func TestWorkerMetadataUnknownInventoryRefusesWithoutMutation(t *testing.T) {
	engine, path := recordMetadataEngine(t)
	root := filepath.Dir(filepath.Dir(filepath.Dir(path)))
	if err := os.RemoveAll(filepath.Join(root, "objects/source")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"bases", "forks", "forks/unknown"} {
		if err := os.Mkdir(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	engine.config.BoundedStore = &storefs.Active{}
	if err := engine.admitWorkerMetadata(context.Background(), workerMetadataDelta{}); err == nil {
		t.Fatal("unknown unfinished fork inventory was discounted")
	}
	entries, err := os.ReadDir(filepath.Join(root, "forks/unknown"))
	if err != nil || len(entries) != 0 {
		t.Fatal("refused inventory check created or removed backing", entries, err)
	}
	if _, err := os.Lstat(filepath.Join(root, "worker-exit.json")); !os.IsNotExist(err) {
		t.Fatal("new-work metadata refusal fenced the old worker", err)
	}
}
