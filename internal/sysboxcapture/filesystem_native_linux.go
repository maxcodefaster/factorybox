//go:build linux

package sysboxcapture

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// NativeFilesystemTree binds two distinct kernel roots. A native proof is
// never a content digest. Member selects upper inside its common upper/work
// root; managed volumes use the entire subvolume.
type NativeFilesystemTree struct {
	Member string            `json:"member,omitempty"`
	Source subvolumeIdentity `json:"source"`
	Saved  subvolumeIdentity `json:"saved"`
}

// OverlayWritablePaths keeps existing flat bundles unchanged. Fresh native
// bundles put upper and work in one real subvolume; an ordinary directory,
// alias or partial native layout cannot impersonate it.
func OverlayWritablePaths(bundle string) (upper, work string, resultErr error) {
	if !cleanAbsolute(bundle) {
		return "", "", fmt.Errorf("overlay bundle is not canonical")
	}
	path := bundle + "/mutable"
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return bundle + "/upper", bundle + "/work", nil
	} else if err != nil {
		return "", "", err
	}
	root, identity, err := openNativeFilesystemRoot(context.Background(), path)
	if err != nil {
		return "", "", err
	}
	defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
	if identity.Flags != 0 {
		return "", "", fmt.Errorf("working overlay requires its writable native root")
	}
	if err := verifyNativeMembers(root, "upper", identity); err != nil {
		return "", "", err
	}
	return path + "/upper", path + "/work", nil
}

func nativeTreeRoot(path, member string) (string, error) {
	if !cleanAbsolute(path) || member != "" && member != "upper" {
		return "", fmt.Errorf("native tree has an invalid path or member")
	}
	if member == "" {
		return path, nil
	}
	if !strings.HasSuffix(path, "/mutable/upper") {
		return "", fmt.Errorf("native upper is outside its common upper/work subvolume")
	}
	return filepath.Dir(path), nil
}

func noNestedSubvolumes(root *os.File) error {
	references := make([]byte, 4096)
	err := subvolumeIOCTL(int(root.Fd()), btrfsRootRefsIOCTL, references)
	if errors.Is(err, unix.EOVERFLOW) || references[4088] != 0 {
		return errNestedSubvolume
	}
	return err
}

// Open every ancestor without aliases and retain the actual root descriptor.
// The parent mount must be the same, including for a same-data self-bind.
// Only bounded metadata is read; private file data is not traversed.
func openNativeFilesystemRoot(ctx context.Context, path string) (_ *os.File, identity subvolumeIdentity, resultErr error) {
	if err := ctx.Err(); err != nil {
		return nil, identity, err
	}
	if !cleanAbsolute(path) {
		return nil, identity, fmt.Errorf("native filesystem root is not canonical")
	}
	fd, err := unix.Openat2(unix.AT_FDCWD, path, &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOATIME | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_NO_SYMLINKS,
	})
	if err != nil {
		return nil, identity, err
	}
	root := os.NewFile(uintptr(fd), path)
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, root.Close())
		}
	}()
	if err := unix.Syncfs(fd); err != nil {
		return nil, identity, err
	}
	identity, err = inspectSubvolume(root)
	if err != nil {
		return nil, identity, err
	}
	if err := retainedDirectoryMount(root, path); err != nil {
		return nil, identity, err
	}
	if err := noImmutableDescendantMounts(path); err != nil {
		return nil, identity, err
	}
	if err := noNestedSubvolumes(root); err != nil {
		return nil, identity, err
	}
	return root, identity, nil
}

func sameNativeRoot(actual, expected subvolumeIdentity) bool {
	if expected.ID < 256 || expected.UUID == "" || expected.FilesystemUUID == "" ||
		expected.Created == 0 || expected.Generation == 0 || expected.MountID == 0 ||
		actual.Generation < expected.Generation {
		return false
	}
	// A clone can advance bookkeeping generation, but never the original
	// content-change transaction, UUID, lineage, mount or read-only property.
	expected.Generation = actual.Generation
	return actual == expected
}

func verifyNativeMembers(root *os.File, member string, identity subvolumeIdentity) error {
	if member == "" {
		return nil
	}
	for _, name := range []string{"upper", "work"} {
		fd, err := unix.Openat2(int(root.Fd()), name, &unix.OpenHow{
			Flags:   unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOATIME | unix.O_CLOEXEC,
			Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS,
		})
		if err != nil {
			return err
		}
		var stat unix.Stat_t
		var mount unix.Statx_t
		err = errors.Join(unix.Fstat(fd, &stat),
			unix.Statx(fd, "", unix.AT_EMPTY_PATH, unix.STATX_MNT_ID, &mount), unix.Close(fd))
		if err != nil {
			return err
		}
		if uint64(stat.Dev) != identity.Device || mount.Mask&unix.STATX_MNT_ID == 0 ||
			mount.Mnt_id != identity.MountID {
			return fmt.Errorf("native upper and work must belong to their original common root")
		}
	}
	return nil
}

func verifyNativeFilesystemRoot(ctx context.Context, path string, expected subvolumeIdentity) error {
	root, actual, err := openNativeFilesystemRoot(ctx, path)
	if err != nil {
		return err
	}
	err = root.Close()
	if !sameNativeRoot(actual, expected) {
		return errors.Join(err, fmt.Errorf("native filesystem root changed: %s", path))
	}
	return err
}

// Native capture is selected only by the fresh common mutable layout. A flat
// existing bundle keeps its original copier. Once selected, every private root
// must be native; silently copying an unsupported Docker root would hide an
// incomplete layout and reintroduce the expensive path.
func nativeFilesystemPlan(ctx context.Context, roots []filesystemRoot) (native, materialized bool, resultErr error) {
	for _, root := range roots {
		if root.Name == "upper" {
			native = strings.HasSuffix(root.Source, "/mutable/upper")
			materialized = root.Expected != nil && root.Expected.Native != nil
			native = native || materialized
			break
		}
	}
	if !native {
		return false, false, nil
	}
	for _, root := range roots {
		if root.Name == "lower" {
			if root.Expected == nil || root.Expected.Immutable == nil {
				return false, false, fmt.Errorf("native capture requires its already verified immutable lower")
			}
			continue
		}
		member := ""
		if root.Name == "upper" {
			member = "upper"
		}
		path, err := nativeTreeRoot(root.Source, member)
		if err != nil {
			return false, false, err
		}
		source, identity, err := openNativeFilesystemRoot(ctx, path)
		if err != nil {
			return false, false, err
		}
		if err := errors.Join(verifyNativeMembers(source, member, identity), source.Close()); err != nil {
			return false, false, err
		}
		if materialized {
			if root.Expected == nil || root.Expected.Native == nil ||
				root.Expected.Native.Member != member ||
				root.Expected.Native.Saved.Flags != btrfsReadonlyRoot ||
				!sameNativeRoot(identity, root.Expected.Native.Saved) {
				return false, false, fmt.Errorf("native materialization lacks its sealed original root")
			}
		} else if identity.Flags != 0 || root.Expected != nil {
			return false, false, fmt.Errorf("native capture lacks an original writable root")
		}
	}
	return true, materialized, nil
}

func cloneNativeFilesystemTree(ctx context.Context, root filesystemRoot, destination string, materialized bool) (tree FilesystemTree, resultErr error) {
	started := time.Now()
	member := ""
	if root.Name == "upper" {
		member = "upper"
		destination = filepath.Dir(destination) + "/mutable/upper"
	}
	sourcePath, err := nativeTreeRoot(root.Source, member)
	if err != nil {
		return tree, err
	}
	targetPath, err := nativeTreeRoot(destination, member)
	if err != nil {
		return tree, err
	}
	source, identity, err := openNativeFilesystemRoot(ctx, sourcePath)
	if err != nil {
		return tree, err
	}
	defer func() { resultErr = errors.Join(resultErr, source.Close()) }()
	if err := verifyNativeMembers(source, member, identity); err != nil {
		return tree, err
	}
	if materialized && (root.Expected == nil || root.Expected.Native == nil ||
		!sameNativeRoot(identity, root.Expected.Native.Saved)) {
		return tree, fmt.Errorf("native clone source changed after admission")
	}
	saved, err := cloneSubvolume(ctx, source, targetPath, !materialized)
	if err != nil {
		return tree, err
	}
	// Capture the original source after the clone's bookkeeping transaction.
	// No legitimate content write is allowed between these observations.
	current, err := inspectSubvolume(source)
	if err != nil || !sameNativeRoot(current, identity) {
		return tree, errors.Join(err, fmt.Errorf("native clone source changed during capture"))
	}
	return FilesystemTree{Name: root.Name, Source: root.Source, Saved: destination,
		CaptureUS: time.Since(started).Microseconds(),
		Native:    &NativeFilesystemTree{Member: member, Source: current, Saved: saved}}, nil
}

func validateNativeFilesystemTree(tree FilesystemTree, writable bool) error {
	native := tree.Native
	if native == nil || tree.Immutable != nil || tree.Alias != nil ||
		tree.SHA256 != "" || tree.Entries != 0 || tree.FileBytes != 0 || tree.HashedBytes != 0 ||
		tree.SourceReadUS != 0 || tree.CopyUS != 0 || tree.TargetReadUS != 0 {
		return fmt.Errorf("native filesystem proof is absent or mixed with a content digest")
	}
	member := ""
	if tree.Name == "upper" {
		member = "upper"
	}
	if native.Member != member || tree.Name != "upper" && !strings.HasPrefix(tree.Name, "volumes/") {
		return fmt.Errorf("native filesystem root has an unexpected member")
	}
	for _, path := range []string{tree.Source, tree.Saved} {
		if _, err := nativeTreeRoot(path, member); err != nil {
			return err
		}
	}
	sourceFlags, savedFlags := uint64(0), btrfsReadonlyRoot
	if writable {
		sourceFlags, savedFlags = btrfsReadonlyRoot, 0
	}
	if native.Source.Flags != sourceFlags || native.Saved.Flags != savedFlags ||
		!sameNativeRoot(native.Source, native.Source) || !sameNativeRoot(native.Saved, native.Saved) ||
		native.Source.FilesystemUUID != native.Saved.FilesystemUUID ||
		native.Source.ID == native.Saved.ID || native.Source.UUID == native.Saved.UUID ||
		native.Saved.ParentUUID != native.Source.UUID {
		return fmt.Errorf("native filesystem proof has invalid roots, lineage or read-only properties")
	}
	return nil
}

func materializedNativeFilesystemTree(actual, expected FilesystemTree) bool {
	if actual.Native == nil || expected.Native == nil ||
		validateNativeFilesystemTree(actual, true) != nil ||
		validateNativeFilesystemTree(expected, false) != nil {
		return false
	}
	return actual.Name == expected.Name && actual.Native.Member == expected.Native.Member &&
		sameNativeRoot(actual.Native.Source, expected.Native.Saved)
}
