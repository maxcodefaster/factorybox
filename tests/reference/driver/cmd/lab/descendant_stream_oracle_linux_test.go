package main

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
)

func streamOriginalFixture(t *testing.T) string {
	t.Helper()
	head, _, control, _ := rangeSnapshotFixture(t)
	original := head + "/original-writer"
	if err := os.Mkdir(original, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(head + "/pages-1.img"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(control + "/pages-30.img")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		original + "/pages-10.img": data,
		original + "/pagemap-1.img": prefetchTestImage(
			map[protowire.Number]uint64{1: 10},
			map[protowire.Number]uint64{1: 0x1000, 2: 3, 4: 4}),
	}
	header := []uint64{0x4c53545245414d31, 1, 3 * 4096, 3 * 4096, 4096, 2 * 4096}
	var encoded []byte
	for _, value := range header {
		encoded = binary.LittleEndian.AppendUint64(encoded, value)
	}
	files[head+"/lazy-stream-1.bin"] = encoded
	encoded = nil
	for _, value := range []uint64{descendantOracleMagic, 1, 7, 2, 0x1000, 0x2000, 0x3000, 0x4000} {
		encoded = binary.LittleEndian.AppendUint64(encoded, value)
	}
	files[head+"/lazy-oracle-1.bin"] = encoded
	for path, data := range files {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return head
}

func TestStreamOriginalMetadataAdmissionAndByteOracle(t *testing.T) {
	head := streamOriginalFixture(t)
	proof, err := descendantPrepareStreamOriginal(head)
	if err != nil {
		t.Fatal(err)
	}
	if proof["go_derived_candidate"] != false || proof["native_stream_original_writer"] != true ||
		proof["original_bytes"] != int64(3*4096) || proof["inherited_bytes"] != int64(2*4096) {
		t.Fatal("stream original writer did not retain exact input provenance", proof)
	}
	compared, err := descendantCompareImageMemory(head, head+"/original-writer")
	if err != nil || compared["compared_bytes"] != int64(3*4096) {
		t.Fatal("stream task bytes differ from their complete original writer", compared, err)
	}
	if _, err := descendantPrepareStreamOriginal(head); err == nil {
		t.Fatal("reused a completed stream metadata admission")
	}
	for _, name := range []string{"inventory.img", "pstree.img", "lazy-oracle-1.bin", "lazy-parent-1.bin"} {
		source, err := os.Stat(filepath.Join(head, name))
		dest, destErr := os.Stat(filepath.Join(head, "original-writer", name))
		if err != nil || destErr != nil || os.SameFile(source, dest) {
			t.Fatal("original writer metadata was not independently cloned", name, err, destErr)
		}
	}
}

func TestStreamOriginalRejectsIncompleteAndForeignFrame(t *testing.T) {
	for _, mode := range []string{"header", "counter", "short", "alias", "extra-page"} {
		t.Run(mode, func(t *testing.T) {
			head := streamOriginalFixture(t)
			path, offset := head+"/lazy-stream-1.bin", int64(0)
			switch mode {
			case "counter":
				offset = 32
			case "short":
				if err := os.Truncate(head+"/original-writer/pages-10.img", 4096); err != nil {
					t.Fatal(err)
				}
			case "alias":
				path, offset = head+"/lazy-oracle-1.bin", 16
			case "extra-page":
				if err := os.WriteFile(head+"/pages-999.img", []byte("unreferenced"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "header" || mode == "counter" || mode == "alias" {
				file, err := os.OpenFile(path, os.O_WRONLY, 0)
				if err != nil {
					t.Fatal(err)
				}
				_, writeErr := file.WriteAt(binary.LittleEndian.AppendUint64(nil, 13), offset)
				if err := errors.Join(writeErr, file.Close()); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := descendantPrepareStreamOriginal(head); err == nil {
				t.Fatal("accepted an invalid same-stream original writer", mode)
			}
			if _, err := os.Lstat(head + "/original-writer/streaming-original-writer-complete.json"); !os.IsNotExist(err) {
				t.Fatal("rejected stream writer published a completion", err)
			}
		})
	}
}

func TestStreamOriginalMapsFreshWriterOwnershipWithoutFollowingParent(t *testing.T) {
	head := streamOriginalFixture(t)
	parent := filepath.Join(filepath.Dir(head), "parent/pages-20.img")
	before, err := os.Lstat(parent)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(head, 165536, 165536); err != nil {
		t.Fatal(err)
	}
	if _, err := descendantPrepareStreamOriginal(head); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{head + "/pagemap-1.img", head + "/pages-10.img",
		head + "/original-writer/pages-10.img", head + "/original-writer/inventory.img"} {
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		id := descendantLeaseFileIdentity(info)
		if id.UID != 165536 || id.GID != 165536 || info.Mode().Perm() != 0600 {
			t.Fatal("raw capture did not receive private mapped-root ownership", path, id)
		}
	}
	after, err := os.Lstat(parent)
	if err != nil || descendantLeaseFileIdentity(before) != descendantLeaseFileIdentity(after) {
		t.Fatal("normalizing fresh stream outputs modified the readonly parent", err)
	}
	if _, err := descendantCompareImageMemory(head, head+"/original-writer"); err != nil {
		t.Fatal("mapped ownership normalization changed captured bytes", err)
	}
}

func TestStreamOriginalRejectsForeignWriterInodeBeforeOwnershipChanges(t *testing.T) {
	head := streamOriginalFixture(t)
	path := head + "/foreign.img"
	if err := os.WriteFile(path, []byte("foreign owned output"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(path, 424242, 424242); err != nil {
		t.Fatal(err)
	}
	if _, err := descendantPrepareStreamOriginal(head); err == nil {
		t.Fatal("stream preparation claimed a foreign output")
	}
	info, err := os.Lstat(path)
	if err != nil || descendantLeaseFileIdentity(info).UID != 424242 {
		t.Fatal("foreign output ownership changed", err)
	}
}
