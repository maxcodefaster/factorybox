package main

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
)

func prefetchTestImage(records ...map[protowire.Number]uint64) []byte {
	buffer := new(bytes.Buffer)
	_ = binary.Write(buffer, binary.LittleEndian, uint32(0x54564319))
	_ = binary.Write(buffer, binary.LittleEndian, uint32(0x56084025))
	for _, record := range records {
		var message []byte
		for number := protowire.Number(1); number <= 5; number++ {
			if value, exists := record[number]; exists {
				message = protowire.AppendTag(message, number, protowire.VarintType)
				message = protowire.AppendVarint(message, value)
			}
		}
		_ = binary.Write(buffer, binary.LittleEndian, uint32(len(message)))
		_, _ = buffer.Write(message)
	}
	return buffer.Bytes()
}

func TestEagerPrefetchResolvesExactCurrentAndParentBytes(t *testing.T) {
	current, err := criuReadSavedMap(bytes.NewReader(prefetchTestImage(
		map[protowire.Number]uint64{1: 20},
		map[protowire.Number]uint64{1: 0x1000, 2: 1, 4: 6}, // local lazy occupies file offset zero
		map[protowire.Number]uint64{1: 0x3000, 2: 2, 4: 1}, // eager parent, with a discarded gap before it
		map[protowire.Number]uint64{1: 0x7000, 2: 1, 4: 4}, // eager local follows the lazy payload
		map[protowire.Number]uint64{1: 0x9000, 2: 1, 4: 3}, // lazy parent must not be prefetched
	)))
	if err != nil {
		t.Fatal(err)
	}
	parent, err := criuReadSavedMap(bytes.NewReader(prefetchTestImage(
		map[protowire.Number]uint64{1: 10},
		map[protowire.Number]uint64{1: 0x1000, 2: 3, 4: 6},
		map[protowire.Number]uint64{1: 0x4000, 2: 1, 4: 4},
	)))
	if err != nil {
		t.Fatal(err)
	}
	got, err := criuEagerRanges(current, parent, "current", "parent")
	want := []criuPrefetchRange{{"parent", 8192, 4096}, {"parent", 12288, 4096}, {"current", 4096, 4096}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("incorrect parent or local offset, discarded/lazy range included: %v %v", got, err)
	}
	parent.Ranges[1].Address += 4096
	if _, err := criuEagerRanges(current, parent, "current", "parent"); err == nil {
		t.Fatal("missing parent bytes accepted")
	}
}

func TestEagerPrefetchCompatibilityAndMalformedMaps(t *testing.T) {
	image := prefetchTestImage(map[protowire.Number]uint64{1: 10}, map[protowire.Number]uint64{1: 0x1000, 2: 1},
		map[protowire.Number]uint64{1: 0x2000, 2: 0, 3: 1, 5: 2})
	got, err := criuReadSavedMap(bytes.NewReader(image))
	if err != nil || got.Ranges[0].Flags != 4 || got.Ranges[1].Flags != 1 || got.Ranges[1].Pages != 2 || got.Ranges[1].Offset != 4096 {
		t.Fatalf("pinned compatibility schema: %+v %v", got, err)
	}
	for name, data := range map[string][]byte{
		"truncated":               image[:len(image)-1],
		"wrong-magic":             append([]byte{0}, image[1:]...),
		"overlap":                 prefetchTestImage(map[protowire.Number]uint64{1: 10}, map[protowire.Number]uint64{1: 4096, 2: 2}, map[protowire.Number]uint64{1: 8192, 2: 1}),
		"both-parent-and-present": prefetchTestImage(map[protowire.Number]uint64{1: 10}, map[protowire.Number]uint64{1: 4096, 2: 1, 4: 5}),
		"zero-pages":              prefetchTestImage(map[protowire.Number]uint64{1: 10}, map[protowire.Number]uint64{1: 4096, 2: 0}),
		"overflow":                prefetchTestImage(map[protowire.Number]uint64{1: 10}, map[protowire.Number]uint64{1: 4096, 2: 0, 5: 1 << 63}),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := criuReadSavedMap(bytes.NewReader(data)); err == nil {
				t.Fatal("invalid image accepted")
			}
		})
	}
}

func TestEagerPrefetchBudgetDeduplicatesAndDistributes(t *testing.T) {
	plan, err := criuBoundPrefetch([]criuPrefetchRange{{"b", 4096, 1 << 20}, {"a", 0, 1 << 20}, {"a", 0, 1 << 20}}, 512<<10)
	want := []criuPrefetchRange{{"a", 0, 256 << 10}, {"b", 4096, 256 << 10}}
	if err != nil || !reflect.DeepEqual(plan, want) {
		t.Fatalf("budget duplicated or consumed by one extent: %v %v", plan, err)
	}
	for _, budget := range []int64{0, -1, 1, criuEagerPrefetchBudget + 4096} {
		if _, err := criuBoundPrefetch(nil, budget); err == nil {
			t.Fatal("unbounded prefetch accepted")
		}
	}
}
