package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func imageGraphRAMParentFixture(t *testing.T, readonly bool) (string, string, string, string, []byte) {
	return imageGraphRAMParentFixtureWithLimit(t, readonly, "16m")
}

func imageGraphRAMParentFixtureWithLimit(t *testing.T, readonly bool, limit string) (string, string, string, string, []byte) {
	t.Helper()
	source, head, target := imageGraphFixture(t)
	parent := filepath.Join(source, "parent")
	if err := os.Mkdir(parent, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"inventory.img", "pstree.img", "pages-1.img"} {
		if err := os.WriteFile(filepath.Join(parent, name), []byte("private-parent-"+name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	ram := filepath.Join(filepath.Dir(source), "ram")
	if err := os.Mkdir(ram, 0700); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mount("owned-parent-graph", ram, "tmpfs", unix.MS_NOSUID|unix.MS_NODEV|unix.MS_NOEXEC,
		"size="+limit+",mode=0700"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := unix.Unmount(ram, 0); err != nil {
			t.Error(err)
		}
	})
	file, err := os.CreateTemp(ram, "owned-parent-graph-*")
	if err != nil {
		t.Fatal(err)
	}
	backing := file.Name()
	t.Cleanup(func() { os.Remove(backing) })
	payload := bytes.Repeat([]byte{0x72, 0xA3, 0x1F, 0x89}, 2048)
	if _, err := file.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	mounted := filepath.Join(parent, "pages-1.img")
	if err := unix.Mount(backing, mounted, "", unix.MS_BIND, ""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := unix.Unmount(mounted, 0); err != nil {
			t.Error(err)
		}
	})
	if readonly {
		if err := unix.Mount("", mounted, "", unix.MS_BIND|unix.MS_REMOUNT|unix.MS_RDONLY, ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("../parent", filepath.Join(head, "parent")); err != nil {
		t.Fatal(err)
	}
	return head, parent, target, backing, payload
}

func TestImageGraphCopiesOnlyAdmittedReadonlyRAMAncestor(t *testing.T) {
	head, _, target, backing, payload := imageGraphRAMParentFixture(t, true)
	proof, err := descendantCloneImageGraphPolicy(head, target, true)
	if err != nil {
		t.Fatal(err)
	}
	if proof["verified_ram_parent_copy_bytes"] != int64(len(payload)) ||
		proof["verified_ram_parent_copy_files"] != 1 || proof["readonly_ram_parent_policy"] != true {
		t.Fatal("RAM copy was hidden as a kernel clone", proof)
	}
	if proof["parent_links"].(map[string]string)[filepath.Join(target, "head/parent")] != "../parent-1" {
		t.Fatal("copied graph did not retain a private relative parent", proof)
	}
	cloned := filepath.Join(target, "parent-1/pages-1.img")
	info, err := os.Stat(cloned)
	original, originalErr := os.Stat(backing)
	if err != nil || originalErr != nil || os.SameFile(info, original) {
		t.Fatal("RAM ancestor was not made independent", err, originalErr)
	}
	if err := os.WriteFile(backing, bytes.Repeat([]byte{0x22}, len(payload)), 0600); err != nil {
		t.Fatal("source read lease was not released", err)
	}
	actual, err := os.ReadFile(cloned)
	if err != nil || !bytes.Equal(actual, payload) {
		t.Fatal("original RAM mutation changed the private graph", err)
	}
}

func TestImageGraphRejectsWritableRAMAncestorAndCleansTarget(t *testing.T) {
	head, _, target, backing, payload := imageGraphRAMParentFixture(t, false)
	_, err := descendantCloneImageGraphPolicy(head, target, true)
	if err == nil || !strings.Contains(err.Error(), "bounded readonly tmpfs source") {
		t.Fatalf("writable RAM ancestor was not rejected: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("failed RAM graph left an owned partial target", err)
	}
	actual, err := os.ReadFile(backing)
	if err != nil || !bytes.Equal(actual, payload) {
		t.Fatal("rejected RAM graph changed its source", err)
	}
}

func TestImageGraphRejectsOversizedReadonlyRAMAncestor(t *testing.T) {
	head, _, target, backing, payload := imageGraphRAMParentFixtureWithLimit(t, true, "2g")
	_, err := descendantCloneImageGraphPolicy(head, target, true)
	if err == nil || !strings.Contains(err.Error(), "bounded readonly tmpfs source") {
		t.Fatalf("unbounded RAM snapshot mount was accepted: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("unsupported RAM graph left a partial target", err)
	}
	actual, err := os.ReadFile(backing)
	if err != nil || !bytes.Equal(actual, payload) {
		t.Fatal("unsupported RAM graph changed its source", err)
	}
}
