//go:build linux

package sysboxcapture

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"factorybox/internal/ownedtree"
	"golang.org/x/sys/unix"
)

func retainedFilesystemFixture(t *testing.T, parent string) (string, *os.Root, *FilesystemSnapshot) {
	t.Helper()
	home, err := os.MkdirTemp(parent, "retained-inspection-")
	if err != nil {
		t.Fatal(err)
	}
	identity, err := os.Lstat(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		actual, err := os.Lstat(home)
		if err != nil || !os.SameFile(identity, actual) {
			t.Error("refuse cleanup of a changed fixture root", err)
			return
		}
		if err := os.RemoveAll(home); err != nil {
			t.Error(err)
		}
	})
	for _, path := range []string{"input", "input/lower", "input/upper", "old", "generation", "generation/filesystem"} {
		if err := os.Mkdir(home+"/"+path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, kind := range []string{"lower", "upper"} {
		path := home + "/input/" + kind + "/payload"
		if err := os.WriteFile(path, []byte("retained private "+kind+" bytes\n"), 0640); err != nil {
			t.Fatal(err)
		}
		if err := unix.Setxattr(path, "user.factorybox", []byte("original metadata"), 0); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	base, err := PrepareFilesystemBase(ctx, home+"/input/lower", home+"/old/rootfs")
	if err != nil {
		t.Fatal(err)
	}
	baseMounted := true
	t.Cleanup(func() {
		if baseMounted {
			if err := ReleaseFilesystemBaseMount(base); err != nil {
				t.Error("release only the original fixture base", err)
			}
		}
	})
	lower, err := BindFilesystemBase(base, home+"/generation/filesystem/lower")
	if err != nil {
		t.Fatal(err)
	}
	lower.Saved = home + "/generation/filesystem/lower"
	aliasMounted := true
	t.Cleanup(func() {
		if aliasMounted {
			if err := RetireImmutableAlias(lower.Saved, lower); err != nil {
				t.Error("release only the original fixture alias", err)
			}
		}
	})
	upperRoot, err := openDirectory(home + "/input/upper")
	if err != nil {
		t.Fatal(err)
	}
	upper, cloneErr := cloneFilesystemTree(ctx, upperRoot, home+"/generation/filesystem/upper")
	if err := errors.Join(cloneErr, upperRoot.Close()); err != nil {
		t.Fatal(err)
	}
	upper.Name, upper.Saved = "upper", home+"/generation/filesystem/upper"
	if err := RetireImmutableAlias(lower.Saved, lower); err != nil {
		t.Fatal(err)
	}
	aliasMounted = false
	if err := ReleaseFilesystemBaseMount(base); err != nil {
		t.Fatal(err)
	}
	baseMounted = false
	root, err := os.OpenRoot(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	return home, root, &FilesystemSnapshot{Version: 1, Roots: []FilesystemTree{lower, upper}}
}

func TestRetainedFilesystemNative(t *testing.T) {
	parent := os.Getenv("FACTORYBOX_DEVICE_RECOVERY_TEST_ROOT")
	if os.Getenv("FACTORYBOX_NATIVE_RETAINED_CAPTURE") != "1" || parent == "" {
		t.Skip("owned bounded ext4 retained-inspection fixture only")
	}
	for _, fault := range []string{"unchanged", "translated", "unauthorized-translation", "denied-translation",
		"foreign-base", "foreign-alias", "foreign-upper", "base-content", "base-xattr", "upper-content",
		"alias-added", "alias-successor", "alias-mode", "base-inode", "alias-inode", "canceled"} {
		t.Run(fault, func(t *testing.T) {
			home, root, snapshot := retainedFilesystemFixture(t, parent)
			base := &snapshot.Roots[0]
			originalDevice := base.Immutable.Device
			ctx := context.Background()
			var authority BaseRecoveryVerifier
			called := 0
			allow := func(tree FilesystemTree, current uint64) error {
				called++
				if tree.Immutable == nil || tree.Immutable.Root != home+"/old/rootfs" ||
					tree.Immutable.Device != originalDevice+1 || current != originalDevice {
					return fmt.Errorf("unexpected fixture translation")
				}
				return nil
			}
			want := ""
			foreign := ""
			baseOnly := false
			switch fault {
			case "translated", "unauthorized-translation", "denied-translation", "foreign-base":
				base.Immutable.Device++
				base.Alias.Device++
				authority = allow
				if fault == "unauthorized-translation" {
					authority = nil
					want = "device changed without store authority"
				} else if fault == "denied-translation" {
					authority = func(FilesystemTree, uint64) error { return fmt.Errorf("fixture authority refused") }
					want = "fixture authority refused"
				} else if fault == "foreign-base" {
					foreign, baseOnly, want = base.Immutable.Root, true, "unexpected covering mount"
				}
			case "foreign-alias":
				foreign, want = base.Saved, "unexpected covering mount"
			case "foreign-upper":
				foreign, want = snapshot.Roots[1].Saved, "unexpected covering mount"
			case "base-content", "upper-content":
				path := base.Immutable.Root + "/payload"
				if fault == "upper-content" {
					path = snapshot.Roots[1].Saved + "/payload"
				}
				info, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, bytes.Repeat([]byte{'x'}, int(info.Size())), 0640); err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
					t.Fatal(err)
				}
				want = "retained filesystem tree changed"
			case "base-xattr":
				if err := unix.Setxattr(base.Immutable.Root+"/payload", "user.factorybox", []byte("changed metadata"), 0); err != nil {
					t.Fatal(err)
				}
				want = "retained filesystem tree changed"
			case "alias-added":
				if err := os.WriteFile(base.Saved+"/foreign-added", []byte("preserve"), 0600); err != nil {
					t.Fatal(err)
				}
				want = "mountpoint is not empty"
			case "alias-successor":
				if err := os.Rename(base.Saved, base.Saved+"-original"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(base.Saved, 0700); err != nil {
					t.Fatal(err)
				}
				want = "original inode or ownership changed"
			case "alias-mode":
				if err := os.Chmod(base.Saved, 0755); err != nil {
					t.Fatal(err)
				}
				want = "original inode or ownership changed"
			case "base-inode":
				base.Immutable.Inode++
				want = "persisted base inode or metadata changed"
			case "alias-inode":
				base.Alias.Inode++
				want = "original inode or ownership changed"
			case "canceled":
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				ctx, want = canceled, "context canceled"
			}
			manifest, err := json.Marshal(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			before, err := ownedtree.Capture(context.Background(), root, []string{"input", "old", "generation"})
			if err != nil {
				t.Fatal(err)
			}
			var unmount func() error
			if foreign != "" {
				if err := unix.Mount(foreign, foreign, "", unix.MS_BIND, ""); err != nil {
					t.Fatal(err)
				}
				var mounted unix.Statx_t
				if err := unix.Statx(unix.AT_FDCWD, foreign, 0, unix.STATX_MNT_ID, &mounted); err != nil {
					t.Fatal(err)
				}
				active := true
				unmount = func() error {
					if !active {
						return nil
					}
					var actual unix.Statx_t
					if err := unix.Statx(unix.AT_FDCWD, foreign, 0, unix.STATX_MNT_ID, &actual); err != nil || actual.Mnt_id != mounted.Mnt_id {
						return fmt.Errorf("refuse cleanup of changed negative fixture mount: %v", err)
					}
					if err := unix.Unmount(foreign, 0); err != nil {
						return err
					}
					active = false
					return nil
				}
				defer func() {
					if err := unmount(); err != nil {
						t.Error(err)
					}
				}()
			}
			if baseOnly {
				err = VerifyRetainedFilesystemBase(ctx, *base, authority)
			} else {
				err = VerifyRetainedFilesystemSnapshot(ctx, home+"/generation", snapshot, authority)
			}
			if want == "" && err != nil || want != "" && (err == nil || !strings.Contains(err.Error(), want)) {
				t.Fatal("retained inspection disagrees with the exact control", err, want)
			}
			if fault == "translated" && called != 2 || fault == "foreign-base" && called != 0 {
				t.Fatal("device authority ran at the wrong verification boundary", called)
			}
			if unmount != nil {
				if err := unmount(); err != nil {
					t.Fatal(err)
				}
			}
			after, err := ownedtree.Capture(context.Background(), root, []string{"input", "old", "generation"})
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("read-only verification changed retained objects or content", err)
			}
			afterManifest, err := json.Marshal(snapshot)
			if err != nil || !bytes.Equal(manifest, afterManifest) {
				t.Fatal("inspection rewrote its persisted source identity", err)
			}
			if want == "" && VerifyImmutableTree(base.Saved, *base) == nil {
				t.Fatal("read-only inspection recreated or granted immutable runtime mount authority")
			}
		})
	}
}
