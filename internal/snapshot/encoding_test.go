package snapshot

import (
	"bytes"
	"reflect"
	"testing"
)

func TestPageMapRoundtripPreservesLocalOffsetsAndSparseInheritedRanges(t *testing.T) {
	input := savedMap{PageID: 20, Ranges: []savedRange{
		{Address: 0x1000, Pages: 2, Flags: 6, Offset: 0},
		{Address: 0x5000, Pages: 1 << 36, Flags: 3, Offset: 8192},
	}}
	data, err := encodeSavedMap(input)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := readSavedMap(bytes.NewReader(data))
	if err != nil || !reflect.DeepEqual(input, decoded) {
		t.Fatal("pagemap roundtrip changed byte provenance", decoded, err)
	}
	input.Ranges[1].Offset++
	if _, err := encodeSavedMap(input); err == nil {
		t.Fatal("accepted an incorrect local payload offset")
	}
}
