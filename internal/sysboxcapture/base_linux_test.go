//go:build linux

package sysboxcapture

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestImmutableBaseNative(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_RETAINED_CAPTURE") != "1" {
		t.Skip("requires the owned native filesystem and private mount namespace")
	}
	home, err := os.MkdirTemp("/var/lib/sysbox", "factorybox-immutable-")
	if err != nil {
		t.Fatal(err)
	}
	source, store := home+"/input", home+"/store"
	for _, path := range []string{source, source + "/directory", store} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	payload := []byte(strings.Repeat("original-complete-base\n", 1024))
	if err := os.WriteFile(source+"/state", payload, 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(source+"/state", 12345, 23456); err != nil {
		t.Fatal(err)
	}
	if err := unix.Setxattr(source+"/state", "user.factorybox-base", []byte("original-xattr"), 0); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(source+"/state", source+"/hardlink"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("state", source+"/symlink"); err != nil {
		t.Fatal(err)
	}
	base, err := PrepareFilesystemBase(context.Background(), source, store+"/rootfs")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := unix.Unmount(base.Immutable.Root, 0); err != nil {
			t.Error("unmount only the prepared test base", err)
		}
	})
	bind := func(t *testing.T, source string, tree FilesystemTree, destination string) FilesystemTree {
		t.Helper()
		bound, err := bindImmutableTree(source, destination, tree)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := RetireImmutableAlias(destination, bound); err != nil {
				t.Error("retire only the owned test alias", err)
			}
		})
		return bound
	}
	t.Run("complete-import-with-independent-inodes", func(t *testing.T) {
		root, err := openDirectory(base.Immutable.Root)
		if err != nil {
			t.Fatal(err)
		}
		actual, scanErr := fingerprintTree(context.Background(), root, nil, false)
		root.Close()
		if scanErr != nil || !sameFilesystemTree(actual, base) {
			t.Fatal("prepared base differs from its complete content/metadata digest", scanErr)
		}
		before, err := os.Stat(source + "/state")
		if err != nil {
			t.Fatal(err)
		}
		after, err := os.Stat(base.Immutable.Root + "/state")
		if err != nil || os.SameFile(before, after) {
			t.Fatal("import shares its caller's mutable inode", err)
		}
		link, err := os.Stat(base.Immutable.Root + "/hardlink")
		if err != nil || !os.SameFile(after, link) || base.HashedBytes != int64(len(payload)) {
			t.Fatal("import lost internal hardlinks or hashed their payload repeatedly", err)
		}
	})
	t.Run("readonly-canonical-and-alias", func(t *testing.T) {
		alias := home + "/alias"
		bind(t, base.Immutable.Root, base, alias)
		for _, path := range []string{base.Immutable.Root, alias} {
			file, err := os.OpenFile(path+"/state", os.O_WRONLY, 0)
			if file != nil {
				file.Close()
			}
			if !errors.Is(err, unix.EROFS) {
				t.Fatal("immutable base accepted a writable canonical path or alias", path, err)
			}
		}
	})
	t.Run("writable-base-rejected", func(t *testing.T) {
		if err := unix.Mount("", base.Immutable.Root, "", unix.MS_REMOUNT|unix.MS_BIND, ""); err != nil {
			t.Fatal(err)
		}
		rejected := VerifyImmutableTree(base.Immutable.Root, base)
		if err := unix.Mount("", base.Immutable.Root, "", unix.MS_REMOUNT|unix.MS_BIND|unix.MS_RDONLY, ""); err != nil {
			t.Fatal(err)
		}
		if rejected == nil {
			t.Fatal("writable canonical base accepted")
		}
	})
	t.Run("foreign-source-rejected-before-copy", func(t *testing.T) {
		destination := home + "/must-not-exist"
		if _, err := bindImmutableTree(source, destination, base); err == nil {
			t.Fatal("caller-owned mutable source accepted as the prepared base")
		}
		if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("rejected source created a target before admission", err)
		}
	})
	t.Run("foreign-successor-rejected", func(t *testing.T) {
		if err := readonlyBind(source, base.Immutable.Root); err != nil {
			t.Fatal(err)
		}
		rejected := VerifyImmutableTree(base.Immutable.Root, base)
		if err := unix.Unmount(base.Immutable.Root, 0); err != nil {
			t.Fatal(err)
		}
		if rejected == nil {
			t.Fatal("foreign read-only successor accepted as the original base")
		}
	})
	t.Run("descendant-mount-rejected", func(t *testing.T) {
		path := base.Immutable.Root + "/directory"
		if err := unix.Mount("tmpfs", path, "tmpfs", 0, "size=1m"); err != nil {
			t.Fatal(err)
		}
		rejected := VerifyImmutableTree(base.Immutable.Root, base)
		if err := unix.Unmount(path, 0); err != nil {
			t.Fatal(err)
		}
		if rejected == nil || !strings.Contains(rejected.Error(), "descendant mount") {
			t.Fatal("foreign mount hid part of the prepared base", rejected)
		}
	})
	t.Run("changed-manifest-rejected", func(t *testing.T) {
		corrupt := base
		corrupt.SHA256 = strings.Repeat("0", 64)
		if err := VerifyImmutableTree(base.Immutable.Root, corrupt); err == nil {
			t.Fatal("base shortcut accepted a changed complete digest")
		}
	})
	t.Run("source-deletion-and-independent-writable-siblings", func(t *testing.T) {
		upper := home + "/upper"
		if err := os.Mkdir(upper, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(upper+"/state", []byte("original-upper"), 0600); err != nil {
			t.Fatal(err)
		}
		generation := home + "/generation"
		if err := os.Mkdir(generation, 0700); err != nil {
			t.Fatal(err)
		}
		snapshot, err := copyFilesystem(context.Background(), []filesystemRoot{
			{Name: "lower", Source: base.Immutable.Root, Expected: &base},
			{Name: "upper", Source: upper},
		}, generation+"/filesystem")
		if err != nil {
			t.Fatal(err)
		}
		retire := func(path string, snapshot *FilesystemSnapshot) {
			t.Helper()
			for _, tree := range snapshot.Roots {
				if tree.Immutable != nil {
					tree := tree
					t.Cleanup(func() {
						if err := RetireImmutableAlias(path+"/filesystem/"+tree.Name, tree); err != nil {
							t.Error(err)
						}
					})
				}
			}
		}
		retire(generation, snapshot)
		if err := os.RemoveAll(source); err != nil {
			t.Fatal(err)
		}
		left, err := MaterializeFilesystemSnapshot(context.Background(), generation, home+"/left", snapshot)
		if err != nil {
			t.Fatal(err)
		}
		retire(home+"/left", left)
		right, err := MaterializeFilesystemSnapshot(context.Background(), generation, home+"/right", snapshot)
		if err != nil {
			t.Fatal(err)
		}
		retire(home+"/right", right)
		if err := os.WriteFile(home+"/left/filesystem/upper/state", []byte("left-only"), 0600); err != nil {
			t.Fatal(err)
		}
		for _, path := range []string{generation, home + "/right"} {
			saved := snapshot
			if path == home+"/right" {
				saved = right
			}
			if err := VerifyFilesystemSnapshot(context.Background(), path, saved); err != nil {
				t.Fatal("sibling mutation or source deletion changed retained backing", err)
			}
			actual, err := os.ReadFile(path + "/filesystem/lower/state")
			if err != nil || string(actual) != string(payload) {
				t.Fatal("saved base depended on the deleted caller input", err)
			}
		}
	})
	t.Run("foreign-alias-retirement-rejected", func(t *testing.T) {
		alias := home + "/retiring-alias"
		bound := bind(t, base.Immutable.Root, base, alias)
		if err := RetireImmutableAlias(alias, bound); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(alias, alias+"-original"); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(alias, 0700); err != nil {
			t.Fatal(err)
		}
		rejected := RetireImmutableAlias(alias, bound)
		if err := os.Remove(alias); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(alias+"-original", alias); err != nil {
			t.Fatal(err)
		}
		if rejected == nil {
			t.Fatal("retirement adopted a foreign successor")
		}
	})
	if err := VerifyImmutableTree(filepath.Join(store, "rootfs"), base); err != nil {
		t.Fatal("negative cases changed the original base", err)
	}
}
