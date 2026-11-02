//go:build linux

package main

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestWritableMappingRetainsItsInodeAfterClosingTheOnlyDescription(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mapped-shm")
	mapping, err := closedWritableMapping(path)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Munmap(mapping)
	inode, err := writableMappingInode(mapping)
	if err != nil || inode == 0 {
		t.Fatal("original writable mapping is absent", err)
	}
	descriptors, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range descriptors {
		target, _ := os.Readlink("/proc/self/fd/" + entry.Name())
		if target == path {
			t.Fatal("mapping fixture incorrectly kept its original descriptor open")
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("independent replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	after, err := writableMappingInode(mapping)
	if err != nil || after != inode {
		t.Fatal("replacement pathname changed the held mapping identity", err)
	}
	mapping[0]++
	if err := unix.Msync(mapping, unix.MS_SYNC); err != nil {
		t.Fatal(err)
	}
	if replacement, err := os.ReadFile(path); err != nil || string(replacement) != "independent replacement" {
		t.Fatal("shared mapping overwrote the replacement pathname", err)
	}
}
