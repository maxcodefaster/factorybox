package timerstate

import (
	"encoding/binary"
	"testing"
)

func TestModernBTFVlenAndBounds(t *testing.T) {
	// One enum64 has more than 65,535 members. A 16-bit decoder loses the
	// following task_struct record, as the older pinned indirect library does.
	const count = 65536
	strings := []byte("\x00wide_enum\x00task_struct\x00")
	types := make([]byte, 12+12*count+12)
	binary.LittleEndian.PutUint32(types, 1)
	binary.LittleEndian.PutUint32(types[4:], 19<<24|count)
	binary.LittleEndian.PutUint32(types[8:], 8)
	for offset := 12; offset < 12+12*count; offset += 12 {
		binary.LittleEndian.PutUint32(types[offset:], 1)
	}
	binary.LittleEndian.PutUint32(types[12+12*count:], 11)
	binary.LittleEndian.PutUint32(types[16+12*count:], 4<<24)
	binary.LittleEndian.PutUint32(types[20+12*count:], 64)
	header := make([]byte, 24)
	binary.LittleEndian.PutUint16(header, 0xeb9f)
	header[2] = 1
	binary.LittleEndian.PutUint32(header[4:], 24)
	binary.LittleEndian.PutUint32(header[12:], uint32(len(types)))
	binary.LittleEndian.PutUint32(header[16:], uint32(len(types)))
	binary.LittleEndian.PutUint32(header[20:], uint32(len(strings)))
	raw := append(append(header, types...), strings...)
	btf, err := parseBTF(raw)
	if err != nil {
		t.Fatal(err)
	}
	id, err := btf.named("task_struct", 4)
	if err != nil || id != 2 || btf.types[id].value != 64 {
		t.Fatal("lost the record after the wide vlen", id, err)
	}
	for _, length := range []int{0, 23, len(raw) - 1} {
		if _, err := parseBTF(raw[:length]); err == nil {
			t.Fatal("accepted truncated kernel BTF", length)
		}
	}
	broken := append([]byte(nil), raw...)
	binary.LittleEndian.PutUint32(broken[12:], uint32(len(types)+len(strings)+1))
	if _, err := parseBTF(broken); err == nil {
		t.Fatal("accepted an overflowing kernel BTF section")
	}
}
