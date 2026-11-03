//go:build linux

package lifecycle

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"factorybox/internal/sysboxcapture"
)

func TestEpochAliasReceiptIsBoundToPublishedLower(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-owned metadata; exercised in the native Linux regression lane")
	}
	path := t.TempDir()
	if err := os.Mkdir(path+"/generation", 0700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	lower := sysboxcapture.FilesystemTree{Name: "lower", Source: path + "/source", Saved: path + "/generation/filesystem/lower",
		SHA256: strings.Repeat("a", 64), Entries: 2, FileBytes: 4, HashedBytes: 4,
		Alias:     &sysboxcapture.ImmutableAlias{Device: 1, Inode: 2, MountID: 3},
		Immutable: &sysboxcapture.ImmutableTree{Root: path + "/base", Device: 1, Inode: 4, MountID: 3}}
	snapshot := &sysboxcapture.FilesystemSnapshot{Version: 2, Roots: []sysboxcapture.FilesystemTree{lower}}
	builder := epochInventoryBuilder{engine: &Engine{root: root, config: Config{Root: path}}, allowed: map[string]uint32{}}
	if err := builder.captureAlias("generation", snapshot); err != nil {
		t.Fatal("historical lower without optional receipt was refused", err)
	}
	receipt := lower
	receipt.CaptureUS = 123 // Pre-mount timing is not the returned alias timing.
	write := func(tree sysboxcapture.FilesystemTree) {
		t.Helper()
		data, err := json.Marshal(tree)
		if err != nil {
			t.Fatal(err)
		}
		if err := root.WriteFile("generation/"+sysboxcapture.CaptureLowerAliasIntent, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(receipt)
	if err := builder.captureAlias("generation", snapshot); err != nil {
		t.Fatal("exact published alias receipt was not admitted", err)
	}
	for _, field := range []string{"source", "saved", "alias", "base", "native", "hash"} {
		changed := receipt
		switch field {
		case "source":
			changed.Source += "-foreign"
		case "saved":
			changed.Saved += "-foreign"
		case "alias":
			alias := *changed.Alias
			alias.Inode++
			changed.Alias = &alias
		case "base":
			base := *changed.Immutable
			base.Root += "-foreign"
			changed.Immutable = &base
		case "native":
			changed.Native = &sysboxcapture.NativeFilesystemTree{}
		case "hash":
			changed.SHA256 = strings.Repeat("b", 64)
		}
		write(changed)
		if err := builder.captureAlias("generation", snapshot); err == nil {
			t.Fatal("changed alias receipt admitted", field)
		}
	}
}
