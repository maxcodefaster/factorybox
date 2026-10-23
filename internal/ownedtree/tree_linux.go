//go:build linux

// Package ownedtree removes only the exact objects of a caller-authorized
// inventory. It supplies no authority to select a tree or resolve dependencies.
package ownedtree

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"

	"golang.org/x/sys/unix"
)

type Object struct {
	Device  uint64 `json:"device"`
	Inode   uint64 `json:"inode"`
	MountID uint64 `json:"mount_id"`
	Mode    uint32 `json:"mode"`
	UID     uint32 `json:"uid"`
	GID     uint32 `json:"gid"`
}

type Entry struct {
	Path    string `json:"path"`
	Object  Object `json:"object"`
	Bytes   int64  `json:"bytes,omitempty"`
	Links   uint64 `json:"links,omitempty"`
	Rdev    uint64 `json:"rdev,omitempty"`
	Content string `json:"content_sha256,omitempty"`
	Link    string `json:"link,omitempty"`
}

type Plan struct {
	Version int      `json:"version"`
	Root    Object   `json:"root"`
	Paths   []string `json:"paths"`
	Anchors []Entry  `json:"anchors"`
	Entries []Entry  `json:"entries"`
}

const MaximumEntries = 1000000

func local(name string) bool {
	return name != "." && name != "" && path.Clean(name) == name && !strings.HasPrefix(name, "/") &&
		name != ".." && !strings.HasPrefix(name, "../") && !strings.ContainsRune(name, 0)
}

func open(anchor int, name string, flags int) (int, error) {
	return unix.Openat2(anchor, name, &unix.OpenHow{Flags: uint64(flags | unix.O_CLOEXEC), Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS})
}

func object(fd int) (Object, unix.Stat_t, error) {
	var stat unix.Stat_t
	var extended unix.Statx_t
	err := errors.Join(unix.Fstat(fd, &stat), unix.Statx(fd, "", unix.AT_EMPTY_PATH, unix.STATX_MNT_ID, &extended))
	if err != nil {
		return Object{}, stat, err
	}
	if extended.Mask&unix.STATX_MNT_ID == 0 || extended.Mnt_id == 0 {
		return Object{}, stat, fmt.Errorf("owned tree mount identity unavailable")
	}
	return Object{uint64(stat.Dev), stat.Ino, extended.Mnt_id, stat.Mode, stat.Uid, stat.Gid}, stat, nil
}

func inspect(ctx context.Context, anchor int, name string, payload bool) (entry Entry, resultErr error) {
	entry.Path = name
	if err := ctx.Err(); err != nil {
		return entry, err
	}
	fd, err := open(anchor, name, unix.O_PATH|unix.O_NOFOLLOW)
	if err != nil {
		return entry, err
	}
	defer func() { resultErr = errors.Join(resultErr, unix.Close(fd)) }()
	entry.Object, _, err = object(fd)
	if err != nil {
		return entry, err
	}
	if !payload {
		return entry, nil
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return entry, err
	}
	entry.Bytes, entry.Links, entry.Rdev = stat.Size, uint64(stat.Nlink), uint64(stat.Rdev)
	switch stat.Mode & unix.S_IFMT {
	case unix.S_IFDIR:
		entry.Bytes, entry.Links = 0, 0
	case unix.S_IFREG:
		content, err := open(anchor, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NOATIME|unix.O_NONBLOCK)
		if err != nil {
			return entry, err
		}
		file := os.NewFile(uintptr(content), name)
		actual, _, identityErr := object(content)
		hash := sha256.New()
		_, readErr := io.Copy(hash, contextReader{ctx, file})
		if err := errors.Join(identityErr, readErr, file.Close()); err != nil {
			return entry, err
		}
		if actual != entry.Object {
			return entry, fmt.Errorf("owned file changed while hashing: %s", name)
		}
		entry.Content = hex.EncodeToString(hash.Sum(nil))
	case unix.S_IFLNK:
		buffer := make([]byte, 4097)
		n, err := unix.Readlinkat(fd, "", buffer)
		if err != nil || n > 4096 {
			return entry, fmt.Errorf("owned symlink could not be identified: %s: %v", name, err)
		}
		entry.Link = string(buffer[:n])
	case unix.S_IFCHR, unix.S_IFBLK, unix.S_IFIFO, unix.S_IFSOCK:
	default:
		return entry, fmt.Errorf("unsupported owned node: %s", name)
	}
	return entry, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(data)
}

func list(anchor int, name string) ([]os.DirEntry, error) {
	fd, err := open(anchor, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_NOATIME)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), name)
	entries, readErr := file.ReadDir(MaximumEntries + 1)
	if errors.Is(readErr, io.EOF) {
		readErr = nil
	}
	if err := errors.Join(readErr, file.Close()); err != nil {
		return nil, err
	}
	if len(entries) > MaximumEntries {
		return nil, fmt.Errorf("owned directory exceeds inventory bound")
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	return entries, nil
}

func walk(ctx context.Context, anchor int, name string, mount uint64, visit func(Entry) error, depth int) error {
	if depth > 256 {
		return fmt.Errorf("owned tree exceeds depth bound")
	}
	entry, err := inspect(ctx, anchor, name, true)
	if err != nil {
		return err
	}
	if entry.Object.MountID != mount {
		return fmt.Errorf("owned tree crosses foreign mount: %s", name)
	}
	if err := visit(entry); err != nil {
		return err
	}
	if entry.Object.Mode&unix.S_IFMT != unix.S_IFDIR {
		return nil
	}
	entries, err := list(anchor, name)
	if err != nil {
		return err
	}
	for _, child := range entries {
		if err := walk(ctx, anchor, name+"/"+child.Name(), mount, visit, depth+1); err != nil {
			return err
		}
	}
	return nil
}

// Capture must follow the caller's complete ownership/content authorization.
// No file, mount or permission is changed while taking the inventory.
func Capture(ctx context.Context, root *os.Root, paths []string) (plan Plan, resultErr error) {
	anchor, err := root.Open(".")
	if err != nil {
		return plan, err
	}
	defer func() { resultErr = errors.Join(resultErr, anchor.Close()) }()
	plan.Version = 1
	plan.Root, _, err = object(int(anchor.Fd()))
	if err != nil {
		return plan, err
	}
	plan.Paths = append([]string(nil), paths...)
	sort.Strings(plan.Paths)
	anchors := map[string]bool{}
	for i, name := range plan.Paths {
		if !local(name) || i > 0 && (name == plan.Paths[i-1] || strings.HasPrefix(name, plan.Paths[i-1]+"/")) {
			return plan, fmt.Errorf("owned payload paths are invalid or overlap")
		}
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			anchors[parent] = true
		}
		if err := walk(ctx, int(anchor.Fd()), name, plan.Root.MountID, func(entry Entry) error {
			if len(plan.Entries) >= MaximumEntries {
				return fmt.Errorf("owned tree exceeds inventory bound")
			}
			plan.Entries = append(plan.Entries, entry)
			return nil
		}, 0); err != nil {
			return plan, err
		}
	}
	names := make([]string, 0, len(anchors))
	for name := range anchors {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		entry, err := inspect(ctx, int(anchor.Fd()), name, false)
		if err != nil {
			return plan, err
		}
		if entry.Object.MountID != plan.Root.MountID || entry.Object.Mode&unix.S_IFMT != unix.S_IFDIR {
			return plan, fmt.Errorf("owned payload parent is outside its root")
		}
		plan.Anchors = append(plan.Anchors, entry)
	}
	if err := validate(ctx, int(anchor.Fd()), plan, false); err != nil {
		return plan, err
	}
	return plan, nil
}

func same(actual, expected Entry) bool {
	return actual.Path == expected.Path && actual.Object == expected.Object && actual.Bytes == expected.Bytes &&
		actual.Rdev == expected.Rdev && actual.Content == expected.Content && actual.Link == expected.Link
}

func validate(ctx context.Context, anchor int, plan Plan, missing bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	actualRoot, _, err := object(anchor)
	if err != nil {
		return err
	}
	if plan.Version != 1 || actualRoot != plan.Root || len(plan.Entries) > MaximumEntries || len(plan.Paths) == 0 {
		return fmt.Errorf("owned inventory root or version changed")
	}
	expected := map[string]Entry{}
	for _, entry := range plan.Entries {
		if !local(entry.Path) || entry.Object.Inode == 0 || entry.Object.MountID != plan.Root.MountID {
			return fmt.Errorf("invalid owned inventory entry")
		}
		if _, exists := expected[entry.Path]; exists {
			return fmt.Errorf("duplicate owned inventory entry")
		}
		inside := false
		for _, name := range plan.Paths {
			inside = inside || entry.Path == name || strings.HasPrefix(entry.Path, name+"/")
		}
		if !inside {
			return fmt.Errorf("owned inventory entry is outside its payload roots")
		}
		expected[entry.Path] = entry
	}
	for _, entry := range plan.Anchors {
		actual, err := inspect(ctx, anchor, entry.Path, false)
		if err != nil || actual.Object != entry.Object {
			return errors.Join(fmt.Errorf("owned payload parent changed: %s", entry.Path), err)
		}
	}
	seen := map[string]bool{}
	links := map[[2]uint64]uint64{}
	observed := map[[2]uint64]uint64{}
	for _, name := range plan.Paths {
		if !local(name) {
			return fmt.Errorf("invalid owned inventory root path")
		}
		if _, exists := expected[name]; !exists {
			return fmt.Errorf("inventory omits its payload root")
		}
		err := walk(ctx, anchor, name, plan.Root.MountID, func(actual Entry) error {
			entry, exists := expected[actual.Path]
			if !exists || !same(actual, entry) {
				return fmt.Errorf("owned payload changed or contains an unknown entry: %s", actual.Path)
			}
			seen[actual.Path] = true
			if actual.Object.Mode&unix.S_IFMT != unix.S_IFDIR {
				key := [2]uint64{actual.Object.Device, actual.Object.Inode}
				links[key]++
				observed[key] = actual.Links
			}
			return nil
		}, 0)
		if err != nil && !(missing && errors.Is(err, os.ErrNotExist)) {
			return err
		}
	}
	if !missing && len(seen) != len(expected) {
		return fmt.Errorf("owned inventory is incomplete")
	}
	for key, count := range links {
		if count != observed[key] {
			return fmt.Errorf("owned payload has an external hardlink")
		}
	}
	return nil
}

// ValidateRemaining accepts absent original objects only after the caller has
// durably published this exact plan as its deletion intent. Successors and
// unknown entries are always refused, including on a retry.
func ValidateRemaining(ctx context.Context, root *os.Root, plan Plan) error {
	anchor, err := root.Open(".")
	if err != nil {
		return err
	}
	defer anchor.Close()
	return validate(ctx, int(anchor.Fd()), plan, true)
}

// ValidateComplete admits no absent original object. A prepared but unaccepted
// deletion must still prove that nothing was removed before its durable fence.
func ValidateComplete(ctx context.Context, root *os.Root, plan Plan) error {
	anchor, err := root.Open(".")
	if err != nil {
		return err
	}
	defer anchor.Close()
	return validate(ctx, int(anchor.Fd()), plan, false)
}

// RemoveRemaining never truncates payloads and never uses recursive RemoveAll.
// Only inventoried original objects are unlinked; a live open reference retains
// its data. The caller must observe filesystem capacity separately.
func RemoveRemaining(ctx context.Context, root *os.Root, plan Plan) error {
	anchor, err := root.Open(".")
	if err != nil {
		return err
	}
	defer anchor.Close()
	if err := validate(ctx, int(anchor.Fd()), plan, true); err != nil {
		return err
	}
	entries := append([]Entry(nil), plan.Entries...)
	sort.Slice(entries, func(i, j int) bool {
		a, b := strings.Count(entries[i].Path, "/"), strings.Count(entries[j].Path, "/")
		if a != b {
			return a > b
		}
		return entries[i].Path > entries[j].Path
	})
	parents := map[string]Object{".": plan.Root}
	for _, entry := range plan.Anchors {
		parents[entry.Path] = entry.Object
	}
	for _, entry := range plan.Entries {
		if entry.Object.Mode&unix.S_IFMT == unix.S_IFDIR {
			parents[entry.Path] = entry.Object
		}
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		parentName := path.Dir(entry.Path)
		parent, err := open(int(anchor.Fd()), parentName, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW)
		if errors.Is(err, unix.ENOENT) {
			continue
		}
		if err != nil {
			return err
		}
		actualParent, _, identityErr := object(parent)
		if identityErr != nil || actualParent != parents[parentName] {
			unix.Close(parent)
			return errors.Join(fmt.Errorf("owned removal parent changed: %s", parentName), identityErr)
		}
		name := path.Base(entry.Path)
		actual, err := inspect(ctx, parent, name, true)
		if errors.Is(err, unix.ENOENT) {
			unix.Close(parent)
			continue
		}
		actual.Path = entry.Path
		if err != nil || !same(actual, entry) {
			unix.Close(parent)
			return errors.Join(fmt.Errorf("owned removal object changed: %s", entry.Path), err)
		}
		flags := 0
		if entry.Object.Mode&unix.S_IFMT == unix.S_IFDIR {
			flags = unix.AT_REMOVEDIR
		}
		removeErr := unix.Unlinkat(parent, name, flags)
		if err := errors.Join(removeErr, unix.Close(parent)); err != nil {
			return err
		}
	}
	if err := unix.Syncfs(int(anchor.Fd())); err != nil {
		return err
	}
	for _, name := range plan.Paths {
		_, err := inspect(ctx, int(anchor.Fd()), name, false)
		if !errors.Is(err, unix.ENOENT) {
			return fmt.Errorf("owned removal completion is unconfirmed: %s: %v", name, err)
		}
	}
	return nil
}
