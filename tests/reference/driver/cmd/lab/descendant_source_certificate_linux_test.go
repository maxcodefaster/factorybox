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

func sourceCertificateFixture(t *testing.T) (descendantLayout, *descendantInheritedCapture, string) {
	t.Helper()
	head, oldParent, control, _ := rangeSnapshotFixture(t)
	source, err := descendantOwnedLayout("copy-sysbox")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(source.Root); !os.IsNotExist(err) {
		t.Fatal("source certificate fixture requires an absent owned runtime", err)
	}
	t.Cleanup(func() { os.RemoveAll(source.Root) })
	if err := os.MkdirAll(source.Root, 0755); err != nil {
		t.Fatal(err)
	}
	images, parent := source.Root+"/images-11111111111111111111111111111111", source.Root+"/images"
	for _, pair := range [][2]string{{head, images}, {oldParent, parent}} {
		if err := os.Mkdir(pair[1], 0700); err != nil {
			t.Fatal(err)
		}
		entries, err := os.ReadDir(pair[0])
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if entry.Name() == "parent" {
				continue
			}
			data, err := os.ReadFile(filepath.Join(pair[0], entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(pair[1], entry.Name())
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chown(path, 165536, 165536); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Chown(pair[1], 165536, 165536); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("../images", images+"/parent"); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(images+"/pages-10.img", 64<<20); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(parent)
	if err != nil {
		t.Fatal(err)
	}
	identity := descendantLeaseFileIdentity(info)
	capture := &descendantInheritedCapture{Images: images, Proof: map[string]any{
		"runtime": source, "parent_images": parent, "parent_device": identity.Device, "parent_inode": identity.Inode,
		"ordinary_fast_collector": true, "same_capture_original_writer": false,
		"single_source_capture": true, "single_capture_transaction_completed": true, "guard_safe_after_only_writer": true,
	}}
	return source, capture, control
}

func TestRangeSnapshotSourceCertificateAvoidsEagerPageHashes(t *testing.T) {
	source, capture, control := sourceCertificateFixture(t)
	before := rangeSnapshotProcessReadBytes(t)
	protection, metadata, err := descendantProtectInheritedSource(source, capture)
	admissionRead := rangeSnapshotProcessReadBytes(t) - before
	if err != nil {
		t.Fatal(err)
	}
	defer protection.close(false)
	before = rangeSnapshotProcessReadBytes(t)
	full, err := descendantSnapshotManifest(capture.Images)
	fullRead := rangeSnapshotProcessReadBytes(t) - before
	if err != nil {
		t.Fatal(err)
	}
	for name, checksum := range metadata {
		if criuRAMPageName.MatchString(name) || checksum == "" {
			t.Fatal("source metadata falsely claimed a page-content hash", name, checksum)
		}
		if !criuInheritAliasName.MatchString(name) && full[name] != checksum {
			t.Fatal("source metadata differs from the independent complete hash", name)
		}
	}
	if admissionRead >= 1<<20 || fullRead < 64<<20 {
		t.Fatalf("source certificate did not avoid eager payload reading: admission=%d full=%d", admissionRead, fullRead)
	}
	t.Logf("actual kernel rchar: source certificate=%d complete source hash=%d", admissionRead, fullRead)
	if err := os.MkdirAll("/experiment/branches", 0755); err != nil {
		t.Fatal(err)
	}
	target, _ := descendantOwnedLayout("branch-99999999999999999999999999999999")
	t.Cleanup(func() { os.RemoveAll(target.Root) })
	var targetProtection *descendantBackingLeases
	graph, _, err := descendantCloneInheritedTargetProtectedPolicy(capture, target, true, true, &targetProtection)
	if err != nil {
		t.Fatal(err)
	}
	defer targetProtection.close(false)
	if _, err := descendantCompareImageMemory(target.Root+"/images", control); err != nil {
		t.Fatal("independent complete original writer disagrees with the source-identity target", err)
	}
	if _, err := descendantVerifyInheritedTargetGraph(graph, target); err != nil {
		t.Fatal("independent final target hashes failed", err)
	}
	again, err := protection.manifest()
	if err != nil || asJSON(again) != asJSON(metadata) {
		t.Fatal("source identity protection did not survive actual target assembly and byte oracles", again, err)
	}
	for _, lease := range protection.Backing.Pages {
		if lease.SHA256 != "" {
			t.Fatal("source page certificate fabricated an original-writer hash")
		}
	}
	if err := protection.close(true); err != nil {
		t.Fatal(err)
	}
	if _, err := protection.manifest(); err == nil {
		t.Fatal("closed source certificate still admitted its capture")
	}
}

func TestRangeSnapshotSourceCertificateRejectsBrokenIdentity(t *testing.T) {
	for _, mode := range []string{"page-writer", "alias-writer", "replaced-page", "changed-parent", "missing-page", "closed"} {
		t.Run(mode, func(t *testing.T) {
			source, capture, _ := sourceCertificateFixture(t)
			protection, _, err := descendantProtectInheritedSource(source, capture)
			if err != nil {
				t.Fatal(err)
			}
			defer protection.close(false)
			page := capture.Images + "/pages-10.img"
			switch mode {
			case "page-writer", "alias-writer":
				path := page
				if mode == "alias-writer" {
					path = capture.Images + "/lazy-parent-1.bin"
				}
				fd, err := unix.Open(path, unix.O_WRONLY|unix.O_NONBLOCK, 0)
				if err == nil {
					unix.Close(fd)
					t.Fatal("source read lease admitted a writer", mode)
				}
				if !errors.Is(err, syscall.EWOULDBLOCK) {
					t.Fatal("unexpected source writer rejection", err)
				}
			case "replaced-page":
				if err := os.Rename(page, page+".retained"); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(page, bytes.Repeat([]byte{0x72}, 4096), 0600); err != nil {
					t.Fatal(err)
				}
			case "changed-parent":
				if err := os.Remove(capture.Images + "/parent"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("../wrong-parent", capture.Images+"/parent"); err != nil {
					t.Fatal(err)
				}
			case "missing-page":
				for name, lease := range protection.Backing.Pages {
					if err := lease.close(); err != nil {
						t.Fatal(err)
					}
					delete(protection.Backing.Pages, name)
					break
				}
			case "closed":
				if err := protection.close(true); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := protection.manifest(); err == nil {
				t.Fatal("broken source identity certificate was accepted", mode)
			}
		})
	}
}

func TestRangeSnapshotSourceSyncDoesNotReadPagesOrDropLeases(t *testing.T) {
	source, capture, _ := sourceCertificateFixture(t)
	protection, _, err := descendantProtectInheritedSource(source, capture)
	if err != nil {
		t.Fatal(err)
	}
	defer protection.close(false)
	before := rangeSnapshotProcessReadBytes(t)
	synced, err := protection.syncCapture()
	read := rangeSnapshotProcessReadBytes(t) - before
	if err != nil || synced["original_read_leases_retained"] != true ||
		synced["synced_files"] != protection.PageFiles+protection.MetaFiles ||
		synced["page_content_hash_bytes"] != int64(0) || read >= 1<<20 {
		t.Fatal("source flush reread page data or lost its original file leases", synced, read, err)
	}
	t.Logf("actual kernel rchar during source-only sync=%d; logical capture bytes=%v",
		read, synced["synced_logical_bytes"])
	if err := protection.verify(); err != nil {
		t.Fatal("source sync changed its original captured identity", err)
	}
	if err := protection.close(true); err != nil {
		t.Fatal(err)
	}
	if _, err := protection.syncCapture(); err == nil {
		t.Fatal("closed source capture was admitted for sync")
	}
}

func TestRangeSnapshotSourceCertificateRejectsUnownedAdmission(t *testing.T) {
	for _, mode := range []string{"unacknowledged", "wrong-parent", "foreign-page", "open-writer", "audit-shadow"} {
		t.Run(mode, func(t *testing.T) {
			source, capture, _ := sourceCertificateFixture(t)
			switch mode {
			case "unacknowledged":
				capture.Proof["guard_safe_after_only_writer"] = false
			case "wrong-parent":
				capture.Proof["parent_inode"] = uint64(1)
			case "foreign-page":
				if err := os.Chown(capture.Images+"/pages-10.img", 165537, 165537); err != nil {
					t.Fatal(err)
				}
			case "open-writer":
				writer, err := os.OpenFile(capture.Images+"/pages-10.img", os.O_WRONLY, 0)
				if err != nil {
					t.Fatal(err)
				}
				defer writer.Close()
			case "audit-shadow":
				capture.OriginalImages = capture.Images + "/original-writer"
			}
			if protection, _, err := descendantProtectInheritedSource(source, capture); err == nil {
				protection.close(false)
				t.Fatal("unowned or unsafe source admission was accepted", mode)
			}
			if _, err := os.Stat(capture.Images + "/pages-10.img"); err != nil {
				t.Fatal("failed source admission removed its captured data", err)
			}
		})
	}
}
