package main

import (
	"bytes"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func TestStreamSnapshotOwnerUsesOnlyItsExplicitCapture(t *testing.T) {
	if os.Getenv("RUNTIME_FORK_SNAPSHOT_OWNER_NATIVE") != "1" {
		t.Skip("requires the exactly owned native /experiment volume and bind mounts")
	}
	for _, mode := range []string{"normal", "disabled", "nested", "symlink", "foreign"} {
		t.Run(mode, func(t *testing.T) {
			root := "/experiment/sysbox-copy"
			if _, err := os.Lstat(root); !os.IsNotExist(err) {
				t.Fatal("snapshot owner fixture must own an absent runtime root", err)
			}
			t.Cleanup(func() { os.RemoveAll(root) })
			parent := root + "/images"
			head := root + "/images-11111111111111111111111111111111"
			shadow := head + "/original-writer"
			for _, path := range []string{parent, shadow} {
				if err := os.MkdirAll(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			payload := bytes.Repeat([]byte{0x37, 0xA4}, 4096)
			for _, path := range []string{parent + "/pages-1.img", head + "/inventory.img", shadow + "/pages-2.img"} {
				if err := os.WriteFile(path, payload, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink("../images", head+"/parent"); err != nil {
				t.Fatal(err)
			}
			if err := unix.Mount(parent, parent, "", unix.MS_BIND, ""); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { unix.Unmount(parent, 0) })
			if err := unix.Mount("", parent, "", unix.MS_BIND|unix.MS_REMOUNT|unix.MS_RDONLY, ""); err != nil {
				t.Fatal(err)
			}
			for name, value := range map[string]string{
				"RUNTIME_FORK_LAZY_INHERIT_ORACLE": "2", "RUNTIME_FORK_DESCENDANT_STREAM_ORACLE": "1",
				"RUNTIME_FORK_DESCENDANT_IMAGES": head, "RUNTIME_FORK_DESCENDANT_PARENT": parent,
				"RUNTIME_FORK_DESCENDANT_INHERIT_CAPTURE": "1", "RUNTIME_FORK_DESCENDANT_INHERIT_PROOF": "1",
				"RUNTIME_FORK_DESCENDANT": "1", "CRIU_SYSBOX_LAZY_INHERIT": "1",
				"RUNTIME_FORK_COW_CAPTURE": "0", "RUNTIME_FORK_PIPE_CAPTURE": "0",
			} {
				t.Setenv(name, value)
			}
			switch mode {
			case "disabled":
				t.Setenv("RUNTIME_FORK_DESCENDANT_STREAM_ORACLE", "0")
			case "nested":
				if err := os.Mkdir(shadow+"/unexpected", 0700); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Remove(shadow + "/pages-2.img"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(parent+"/pages-1.img", shadow+"/pages-2.img"); err != nil {
					t.Fatal(err)
				}
			case "foreign":
				if err := os.Chown(shadow+"/pages-2.img", 424242, 424242); err != nil {
					t.Fatal(err)
				}
			}
			err := ownSnapshotFiles(head, 165536, 165536)
			if (err == nil) != (mode == "normal") {
				t.Fatal("snapshot writer directory admission differs", mode, err)
			}
			if mode == "normal" {
				for _, path := range []string{head + "/inventory.img", shadow, shadow + "/pages-2.img"} {
					info, err := os.Lstat(path)
					if err != nil {
						t.Fatal(err)
					}
					stat := info.Sys().(*syscall.Stat_t)
					if stat.Uid != 165536 || stat.Gid != 165536 ||
						(info.IsDir() && info.Mode().Perm() != 0700) ||
						(!info.IsDir() && info.Mode().Perm() != 0600) {
						t.Fatal("capture ownership is not private mapped root", path, stat)
					}
				}
			}
			if mode == "foreign" {
				info, err := os.Lstat(shadow + "/pages-2.img")
				if err != nil || info.Sys().(*syscall.Stat_t).Uid != 424242 {
					t.Fatal("stream checkpoint claimed a foreign payload", err)
				}
			}
			actual, err := os.ReadFile(filepath.Join(parent, "pages-1.img"))
			if err != nil || !bytes.Equal(actual, payload) {
				t.Fatal("snapshot ownership changed original parent bytes", err)
			}
			info, err := os.Lstat(parent + "/pages-1.img")
			if err != nil || info.Sys().(*syscall.Stat_t).Uid != uint32(os.Geteuid()) {
				t.Fatal("snapshot ownership followed the readonly parent", err)
			}
		})
	}
}
