//go:build linux

package sysboxcapture

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

type filesystemRoot struct {
	Name, Source string
}

type FilesystemSnapshot struct {
	Version int              `json:"version"`
	Roots   []FilesystemTree `json:"roots"`
}

type FilesystemTree struct {
	Name        string `json:"name"`
	Source      string `json:"source"`
	Saved       string `json:"saved"`
	SHA256      string `json:"sha256"`
	Entries     int    `json:"entries"`
	FileBytes   int64  `json:"file_bytes"`
	HashedBytes int64  `json:"hashed_bytes"`
	CaptureUS   int64  `json:"capture_us"`
}

var managedDirectories = map[string]string{
	"/var/lib/docker": "docker", "/var/lib/kubelet": "kubelet",
	"/var/lib/k0s": "k0s", "/var/lib/rancher/k3s": "rancher-k3s",
	"/var/lib/rancher/rke2": "rancher-rke2", "/var/lib/buildkit": "buildkit",
	"/var/lib/containerd/io.containerd.snapshotter.v1.overlayfs": "containerd",
}

func cleanAbsolute(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path &&
		!strings.ContainsAny(path, "\n,:\\")
}

func planFilesystem(request Request, stateData []byte) ([]filesystemRoot, error) {
	if !cleanAbsolute(request.Bundle) || !cleanAbsolute(request.ManagerRoot) ||
		request.Rootfs != request.Bundle+"/rootfs" {
		return nil, fmt.Errorf("filesystem capture requires explicit canonical overlay and manager roots")
	}
	var state struct {
		Config struct {
			Rootfs  string                       `json:"rootfs"`
			Shift   int                          `json:"rootfs_uid_shift_type"`
			Cloned  bool                         `json:"rootfs_cloned"`
			Hooks   map[string][]json.RawMessage `json:"hooks"`
			Shiftfs []json.RawMessage            `json:"shiftfs_mounts"`
			Mounts  []struct {
				Source, Destination, Device string
				IDMapped                    bool `json:"idmap_mount"`
			} `json:"mounts"`
		} `json:"config"`
		Sysbox struct {
			ID string `json:"Id"`
			Fs struct {
				ID, Mountpoint string
				Active, Reg    bool
			}
		} `json:"sysbox"`
	}
	if err := json.Unmarshal(stateData, &state); err != nil {
		return nil, err
	}
	// IDMappedMount is 2 in the pinned Sysbox idShiftUtils enum.
	if state.Config.Rootfs != request.Rootfs || state.Config.Shift != 2 || state.Config.Cloned ||
		filesystemHookCount(state.Config.Hooks) != 0 || len(state.Config.Shiftfs) != 0 ||
		state.Sysbox.ID != request.RuntimeID || state.Sysbox.Fs.ID != request.RuntimeID ||
		!state.Sysbox.Fs.Active || !state.Sysbox.Fs.Reg || !cleanAbsolute(state.Sysbox.Fs.Mountpoint) {
		return nil, fmt.Errorf("filesystem source admission differs: rootfs=%q shift=%d cloned=%t hooks=%d shiftfs=%d sysbox=%q fs-id=%q active=%t registered=%t mountpoint=%q",
			state.Config.Rootfs, state.Config.Shift, state.Config.Cloned, filesystemHookCount(state.Config.Hooks),
			len(state.Config.Shiftfs), state.Sysbox.ID, state.Sysbox.Fs.ID,
			state.Sysbox.Fs.Active, state.Sysbox.Fs.Reg, state.Sysbox.Fs.Mountpoint)
	}
	if err := CheckOverlayLayout(request.Bundle); err != nil {
		return nil, err
	}
	roots := []filesystemRoot{{"lower", request.Bundle + "/lower"}, {"upper", request.Bundle + "/upper"}}
	seen := map[string]bool{}
	fusePrefix := state.Sysbox.Fs.Mountpoint + "/" + request.RuntimeID + "/"
	for _, mount := range state.Config.Mounts {
		if mount.Device != "bind" {
			switch mount.Device {
			case "proc", "sysfs", "tmpfs", "devpts", "mqueue", "cgroup", "cgroup2":
				continue
			default:
				return nil, fmt.Errorf("unsupported checkpoint mount type %s", mount.Device)
			}
		}
		if !cleanAbsolute(mount.Source) {
			return nil, fmt.Errorf("noncanonical checkpoint bind source")
		}
		if strings.HasPrefix(mount.Source, fusePrefix) || mount.Source == "/dev/null" {
			continue
		}
		kind, ok := managedDirectories[mount.Destination]
		expected := request.ManagerRoot + "/" + kind + "/" + request.RuntimeID
		if !ok || !mount.IDMapped || mount.Source != expected || seen[kind] {
			return nil, fmt.Errorf("checkpoint cannot omit or share external bind %s -> %s", mount.Source, mount.Destination)
		}
		seen[kind] = true
		roots = append(roots, filesystemRoot{"volumes/" + kind, mount.Source})
	}
	return roots, nil
}

func filesystemHookCount(hooks map[string][]json.RawMessage) int {
	total := 0
	for _, entries := range hooks {
		total += len(entries)
	}
	return total
}

var ErrOverlayAbsent = errors.New("admitted overlay mount is absent")

func CheckOverlayLayout(bundle string) error {
	file, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 10 || fields[4] != bundle+"/rootfs" {
			continue
		}
		separator := -1
		for i, value := range fields {
			if value == "-" {
				separator = i
				break
			}
		}
		if separator < 0 || len(fields) <= separator+3 || fields[separator+1] != "overlay" {
			return fmt.Errorf("checkpoint rootfs is not the expected overlay")
		}
		options := map[string]string{}
		for _, option := range strings.Split(fields[separator+3], ",") {
			key, value, ok := strings.Cut(option, "=")
			if ok {
				options[key] = value
			}
		}
		if options["lowerdir"] != bundle+"/lower" || options["upperdir"] != bundle+"/upper" ||
			options["workdir"] != bundle+"/work" {
			return fmt.Errorf("checkpoint overlay backing paths differ from the admitted layout")
		}
		return nil
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return ErrOverlayAbsent
}

func openDirectory(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NOATIME, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

func captureFilesystem(roots []filesystemRoot, destination string) (*FilesystemSnapshot, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	return copyFilesystem(ctx, roots, destination)
}

func copyFilesystem(ctx context.Context, roots []filesystemRoot, destination string) (*FilesystemSnapshot, error) {
	if err := os.Mkdir(destination, 0700); err != nil {
		return nil, err
	}
	if err := os.Mkdir(destination+"/volumes", 0700); err != nil {
		return nil, err
	}
	result := &FilesystemSnapshot{Version: 1}
	for _, root := range roots {
		start := time.Now()
		source, err := openDirectory(root.Source)
		if err != nil {
			return nil, err
		}
		tree, err := cloneFilesystemTree(ctx, source, destination+"/"+root.Name)
		closeErr := source.Close()
		if err := errors.Join(err, closeErr); err != nil {
			return nil, err
		}
		tree.Name, tree.Source, tree.Saved = root.Name, root.Source, destination+"/"+root.Name
		tree.CaptureUS = time.Since(start).Microseconds()
		result.Roots = append(result.Roots, tree)
	}
	for _, path := range []string{destination + "/volumes", destination} {
		directory, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		if err := errors.Join(directory.Sync(), directory.Close()); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// MaterializeFilesystemSnapshot makes another complete, inode-independent copy
// of a saved generation. The destination must be new; failed copies remain for
// reconciliation and are never silently replaced.
func MaterializeFilesystemSnapshot(ctx context.Context, generation, destination string, snapshot *FilesystemSnapshot) (*FilesystemSnapshot, error) {
	if !cleanAbsolute(generation) || !cleanAbsolute(destination) ||
		destination == generation || strings.HasPrefix(destination, generation+"/") ||
		strings.HasPrefix(generation, destination+"/") {
		return nil, fmt.Errorf("filesystem materialization requires separate canonical generation and destination paths")
	}
	if err := VerifyFilesystemSnapshot(ctx, generation, snapshot); err != nil {
		return nil, err
	}
	parentFD, err := unix.Openat2(unix.AT_FDCWD, filepath.Dir(destination), &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOATIME,
		Resolve: unix.RESOLVE_NO_SYMLINKS})
	if err != nil {
		return nil, err
	}
	parent := os.NewFile(uintptr(parentFD), "filesystem materialization parent")
	defer parent.Close()
	if err := unix.Mkdirat(parentFD, filepath.Base(destination), 0700); err != nil {
		return nil, err
	}
	if err := parent.Sync(); err != nil {
		return nil, err
	}
	roots := make([]filesystemRoot, 0, len(snapshot.Roots))
	for _, tree := range snapshot.Roots {
		roots = append(roots, filesystemRoot{Name: tree.Name, Source: tree.Saved})
	}
	result, err := copyFilesystem(ctx, roots, destination+"/filesystem")
	if err != nil {
		return nil, err
	}
	for index, tree := range result.Roots {
		expected := snapshot.Roots[index]
		if !sameFilesystemTree(tree, expected) {
			return nil, fmt.Errorf("materialized filesystem differs from its saved checkpoint: %s", tree.Name)
		}
	}
	directory, err := openDirectory(destination)
	if err != nil {
		return nil, err
	}
	return result, errors.Join(directory.Sync(), directory.Close())
}

func sameFilesystemTree(actual, expected FilesystemTree) bool {
	return actual.SHA256 == expected.SHA256 && actual.Entries == expected.Entries &&
		actual.FileBytes == expected.FileBytes && actual.HashedBytes == expected.HashedBytes
}

// VerifyFilesystemSnapshot reads every saved tree again and checks the exact
// content/metadata digest. Saved paths must belong to this generation.
func VerifyFilesystemSnapshot(ctx context.Context, generation string, snapshot *FilesystemSnapshot) error {
	if snapshot == nil || snapshot.Version != 1 || len(snapshot.Roots) < 2 || len(snapshot.Roots) > 9 {
		return fmt.Errorf("filesystem snapshot root set is absent or invalid")
	}
	seen := map[string]bool{}
	for _, tree := range snapshot.Roots {
		allowed := tree.Name == "lower" || tree.Name == "upper"
		for _, kind := range managedDirectories {
			allowed = allowed || tree.Name == "volumes/"+kind
		}
		if !allowed || seen[tree.Name] || tree.Saved != generation+"/filesystem/"+tree.Name {
			return fmt.Errorf("filesystem snapshot root escaped its generation or is duplicated")
		}
		seen[tree.Name] = true
		directory, err := openDirectory(tree.Saved)
		if err != nil {
			return err
		}
		actual, readErr := fingerprintTree(ctx, directory, nil, false)
		closeErr := directory.Close()
		if err := errors.Join(readErr, closeErr); err != nil {
			return err
		}
		if !sameFilesystemTree(actual, tree) {
			return fmt.Errorf("saved filesystem tree changed: %s", tree.Name)
		}
	}
	if !seen["lower"] || !seen["upper"] {
		return fmt.Errorf("filesystem snapshot omitted an overlay layer")
	}
	return nil
}

// VerifyLiveFilesystemSnapshot requires the stopped worker-owned backing to
// remain exactly at the pause point before restoring RAM into that runtime.
func VerifyLiveFilesystemSnapshot(ctx context.Context, snapshot *FilesystemSnapshot) error {
	if snapshot == nil || snapshot.Version != 1 || len(snapshot.Roots) < 2 || len(snapshot.Roots) > 9 {
		return fmt.Errorf("live pause backing is absent or invalid")
	}
	for _, tree := range snapshot.Roots {
		if !cleanAbsolute(tree.Source) {
			return fmt.Errorf("live pause backing path is not canonical")
		}
		directory, err := openDirectory(tree.Source)
		if err != nil {
			return err
		}
		actual, readErr := fingerprintTree(ctx, directory, nil, false)
		closeErr := directory.Close()
		if err := errors.Join(readErr, closeErr); err != nil {
			return err
		}
		if !sameFilesystemTree(actual, tree) {
			return fmt.Errorf("stopped working filesystem changed after pause: %s", tree.Name)
		}
	}
	return nil
}

func cloneFilesystemTree(ctx context.Context, source *os.File, destination string) (FilesystemTree, error) {
	expected, err := fingerprintTree(ctx, source, nil, false)
	if err != nil {
		return expected, err
	}
	if err := os.Mkdir(destination, 0700); err != nil {
		return expected, err
	}
	target, err := openDirectory(destination)
	if err != nil {
		return expected, err
	}
	defer target.Close()
	// The source and destination descriptors pin both roots. GNU cp preserves
	// whiteouts, ownership, links and xattrs; reflinks have independent inodes.
	command := exec.CommandContext(ctx, "cp", "-a", "--reflink=auto", "--", "/proc/self/fd/3/.", "/proc/self/fd/4/")
	command.ExtraFiles = []*os.File{source, target}
	command.WaitDelay = time.Second
	if output, err := command.CombinedOutput(); err != nil {
		return expected, fmt.Errorf("clone filesystem backing: %w: %.4096s", err, output)
	}
	actual, err := fingerprintTree(ctx, target, source, true)
	if err != nil {
		return expected, err
	}
	if !sameFilesystemTree(actual, expected) {
		return expected, fmt.Errorf("complete filesystem copy differs from its frozen source")
	}
	current, err := os.Lstat(destination)
	saved, statErr := target.Stat()
	if err != nil || statErr != nil || !os.SameFile(current, saved) {
		return expected, fmt.Errorf("filesystem destination identity changed: %v %v", err, statErr)
	}
	return expected, nil
}

type treeEntry struct {
	Path                    string `json:"path"`
	Mode                    uint32 `json:"mode"`
	UID, GID                uint32
	Size                    int64  `json:"size,omitempty"`
	Rdev                    uint64 `json:"rdev,omitempty"`
	MtimeSec, MtimeNano     int64
	Link, Hardlink, Content string
	Xattrs                  map[string][]byte `json:"xattrs,omitempty"`
}

func fingerprintTree(ctx context.Context, root, independentFrom *os.File, syncFiles bool) (FilesystemTree, error) {
	result := FilesystemTree{}
	hash := sha256.New()
	encoder := json.NewEncoder(hash)
	type hardlinkContent struct{ Path, SHA256 string }
	links := map[[2]uint64]hardlinkContent{}
	var rootStat unix.Statx_t
	if err := unix.Statx(int(root.Fd()), "", unix.AT_EMPTY_PATH, unix.STATX_MNT_ID, &rootStat); err != nil {
		return result, err
	}
	if rootStat.Mask&unix.STATX_MNT_ID == 0 || rootStat.Mnt_id == 0 {
		return result, fmt.Errorf("filesystem root mount identity is unknown")
	}
	var visit func(*os.File, string, int) error
	visit = func(directory *os.File, relative string, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if depth > 256 || result.Entries >= 1000000 {
			return fmt.Errorf("filesystem tree exceeds capture bounds")
		}
		var stat unix.Stat_t
		if err := unix.Fstat(int(directory.Fd()), &stat); err != nil {
			return err
		}
		entry := treeEntry{Path: relative, Mode: stat.Mode, UID: stat.Uid, GID: stat.Gid,
			MtimeSec: stat.Mtim.Sec, MtimeNano: stat.Mtim.Nsec}
		attrs, err := treeXattrs("/proc/self/fd/" + strconv.Itoa(int(directory.Fd())))
		if err != nil {
			return err
		}
		entry.Xattrs = attrs
		if err := independentEntry(independentFrom, relative, stat); err != nil {
			return err
		}
		if err := encoder.Encode(entry); err != nil {
			return err
		}
		result.Entries++
		fd, err := unix.Openat(int(directory.Fd()), ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NOATIME, 0)
		if err != nil {
			return err
		}
		list := os.NewFile(uintptr(fd), "filesystem entries")
		entries, readErr := list.ReadDir(1000001)
		closeErr := list.Close()
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return errors.Join(readErr, closeErr)
		}
		if closeErr != nil || len(entries) > 1000000 {
			return errors.Join(closeErr, fmt.Errorf("filesystem directory exceeds capture bounds"))
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, child := range entries {
			if err := ctx.Err(); err != nil {
				return err
			}
			name := child.Name()
			path := name
			if relative != "." {
				path = relative + "/" + name
			}
			var extended unix.Statx_t
			if err := unix.Statx(int(directory.Fd()), name, unix.AT_SYMLINK_NOFOLLOW, unix.STATX_MNT_ID, &extended); err != nil {
				return err
			}
			if extended.Mask&unix.STATX_MNT_ID == 0 || extended.Mnt_id != rootStat.Mnt_id {
				return fmt.Errorf("filesystem capture crosses a foreign mount: %s", path)
			}
			if err := unix.Fstatat(int(directory.Fd()), name, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
				return err
			}
			if stat.Mode&unix.S_IFMT == unix.S_IFDIR {
				fd, err := unix.Openat(int(directory.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NOATIME, 0)
				if err != nil {
					return err
				}
				subdirectory := os.NewFile(uintptr(fd), name)
				visitErr := visit(subdirectory, path, depth+1)
				if err := errors.Join(visitErr, subdirectory.Close()); err != nil {
					return err
				}
				continue
			}
			if err := independentEntry(independentFrom, path, stat); err != nil {
				return err
			}
			node := treeEntry{Path: path, Mode: stat.Mode, UID: stat.Uid, GID: stat.Gid,
				MtimeSec: stat.Mtim.Sec, MtimeNano: stat.Mtim.Nsec}
			node.Xattrs, err = treeXattrs("/proc/self/fd/" + strconv.Itoa(int(directory.Fd())) + "/" + name)
			if err != nil {
				return err
			}
			switch stat.Mode & unix.S_IFMT {
			case unix.S_IFREG:
				node.Size = stat.Size
				result.FileBytes += stat.Size
				key := [2]uint64{uint64(stat.Dev), stat.Ino}
				if stat.Nlink > 1 {
					if first, ok := links[key]; ok {
						node.Hardlink, node.Content = first.Path, first.SHA256
						break
					}
				}
				fd, err := unix.Openat(int(directory.Fd()), name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NOATIME, 0)
				if err != nil {
					return err
				}
				file := os.NewFile(uintptr(fd), name)
				payload := sha256.New()
				_, copyErr := io.Copy(payload, contextFileReader{ctx, file})
				var syncErr error
				if syncFiles {
					syncErr = file.Sync()
				}
				closeErr := file.Close()
				if err := errors.Join(copyErr, syncErr, closeErr); err != nil {
					return err
				}
				node.Content = hex.EncodeToString(payload.Sum(nil))
				result.HashedBytes += stat.Size
				if stat.Nlink > 1 {
					links[key] = hardlinkContent{path, node.Content}
				}
			case unix.S_IFLNK:
				buffer := make([]byte, 4097)
				n, err := unix.Readlinkat(int(directory.Fd()), name, buffer)
				if err != nil || n > 4096 {
					return fmt.Errorf("invalid captured symlink %s: %v", path, err)
				}
				node.Link = string(buffer[:n])
			case unix.S_IFCHR, unix.S_IFBLK:
				node.Rdev = uint64(stat.Rdev)
			case unix.S_IFIFO, unix.S_IFSOCK:
			default:
				return fmt.Errorf("unsupported filesystem node %s", path)
			}
			if err := encoder.Encode(node); err != nil {
				return err
			}
			result.Entries++
		}
		if syncFiles {
			return directory.Sync()
		}
		return nil
	}
	if err := visit(root, ".", 0); err != nil {
		return result, err
	}
	result.SHA256 = hex.EncodeToString(hash.Sum(nil))
	return result, nil
}

type contextFileReader struct {
	context context.Context
	reader  io.Reader
}

func (reader contextFileReader) Read(data []byte) (int, error) {
	if err := reader.context.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(data)
}

func independentEntry(source *os.File, path string, captured unix.Stat_t) error {
	if source == nil {
		return nil
	}
	var original unix.Stat_t
	if err := unix.Fstatat(int(source.Fd()), path, &original, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if original.Dev == captured.Dev && original.Ino == captured.Ino {
		return fmt.Errorf("filesystem copy shares its source inode: %s", path)
	}
	return nil
}

func treeXattrs(path string) (map[string][]byte, error) {
	// /proc/self/fd/N is intentionally followed for directories. The final
	// guest-controlled name uses l* operations so symlinks remain data.
	directoryFD := !strings.Contains(strings.TrimPrefix(path, "/proc/self/fd/"), "/")
	var size int
	var err error
	if directoryFD {
		size, err = unix.Listxattr(path, nil)
	} else {
		size, err = unix.Llistxattr(path, nil)
	}
	if err != nil {
		return nil, err
	}
	if size > 1<<20 {
		return nil, fmt.Errorf("filesystem xattr names exceed bound")
	}
	names := make([]byte, size)
	if directoryFD {
		size, err = unix.Listxattr(path, names)
	} else {
		size, err = unix.Llistxattr(path, names)
	}
	if err != nil {
		return nil, err
	}
	attrs := map[string][]byte{}
	for _, name := range strings.Split(string(names[:size]), "\x00") {
		if name == "" {
			continue
		}
		var length int
		if directoryFD {
			length, err = unix.Getxattr(path, name, nil)
		} else {
			length, err = unix.Lgetxattr(path, name, nil)
		}
		if err != nil {
			return nil, err
		}
		if length > 1<<20 {
			return nil, fmt.Errorf("filesystem xattr value exceeds bound")
		}
		value := make([]byte, length)
		if directoryFD {
			_, err = unix.Getxattr(path, name, value)
		} else {
			_, err = unix.Lgetxattr(path, name, value)
		}
		if err != nil {
			return nil, err
		}
		attrs[name] = value
	}
	if len(attrs) == 0 {
		return nil, nil
	}
	return attrs, nil
}
