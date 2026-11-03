//go:build linux

package sysboxcapture

import (
	"context"
	"errors"
	"fmt"
	"os"
)

// ObserveRetainedNativeFilesystemRetirement binds a cold owned root to this
// namespace. The caller must first verify closed runtime ownership and exclude
// live dependencies. A working descendant may have advanced since preparation;
// its UUID, lineage and creation identity must still match. Sealed data may not
// have changed. This observation precedes the caller's durable deletion intent.
func ObserveRetainedNativeFilesystemRetirement(ctx context.Context, tree FilesystemTree, verify BaseRecoveryVerifier) (FilesystemTree, error) {
	return bindNativeFilesystemRetirement(ctx, tree, verify, true)
}

// RebindNativeFilesystemRetirement translates only namespace-local numbers in
// an accepted retirement. It cannot adopt later writes, even to a working root.
func RebindNativeFilesystemRetirement(ctx context.Context, tree FilesystemTree, verify BaseRecoveryVerifier) (FilesystemTree, error) {
	return bindNativeFilesystemRetirement(ctx, tree, verify, false)
}

func bindNativeFilesystemRetirement(ctx context.Context, tree FilesystemTree, verify BaseRecoveryVerifier, observeWorking bool) (FilesystemTree, error) {
	if tree.Native == nil {
		return tree, fmt.Errorf("native retirement lacks its original storage proof")
	}
	writable := tree.Native.Saved.Flags == 0
	if err := validateNativeFilesystemTree(tree, writable); err != nil {
		return tree, err
	}
	path, err := nativeTreeRoot(tree.Saved, tree.Native.Member)
	if err != nil {
		return tree, err
	}
	root, current, err := openNativeFilesystemRoot(ctx, path)
	if err != nil {
		return tree, err
	}
	defer root.Close()
	expected := tree.Native.Saved
	translated := expected.Device != current.Device || expected.MountID != current.MountID
	if verify != nil {
		if err := verify(tree, current.Device); err != nil {
			return tree, err
		}
	} else if translated {
		return tree, fmt.Errorf("native retirement translation lacks original store authority")
	}
	expected.Device, expected.MountID = current.Device, current.MountID
	if writable && observeWorking {
		if current.Changed < expected.Changed {
			return tree, fmt.Errorf("cold native working root content transaction moved backwards")
		}
		expected.Changed = current.Changed
	}
	if !sameNativeRoot(current, expected) {
		return tree, fmt.Errorf("native retirement root identity, lineage or content changed")
	}
	if err := verifyNativeMembers(root, tree.Native.Member, current); err != nil {
		return tree, err
	}
	native := *tree.Native
	native.Saved = current
	tree.Native = &native
	return tree, nil
}

// These cold-root commands require the caller's durable exact-object retirement
// and excluded live dependencies. They do not broaden sealed-checkpoint APIs.
func CheckColdNativeFilesystemRetirement(ctx context.Context, tree FilesystemTree) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tree.Native == nil {
		return fmt.Errorf("cold native retirement lacks its storage proof")
	}
	if err := validateNativeFilesystemTree(tree, tree.Native.Saved.Flags == 0); err != nil {
		return err
	}
	path, err := nativeTreeRoot(tree.Saved, tree.Native.Member)
	if err != nil {
		return err
	}
	parent, err := subvolumeRetirementParentFlags(path, tree.Native.Saved, tree.Native.Saved.Flags)
	if err != nil {
		return err
	}
	if err := parent.Close(); err != nil {
		return err
	}
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	return verifyNativeFilesystemRoot(ctx, path, tree.Native.Saved)
}

func RetireColdNativeFilesystemTree(ctx context.Context, tree FilesystemTree) error {
	if err := CheckColdNativeFilesystemRetirement(ctx, tree); err != nil {
		return err
	}
	path, _ := nativeTreeRoot(tree.Saved, tree.Native.Member)
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	parent, err := subvolumeRetirementParentFlags(path, tree.Native.Saved, tree.Native.Saved.Flags)
	if err != nil {
		return err
	}
	defer parent.Close()
	// Recheck after opening the independently bound parent, before the ioctl.
	if err := verifyNativeFilesystemRoot(ctx, path, tree.Native.Saved); err != nil {
		return err
	}
	return destroySubvolume(ctx, parent, path, tree.Native.Saved.ID)
}

func WaitColdNativeFilesystemRetirement(ctx context.Context, tree FilesystemTree) error {
	if tree.Native == nil {
		return fmt.Errorf("cold native retirement lacks its storage proof")
	}
	if err := validateNativeFilesystemTree(tree, tree.Native.Saved.Flags == 0); err != nil {
		return err
	}
	path, err := nativeTreeRoot(tree.Saved, tree.Native.Member)
	if err != nil {
		return err
	}
	return waitSubvolumeRetirementFlags(ctx, path, tree.Native.Saved, tree.Native.Saved.Flags)
}
