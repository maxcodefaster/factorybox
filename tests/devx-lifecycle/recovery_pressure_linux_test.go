//go:build linux && factorybox_worker_probe

package devx_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"factorybox/internal/lifecycle"
	"factorybox/internal/ownedtree"
	"factorybox/internal/worker"
	"golang.org/x/sys/unix"
)

func recoveryPressureCommand(t *testing.T, f *fixture, label string, args ...string) ([]byte, string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/factorybox", append([]string{"--worker-run-root", f.workerRunRoot}, args...)...)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	start := time.Now()
	err := command.Run()
	for suffix, data := range map[string][]byte{".stdout": stdout.Bytes(), ".stderr": stderr.Bytes()} {
		if err := os.WriteFile("/output/recovery-pressure-"+label+suffix, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	data, marshalErr := json.Marshal(map[string]any{"arguments": args, "seconds": time.Since(start).Seconds(),
		"context_error": fmt.Sprint(ctx.Err()), "operation_deadline_seconds": 90})
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	if err := os.WriteFile("/output/recovery-pressure-"+label+".timing.json", data, 0600); err != nil {
		t.Fatal(err)
	}
	if ctx.Err() != nil {
		t.Fatal("recovery pressure observation exceeded its original command bound", ctx.Err())
	}
	return stdout.Bytes(), stderr.String(), err
}

func checkConsumerRecoveryPressure(t *testing.T, f *fixture, saved consumerPauseProof, originalRequest []string) {
	t.Helper()
	status, err := worker.Current("/run/factorybox")
	if err != nil || status.Lifecycle.Root != f.root || status.Lifecycle.BoundedStore == nil ||
		status.Lifecycle.BoundedStore.Image.Bytes != 32<<30 {
		t.Fatal("recovery pressure requires its exact delivered bounded worker", err)
	}
	capacity := func() lifecycle.StorageCapacity {
		t.Helper()
		var value lifecycle.StorageCapacity
		if err := json.Unmarshal(f.command("capacity"), &value); err != nil || value.BoundedImageBytes != 32<<30 ||
			value.Host == nil || value.Host.AvailableBytes <= value.Host.MinFreeBytes {
			t.Fatal("recovery pressure capacity or separate host floor changed", value, err)
		}
		return value
	}
	before := capacity()
	keep := before.MinFreeBytes + before.WriteReserve + (8 << 20)
	if !before.WriteAdmitted || before.AvailableBytes <= keep+(1<<30) {
		t.Fatal("recovery pressure lacks a useful original capacity control", before)
	}
	metadata := map[string][]byte{}
	for _, name := range []string{"objects/source/record.json", "objects/source/captures/" + saved.Paused.Pause.SnapshotID + "/checkpoint.json"} {
		data, err := os.ReadFile(saved.SourceRoot + "/" + name)
		if err != nil {
			t.Fatal(err)
		}
		metadata[name] = data
	}
	path := status.Store + "/recovery-pressure-" + saved.Owner
	pressure, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	var held unix.Stat_t
	if err := unix.Fstat(int(pressure.Fd()), &held); err != nil || held.Dev != status.Lifecycle.BoundedStore.BlockDevice || held.Nlink != 1 || held.Mode != unix.S_IFREG|0600 {
		pressure.Close()
		t.Fatal("pressure file is not an exact private bounded inode", held, err)
	}
	released := false
	release := func() error {
		var current, named unix.Stat_t
		if err := errors.Join(unix.Fstat(int(pressure.Fd()), &current), unix.Lstat(path, &named)); err != nil {
			return err
		}
		for _, observed := range []unix.Stat_t{current, named} {
			if observed.Dev != held.Dev || observed.Ino != held.Ino || observed.Mode != held.Mode ||
				observed.Uid != held.Uid || observed.Gid != held.Gid || observed.Nlink != 1 {
				return fmt.Errorf("owned pressure identity changed before release")
			}
		}
		if err := errors.Join(pressure.Truncate(0), pressure.Sync()); err != nil {
			return err
		}
		if err := os.Remove(path); err != nil {
			return err
		}
		if err := errors.Join(unix.Syncfs(int(pressure.Fd())), pressure.Close()); err != nil {
			return err
		}
		released = true
		return nil
	}
	defer func() {
		if !released {
			if err := release(); err != nil {
				t.Error("release only exact test-owned pressure allocation", err)
			}
		}
	}()
	allocated := int64(before.AvailableBytes - keep)
	if err := unix.Fallocate(int(pressure.Fd()), 0, 0, allocated); err != nil {
		t.Fatal("allocate bounded test pressure", err)
	}
	if err := pressure.Sync(); err != nil {
		t.Fatal(err)
	}
	under := capacity()
	if !under.WriteAdmitted || under.AvailableBytes < under.MinFreeBytes+under.WriteReserve {
		t.Fatal("copy must pass actual admission before its ENOSPC", under)
	}
	request := append([]string{}, originalRequest...)
	request[len(request)-1] = "capacity-import"
	_, failure, failed := recoveryPressureCommand(t, f, "import", request...)
	if failed == nil || !strings.Contains(strings.ToLower(failure), "no space left on device") {
		t.Fatal("admitted real recovery copy did not hit actual ENOSPC", failed, failure)
	}
	root, err := os.OpenRoot(f.root)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	for _, name := range []string{"forks/capacity-import/intent.json", "objects/capacity-import"} {
		if _, err := root.Lstat(name); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("failed import published uncertain runtime authority", name, err)
		}
	}
	ctx := context.Background()
	partial, err := ownedtree.Capture(ctx, root, []string{"forks/capacity-import"})
	if err != nil || len(partial.Entries) < 3 {
		t.Fatal("actual partial import was not retained intact", err)
	}
	inspect := []string{"inspect-recovery-checkpoint", "--source-root", saved.SourceRoot, "--source-owner", saved.Paused.Owner,
		"--snapshot-id", saved.Paused.Pause.SnapshotID, "--checkpoint-sha256", saved.Paused.Pause.CheckpointSHA256, "source"}
	data, inspectFailure, inspectErr := recoveryPressureCommand(t, f, "inspect-full", inspect...)
	var inspection lifecycle.RetainedCheckpointInspection
	if err := json.Unmarshal(data, &inspection); inspectErr != nil || err != nil || !inspection.FullContentVerified || inspection.Files <= 0 || inspection.FilesystemTrees != 9 {
		t.Fatal("original complete checkpoint is not usable under pressure", inspectErr, inspectFailure, err)
	}
	for name, expected := range metadata {
		actual, err := os.ReadFile(saved.SourceRoot + "/" + name)
		if err != nil || !bytes.Equal(actual, expected) {
			t.Fatal("failed import changed original committed metadata", name, err)
		}
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	after := capacity()
	if !after.WriteAdmitted || after.AvailableBytes <= under.AvailableBytes+(1<<30) {
		t.Fatal("exact owned release did not restore useful capacity", after)
	}
	_, retryFailure, retryErr := recoveryPressureCommand(t, f, "retry", request...)
	if retryErr == nil || !strings.Contains(retryFailure, "import is incomplete") {
		t.Fatal("incomplete import identity was blindly retried after reclaim", retryErr, retryFailure)
	}
	unchanged, err := ownedtree.Capture(ctx, root, partial.Paths)
	if err != nil || !reflect.DeepEqual(unchanged, partial) {
		t.Fatal("refused retry modified its retained partial import", err)
	}
	proof := map[string]any{"schema": 1, "passed": false, "production_qualified": false, "source_root": saved.SourceRoot,
		"before": before, "under_pressure": under, "after_reclaim": after, "allocated_pressure_bytes": allocated,
		"admitted_before_copy": true, "actual_import_enospc": true, "import_error": failure,
		"original_metadata_unchanged": true, "old_checkpoint_fully_verified_under_pressure": true, "inspection": inspection,
		"incomplete_target_refused": true, "incomplete_target_error": retryFailure, "partial_inventory": partial,
		"partial_inventory_unchanged_by_retry": true, "only_exact_owned_pressure_released": true, "admission_restored": true}
	t.Cleanup(func() {
		root, err := os.OpenRoot(f.root)
		if err != nil {
			t.Error(err)
			return
		}
		defer root.Close()
		final, err := ownedtree.Capture(context.Background(), root, partial.Paths)
		if err != nil || !reflect.DeepEqual(final, partial) {
			t.Error("full original consumer changed its retained incomplete import", err)
			return
		}
		proof["partial_inventory_preserved_at_cleanup"] = true
		proof["full_original_consumer_passed"] = !t.Failed()
		proof["partial_retained_after_full_consumer"] = !t.Failed()
		proof["passed"] = !t.Failed()
		data, err := json.MarshalIndent(proof, "", "  ")
		if err != nil {
			t.Error(err)
			return
		}
		if err := os.WriteFile("/output/recovery-pressure.json", append(data, '\n'), 0600); err != nil {
			t.Error(err)
		}
	})
}
