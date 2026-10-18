package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
)

func inheritedMemoryFixture(t *testing.T) (string, string, string) {
	t.Helper()
	source, head, _ := imageGraphFixture(t)
	parent := filepath.Join(source, "parent")
	control := filepath.Join(filepath.Dir(source), "original")
	for _, path := range []string{parent, control} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path string, data []byte) {
		t.Helper()
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	childMap := prefetchTestImage(
		map[protowire.Number]uint64{1: 10},
		map[protowire.Number]uint64{1: 0x1000, 2: 1, 4: 1},
		map[protowire.Number]uint64{1: 0x2000, 2: 1, 4: 4},
		map[protowire.Number]uint64{1: 0x3000, 2: 1, 4: 3})
	parentMap := prefetchTestImage(
		map[protowire.Number]uint64{1: 20},
		map[protowire.Number]uint64{1: 0x1000, 2: 3, 4: 6})
	originalMap := prefetchTestImage(
		map[protowire.Number]uint64{1: 30},
		map[protowire.Number]uint64{1: 0x1000, 2: 3, 4: 4})
	first := bytes.Repeat([]byte{0x19}, 4096)
	middle := bytes.Repeat([]byte{0xA7}, 4096)
	last := bytes.Repeat([]byte{0x53}, 4096)
	write(filepath.Join(head, "pagemap-1.img"), childMap)
	write(filepath.Join(head, "pages-10.img"), middle)
	write(filepath.Join(parent, "pagemap-7.img"), parentMap)
	write(filepath.Join(parent, "pages-20.img"), append(append(first, bytes.Repeat([]byte{0xE1}, 4096)...), last...))
	write(filepath.Join(control, "pagemap-1.img"), originalMap)
	write(filepath.Join(control, "pages-30.img"), append(append(bytes.Clone(first), middle...), last...))
	var alias [16]byte
	binary.LittleEndian.PutUint64(alias[:8], 0x4c415a59494e4832)
	binary.LittleEndian.PutUint64(alias[8:], 7)
	write(filepath.Join(head, "lazy-parent-1.bin"), alias[:])
	if err := os.Symlink("../parent", filepath.Join(head, "parent")); err != nil {
		t.Fatal(err)
	}
	return head, parent, control
}

func TestInheritedMemoryResolvesAliasesAndLocalChanges(t *testing.T) {
	head, _, control := inheritedMemoryFixture(t)
	proof, err := descendantCompareImageMemory(head, control)
	if err != nil {
		t.Fatal(err)
	}
	if proof["compared_bytes"] != int64(3*4096) ||
		proof["inherited_bytes"] != int64(2*4096) ||
		proof["all_original_writer_bytes_match"] != true {
		t.Fatal("incomplete logical byte oracle", proof)
	}
}

func TestInheritedMemoryCapturedGraphComparisonKeepsOriginalOracleStrict(t *testing.T) {
	head, parent, control := inheritedMemoryFixture(t)
	if _, err := descendantCompareImageMemory(control, head); err == nil ||
		!strings.Contains(err.Error(), "not a complete original writer") {
		t.Fatalf("original-writer oracle admitted an inherited reference: %v", err)
	}
	proof, err := descendantCompareCapturedMemory(control, head)
	if err != nil || proof["all_captured_graph_bytes_match"] != true ||
		proof["reference_is_captured_graph"] != true ||
		proof["all_original_writer_bytes_match"] != nil || proof["compared_bytes"] != int64(12288) {
		t.Fatalf("captured graph comparison lost bytes or claimed an original writer: %v %v", proof, err)
	}
	file, err := os.OpenFile(filepath.Join(parent, "pages-20.img"), os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := file.WriteAt([]byte{0x99}, 12287)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatal(writeErr, closeErr)
	}
	_, err = descendantCompareCapturedMemory(control, head)
	if err == nil || !strings.Contains(err.Error(), "image=1 address=0x3fff compared_bytes=12287") {
		t.Fatalf("captured reference corruption was not rejected at its final byte: %v", err)
	}
}

func TestInheritedMemoryRejectsMissingParent(t *testing.T) {
	head, _, control := inheritedMemoryFixture(t)
	if err := os.Remove(filepath.Join(head, "parent")); err != nil {
		t.Fatal(err)
	}
	if _, err := descendantCompareImageMemory(head, control); err == nil {
		t.Fatal("accepted a flat inherited snapshot with missing bytes")
	}
}

func TestInheritedMemoryRejectsWrongParentBytes(t *testing.T) {
	head, parent, control := inheritedMemoryFixture(t)
	file, err := os.OpenFile(filepath.Join(parent, "pages-20.img"), os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteAt([]byte{0x44}, 0); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = descendantCompareImageMemory(head, control)
	if err == nil || !strings.Contains(err.Error(), "image=1 address=0x1000") {
		t.Fatalf("original writer did not catch the inherited byte change: %v", err)
	}
}

func TestInheritedMemoryAcceptsDifferentReferenceExtentBoundaries(t *testing.T) {
	head, parent, control := inheritedMemoryFixture(t)
	childMap := prefetchTestImage(
		map[protowire.Number]uint64{1: 10},
		map[protowire.Number]uint64{1: 0x1000, 2: 3, 4: 3})
	originalMap := prefetchTestImage(
		map[protowire.Number]uint64{1: 30},
		map[protowire.Number]uint64{1: 0x1000, 2: 1, 4: 4},
		map[protowire.Number]uint64{1: 0x2000, 2: 2, 4: 4})
	originalBytes, err := os.ReadFile(filepath.Join(control, "pages-30.img"))
	if err != nil {
		t.Fatal(err)
	}
	for path, data := range map[string][]byte{
		filepath.Join(head, "pagemap-1.img"):    childMap,
		filepath.Join(control, "pagemap-1.img"): originalMap,
		filepath.Join(parent, "pages-20.img"):   originalBytes,
	} {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	proof, err := descendantCompareImageMemory(head, control)
	if err != nil {
		t.Fatal(err)
	}
	if proof["compared_bytes"] != int64(3*4096) || proof["inherited_bytes"] != int64(3*4096) ||
		proof["all_original_writer_bytes_match"] != true {
		t.Fatal("extent boundaries skipped original-writer bytes", proof)
	}
	parentFile, err := os.OpenFile(filepath.Join(parent, "pages-20.img"), os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := parentFile.WriteAt([]byte{originalBytes[len(originalBytes)-1] ^ 0xff}, int64(len(originalBytes)-1))
	closeErr := parentFile.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatal(writeErr, closeErr)
	}
	_, err = descendantCompareImageMemory(head, control)
	if err == nil || !strings.Contains(err.Error(), "image=1 address=0x3fff compared_bytes=12287") {
		t.Fatalf("extent-boundary comparison skipped a corrupt final byte: %v", err)
	}
}
