package snapshot

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"google.golang.org/protobuf/encoding/protowire"
	"io"
	"math"
	"reflect"
)

type savedRange struct {
	Address uint64
	Pages   uint64
	Flags   uint64
	Offset  int64
}

type savedMap struct {
	PageID uint64
	Ranges []savedRange
}

func readSavedMap(input io.Reader) (result savedMap, err error) {
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
		entry := savedRange{Address: address, Pages: pages, Flags: flags, Offset: offset}
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

func encodeSavedMap(saved savedMap) ([]byte, error) {
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
	check, err := readSavedMap(bytes.NewReader(data))
	if err != nil || !reflect.DeepEqual(check, saved) {
		return nil, fmt.Errorf("encoded pagemap did not preserve identity/ranges/offsets: %v", err)
	}
	return data, nil
}
