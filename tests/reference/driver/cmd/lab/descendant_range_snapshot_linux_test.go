package main

import (
	"bytes"
	"errors"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
	"google.golang.org/protobuf/encoding/protowire"
)

func rangeSnapshotFixture(t *testing.T) (string, string, string, string) {
	t.Helper()
	head, parent, control := inheritedMemoryFixture(t)
	for _, name := range []string{"inventory.img", "pstree.img"} {
		if err := os.WriteFile(filepath.Join(parent, name), []byte("parent-"+name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	target := filepath.Join(filepath.Dir(control), "complete")
	return head, parent, control, target
}

func TestRangeSnapshotCurrentMemorySurvivesAncestorRemoval(t *testing.T) {
	head, parent, control, target := rangeSnapshotFixture(t)
	proof, err := descendantCompileRangeSnapshot(head, target)
	if err != nil {
		t.Fatal(err)
	}
	if proof["kernel_range_clone_bytes"] != int64(3*4096) ||
		proof["complete_range_snapshot"] != true || proof["memory_maps"] != 1 {
		t.Fatal("range snapshot did not assemble exactly current logical memory", proof)
	}
	for _, name := range []string{"parent", "lazy-parent-1.bin", "pages-1.img", "pages-20.img"} {
		if _, err := os.Lstat(filepath.Join(target, name)); !os.IsNotExist(err) {
			t.Fatalf("complete snapshot retained a parent or obsolete payload: %s %v", name, err)
		}
	}
	file, err := os.Open(filepath.Join(target, "pagemap-1.img"))
	if err != nil {
		t.Fatal(err)
	}
	saved, readErr := criuReadSavedMap(file)
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || len(saved.Ranges) != 3 ||
		saved.Ranges[0].Flags != 4 || saved.Ranges[1].Flags != 4 || saved.Ranges[2].Flags != 6 {
		t.Fatal("complete snapshot changed lazy policy or retained inherited ranges", saved, readErr, closeErr)
	}
	if err := errors.Join(os.RemoveAll(head), os.RemoveAll(parent)); err != nil {
		t.Fatal(err)
	}
	compared, err := descendantCompareImageMemory(target, control)
	if err != nil || compared["compared_bytes"] != int64(3*4096) || compared["inherited_bytes"] != int64(0) {
		t.Fatal("complete snapshot depends on removed ancestors", compared, err)
	}
	again := filepath.Join(filepath.Dir(target), "complete-again")
	repeated, err := descendantCompileRangeSnapshot(target, again)
	if err != nil || repeated["kernel_range_clone_bytes"] != int64(3*4096) {
		t.Fatal("repeated range assembly grew beyond current logical memory", repeated, err)
	}
	if err := os.RemoveAll(target); err != nil {
		t.Fatal(err)
	}
	if _, err := descendantCompareImageMemory(again, control); err != nil {
		t.Fatal(err)
	}
	payload := filepath.Join(again, "pages-10.img")
	file, err = os.OpenFile(payload, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := file.WriteAt([]byte{0xff}, 3*4096-1)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		t.Fatal(err)
	}
	if _, err := descendantCompareImageMemory(again, control); err == nil {
		t.Fatal("original-writer oracle accepted a corrupt last byte")
	}
}

func TestRangeSnapshotRejectsMissingAncestorAndPreservesSource(t *testing.T) {
	head, parent, _, target := rangeSnapshotFixture(t)
	if err := os.Remove(filepath.Join(parent, "pages-20.img")); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(head, "pages-10.img"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := descendantCompileRangeSnapshot(head, target); err == nil {
		t.Fatal("accepted missing inherited memory")
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatal("partial complete snapshot was not removed", err)
	}
	after, err := os.ReadFile(filepath.Join(head, "pages-10.img"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed assembly changed source", err)
	}
}

func TestRangeSnapshotPreservesExistingTarget(t *testing.T) {
	head, _, _, target := rangeSnapshotFixture(t)
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(target, "keep")
	if err := os.WriteFile(marker, []byte("retained"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := descendantCompileRangeSnapshot(head, target); err == nil {
		t.Fatal("assembly reused an existing target")
	}
	actual, err := os.ReadFile(marker)
	if err != nil || string(actual) != "retained" {
		t.Fatal("existing target changed", err)
	}
}

func TestRangeSnapshotRejectsDuplicatePayloadIdentity(t *testing.T) {
	head, _, _, target := rangeSnapshotFixture(t)
	duplicate, err := os.ReadFile(filepath.Join(head, "pagemap-1.img"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(head, "pagemap-2.img"), duplicate, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := descendantCompileRangeSnapshot(head, target); err == nil {
		t.Fatal("accepted ambiguous destination page-file identity")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("ambiguous output was retained", err)
	}
}

func TestRangeSnapshotRequiresKernelRangeClone(t *testing.T) {
	head, _, _, target := rangeSnapshotFixture(t)
	data, err := os.ReadFile(filepath.Join(head, "pages-10.img"))
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.CreateTemp("/dev/shm", "range-snapshot-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(file.Name()) })
	if _, err := file.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	page := filepath.Join(head, "pages-10.img")
	if err := unix.Mount(file.Name(), page, "", unix.MS_BIND, ""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { unix.Unmount(page, 0) })
	if _, err := descendantCompileRangeSnapshot(head, target); !errors.Is(err, syscall.EXDEV) {
		t.Fatalf("range reflink silently fell back to copying: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("unsupported partial output was retained", err)
	}
}

func TestRangeSnapshotSplitsInheritedExtentAtParentBoundaries(t *testing.T) {
	head, parent, control, target := rangeSnapshotFixture(t)
	original, err := os.ReadFile(filepath.Join(control, "pages-30.img"))
	if err != nil {
		t.Fatal(err)
	}
	for path, data := range map[string][]byte{
		filepath.Join(head, "pagemap-1.img"): prefetchTestImage(
			map[protowire.Number]uint64{1: 10},
			map[protowire.Number]uint64{1: 0x1000, 2: 3, 4: 3}),
		filepath.Join(parent, "pagemap-7.img"): prefetchTestImage(
			map[protowire.Number]uint64{1: 20},
			map[protowire.Number]uint64{1: 0x1000, 2: 1, 4: 4},
			map[protowire.Number]uint64{1: 0x2000, 2: 2, 4: 6}),
		filepath.Join(parent, "pages-20.img"): original,
	} {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := descendantCompileRangeSnapshot(head, target); err != nil {
		t.Fatal(err)
	}
	compared, err := descendantCompareImageMemory(target, control)
	if err != nil || compared["compared_bytes"] != int64(3*4096) || compared["inherited_bytes"] != int64(0) {
		t.Fatal("range assembly skipped a parent boundary", compared, err)
	}
}

func TestRangeSnapshotRuntimeTargetRetiresOnlyVerifiedTemporaryGraph(t *testing.T) {
	head, _, control, _ := rangeSnapshotFixture(t)
	if err := os.MkdirAll("/experiment/branches", 0755); err != nil {
		t.Fatal(err)
	}
	target, err := descendantOwnedLayout("branch-55555555555555555555555555555555")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(target.Root); !os.IsNotExist(err) {
		t.Fatal("native target is not fresh", err)
	}
	capture := &descendantInheritedCapture{Images: head, OriginalImages: control,
		Proof: map[string]any{"same_capture_original_writer": true}}
	graph, expected, err := descendantCloneInheritedTarget(capture, target)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(target.Root) })
	if graph["input_graph_retired_after_complete_byte_oracle"] != true ||
		graph["kernel_range_clone_bytes"] != int64(3*4096) ||
		expected["inventory.img"] == "" || expected["pstree.img"] == "" {
		t.Fatal("runtime adapter did not certify complete target", graph, expected)
	}
	if _, err := os.Lstat(target.Root + "/range-input"); !os.IsNotExist(err) {
		t.Fatal("runtime adapter retained temporary ancestors", err)
	}
	if _, err := descendantCompareImageMemory(head, control); err != nil {
		t.Fatal("temporary graph retirement changed original capture", err)
	}
	final, err := descendantVerifyInheritedTargetGraph(graph, target)
	if err != nil || final["complete_range_snapshot"] != true || final["private_parent_links_verified"] != 0 {
		t.Fatal("complete target failed final runtime verification", final, err)
	}
	file, err := os.OpenFile(target.Root+"/images/pages-10.img", os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := file.WriteAt([]byte{0xff}, 3*4096-1)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		t.Fatal(err)
	}
	if _, err := descendantVerifyInheritedTargetGraph(graph, target); err == nil {
		t.Fatal("runtime adapter accepted changed complete snapshot")
	}
}

func TestRangeSnapshotFastRuntimeTargetUsesExplicitCapturedReference(t *testing.T) {
	head, _, control, _ := rangeSnapshotFixture(t)
	if err := os.MkdirAll("/experiment/branches", 0755); err != nil {
		t.Fatal(err)
	}
	target, err := descendantOwnedLayout("branch-77777777777777777777777777777777")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(target.Root) })
	capture := &descendantInheritedCapture{Images: head, Proof: map[string]any{
		"same_capture_original_writer": false, "ordinary_fast_collector": true,
		"single_source_capture": true, "single_capture_transaction_completed": true,
	}}
	capture.OriginalImages = control
	if _, _, err := descendantCloneInheritedTargetInputPolicy(capture, target, true, true); err == nil {
		t.Fatal("ordinary collection accepted a hidden original-writer reference")
	}
	capture.OriginalImages = ""
	graph, expected, err := descendantCloneInheritedTargetInputPolicy(capture, target, true, true)
	if err != nil {
		t.Fatal(err)
	}
	compared, ok := graph["compiled_captured_graph_memory"].(map[string]any)
	if !ok || compared["all_captured_graph_bytes_match"] != true ||
		compared["all_original_writer_bytes_match"] != nil || graph["compiled_original_writer_memory"] != nil ||
		graph["same_capture_original_writer_images"] != nil || graph["captured_reference_images"] != head ||
		graph["temporary_input_graph_created"] != false || expected["inventory.img"] == "" {
		t.Fatal("ordinary target falsely claimed the original writer or lost its complete certificate", graph, expected)
	}
	if _, err := descendantCompareImageMemory(target.Root+"/images", control); err != nil {
		t.Fatal("independent complete original writer disagrees with fast target", err)
	}
	if final, err := descendantVerifyInheritedTargetGraph(graph, target); err != nil ||
		final["all_graph_payload_hashes_match"] != true || final["complete_range_snapshot"] != true {
		t.Fatal("ordinary target lost independent final verification", final, err)
	}
}

func rangeSnapshotRAMFixture(t *testing.T, readonly bool, limit string) (string, string, string, string) {
	t.Helper()
	head, parent, control, target := rangeSnapshotFixture(t)
	ram := filepath.Join(filepath.Dir(control), "ram")
	if err := os.Mkdir(ram, 0700); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mount("range-ram-parent", ram, "tmpfs", unix.MS_NOSUID|unix.MS_NODEV|unix.MS_NOEXEC,
		"size="+limit+",mode=0700"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { unix.Unmount(ram, 0) })
	payload, err := os.ReadFile(filepath.Join(parent, "pages-20.img"))
	if err != nil {
		t.Fatal(err)
	}
	backing := filepath.Join(ram, "pages")
	if err := os.WriteFile(backing, payload, 0600); err != nil {
		t.Fatal(err)
	}
	page := filepath.Join(parent, "pages-20.img")
	if err := unix.Mount(backing, page, "", unix.MS_BIND, ""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { unix.Unmount(page, 0) })
	if readonly {
		if err := unix.Mount("", page, "", unix.MS_BIND|unix.MS_REMOUNT|unix.MS_RDONLY, ""); err != nil {
			t.Fatal(err)
		}
	}
	return head, control, target, backing
}

func TestRangeSnapshotDirectCopiesOnlyCurrentReadonlyRAMExtents(t *testing.T) {
	head, control, target, backing := rangeSnapshotRAMFixture(t, true, "16m")
	proof, err := descendantCompileDirectRangeSnapshot(head, target)
	if err != nil {
		t.Fatal(err)
	}
	if proof["direct_original_graph"] != true || proof["logical_memory_bytes"] != int64(3*4096) ||
		proof["kernel_range_clone_bytes"] != int64(4096) ||
		proof["verified_ram_range_copy_bytes"] != int64(2*4096) {
		t.Fatal("direct assembly copied obsolete RAM bytes or misreported kernel clones", proof)
	}
	if err := os.WriteFile(backing, bytes.Repeat([]byte{0x39}, 3*4096), 0600); err != nil {
		t.Fatal("direct source leases were not released", err)
	}
	compared, err := descendantCompareImageMemory(target, control)
	if err != nil || compared["compared_bytes"] != int64(3*4096) || compared["inherited_bytes"] != int64(0) {
		t.Fatal("original RAM mutation changed complete target", compared, err)
	}
}

func TestRangeSnapshotDirectRejectsUnsupportedRAMAndPreservesSource(t *testing.T) {
	for _, value := range []struct {
		name, limit string
		readonly    bool
	}{{"writable", "16m", false}, {"oversized", "2g", true}} {
		t.Run(value.name, func(t *testing.T) {
			head, control, target, _ := rangeSnapshotRAMFixture(t, value.readonly, value.limit)
			if _, err := descendantCompileDirectRangeSnapshot(head, target); err == nil {
				t.Fatal("direct assembly accepted unsupported RAM")
			}
			if _, err := os.Lstat(target); !os.IsNotExist(err) {
				t.Fatal("direct assembly retained a rejected partial target", err)
			}
			if _, err := descendantCompareImageMemory(head, control); err != nil {
				t.Fatal("rejected direct assembly changed original bytes", err)
			}
		})
	}
}

func TestRangeSnapshotDirectRuntimeTargetHasNoIntermediateClone(t *testing.T) {
	head, _, control, _ := rangeSnapshotFixture(t)
	if err := os.MkdirAll("/experiment/branches", 0755); err != nil {
		t.Fatal(err)
	}
	target, err := descendantOwnedLayout("branch-66666666666666666666666666666666")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(target.Root); !os.IsNotExist(err) {
		t.Fatal("direct runtime target is not fresh", err)
	}
	graph, _, err := descendantCloneInheritedTargetPolicy(&descendantInheritedCapture{
		Images: head, OriginalImages: control, Proof: map[string]any{"same_capture_original_writer": true}},
		target, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(target.Root) })
	if graph["direct_original_graph"] != true || graph["temporary_input_graph_created"] != false ||
		graph["logical_memory_bytes"] != int64(3*4096) || graph["input_graph"] != nil {
		t.Fatal("direct runtime adapter secretly prepared a cloned input", graph)
	}
	if _, err := descendantVerifyInheritedTargetGraph(graph, target); err != nil {
		t.Fatal(err)
	}
	if _, err := descendantCompareImageMemory(head, control); err != nil {
		t.Fatal("direct adapter modified original memory", err)
	}
}

func TestRangeSnapshotRAMReadbackRejectsChangedDestination(t *testing.T) {
	head, _, target, _ := rangeSnapshotRAMFixture(t, true, "16m")
	lease, err := descendantAcquireFileLease(filepath.Join(filepath.Dir(head), "parent/pages-20.img"))
	if err != nil {
		t.Fatal(err)
	}
	defer lease.close()
	file, err := os.OpenFile(target, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	copied, err := descendantCopyLeasedRAMRange(lease, file, 0, 4096, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteAt([]byte{0xff}, 4095); err != nil {
		t.Fatal(err)
	}
	if err := descendantVerifySyncedRAMRanges(file, []descendantRAMRangeReadback{copied}); err == nil {
		t.Fatal("RAM range readback accepted changed destination bytes")
	}
}

func rangeSnapshotProcessReadBytes(t *testing.T) uint64 {
	t.Helper()
	data, err := os.ReadFile("/proc/self/io")
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(string(data))
	for index := 0; index+1 < len(fields); index += 2 {
		if fields[index] == "rchar:" {
			value, err := strconv.ParseUint(fields[index+1], 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			return value
		}
	}
	t.Fatal("native kernel did not expose process read accounting")
	return 0
}

func TestRangeSnapshotLeaseOnlyAvoidsEagerUnusedPayloadRead(t *testing.T) {
	head, parent, control, target := rangeSnapshotFixture(t)
	// The current map requires only three pages. The old parent may retain
	// much more memory from its own generation; it must not be read eagerly.
	if err := os.Truncate(filepath.Join(parent, "pages-20.img"), 64<<20); err != nil {
		t.Fatal(err)
	}
	before := rangeSnapshotProcessReadBytes(t)
	proof, err := descendantCompileRangeSnapshotLeasedInputs(head, target)
	after := rangeSnapshotProcessReadBytes(t)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := descendantCompareImageMemory(target, control); err != nil {
		t.Fatal("avoiding unused input reads changed current memory", err)
	}
	t.Logf("actual kernel rchar delta=%d for current logical bytes=%v", after-before, proof["logical_memory_bytes"])
	if after-before > 1<<20 {
		t.Fatalf("range assembly eagerly read obsolete page payload: kernel rchar delta=%d", after-before)
	}
}

func TestRangeSnapshotLeaseOnlyRejectsAnOpenWriter(t *testing.T) {
	head, parent, control, target := rangeSnapshotFixture(t)
	writer, err := os.OpenFile(filepath.Join(parent, "pages-20.img"), os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if _, err := descendantCompileRangeSnapshotLeasedInputs(head, target); err == nil {
		t.Fatal("range assembly accepted a source with an open writer")
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatal("rejected writer retained a partial target", err)
	}
	if _, err := descendantCompareImageMemory(head, control); err != nil {
		t.Fatal("rejected writer changed source memory", err)
	}
}

func TestRangeSnapshotLeaseOnlyRetainsLeaseAndPathGuards(t *testing.T) {
	for _, mode := range []string{"new-writer", "replaced-path"} {
		t.Run(mode, func(t *testing.T) {
			_, parent, _, _ := rangeSnapshotFixture(t)
			path := filepath.Join(parent, "pages-20.img")
			notify := make(chan os.Signal, 1)
			signal.Notify(notify, syscall.SIGIO)
			defer signal.Stop(notify)
			lease, err := descendantAcquireFileLeasePolicy(path, false)
			if err != nil {
				t.Fatal(err)
			}
			defer lease.close()
			if lease.SHA256 != "" {
				t.Fatal("lease-only admission read a whole input digest")
			}
			if mode == "new-writer" {
				fd, err := unix.Open(path, unix.O_WRONLY|unix.O_NONBLOCK, 0)
				if err == nil {
					unix.Close(fd)
					t.Fatal("kernel read lease permitted a new writer")
				}
				if !errors.Is(err, syscall.EWOULDBLOCK) {
					t.Fatal("unexpected writer rejection", err)
				}
			} else {
				if err := os.Rename(path, path+".retained"); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, bytes.Repeat([]byte{0x84}, 3*4096), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := lease.verify(); err == nil {
				t.Fatal("lease-only admission lost its original identity/lease guard", mode)
			}
		})
	}
}

func TestRangeSnapshotLeaseOnlyRuntimeAdapterKeepsOriginalWriterOracle(t *testing.T) {
	head, _, control, _ := rangeSnapshotFixture(t)
	if err := os.MkdirAll("/experiment/branches", 0755); err != nil {
		t.Fatal(err)
	}
	target, err := descendantOwnedLayout("branch-77777777777777777777777777777777")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(target.Root); !os.IsNotExist(err) {
		t.Fatal("lease-only runtime target is not fresh", err)
	}
	graph, expected, err := descendantCloneInheritedTargetInputPolicy(&descendantInheritedCapture{
		Images: head, OriginalImages: control, Proof: map[string]any{"same_capture_original_writer": true}},
		target, true, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(target.Root) })
	if graph["lease_only_page_inputs"] != true || graph["source_input_page_hash_bytes"] != int64(0) ||
		graph["source_unhashed_page_bytes"].(int64) <= 0 || expected["inventory.img"] == "" {
		t.Fatal("runtime adapter failed to select the bounded page-input lease policy", graph)
	}
	oracle := graph["compiled_original_writer_memory"].(map[string]any)
	if oracle["all_original_writer_bytes_match"] != true || oracle["compared_bytes"] != int64(3*4096) {
		t.Fatal("runtime adapter dropped its complete original-writer byte oracle", oracle)
	}
	if _, err := descendantVerifyInheritedTargetGraph(graph, target); err != nil {
		t.Fatal(err)
	}
}
