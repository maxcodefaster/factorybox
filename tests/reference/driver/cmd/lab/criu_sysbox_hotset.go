package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"google.golang.org/protobuf/encoding/protowire"
)

type criuHotRange struct {
	Start uint64 `json:"start"`
	End   uint64 `json:"end"`
}

const criuHotsetBudget = 64 << 20

func criuHotsetEnabled() bool {
	return os.Getenv("CRIU_SYSBOX_CHILD_HOTSET") == "1"
}

func criuHotsetGuard(root, id string) error {
	switch os.Getenv("CRIU_SYSBOX_CHILD_HOTSET") {
	case "", "0":
		return nil
	case "1":
	default:
		return fmt.Errorf("CRIU_SYSBOX_CHILD_HOTSET must be 0 or 1")
	}
	if root != "/experiment" || id != "source-sysbox" {
		return fmt.Errorf("child hot pages require the owned source")
	}
	for _, name := range []string{"CRIU_SYSBOX_LAZY_INHERIT", "CRIU_SYSBOX_RETAIN_SOURCE",
		"CRIU_SYSBOX_LAZY", "CRIU_SYSBOX_PROBE", "CRIU_SYSBOX_COPY_PIPELINE"} {
		if os.Getenv(name) != "1" {
			return fmt.Errorf("child hot pages require %s=1", name)
		}
	}
	for _, name := range []string{"CRIU_SYSBOX_CHILD_READAROUND", "CRIU_SYSBOX_LAZY_WP", "CRIU_SYSBOX_LAZY_OBSERVE"} {
		if value := os.Getenv(name); value != "" && value != "0" {
			return fmt.Errorf("child hot pages require %s=0", name)
		}
	}
	if os.Getenv("CRIU_SYSBOX_LAZY_PREFETCH") != "0" {
		return fmt.Errorf("child hot pages exclude unbounded background prefetch")
	}
	return nil
}

// Hint changes affect when CRIU loads bytes, never which bytes it restores.
func criuApplyHotHints(current criuSavedMap, hot []criuHotRange, budget int64) (criuSavedMap, uint64, error) {
	result := criuSavedMap{PageID: current.PageID}
	if budget < 0 || budget > criuHotsetBudget || budget%4096 != 0 || len(hot) > 262144 {
		return result, 0, fmt.Errorf("invalid hot-page budget or range count")
	}
	hot = append([]criuHotRange(nil), hot...)
	for _, r := range hot {
		if r.Start >= r.End || r.Start%4096 != 0 || r.End%4096 != 0 {
			return result, 0, fmt.Errorf("invalid hot-page range")
		}
	}
	sort.Slice(hot, func(i, j int) bool { return hot[i].Start < hot[j].Start })
	merged := hot[:0]
	for _, r := range hot {
		if len(merged) > 0 && merged[len(merged)-1].End >= r.Start {
			merged[len(merged)-1].End = max(merged[len(merged)-1].End, r.End)
		} else {
			merged = append(merged, r)
		}
	}
	var selected uint64
	index := 0
	for _, entry := range current.Ranges {
		cursor, end := entry.Address, entry.Address+entry.Pages*4096
		appendRange := func(stop, flags uint64) {
			part := criuSavedRange{Address: cursor, Pages: (stop - cursor) / 4096, Flags: flags, Offset: entry.Offset}
			if entry.Flags&4 != 0 {
				part.Offset += int64(cursor - entry.Address)
			}
			result.Ranges = append(result.Ranges, part)
			cursor = stop
		}
		if entry.Flags&2 == 0 || budget == 0 {
			appendRange(end, entry.Flags)
			continue
		}
		for cursor < end {
			for index < len(merged) && merged[index].End <= cursor {
				index++
			}
			if index == len(merged) || merged[index].Start >= end || budget == 0 {
				appendRange(end, entry.Flags)
				break
			}
			if cursor < merged[index].Start {
				appendRange(merged[index].Start, entry.Flags)
			}
			stop := min(end, merged[index].End, cursor+uint64(budget))
			pages := (stop - cursor) / 4096
			appendRange(stop, entry.Flags&^2)
			selected += pages
			budget -= int64(pages * 4096)
		}
	}
	return result, selected, nil
}

// Canonical pinned protobuf framing. Reparse every output before using it;
// neither pages_id, content provenance nor local payload offsets may change.
func criuEncodeSavedMap(saved criuSavedMap) ([]byte, error) {
	if saved.PageID == 0 || saved.PageID > math.MaxUint32 {
		return nil, fmt.Errorf("invalid page identity")
	}
	data := binary.LittleEndian.AppendUint32(nil, 0x54564319)
	data = binary.LittleEndian.AppendUint32(data, 0x56084025)
	field := func(record []byte, number protowire.Number, value uint64) []byte {
		record = protowire.AppendTag(record, number, protowire.VarintType)
		return protowire.AppendVarint(record, value)
	}
	record := func(payload []byte) {
		data = binary.LittleEndian.AppendUint32(data, uint32(len(payload)))
		data = append(data, payload...)
	}
	record(field(nil, 1, saved.PageID))
	for _, r := range saved.Ranges {
		message := field(nil, 1, r.Address)
		compat := r.Pages
		if compat > math.MaxUint32 {
			compat = 0
		}
		message = field(message, 2, compat)
		message = field(message, 4, r.Flags)
		if r.Pages > math.MaxUint32 {
			message = field(message, 5, r.Pages)
		}
		record(message)
	}
	check, err := criuReadSavedMap(bytes.NewReader(data))
	if err != nil || !reflect.DeepEqual(check, saved) {
		return nil, fmt.Errorf("encoded pagemap did not preserve identity/ranges/offsets: %v", err)
	}
	return data, nil
}

var criuHotRead = regexp.MustCompile(`^\([0-9.]+\) pr([1-9][0-9]*)-[1-9][0-9]* Read ([0-9a-f]+) ([1-9][0-9]*) pages$`)

// Only the log prefix captured at first complete source readiness is eligible.
// Entries describe addresses, not contents. Latest child pagemaps supply bytes.
func criuReadHotTrace(input io.Reader) (map[int][]criuHotRange, error) {
	result := map[int][]criuHotRange{}
	scanner := bufio.NewScanner(io.LimitReader(input, (4<<20)+1))
	total, records := 0, 0
	for scanner.Scan() {
		line := scanner.Text()
		total += len(line) + 1
		if total > 4<<20 {
			return nil, fmt.Errorf("startup page trace exceeded limit")
		}
		if !strings.Contains(line, " Read ") {
			continue
		}
		match := criuHotRead.FindStringSubmatch(line)
		if match == nil {
			return nil, fmt.Errorf("unexpected startup page-read record")
		}
		pid, err := strconv.ParseUint(match[1], 10, 31)
		if err != nil {
			return nil, err
		}
		address, err := strconv.ParseUint(match[2], 16, 64)
		if err != nil || address%4096 != 0 {
			return nil, fmt.Errorf("invalid startup page address")
		}
		pages, err := strconv.ParseUint(match[3], 10, 64)
		if err != nil || pages > (math.MaxUint64-address)/4096 {
			return nil, fmt.Errorf("invalid startup page extent")
		}
		records++
		if records > 65536 {
			return nil, fmt.Errorf("too many startup page records")
		}
		result[int(pid)] = append(result[int(pid)], criuHotRange{address, address + pages*4096})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
