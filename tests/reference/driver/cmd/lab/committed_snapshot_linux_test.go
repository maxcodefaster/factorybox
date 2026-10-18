//go:build linux

package main

import "testing"

func TestCommittedMountRejectsLocalOnlyReadonlyAndAmbiguity(t *testing.T) {
	path := "/experiment/sysbox/images-again"
	local := "42 1 0:99 / " + path + " ro,nosuid,nodev - tmpfs owned rw,size=1024k\n"
	if _, err := committedMountID(local, path); err == nil {
		t.Fatal("accepted local-only readonly protection")
	}
	global := "42 1 0:99 / " + path + " ro,nosuid,nodev - tmpfs owned ro,size=1024k\n"
	id, err := committedMountID(global, path)
	if err != nil || id != 42 {
		t.Fatalf("rejected global readonly mount: %d %v", id, err)
	}
	for _, text := range []string{global + global, "", "broken", "0 1 0:99 / " + path + " ro - tmpfs owned ro"} {
		if _, err := committedMountID(text, path); err == nil {
			t.Fatal("accepted ambiguous/missing/invalid committed mount")
		}
	}
}
