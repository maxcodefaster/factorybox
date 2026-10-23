//go:build linux

package sysboxcapture

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// RecoverFilesystemSnapshot imports file backing from a stopped, absent worker.
// Its caller must exclusively own the persistent store. Old mount IDs and
// aliases cannot establish immutable authority; only the original canonical
// base inode and a complete verified new copy can establish a fresh base.
// Mutable roots are always copied independently. A partial destination remains
// retained and cannot be overwritten by retrying this function.
func RecoverFilesystemSnapshot(ctx context.Context, generation, destination string, saved *FilesystemSnapshot) (*FilesystemSnapshot, error) {
	return recoverFilesystemSnapshot(ctx, generation, destination, saved, nil)
}

// RecoverFilesystemSnapshotWithBaseVerifier keeps the strict default recovery
// checks while delegating only changed device-number authority to the caller.
func RecoverFilesystemSnapshotWithBaseVerifier(ctx context.Context, generation, destination string, saved *FilesystemSnapshot, verify BaseRecoveryVerifier) (*FilesystemSnapshot, error) {
	return recoverFilesystemSnapshot(ctx, generation, destination, saved, verify)
}

func recoverFilesystemSnapshot(ctx context.Context, generation, destination string, saved *FilesystemSnapshot, verify BaseRecoveryVerifier) (*FilesystemSnapshot, error) {
	if !cleanAbsolute(generation) || !cleanAbsolute(destination) || generation == destination ||
		strings.HasPrefix(generation, destination+"/") || strings.HasPrefix(destination, generation+"/") {
		return nil, fmt.Errorf("recovery requires separate canonical filesystem roots")
	}
	if err := validateFilesystemSnapshotPaths(generation, saved); err != nil {
		return nil, err
	}
	// Admit all persistent mutable roots before creating output or traversing
	// payloads. A covering mount is foreign even if its bytes happen to match.
	for _, tree := range saved.Roots {
		if tree.Immutable != nil {
			continue
		}
		if err := immutableParents(tree.Saved); err != nil {
			return nil, err
		}
		root, err := openDirectory(tree.Saved)
		if err != nil {
			return nil, err
		}
		err = admitUncoveredPersistedRoot(root, tree.Saved)
		if err == nil {
			err = admitFilesystemCopySource(ctx, root)
		}
		if err := errors.Join(err, root.Close()); err != nil {
			return nil, err
		}
	}
	if err := os.Mkdir(destination, 0700); err != nil {
		return nil, err
	}
	roots := make([]filesystemRoot, 0, len(saved.Roots))
	for _, tree := range saved.Roots {
		expected := tree
		if tree.Immutable != nil {
			if err := os.Mkdir(destination+"/base", 0700); err != nil {
				return nil, err
			}
			base, err := recoverFilesystemBase(ctx, tree, destination+"/base/rootfs", verify)
			if err != nil {
				return nil, err
			}
			expected = base
		}
		roots = append(roots, filesystemRoot{Name: tree.Name, Source: expected.Saved, Expected: &expected})
	}
	filesystem, err := copyFilesystem(ctx, roots, destination+"/filesystem")
	if err != nil {
		return nil, err
	}
	if err := VerifyFilesystemSnapshot(ctx, destination, filesystem); err != nil {
		return nil, err
	}
	return filesystem, nil
}

func admitUncoveredPersistedRoot(root *os.File, path string) error {
	parent, err := openDirectory(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer parent.Close()
	var current, above unix.Statx_t
	if err := unix.Statx(int(root.Fd()), "", unix.AT_EMPTY_PATH, unix.STATX_MNT_ID, &current); err != nil {
		return err
	}
	if err := unix.Statx(int(parent.Fd()), "", unix.AT_EMPTY_PATH, unix.STATX_MNT_ID, &above); err != nil {
		return err
	}
	if current.Mask&unix.STATX_MNT_ID == 0 || above.Mask&unix.STATX_MNT_ID == 0 ||
		current.Mnt_id == 0 || current.Mnt_id != above.Mnt_id {
		return fmt.Errorf("persisted filesystem root has an unexpected covering mount")
	}
	return nil
}
