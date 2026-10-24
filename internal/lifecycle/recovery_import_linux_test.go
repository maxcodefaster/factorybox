//go:build linux

package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"factorybox/internal/ownedtree"
	"factorybox/internal/storefs"
	"factorybox/internal/sysboxcapture"
)

// Exercise the real metadata reader with an interrupted, unpublished import.
// Root ownership is part of that reader's production contract.
func recoveryImportMetadataFixture(t *testing.T) (*Engine, string, map[string]any) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("requires root-owned controller metadata")
	}
	e, _ := recordMetadataEngine(t)
	if err := e.root.Remove("objects/source"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"bases", "forks", "forks/interrupted", "forks/interrupted/recovery"} {
		if err := e.root.Mkdir(name, 0700); err != nil {
			t.Fatal(err)
		}
	}
	e.boot = "same-boot"
	e.config.RuntimeSHA256 = strings.Repeat("d", 64)
	e.config.CriuSHA256 = strings.Repeat("e", 64)
	e.config.ManagerRoot = t.TempDir()
	e.config.BoundedStore = &storefs.Active{}
	old := t.TempDir()
	saved := Checkpoint{Version: 1, ID: strings.Repeat("a", 32), SourceID: "source",
		Owner: strings.Repeat("b", 32), RuntimeID: "old-runtime", BootID: e.boot,
		ConfigSHA256: strings.Repeat("c", 64), RuntimeSHA256: e.config.RuntimeSHA256,
		CriuSHA256: e.config.CriuSHA256, ManagerRoot: e.config.ManagerRoot,
		CapturedAt: "captured", ReleasedAt: "released", SourceStopped: true, SourcePID: 123,
		Filesystem: &sysboxcapture.FilesystemSnapshot{Version: 1, Roots: []sysboxcapture.FilesystemTree{{
			Name: "lower", Source: old + "/base", Saved: old + "/saved/lower", SHA256: strings.Repeat("f", 64),
			Immutable: &sysboxcapture.ImmutableTree{Root: old + "/base", SHA256: strings.Repeat("f", 64)}}}}}
	data, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	request := RecoveryRequest{SourceRoot: old, SourceID: saved.SourceID, SnapshotID: saved.ID,
		SourceOwner: saved.Owner, CheckpointSHA256: sum(data)}
	source := Record{Version: 1, ID: saved.SourceID, Owner: saved.Owner, RuntimeID: saved.RuntimeID,
		BootID: saved.BootID, ConfigSHA256: saved.ConfigSHA256, RuntimeSHA256: saved.RuntimeSHA256,
		Phase: "paused", Pause: &PauseOperation{SnapshotID: saved.ID, CheckpointSHA256: request.CheckpointSHA256,
			RequestedAt: "requested", CompletedAt: "completed", SourcePID: saved.SourcePID}}
	prepared := map[string]any{"version": 1, "root": e.config.Root, "target_id": "interrupted",
		"owner": strings.Repeat("c", 32), "boot_id": e.boot, "runtime_sha256": e.config.RuntimeSHA256,
		"criu_sha256": e.config.CriuSHA256, "manager_root": e.config.ManagerRoot,
		"request": request, "source": source, "checkpoint": saved, "requested_at": "requested"}
	operation := "forks/interrupted"
	if err := e.publishCheckpoint(operation, "import.json", prepared); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.config.Root, operation, "recovery/partial-copy"), []byte("incomplete payload"), 0600); err != nil {
		t.Fatal(err)
	}
	return e, operation, prepared
}

func TestRecoveryImportReservesMetadataWithoutBlockingIndependentWork(t *testing.T) {
	e, operation, _ := recoveryImportMetadataFixture(t)
	before, err := ownedtree.Capture(context.Background(), e.root, []string{operation})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.admitWorkerMetadata(context.Background(), workerMetadataDelta{}); err != nil {
		t.Fatal("identified incomplete import blocked independent admission", err)
	}
	// Its prospective complete fork still consumes the exit-metadata budget.
	if err := e.admitWorkerMetadata(context.Background(), workerMetadataDelta{BaseBytes: checkpointManifestLimit - 4096}); err == nil {
		t.Fatal("incomplete import lost its eventual record/base reservation")
	}
	if _, err := e.readForkIntent(operation, "source", strings.Repeat("a", 32), "interrupted"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("import preparation became restore authority", err)
	}
	after, err := ownedtree.Capture(context.Background(), e.root, []string{operation})
	if err != nil || !equalOwnedPlans(before, after) {
		t.Fatal("admission changed the retained partial import", err)
	}
}

func TestRecoveryImportWithoutKernelObjectsDoesNotBlockCleanExit(t *testing.T) {
	e, operation, _ := recoveryImportMetadataFixture(t)
	before, err := ownedtree.Capture(context.Background(), e.root, []string{operation})
	if err != nil {
		t.Fatal(err)
	}
	bases, aliases, err := e.exitForkBases(context.Background())
	if err != nil || len(bases) != 0 || len(aliases) != 0 {
		t.Fatal("pure partial copy invented kernel release work or blocked exit", bases, aliases, err)
	}
	after, err := ownedtree.Capture(context.Background(), e.root, []string{operation})
	if err != nil || !equalOwnedPlans(before, after) {
		t.Fatal("exit inspection changed retained import bytes or identities", err)
	}
}

func equalOwnedPlans(a, b ownedtree.Plan) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return string(left) == string(right)
}

func TestRecoveryImportChangedProofOrPublishedObjectRefuses(t *testing.T) {
	for _, field := range []string{"root", "target_id", "owner", "boot_id", "runtime_sha256", "criu_sha256", "manager_root", "requested_at", "request", "source", "checkpoint", "object", "unknown"} {
		t.Run(field, func(t *testing.T) {
			e, operation, prepared := recoveryImportMetadataFixture(t)
			if field == "object" {
				if err := e.root.Mkdir("objects/interrupted", 0700); err != nil {
					t.Fatal(err)
				}
			} else if field == "unknown" {
				if err := os.WriteFile(filepath.Join(e.config.Root, operation, "foreign"), []byte("retain"), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				prepared[field] = "changed"
				if field == "requested_at" {
					prepared[field] = ""
				}
				data, err := json.Marshal(prepared)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(e.config.Root, operation, "import.json"), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			before, err := ownedtree.Capture(context.Background(), e.root, []string{operation})
			if err != nil {
				t.Fatal(err)
			}
			if err := e.admitWorkerMetadata(context.Background(), workerMetadataDelta{}); err == nil {
				t.Fatal("changed or unconfirmed import was admitted")
			}
			if _, _, err := e.exitForkBases(context.Background()); err == nil {
				t.Fatal("changed or unconfirmed import established exit authority")
			}
			after, err := ownedtree.Capture(context.Background(), e.root, []string{operation})
			if err != nil || !equalOwnedPlans(before, after) {
				t.Fatal("refusal modified retained import", err)
			}
		})
	}
}
