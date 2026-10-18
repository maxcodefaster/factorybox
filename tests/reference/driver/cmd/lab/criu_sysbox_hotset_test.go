package main

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
)

func TestHotHintsSelectOnlyCurrentLazyPagesWithinBudget(t *testing.T) {
	current, err := criuReadSavedMap(bytes.NewReader(prefetchTestImage(
		map[protowire.Number]uint64{1: 20},
		map[protowire.Number]uint64{1: 0x1000, 2: 4, 4: 6},
		map[protowire.Number]uint64{1: 0x8000, 2: 2, 4: 3},
		map[protowire.Number]uint64{1: 0xc000, 2: 1, 4: 4},
	)))
	if err != nil {
		t.Fatal(err)
	}
	before := append([]criuSavedRange(nil), current.Ranges...)
	result, pages, err := criuApplyHotHints(current,
		[]criuHotRange{{0x2000, 0x4000}, {0x3000, 0x4000}, {0x6000, 0x9000}, {0xc000, 0xd000}}, 3*4096)
	if err != nil || pages != 3 {
		t.Fatalf("hot pages were not selected/deduplicated under exact budget: pages=%d err=%v", pages, err)
	}
	want := []criuSavedRange{
		{Address: 0x1000, Pages: 1, Flags: 6, Offset: 0},
		{Address: 0x2000, Pages: 2, Flags: 4, Offset: 4096},
		{Address: 0x4000, Pages: 1, Flags: 6, Offset: 12288},
		{Address: 0x8000, Pages: 1, Flags: 1, Offset: 16384},
		{Address: 0x9000, Pages: 1, Flags: 3, Offset: 16384},
		{Address: 0xc000, Pages: 1, Flags: 4, Offset: 16384},
	}
	if result.PageID != current.PageID || !reflect.DeepEqual(result.Ranges, want) {
		t.Fatalf("changed content provenance, gaps, eager pages or offsets: %+v", result)
	}
	if !reflect.DeepEqual(current.Ranges, before) {
		t.Fatal("modified immutable input metadata")
	}
	encoded, err := criuEncodeSavedMap(result)
	if err != nil {
		t.Fatal(err)
	}
	roundtrip, err := criuReadSavedMap(bytes.NewReader(encoded))
	if err != nil || !reflect.DeepEqual(roundtrip, result) {
		t.Fatalf("lost content identity in serialized metadata: %+v %v", roundtrip, err)
	}
}

func TestHotHintsBoundSparseHugeRangesWithoutPageExpansion(t *testing.T) {
	current := criuSavedMap{PageID: 8, Ranges: []criuSavedRange{
		{Address: 0x1000, Pages: 1 << 36, Flags: 3},
	}}
	result, pages, err := criuApplyHotHints(current,
		[]criuHotRange{{0x1000, (1 << 40) + 0x1000}}, criuHotsetBudget)
	if err != nil || pages != criuHotsetBudget/4096 || len(result.Ranges) != 2 {
		t.Fatalf("sparse extent must remain bounded: %+v %d %v", result, pages, err)
	}
	if _, err := criuEncodeSavedMap(result); err != nil {
		t.Fatal(err)
	}
	for _, budget := range []int64{-4096, 1, criuHotsetBudget + 4096} {
		if _, _, err := criuApplyHotHints(current, nil, budget); err == nil {
			t.Fatalf("accepted invalid budget %d", budget)
		}
	}
	for _, hot := range []criuHotRange{{2, 4096}, {4096, 4096}, {8192, 4096}} {
		if _, _, err := criuApplyHotHints(current, []criuHotRange{hot}, 4096); err == nil {
			t.Fatalf("accepted invalid range %+v", hot)
		}
	}
	invalid := current
	invalid.Ranges = []criuSavedRange{{Address: 4096, Pages: 1, Flags: 4, Offset: 4096}}
	if _, err := criuEncodeSavedMap(invalid); err == nil {
		t.Fatal("accepted incorrect local content offset")
	}
}

func TestHotTraceUsesExactReadRecordsAndBoundsInput(t *testing.T) {
	trace := "(00.302479) pr784-24 Read 4500010000 16 pages\n" +
		"(00.302480) unrelated ordinary log entry\n" +
		"(00.302490) pr784-25 Read 4500020000 1 pages\n"
	got, err := criuReadHotTrace(strings.NewReader(trace))
	if err != nil || !reflect.DeepEqual(got[784], []criuHotRange{
		{0x4500010000, 0x4500020000}, {0x4500020000, 0x4500021000},
	}) {
		t.Fatalf("wrong startup advice: %+v %v", got, err)
	}
	for _, text := range []string{
		"(0.1) pr2147483648-1 Read 1000 1 pages\n",
		"(0.1) pr1-1 Read 1001 1 pages\n",
		"(0.1) pr1-1 Read fffffffffffff000 2 pages\n",
		"(0.1) pr1-1 Read 1000 0 pages\n",
		strings.Repeat("x", (4<<20)+1),
	} {
		if _, err := criuReadHotTrace(strings.NewReader(text)); err == nil {
			t.Fatal("accepted invalid or oversized trace")
		}
	}
}

func TestHotsetGuardRequiresOwnedChildAndSingleTreatment(t *testing.T) {
	for _, name := range []string{"CRIU_SYSBOX_CHILD_HOTSET", "CRIU_SYSBOX_CHILD_READAROUND",
		"CRIU_SYSBOX_LAZY_WP", "CRIU_SYSBOX_LAZY_OBSERVE"} {
		t.Setenv(name, "0")
	}
	if err := criuHotsetGuard("/other", "other"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CRIU_SYSBOX_CHILD_HOTSET", "1")
	for _, name := range []string{"CRIU_SYSBOX_LAZY_INHERIT", "CRIU_SYSBOX_RETAIN_SOURCE",
		"CRIU_SYSBOX_LAZY", "CRIU_SYSBOX_PROBE", "CRIU_SYSBOX_COPY_PIPELINE"} {
		t.Setenv(name, "1")
	}
	t.Setenv("CRIU_SYSBOX_LAZY_PREFETCH", "0")
	if err := criuHotsetGuard("/experiment", "source-sysbox"); err != nil {
		t.Fatal(err)
	}
	if err := criuHotsetGuard("/experiment", "copy-sysbox"); err == nil {
		t.Fatal("accepted an unrelated source")
	}
	for _, name := range []string{"CRIU_SYSBOX_CHILD_READAROUND", "CRIU_SYSBOX_LAZY_WP",
		"CRIU_SYSBOX_LAZY_OBSERVE", "CRIU_SYSBOX_LAZY_PREFETCH"} {
		t.Setenv(name, "1")
		if err := criuHotsetGuard("/experiment", "source-sysbox"); err == nil {
			t.Fatalf("accepted conflicting treatment %s", name)
		}
		t.Setenv(name, "0")
	}
}
