//go:build linux

package storefs

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func nativeImage(t *testing.T) (Image, string, FormatTool) {
	t.Helper()
	if os.Getenv("FACTORYBOX_NATIVE_STOREFS") != "1" {
		t.Skip("requires the owned fixed-filesystem native fixture")
	}
	root, err := os.MkdirTemp("/store", "fixed-store-")
	if err != nil {
		t.Fatal(err)
	}
	var owner [16]byte
	if _, err := rand.Read(owner[:]); err != nil {
		t.Fatal(err)
	}
	formatter, err := filepath.EvalSymlinks("/usr/sbin/mkfs.ext4")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(formatter)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(data)
	pin := FormatTool{Path: formatter, SHA256: hex.EncodeToString(hash[:])}
	image, err := Create(context.Background(), root+"/store.img", 96<<20, hex.EncodeToString(owner[:]), pin)
	if err != nil {
		t.Fatal(err)
	}
	target := root + "/data"
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	return image, target, pin
}

func saveProof(t *testing.T, name string, proof map[string]any) {
	t.Helper()
	proof["schema"], proof["passed"], proof["production_qualified"] = 1, true, false
	data, err := json.MarshalIndent(proof, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("/output/"+name+".json", append(data, '\n'), 0644); err != nil {
		t.Fatal(err)
	}
}

func writeDurable(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	return errors.Join(writeErr, file.Sync(), file.Close())
}

func TestNativeStoreFSCapacityAndRemount(t *testing.T) {
	image, target, pin := nativeImage(t)
	attachment, err := Attach(image, target)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := attachment.Close(); err != nil {
			t.Error(err)
		}
	})
	if _, err := Create(context.Background(), image.Path, image.Bytes, image.Owner, pin); !errors.Is(err, os.ErrExist) {
		t.Fatal("existing image initialization was not refused", err)
	}
	retained := []byte("retained-" + image.Owner)
	if err := writeDurable(target+"/retained", retained); err != nil {
		t.Fatal(err)
	}
	reserve, err := os.OpenFile(target+"/reserve", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if reserve != nil {
			reserve.Close()
		}
	})
	if err := errors.Join(unix.Fallocate(int(reserve.Fd()), 0, 0, 8<<20), reserve.Sync()); err != nil {
		t.Fatal(err)
	}
	pressure, err := os.OpenFile(target+"/pressure", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	var written int64
	for _, size := range []int{1 << 20, 4096} {
		for written <= image.Bytes {
			n, writeErr := pressure.Write(make([]byte, size))
			written += int64(n)
			err := errors.Join(writeErr, pressure.Sync())
			if errors.Is(err, unix.ENOSPC) {
				break
			}
			if err != nil {
				pressure.Close()
				t.Fatal(err)
			}
		}
	}
	if err := pressure.Close(); err != nil || written > image.Bytes {
		t.Fatal("fixed filesystem accepted excessive data", written, err)
	}
	var fs unix.Statfs_t
	if err := unix.Statfs(target, &fs); err != nil || fs.Bavail != 0 {
		t.Fatal("did not reach actual zero-space boundary", fs.Bavail, err)
	}
	if err := writeDurable(target+"/canary", make([]byte, 4096)); !errors.Is(err, unix.ENOSPC) {
		t.Fatal("second boundary allocation not refused", err)
	}
	if err := errors.Join(reserve.Truncate(0), reserve.Sync()); err != nil {
		t.Fatal(err)
	}
	rollback := []byte("rollback-" + image.Owner)
	if err := writeDurable(target+"/rollback", rollback); err != nil {
		t.Fatal("reserve did not permit rollback", err)
	}
	if err := reserve.Close(); err != nil {
		t.Fatal(err)
	}
	reserve = nil
	first := attachment.Active
	if !first.DirectIO {
		t.Fatal("backing image direct I/O was not enabled")
	}
	if err := Verify(first); err != nil {
		t.Fatal("independent active attachment verification", err)
	}
	if err := attachment.Close(); err != nil {
		t.Fatal(err)
	}
	attachment, err = Attach(image, target)
	if err != nil {
		t.Fatal("reopen same exact image", err)
	}
	for name, expected := range map[string][]byte{"retained": retained, "rollback": rollback} {
		data, err := os.ReadFile(target + "/" + name)
		if err != nil || sha256.Sum256(data) != sha256.Sum256(expected) {
			t.Fatal("remount changed durable data", name, err)
		}
	}
	if err := attachment.Close(); err != nil {
		t.Fatal(err)
	}
	saveProof(t, "capacity", map[string]any{"image": image, "first_mount": first,
		"written_bytes": written, "real_enospc": true, "boundary_canary_enospc": true,
		"rollback_after_reserve_release": true, "retained_unchanged": true, "remount_persistence": true,
		"existing_initialization_refused": true, "exact_autoclear_cleanup": true,
		"loop_direct_io": true, "independent_verification": true})
}

func TestNativeStoreFSIdentityAndLeaseRefusals(t *testing.T) {
	image, target, _ := nativeImage(t)
	attachment, err := Attach(image, target)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := attachment.Close(); err != nil {
			t.Error(err)
		}
	})
	retained := []byte(image.Owner)
	if err := writeDurable(target+"/retained", retained); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(filepath.Dir(target), "other")
	if err := os.Mkdir(other, 0700); err != nil {
		t.Fatal(err)
	}
	if unexpected, err := Attach(image, other); err == nil {
		unexpected.Close()
		t.Fatal("second image owner admitted")
	}
	alias := image.Path + "-alias"
	if err := os.Link(image.Path, alias); err != nil {
		t.Fatal(err)
	}
	if err := attachment.Check(); err == nil {
		t.Fatal("shared image inode accepted")
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	original := image.Path + "-original"
	if err := os.Rename(image.Path, original); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(original, image.Path); err != nil {
		t.Fatal(err)
	}
	if err := attachment.Close(); err == nil {
		t.Fatal("symlink-substituted backing authorized unmount")
	}
	if err := os.Remove(image.Path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(original, image.Path); err != nil {
		t.Fatal(err)
	}
	// An association without the image lease must still be detected, not
	// adopted or detached by a new caller that acquires the released lease.
	if err := unix.Flock(int(attachment.image.Fd()), unix.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	if err := Verify(attachment.Active); err == nil {
		t.Fatal("independent verification accepted a released image lease")
	}
	if unexpected, err := Attach(image, other); err == nil || !strings.Contains(err.Error(), "already has a loop association") {
		if unexpected != nil {
			unexpected.Close()
		}
		t.Fatal("existing kernel association was not independently refused", err)
	}
	if err := unix.Flock(int(attachment.image.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mount("tmpfs", target, "tmpfs", unix.MS_NODEV|unix.MS_NOSUID, "size=1m"); err != nil {
		t.Fatal(err)
	}
	foreign, _, err := targetIdentity(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeDurable(target+"/new-mount-canary", retained); err != nil {
		t.Fatal(err)
	}
	if err := attachment.Close(); err == nil {
		t.Fatal("substituted mount authorized unmount")
	}
	if err := Verify(attachment.Active); err == nil {
		t.Fatal("independent verification accepted a substituted mount")
	}
	actual, _, err := targetIdentity(target)
	if err != nil || actual != foreign {
		t.Fatal("refusal changed the substituted mount", err)
	}
	if err := unix.Unmount(target, 0); err != nil {
		t.Fatal("remove only the test's own substituted mount", err)
	}
	if err := attachment.Check(); err != nil {
		t.Fatal("restored exact attachment no longer usable", err)
	}
	if data, err := os.ReadFile(target + "/retained"); err != nil || string(data) != string(retained) {
		t.Fatal("identity refusals changed the original data", err)
	}
	if err := attachment.Close(); err != nil {
		t.Fatal(err)
	}
	saveProof(t, "identity", map[string]any{"image": image, "second_owner_refused": true,
		"shared_inode_refused": true, "symlink_substitution_refused": true,
		"existing_kernel_association_refused": true, "changed_mount_preserved": true,
		"original_data_preserved": true, "exact_autoclear_cleanup": true,
		"unheld_image_lease_refused": true, "independent_changed_mount_refused": true})
}

func TestNativeStoreFSRetainsLeaseUntilAutoclear(t *testing.T) {
	image, target, _ := nativeImage(t)
	attachment, err := Attach(image, target)
	if err != nil {
		t.Fatal(err)
	}
	held, err := unix.Open(attachment.node.path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if held >= 0 {
			unix.Close(held)
		}
		if err := attachment.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := attachment.Close(); err == nil || !strings.Contains(err.Error(), "release is unconfirmed") {
		t.Fatal("association with an extra descriptor was considered released", err)
	}
	if unexpected, err := Attach(image, target); err == nil || !strings.Contains(err.Error(), "already has an owner") {
		if unexpected != nil {
			unexpected.Close()
		}
		t.Fatal("image lease released before the loop association", err)
	}
	if err := unix.Close(held); err != nil {
		t.Fatal(err)
	}
	held = -1
	if err := attachment.Close(); err != nil {
		t.Fatal("exact cleanup could not finish after last external reference closed", err)
	}
	attachment, err = Attach(image, target)
	if err != nil {
		t.Fatal("confirmed release did not permit exact reopen", err)
	}
	if err := attachment.Close(); err != nil {
		t.Fatal(err)
	}
	saveProof(t, "autoclear", map[string]any{"image": image, "unconfirmed_release_refused": true,
		"lease_retained_until_release": true, "cleanup_retry_completed": true, "exact_reopen": true})
}

func TestCreateRefusesUnprivilegedProcess(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("portable user-boundary test runs outside worker root")
	}
	path := filepath.Join(t.TempDir(), "never-created.img")
	if _, err := Create(context.Background(), path, 96<<20, "11111111111111111111111111111111", FormatTool{}); err == nil || !strings.Contains(err.Error(), "worker root") {
		t.Fatal("unprivileged process reached image initialization", err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unprivileged refusal created an image", err)
	}
}

func TestNativeStoreFSUncertainInitializationRefused(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_STOREFS") != "1" {
		t.Skip("requires the owned fixed-filesystem native fixture")
	}
	root, err := os.MkdirTemp("/store", "fixed-store-initialization-")
	if err != nil {
		t.Fatal(err)
	}
	owner := "11111111111111111111111111111111"
	path := root + "/store.img"
	formatter, err := filepath.EvalSymlinks("/usr/sbin/mkfs.ext4")
	if err != nil {
		t.Fatal(err)
	}
	wrong := FormatTool{Path: formatter, SHA256: strings.Repeat("0", 64)}
	if _, err := Create(context.Background(), path, 96<<20, owner, wrong); err == nil {
		t.Fatal("wrong formatter pin accepted")
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("wrong formatter pin created an image", err)
	}
	failing, err := filepath.EvalSymlinks("/usr/bin/false")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(failing)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(data)
	if _, err := Create(context.Background(), path, 96<<20, owner, FormatTool{Path: failing, SHA256: hex.EncodeToString(hash[:])}); err == nil {
		t.Fatal("failed formatter published an image identity")
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal("uncertain initialization was discarded", err)
	}
	identity, identityErr := imageIdentity(file, path, owner, 96<<20)
	var before [120]byte
	_, readErr := file.ReadAt(before[:], 1024)
	closeErr := file.Close()
	if err := errors.Join(identityErr, readErr, closeErr); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(formatter)
	if err != nil {
		t.Fatal(err)
	}
	hash = sha256.Sum256(data)
	if _, err := Create(context.Background(), path, 96<<20, owner, FormatTool{Path: formatter, SHA256: hex.EncodeToString(hash[:])}); !errors.Is(err, os.ErrExist) {
		t.Fatal("uncertain image was not refused before reformatting", err)
	}
	target := root + "/data"
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	if unexpected, err := Attach(identity, target); err == nil {
		unexpected.Close()
		t.Fatal("uninitialized image adopted")
	}
	file, err = os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var after [120]byte
	_, readErr = file.ReadAt(after[:], 1024)
	actual, identityErr := imageIdentity(file, path, owner, 96<<20)
	closeErr = file.Close()
	if err := errors.Join(readErr, identityErr, closeErr); err != nil || actual != identity || before != after {
		t.Fatal("refused uncertain initialization changed its retained image", err)
	}
	saveProof(t, "initialization", map[string]any{"image": identity, "wrong_formatter_refused_before_creation": true,
		"failed_initialization_retained": true, "uncertain_image_not_reformatted": true,
		"uninitialized_attachment_refused": true, "retained_image_unchanged": true})
}
