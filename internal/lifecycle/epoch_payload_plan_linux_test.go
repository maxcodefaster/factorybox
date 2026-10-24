//go:build linux

package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"

	"factorybox/internal/ownedtree"
)

func epochPayloadSelectionFixture(t testing.TB, files int) (*os.Root, []string, []string) {
	t.Helper()
	directory := t.TempDir()
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	paths := []string{"objects/source/images", "objects/source/fs/root", "a/child", "a-b/leaf"}
	for _, name := range append(append([]string(nil), paths...), "metadata") {
		if err := root.MkdirAll(name, 0700); err != nil {
			t.Fatal(err)
		}
	}
	content := bytes.Repeat([]byte("independent file contents\n"), 43)
	for i := range files {
		if err := root.WriteFile(fmt.Sprintf("objects/source/fs/root/%04d", i), content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"metadata/ledger", "objects/source/images/ram", "a/child/data", "a-b/leaf/data"} {
		if err := root.WriteFile(name, []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := root.Link("objects/source/images/ram", "objects/source/images/ram-link"); err != nil {
		t.Fatal(err)
	}
	if err := root.Symlink("ram", "objects/source/images/symlink"); err != nil {
		t.Fatal(err)
	}
	return root, []string{"objects", "metadata", "a-b", "a"}, paths
}

func TestEpochPayloadSelectionEqualsIndependentCapture(t *testing.T) {
	root, all, paths := epochPayloadSelectionFixture(t, 3)
	witness, err := ownedtree.Capture(context.Background(), root, all)
	if err != nil {
		t.Fatal(err)
	}
	before, err := json.Marshal(witness)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := ownedtree.Capture(context.Background(), root, paths)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := epochPayloadPlan(context.Background(), root, witness, paths)
	if err != nil || !reflect.DeepEqual(actual, expected) {
		t.Fatal("selection must preserve the exact independently captured proof", err, actual, expected)
	}
	// Later loop-device rebinding mutates entry objects. It must not change the
	// independent complete witness or the caller's roots through shared slices.
	actual.Entries[0].Object.Device++
	actual.Anchors[0].Object.MountID++
	actual.Paths[0] = "changed"
	after, err := json.Marshal(witness)
	if err != nil || !bytes.Equal(before, after) || paths[0] != "objects/source/images" {
		t.Fatal("payload selection aliases the complete witness or authorized paths", err)
	}
}

func TestEpochPayloadSelectionRechecksEverySelectedTree(t *testing.T) {
	for _, change := range []string{"content", "unknown", "missing", "parent", "symlink", "external-hardlink"} {
		t.Run(change, func(t *testing.T) {
			root, all, paths := epochPayloadSelectionFixture(t, 3)
			witness, err := ownedtree.Capture(context.Background(), root, all)
			if err != nil {
				t.Fatal(err)
			}
			name := "objects/source/fs/root/0000"
			switch change {
			case "content":
				original, readErr := root.ReadFile(name)
				if readErr != nil {
					t.Fatal(readErr)
				}
				original[0] ^= 1
				err = root.WriteFile(name, original, 0600)
			case "unknown":
				err = root.WriteFile("objects/source/images/foreign", []byte("preserve"), 0600)
			case "missing":
				err = root.Remove(name)
			case "parent":
				err = root.Rename("objects/source/images", "objects/source/saved-images")
				if err == nil {
					err = root.Mkdir("objects/source/images", 0700)
				}
			case "symlink":
				err = root.Remove(name)
				if err == nil {
					err = root.Symlink("../../../../metadata/ledger", name)
				}
			case "external-hardlink":
				err = root.Link(name, "metadata/external-link")
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := epochPayloadPlan(context.Background(), root, witness, paths); err == nil {
				t.Fatal("selected payload mutation was accepted", change)
			}
			if data, err := root.ReadFile("metadata/ledger"); err != nil || string(data) != "metadata/ledger" {
				t.Fatal("refused selection changed retained metadata", err)
			}
		})
	}
}

func TestEpochPayloadSelectionRejectsLinksIntoRetainedMetadata(t *testing.T) {
	root, all, paths := epochPayloadSelectionFixture(t, 1)
	if err := root.Link("objects/source/fs/root/0000", "metadata/shared-payload"); err != nil {
		t.Fatal(err)
	}
	// All hardlinks are inside the complete witness. Only the subsequent,
	// independent selected-tree check can detect the retained external link.
	witness, err := ownedtree.Capture(context.Background(), root, all)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := epochPayloadPlan(context.Background(), root, witness, paths); err == nil {
		t.Fatal("complete-witness link counts authorized an external payload hardlink")
	}
}

func TestEpochPayloadSelectionRejectsInvalidOrUnobservedRoots(t *testing.T) {
	root, all, paths := epochPayloadSelectionFixture(t, 1)
	witness, err := ownedtree.Capture(context.Background(), root, all)
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range [][]string{nil, {"."}, {"../outside"}, {"/objects"}, {"objects/../metadata"},
		{"objects", "objects/source/images"}, {"a", "a-b", "a/child"}, {paths[0], paths[0]}, {"objects/missing"}} {
		if _, err := epochPayloadPlan(context.Background(), root, witness, invalid); err == nil {
			t.Fatal("invalid, overlapping or unobserved payload roots accepted", invalid)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := epochPayloadPlan(ctx, root, witness, paths); err == nil {
		t.Fatal("cancelled complete validation was accepted")
	}
	for _, changed := range []string{"root", "parent", "payload"} {
		t.Run(changed, func(t *testing.T) {
			copy := witness
			copy.Entries = append([]ownedtree.Entry(nil), witness.Entries...)
			if changed == "root" {
				copy.Root.MountID++
			} else {
				name := "objects/source"
				if changed == "payload" {
					name = paths[0]
				}
				for i := range copy.Entries {
					if copy.Entries[i].Path == name {
						copy.Entries[i].Object.MountID++
					}
				}
			}
			if _, err := epochPayloadPlan(context.Background(), root, copy, paths); err == nil {
				t.Fatal("changed mount identity was accepted")
			}
		})
	}
}

func BenchmarkEpochPayloadInventory(b *testing.B) {
	root, all, paths := epochPayloadSelectionFixture(b, 1000)
	for _, method := range []string{"recapture", "select-and-validate"} {
		b.Run(method, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				witness, err := ownedtree.Capture(context.Background(), root, all)
				if err != nil {
					b.Fatal(err)
				}
				var selected ownedtree.Plan
				if method == "recapture" {
					selected, err = ownedtree.Capture(context.Background(), root, paths)
				} else {
					selected, err = epochPayloadPlan(context.Background(), root, witness, paths)
				}
				if err != nil || len(selected.Entries) != 1009 {
					b.Fatal("complete witness and independently verified payloads", len(selected.Entries), err)
				}
			}
		})
	}
}
