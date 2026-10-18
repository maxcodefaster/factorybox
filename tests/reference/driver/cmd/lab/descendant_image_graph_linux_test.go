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

func imageGraphFixture(t *testing.T) (string, string, string) {
	t.Helper()
	if os.Getenv("RUNTIME_FORK_GRAPH_NATIVE") != "1" {
		t.Skip("requires the exactly owned native /experiment fixture")
	}
	root, err := os.MkdirTemp("/experiment", "image-graph-unit-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	source, target := filepath.Join(root, "source"), filepath.Join(root, "target")
	head := filepath.Join(source, "head")
	if err := os.MkdirAll(head, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"inventory.img", "pstree.img", "pages-1.img"} {
		if err := os.WriteFile(filepath.Join(head, name), []byte("owned-"+name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return source, head, target
}

func TestImageGraphRejectsForeignParent(t *testing.T) {
	source, head, target := imageGraphFixture(t)
	foreign := filepath.Join(filepath.Dir(source), "foreign")
	if err := os.Mkdir(foreign, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../foreign", filepath.Join(head, "parent")); err != nil {
		t.Fatal(err)
	}
	if _, err := descendantCloneImageGraph(head, target); err == nil {
		t.Fatal("accepted parent outside the source graph")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("rejected graph created a target: %v", err)
	}
}

func TestImageGraphRejectsCycle(t *testing.T) {
	_, head, target := imageGraphFixture(t)
	if err := os.Symlink(".", filepath.Join(head, "parent")); err != nil {
		t.Fatal(err)
	}
	if _, err := descendantCloneImageGraph(head, target); err == nil {
		t.Fatal("accepted a cyclic snapshot parent")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("cyclic graph created a target: %v", err)
	}
}

func TestImageGraphPreservesExistingTarget(t *testing.T) {
	_, head, target := imageGraphFixture(t)
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(target, "retained")
	if err := os.WriteFile(sentinel, []byte("must remain"), 0600); err != nil {
		t.Fatal(err)
	}
	original, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := descendantCloneImageGraph(head, target); err == nil {
		t.Fatal("reused an existing target")
	}
	current, err := os.Stat(target)
	data, readErr := os.ReadFile(sentinel)
	if err != nil || readErr != nil || !os.SameFile(original, current) || string(data) != "must remain" {
		t.Fatalf("existing target changed: %v %v %q", err, readErr, data)
	}
}

func TestImageGraphCleansFailedCrossFilesystemClone(t *testing.T) {
	_, head, target := imageGraphFixture(t)
	file, err := os.CreateTemp("/dev/shm", "owned-image-graph-*")
	if err != nil {
		t.Fatal(err)
	}
	name := file.Name()
	t.Cleanup(func() { os.Remove(name) })
	expected := bytes.Repeat([]byte{0x83, 0x27, 0xE1, 0x59}, 2048)
	if _, err := file.Write(expected); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	page := filepath.Join(head, "pages-1.img")
	if err := unix.Mount(name, page, "", unix.MS_BIND, ""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := unix.Unmount(page, 0); err != nil {
			t.Error(err)
		}
	})
	if _, err := descendantCloneImageGraph(head, target); !errors.Is(err, syscall.EXDEV) {
		t.Fatalf("did not fail the mandatory cross-filesystem FICLONE: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("owned partial target was not removed: %v", err)
	}
	actual, err := os.ReadFile(name)
	if err != nil || !bytes.Equal(actual, expected) {
		t.Fatalf("source changed during failed clone: %v", err)
	}
}
