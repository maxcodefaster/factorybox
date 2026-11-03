//go:build linux

package ownedtree

import (
	"context"
	"os"
	"reflect"
	"testing"

	"golang.org/x/sys/unix"
)

func TestDelegatedWitnessNeverTraversesOrRemovesSeparateRoots(t *testing.T) {
	root, base := fixture(t)
	ctx := context.Background()
	watch, err := unix.InotifyInit1(unix.IN_CLOEXEC | unix.IN_NONBLOCK)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(watch)
	if _, err := unix.InotifyAddWatch(watch, base+"/generation/payload/sub/file", unix.IN_OPEN); err != nil {
		t.Fatal(err)
	}
	plan, err := CaptureDelegating(ctx, root, []string{"generation"}, []string{"generation/payload"})
	if err != nil || len(plan.Entries) != 1 || plan.Entries[0].Path != "generation" {
		t.Fatal("delegation did not keep a bounded metadata witness", plan, err)
	}
	if err := os.WriteFile(base+"/generation/payload/guest-change", []byte("separately owned"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateComplete(ctx, root, plan); err != nil {
		t.Fatal("witness adopted responsibility for delegated content", err)
	}
	if err := RemoveRemaining(ctx, root, plan); err == nil {
		t.Fatal("witness authorized removal of a delegated tree")
	}
	if count, err := unix.Read(watch, make([]byte, 4096)); count > 0 || err != unix.EAGAIN {
		t.Fatal("witness or rejected removal opened delegated file data", count, err)
	}
	if err := os.WriteFile(base+"/generation/foreign", []byte("unknown metadata"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateRemaining(ctx, root, plan); err == nil {
		t.Fatal("delegation hid unrelated unknown metadata")
	}
	if err := os.Remove(base + "/generation/foreign"); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(base+"/generation/payload", base+"/retained-payload"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateComplete(ctx, root, plan); err != nil {
		t.Fatal("separately authorized withdrawal invalidated retained metadata", err)
	}
	if err := RemoveRemaining(ctx, root, plan); err == nil {
		t.Fatal("absence turned a delegated witness into deletion authority")
	}
}

func TestDelegatedWitnessRejectsEscapesAndOverlappingAuthority(t *testing.T) {
	root, _ := fixture(t)
	for _, paths := range [][]string{{"generation"}, {"foreign"}, {"../foreign"},
		{"generation/payload", "generation/payload"}, {"generation/payload", "generation/payload/sub"}} {
		if _, err := CaptureDelegating(context.Background(), root, []string{"generation"}, paths); err == nil {
			t.Fatal("invalid delegation accepted", paths)
		}
	}
	plan, err := CaptureDelegating(context.Background(), root, []string{"generation"}, []string{"generation/payload/sub"})
	if err != nil {
		t.Fatal(err)
	}
	changed := plan
	changed.DelegatedRoots = []string{"generation/payload"}
	if reflect.DeepEqual(changed, plan) || ValidateComplete(context.Background(), root, changed) == nil {
		t.Fatal("widened delegation retained overlapping inventory authority")
	}
}
