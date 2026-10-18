package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestOraclePlanPreservesLocalOffsetsAndRejectsGaps(t *testing.T) {
	original := criuSavedMap{PageID: 9, Ranges: []criuSavedRange{
		{Address: 0x1000, Pages: 4, Flags: 6},
	}}
	derived, copies, inherited, err := descendantApplyOraclePlan(original,
		[]criuHotRange{{Start: 0x2000, End: 0x4000}})
	if err != nil || inherited != 8192 || len(copies) != 2 ||
		copies[0] != (descendantOracleCopy{Bytes: 4096}) ||
		copies[1] != (descendantOracleCopy{Offset: 12288, Bytes: 4096}) {
		t.Fatal("incomplete same-writer local extents", derived, copies, inherited, err)
	}
	if derived.Ranges[0].Offset != 0 || derived.Ranges[1].Offset != 4096 ||
		derived.Ranges[1].Flags != 3 || derived.Ranges[2].Offset != 4096 {
		t.Fatal("compacted local or inherited offsets changed", derived)
	}
	if _, err := criuEncodeSavedMap(derived); err != nil {
		t.Fatal(err)
	}
	original.Ranges = []criuSavedRange{
		{Address: 0x1000, Pages: 1, Flags: 6},
		{Address: 0x3000, Pages: 1, Flags: 6, Offset: 4096},
	}
	if _, _, _, err := descendantApplyOraclePlan(original,
		[]criuHotRange{{Start: 0x1000, End: 0x4000}}); err == nil {
		t.Fatal("accepted selected bytes absent from the original writer")
	}
	original.Ranges[0].Flags = 3
	if _, _, _, err := descendantApplyOraclePlan(original, nil); err == nil {
		t.Fatal("accepted an inherited view as the original writer")
	}
}

func TestOracleDerivationComparesEveryByteAndRejectsCorruption(t *testing.T) {
	head, parent, control := inheritedMemoryFixture(t)
	source := filepath.Dir(head)
	full, target := filepath.Join(source, "full"), filepath.Join(source, "derived")
	if err := os.Mkdir(full, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"pagemap-1.img", "pages-30.img"} {
		data, err := os.ReadFile(filepath.Join(control, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(full, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	alias, err := os.ReadFile(filepath.Join(head, "lazy-parent-1.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(full, "lazy-parent-1.bin"), alias, 0600); err != nil {
		t.Fatal(err)
	}
	var plan bytes.Buffer
	for _, value := range []uint64{descendantOracleMagic, 1, 7, 2, 0x1000, 0x2000, 0x3000, 0x4000} {
		if err := binary.Write(&plan, binary.LittleEndian, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(full, "lazy-oracle-1.bin"), plan.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../parent", filepath.Join(full, "parent")); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mount(parent, parent, "", unix.MS_BIND, ""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := unix.Unmount(parent, 0); err != nil {
			t.Error(err)
		}
	})
	if err := unix.Mount("", parent, "", unix.MS_BIND|unix.MS_REMOUNT|unix.MS_RDONLY, ""); err != nil {
		t.Fatal(err)
	}
	proof, err := descendantDeriveInheritedOracle(full, target)
	if err != nil {
		t.Fatal(err)
	}
	if proof["inherited_bytes"] != uint64(8192) || proof["local_bytes"] != uint64(4096) {
		t.Fatal("incomplete original-writer derivation", proof)
	}
	comparison, err := descendantCompareImageMemory(target, full)
	if err != nil || comparison["compared_bytes"] != int64(12288) ||
		comparison["inherited_bytes"] != int64(8192) {
		t.Fatal("derived bytes differ from the same complete writer", comparison, err)
	}
	before, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := descendantDeriveInheritedOracle(full, target); err == nil {
		t.Fatal("overwrote an existing diagnostic image")
	}
	after, err := os.Stat(target)
	if err != nil || !os.SameFile(before, after) {
		t.Fatal("rejected derivation changed existing target", err)
	}
	file, err := os.OpenFile(filepath.Join(target, "pages-30.img"), os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteAt([]byte{0x44}, 4095); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = descendantCompareImageMemory(target, full)
	if err == nil || !strings.Contains(err.Error(), "image=1 address=0x2fff") {
		t.Fatalf("same-capture oracle hid a corrupt final local byte: %v", err)
	}
}

func TestOraclePlanRejectsIncompleteCommitAndForeignImagePID(t *testing.T) {
	_, head, _ := imageGraphFixture(t)
	path := filepath.Join(head, "plan.bin")
	for _, header := range [][4]uint64{
		{0, 1, 7, 0}, {descendantOracleMagic, 2, 7, 0},
		{descendantOracleMagic, 1, 7, 1}, {descendantOracleMagic, 1, 1 << 31, 0},
	} {
		var data bytes.Buffer
		if err := binary.Write(&data, binary.LittleEndian, header); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := descendantReadOraclePlan(path, "1"); err == nil {
			t.Fatal("accepted an incomplete or foreign-image decision", header)
		}
	}
}
