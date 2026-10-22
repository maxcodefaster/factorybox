//go:build linux

package ownedtree

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func fixture(t *testing.T) (*os.Root, string) {
	t.Helper()
	base := t.TempDir()
	if err := os.MkdirAll(base+"/generation/payload/sub", 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(base+"/generation/payload/sub/file", []byte("retained contents\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(base+"/foreign", []byte("foreign contents\n"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	return root, base
}

func TestRemovalPreservesOpenDataAndExactCompletion(t *testing.T) {
	root, base := fixture(t)
	if err := os.Link(base+"/generation/payload/sub/file", base+"/generation/payload/hardlink"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../../foreign", base+"/generation/payload/sub/link"); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(base+"/generation/payload/fifo", 0600); err != nil {
		t.Fatal(err)
	}
	retained, err := os.Open(base + "/generation/payload/sub/file")
	if err != nil {
		t.Fatal(err)
	}
	defer retained.Close()
	plan, err := Capture(context.Background(), root, []string{"generation/payload"})
	if err != nil {
		t.Fatal(err)
	}
	if err := RemoveRemaining(context.Background(), root, plan); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 18)
	n, err := retained.ReadAt(data, 0)
	if err != nil || string(data[:n]) != "retained contents\n" {
		t.Fatal("unlink damaged live open data", string(data[:n]), err)
	}
	foreign, err := os.ReadFile(base + "/foreign")
	if err != nil || string(foreign) != "foreign contents\n" {
		t.Fatal("retirement followed a payload symlink", err)
	}
	if err := RemoveRemaining(context.Background(), root, plan); err != nil {
		t.Fatal("exact replay failed", err)
	}
	if err := os.Mkdir(base+"/generation/payload", 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(base+"/generation/payload/successor", []byte("preserved"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := RemoveRemaining(context.Background(), root, plan); err == nil {
		t.Fatal("successor was adopted")
	}
	if data, err := os.ReadFile(base + "/generation/payload/successor"); err != nil || string(data) != "preserved" {
		t.Fatal("successor changed", err)
	}
}

func TestExternalLinksAndUnknownEntriesBlockBeforeRemoval(t *testing.T) {
	root, base := fixture(t)
	external := base + "/outside-hardlink"
	if err := os.Link(base+"/generation/payload/sub/file", external); err != nil {
		t.Fatal(err)
	}
	if _, err := Capture(context.Background(), root, []string{"generation/payload"}); err == nil {
		t.Fatal("external hardlink accepted")
	}
	if err := os.Remove(external); err != nil {
		t.Fatal(err)
	}
	plan, err := Capture(context.Background(), root, []string{"generation/payload"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(base+"/generation/payload/foreign", []byte("leave alone"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := RemoveRemaining(context.Background(), root, plan); err == nil {
		t.Fatal("unknown entry accepted")
	}
	if _, err := os.Stat(base + "/generation/payload/sub/file"); err != nil {
		t.Fatal("preflight refusal deleted old data", err)
	}
	if err := os.Remove(base + "/generation/payload/foreign"); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(base+"/generation/payload/sub/file", external); err != nil {
		t.Fatal(err)
	}
	if err := RemoveRemaining(context.Background(), root, plan); err == nil {
		t.Fatal("new external hardlink accepted")
	}
	if data, err := os.ReadFile(external); err != nil || string(data) != "retained contents\n" {
		t.Fatal("external linked data changed", err)
	}
}

func TestRetryRefusesChangedParentAndChangedContents(t *testing.T) {
	root, base := fixture(t)
	plan, err := Capture(context.Background(), root, []string{"generation/payload"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(base+"/generation/payload/sub/file", []byte("different data!!!\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := RemoveRemaining(context.Background(), root, plan); err == nil {
		t.Fatal("changed content accepted")
	}
	if err := os.WriteFile(base+"/generation/payload/sub/file", []byte("retained contents\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(base+"/generation/payload/sub", base+"/old-sub"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(base, "old-sub"), base+"/generation/payload/sub"); err != nil {
		t.Fatal(err)
	}
	if err := RemoveRemaining(context.Background(), root, plan); err == nil {
		t.Fatal("symlink replacement accepted")
	}
	if _, err := os.Stat(base + "/old-sub/file"); err != nil {
		t.Fatal("changed parent was followed", err)
	}
}

func TestInterruptedRemovalKeepsOriginalInventoryAuthority(t *testing.T) {
	root, base := fixture(t)
	plan, err := Capture(context.Background(), root, []string{"generation/payload"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := RemoveRemaining(ctx, root, plan); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored", err)
	}
	// Reproduce one already-completed original unlink from this exact plan.
	// Its parent still has the original identity, and no successor is present.
	if err := os.Remove(base + "/generation/payload/sub/file"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateRemaining(context.Background(), root, plan); err != nil {
		t.Fatal("verified partial progress refused", err)
	}
	if err := RemoveRemaining(context.Background(), root, plan); err != nil {
		t.Fatal("partial removal could not complete", err)
	}
}
