//go:build linux

package sysboxcapture

import (
	"context"
	"errors"
	"os"
)

func nativeFilesystemRetirementPath(tree FilesystemTree) (string, error) {
	if err := validateNativeFilesystemTree(tree, false); err != nil {
		return "", err
	}
	return nativeTreeRoot(tree.Saved, tree.Native.Member)
}

// CheckNativeFilesystemRetirement verifies a sealed root or its already absent
// name under the original filesystem and mount. The caller must independently
// own a durable retirement intent before accepting absence as a retry.
func CheckNativeFilesystemRetirement(ctx context.Context, tree FilesystemTree) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := nativeFilesystemRetirementPath(tree)
	if err != nil {
		return err
	}
	parent, err := subvolumeRetirementParent(path, tree.Native.Saved)
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

// RetireNativeFilesystemTree withdraws only the exact sealed kernel root.
// Dependency admission, durable intent and exclusive lifecycle ownership belong
// to the caller. It never makes a snapshot writable or traverses its contents.
func RetireNativeFilesystemTree(ctx context.Context, tree FilesystemTree) error {
	if err := CheckNativeFilesystemRetirement(ctx, tree); err != nil {
		return err
	}
	path, err := nativeFilesystemRetirementPath(tree)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	return retireSubvolume(ctx, path, tree.Native.Saved)
}

// WaitNativeFilesystemRetirement confirms cleaning of the original kernel ID.
// Withdraw all roots first so their cleaner work can proceed together. Actual
// free capacity must still be observed separately from root-item removal.
func WaitNativeFilesystemRetirement(ctx context.Context, tree FilesystemTree) error {
	path, err := nativeFilesystemRetirementPath(tree)
	if err != nil {
		return err
	}
	return waitSubvolumeRetirement(ctx, path, tree.Native.Saved)
}
