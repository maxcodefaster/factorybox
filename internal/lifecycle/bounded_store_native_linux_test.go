//go:build linux

package lifecycle

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

	"golang.org/x/sys/unix"
)

func TestNativeBoundedWorkerStore(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_BOUNDED_STORE") != "1" {
		t.Skip("requires the exact delivered bounded-store worker")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 390*time.Second)
	defer cancel()
	data, err := exec.Command("/opt/factorybox/factorybox", "worker-status").Output()
	if err != nil {
		t.Fatal("live delivered worker status", err)
	}
	var status struct {
		Store     string `json:"store"`
		Lifecycle Config `json:"lifecycle"`
	}
	if err := json.Unmarshal(data, &status); err != nil || status.Lifecycle.BoundedStore == nil {
		t.Fatal("missing bounded worker identity", err)
	}
	engine, err := Open(status.Lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { engine.Close() })
	report, err := engine.Capacity()
	if err != nil || report.BoundedImageBytes != 512<<20 || report.TotalBytes > report.BoundedImageBytes ||
		!report.WriteAdmitted || report.Host == nil || report.Host.MinFreeBytes != 12<<30 ||
		report.MinFreeBytes != 64<<20 || report.WriteReserve != 128<<20 {
		t.Fatal("bounded and host capacity were not separated", report, err)
	}
	bundle := status.Store + "/bundle"
	for _, path := range []string{bundle, bundle + "/upper", bundle + "/work", bundle + "/rootfs"} {
		mode := os.FileMode(0755)
		if path == bundle {
			mode = 0700
		}
		if err := os.Mkdir(path, mode); err != nil {
			t.Fatal(err)
		}
	}
	if out, err := exec.Command("cp", "-a", "/busybox", bundle+"/lower").CombinedOutput(); err != nil {
		t.Fatal("copy private fixture root", string(out), err)
	}
	binary, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundle+"/lower/bin/capture-actor", binary, 0755); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mount("overlay", bundle+"/rootfs", "overlay", 0,
		"index=off,lowerdir="+bundle+"/lower,upperdir="+bundle+"/upper,workdir="+bundle+"/work"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := unix.Unmount(bundle+"/rootfs", 0); err != nil {
			t.Error("release exact fixture overlay", err)
		}
	})
	if out, err := exec.Command(engine.config.Runtime, "spec", "--bundle", bundle).CombinedOutput(); err != nil {
		t.Fatal("create private OCI fixture", string(out), err)
	}
	data, err = os.ReadFile(bundle + "/config.json")
	if err != nil {
		t.Fatal(err)
	}
	var spec map[string]any
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatal(err)
	}
	process := spec["process"].(map[string]any)
	process["terminal"], process["cwd"] = false, "/"
	process["args"] = []string{"/bin/sh", "-c", "exec /bin/env FACTORYBOX_CAPTURE_ACTOR=1 /bin/capture-actor -test.run=^TestCaptureActor$ -test.timeout=0 </dev/null >/dev/null 2>&1"}
	process["env"] = []string{"HOME=/root", "PATH=/bin:/sbin:/usr/bin:/usr/sbin"}
	spec["root"].(map[string]any)["readonly"] = false
	spec["linux"].(map[string]any)["resources"] = map[string]any{"pids": map[string]any{"limit": 128},
		"devices": []map[string]any{{"allow": true, "access": "rwm"}}}
	input, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundle+"/config.json", input, 0600); err != nil {
		t.Fatal(err)
	}
	// A real external writable source must be rejected before intent exists.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(input, &raw); err != nil {
		t.Fatal(err)
	}
	var mounts []map[string]any
	if err := json.Unmarshal(raw["mounts"], &mounts); err != nil {
		t.Fatal(err)
	}
	mounts = append(mounts, map[string]any{"destination": "/outside", "type": "bind", "source": "/var/lib/sysbox", "options": []string{"rbind", "rw"}})
	raw["mounts"], _ = json.Marshal(mounts)
	outside, _ := json.Marshal(raw)
	if err := os.WriteFile(bundle+"/config.json", outside, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Start(ctx, "outside", bundle); err == nil || !strings.Contains(err.Error(), "outside bounded store") {
		t.Fatal("external writable source accepted", err)
	}
	if _, err := engine.root.Lstat("objects/outside"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("refusal published intent", err)
	}
	if err := os.WriteFile(bundle+"/config.json", input, 0600); err != nil {
		t.Fatal(err)
	}
	view, err := engine.Start(ctx, "source", bundle)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, id := range []string{"branch", "source"} {
			if _, err := engine.Delete(context.Background(), id); err != nil {
				t.Error("delete owned fixture", id, err)
			}
		}
	})
	var before captureActorState
	for deadline := time.Now().Add(10 * time.Second); ; {
		before, err = readCaptureActor(view.Runtime.PID, "get")
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("RAM actor readiness", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	first := strings.Repeat("1", 32)
	if _, err := engine.Checkpoint(ctx, "source", first); err != nil {
		t.Fatal("bounded checkpoint", err)
	}
	branch, err := engine.Fork(ctx, "source", first, "branch")
	if err != nil {
		t.Fatal("bounded fork", err)
	}
	if actual, err := readCaptureActor(branch.Runtime.PID, "get"); err != nil || !reflect.DeepEqual(actual, before) {
		t.Fatal("fork lost private RAM", err)
	}
	if _, err := readCaptureActor(branch.Runtime.PID, "advance"); err != nil {
		t.Fatal(err)
	}
	if actual, err := readCaptureActor(view.Runtime.PID, "get"); err != nil || !reflect.DeepEqual(actual, before) {
		t.Fatal("branch modified source RAM", err)
	}
	if _, err := engine.Delete(ctx, "branch"); err != nil {
		t.Fatal(err)
	}
	paused, err := engine.Pause(ctx, "source", strings.Repeat("2", 32))
	if err != nil {
		t.Fatal("bounded pause", err)
	}
	if paused.Record.Pause == nil || !paused.Record.Pause.Snapshot.FileBacked || paused.Record.Pause.Snapshot.ResidentBytesAfter != 0 || paused.Record.Pause.After.Anon != 0 {
		t.Fatal("pause did not release its measured RAM", paused.Record.Pause)
	}
	view, err = engine.Resume(ctx, "source", strings.Repeat("2", 32))
	if err != nil {
		t.Fatal("bounded resume", err)
	}
	if actual, err := readCaptureActor(view.Runtime.PID, "get"); err != nil || !reflect.DeepEqual(actual, before) {
		t.Fatal("resume lost private RAM", err)
	}
	// The actual Sysbox guest fills only its bounded overlay, including the
	// final blocks after a large write fails. No host filesystem is filled.
	lock, err := engine.lock()
	if err != nil {
		t.Fatal(err)
	}
	out, pressureErr := engine.command(ctx, lock, "exec", view.Record.RuntimeID, "/bin/sh", "-c",
		"dd if=/dev/zero of=/owned-pressure bs=1048576; dd if=/dev/zero bs=4096 >>/owned-pressure; sync; dd if=/dev/zero of=/boundary-canary bs=4096 count=1")
	lock.Close()
	if pressureErr == nil || !strings.Contains(strings.ToLower(pressureErr.Error()), "no space left") {
		t.Fatal("guest did not encounter the fixed capacity", string(out), pressureErr)
	}
	full, err := engine.Capacity()
	if err != nil || full.AvailableBytes != 0 || full.WriteAdmitted || full.Host == nil || full.Host.AvailableBytes <= 12<<30 {
		t.Fatal("guest exhaustion escaped store or consumed host floor", full, err)
	}
	_, rejection := engine.Checkpoint(ctx, "source", strings.Repeat("3", 32))
	if rejection == nil || !strings.Contains(rejection.Error(), "new storage writes refused") {
		t.Fatal("full store admitted capture", rejection)
	}
	if actual, err := readCaptureActor(view.Runtime.PID, "get"); err != nil || !reflect.DeepEqual(actual, before) {
		t.Fatal("capacity refusal changed live RAM", err)
	}
	for _, name := range []string{"owned-pressure", "boundary-canary"} {
		for _, path := range []string{view.Record.Rootfs + "/" + name, fmt.Sprintf("/proc/%d/root/%s", view.Runtime.PID, name)} {
			var observed unix.Stat_t
			statErr := unix.Stat(path, &observed)
			t.Logf("pressure before removal %s dev=%d inode=%d bytes=%d links=%d mode=%o err=%v", path, observed.Dev, observed.Ino, observed.Size, observed.Nlink, observed.Mode, statErr)
		}
		if err := os.Remove(view.Record.Rootfs + "/" + name); err != nil {
			t.Fatal("remove only owned pressure file", err)
		}
	}
	for _, name := range []string{"owned-pressure", "boundary-canary"} {
		var observed unix.Stat_t
		path := fmt.Sprintf("/proc/%d/root/%s", view.Runtime.PID, name)
		t.Logf("guest path after removal %s stat=%+v err=%v", path, &observed, unix.Stat(path, &observed))
	}
	// Persist the unlink and complete the filesystem's pending work before
	// asserting that the owned allocation has returned to available space.
	directory, err := os.Open(view.Record.Rootfs)
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(unix.Syncfs(int(directory.Fd())), directory.Close()); err != nil {
		t.Fatal(err)
	}
	// The guest's separate idmapped overlay can retain this unlinked inode.
	// Never count an unlink as reclaimed capacity: inspect the actual guest
	// alias, then truncate only the exact pressure inode created above.
	guestPressure := fmt.Sprintf("/proc/%d/root/owned-pressure", view.Runtime.PID)
	var held unix.Stat_t
	if err := unix.Stat(guestPressure, &held); err != nil || held.Nlink != 0 || held.Size <= 0 {
		t.Fatal("expected retained unlinked pressure alias", held, err)
	}
	pressure, err := os.OpenFile(guestPressure, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	var actual unix.Stat_t
	if err := unix.Fstat(int(pressure.Fd()), &actual); err != nil || actual.Dev != held.Dev || actual.Ino != held.Ino || actual.Nlink != 0 || actual.Size != held.Size {
		pressure.Close()
		t.Fatal("retained pressure inode changed before exact release", actual, err)
	}
	if err := errors.Join(pressure.Truncate(0), pressure.Sync(), pressure.Close()); err != nil {
		t.Fatal("release exact owned pressure blocks", err)
	}
	if available, err := engine.Capacity(); err != nil || !available.WriteAdmitted {
		t.Fatal("owned release did not restore admission", available, err)
	}
	if _, err := engine.Checkpoint(ctx, "source", first); err != nil {
		t.Fatal("old complete checkpoint lost after pressure", err)
	}
	// Admission returning is not useful recovery. Restore the retained pre-full
	// generation through the delivered CLI and actually use its private RAM.
	prior := boundedLossView(t, "fork", "--source", "source", "--snapshot-id", first, "prior-recovered")
	t.Cleanup(func() {
		if _, err := engine.Delete(context.Background(), "prior-recovered"); err != nil {
			t.Error("delete retained checkpoint recovery", err)
		}
	})
	if actual, err := readCaptureActor(prior.Runtime.PID, "get"); err != nil || !reflect.DeepEqual(actual, before) {
		t.Fatal("pre-full checkpoint no longer restores useful private RAM", actual, err)
	}
	if advanced, err := readCaptureActor(prior.Runtime.PID, "advance"); err != nil || advanced.Counter != before.Counter+1 {
		t.Fatal("retained recovery did not perform new useful work", advanced, err)
	}
	if actual, err := readCaptureActor(view.Runtime.PID, "get"); err != nil || !reflect.DeepEqual(actual, before) {
		t.Fatal("retained recovery changed the original source", actual, err)
	}
	boundedLossView(t, "delete", "prior-recovered")

	// Also require new durable work after reclaim. This cannot pass by merely
	// replaying the older successful checkpoint or inspecting retained files.
	freshID := strings.Repeat("4", 32)
	freshRaw := boundedLossAction(t, "checkpoint", "--snapshot-id", freshID, "source")
	var fresh Checkpoint
	if err := json.Unmarshal(freshRaw, &fresh); err != nil || fresh.ID != freshID ||
		fresh.SourcePID != view.Runtime.PID || fresh.ReleasedAt == "" || len(fresh.Files) == 0 {
		t.Fatal("fresh post-reclaim checkpoint did not complete and release source", fresh, err)
	}
	if replay := boundedLossAction(t, "checkpoint", "--snapshot-id", freshID, "source"); !bytes.Equal(replay, freshRaw) {
		t.Fatal("fresh post-reclaim checkpoint replay changed its exact result")
	}
	recovered := boundedLossView(t, "fork", "--source", "source", "--snapshot-id", freshID, "post-reclaim")
	t.Cleanup(func() {
		if _, err := engine.Delete(context.Background(), "post-reclaim"); err != nil {
			t.Error("delete new post-reclaim descendant", err)
		}
	})
	if actual, err := readCaptureActor(recovered.Runtime.PID, "get"); err != nil || !reflect.DeepEqual(actual, before) {
		t.Fatal("fresh post-reclaim fork lost private RAM", actual, err)
	}
	advanced, err := readCaptureActor(recovered.Runtime.PID, "advance")
	if err != nil || advanced.Counter != before.Counter+1 {
		t.Fatal("fresh post-reclaim descendant did not perform useful work", advanced, err)
	}
	if actual, err := readCaptureActor(view.Runtime.PID, "get"); err != nil || !reflect.DeepEqual(actual, before) {
		t.Fatal("fresh post-reclaim descendant changed source RAM", actual, err)
	}
	postPauseID := strings.Repeat("5", 32)
	postPause := boundedLossView(t, "pause", "--snapshot-id", postPauseID, "post-reclaim")
	if postPause.Record.Pause == nil || postPause.Record.Pause.CompletedAt == "" ||
		postPause.Record.Pause.After.Anon != 0 || postPause.Record.Pause.Snapshot.ResidentBytesAfter != 0 {
		t.Fatal("post-reclaim pause did not commit and release RAM", postPause.Record.Pause)
	}
	recovered = boundedLossView(t, "resume", "--snapshot-id", postPauseID, "post-reclaim")
	if actual, err := readCaptureActor(recovered.Runtime.PID, "get"); err != nil || !reflect.DeepEqual(actual, advanced) {
		t.Fatal("post-reclaim resume lost newly advanced RAM", actual, err)
	}
	boundedLossView(t, "delete", "source")
	if actual, err := readCaptureActor(recovered.Runtime.PID, "get"); err != nil || !reflect.DeepEqual(actual, advanced) {
		t.Fatal("post-reclaim descendant depends on deleted original source", actual, err)
	}
	proof := map[string]any{"schema": 1, "passed": true, "production_qualified": false, "capacity": report, "full_capacity": full,
		"direct_io": status.Lifecycle.BoundedStore.DirectIO, "external_writable_bind_refused": true, "checkpoint_fork": true,
		"branch_ram_independent": true, "pause": paused.Record.Pause, "resume_ram_preserved": true,
		"guest_actual_enospc": pressureErr.Error(), "capture_refused_at_limit": true, "prior_checkpoint_preserved": true}
	proof["retained_unlinked_guest_alias"] = held
	proof["owned_truncate_released_blocks"] = true
	proof["prior_checkpoint_restored_and_useful_after_reclaim"] = true
	proof["fresh_public_checkpoint_after_reclaim"] = fresh
	proof["fresh_public_checkpoint_exact_replay"] = true
	proof["post_reclaim_descendant_ram_independent"] = true
	proof["post_reclaim_pause"] = postPause.Record.Pause
	proof["post_reclaim_resume_preserves_new_work"] = true
	proof["post_reclaim_descendant_useful_after_source_delete"] = true
	data, err = json.MarshalIndent(proof, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("/output/bounded-store.json", append(data, '\n'), 0644); err != nil {
		t.Fatal(err)
	}
}
