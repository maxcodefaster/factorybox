package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func protectedRangeTargetFixture(t *testing.T) (*descendantInheritedCapture, descendantLayout, map[string]any,
	map[string]string, *descendantBackingLeases) {
	t.Helper()
	head, _, _, _ := rangeSnapshotFixture(t)
	if err := os.MkdirAll("/experiment/branches", 0755); err != nil {
		t.Fatal(err)
	}
	target, err := descendantOwnedLayout("branch-88888888888888888888888888888888")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(target.Root); !os.IsNotExist(err) {
		t.Fatal("protected target fixture must own an absent runtime", err)
	}
	t.Cleanup(func() { os.RemoveAll(target.Root) })
	capture := &descendantInheritedCapture{Images: head, Proof: map[string]any{
		"ordinary_fast_collector": true, "same_capture_original_writer": false,
		"single_source_capture": true, "single_capture_transaction_completed": true,
	}}
	var protection *descendantBackingLeases
	graph, expected, err := descendantCloneInheritedTargetProtectedPolicy(capture, target, true, true, &protection)
	if err != nil || protection == nil {
		t.Fatal("complete target did not transfer its original output leases", err)
	}
	t.Cleanup(func() { protection.close(false) })
	return capture, target, graph, expected, protection
}

func TestRangeSnapshotProtectedTargetAvoidsRepeatedPageReads(t *testing.T) {
	capture, target, graph, expected, protection := protectedRangeTargetFixture(t)
	before := rangeSnapshotProcessReadBytes(t)
	full, err := descendantSnapshotManifest(target.Root + "/images")
	fullRead := rangeSnapshotProcessReadBytes(t) - before
	if err != nil || asJSON(full) != asJSON(expected) {
		t.Fatal("independent full target hash differs", full, err)
	}
	before = rangeSnapshotProcessReadBytes(t)
	certified, err := descendantProtectedTargetManifest(protection, graph, target)
	certifiedRead := rangeSnapshotProcessReadBytes(t) - before
	if err != nil || asJSON(certified) != asJSON(full) {
		t.Fatal("continuous output lease certificate differs from independent full bytes", certified, err)
	}
	if fullRead < uint64(protection.Bytes) || certifiedRead >= uint64(protection.Bytes) {
		t.Fatalf("certificate path reread complete pages: full=%d certified=%d pages=%d",
			fullRead, certifiedRead, protection.Bytes)
	}
	t.Logf("actual kernel rchar: full=%d certified=%d page payload=%d", fullRead, certifiedRead, protection.Bytes)
	compared, err := descendantProtectedTargetMemory(protection, graph, capture, target)
	if err != nil || compared["all_captured_graph_bytes_match"] != true {
		t.Fatal("continuous output leases lost the actual whole-memory comparison", compared, err)
	}
	final, err := descendantVerifyInheritedTargetGraph(graph, target)
	if err != nil || final["all_graph_payload_hashes_match"] != true {
		t.Fatal("mandatory independent final full hashes failed", final, err)
	}
	if err := protection.close(true); err != nil {
		t.Fatal(err)
	}
	if _, err := descendantProtectedTargetMemory(protection, graph, capture, target); err == nil {
		t.Fatal("closed output leases still certified the target")
	}
}

func TestRangeSnapshotProtectedTargetRejectsBrokenCertificates(t *testing.T) {
	for _, mode := range []string{"new-writer", "replaced-path", "changed-metadata", "wrong-capture",
		"missing-page", "wrong-byte-count", "closed"} {
		t.Run(mode, func(t *testing.T) {
			capture, target, graph, _, protection := protectedRangeTargetFixture(t)
			page := target.Root + "/images/pages-10.img"
			switch mode {
			case "new-writer":
				fd, err := unix.Open(page, unix.O_WRONLY|unix.O_NONBLOCK, 0)
				if err == nil {
					unix.Close(fd)
					t.Fatal("continuously leased complete target permitted a writer")
				}
				if !errors.Is(err, syscall.EWOULDBLOCK) {
					t.Fatal("writer was rejected for an unexpected reason", err)
				}
			case "replaced-path":
				if err := os.Rename(page, page+".retained"); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(page, bytes.Repeat([]byte{0x39}, 12288), 0600); err != nil {
					t.Fatal(err)
				}
			case "changed-metadata":
				if err := os.WriteFile(filepath.Join(target.Root, "images/inventory.img"), []byte("changed"), 0600); err != nil {
					t.Fatal(err)
				}
			case "wrong-capture":
				capture.Images += "-wrong"
			case "missing-page":
				for name, lease := range protection.Pages {
					if err := lease.close(); err != nil {
						t.Fatal(err)
					}
					delete(protection.Pages, name)
					break
				}
			case "wrong-byte-count":
				protection.Bytes--
			case "closed":
				if err := protection.close(true); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "changed-metadata" || mode == "missing-page" {
				if _, err := descendantProtectedTargetManifest(protection, graph, target); err == nil {
					t.Fatal("changed metadata or missing page certificate was accepted", mode)
				}
			} else if _, err := descendantProtectedTargetMemory(protection, graph, capture, target); err == nil {
				t.Fatal("broken continuous output lease certificate was accepted", mode)
			}
		})
	}
}
