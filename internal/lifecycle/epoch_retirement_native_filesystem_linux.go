//go:build linux

package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"factorybox/internal/storefs"
	"factorybox/internal/sysboxcapture"
)

func epochNativeRoot(tree sysboxcapture.FilesystemTree) string {
	if tree.Native != nil && tree.Native.Member == "upper" {
		return filepath.Dir(tree.Saved)
	}
	return tree.Saved
}

func epochNativePaths(root string, trees []sysboxcapture.FilesystemTree) ([]string, error) {
	var paths []string
	seen := map[string]bool{}
	for _, tree := range trees {
		path := epochNativeRoot(tree)
		if tree.Native == nil || !withinStore(root, path) {
			return nil, fmt.Errorf("native epoch root escapes its exact controller")
		}
		name := strings.TrimPrefix(path, root+"/")
		if seen[name] {
			return nil, fmt.Errorf("native epoch roots are repeated")
		}
		seen[name] = true
		paths = append(paths, name)
	}
	for name := range seen {
		for parent := filepath.Dir(name); parent != "."; parent = filepath.Dir(parent) {
			if seen[parent] {
				return nil, fmt.Errorf("native epoch roots overlap")
			}
		}
	}
	sort.Strings(paths)
	return paths, nil
}

func (b *epochInventoryBuilder) nativePayload(tree sysboxcapture.FilesystemTree) error {
	current, err := sysboxcapture.ObserveRetainedNativeFilesystemRetirement(b.ctx, tree, b.translate)
	if err != nil {
		return err
	}
	if err := b.payload(epochNativeRoot(current)); err != nil {
		return err
	}
	b.native = append(b.native, current)
	return nil
}

// An accepted native retirement keeps persistent UUID/lineage/content identity.
// Only its independently verified same-store attachment may translate local
// mount/device numbers. Absent names are allowed only after acceptance.
func (e *Engine) bindEpochNativeRetirement(ctx context.Context, source *Engine, prepared epochRetirementPreparation, missing bool) ([]sysboxcapture.FilesystemTree, error) {
	if _, err := epochNativePaths(source.config.Root, prepared.Native); err != nil {
		return nil, err
	}
	if len(prepared.Native) != 0 {
		if e.config.BoundedStore == nil {
			return nil, fmt.Errorf("native retirement has no current bounded attachment")
		}
		if err := storefs.Verify(*e.config.BoundedStore); err != nil {
			return nil, err
		}
	}
	var result []sysboxcapture.FilesystemTree
	for _, tree := range prepared.Native {
		if e.config.BoundedStore == nil || prepared.Attachment.Image != e.config.BoundedStore.Image ||
			tree.Native.Saved.FilesystemUUID != prepared.Attachment.Image.UUID ||
			tree.Native.Saved.MountID != prepared.Attachment.Mount.MountID {
			return nil, fmt.Errorf("native retirement differs from its accepted original attachment")
		}
		path := epochNativeRoot(tree)
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) && missing {
			native := *tree.Native
			native.Saved.MountID = e.config.BoundedStore.Mount.MountID
			tree.Native = &native
		} else if err != nil {
			return nil, err
		} else {
			current, err := sysboxcapture.RebindNativeFilesystemRetirement(ctx, tree, func(original sysboxcapture.FilesystemTree, device uint64) error {
				return source.verifyNativeRetainedStorage(prepared.Attachment, original, device)
			})
			if err != nil {
				return nil, err
			}
			tree = current
		}
		if err := sysboxcapture.CheckColdNativeFilesystemRetirement(ctx, tree); err != nil {
			return nil, err
		}
		result = append(result, tree)
	}
	return result, nil
}

func (e *Engine) epochNativeAbsent(ctx context.Context, source *Engine, prepared epochRetirementPreparation) error {
	trees, err := e.bindEpochNativeRetirement(ctx, source, prepared, true)
	if err != nil {
		return err
	}
	for _, tree := range trees {
		if _, err := os.Lstat(epochNativeRoot(tree)); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("retired epoch retains native backing or a successor: %v", err)
		}
		if err := sysboxcapture.WaitColdNativeFilesystemRetirement(ctx, tree); err != nil {
			return err
		}
	}
	return nil
}
