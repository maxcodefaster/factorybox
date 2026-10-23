//go:build linux

package sysboxcapture

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// VerifyRetainedFilesystemBase checks the original inode, complete metadata and
// content after the original worker's read-only mount has disappeared. It does
// not copy data, create a mount, confer immutable runtime authority or authorize
// deletion. Device translation needs the same independent store proof as copy
// recovery. The caller must separately establish ownership and dependencies.
func VerifyRetainedFilesystemBase(ctx context.Context, saved FilesystemTree, verify BaseRecoveryVerifier) error {
	root, err := openRetainedFilesystemBase(ctx, saved, verify)
	if err != nil {
		return err
	}
	defer root.Close()
	return fingerprintRetainedTree(ctx, root, saved)
}

func fingerprintRetainedTree(ctx context.Context, root *os.File, expected FilesystemTree) error {
	actual, err := fingerprintTree(ctx, root, nil, false)
	if err != nil {
		return err
	}
	if !sameFilesystemTree(actual, expected) {
		return fmt.Errorf("retained filesystem tree changed: %s", expected.Name)
	}
	return nil
}

// A foreign self-bind is a different mount even when it exposes identical
// bytes. Vanished mount IDs never establish the identity of a successor.
func retainedDirectoryMount(root *os.File, path string) error {
	parent, err := openDirectory(filepath.Dir(path))
	if err != nil {
		return err
	}
	var actual, enclosing unix.Statx_t
	err = unix.Statx(int(root.Fd()), "", unix.AT_EMPTY_PATH, unix.STATX_MNT_ID, &actual)
	if err == nil {
		err = unix.Statx(int(parent.Fd()), "", unix.AT_EMPTY_PATH, unix.STATX_MNT_ID, &enclosing)
	}
	if err := errors.Join(err, parent.Close()); err != nil {
		return err
	}
	if actual.Mask&unix.STATX_MNT_ID == 0 || enclosing.Mask&unix.STATX_MNT_ID == 0 ||
		actual.Mnt_id == 0 || actual.Mnt_id != enclosing.Mnt_id {
		return fmt.Errorf("retained filesystem has an unexpected covering mount: %s", path)
	}
	return nil
}

func verifyRetainedAlias(ctx context.Context, tree FilesystemTree, verify BaseRecoveryVerifier) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tree.Immutable == nil || tree.Alias == nil || tree.Saved == tree.Immutable.Root {
		return fmt.Errorf("retained snapshot has no original immutable alias mountpoint")
	}
	root, err := openDirectory(tree.Saved)
	if err != nil {
		return err
	}
	defer root.Close()
	var stat unix.Stat_t
	if err := unix.Fstat(int(root.Fd()), &stat); err != nil {
		return err
	}
	if stat.Ino != tree.Alias.Inode || stat.Uid != 0 || stat.Mode&0777 != 0700 {
		return fmt.Errorf("retained immutable alias original inode or ownership changed")
	}
	if err := retainedDirectoryMount(root, tree.Saved); err != nil {
		return err
	}
	if uint64(stat.Dev) != tree.Alias.Device {
		if verify == nil {
			return fmt.Errorf("retained immutable alias device changed without store authority")
		}
		if err := verify(tree, uint64(stat.Dev)); err != nil {
			return fmt.Errorf("retained immutable alias device translation refused: %w", err)
		}
	}
	entries, err := root.Readdirnames(1)
	if len(entries) != 0 {
		return fmt.Errorf("retained immutable alias original mountpoint is not empty")
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

// VerifyRetainedFilesystemSnapshot reads a complete saved generation after its
// worker mounts are gone. Immutable lower data are checked at their original
// canonical base, and the uncovered alias must be the exact empty original
// directory. A mounted live snapshot must use VerifyFilesystemSnapshot instead.
// No success here permits unlinking without a separate owned deletion plan.
func VerifyRetainedFilesystemSnapshot(ctx context.Context, generation string, snapshot *FilesystemSnapshot, verify BaseRecoveryVerifier) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateFilesystemSnapshotPaths(generation, snapshot); err != nil {
		return err
	}
	for _, tree := range snapshot.Roots {
		if tree.Immutable != nil {
			if err := verifyRetainedAlias(ctx, tree, verify); err != nil {
				return err
			}
			if err := VerifyRetainedFilesystemBase(ctx, tree, verify); err != nil {
				return err
			}
			continue
		}
		root, err := openDirectory(tree.Saved)
		if err != nil {
			return err
		}
		readErr := retainedDirectoryMount(root, tree.Saved)
		if readErr == nil {
			readErr = fingerprintRetainedTree(ctx, root, tree)
		}
		if err := errors.Join(readErr, root.Close()); err != nil {
			return err
		}
	}
	return nil
}
