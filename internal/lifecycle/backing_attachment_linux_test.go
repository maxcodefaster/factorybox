//go:build linux

package lifecycle

import (
	"os"
	"strings"
	"testing"

	"factorybox/internal/storefs"
)

func TestBackingRetirementAttachmentPreservesPersistentIdentity(t *testing.T) {
	active := storefs.Active{
		Image: storefs.Image{Schema: 2, Owner: strings.Repeat("a", 32), UUID: strings.Repeat("a", 32),
			Path: "/fixed/store.img", Device: 31, Inode: 1234, Bytes: 512 << 20},
		Target: "/fixed/data", DirectIO: true, FilesystemMax: 500 << 20, LoopNumber: 1,
		BlockDevice: 12345, Mount: storefs.Object{Device: 12345, Inode: 256, MountID: 20},
	}
	previous := active
	previous.BlockDevice, previous.Mount.Device, previous.Mount.MountID = 23456, 23456, 40
	engine := &Engine{config: Config{BoundedStore: &active}, retainedStorage: &previous, metadataOnly: true}
	if actual, err := engine.backingRetirementAttachment(nil); err != nil || actual != previous {
		t.Fatal("retained cleanup did not preserve the original attachment", err)
	}
	saved := KernelObject{Device: previous.BlockDevice, Inode: 2000, MountID: previous.Mount.MountID}
	current := KernelObject{Device: active.BlockDevice, Inode: saved.Inode, MountID: active.Mount.MountID}
	if !engine.backingDirectoryMatches(saved, current, nil) {
		t.Fatal("same-image attachment translation was refused")
	}
	for name, change := range map[string]func(*storefs.Active){
		"image":       func(a *storefs.Active) { a.Image.Inode++ },
		"owner":       func(a *storefs.Active) { a.Image.Owner = strings.Repeat("b", 32) },
		"uuid":        func(a *storefs.Active) { a.Image.UUID = strings.Repeat("b", 32) },
		"image-path":  func(a *storefs.Active) { a.Image.Path += "-replacement" },
		"target":      func(a *storefs.Active) { a.Target += "-replacement" },
		"capacity":    func(a *storefs.Active) { a.FilesystemMax-- },
		"direct-io":   func(a *storefs.Active) { a.DirectIO = false },
		"mount-root":  func(a *storefs.Active) { a.Mount.Inode++ },
		"device":      func(a *storefs.Active) { a.Mount.Device++ },
		"zero-mount":  func(a *storefs.Active) { a.Mount.MountID = 0 },
		"zero-device": func(a *storefs.Active) { a.BlockDevice = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			changed := previous
			change(&changed)
			if _, err := engine.backingRetirementAttachment(&changed); err == nil ||
				engine.backingDirectoryMatches(saved, current, &changed) {
				t.Fatal("changed persistent ownership admitted retirement")
			}
		})
	}
	current.Inode++
	if engine.backingDirectoryMatches(saved, current, nil) {
		t.Fatal("replaced directory admitted by attachment translation")
	}
	ordinary := &Engine{config: Config{BoundedStore: &active}}
	current.Inode--
	if ordinary.backingDirectoryMatches(saved, current, nil) {
		t.Fatal("ordinary current execution silently adopted another attachment")
	}
	if engine.config.BoundedStore != &active || *engine.retainedStorage != previous {
		t.Fatal("identity verification changed retained evidence")
	}
}

func TestManagerJournalRemountRequiresItsBackingParent(t *testing.T) {
	directory := t.TempDir()
	parent, err := directoryIdentity(directory)
	if err != nil {
		t.Fatal(err)
	}
	childPath := directory + "/journal"
	if err := os.Mkdir(childPath, 0700); err != nil {
		t.Fatal(err)
	}
	current, err := directoryIdentity(childPath)
	if err != nil {
		t.Fatal(err)
	}
	active := storefs.Active{Image: storefs.Image{Path: directory + "/store.img", Device: parent.Device}}
	engine := &Engine{metadataOnly: true, config: Config{BoundedStore: &active}}
	previous, original := parent, current
	previous.MountID++
	original.MountID = previous.MountID
	if !engine.managerJournalDirectoryMatches(original, current, &previous) {
		t.Fatal("verified external journal parent could not change mount ID")
	}
	for name, change := range map[string]func(*KernelObject){
		"parent-inode":  func(p *KernelObject) { p.Inode++ },
		"parent-device": func(p *KernelObject) { p.Device++ },
		"parent-mount":  func(p *KernelObject) { p.MountID++ },
	} {
		t.Run(name, func(t *testing.T) {
			changed := previous
			change(&changed)
			if engine.managerJournalDirectoryMatches(original, current, &changed) {
				t.Fatal("foreign external journal parent was admitted")
			}
		})
	}
	if engine.managerJournalDirectoryMatches(original, current, nil) {
		t.Fatal("legacy journal without remount evidence was retargeted")
	}
	engine.metadataOnly = false
	if engine.managerJournalDirectoryMatches(original, current, &previous) {
		t.Fatal("ordinary execution adopted another journal mount")
	}
	engine.metadataOnly = true
	current.Inode++
	if engine.managerJournalDirectoryMatches(original, current, &previous) {
		t.Fatal("replacement journal was admitted")
	}
}
