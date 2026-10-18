//go:build linux

package main

import (
	"context"
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

func TestCommittedEpochRegistrationDoesNotPinLaterBind(t *testing.T) {
	path, expected := committedNativeFixture(t)
	owner := committedSnapshots[path]
	for _, page := range owner.Pages {
		if err := page.File.Close(); err != nil {
			t.Fatal(err)
		}
	}
	owner.Pages = map[string]committedPage{}
	if err := criuParentReadonlyBind(path, path); err != nil {
		t.Fatal(err)
	}
	bound := true
	t.Cleanup(func() {
		if bound {
			_ = criuCloseCommittedSnapshot(path)
			if err := unix.Unmount(path, 0); err != nil {
				t.Error(err)
			}
		}
	})
	// The real epoch registers the pages after adding its self bind. The
	// certificate must keep the original mount alive, not pin this alias.
	if err := criuRegisterCommittedParent(path, expected); err != nil {
		t.Fatal(err)
	}
	if err := unix.Unmount(path, 0); err != nil {
		t.Fatalf("page anchors prevent normal epoch alias retirement: %v", err)
	}
	bound = false
	result := checkEpochParentFiles(context.Background(), path, expected, 1<<20)
	if result.Err != nil || result.KernelBytes != 64 || result.ReadBytes != 2 {
		t.Fatalf("original certificate failed after alias removal: %+v", result)
	}
}

func TestCommittedEpochMountAliasesKeepOriginalIdentity(t *testing.T) {
	path := "/experiment/sysbox/images-again"
	original := "42 1 0:99 / " + path + " ro - tmpfs owned ro,size=1024k\n"
	alias := "31 42 0:99 / " + path + " ro - tmpfs owned ro,size=1024k\n"
	id, err := committedMountWithAnchor(original+alias, path, 42)
	if err != nil || id != 42 {
		t.Fatalf("lost original mount under valid later alias: %d %v", id, err)
	}
	for _, text := range []string{alias, original + alias + alias,
		original + "77 42 0:100 / " + path + " ro - tmpfs other ro\n",
		original + "77 42 0:99 / " + path + " ro - tmpfs owned rw\n"} {
		if _, err := committedMountWithAnchor(text, path, 42); err == nil {
			t.Fatal("accepted missing/duplicate/foreign/writable epoch alias")
		}
	}
}

func TestCommittedEpochRetirementRequiresVerifiedExportAndInvalidatesFirst(t *testing.T) {
	path, expected := committedNativeFixture(t)
	t.Setenv("RUNTIME_FORK_EPOCH_REBASE", "1")
	t.Setenv("RUNTIME_FORK_EPOCH_RAM_PARENT", "1")
	if err := criuRetireCommittedSnapshot(path, map[string]criuRAMFile{}, true); err == nil || committedSnapshots[path] == nil {
		t.Fatal("retired without verified export")
	}
	owner := committedSnapshots[path]
	manifest := map[string]criuRAMFile{}
	for name, page := range owner.Pages {
		manifest[name] = criuRAMFile{Name: name, SHA256: expected[name], Bytes: page.Identity.Size,
			UID: page.Identity.UID, GID: page.Identity.GID, Mode: page.Identity.Mode & 07777}
	}
	old := criuLazyDaemons
	criuLazyDaemons = []*criuLazyDaemon{{}}
	err := criuRetireCommittedSnapshot(path, manifest, true)
	criuLazyDaemons = old
	if err == nil || committedSnapshots[path] == nil {
		t.Fatal("retired while readers remained")
	}
	if err := criuRetireCommittedSnapshot(path, manifest, true); err != nil {
		t.Fatal(err)
	}
	if committedSnapshots[path] != nil {
		t.Fatal("certificate survived writable retirement")
	}
	if result := checkEpochParentFiles(context.Background(), path, expected, 1<<20); result.Err == nil {
		t.Fatal("reused a certificate after retirement")
	}
	if err := os.WriteFile(path+"/retirement-only", []byte("allowed after invalidation"), 0600); err != nil {
		t.Fatal(err)
	}
}
