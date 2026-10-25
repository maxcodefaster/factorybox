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

func TestReferencedAggregateDoesNotSelectAnUnrelatedDuplicateName(t *testing.T) {
	strings := []byte("\x00bpf_iter__task\x00task_struct\x00task\x00birth\x00")
	var types []byte
	record := func(name, info, value uint32, tail ...uint32) {
		for _, value := range append([]uint32{name, info, value}, tail...) {
			types = binary.LittleEndian.AppendUint32(types, value)
		}
	}
	record(1, 4<<24|1, 8, 28, 2, 0)    // 1: iterator.task -> pointer 2
	record(0, 2<<24, 3)                // 2: pointer -> actual task 3
	record(16, 4<<24|1, 64, 33, 0, 32) // 3: actual task.birth at byte 4
	record(16, 4<<24|1, 8, 33, 0, 0)   // 4: unrelated same-named task
	record(0, 3<<24, 0, 2, 0, 0)       // 5: array of pointers to actual task
	record(0, 2<<24, 6)                // 6: recursive pointer, must refuse
	header := make([]byte, 24)
	binary.LittleEndian.PutUint16(header, 0xeb9f)
	header[2] = 1
	binary.LittleEndian.PutUint32(header[4:], 24)
	binary.LittleEndian.PutUint32(header[12:], uint32(len(types)))
	binary.LittleEndian.PutUint32(header[16:], uint32(len(types)))
	binary.LittleEndian.PutUint32(header[20:], uint32(len(strings)))
	btf, err := parseBTF(append(append(header, types...), strings...))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := btf.named("task_struct", 4); err == nil {
		t.Fatal("a global duplicate must remain ambiguous")
	}
	child, _, err := btf.member(1, "task")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []uint32{child, 5} {
		actual, err := btf.referencedAggregate(id, "task_struct")
		if err != nil || actual != 3 {
			t.Fatal("lost the exact referenced aggregate", actual, err)
		}
		_, offset, err := btf.member(actual, "birth")
		if err != nil || offset != 4 {
			t.Fatal("selected another task's layout", offset, err)
		}
	}
	for _, id := range []uint32{0, 6, 999} {
		if _, err := btf.referencedAggregate(id, "task_struct"); err == nil {
			t.Fatal("accepted an invalid or recursive type", id)
		}
	}
	if _, err := btf.referencedAggregate(child, "another_struct"); err == nil {
		t.Fatal("accepted an unrelated referenced name")
	}
}
