package main

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"sort"

	"google.golang.org/protobuf/encoding/protowire"
)

const criuEagerPrefetchBudget = 64 << 20

type criuSavedRange struct {
	Address uint64
	Pages   uint64
	Flags   uint64
	Offset  int64
}

type criuSavedMap struct {
	PageID uint64
	Ranges []criuSavedRange
}

type criuPrefetchRange struct {
	File   string `json:"file"`
	Offset int64  `json:"offset"`
	Bytes  int64  `json:"bytes"`
}

// Read the pinned pagemap framing and schema. This only plans read-only I/O;
// CRIU remains responsible for restoring mappings, holes and page contents.
func criuReadSavedMap(input io.Reader) (result criuSavedMap, err error) {
	r := bufio.NewReader(io.LimitReader(input, 8<<20+1))
	var header [8]byte
	if _, err = io.ReadFull(r, header[:]); err != nil {
		return result, err
	}
	if binary.LittleEndian.Uint32(header[:4]) != 0x54564319 || binary.LittleEndian.Uint32(header[4:]) != 0x56084025 {
		return result, fmt.Errorf("unexpected saved pagemap magic")
	}
	var previousEnd uint64
	var offset int64
	for index := 0; ; index++ {
		var size uint32
		err = binary.Read(r, binary.LittleEndian, &size)
		if err == io.EOF && index > 0 {
			return result, nil
		}
		if err != nil || size == 0 || size > 256 || index > 262144 {
			return result, fmt.Errorf("invalid saved pagemap record %d: %w", index, err)
		}
		message := make([]byte, size)
		if _, err = io.ReadFull(r, message); err != nil {
			return result, err
		}
		fields := map[protowire.Number]uint64{}
		for len(message) > 0 {
			number, kind, count := protowire.ConsumeTag(message)
			if count < 0 || kind != protowire.VarintType || number < 1 || number > 5 {
				return result, fmt.Errorf("unsupported saved pagemap field")
			}
			message = message[count:]
			value, count := protowire.ConsumeVarint(message)
			if count < 0 {
				return result, fmt.Errorf("truncated saved pagemap field")
			}
			if _, exists := fields[number]; exists {
				return result, fmt.Errorf("duplicate saved pagemap field")
			}
			fields[number] = value
			message = message[count:]
		}
		if index == 0 {
			if len(fields) != 1 || fields[1] == 0 || fields[1] > math.MaxUint32 {
				return result, fmt.Errorf("invalid saved page identity")
			}
			result.PageID = fields[1]
			continue
		}
		address, hasAddress := fields[1]
		pages, hasPages := fields[2]
		if extended, exists := fields[5]; exists {
			pages = extended
		}
		flags, hasFlags := fields[4]
		if fields[3] > 1 || fields[2] > math.MaxUint32 {
			return result, fmt.Errorf("invalid saved pagemap compatibility fields")
		}
		if fields[3] == 1 {
			flags |= 1
		} else if !hasFlags {
			flags = 4
		}
		if !hasAddress || !hasPages || pages == 0 || pages > math.MaxInt64/4096 ||
			address%4096 != 0 || address < previousEnd || pages > (math.MaxUint64-address)/4096 ||
			flags&^uint64(7) != 0 || flags&5 == 5 || flags&5 == 0 {
			return result, fmt.Errorf("invalid saved page range")
		}
		entry := criuSavedRange{Address: address, Pages: pages, Flags: flags, Offset: offset}
		result.Ranges = append(result.Ranges, entry)
		previousEnd = address + pages*4096
		if flags&4 != 0 {
			if offset > math.MaxInt64-int64(pages*4096) {
				return result, fmt.Errorf("saved page offset overflow")
			}
			offset += int64(pages * 4096)
		}
	}
}

// The current image decides whether a range is eager. For an inherited range,
// resolve its actual bytes in the immutable base, even if the base marked them
// lazy. Never prefetch gaps or ranges discarded from the current pagemap.
func criuEagerRanges(current, parent criuSavedMap, currentFile, parentFile string) ([]criuPrefetchRange, error) {
	var result []criuPrefetchRange
	for _, entry := range current.Ranges {
		if entry.Flags&2 != 0 {
			continue
		}
		if entry.Flags&4 != 0 {
			result = append(result, criuPrefetchRange{currentFile, entry.Offset, int64(entry.Pages * 4096)})
			continue
		}
		cursor, end := entry.Address, entry.Address+entry.Pages*4096
		index := sort.Search(len(parent.Ranges), func(i int) bool {
			p := parent.Ranges[i]
			return p.Address+p.Pages*4096 > cursor
		})
		for cursor < end {
			if index >= len(parent.Ranges) || parent.Ranges[index].Address > cursor || parent.Ranges[index].Flags&4 == 0 {
				return nil, fmt.Errorf("eager inherited page has no complete local base payload")
			}
			p := parent.Ranges[index]
			stop := min(end, p.Address+p.Pages*4096)
			result = append(result, criuPrefetchRange{parentFile, p.Offset + int64(cursor-p.Address), int64(stop - cursor)})
			cursor = stop
			index++
		}
	}
	return result, nil
}

func criuBoundPrefetch(ranges []criuPrefetchRange, budget int64) ([]criuPrefetchRange, error) {
	if budget <= 0 || budget > criuEagerPrefetchBudget || budget%4096 != 0 {
		return nil, fmt.Errorf("invalid eager prefetch budget")
	}
	sort.Slice(ranges, func(i, j int) bool {
		if ranges[i].File != ranges[j].File {
			return ranges[i].File < ranges[j].File
		}
		return ranges[i].Offset < ranges[j].Offset
	})
	var merged []criuPrefetchRange
	for _, r := range ranges {
		if r.File == "" || r.Offset < 0 || r.Bytes <= 0 || r.Offset%4096 != 0 || r.Bytes%4096 != 0 || r.Offset > math.MaxInt64-r.Bytes {
			return nil, fmt.Errorf("invalid prefetch file extent")
		}
		if len(merged) > 0 {
			last := &merged[len(merged)-1]
			if last.File == r.File && last.Offset+last.Bytes >= r.Offset {
				last.Bytes = max(last.Offset+last.Bytes, r.Offset+r.Bytes) - last.Offset
				continue
			}
		}
		merged = append(merged, r)
	}
	// Visit each saved extent in bounded chunks so one large process cannot
	// consume the entire startup working-set budget before others get a turn.
	var result []criuPrefetchRange
	for len(merged) > 0 && budget > 0 {
		next := merged[:0]
		for _, r := range merged {
			length := min(r.Bytes, int64(256<<10), budget)
			if length > 0 {
				result = append(result, criuPrefetchRange{r.File, r.Offset, length})
				budget -= length
				r.Offset += length
				r.Bytes -= length
			}
			if r.Bytes > 0 {
				next = append(next, r)
			}
		}
		merged = next
	}
	return result, nil
}
