package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

const descendantOracleMagic = uint64(0x4c494e484f524331)

type descendantOraclePlan struct {
	ImagePID, ParentPID uint64
	Ranges              []criuHotRange
}

type descendantOracleCopy struct {
	Offset, Bytes int64
}

func descendantReadOraclePlan(path, key string) (_ descendantOraclePlan, resultErr error) {
	var result descendantOraclePlan
	pid, err := strconv.ParseUint(key, 10, 31)
	if err != nil || pid == 0 {
		return result, fmt.Errorf("oracle plan has no canonical image PID")
	}
	file, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return result, err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 32 || info.Size() > 32+262144*16 {
		return result, fmt.Errorf("invalid full-writer inheritance plan file: %v", err)
	}
	var header [4]uint64
	if err := binary.Read(file, binary.LittleEndian, &header); err != nil {
		return result, err
	}
	if header[0] != descendantOracleMagic || header[1] != pid || header[2] > 1<<31-1 ||
		header[3] > 262144 || int64(32+header[3]*16) != info.Size() ||
		(header[3] > 0 && header[2] == 0) {
		return result, fmt.Errorf("incomplete or mismatched full-writer inheritance plan")
	}
	result.ImagePID, result.ParentPID = pid, header[2]
	for index := uint64(0); index < header[3]; index++ {
		var entry criuHotRange
		if err := binary.Read(file, binary.LittleEndian, &entry); err != nil {
			return result, err
		}
		if entry.Start >= entry.End || entry.Start%4096 != 0 || entry.End%4096 != 0 ||
			(len(result.Ranges) > 0 && entry.Start < result.Ranges[len(result.Ranges)-1].End) {
			return result, fmt.Errorf("invalid or overlapping full-writer inheritance ranges")
		}
		result.Ranges = append(result.Ranges, entry)
	}
	return result, nil
}

func descendantApplyOraclePlan(original criuSavedMap, plan []criuHotRange) (
	criuSavedMap, []descendantOracleCopy, uint64, error) {
	derived := criuSavedMap{PageID: original.PageID}
	var copies []descendantOracleCopy
	var inherited, expected uint64
	for index, entry := range plan {
		if entry.Start >= entry.End || entry.Start%4096 != 0 || entry.End%4096 != 0 ||
			(index > 0 && entry.Start < plan[index-1].End) {
			return derived, nil, 0, fmt.Errorf("invalid same-capture inheritance selection")
		}
		expected += entry.End - entry.Start
	}
	index, localOffset := 0, int64(0)
	for _, entry := range original.Ranges {
		if entry.Flags&4 == 0 || entry.Flags&1 != 0 {
			return derived, nil, 0, fmt.Errorf("same-capture byte oracle must be a complete original writer")
		}
		cursor, end := entry.Address, entry.Address+entry.Pages*4096
		for cursor < end {
			for index < len(plan) && plan[index].End <= cursor {
				index++
			}
			stop, flags := end, entry.Flags
			selected := index < len(plan) && plan[index].Start <= cursor && cursor < plan[index].End
			if selected {
				stop = min(stop, plan[index].End)
				flags = entry.Flags&2 | 1
				inherited += stop - cursor
			} else {
				if index < len(plan) {
					stop = min(stop, plan[index].Start)
				}
				if stop <= cursor {
					return derived, nil, 0, fmt.Errorf("inheritance plan does not advance through the original writer")
				}
				copies = append(copies, descendantOracleCopy{
					Offset: entry.Offset + int64(cursor-entry.Address), Bytes: int64(stop - cursor)})
			}
			derived.Ranges = append(derived.Ranges, criuSavedRange{
				Address: cursor, Pages: (stop - cursor) / 4096, Flags: flags, Offset: localOffset})
			if !selected {
				localOffset += int64(stop - cursor)
			}
			cursor = stop
		}
	}
	if inherited != expected {
		return derived, nil, 0, fmt.Errorf("inheritance selection includes bytes outside the complete writer")
	}
	return derived, copies, inherited, nil
}

func descendantOracleWrite(path string, data []byte, owner descendantLeaseIdentity) (resultErr error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	if _, err := file.Write(data); err != nil {
		return err
	}
	return errors.Join(file.Chown(int(owner.UID), int(owner.GID)), file.Sync())
}

func descendantOracleCopyFile(source, target string, spans []descendantOracleCopy,
	owner descendantLeaseIdentity) (written int64, resultErr error) {
	file, err := os.OpenFile(source, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return 0, err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return 0, fmt.Errorf("original writer is not a regular payload: %v", err)
	}
	if spans == nil {
		spans = []descendantOracleCopy{{Bytes: info.Size()}}
	}
	output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return 0, err
	}
	defer func() { resultErr = errors.Join(resultErr, output.Close()) }()
	buffer := make([]byte, 64<<10)
	for _, span := range spans {
		if span.Offset < 0 || span.Bytes < 0 || span.Offset > info.Size() ||
			span.Bytes > info.Size()-span.Offset {
			return written, fmt.Errorf("full-writer copy extent is outside its original payload")
		}
		count, err := io.CopyBuffer(output, io.NewSectionReader(file, span.Offset, span.Bytes), buffer)
		written += count
		if err != nil || count != span.Bytes {
			return written, fmt.Errorf("incomplete original-writer extent copy: %d/%d %v", count, span.Bytes, err)
		}
	}
	return written, errors.Join(output.Chown(int(owner.UID), int(owner.GID)), output.Sync())
}

// This diagnostic derives the exact proposed image from decisions recorded
// while the original CRIU writer collected those same bytes. The direct
// inheritance path remains separate and has not passed this oracle yet.
func descendantDeriveInheritedOracle(original, target string) (_ map[string]any, resultErr error) {
	if filepath.Clean(original) != original || filepath.Clean(target) != target ||
		!strings.HasPrefix(original, "/experiment/") || filepath.Dir(original) != filepath.Dir(target) {
		return nil, fmt.Errorf("same-capture oracle paths are outside one private image root")
	}
	actual, err := filepath.EvalSymlinks(original)
	if err != nil || actual != original {
		return nil, fmt.Errorf("original writer directory is not canonical: %v", err)
	}
	info, err := os.Lstat(original)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("original writer is not a real image directory: %v", err)
	}
	owner := descendantLeaseFileIdentity(info)
	link, err := os.Readlink(filepath.Join(original, "parent"))
	if err != nil || filepath.IsAbs(link) {
		return nil, fmt.Errorf("same-capture selection has no relative original parent: %v", err)
	}
	parent, err := filepath.EvalSymlinks(filepath.Join(original, link))
	if err != nil || !descendantGraphWithin(filepath.Dir(original), parent) {
		return nil, fmt.Errorf("same-capture parent escaped its private image root: %v", err)
	}
	var fs unix.Statfs_t
	if err := unix.Statfs(parent, &fs); err != nil || fs.Flags&unix.ST_RDONLY == 0 {
		return nil, fmt.Errorf("same-capture parent is not pinned readonly: %v", err)
	}
	if err := os.Mkdir(target, 0700); err != nil {
		return nil, err
	}
	created, err := os.Stat(target)
	if err != nil {
		return nil, err
	}
	defer func() {
		if resultErr != nil {
			if current, err := os.Lstat(target); err == nil && os.SameFile(created, current) {
				resultErr = errors.Join(resultErr, os.RemoveAll(target))
			}
		}
	}()
	entries, err := os.ReadDir(original)
	if err != nil {
		return nil, err
	}
	consumed := map[string]bool{}
	var inherited, local uint64
	maps, plans := 0, 0
	for _, entry := range entries {
		match := descendantMemoryName.FindStringSubmatch(entry.Name())
		if match == nil {
			continue
		}
		file, err := os.OpenFile(filepath.Join(original, entry.Name()),
			os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
		if err != nil {
			return nil, err
		}
		saved, parseErr := criuReadSavedMap(file)
		if err := errors.Join(parseErr, file.Close()); err != nil {
			return nil, err
		}
		var plan descendantOraclePlan
		if !strings.HasPrefix(match[1], "shmem-") {
			plan, err = descendantReadOraclePlan(filepath.Join(original, "lazy-oracle-"+match[1]+".bin"), match[1])
			if err != nil {
				return nil, err
			}
			plans++
			if len(plan.Ranges) > 0 {
				alias, err := os.ReadFile(filepath.Join(original, "lazy-parent-"+match[1]+".bin"))
				if err != nil || len(alias) != 16 ||
					binary.LittleEndian.Uint64(alias) != 0x4c415a59494e4832 ||
					binary.LittleEndian.Uint64(alias[8:]) != plan.ParentPID {
					return nil, fmt.Errorf("inheritance decision lost its actual parent image PID: %v", err)
				}
			}
		}
		derived, copies, inheritedBytes, err := descendantApplyOraclePlan(saved, plan.Ranges)
		if err != nil {
			return nil, err
		}
		encoded, err := criuEncodeSavedMap(derived)
		if err != nil {
			return nil, err
		}
		if err := descendantOracleWrite(filepath.Join(target, entry.Name()), encoded, owner); err != nil {
			return nil, err
		}
		pageName := fmt.Sprintf("pages-%d.img", saved.PageID)
		if consumed[pageName] {
			return nil, fmt.Errorf("full writer has duplicate page payload identities")
		}
		consumed[pageName], consumed[entry.Name()] = true, true
		// A non-nil empty slice means that every range was inherited.
		if copies == nil {
			copies = []descendantOracleCopy{}
		}
		count, err := descendantOracleCopyFile(filepath.Join(original, pageName),
			filepath.Join(target, pageName), copies, owner)
		if err != nil {
			return nil, err
		}
		local += uint64(count)
		inherited += inheritedBytes
		maps++
	}
	if maps == 0 || plans == 0 {
		return nil, fmt.Errorf("same-capture oracle has no original memory or recorded decisions")
	}
	for _, entry := range entries {
		name := entry.Name()
		if consumed[name] || name == "parent" ||
			(!criuInheritParentEntry(name) && name != "runtime-config.json" && name != "descriptors.json") {
			continue
		}
		if strings.HasPrefix(name, "pages-") {
			return nil, fmt.Errorf("original writer has an unreferenced page payload")
		}
		if _, err := descendantOracleCopyFile(filepath.Join(original, name), filepath.Join(target, name), nil, owner); err != nil {
			return nil, err
		}
	}
	if err := os.Symlink(link, filepath.Join(target, "parent")); err != nil {
		return nil, err
	}
	if err := errors.Join(os.Lchown(filepath.Join(target, "parent"), int(owner.UID), int(owner.GID)),
		os.Chown(target, int(owner.UID), int(owner.GID))); err != nil {
		return nil, err
	}
	directory, err := os.Open(target)
	if err != nil {
		return nil, err
	}
	if err := errors.Join(directory.Sync(), directory.Close()); err != nil {
		return nil, err
	}
	return map[string]any{
		"original_writer_images": original, "derived_images": target, "memory_maps": maps,
		"decision_files": plans, "inherited_bytes": inherited, "local_bytes": local,
		"scope": "Actual fast-path decisions recorded while a complete original CRIU writer collected " +
			"the same memory. Diagnostic derivation only; direct fast capture and target restore remain separate gates.",
	}, nil
}

func criuInheritedOracleCommand(command string, args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("%s requires two image directories", command)
	}
	if command == "compare-image-memory" || command == "compare-captured-memory" {
		compare := descendantCompareImageMemory
		if command == "compare-captured-memory" {
			compare = descendantCompareCapturedMemory
		}
		proof, err := compare(args[0], args[1])
		if err == nil {
			fmt.Println(asJSON(proof))
		}
		return err
	}
	derived, err := descendantDeriveInheritedOracle(args[0], args[1])
	if err != nil {
		return err
	}
	compared, err := descendantCompareImageMemory(args[1], args[0])
	if err == nil {
		fmt.Println(asJSON(map[string]any{"derived": derived, "comparison": compared}))
	}
	return err
}
