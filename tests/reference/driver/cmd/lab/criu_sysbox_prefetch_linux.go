//go:build linux

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

func criuStartEagerPrefetch(images string) (func() error, error) {
	if os.Getenv("CRIU_SYSBOX_EAGER_PREFETCH") != "1" {
		return func() error { return nil }, nil
	}
	if images != criuRoot+"/sysbox/images" && images != criuRoot+"/sysbox-copy/images" && images != criuRoot+"/sysbox/images-again" {
		return nil, fmt.Errorf("eager prefetch is outside the owned image paths")
	}
	start := time.Now()
	// Parent locations use the same guarded, immutable one-generation chain
	// as the actual restore. No caller-supplied links are followed.
	parent := ""
	if _, err := os.Lstat(filepath.Join(images, "parent")); err == nil {
		if err := criuSysboxParentLink(images); err != nil {
			return nil, err
		}
		parent = filepath.Join(filepath.Dir(images), "images-base")
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	done := make(chan error, 1)
	go func() {
		var bytes int64
		plan, err := criuBuildEagerPlan(images, parent)
		if err == nil {
			bytes, err = criuReadPrefetch(plan)
		}
		record("sysbox-eager-working-set-prefetch", start, asJSON(map[string]any{
			"images": images, "bytes": bytes, "budget_bytes": criuEagerPrefetchBudget, "extents": len(plan),
			"scope": "read-only eager saved extents, including immutable parent bytes; one 256KiB buffer, <=64MiB requested cache; overlaps daemon/runtime/volume preparation; completion joined before readiness; no source hydration or lazy page population",
		}), err)
		done <- err
	}()
	return func() error { return <-done }, nil
}

func criuOpenPrefetchFile(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, fmt.Errorf("prefetch requires a regular immutable saved file: %s: %v", path, err)
	}
	return file, nil
}

func criuBuildEagerPlan(images, parent string) ([]criuPrefetchRange, error) {
	paths, err := filepath.Glob(filepath.Join(images, "pagemap-*.img"))
	if err != nil || len(paths) == 0 || len(paths) > 4096 {
		return nil, fmt.Errorf("invalid eager pagemap set: %v", err)
	}
	read := func(path string) (criuSavedMap, error) {
		file, err := criuOpenPrefetchFile(path)
		if err != nil {
			return criuSavedMap{}, err
		}
		defer file.Close()
		return criuReadSavedMap(file)
	}
	var ranges []criuPrefetchRange
	for _, path := range paths {
		current, err := read(path)
		if err != nil {
			return nil, err
		}
		var base criuSavedMap
		for _, entry := range current.Ranges {
			if entry.Flags&3 == 1 {
				if parent == "" {
					return nil, fmt.Errorf("eager inherited range without an immutable parent")
				}
				base, err = read(filepath.Join(parent, filepath.Base(path)))
				if err != nil {
					return nil, err
				}
				break
			}
		}
		currentFile := filepath.Join(images, fmt.Sprintf("pages-%d.img", current.PageID))
		parentFile := filepath.Join(parent, fmt.Sprintf("pages-%d.img", base.PageID))
		selected, err := criuEagerRanges(current, base, currentFile, parentFile)
		if err != nil {
			return nil, err
		}
		ranges = append(ranges, selected...)
	}
	return criuBoundPrefetch(ranges, criuEagerPrefetchBudget)
}

func criuReadPrefetch(plan []criuPrefetchRange) (bytes int64, err error) {
	buffer := make([]byte, 256<<10)
	files := map[string]*os.File{}
	defer func() {
		for _, file := range files {
			err = errors.Join(err, file.Close())
		}
	}()
	for _, extent := range plan {
		file := files[extent.File]
		if file == nil {
			file, err = criuOpenPrefetchFile(extent.File)
			if err != nil {
				return bytes, err
			}
			files[extent.File] = file
		}
		if extent.Bytes > int64(len(buffer)) {
			return bytes, fmt.Errorf("prefetch extent exceeds buffer")
		}
		n, readErr := file.ReadAt(buffer[:extent.Bytes], extent.Offset)
		bytes += int64(n)
		if readErr != nil || int64(n) != extent.Bytes {
			return bytes, errors.Join(io.ErrUnexpectedEOF, readErr)
		}
	}
	return bytes, nil
}
