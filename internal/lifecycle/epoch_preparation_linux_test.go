//go:build linux

package lifecycle

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"factorybox/internal/ownedtree"
	"golang.org/x/sys/unix"
)

func epochPreparationTestPlan() ownedtree.Plan {
	root := ownedtree.Object{Device: 7, Inode: 100, MountID: 44, Mode: unix.S_IFDIR | 0700}
	directory := root
	directory.Inode++
	file := root
	file.Inode, file.Mode = 103, unix.S_IFREG|0600
	return ownedtree.Plan{Version: 1, Root: root, Paths: []string{epochRetirementDirectory}, Entries: []ownedtree.Entry{
		{Path: epochRetirementDirectory, Object: directory},
		{Path: epochRetirementDirectory + "/retirement-payload.json.gz", Object: file, Links: 1, Bytes: 32, Content: strings.Repeat("a", 64)},
	}}
}

func TestEpochPreparationCancellationCannotAuthorizePayloadsOrForeignMetadata(t *testing.T) {
	base := epochPreparationTestPlan()
	if err := validateEpochPreparationPlan(base); err != nil {
		t.Fatal("legitimate incomplete preparation refused", err)
	}
	cases := map[string]func(*ownedtree.Plan){
		"whole-controller":  func(p *ownedtree.Plan) { p.Paths[0] = "." },
		"extra-root":        func(p *ownedtree.Plan) { p.Paths = append(p.Paths, "objects") },
		"missing-root":      func(p *ownedtree.Plan) { p.Entries = p.Entries[1:] },
		"unknown":           func(p *ownedtree.Plan) { p.Entries[1].Path = epochRetirementDirectory + "/foreign" },
		"payload":           func(p *ownedtree.Plan) { p.Entries[1].Path = "objects/source/images" },
		"acceptance":        func(p *ownedtree.Plan) { p.Entries[1].Path = epochRetirementDirectory + "/intent.json" },
		"completion":        func(p *ownedtree.Plan) { p.Entries[1].Path = epochRetirementDirectory + "/complete.json" },
		"witness-foreign":   func(p *ownedtree.Plan) { p.Entries[1].Path = epochRetirementDirectory + "/witness/foreign" },
		"bad-temporary":     func(p *ownedtree.Plan) { p.Entries[1].Path = epochRetirementDirectory + "/.checkpoint-bad" },
		"symlink":           func(p *ownedtree.Plan) { p.Entries[1].Object.Mode = unix.S_IFLNK | 0600 },
		"public-mode":       func(p *ownedtree.Plan) { p.Entries[1].Object.Mode |= 0044 },
		"foreign-owner":     func(p *ownedtree.Plan) { p.Entries[1].Object.UID = 1000 },
		"foreign-group":     func(p *ownedtree.Plan) { p.Entries[1].Object.GID = 1000 },
		"hardlink":          func(p *ownedtree.Plan) { p.Entries[1].Links = 2 },
		"device":            func(p *ownedtree.Plan) { p.Entries[1].Object.Device++ },
		"mount":             func(p *ownedtree.Plan) { p.Entries[1].Object.MountID++ },
		"oversize":          func(p *ownedtree.Plan) { p.Entries[1].Bytes = retirementEncodedLimit + 1 },
		"missing-hash":      func(p *ownedtree.Plan) { p.Entries[1].Content = "" },
		"directory-payload": func(p *ownedtree.Plan) { p.Entries[0].Bytes = 1 },
		"duplicate":         func(p *ownedtree.Plan) { p.Entries = append(p.Entries, p.Entries[1]) },
		"anchor":            func(p *ownedtree.Plan) { p.Anchors = append(p.Anchors, p.Entries[0]) },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			data, _ := json.Marshal(base)
			var plan ownedtree.Plan
			if err := json.Unmarshal(data, &plan); err != nil {
				t.Fatal(err)
			}
			change(&plan)
			if err := validateEpochPreparationPlan(plan); err == nil {
				t.Fatal("unsafe cancellation inventory accepted")
			}
		})
	}
}

func TestEpochPreparationCancellationRefusesEveryAcceptanceShape(t *testing.T) {
	for _, name := range []string{"intent.json", "complete.json", "store-fence"} {
		for _, shape := range []string{"regular", "directory", "dangling-symlink"} {
			t.Run(name+"/"+shape, func(t *testing.T) {
				path := t.TempDir()
				if err := os.Mkdir(path+"/"+epochRetirementDirectory, 0700); err != nil {
					t.Fatal(err)
				}
				root, err := os.OpenRoot(path)
				if err != nil {
					t.Fatal(err)
				}
				defer root.Close()
				source, store := &Engine{root: root}, &Engine{root: root}
				if err := epochPreparationUnaccepted(source, store); err != nil {
					t.Fatal(err)
				}
				target := path + "/" + epochRetirementDirectory + "/" + name
				if name == "store-fence" {
					target = path + "/" + epochRetirementFenceName
				}
				switch shape {
				case "regular":
					err = os.WriteFile(target, []byte("{partial"), 0600)
				case "directory":
					err = os.Mkdir(target, 0700)
				case "dangling-symlink":
					err = os.Symlink("missing", target)
				}
				if err != nil {
					t.Fatal(err)
				}
				if err := epochPreparationUnaccepted(source, store); err == nil {
					t.Fatal("accepted or uncertain state allowed cancellation")
				}
			})
		}
	}
}

func TestEpochPreparationChangedInspectionMustBeRefreshed(t *testing.T) {
	path := t.TempDir()
	if err := os.Mkdir(path+"/"+epochRetirementDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	file := path + "/" + epochRetirementDirectory + "/retirement-payload.json.gz"
	if err := os.WriteFile(file, []byte("partial encoded metadata"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	source := &Engine{root: root}
	birth, change, err := epochPreparationTimes(source)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := ownedtree.Capture(context.Background(), root, []string{epochRetirementDirectory})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	newBirth, newChange, err := epochPreparationTimes(source)
	if err != nil || newBirth != birth || newChange == change {
		t.Fatal("partial cancellation did not invalidate the directory observation", err)
	}
	if err := ownedtree.ValidateComplete(context.Background(), root, plan); err == nil {
		t.Fatal("stale inspection authorized missing original metadata")
	}
	// An interrupted cancellation can be inspected again. The remaining exact
	// tree is a new authorization, never a blind replay of the previous plan.
	fresh, err := ownedtree.Capture(context.Background(), root, []string{epochRetirementDirectory})
	if err != nil {
		t.Fatal(err)
	}
	if err := ownedtree.ValidateComplete(context.Background(), root, fresh); err != nil {
		t.Fatal(err)
	}
}
