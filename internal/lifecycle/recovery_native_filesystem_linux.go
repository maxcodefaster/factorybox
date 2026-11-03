//go:build linux

package lifecycle

import (
	"fmt"
	"path/filepath"

	"factorybox/internal/storefs"
	"factorybox/internal/sysboxcapture"
	"golang.org/x/sys/unix"
)

// The caller has separately verified the original epoch and current attachment.
// Native subvolumes have virtual device numbers, unlike the canonical lower.
// Do not compare them to the loop filesystem's st_dev or adopt foreign mounts.
func (e *Engine) verifyNativeRetainedStorage(old storefs.Active, tree sysboxcapture.FilesystemTree, device uint64) error {
	current := e.config.BoundedStore
	native := tree.Native
	if current == nil || old.Image.Schema != 2 || current.Image.Schema != 2 || old.Image != current.Image ||
		native == nil || native.Saved.FilesystemUUID != old.Image.UUID ||
		native.Saved.MountID != old.Mount.MountID || native.Saved.Flags != 1 ||
		!withinStore(e.config.Root, tree.Saved) {
		return fmt.Errorf("native retained tree differs from its original epoch and fixed store")
	}
	path := tree.Saved
	if native.Member == "upper" {
		path = filepath.Dir(path)
	} else if native.Member != "" {
		return fmt.Errorf("native retained tree has an unrecognized member")
	}
	var actual unix.Stat_t
	if err := unix.Lstat(path, &actual); err != nil {
		return err
	}
	if uint64(actual.Dev) != device {
		return fmt.Errorf("native retained translation differs from its actual root device")
	}
	return storefs.VerifyObjectBacking(*current, path)
}
