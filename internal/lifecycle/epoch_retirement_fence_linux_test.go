//go:build linux

package lifecycle

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"factorybox/internal/ownedtree"
	"factorybox/internal/storefs"
	"golang.org/x/sys/unix"
)

func TestEpochRetirementFenceCannotBeBypassedByExplicitRoot(t *testing.T) {
	for _, kind := range []string{"valid-json", "malformed-json", "directory", "dangling-symlink"} {
		t.Run(kind, func(t *testing.T) {
			store := t.TempDir()
			rootPath := store + "/epochs/" + strings.Repeat("a", 32) + "/controller"
			if err := os.MkdirAll(rootPath, 0700); err != nil {
				t.Fatal(err)
			}
			root, err := os.OpenRoot(rootPath)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			// This explicit controller has no bounded-store options at all.
			engine := &Engine{root: root, config: Config{Root: rootPath}}
			if err := engine.epochRetirementFence(); err != nil {
				t.Fatal("unfenced controller refused", err)
			}
			path := store + "/" + epochRetirementFenceName
			switch kind {
			case "valid-json":
				err = os.WriteFile(path, []byte("{}\n"), 0600)
			case "malformed-json":
				err = os.WriteFile(path, []byte("{unfinished"), 0600)
			case "directory":
				err = os.Mkdir(path, 0700)
			case "dangling-symlink":
				err = os.Symlink("missing", path)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := engine.epochRetirementFence(); err == nil || !strings.Contains(err.Error(), "epoch retirement") {
				t.Fatal("explicit options bypassed the store fence", err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(rootPath+"/"+epochRetirementDirectory, 0700); err != nil {
				t.Fatal(err)
			}
			if err := engine.epochRetirementFence(); err == nil {
				t.Fatal("source tombstone stopped fencing after the global fence disappeared")
			}
		})
	}
}

func TestEpochRetirementPlanTranslationPreservesOriginalEvidence(t *testing.T) {
	previous := storefs.Active{Image: storefs.Image{Owner: strings.Repeat("a", 32), Inode: 91},
		Target: "/fixed/data", DirectIO: true, FilesystemMax: 1 << 30,
		LoopNumber: 0, BlockDevice: uint64(unix.Mkdev(7, 0)), Mount: storefs.Object{Device: uint64(unix.Mkdev(7, 0)), Inode: 2, MountID: 100}}
	current := previous
	current.LoopNumber, current.BlockDevice = 1, uint64(unix.Mkdev(7, 1))
	current.Mount.Device, current.Mount.MountID = current.BlockDevice, 200
	root := ownedtree.Object{Device: previous.BlockDevice, MountID: previous.Mount.MountID, Inode: 12, Mode: unix.S_IFDIR | 0700}
	file := root
	file.Inode, file.Mode = 13, unix.S_IFREG|0600
	plan := ownedtree.Plan{Version: 1, Root: root, Paths: []string{"payload/file"},
		Anchors: []ownedtree.Entry{{Path: "payload", Object: root}},
		Entries: []ownedtree.Entry{{Path: "payload/file", Object: file, Content: strings.Repeat("f", 64), Bytes: 4096, Links: 1, Rdev: 17}}}
	original, _ := json.Marshal(plan)
	translated, err := rebindEpochRetirementPlan(plan, previous, current)
	if err != nil {
		t.Fatal(err)
	}
	if translated.Root.Inode != plan.Root.Inode || translated.Root.Device != current.BlockDevice || translated.Root.MountID != current.Mount.MountID ||
		translated.Entries[0].Content != plan.Entries[0].Content || translated.Entries[0].Rdev != plan.Entries[0].Rdev ||
		translated.Entries[0].Object.Inode != file.Inode || translated.Entries[0].Object.Mode != file.Mode {
		t.Fatal("translation changed persistent ownership or content", translated)
	}
	translated.Paths[0] = "other"
	translated.Anchors[0].Object.Inode++
	translated.Entries[0].Bytes++
	after, _ := json.Marshal(plan)
	if string(original) != string(after) {
		t.Fatal("translation rewrote original evidence")
	}
	for name, change := range map[string]func(*storefs.Active){
		"image-owner":  func(a *storefs.Active) { a.Image.Owner = strings.Repeat("b", 32) },
		"image-inode":  func(a *storefs.Active) { a.Image.Inode++ },
		"target":       func(a *storefs.Active) { a.Target += "-foreign" },
		"direct-io":    func(a *storefs.Active) { a.DirectIO = false },
		"capacity":     func(a *storefs.Active) { a.FilesystemMax++ },
		"mount-inode":  func(a *storefs.Active) { a.Mount.Inode++ },
		"loop-device":  func(a *storefs.Active) { a.BlockDevice++ },
		"mount-device": func(a *storefs.Active) { a.Mount.Device++ },
		"mount-id":     func(a *storefs.Active) { a.Mount.MountID = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			wrong := current
			change(&wrong)
			if _, err := rebindEpochRetirementPlan(plan, previous, wrong); err == nil {
				t.Fatal("foreign attachment accepted")
			}
		})
	}
	for _, position := range []string{"root", "anchor", "payload"} {
		t.Run("foreign-"+position, func(t *testing.T) {
			var wrong ownedtree.Plan
			if err := json.Unmarshal(original, &wrong); err != nil {
				t.Fatal(err)
			}
			switch position {
			case "root":
				wrong.Root.MountID++
			case "anchor":
				wrong.Anchors[0].Object.Device++
			case "payload":
				wrong.Entries[0].Object.MountID++
			}
			if _, err := rebindEpochRetirementPlan(wrong, previous, current); err == nil {
				t.Fatal("foreign object translated")
			}
		})
	}
	if !reflect.DeepEqual(plan.Paths, []string{"payload/file"}) || epochStoreRoot(filepath.Clean("/fixed/data/store/epochs/"+strings.Repeat("a", 32)+"/controller")) != "/fixed/data/store" {
		t.Fatal("path evidence changed")
	}
}
