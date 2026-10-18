//go:build linux

package main

import (
	"bytes"
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
	"google.golang.org/protobuf/encoding/protowire"
)

func TestEagerPrefetchNativeReadsAndPreservesSavedFiles(t *testing.T) {
	dir := t.TempDir()
	data := bytes.Repeat([]byte("saved-nonzero-page"), 1<<16)
	path := filepath.Join(dir, "pages-20.img")
	if err := os.WriteFile(path, data, 0400); err != nil {
		t.Fatal(err)
	}
	image := prefetchTestImage(map[protowire.Number]uint64{1: 20},
		map[protowire.Number]uint64{1: 4096, 2: 1, 4: 6}, map[protowire.Number]uint64{1: 8192, 2: 2, 4: 4})
	if err := os.WriteFile(filepath.Join(dir, "pagemap-1.img"), image, 0400); err != nil {
		t.Fatal(err)
	}
	plan, err := criuBuildEagerPlan(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	count, err := criuReadPrefetch(plan)
	if err != nil || count != 8192 || len(plan) != 1 || plan[0].Offset != 4096 {
		t.Fatalf("incorrect native bounded read: %d %v %v", count, plan, err)
	}
	retained, err := os.ReadFile(path)
	if err != nil || sha256.Sum256(retained) != sha256.Sum256(data) {
		t.Fatal("saved payload changed")
	}
	truncated := filepath.Join(dir, "truncated")
	if err := os.WriteFile(truncated, []byte("incomplete"), 0400); err != nil {
		t.Fatal(err)
	}
	if _, err := criuReadPrefetch([]criuPrefetchRange{{truncated, 0, 4096}}); err == nil {
		t.Fatal("incomplete payload accepted")
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(dir, "fifo")
	if err := unix.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	for _, special := range []string{link, fifo, dir} {
		if file, err := criuOpenPrefetchFile(special); err == nil {
			file.Close()
			t.Fatalf("special payload accepted: %s", special)
		}
	}
}
