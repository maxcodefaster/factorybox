package snapshot

import (
	"bytes"
	"encoding/binary"
	"google.golang.org/protobuf/encoding/protowire"
	"testing"
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

func TestPageMapCompatibilityAndMalformedData(t *testing.T) {
	image := prefetchTestImage(map[protowire.Number]uint64{1: 10}, map[protowire.Number]uint64{1: 0x1000, 2: 1},
		map[protowire.Number]uint64{1: 0x2000, 2: 0, 3: 1, 5: 2})
	got, err := readSavedMap(bytes.NewReader(image))
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
			if _, err := readSavedMap(bytes.NewReader(data)); err == nil {
				t.Fatal("invalid image accepted")
			}
		})
	}
}
