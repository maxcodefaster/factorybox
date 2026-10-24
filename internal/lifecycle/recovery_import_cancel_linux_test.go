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
	"strings"
	"testing"

	"factorybox/internal/ownedtree"
)

func TestRecoveryImportCancellationPreservesIdentityAndClearsOnlyItsCopy(t *testing.T) {
	e, operation, _ := recoveryImportMetadataFixture(t)
	journal, err := os.ReadFile(filepath.Join(e.config.Root, operation, "import.json"))
	if err != nil {
		t.Fatal(err)
	}
	inspected, err := e.inspectRecoveryImportLocked(context.Background(), e, "interrupted")
	if err != nil {
		t.Fatal(err)
	}
	completed, err := e.cancelRecoveryImportLocked(context.Background(), e, inspected)
	if err != nil || completed.CompletedAt == "" || !e.cancelledRecoveryImport(context.Background(), "interrupted") {
		t.Fatal("explicit cancellation did not leave a verified terminal reservation", completed, err)
	}
	if _, err := e.root.Lstat(operation + "/recovery"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("cancelled copy remains", err)
	}
	after, err := os.ReadFile(filepath.Join(e.config.Root, operation, "import.json"))
	if err != nil || string(after) != string(journal) {
		t.Fatal("cancellation rewrote the original provenance", err)
	}
	if err := e.admitWorkerMetadata(context.Background(), workerMetadataDelta{BaseBytes: checkpointManifestLimit - 4096}); err != nil {
		t.Fatal("cancelled target still reserves a future restore or pause", err)
	}
	replay, err := e.cancelRecoveryImportLocked(context.Background(), e, inspected)
	if err != nil || !reflect.DeepEqual(replay, completed) {
		t.Fatal("exact cancellation replay changed its completion", replay, err)
	}
	p, err := e.readRecoveryImportPreparation(operation, "interrupted")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.beginRecoveryImport(operation, p.Request, p.TargetID, p.Source, p.Checkpoint); err == nil {
		t.Fatal("cancelled target identity was reused")
	}
	current, err := e.inspectRecoveryImportLocked(context.Background(), e, "interrupted")
	if err != nil || current.Cancelled == nil || !reflect.DeepEqual(*current.Cancelled, completed) {
		t.Fatal("inspection lost the durable cancellation", current, err)
	}
}

func TestRecoveryImportCancellationRejectsChangedStageBeforeAnyRemoval(t *testing.T) {
	for _, change := range []string{"content", "owner", "root", "target", "hash", "outside-plan", "directory-generation", "object", "unknown", "external-hardlink"} {
		t.Run(change, func(t *testing.T) {
			e, operation, _ := recoveryImportMetadataFixture(t)
			inspected, err := e.inspectRecoveryImportLocked(context.Background(), e, "interrupted")
			if err != nil {
				t.Fatal(err)
			}
			payload := filepath.Join(e.config.Root, operation, "recovery/partial-copy")
			switch change {
			case "content":
				err = os.WriteFile(payload, []byte("changed payload"), 0600)
			case "owner":
				inspected.Owner = "different"
			case "root":
				inspected.SourceRoot += "-different"
			case "target":
				inspected.TargetID = "different"
			case "hash":
				inspected.InventorySHA256 = "different"
			case "outside-plan":
				inspected.Inventory.Paths = []string{"objects"}
				data, _ := json.Marshal(inspected.Inventory)
				inspected.InventorySHA256 = sum(data)
			case "directory-generation":
				inspected.DirectoryChange = "different"
			case "object":
				err = e.root.Mkdir("objects/interrupted", 0700)
			case "unknown":
				err = os.WriteFile(filepath.Join(e.config.Root, operation, "foreign"), []byte("retain"), 0600)
			case "external-hardlink":
				err = os.Link(payload, filepath.Join(e.config.Root, "outside-link"))
			}
			if err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(payload)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := e.cancelRecoveryImportLocked(context.Background(), e, inspected); err == nil {
				t.Fatal("changed or foreign import was cancelled")
			}
			after, err := os.ReadFile(payload)
			if err != nil || string(before) != string(after) {
				t.Fatal("refused cancellation removed or changed remaining payload", err)
			}
			if _, err := e.root.Lstat(operation + "/import-cancelled.json"); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("refused cancellation published completion", err)
			}
		})
	}
}

func TestRecoveryImportPartialCancellationRequiresFreshInspection(t *testing.T) {
	e, operation, _ := recoveryImportMetadataFixture(t)
	other := filepath.Join(e.config.Root, operation, "recovery/second-file")
	if err := os.WriteFile(other, []byte("retain until freshly authorized"), 0600); err != nil {
		t.Fatal(err)
	}
	old, err := e.inspectRecoveryImportLocked(context.Background(), e, "interrupted")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.root.Remove(operation + "/recovery/partial-copy"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.cancelRecoveryImportLocked(context.Background(), e, old); err == nil {
		t.Fatal("old inspection granted missing-entry deletion replay")
	}
	if data, err := os.ReadFile(other); err != nil || string(data) != "retain until freshly authorized" {
		t.Fatal("stale inspection changed a surviving member", err)
	}
	fresh, err := e.inspectRecoveryImportLocked(context.Background(), e, "interrupted")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.cancelRecoveryImportLocked(context.Background(), e, fresh); err != nil {
		t.Fatal("fresh complete inspection could not cancel remaining copy", err)
	}
}

func TestRecoveryImportCancelledPayloadCannotReappear(t *testing.T) {
	e, operation, _ := recoveryImportMetadataFixture(t)
	inspected, err := e.inspectRecoveryImportLocked(context.Background(), e, "interrupted")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.cancelRecoveryImportLocked(context.Background(), e, inspected); err != nil {
		t.Fatal(err)
	}
	if err := e.root.Mkdir(operation+"/recovery", 0700); err != nil {
		t.Fatal(err)
	}
	before, err := ownedtree.Capture(context.Background(), e.root, []string{operation})
	if err != nil {
		t.Fatal(err)
	}
	if e.cancelledRecoveryImport(context.Background(), "interrupted") {
		t.Fatal("new payload inherited an old cancellation")
	}
	if err := e.admitWorkerMetadata(context.Background(), workerMetadataDelta{}); err == nil {
		t.Fatal("changed cancelled inventory was silently discounted")
	}
	if _, err := e.cancelRecoveryImportLocked(context.Background(), e, inspected); err == nil {
		t.Fatal("cancellation replay removed a replacement payload")
	}
	after, err := ownedtree.Capture(context.Background(), e.root, []string{operation})
	if err != nil || !equalOwnedPlans(before, after) {
		t.Fatal("refused replay changed replacement bytes or identities", err)
	}
}

func TestRecoveryImportCancellationHandlesOnlyOneExactInterruptedPublication(t *testing.T) {
	for _, kind := range []string{"partial", "changed", "multiple", "bad-name", "bad-mode", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			e, operation, _ := recoveryImportMetadataFixture(t)
			name := ".checkpoint-" + strings.Repeat("a", 32)
			if kind == "bad-name" {
				name = ".checkpoint-unknown"
			}
			path := filepath.Join(e.config.Root, operation, name)
			if err := os.WriteFile(path, []byte(`{"partial":`), 0600); err != nil {
				t.Fatal(err)
			}
			if kind == "multiple" {
				if err := os.WriteFile(filepath.Join(e.config.Root, operation, ".checkpoint-"+strings.Repeat("b", 32)), []byte("second"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "bad-mode" {
				if err := os.Chmod(path, 0644); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "hardlink" {
				if err := os.Link(path, filepath.Join(e.config.Root, "outside-publication")); err != nil {
					t.Fatal(err)
				}
			}
			inspected, err := e.inspectRecoveryImportLocked(context.Background(), e, "interrupted")
			if kind != "partial" && kind != "changed" {
				if err == nil {
					t.Fatal("unknown or unsafe publication acquired cancellation authority")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if kind == "changed" {
				if err := os.WriteFile(path, []byte("replacement"), 0600); err != nil {
					t.Fatal(err)
				}
				if _, err := e.cancelRecoveryImportLocked(context.Background(), e, inspected); err == nil {
					t.Fatal("stale inspection removed a changed publication")
				}
				return
			}
			if _, err := e.cancelRecoveryImportLocked(context.Background(), e, inspected); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) || !e.cancelledRecoveryImport(context.Background(), "interrupted") {
				t.Fatal("exact interrupted publication was not explicitly cancelled", err)
			}
		})
	}
}

// A killed initial publication has no committed reservation and cannot own any
// recovery payload yet. Its exact private metadata stage still needs an
// explicit manual path, without treating an unknown directory as a valid fork.
func TestRecoveryImportInitialPublicationCancellation(t *testing.T) {
	for _, temporary := range []bool{false, true} {
		t.Run(fmt.Sprint(temporary), func(t *testing.T) {
			e, operation, _ := recoveryImportMetadataFixture(t)
			for _, path := range []string{operation + "/recovery/partial-copy", operation + "/recovery", operation + "/import.json"} {
				if err := e.root.Remove(path); err != nil {
					t.Fatal(err)
				}
			}
			if temporary {
				if err := e.root.WriteFile(operation+"/.checkpoint-"+strings.Repeat("a", 32), []byte(`{"uncommitted":`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := e.admitWorkerMetadata(context.Background(), workerMetadataDelta{}); err == nil {
				t.Fatal("initial uncommitted stage silently gained fork admission authority")
			}
			inspected, err := e.inspectRecoveryImportLocked(context.Background(), e, "interrupted")
			if err != nil {
				t.Fatal("initial unpublished metadata cannot be inspected for explicit cancellation", err)
			}
			if !inspected.ReservationAbsent || inspected.Owner != "" || inspected.PreparationSHA256 != "" || inspected.Cancelled != nil {
				t.Fatal("uncommitted metadata invented an original reservation", inspected)
			}
			completed, err := e.cancelRecoveryImportLocked(context.Background(), e, inspected)
			if err != nil || completed.CompletedAt == "" || !completed.ReservationAbsent {
				t.Fatal("exact initial publication could not be explicitly cancelled", completed, err)
			}
			if _, err := e.root.Lstat(operation); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("cancelled uncommitted metadata remains", err)
			}
			if err := e.admitWorkerMetadata(context.Background(), workerMetadataDelta{}); err != nil {
				t.Fatal("explicit initial-stage cancellation did not restore independent admission", err)
			}
			if _, err := e.cancelRecoveryImportLocked(context.Background(), e, inspected); err == nil {
				t.Fatal("absent unaccepted stage gained durable missing-entry replay authority")
			}
		})
	}
}

func recoveryImportInitialMetadataFixture(t *testing.T) (*Engine, string) {
	t.Helper()
	e, operation, _ := recoveryImportMetadataFixture(t)
	for _, path := range []string{operation + "/recovery/partial-copy", operation + "/recovery", operation + "/import.json"} {
		if err := e.root.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	return e, operation
}

func TestRecoveryImportInitialInspectionRefusesPayloadAndUnknownMetadata(t *testing.T) {
	for _, kind := range []string{"payload", "intent", "object", "unknown", "bad-temporary", "repeated-temporary", "symlink", "hardlink", "owner", "mode", "oversize", "corrupt-journal"} {
		t.Run(kind, func(t *testing.T) {
			e, operation := recoveryImportInitialMetadataFixture(t)
			name := operation + "/.checkpoint-" + strings.Repeat("a", 32)
			if err := e.root.WriteFile(name, []byte(`{"partial":`), 0600); err != nil {
				t.Fatal(err)
			}
			paths := []string{operation}
			var err error
			switch kind {
			case "payload":
				err = e.root.Mkdir(operation+"/recovery", 0700)
			case "intent":
				err = e.root.WriteFile(operation+"/intent.json", []byte("retain"), 0600)
			case "object":
				err = e.root.Mkdir("objects/interrupted", 0700)
				paths = append(paths, "objects/interrupted")
			case "unknown":
				err = e.root.WriteFile(operation+"/foreign", []byte("retain"), 0600)
			case "bad-temporary":
				err = e.root.Rename(name, operation+"/.checkpoint-not-a-token")
			case "repeated-temporary":
				err = e.root.WriteFile(operation+"/.checkpoint-"+strings.Repeat("b", 32), []byte("retain"), 0600)
			case "symlink":
				err = e.root.Remove(name)
				if err == nil {
					err = e.root.Symlink("../retained-input", name)
				}
			case "hardlink":
				err = os.Link(filepath.Join(e.config.Root, name), filepath.Join(e.config.Root, "outside-publication"))
				paths = append(paths, "outside-publication")
			case "owner":
				err = os.Chown(filepath.Join(e.config.Root, name), 1, 0)
			case "mode":
				err = os.Chmod(filepath.Join(e.config.Root, name), 0644)
			case "oversize":
				var file *os.File
				file, err = e.root.OpenFile(name, os.O_WRONLY, 0600)
				if err == nil {
					err = errors.Join(file.Truncate(checkpointManifestLimit+1), file.Close())
				}
			case "corrupt-journal":
				err = e.root.WriteFile(operation+"/import.json", []byte(`{"incomplete":`), 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			before, err := ownedtree.Capture(context.Background(), e.root, paths)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := e.inspectRecoveryImportLocked(context.Background(), e, "interrupted"); err == nil {
				t.Fatal("payload, published ownership or foreign metadata gained initial-stage cancellation authority")
			}
			after, err := ownedtree.Capture(context.Background(), e.root, paths)
			if err != nil || !equalOwnedPlans(before, after) {
				t.Fatal("refused initial inspection changed original input", err)
			}
		})
	}
}

func TestRecoveryImportInitialCancellationRejectsChangedInspectionAndNeedsFreshRemainder(t *testing.T) {
	for _, kind := range []string{"content", "unknown", "object", "root", "owner", "preparation", "generation", "boot", "inventory-hash", "mode-flag", "partial-removal"} {
		t.Run(kind, func(t *testing.T) {
			e, operation := recoveryImportInitialMetadataFixture(t)
			name := operation + "/.checkpoint-" + strings.Repeat("a", 32)
			if err := e.root.WriteFile(name, []byte("initial partial metadata"), 0600); err != nil {
				t.Fatal(err)
			}
			inspected, err := e.inspectRecoveryImportLocked(context.Background(), e, "interrupted")
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "content":
				err = e.root.WriteFile(name, []byte("replacement"), 0600)
			case "unknown":
				err = e.root.WriteFile(operation+"/foreign", []byte("retain"), 0600)
			case "object":
				err = e.root.Mkdir("objects/interrupted", 0700)
			case "root":
				inspected.SourceRoot += "-different"
			case "owner":
				inspected.Owner = strings.Repeat("f", 32)
			case "preparation":
				inspected.PreparationSHA256 = strings.Repeat("f", 64)
			case "generation":
				inspected.DirectoryBirth = "different"
			case "boot":
				inspected.BootID = "different"
			case "inventory-hash":
				inspected.InventorySHA256 = strings.Repeat("f", 64)
			case "mode-flag":
				inspected.ReservationAbsent = false
			case "partial-removal":
				err = e.root.Remove(name)
			}
			if err != nil {
				t.Fatal(err)
			}
			before, err := ownedtree.Capture(context.Background(), e.root, []string{operation})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := e.cancelRecoveryImportLocked(context.Background(), e, inspected); err == nil {
				t.Fatal("stale or changed initial inspection authorized metadata removal")
			}
			after, err := ownedtree.Capture(context.Background(), e.root, []string{operation})
			if err != nil || !equalOwnedPlans(before, after) {
				t.Fatal("refused initial cancellation changed remaining metadata", err)
			}
			if kind == "partial-removal" {
				fresh, err := e.inspectRecoveryImportLocked(context.Background(), e, "interrupted")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := e.cancelRecoveryImportLocked(context.Background(), e, fresh); err != nil {
					t.Fatal("fresh inspection could not cancel its exact remaining empty stage", err)
				}
			}
		})
	}
}

func TestRecoveryImportInitialCancellationCannotWithdrawCommittedReservation(t *testing.T) {
	e, operation, _ := recoveryImportMetadataFixture(t)
	inspected, err := e.inspectRecoveryImportLocked(context.Background(), e, "interrupted")
	if err != nil {
		t.Fatal(err)
	}
	before := inspected.Inventory
	inspected.ReservationAbsent, inspected.Owner, inspected.PreparationSHA256 = true, "", ""
	if _, err := e.cancelRecoveryImportLocked(context.Background(), e, inspected); err == nil {
		t.Fatal("committed reservation was abandoned through a forged initial-stage inspection")
	}
	after, err := ownedtree.Capture(context.Background(), e.root, []string{operation})
	if err != nil || !equalOwnedPlans(before, after) {
		t.Fatal("forged initial-stage cancellation changed a committed reservation", err)
	}
}
