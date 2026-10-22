//go:build linux

package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// A private 128-MiB tmpfs bounds all pressure; no host filesystem is filled.
// This fixture exercises both preflight rejection and a real post-admission
// ENOSPC. Successful preflight alone is not a hard storage quota.
func TestNativeStorageAdmissionAndENOSPC(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_STORAGE") != "1" {
		t.Skip("requires the owned native storage-pressure worker")
	}
	original, bundle := nativeEngine(t, "/bin/env FACTORYBOX_CAPTURE_ACTOR=1 /bin/capture-actor -test.run=^TestCaptureActor$ -test.timeout=0")
	binary, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundle+"/lower/bin/capture-actor", binary, 0755); err != nil {
		t.Fatal(err)
	}
	config := original.config
	if err := unix.Mount("tmpfs", config.Root, "tmpfs", unix.MS_NOSUID|unix.MS_NODEV, "size=128m,mode=0700"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if t.Failed() {
			// Retain the complete bounded failed store before losing tmpfs with
			// the worker. Tar omits live Unix sockets but preserves their logs
			// and all persistent regular-file input bytes. The outer runner also
			// retains the complete stopped volume and fixture source.
			command := exec.Command("tar", "--sparse", "-C", config.Root, "-cpf", filepath.Dir(config.Root)+"/failed-pressure-store.tar", ".")
			if out, err := command.CombinedOutput(); err != nil {
				t.Errorf("retain bounded failed tmpfs: %v: %s", err, out)
			}
		}
		if err := unix.Unmount(config.Root, 0); err != nil {
			t.Error("unmount exact owned pressure fixture", err)
		}
	})
	config.MinFreeBytes, config.WriteReserve = 8<<20, 8<<20
	config.Criu, config.CriuSHA256, config.ManagerRoot = "/out/criu", os.Getenv("FACTORYBOX_CRIU_SHA256"), "/var/lib/sysbox/store/manager"
	engine, err := Open(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { engine.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Second)
	defer cancel()
	started, err := engine.Start(ctx, "source", bundle)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		// Explicit fixture-only cleanup headroom after all pressure assertions.
		// This changes no host setting and does not rescue the tested operation.
		if err := unix.Mount("tmpfs", config.Root, "tmpfs", unix.MS_REMOUNT|unix.MS_NOSUID|unix.MS_NODEV, "size=144m,mode=0700"); err != nil {
			t.Error("allow bounded cleanup metadata on the owned tmpfs", err)
		}
		if _, err := engine.Delete(context.Background(), "source"); err != nil {
			t.Error("delete exact pressure source", err)
		}
	})
	var before captureActorState
	for deadline := time.Now().Add(10 * time.Second); ; {
		before, err = readCaptureActor(started.Runtime.PID, "get")
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("private RAM actor did not become ready", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	first := strings.Repeat("1", 32)
	checkpoint, err := engine.Checkpoint(ctx, "source", first)
	if err != nil {
		t.Fatal("initial complete checkpoint", err)
	}
	relative, _ := checkpointPath("source", first)
	verifyOriginal := func() {
		t.Helper()
		if err := engine.verifyCheckpoint(ctx, started.Record, first, relative, checkpoint); err != nil {
			t.Fatal("earlier complete checkpoint changed", err)
		}
	}
	pressure := config.Root + "/owned-pressure"
	fill := func() {
		t.Helper()
		report, err := engine.Capacity()
		if err != nil || report.AvailableBytes <= 8<<20 {
			t.Fatal("pressure setup lacks bounded available capacity", report, err)
		}
		file, err := os.OpenFile(pressure, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		err = unix.Fallocate(int(file.Fd()), 0, 0, int64(report.AvailableBytes-(8<<20)))
		closeErr := file.Close()
		if err != nil || closeErr != nil {
			t.Fatal("allocate only the bounded private pressure file", err, closeErr)
		}
	}
	clear := func() {
		t.Helper()
		if err := os.Remove(pressure); err != nil {
			t.Fatal(err)
		}
	}
	fill()
	beforeRecord, err := engine.root.ReadFile("objects/source/record.json")
	if err != nil {
		t.Fatal(err)
	}
	reject := func(name string, operation func() error) {
		t.Helper()
		if err := operation(); err == nil || !strings.Contains(err.Error(), "new storage writes refused") {
			t.Fatal(name+" did not refuse before new writes", err)
		}
	}
	reject("start", func() error { _, err := engine.Start(ctx, "new-source", bundle); return err })
	reject("base", func() error { _, err := engine.PrepareBase(ctx, "new-base", bundle+"/lower"); return err })
	reject("checkpoint", func() error { _, err := engine.Checkpoint(ctx, "source", strings.Repeat("2", 32)); return err })
	reject("pause", func() error { _, err := engine.Pause(ctx, "source", strings.Repeat("3", 32)); return err })
	reject("fork", func() error { _, err := engine.Fork(ctx, "source", first, "new-fork"); return err })
	for _, path := range []string{"objects/new-source", "bases/new-base", "forks/new-fork", "objects/source/captures/" + strings.Repeat("2", 32), "objects/source/captures/" + strings.Repeat("3", 32)} {
		if _, err := engine.root.Lstat(path); !os.IsNotExist(err) {
			t.Fatal("rejected operation published backing", path, err)
		}
	}
	afterRecord, err := engine.root.ReadFile("objects/source/record.json")
	if err != nil || string(beforeRecord) != string(afterRecord) {
		t.Fatal("refused pause changed the live source transition", err)
	}
	if _, err := engine.Checkpoint(ctx, "source", first); err != nil {
		t.Fatal("low-space completed checkpoint replay was blocked", err)
	}
	verifyOriginal()
	clear()
	actual, err := readCaptureActor(started.Runtime.PID, "get")
	if err != nil || !reflect.DeepEqual(before, actual) {
		t.Fatal("preflight rejection changed private RAM/file/watch identity", err)
	}
	// Nonzero data prevents a sparse-copy shortcut from evading actual ENOSPC.
	large, err := os.OpenFile(bundle+"/upper/large-pressure", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	chunk := []byte(strings.Repeat("x", 1<<20))
	for range 160 {
		if _, err := large.Write(chunk); err != nil {
			large.Close()
			t.Fatal(err)
		}
	}
	if err := large.Close(); err != nil {
		t.Fatal(err)
	}
	report, err := engine.Capacity()
	if err != nil || !report.WriteAdmitted {
		t.Fatal("ENOSPC case never passed initial admission", report, err)
	}
	_, failure := engine.Checkpoint(ctx, "source", strings.Repeat("5", 32))
	if failure == nil || !strings.Contains(strings.ToLower(failure.Error()), "no space left") {
		t.Fatal("actual bounded filesystem exhaustion was not exercised", failure)
	}
	current, err := engine.Inspect(ctx, "source")
	if err != nil || current.Runtime == nil || current.Runtime.PID != started.Runtime.PID || current.Runtime.Status != "running" {
		t.Fatal("post-admission ENOSPC lost or froze the original source", current, err, failure)
	}
	actual, err = readCaptureActor(started.Runtime.PID, "get")
	if err != nil || !reflect.DeepEqual(before, actual) {
		t.Fatal("ENOSPC corrupted the original private RAM/file/watch state", err)
	}
	verifyOriginal()
	result := map[string]any{"passed": true, "source_pid": started.Runtime.PID,
		"private_ram_sha256": sum(before.Payload), "initial_checkpoint": first,
		"bounded_filesystem_bytes": 128 << 20, "preflight_rejections": 5, "actual_enospc": failure.Error(), "capacity_before_enospc": report}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("/output/storage.json", data, 0644); err != nil {
		t.Fatal(err)
	}
	t.Log("bounded ENOSPC and unchanged source/checkpoint", strconv.Itoa(started.Runtime.PID), fmt.Sprint(failure))
}

func TestNativeStorageResumeAdmission(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_STORAGE") != "1" {
		t.Skip("requires the owned native storage-pressure worker")
	}
	// Resume needs genuinely file-backed snapshot pages. Keep this independent
	// source on the existing data volume; tmpfs cannot prove snapshot-RAM release.
	engine, bundle := nativeEngine(t, "/bin/env FACTORYBOX_CAPTURE_ACTOR=1 /bin/capture-actor -test.run=^TestCaptureActor$ -test.timeout=0")
	binary, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundle+"/lower/bin/capture-actor", binary, 0755); err != nil {
		t.Fatal(err)
	}
	engine.config.Criu, engine.config.CriuSHA256 = "/out/criu", os.Getenv("FACTORYBOX_CRIU_SHA256")
	engine.config.ManagerRoot = "/var/lib/sysbox/store/manager"
	engine.config.MinFreeBytes, engine.config.WriteReserve = 8<<20, 8<<20
	ctx := context.Background()
	started, err := engine.Start(ctx, "source", bundle)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := engine.Delete(ctx, "source"); err != nil {
			t.Error("delete exact resume-admission source", err)
		}
	})
	var before captureActorState
	for deadline := time.Now().Add(10 * time.Second); ; {
		before, err = readCaptureActor(started.Runtime.PID, "get")
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	snapshot := strings.Repeat("6", 32)
	paused, err := engine.Pause(ctx, "source", snapshot)
	if err != nil {
		t.Fatal("create real file-backed pause", err)
	}
	if paused.Record.Pause.After.Anon != 0 || paused.Record.Pause.Snapshot.ResidentBytesAfter != 0 {
		t.Fatal("pause did not release actual process and snapshot RAM")
	}
	beforeRecord, err := engine.root.ReadFile("objects/source/record.json")
	if err != nil {
		t.Fatal(err)
	}
	capacity, err := engine.Capacity()
	if err != nil {
		t.Fatal(err)
	}
	// Raising the administrator's floor above this entire filesystem exercises
	// the real admission decision without filling the host data volume.
	engine.config.MinFreeBytes = capacity.TotalBytes
	if _, err := engine.Resume(ctx, "source", snapshot); err == nil || !strings.Contains(err.Error(), "new storage writes refused") {
		t.Fatal("resume was not rejected before its transition", err)
	}
	afterRecord, err := engine.root.ReadFile("objects/source/record.json")
	if err != nil || string(beforeRecord) != string(afterRecord) {
		t.Fatal("rejected resume modified its confirmed pause", err)
	}
	engine.config.MinFreeBytes = 8 << 20
	resumed, err := engine.Resume(ctx, "source", snapshot)
	if err != nil {
		t.Fatal("resume after restoring the admitted policy", err)
	}
	actual, err := readCaptureActor(resumed.Runtime.PID, "get")
	if err != nil || !reflect.DeepEqual(before, actual) {
		t.Fatal("resume lost original private RAM/file/watch state", err)
	}
	data, err := json.Marshal(map[string]any{"passed": true, "preflight_rejections": 1, "private_ram_sha256": sum(before.Payload),
		"checkpoint_sha256": paused.Record.Pause.CheckpointSHA256, "capacity": capacity})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("/output/resume-admission.json", data, 0644); err != nil {
		t.Fatal(err)
	}
}

func TestNativeStorageReserveOwnership(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_STORAGE") != "1" {
		t.Skip("requires the owned native storage-pressure worker")
	}
	root := t.TempDir()
	anchor, err := os.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer anchor.Close()
	config := captureGuardConfig{Version: 1, Token: strings.Repeat("a", 32), Source: Record{Owner: strings.Repeat("b", 32)}}
	if err := captureGuardWrite(anchor, "guard-intent.json", config); err != nil {
		t.Fatal(err)
	}
	if err := prepareCaptureReserve(anchor, config); err != nil {
		t.Fatal(err)
	}
	path := root + "/.rollback-reserve"
	if err := os.Rename(path, path+"-original"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("foreign replacement must survive"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := releaseCaptureReserve(anchor, config); err == nil {
		t.Fatal("foreign reserve inode was adopted")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "foreign replacement must survive" {
		t.Fatal("foreign replacement was modified", err)
	}
	if err := os.Rename(path, path+"-foreign"); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+"-original", path); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(path, path+"-alias"); err != nil {
		t.Fatal(err)
	}
	if err := releaseCaptureReserve(anchor, config); err == nil {
		t.Fatal("shared reserve inode was truncated")
	}
	if err := os.Remove(path + "-alias"); err != nil {
		t.Fatal(err)
	}
	wrong := config
	wrong.Token = strings.Repeat("c", 32)
	if err := releaseCaptureReserve(anchor, wrong); err == nil {
		t.Fatal("another guard released the reserve")
	}
	for range 2 {
		if err := releaseCaptureReserve(anchor, config); err != nil {
			t.Fatal("exact reserve release/replay failed", err)
		}
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() != 0 {
		t.Fatal("original reserve did not release its actual blocks", err)
	}
	if err := os.WriteFile("/output/reserve-ownership.json", []byte("{\"passed\":true,\"replacement_preserved\":true,\"shared_inode_refused\":true,\"replay_verified\":true}\n"), 0644); err != nil {
		t.Fatal(err)
	}
}
