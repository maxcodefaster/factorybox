package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

var descendantCaptureGeneration = regexp.MustCompile(`^images-[0-9a-f]{32}$`)

// This directory is only the fork's transport capture. The independently
// owned clone has already passed complete hashes and keeps the full payload.
// Original restore images and archived stopped ancestors remain untouched.
func descendantRetireTransport(source descendantLayout, images, evidence string) (map[string]any, error) {
	owned, err := descendantOwnedLayout(source.ID)
	if err != nil || owned != source || filepath.Dir(images) != source.Root ||
		!descendantCaptureGeneration.MatchString(filepath.Base(images)) {
		return nil, fmt.Errorf("transport retirement is outside its owned capture generation")
	}
	for _, daemon := range criuLazyDaemons {
		if filepath.Dir(daemon.logPath) == images {
			return nil, fmt.Errorf("transport retirement found a page-daemon reference")
		}
	}
	fd, err := unix.Open(images, unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	directory := os.NewFile(uintptr(fd), images)
	defer directory.Close()
	var fs unix.Statfs_t
	if err := unix.Fstatfs(fd, &fs); err != nil || fs.Type == unix.TMPFS_MAGIC {
		return nil, fmt.Errorf("transport retirement requires file backing: %v", err)
	}
	entries, err := directory.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	sizes := map[string]int64{}
	var logical, allocated int64
	for _, entry := range entries {
		name := entry.Name()
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || info.Size() < 0 ||
			(!strings.HasSuffix(name, ".img") && name != "runtime-config.json" && name != "descriptors.json" &&
				name != "dump.log" && name != "stats-dump") {
			return nil, fmt.Errorf("unexpected transport entry %q: %v", name, err)
		}
		if info.Size() > int64(criuRAMSnapshotLimit)-logical {
			return nil, fmt.Errorf("transport retirement exceeds snapshot bound")
		}
		logical += info.Size()
		allocated += info.Sys().(*syscall.Stat_t).Blocks * 512
		sizes[name] = info.Size()
	}
	for _, name := range []string{"inventory.img", "pstree.img", "runtime-config.json", "descriptors.json"} {
		if sizes[name] == 0 {
			return nil, fmt.Errorf("transport retirement lacks complete metadata")
		}
	}
	metadata := filepath.Join(evidence, "retired-source-capture")
	if err := os.Mkdir(metadata, 0700); err != nil {
		return nil, err
	}
	var preserved int64
	for _, name := range []string{"inventory.img", "pstree.img", "runtime-config.json", "descriptors.json", "dump.log", "stats-dump"} {
		size, exists := sizes[name]
		if !exists {
			continue
		}
		if size > 64<<20-preserved {
			return nil, fmt.Errorf("transport diagnostic metadata exceeds fixed bound")
		}
		readFD, err := unix.Openat(fd, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			return nil, err
		}
		input := os.NewFile(uintptr(readFD), name)
		output, err := os.OpenFile(filepath.Join(metadata, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			input.Close()
			return nil, err
		}
		_, copyErr := io.CopyN(output, input, size)
		if err := errors.Join(copyErr, input.Close(), output.Sync(), output.Close()); err != nil {
			return nil, err
		}
		preserved += size
	}
	for _, entry := range entries {
		if err := unix.Unlinkat(fd, entry.Name(), 0); err != nil {
			return nil, err
		}
	}
	if err := os.Remove(images); err != nil {
		return nil, err
	}
	if _, err := os.Lstat(images); !os.IsNotExist(err) {
		return nil, fmt.Errorf("retired transport capture still exists: %v", err)
	}
	return map[string]any{
		"retired": true, "images": images, "logical_bytes": logical,
		"accounted_inode_allocated_bytes": allocated,
		"retained_metadata":               metadata, "retained_metadata_bytes": preserved,
		"scope": "Only this fork's verified transport capture is unlinked. Full clone, original restore images and stopped-ancestor backing remain. Inode accounting removed is not a measurement of physically freed Btrfs extents.",
	}, nil
}
