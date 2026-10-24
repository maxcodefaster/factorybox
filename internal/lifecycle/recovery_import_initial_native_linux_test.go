//go:build linux

package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"factorybox/internal/ownedtree"
	"golang.org/x/sys/unix"
)

// Every pressure inode is created exclusively by this test and recorded before
// cleanup. No pathname, mount or replacement can acquire release authority.
type initialImportPressure struct {
	root   *os.Root
	name   string
	dir    *os.File
	object ownedtree.Object
	files  map[string]ownedtree.Object
	closed bool
}

func initialImportObject(file *os.File) (ownedtree.Object, unix.Stat_t, error) {
	var stat unix.Stat_t
	var extended unix.Statx_t
	err := errors.Join(unix.Fstat(int(file.Fd()), &stat), unix.Statx(int(file.Fd()), "", unix.AT_EMPTY_PATH, unix.STATX_MNT_ID, &extended))
	if err != nil || extended.Mask&unix.STATX_MNT_ID == 0 || extended.Mnt_id == 0 {
		return ownedtree.Object{}, stat, fmt.Errorf("private pressure identity unavailable: %v", err)
	}
	return ownedtree.Object{Device: uint64(stat.Dev), Inode: stat.Ino, MountID: extended.Mnt_id,
		Mode: stat.Mode, UID: stat.Uid, GID: stat.Gid}, stat, nil
}

func (p *initialImportPressure) releaseOne(name string) error {
	expected, found := p.files[name]
	if !found {
		return fmt.Errorf("pressure inode was never created by this fixture")
	}
	file, err := p.root.Open(p.name + "/" + name)
	if err != nil {
		return err
	}
	actual, stat, identityErr := initialImportObject(file)
	err = errors.Join(identityErr, file.Close())
	if err != nil || actual != expected || stat.Size != 0 || stat.Nlink != 1 {
		return fmt.Errorf("pressure inode changed before its exact release: %v", err)
	}
	if err := p.root.Remove(p.name + "/" + name); err != nil {
		return err
	}
	delete(p.files, name)
	return nil
}

func (p *initialImportPressure) close() error {
	if p.closed {
		return nil
	}
	plan, err := ownedtree.Capture(context.Background(), p.root, []string{p.name})
	if err != nil || len(plan.Entries) != len(p.files)+1 {
		return fmt.Errorf("private inode pressure inventory changed: %v", err)
	}
	for _, entry := range plan.Entries {
		if entry.Path == p.name {
			if entry.Object != p.object {
				return fmt.Errorf("private pressure directory was replaced")
			}
			continue
		}
		name := strings.TrimPrefix(entry.Path, p.name+"/")
		expected, found := p.files[name]
		if !found || entry.Object != expected || entry.Bytes != 0 || entry.Links != 1 || entry.Rdev != 0 || entry.Link != "" || entry.Content != sum(nil) {
			return fmt.Errorf("foreign or changed pressure input: %s", entry.Path)
		}
	}
	if err := ownedtree.RemoveRemaining(context.Background(), p.root, plan); err != nil {
		return err
	}
	if err := errors.Join(unix.Syncfs(int(p.dir.Fd())), p.dir.Close(), p.root.Close()); err != nil {
		return err
	}
	p.closed = true
	return nil
}

func checkRecoveryImportInitialPressure(t *testing.T, status boundedLossStatus, saved boundedLossPause, recoveryArgs func(string, string) []string) {
	t.Helper()
	ctx := context.Background()
	oldRoot, err := os.OpenRoot(saved.Request.SourceRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer oldRoot.Close()
	original, err := ownedtree.Capture(ctx, oldRoot, []string{"objects", "forks", "bases", "runtime"})
	if err != nil {
		t.Fatal("complete old checkpoint inventory before initial-publication fault", err)
	}
	proof := map[string]any{"schema": 1, "passed": false, "production_qualified": false,
		"request": saved.Request, "source_root": status.Lifecycle.Root}
	t.Cleanup(func() {
		proof["full_original_recovery_assertions_passed"] = !t.Failed()
		proof["passed"] = !t.Failed()
		boundedLossSave(t, "recovery-import-initial.json", proof)
	})
	root, err := os.OpenRoot(status.Store)
	if err != nil {
		t.Fatal(err)
	}
	p := &initialImportPressure{root: root, name: "initial-import-inode-pressure-" + saved.Request.SourceOwner,
		files: map[string]ownedtree.Object{}}
	if err := root.Mkdir(p.name, 0700); err != nil {
		root.Close()
		t.Fatal(err)
	}
	p.dir, err = root.Open(p.name)
	if err != nil {
		t.Fatal(err)
	}
	var stat unix.Stat_t
	p.object, stat, err = initialImportObject(p.dir)
	if err != nil || stat.Mode != unix.S_IFDIR|0700 || stat.Uid != 0 || stat.Gid != 0 ||
		uint64(stat.Dev) != status.Lifecycle.BoundedStore.BlockDevice || p.object.MountID != status.Lifecycle.BoundedStore.Mount.MountID {
		t.Fatal("inode-pressure directory is outside this exact private bounded attachment", err)
	}
	t.Cleanup(func() {
		if err := p.close(); err != nil {
			t.Error("release only exact fixture-created inode pressure", err)
		}
	})
	var capacity unix.Statfs_t
	if err := unix.Fstatfs(int(p.dir.Fd()), &capacity); err != nil || capacity.Ffree < 3 || capacity.Ffree > 100000 {
		t.Fatal("unexpected bounded inode-pressure profile", capacity, err)
	}
	initialFree := capacity.Ffree
	for capacity.Ffree > 1 {
		name := fmt.Sprintf("%08x", len(p.files))
		file, err := root.OpenFile(p.name+"/"+name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		object, stat, identityErr := initialImportObject(file)
		err = errors.Join(identityErr, file.Close())
		if err != nil || object.Mode != unix.S_IFREG|0600 || object.UID != 0 || object.GID != 0 ||
			object.Device != p.object.Device || object.MountID != p.object.MountID || stat.Nlink != 1 || stat.Size != 0 {
			t.Fatal("created pressure inode lost its private identity", err)
		}
		p.files[name] = object
		if err := unix.Fstatfs(int(p.dir.Fd()), &capacity); err != nil {
			t.Fatal(err)
		}
	}
	if err := unix.Syncfs(int(p.dir.Fd())); err != nil {
		t.Fatal(err)
	}
	createdCount := len(p.files)
	var admitted StorageCapacity
	if err := json.Unmarshal(boundedLossAction(t, "capacity"), &admitted); err != nil || !admitted.WriteAdmitted || capacity.Ffree != 1 {
		t.Fatal("initial import must pass actual block admission with only its directory inode left", admitted, err)
	}
	_, failure := boundedLossCommand(append([]string{"--worker-run-root", "/run/factorybox"}, recoveryArgs("initial-import", saved.Request.CheckpointSHA256)...)...)
	if failure == nil || !strings.Contains(strings.ToLower(failure.Error()), "no space left on device") {
		t.Fatal("real initial journal publication did not hit kernel ENOSPC", failure)
	}
	current, err := os.OpenRoot(status.Lifecycle.Root)
	if err != nil {
		t.Fatal(err)
	}
	defer current.Close()
	operation := "forks/initial-import"
	for _, path := range []string{operation + "/import.json", operation + "/intent.json", operation + "/recovery", "objects/initial-import"} {
		if _, err := current.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("failed initial publication gained payload or target authority", path, err)
		}
	}
	if err := unix.Fstatfs(int(p.dir.Fd()), &capacity); err != nil || capacity.Ffree != 0 {
		t.Fatal("initial journal did not fail at the actual last inode", capacity, err)
	}
	data := boundedLossAction(t, "inspect-recovery-import", "--target", "initial-import")
	var inspected RecoveryImportInspection
	if err := json.Unmarshal(data, &inspected); err != nil || !inspected.ReservationAbsent || inspected.Owner != "" ||
		inspected.PreparationSHA256 != "" || len(inspected.Inventory.Entries) != 1 || inspected.TargetID != "initial-import" {
		t.Fatal("public initial-stage inspection invented a reservation or omitted its exact directory", err)
	}
	requestFile := "/output/initial-import-cancel-request.json"
	if err := os.WriteFile(requestFile, data, 0644); err != nil {
		t.Fatal(err)
	}
	if err := unix.Fstatfs(int(p.dir.Fd()), &capacity); err != nil || capacity.Ffree != 0 {
		t.Fatal("initial cancellation no longer has zero free store inodes", capacity, err)
	}
	cancelArgs := []string{"cancel-recovery-import", "--request-file", requestFile}
	completion := boundedLossAction(t, cancelArgs...)
	var cancelled RecoveryImportCancellation
	if err := json.Unmarshal(completion, &cancelled); err != nil || !cancelled.ReservationAbsent || cancelled.CompletedAt == "" || cancelled.TargetID != "initial-import" {
		t.Fatal("public initial cancellation needed an unavailable inode or lost its inspection", err)
	}
	if _, err := current.Lstat(operation); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("initial-stage cancellation left its directory", err)
	}
	if _, err := boundedLossCommand(append([]string{"--worker-run-root", "/run/factorybox"}, cancelArgs...)...); err == nil {
		t.Fatal("unaccepted metadata acquired missing-entry cancellation replay")
	}
	// A new same-name directory must not inherit the old inspection even if
	// the filesystem reuses the freed inode. Then exercise the other possible
	// pre-publication state: one private truncated temporary.
	if err := current.Mkdir(operation, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := boundedLossCommand(append([]string{"--worker-run-root", "/run/factorybox"}, cancelArgs...)...); err == nil {
		t.Fatal("recreated metadata generation inherited stale cancellation")
	}
	if err := p.releaseOne("00000000"); err != nil {
		t.Fatal(err)
	}
	if err := current.WriteFile(operation+"/.checkpoint-"+strings.Repeat("a", 32), []byte(`{"partial":`), 0600); err != nil {
		t.Fatal(err)
	}
	data = boundedLossAction(t, "inspect-recovery-import", "--target", "initial-import")
	var partial RecoveryImportInspection
	if err := json.Unmarshal(data, &partial); err != nil || !partial.ReservationAbsent || len(partial.Inventory.Entries) != 2 {
		t.Fatal("exact partial initial publication was not inspected", err)
	}
	if err := os.WriteFile(requestFile, data, 0644); err != nil {
		t.Fatal(err)
	}
	var partialCapacity unix.Statfs_t
	if err := unix.Fstatfs(int(p.dir.Fd()), &partialCapacity); err != nil || partialCapacity.Ffree != 0 {
		t.Fatal("partial initial cancellation no longer has zero free store inodes", partialCapacity, err)
	}
	boundedLossAction(t, cancelArgs...)
	if _, err := current.Lstat(operation); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("partial initial publication remains after explicit cancellation", err)
	}
	if err := ownedtree.ValidateComplete(ctx, oldRoot, original); err != nil {
		t.Fatal("initial publication failure/cancellation changed original saved data", err)
	}
	proof["initial_inode_count"], proof["inodes_before_import"], proof["inodes_at_cancellation"] = initialFree, 1, capacity.Ffree
	proof["partial_inodes_at_cancellation"] = partialCapacity.Ffree
	proof["admitted_before_publication"], proof["actual_initial_publication_enospc"], proof["error"] = true, true, failure.Error()
	proof["first_inspection"], proof["first_cancellation"] = inspected, cancelled
	proof["initial_metadata_only"], proof["explicit_cancel_without_new_inode"] = true, true
	proof["missing_entry_replay_refused"], proof["recreated_generation_refused"], proof["partial_publication_cancelled"] = true, true, true
	proof["original_backing_unchanged"], proof["durable_reservation_created"] = true, false
	pressureCount := len(p.files)
	if err := p.close(); err != nil {
		t.Fatal(err)
	}
	var restored unix.Statfs_t
	file, err := os.Open(status.Store)
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(unix.Fstatfs(int(file.Fd()), &restored), file.Close()); err != nil || restored.Ffree < initialFree {
		t.Fatal("exact private pressure cleanup did not restore inode capacity", err)
	}
	proof["only_exact_owned_pressure_released"], proof["pressure_files_released"], proof["inodes_after_reclaim"] = true, createdCount, restored.Ffree
	proof["pressure_files_released_by_plan"], proof["pressure_files_released_individually"] = pressureCount, 1
	proof["full_original_recovery_follows"] = true
}
