//go:build linux

package lifecycle

import (
	"strings"
	"testing"

	"factorybox/internal/storefs"
)

func TestManagerWithdrawalRequiresRetainedOriginalAttachment(t *testing.T) {
	active := storefs.Active{
		Image: storefs.Image{Schema: 2, Owner: strings.Repeat("a", 32), UUID: strings.Repeat("a", 32),
			Path: "/fixed/store.img", Device: 31, Inode: 1234, Bytes: 32 << 30},
		Target: "/fixed/data", DirectIO: true, FilesystemMax: 31 << 30,
		LoopNumber: 1, BlockDevice: 71, Mount: storefs.Object{Device: 71, Inode: 256, MountID: 20},
	}
	engine := &Engine{metadataOnly: true, config: Config{BoundedStore: &active}}
	intent := managerBackingIntent{
		Manager: KernelObject{Device: 71, Inode: 1000, MountID: 20}, Attachment: &active,
	}
	if previous, err := engine.originalManagerAttachment(intent); err != nil || previous != active {
		t.Fatal("exact retained attachment was rejected", err)
	}
	for name, change := range map[string]func(*Engine, *managerBackingIntent){
		"live-controller":    func(e *Engine, _ *managerBackingIntent) { e.metadataOnly = false },
		"missing-attachment": func(_ *Engine, i *managerBackingIntent) { i.Attachment = nil },
		"foreign-image":      func(_ *Engine, i *managerBackingIntent) { i.Attachment.Image.Inode++ },
		"wrong-device":       func(_ *Engine, i *managerBackingIntent) { i.Manager.Device++ },
		"wrong-mount":        func(_ *Engine, i *managerBackingIntent) { i.Manager.MountID++ },
		"filesystem-root":    func(_ *Engine, i *managerBackingIntent) { i.Manager.Inode = 256 },
		"missing-inode":      func(_ *Engine, i *managerBackingIntent) { i.Manager.Inode = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			other, saved := *engine, intent
			attachment := active
			saved.Attachment = &attachment
			change(&other, &saved)
			if _, err := other.originalManagerAttachment(saved); err == nil {
				t.Fatal("unproved original directory acquired withdrawal authority")
			}
		})
	}
}
