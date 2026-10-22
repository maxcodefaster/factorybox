package timerstate

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// Linux 7.2 uses a 24-bit BTF vlen. The already pinned indirect ebpf
// dependency predates that format, so do not send modern kernel BTF through
// its 16-bit parser. Keep the original bounded bytes and index fixed headers.
type btfType struct{ name, info, value, tail uint32 }
type kernelBTF struct {
	data, strings []byte
	types         []btfType
}

func parseBTF(raw []byte) (*kernelBTF, error) {
	if len(raw) < 24 || len(raw) > 32<<20 || binary.LittleEndian.Uint16(raw) != 0xeb9f || raw[2] != 1 || raw[3] != 0 {
		return nil, fmt.Errorf("unsupported bounded native kernel BTF")
	}
	u32 := func(offset int) uint64 { return uint64(binary.LittleEndian.Uint32(raw[offset:])) }
	header, typeOffset, typeBytes, stringOffset, stringBytes := u32(4), u32(8), u32(12), u32(16), u32(20)
	if header < 24 || header+typeOffset+typeBytes > uint64(len(raw)) || header+stringOffset+stringBytes > uint64(len(raw)) || stringBytes == 0 {
		return nil, fmt.Errorf("kernel BTF sections exceed their bound")
	}
	b := &kernelBTF{data: raw, strings: raw[header+stringOffset : header+stringOffset+stringBytes], types: []btfType{{}}}
	for cursor, end := header+typeOffset, header+typeOffset+typeBytes; cursor < end; {
		if cursor+12 > end {
			return nil, fmt.Errorf("truncated kernel BTF header")
		}
		t := btfType{binary.LittleEndian.Uint32(raw[cursor:]), binary.LittleEndian.Uint32(raw[cursor+4:]), binary.LittleEndian.Uint32(raw[cursor+8:]), uint32(cursor + 12)}
		kind, count := (t.info>>24)&127, uint64(t.info&0xffffff)
		var length uint64
		switch kind {
		case 1, 14, 17:
			length = 4
		case 2, 7, 8, 9, 10, 11, 12, 16, 18:
		case 3:
			length = 12
		case 4, 5, 15, 19:
			length = 12 * count
		case 6, 13:
			length = 8 * count
		default:
			return nil, fmt.Errorf("unsupported kernel BTF kind %d", kind)
		}
		if cursor+12+length > end || uint64(t.name) >= stringBytes {
			return nil, fmt.Errorf("kernel BTF record exceeds its section")
		}
		b.types = append(b.types, t)
		cursor += 12 + length
	}
	return b, nil
}

func (b *kernelBTF) name(offset uint32) ([]byte, error) {
	if uint64(offset) >= uint64(len(b.strings)) {
		return nil, fmt.Errorf("invalid BTF string offset")
	}
	value := b.strings[offset:]
	end := bytes.IndexByte(value, 0)
	if end < 0 {
		return nil, fmt.Errorf("unterminated BTF string")
	}
	return value[:end], nil
}

func (b *kernelBTF) named(name string, kind uint32) (uint32, error) {
	var found uint32
	for id, t := range b.types[1:] {
		if (t.info>>24)&127 != kind {
			continue
		}
		value, err := b.name(t.name)
		if err != nil {
			return 0, err
		}
		if string(value) != name {
			continue
		}
		if found != 0 {
			return 0, fmt.Errorf("ambiguous kernel BTF type %s", name)
		}
		found = uint32(id + 1)
	}
	if found == 0 {
		return 0, fmt.Errorf("missing kernel BTF type %s", name)
	}
	return found, nil
}

func (b *kernelBTF) member(id uint32, name string) (uint32, int32, error) {
	return b.memberDepth(id, name, 0)
}

func (b *kernelBTF) memberDepth(id uint32, name string, depth int) (uint32, int32, error) {
	if id == 0 || uint64(id) >= uint64(len(b.types)) || depth > 8 {
		return 0, 0, fmt.Errorf("invalid or recursive kernel BTF member")
	}
	t := b.types[id]
	if kind := (t.info >> 24) & 127; kind != 4 && kind != 5 {
		return 0, 0, fmt.Errorf("kernel BTF member is not an aggregate")
	}
	for index := uint32(0); index < t.info&0xffffff; index++ {
		row := b.data[t.tail+12*index:]
		text, err := b.name(binary.LittleEndian.Uint32(row))
		if err != nil {
			return 0, 0, err
		}
		target, bits := binary.LittleEndian.Uint32(row[4:]), binary.LittleEndian.Uint32(row[8:])
		if t.info>>31 != 0 {
			if bits>>24 != 0 {
				continue
			}
			bits &= 0xffffff
		}
		if bits%8 != 0 || bits/8 > 1<<20 {
			continue
		}
		offset := int32(bits / 8)
		if string(text) == name {
			return target, offset, nil
		}
		if len(text) == 0 {
			if child, relative, err := b.memberDepth(target, name, depth+1); err == nil {
				return child, offset + relative, nil
			}
		}
	}
	return 0, 0, fmt.Errorf("kernel BTF member %s is absent", name)
}
