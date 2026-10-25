//go:build linux

package sysboxcapture

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

// SharedSourceAnnotation admits one independently owned, mutable project
// directory. Its contents are deliberately absent from private snapshots.
const SharedSourceAnnotation = "io.factorybox.shared-source"

type SharedSource struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Device      uint64 `json:"device"`
	Inode       uint64 `json:"inode"`
	MountID     uint64 `json:"mount_id"`
}

func SharedSourcePaths(annotations map[string]string) (*SharedSource, error) {
	encoded, present := annotations[SharedSourceAnnotation]
	if !present {
		return nil, nil
	}
	var paths struct {
		Source      string `json:"source"`
		Destination string `json:"destination"`
	}
	decoder := json.NewDecoder(bytes.NewBufferString(encoded))
	decoder.DisallowUnknownFields()
	if len(encoded) > 8192 || decoder.Decode(&paths) != nil || decoder.Decode(new(any)) != io.EOF {
		return nil, fmt.Errorf("shared source requires exactly source and destination paths")
	}
	if !cleanAbsolute(paths.Source) || !cleanAbsolute(paths.Destination) || paths.Source == "/" || paths.Destination == "/" {
		return nil, fmt.Errorf("shared source requires canonical non-root paths")
	}
	for _, reserved := range []string{"/proc", "/sys", "/dev", "/run", "/var/lib/docker", "/var/lib/sysbox"} {
		if paths.Destination == reserved || strings.HasPrefix(paths.Destination, reserved+"/") || strings.HasPrefix(reserved, paths.Destination+"/") {
			return nil, fmt.Errorf("shared source overlaps a runtime-managed destination")
		}
	}
	return &SharedSource{Source: paths.Source, Destination: paths.Destination}, nil
}

// IdentifySharedSource pins the directory and mount, rather than its mutable
// contents. Replacing the root or moving it to another mount is not an edit.
func IdentifySharedSource(source SharedSource) (*SharedSource, error) {
	fd, err := unix.Openat2(unix.AT_FDCWD, source.Source, &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_NO_SYMLINKS})
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "shared source root")
	defer file.Close()
	var stat unix.Stat_t
	var extended unix.Statx_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return nil, err
	}
	if err := unix.Statx(fd, "", unix.AT_EMPTY_PATH, unix.STATX_MNT_ID, &extended); err != nil {
		return nil, err
	}
	if extended.Mask&unix.STATX_MNT_ID == 0 || extended.Mnt_id == 0 {
		return nil, fmt.Errorf("shared source mount identity is unavailable")
	}
	source.Device, source.Inode, source.MountID = uint64(stat.Dev), stat.Ino, extended.Mnt_id
	return &source, nil
}

func VerifySharedSource(source *SharedSource) error {
	if source == nil {
		return nil
	}
	if !cleanAbsolute(source.Source) || !cleanAbsolute(source.Destination) || source.Device == 0 || source.Inode == 0 || source.MountID == 0 {
		return fmt.Errorf("shared source has no pinned directory identity")
	}
	actual, err := IdentifySharedSource(*source)
	if err != nil {
		return err
	}
	if *actual != *source {
		return fmt.Errorf("shared source directory or mount identity changed")
	}
	return nil
}

var ErrSharedSourceCopy = errors.New("shared source supports same-worker pause/resume only; independent fork or recovery requires a separate source contract")
