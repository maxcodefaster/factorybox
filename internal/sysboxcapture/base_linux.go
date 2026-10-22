//go:build linux

package sysboxcapture

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// ImmutableTree identifies a complete base imported into private worker-owned
// storage, then covered by a read-only self-bind. Its only runtime aliases are
// read-only binds. The backing is retained with the checkpoints which use it.
// Worker root is trusted; this is not protection against a malicious worker
// administrator or recovery after that worker's mount namespace is lost.
type ImmutableTree struct {
	Root        string `json:"root"`
	Device      uint64 `json:"device"`
	Inode       uint64 `json:"inode"`
	MountID     uint64 `json:"mount_id"`
	Mode        uint32 `json:"mode"`
	UID         uint32 `json:"uid"`
	GID         uint32 `json:"gid"`
	MtimeSec    int64  `json:"mtime_sec"`
	MtimeNano   int64  `json:"mtime_nano"`
	CtimeSec    int64  `json:"ctime_sec"`
	CtimeNano   int64  `json:"ctime_nano"`
	SHA256      string `json:"sha256"`
	Entries     int    `json:"entries"`
	FileBytes   int64  `json:"file_bytes"`
	HashedBytes int64  `json:"hashed_bytes"`
}

type ImmutableAlias struct {
	Device  uint64 `json:"device"`
	Inode   uint64 `json:"inode"`
	MountID uint64 `json:"mount_id"`
}

// PrepareFilesystemBase charges the complete source/copy verification once,
// before an application starts. The destination must be inside a root-owned
// mode-0700 directory and must not already exist. Failed preparation is retained.
func PrepareFilesystemBase(ctx context.Context, source, destination string) (FilesystemTree, error) {
	return prepareFilesystemBase(ctx, source, destination, nil, nil)
}

// RecoverFilesystemBase reimports the canonical file backing after its old
// worker mount has disappeared. Only complete target content/metadata matching
// the saved checkpoint can establish a new immutable identity. Old mount IDs
// and aliases are never treated as authority for the fresh base.
func RecoverFilesystemBase(ctx context.Context, saved FilesystemTree, destination string) (FilesystemTree, error) {
	base := saved.Immutable
	if saved.Name != "lower" || base == nil || !cleanAbsolute(base.Root) ||
		len(saved.SHA256) != 64 || strings.Trim(saved.SHA256, "0123456789abcdef") != "" ||
		saved.Entries <= 0 || saved.HashedBytes < 0 || saved.FileBytes < saved.HashedBytes ||
		base.SHA256 != saved.SHA256 || base.Entries != saved.Entries ||
		base.FileBytes != saved.FileBytes || base.HashedBytes != saved.HashedBytes {
		return FilesystemTree{}, fmt.Errorf("base recovery requires its complete saved immutable-tree manifest")
	}
	if err := immutableParents(base.Root); err != nil {
		return FilesystemTree{}, err
	}
	root, err := openDirectory(base.Root)
	if err != nil {
		return FilesystemTree{}, err
	}
	defer root.Close()
	var stat unix.Stat_t
	var current, parentMount unix.Statx_t
	if err := unix.Fstat(int(root.Fd()), &stat); err != nil {
		return FilesystemTree{}, err
	}
	if uint64(stat.Dev) != base.Device || stat.Ino != base.Inode || stat.Mode != base.Mode ||
		stat.Uid != base.UID || stat.Gid != base.GID || stat.Mtim.Sec != base.MtimeSec ||
		stat.Mtim.Nsec != base.MtimeNano || stat.Ctim.Sec != base.CtimeSec || stat.Ctim.Nsec != base.CtimeNano {
		return FilesystemTree{}, fmt.Errorf("persisted base inode or metadata changed")
	}
	parent, err := openDirectory(filepath.Dir(base.Root))
	if err != nil {
		return FilesystemTree{}, err
	}
	err = unix.Statx(int(root.Fd()), "", unix.AT_EMPTY_PATH, unix.STATX_MNT_ID, &current)
	if err == nil {
		err = unix.Statx(int(parent.Fd()), "", unix.AT_EMPTY_PATH, unix.STATX_MNT_ID, &parentMount)
	}
	if err := errors.Join(err, parent.Close()); err != nil {
		return FilesystemTree{}, err
	}
	if current.Mask&unix.STATX_MNT_ID == 0 || parentMount.Mask&unix.STATX_MNT_ID == 0 ||
		current.Mnt_id == 0 || current.Mnt_id != parentMount.Mnt_id {
		return FilesystemTree{}, fmt.Errorf("persisted base has an unexpected covering mount")
	}
	// The expected digest describes data, not the vanished kernel objects.
	// No old immutable pointer may escape on a failed new preparation.
	saved.Immutable, saved.Alias = nil, nil
	return prepareFilesystemBase(ctx, base.Root, destination, &saved, root)
}

func prepareFilesystemBase(ctx context.Context, source, destination string, expected *FilesystemTree, anchored *os.File) (FilesystemTree, error) {
	var tree FilesystemTree
	if !cleanAbsolute(source) || !cleanAbsolute(destination) || source == destination ||
		strings.HasPrefix(destination, source+"/") || strings.HasPrefix(source, destination+"/") {
		return tree, fmt.Errorf("base preparation requires separate canonical roots")
	}
	if err := immutableParents(destination); err != nil {
		return tree, err
	}
	root := anchored
	if root == nil {
		var err error
		root, err = openDirectory(source)
		if err != nil {
			return tree, err
		}
		defer root.Close()
	}
	if expected != nil {
		if err := admitFilesystemCopySource(ctx, root); err != nil {
			return tree, err
		}
	}
	tree, err := cloneFilesystemTreeExpected(ctx, root, destination, expected)
	if err != nil {
		return tree, err
	}
	// The private backing has no guest-visible writable alias. The inherited
	// read-only self-bind also protects the canonical path used by later forks.
	if err := readonlyBind(destination, destination); err != nil {
		return tree, err
	}
	sealed, err := openDirectory(destination)
	if err != nil {
		return tree, err
	}
	defer sealed.Close()
	var stat unix.Stat_t
	var extended unix.Statx_t
	if err := unix.Fstat(int(sealed.Fd()), &stat); err != nil {
		return tree, err
	}
	if err := unix.Statx(int(sealed.Fd()), "", unix.AT_EMPTY_PATH, unix.STATX_MNT_ID, &extended); err != nil {
		return tree, err
	}
	tree.Name, tree.Source, tree.Saved = "lower", source, destination
	tree.Immutable = &ImmutableTree{
		Root: destination, Device: uint64(stat.Dev), Inode: stat.Ino, MountID: extended.Mnt_id,
		Mode: stat.Mode, UID: stat.Uid, GID: stat.Gid,
		MtimeSec: stat.Mtim.Sec, MtimeNano: stat.Mtim.Nsec, CtimeSec: stat.Ctim.Sec, CtimeNano: stat.Ctim.Nsec,
		SHA256: tree.SHA256, Entries: tree.Entries, FileBytes: tree.FileBytes, HashedBytes: tree.HashedBytes,
	}
	if err := VerifyImmutableTree(destination, tree); err != nil {
		return tree, err
	}
	parent, err := openDirectory(filepath.Dir(destination))
	if err != nil {
		return tree, err
	}
	return tree, errors.Join(parent.Sync(), parent.Close())
}

func immutableParents(path string) error {
	parent := filepath.Dir(path)
	for directory := parent; ; directory = filepath.Dir(directory) {
		var stat unix.Stat_t
		if err := unix.Lstat(directory, &stat); err != nil {
			return err
		}
		if stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Uid != 0 || stat.Mode&0022 != 0 ||
			(directory == parent && stat.Mode&0777 != 0700) {
			return fmt.Errorf("immutable base requires private root-owned storage: %s", directory)
		}
		if directory == "/" {
			return nil
		}
	}
}

// VerifyImmutableTree checks ownership, the original read-only mount and the
// exact alias. It does not replace these checks with caller-supplied provenance
// or treat a read-only alias of a caller's writable tree as an immutable base.
func VerifyImmutableTree(path string, tree FilesystemTree) error {
	base := tree.Immutable
	if base == nil || !cleanAbsolute(base.Root) || !cleanAbsolute(path) ||
		base.Device == 0 || base.Inode == 0 || base.MountID == 0 ||
		base.Entries <= 0 || len(base.SHA256) != 64 ||
		base.SHA256 != tree.SHA256 || base.Entries != tree.Entries ||
		base.FileBytes != tree.FileBytes || base.HashedBytes != tree.HashedBytes {
		return fmt.Errorf("immutable base manifest is absent or differs from the complete imported tree")
	}
	if err := immutableParents(base.Root); err != nil {
		return err
	}
	for _, candidate := range []string{base.Root, path} {
		directory, err := openDirectory(candidate)
		if err != nil {
			return err
		}
		var stat unix.Stat_t
		var extended unix.Statx_t
		var filesystem unix.Statfs_t
		err = unix.Fstat(int(directory.Fd()), &stat)
		if err == nil {
			err = unix.Statx(int(directory.Fd()), "", unix.AT_EMPTY_PATH, unix.STATX_MNT_ID, &extended)
		}
		if err == nil {
			err = unix.Fstatfs(int(directory.Fd()), &filesystem)
		}
		err = errors.Join(err, directory.Close())
		if err != nil {
			return err
		}
		if uint64(stat.Dev) != base.Device || stat.Ino != base.Inode || stat.Mode != base.Mode ||
			stat.Uid != base.UID || stat.Gid != base.GID ||
			stat.Mtim.Sec != base.MtimeSec || stat.Mtim.Nsec != base.MtimeNano ||
			stat.Ctim.Sec != base.CtimeSec || stat.Ctim.Nsec != base.CtimeNano ||
			extended.Mask&unix.STATX_MNT_ID == 0 || extended.Mnt_id == 0 ||
			(candidate == base.Root && extended.Mnt_id != base.MountID) ||
			filesystem.Flags&unix.ST_RDONLY == 0 {
			return fmt.Errorf("immutable base identity or read-only mount changed: %s", candidate)
		}
		if err := noImmutableDescendantMounts(candidate); err != nil {
			return err
		}
	}
	return nil
}

func noImmutableDescendantMounts(path string) error {
	file, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 6 {
			return fmt.Errorf("invalid immutable-base mount table")
		}
		mountpoint := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`).Replace(fields[4])
		if strings.HasPrefix(mountpoint, path+"/") {
			return fmt.Errorf("immutable base contains a descendant mount: %s", fields[4])
		}
	}
	return scanner.Err()
}

func readonlyBind(source, destination string) error {
	root, err := openDirectory(source)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := unix.Mount("/proc/self/fd/"+strconv.Itoa(int(root.Fd())), destination, "", unix.MS_BIND, ""); err != nil {
		return err
	}
	if err := unix.Mount("", destination, "", unix.MS_REMOUNT|unix.MS_BIND|unix.MS_RDONLY, ""); err != nil {
		return errors.Join(err, unix.Unmount(destination, 0))
	}
	return nil
}

func bindImmutableTree(source, destination string, tree FilesystemTree) (FilesystemTree, error) {
	if err := VerifyImmutableTree(source, tree); err != nil {
		return tree, err
	}
	if err := os.Mkdir(destination, 0700); err != nil {
		return tree, err
	}
	directory, err := openDirectory(destination)
	if err != nil {
		return tree, err
	}
	var stat unix.Stat_t
	var extended unix.Statx_t
	err = unix.Fstat(int(directory.Fd()), &stat)
	if err == nil {
		err = unix.Statx(int(directory.Fd()), "", unix.AT_EMPTY_PATH, unix.STATX_MNT_ID, &extended)
	}
	if err := errors.Join(err, directory.Close()); err != nil {
		return tree, err
	}
	if extended.Mask&unix.STATX_MNT_ID == 0 || extended.Mnt_id == 0 {
		return tree, fmt.Errorf("immutable alias has no original mount identity")
	}
	tree.Alias = &ImmutableAlias{Device: uint64(stat.Dev), Inode: stat.Ino, MountID: extended.Mnt_id}
	if err := readonlyBind(tree.Immutable.Root, destination); err != nil {
		return tree, err
	}
	// Sharing this verified read-only base performs no source scan or copy.
	// Its import or previous alias observations belong to that earlier work.
	tree.CaptureUS, tree.SourceReadUS, tree.CopyUS, tree.TargetReadUS = 0, 0, 0, 0
	return tree, VerifyImmutableTree(destination, tree)
}

// BindFilesystemBase creates a new read-only alias of an already prepared base.
// An existing destination is never covered or replaced.
func BindFilesystemBase(tree FilesystemTree, destination string) (FilesystemTree, error) {
	if tree.Immutable == nil || !cleanAbsolute(destination) {
		return tree, fmt.Errorf("base binding requires a prepared base and canonical new destination")
	}
	return bindImmutableTree(tree.Immutable.Root, destination, tree)
}

// RetireImmutableAlias unmounts only a verified alias and acknowledges an
// already-unmounted original empty directory after a lost deletion outcome.
func RetireImmutableAlias(path string, tree FilesystemTree) error {
	if tree.Immutable == nil || tree.Alias == nil || !cleanAbsolute(path) || path == tree.Immutable.Root {
		return fmt.Errorf("immutable alias has no retained original mountpoint")
	}
	checkEmpty := func() error {
		directory, err := openDirectory(path)
		if err != nil {
			return err
		}
		defer directory.Close()
		var stat unix.Stat_t
		var extended unix.Statx_t
		if err := unix.Fstat(int(directory.Fd()), &stat); err != nil {
			return err
		}
		if err := unix.Statx(int(directory.Fd()), "", unix.AT_EMPTY_PATH, unix.STATX_MNT_ID, &extended); err != nil {
			return err
		}
		if uint64(stat.Dev) != tree.Alias.Device || stat.Ino != tree.Alias.Inode ||
			extended.Mnt_id != tree.Alias.MountID || stat.Uid != 0 || stat.Mode&0777 != 0700 {
			return fmt.Errorf("immutable alias original mountpoint changed")
		}
		entries, err := directory.Readdirnames(1)
		if len(entries) != 0 {
			return fmt.Errorf("immutable alias original mountpoint is not empty")
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		return nil
	}
	if err := VerifyImmutableTree(path, tree); err != nil {
		emptyErr := checkEmpty()
		if emptyErr == nil {
			return nil
		}
		return errors.Join(err, emptyErr)
	}
	if err := unix.Unmount(path, 0); err != nil {
		return err
	}
	return checkEmpty()
}
