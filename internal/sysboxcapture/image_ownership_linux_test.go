//go:build linux

package sysboxcapture

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestImageOwnershipNative(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_RETAINED_CAPTURE") != "1" {
		t.Skip("requires its owned native filesystem fixture")
	}
	root, err := os.MkdirTemp("/var/lib/sysbox", "factorybox-image-ownership-")
	if err != nil {
		t.Fatal(err)
	}
	makeImages := func(t *testing.T, name string) string {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
		return path
	}
	t.Run("one-complete-directory-barrier", func(t *testing.T) {
		path := makeImages(t, "many")
		content := bytes.Repeat([]byte("complete-image-payload"), 4096)
		for index := range 256 {
			if err := os.WriteFile(filepath.Join(path, imageName(index)), content, 0644); err != nil {
				t.Fatal(err)
			}
		}
		calls := 0
		count, err := ownImagesWithBarrier(path, 165536, 165536, func(fd int) error {
			calls++
			var info unix.Stat_t
			if err := unix.Fstat(fd, &info); err != nil {
				return err
			}
			if info.Mode&unix.S_IFMT != unix.S_IFDIR {
				return errors.New("image barrier did not cover the complete directory")
			}
			return unix.Syncfs(fd)
		})
		if err != nil || count != 256 || calls != 1 {
			t.Fatalf("complete image ownership/durability failed: count=%d barriers=%d error=%v", count, calls, err)
		}
		for index := range 256 {
			file := filepath.Join(path, imageName(index))
			data, err := os.ReadFile(file)
			if err != nil || !bytes.Equal(data, content) {
				t.Fatal("image ownership changed payload bytes", err)
			}
			var info unix.Stat_t
			if err := unix.Stat(file, &info); err != nil || info.Uid != 165536 ||
				info.Gid != 165536 || info.Mode&0777 != 0600 {
				t.Fatal("image ownership/mode was not applied", err)
			}
		}
	})
	t.Run("failed-barrier-is-not-completion", func(t *testing.T) {
		path := makeImages(t, "failed")
		if err := os.WriteFile(path+"/image", []byte("retained"), 0644); err != nil {
			t.Fatal(err)
		}
		count, err := ownImagesWithBarrier(path, 165536, 165536, func(int) error { return unix.EIO })
		if count != 0 || !errors.Is(err, unix.EIO) {
			t.Fatal("failed persistence was reported complete", count, err)
		}
	})
	for _, kind := range []string{"symlink", "hardlink", "foreign-bind"} {
		t.Run(kind, func(t *testing.T) {
			path := makeImages(t, kind)
			foreign := filepath.Join(root, kind+"-original")
			if err := os.WriteFile(foreign, []byte("foreign data"), 0640); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "symlink":
				err = os.Symlink(foreign, path+"/entry")
			case "hardlink":
				err = os.Link(foreign, path+"/entry")
			case "foreign-bind":
				err = os.WriteFile(path+"/entry", nil, 0600)
				if err == nil {
					err = unix.Mount(foreign, path+"/entry", "", unix.MS_BIND, "")
				}
				t.Cleanup(func() {
					if err := unix.Unmount(path+"/entry", 0); err != nil {
						t.Error(err)
					}
				})
			}
			if err != nil {
				t.Fatal(err)
			}
			count, err := ownImages(path, 165536, 165536)
			if err == nil || count != 0 {
				t.Fatal("foreign image entry was admitted", count, err)
			}
			var info unix.Stat_t
			if err := unix.Stat(foreign, &info); err != nil || info.Uid != 0 ||
				info.Gid != 0 || info.Mode&0777 != 0640 {
				t.Fatal("foreign inode ownership/mode changed", err)
			}
		})
	}
}

func imageName(index int) string {
	return "image-" + string(rune('A'+index/26)) + string(rune('a'+index%26))
}
