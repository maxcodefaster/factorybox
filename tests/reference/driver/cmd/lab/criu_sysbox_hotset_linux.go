//go:build linux

package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

var criuHotTraceExtent int64
var criuHotTraceRegistrationTime time.Duration

func criuHotReadFile(path string, limit int64) ([]byte, os.FileInfo, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return nil, nil, fmt.Errorf("hot-page input is not a bounded regular file: %s: %v", path, err)
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if int64(len(data)) > limit {
		err = errors.Join(err, fmt.Errorf("hot-page input grew beyond limit"))
	}
	return data, info, err
}

func criuSysboxRememberHotTrace(images string) error {
	if !criuHotsetEnabled() {
		return nil
	}
	start := time.Now()
	defer func() { criuHotTraceRegistrationTime += time.Since(start) }()
	if images != "/experiment/sysbox/images" || criuHotTraceExtent != 0 {
		return fmt.Errorf("startup trace requires the first owned source readiness")
	}
	// Stat only here. Parsing and planning are deferred to the charged fork.
	info, err := os.Lstat(filepath.Join(images, "lazy-pages.log"))
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 4<<20 {
		return fmt.Errorf("invalid startup trace extent: %v", err)
	}
	criuHotTraceExtent = info.Size()
	return nil
}

func criuHotRewriteMap(path, original string, hot []criuHotRange, budget int64) (pages uint64, resultErr error) {
	data, info, err := criuHotReadFile(path, 8<<20)
	if err != nil {
		return 0, err
	}
	parent, err := os.Lstat(original)
	if err != nil || !parent.Mode().IsRegular() || os.SameFile(info, parent) {
		return 0, fmt.Errorf("child pagemap must have a private inode: %v", err)
	}
	saved, err := criuReadSavedMap(bytes.NewReader(data))
	if err != nil {
		return 0, err
	}
	changed, pages, err := criuApplyHotHints(saved, hot, budget)
	if err != nil || pages == 0 {
		return pages, err
	}
	encoded, err := criuEncodeSavedMap(changed)
	if err != nil {
		return 0, err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".hot-pages-")
	if err != nil {
		return 0, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, file.Close())
		os.Remove(file.Name())
	}()
	owner := info.Sys().(*syscall.Stat_t)
	if err := file.Chown(int(owner.Uid), int(owner.Gid)); err != nil {
		return 0, err
	}
	if _, err := file.Write(encoded); err != nil {
		return 0, err
	}
	if err := file.Chmod(info.Mode().Perm()); err != nil {
		return 0, err
	}
	latest, err := os.Lstat(path)
	if err != nil || !os.SameFile(info, latest) {
		return 0, fmt.Errorf("private pagemap changed while preparing hints")
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return 0, err
	}
	return pages, nil
}

func criuSysboxApplyHotset(sourceImages, images string) (resultErr error) {
	if !criuHotsetEnabled() {
		return nil
	}
	start := time.Now()
	var selected uint64
	var hinted, aliases int
	defer func() {
		record("sysbox-child-startup-hot-pages", start, asJSON(map[string]any{
			"trace_prefix_bytes": criuHotTraceExtent, "selected_pages": selected,
			"budget_bytes": criuHotsetBudget, "hinted_maps": hinted, "resolved_aliases": aliases,
			"scope": "first complete source readiness trace only; hints select current child bytes, preserve immutable parent and provenance; 64MiB shared maximum; all planning and metadata rewriting inside charged request",
		}), resultErr)
	}()
	if sourceImages != "/experiment/sysbox/images-again" || images != "/experiment/sysbox-copy/images" ||
		criuHotTraceExtent <= 0 || criuHotTraceExtent > 4<<20 {
		return fmt.Errorf("hot-page plan is outside the owned second-generation child")
	}
	// The daemon may still append, but no bytes after the readiness boundary
	// can enter the training data, including later validation or child work.
	fd, err := unix.Open("/experiment/sysbox/images/lazy-pages.log", unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	log := os.NewFile(uintptr(fd), "source-startup-pages")
	prefix, err := io.ReadAll(io.NewSectionReader(log, 0, criuHotTraceExtent))
	log.Close()
	if err != nil {
		return err
	}
	if len(prefix) != int(criuHotTraceExtent) {
		return fmt.Errorf("startup trace prefix was truncated")
	}
	if end := bytes.LastIndexByte(prefix, '\n'); end >= 0 {
		prefix = prefix[:end+1]
	} else {
		return fmt.Errorf("startup trace has no complete lines")
	}
	hot, err := criuReadHotTrace(bytes.NewReader(prefix))
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(images)
	if err != nil {
		return err
	}
	type plan struct {
		name  string
		hot   []criuHotRange
		pages uint64
	}
	var plans []plan
	var total uint64
	for _, entry := range entries {
		if !criuInheritAliasName.MatchString(entry.Name()) {
			continue
		}
		data, _, err := criuHotReadFile(filepath.Join(images, entry.Name()), 16)
		if err != nil || len(data) != 16 || binary.LittleEndian.Uint64(data[:8]) != 0x4c415a59494e4832 {
			return fmt.Errorf("invalid child page identity alias: %s: %v", entry.Name(), err)
		}
		parent := binary.LittleEndian.Uint64(data[8:])
		if parent == 0 || parent > 2147483647 {
			return fmt.Errorf("invalid startup trace PID identity")
		}
		pid, err := strconv.ParseUint(entry.Name()[12:len(entry.Name())-4], 10, 31)
		if err != nil || pid == 0 {
			return fmt.Errorf("invalid child alias PID")
		}
		aliases++
		name := fmt.Sprintf("pagemap-%d.img", pid)
		data, _, err = criuHotReadFile(filepath.Join(images, name), 8<<20)
		if err != nil {
			return err
		}
		saved, err := criuReadSavedMap(bytes.NewReader(data))
		if err != nil {
			return err
		}
		_, pages, err := criuApplyHotHints(saved, hot[int(parent)], criuHotsetBudget)
		if err != nil {
			return err
		}
		if pages > 0 {
			total += pages
			plans = append(plans, plan{name, hot[int(parent)], pages})
		}
	}
	for _, plan := range plans {
		pages := plan.pages
		if total > criuHotsetBudget/4096 {
			// Distribute the fixed total proportionally across actual hinted
			// MMs, rather than letting low-numbered PIDs consume everything.
			pages = plan.pages * (criuHotsetBudget / 4096) / total
		}
		count, err := criuHotRewriteMap(filepath.Join(images, plan.name), filepath.Join(sourceImages, plan.name),
			plan.hot, int64(pages*4096))
		if err != nil {
			return err
		}
		selected += count
		if count > 0 {
			hinted++
		}
	}
	if selected == 0 || selected*4096 > criuHotsetBudget {
		return fmt.Errorf("hot-page selection is empty or exceeds global budget")
	}
	return nil
}
