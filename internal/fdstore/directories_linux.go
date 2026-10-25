//go:build linux

package fdstore

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

const SourceRootName = "factorybox-source-root:1"

// Directory records the original namespace's bind root and mount point. The
// live FD supplies the inode; these strings never grant object identity.
type Directory struct {
	Name              string `json:"name"`
	MountPoint        string `json:"mount_point"`
	MountRootRelative string `json:"mount_root_relative"`
}

func beneath(path string) bool {
	return path != "" && !filepath.IsAbs(path) && filepath.Clean(path) == path && path != ".." && !strings.HasPrefix(path, "../")
}

func directoryStat(file *os.File) (unix.Stat_t, error) {
	var stat unix.Stat_t
	err := unix.Fstat(int(file.Fd()), &stat)
	if err == nil && stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		err = fmt.Errorf("retained source directory is not a directory")
	}
	return stat, err
}

func validateDirectories(names []string, files []*os.File, directories []Directory) error {
	if len(names) != len(files) {
		return fmt.Errorf("retained source directory set differs")
	}
	indices := map[string]int{}
	for index, name := range names {
		indices[name] = index
	}
	rootIndex, hasRoot := indices[SourceRootName]
	if hasRoot != (len(directories) > 0) {
		return fmt.Errorf("retained directories require their canonical source root")
	}
	var root unix.Stat_t
	if hasRoot {
		var err error
		root, err = directoryStat(files[rootIndex])
		if err != nil {
			return err
		}
	}
	seen := map[string]bool{}
	for _, directory := range directories {
		index, exists := indices[directory.Name]
		if !exists || !strings.HasPrefix(directory.Name, "factorybox-dir:") || seen[directory.Name] ||
			!filepath.IsAbs(directory.MountPoint) || filepath.Clean(directory.MountPoint) != directory.MountPoint ||
			strings.ContainsAny(directory.MountPoint, "\x00\r\n") || !beneath(directory.MountRootRelative) {
			return fmt.Errorf("invalid retained source directory mapping")
		}
		stat, err := directoryStat(files[index])
		if err != nil || stat.Dev != root.Dev {
			return fmt.Errorf("retained directory crosses its source filesystem")
		}
		seen[directory.Name] = true
	}
	for _, name := range names {
		if strings.HasPrefix(name, "factorybox-dir:") && !seen[name] {
			return fmt.Errorf("retained source directory mapping is missing")
		}
	}
	return nil
}

func relativeTo(root, path string) (string, bool) {
	relative, err := filepath.Rel(root, path)
	return relative, err == nil && beneath(relative)
}

// ResolveDirectory follows a renamed inode through the original directory FD,
// then opens it beneath the pinned canonical root without following symlinks.
// The fresh path is usable in the restored mount namespace. Reusing the old
// mount FD as cwd would instead produce an unreachable working directory.
func ResolveDirectory(root, original *os.File, directory Directory) (string, error) {
	rootPath, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", root.Fd()))
	if err != nil {
		return "", err
	}
	path, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", original.Fd()))
	if err != nil || strings.HasSuffix(path, " (deleted)") {
		return "", fmt.Errorf("shared working directory was deleted or detached from its source")
	}
	expected, err := directoryStat(original)
	if err != nil {
		return "", err
	}
	var candidates []string
	if relative, ok := relativeTo(rootPath, path); ok {
		candidates = append(candidates, relative)
	}
	if relative, ok := relativeTo(directory.MountPoint, path); ok {
		candidates = append(candidates, filepath.Join(directory.MountRootRelative, relative))
	}
	// A mount whose namespace has exited reports its path relative to that
	// mount's root. It is still accepted only after the exact inode comparison.
	candidates = append(candidates, filepath.Join(directory.MountRootRelative, strings.TrimPrefix(path, "/")))
	for _, relative := range candidates {
		if !beneath(relative) {
			continue
		}
		fd, err := unix.Openat2(int(root.Fd()), relative, &unix.OpenHow{
			Flags:   unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC,
			Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_XDEV,
		})
		if err != nil {
			continue
		}
		var actual unix.Stat_t
		err = unix.Fstat(fd, &actual)
		unix.Close(fd)
		withinMount, ok := relativeTo(directory.MountRootRelative, relative)
		if err == nil && actual.Dev == expected.Dev && actual.Ino == expected.Ino && ok {
			return filepath.Join(directory.MountPoint, withinMount), nil
		}
	}
	return "", fmt.Errorf("shared working directory moved outside its admitted source or changed identity")
}

func restoreNames(saved generation) ([]string, error) {
	names := append([]string{}, saved.names...)
	if len(saved.directories) == 0 {
		return names, nil
	}
	indices := map[string]int{}
	for index, name := range saved.names {
		indices[name] = index
	}
	root := saved.files[indices[SourceRootName]]
	for _, directory := range saved.directories {
		index := indices[directory.Name]
		path, err := ResolveDirectory(root, saved.files[index], directory)
		if err != nil {
			return nil, err
		}
		if len(path) >= unix.PathMax || strings.IndexByte(path, 0) >= 0 {
			return nil, fmt.Errorf("shared directory path exceeds its restore bound")
		}
		names[index] += ":" + hex.EncodeToString([]byte(path))
	}
	return names, nil
}
