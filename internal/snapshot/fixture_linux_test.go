package snapshot

import (
	"os"
	"path/filepath"
	"testing"
)

// The inherited-memory assertions use only these owned image directories.
// Image cloning and Btrfs-specific assertions remain in the retained reference.
func imageGraphFixture(t *testing.T) (string, string, string) {
	t.Helper()
	if os.Getenv("FACTORYBOX_NATIVE") != "1" {
		t.Skip("requires the private native Linux test fixture")
	}
	root, err := os.MkdirTemp("/experiment", "factorybox-graph-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(root); err != nil {
			t.Error(err)
		}
	})
	source := filepath.Join(root, "source")
	head := filepath.Join(source, "images")
	if err := os.MkdirAll(head, 0700); err != nil {
		t.Fatal(err)
	}
	return source, head, filepath.Join(root, "copy")
}
