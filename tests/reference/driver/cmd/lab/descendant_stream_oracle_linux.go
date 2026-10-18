package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// CRIU wrote the canonical inherited map/local payload and this complete
// original payload from one read of each pipe chunk. This adds only the
// current head's metadata and complete ordinary shared-memory maps; it never
// derives or rewrites the canonical fast capture.
func descendantPrepareStreamOriginal(head string) (_ map[string]any, resultErr error) {
	original := head + "/original-writer"
	if filepath.Clean(head) != head || !strings.HasPrefix(head, "/experiment/") {
		return nil, fmt.Errorf("stream original writer is outside the owned experiment")
	}
	for _, path := range []string{head, original} {
		actual, err := filepath.EvalSymlinks(path)
		if err != nil || actual != path {
			return nil, fmt.Errorf("stream original writer path is not canonical: %v", err)
		}
	}
	info, err := os.Lstat(head)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("stream capture head is not a directory: %v", err)
	}
	owner := descendantLeaseFileIdentity(info)
	if _, err := os.Lstat(original + "/streaming-original-writer-complete.json"); !os.IsNotExist(err) {
		return nil, fmt.Errorf("stream original writer metadata completion is single-use")
	}
	if err := descendantOwnStreamCapture(head, owner); err != nil {
		return nil, err
	}
	info, err = os.Lstat(original)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("stream original writer directory is absent: %v", err)
	}
	saved := descendantLeaseFileIdentity(info)
	if saved.UID != owner.UID || saved.GID != owner.GID || info.Mode().Perm() != 0700 {
		return nil, fmt.Errorf("stream original writer lost its private capture ownership")
	}
	notify := make(chan os.Signal, 1)
	signal.Notify(notify, syscall.SIGIO)
	defer signal.Stop(notify)
	entries, err := os.ReadDir(head)
	if err != nil {
		return nil, err
	}
	graph, err := descendantOpenMemoryGraph(head)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, graph.close()) }()
	control, err := descendantOpenMemoryGraph(original)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, control.close()) }()
	captured := map[string]bool{}
	admittedPages := map[string]bool{}
	var localBytes, inheritedBytes, originalBytes int64
	var taskMaps, sharedMaps int
	for _, entry := range entries {
		match := descendantMemoryName.FindStringSubmatch(entry.Name())
		if match == nil || strings.HasPrefix(match[1], "shmem-") {
			continue
		}
		key := match[1]
		pid, err := strconv.ParseUint(key, 10, 31)
		if err != nil || pid == 0 {
			return nil, fmt.Errorf("stream capture has a noncanonical task image identity")
		}
		file, err := os.OpenFile(head+"/lazy-stream-"+key+".bin", os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
		if err != nil {
			return nil, err
		}
		info, statErr := file.Stat()
		var record [6]uint64
		readErr := binary.Read(file, binary.LittleEndian, &record)
		closeErr := file.Close()
		if statErr != nil || readErr != nil || closeErr != nil || !info.Mode().IsRegular() || info.Size() != 48 ||
			record[0] != 0x4c53545245414d31 || record[1] != pid || record[2] == 0 ||
			record[2] != record[3] || record[4] > record[3] || record[5] != record[3]-record[4] ||
			record[3]%4096 != 0 || record[4]%4096 != 0 || record[5]%4096 != 0 {
			return nil, fmt.Errorf("stream writer completion record is incomplete or inconsistent")
		}
		identity := descendantLeaseFileIdentity(info)
		if identity.UID != owner.UID || identity.GID != owner.GID {
			return nil, fmt.Errorf("stream writer completion record has a foreign owner")
		}
		current, err := graph.load(head, key)
		if err != nil {
			return nil, err
		}
		full, err := control.load(original, key)
		if err != nil || full.Parent != nil || current.Saved.PageID != full.Saved.PageID ||
			asJSON(descendantMemoryCoverage(current.Saved)) != asJSON(descendantMemoryCoverage(full.Saved)) {
			return nil, fmt.Errorf("stream original writer has different identity or coverage: %v", err)
		}
		var local, parent, complete uint64
		for _, span := range current.Saved.Ranges {
			if span.Flags&4 != 0 {
				local += span.Pages * 4096
			} else {
				parent += span.Pages * 4096
			}
		}
		for _, span := range full.Saved.Ranges {
			if span.Flags&4 == 0 || span.Flags&1 != 0 {
				return nil, fmt.Errorf("stream byte oracle retained inherited memory")
			}
			complete += span.Pages * 4096
		}
		if local != record[4] || parent != record[5] || complete != record[3] ||
			full.DataBytes != int64(complete) || (local > 0 && current.DataBytes != int64(local)) {
			return nil, fmt.Errorf("stream page payload sizes differ from the writer's completed byte counters")
		}
		for _, path := range []string{head + "/" + entry.Name(), original + "/" + entry.Name(),
			fmt.Sprintf("%s/pages-%d.img", head, full.Saved.PageID),
			fmt.Sprintf("%s/pages-%d.img", original, full.Saved.PageID)} {
			info, err := os.Lstat(path)
			if err != nil || !info.Mode().IsRegular() {
				return nil, fmt.Errorf("stream image entry is not a regular file: %v", err)
			}
			identity := descendantLeaseFileIdentity(info)
			if identity.UID != owner.UID || identity.GID != owner.GID {
				return nil, fmt.Errorf("stream image entry has a foreign owner")
			}
		}
		plan, err := descendantReadOraclePlan(head+"/lazy-oracle-"+key+".bin", key)
		if err != nil {
			return nil, err
		}
		var selected uint64
		var selectedRanges [][2]uint64
		for _, span := range plan.Ranges {
			selected += span.End - span.Start
			selectedRanges = append(selectedRanges, [2]uint64{span.Start, span.End})
		}
		if selected != parent {
			return nil, fmt.Errorf("stream inherited ranges differ from the recorded source decisions")
		}
		var actualRanges [][2]uint64
		for _, span := range current.Saved.Ranges {
			if span.Flags&1 == 0 {
				continue
			}
			end := span.Address + span.Pages*4096
			if len(actualRanges) > 0 && actualRanges[len(actualRanges)-1][1] == span.Address {
				actualRanges[len(actualRanges)-1][1] = end
			} else {
				actualRanges = append(actualRanges, [2]uint64{span.Address, end})
			}
		}
		if asJSON(actualRanges) != asJSON(selectedRanges) || (parent > 0 &&
			(current.Parent == nil || current.Parent.Key != strconv.FormatUint(plan.ParentPID, 10))) {
			return nil, fmt.Errorf("stream canonical inheritance differs from its recorded ranges or parent PID")
		}
		captured[entry.Name()] = true
		pageName := fmt.Sprintf("pages-%d.img", full.Saved.PageID)
		captured[pageName], admittedPages[pageName] = true, true
		localBytes += int64(local)
		inheritedBytes += int64(parent)
		originalBytes += int64(complete)
		taskMaps++
	}
	if taskMaps == 0 {
		return nil, fmt.Errorf("stream capture lacks completed original task writers")
	}
	for _, entry := range entries {
		name := entry.Name()
		if captured[name] {
			continue
		}
		if !criuInheritParentEntry(name) && !strings.HasPrefix(name, "lazy-oracle-") &&
			!strings.HasPrefix(name, "lazy-stream-") && name != "runtime-config.json" && name != "descriptors.json" {
			continue
		}
		if match := descendantMemoryName.FindStringSubmatch(name); match != nil {
			if !strings.HasPrefix(match[1], "shmem-") {
				return nil, fmt.Errorf("stream task pagemap escaped its completion records")
			}
			shared, err := graph.load(head, match[1])
			if err != nil || shared.Parent != nil {
				return nil, fmt.Errorf("stream original writer requires complete ordinary shared-memory maps: %v", err)
			}
			sharedMaps++
			admittedPages[fmt.Sprintf("pages-%d.img", shared.Saved.PageID)] = true
			for _, span := range shared.Saved.Ranges {
				originalBytes += int64(span.Pages * 4096)
				localBytes += int64(span.Pages * 4096)
			}
		}
		if criuRAMPageName.MatchString(name) && !admittedPages[name] {
			return nil, fmt.Errorf("stream head contains an unreferenced page payload")
		}
		lease, err := descendantAcquireFileLeasePolicy(filepath.Join(head, name), !criuRAMPageName.MatchString(name))
		if err != nil {
			return nil, err
		}
		if lease.Saved.UID != owner.UID || lease.Saved.GID != owner.GID {
			lease.close()
			return nil, fmt.Errorf("stream metadata copy has a foreign owner")
		}
		err = descendantCloneLeasedPage(lease, filepath.Join(original, name))
		err = errors.Join(err, lease.close())
		if err != nil {
			return nil, err
		}
	}
	for _, name := range []string{"inventory.img", "pstree.img"} {
		if info, err := os.Lstat(original + "/" + name); err != nil || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("stream original writer lacks complete process metadata: %v", err)
		}
	}
	result := map[string]any{
		"native_stream_original_writer": true, "actual_capture_images": head, "original_writer_images": original,
		"memory_maps": taskMaps + sharedMaps, "task_memory_maps": taskMaps, "decision_files": taskMaps,
		"original_bytes": originalBytes, "local_bytes": localBytes, "inherited_bytes": inheritedBytes,
		"go_derived_candidate": false,
		"scope": "Canonical inherited task maps/local data and a complete original writer from the same once-read CRIU pipe bytes. " +
			"Only metadata and complete ordinary shared-memory images are cloned into the original-writer directory.",
	}
	if err := descendantOracleWrite(original+"/streaming-original-writer-complete.json", []byte(asJSON(result)), owner); err != nil {
		return nil, err
	}
	directory, err := os.Open(original)
	if err != nil {
		return nil, err
	}
	if err := errors.Join(directory.Sync(), directory.Close()); err != nil {
		return nil, err
	}
	return result, nil
}

func criuPrepareStreamOriginal(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("prepare-stream-original requires one canonical capture head")
	}
	stream, err := descendantPrepareStreamOriginal(args[0])
	if err != nil {
		return err
	}
	compared, err := descendantCompareImageMemory(args[0], args[0]+"/original-writer")
	if err != nil {
		return err
	}
	fmt.Println(asJSON(map[string]any{"stream": stream, "comparison": compared}))
	return nil
}
