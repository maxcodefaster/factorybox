//go:build linux

package lifecycle

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestNativeFailedCaptureBackingRetirement(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_FAILED_CAPTURE_RETIREMENT") != "1" {
		t.Skip("owned failed-capture retirement fixture only")
	}
	if root := os.Getenv("FACTORYBOX_FAILED_PAUSE_DELETE_FIXTURE"); root != "" {
		failedPauseDeleteFixture(t, root)
		return
	}
	status := boundedLossStatusNow(t)
	bundle := status.Store + "/failed-capture-input"
	for _, path := range []string{bundle, bundle + "/upper", bundle + "/work", bundle + "/rootfs"} {
		mode := os.FileMode(0755)
		if path == bundle {
			mode = 0700
		}
		if err := os.Mkdir(path, mode); err != nil {
			t.Fatal(err)
		}
	}
	if output, err := exec.Command("cp", "-a", "/busybox", bundle+"/lower").CombinedOutput(); err != nil {
		t.Fatal("prepare private root", string(output), err)
	}
	if err := unix.Mount("overlay", bundle+"/rootfs", "overlay", 0,
		"index=off,lowerdir="+bundle+"/lower,upperdir="+bundle+"/upper,workdir="+bundle+"/work"); err != nil {
		t.Fatal(err)
	}
	mounted := true
	t.Cleanup(func() {
		boundedLossAction(t, "delete", "failed-capture")
		if mounted {
			if err := unix.Unmount(bundle+"/rootfs", 0); err != nil {
				t.Error(err)
			}
		}
	})
	if output, err := exec.Command(status.Lifecycle.Runtime, "spec", "--bundle", bundle).CombinedOutput(); err != nil {
		t.Fatal("prepare exact runtime spec", string(output), err)
	}
	data, err := os.ReadFile(bundle + "/config.json")
	if err != nil {
		t.Fatal(err)
	}
	var spec map[string]any
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatal(err)
	}
	process := spec["process"].(map[string]any)
	process["terminal"], process["cwd"] = false, "/"
	process["args"] = []string{"/bin/sh", "-c", "exec /bin/sleep 300 </dev/null >/dev/null 2>&1"}
	process["env"] = []string{"PATH=/bin:/sbin:/usr/bin:/usr/sbin", "HOME=/root"}
	spec["root"].(map[string]any)["readonly"] = false
	spec["linux"].(map[string]any)["resources"] = map[string]any{
		"pids":    map[string]any{"limit": 64},
		"devices": []map[string]any{{"allow": true, "access": "rwm"}},
	}
	data, err = json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundle+"/config.json", data, 0600); err != nil {
		t.Fatal(err)
	}
	started := boundedLossView(t, "start", "--bundle", bundle, "failed-capture")
	child := exec.Command(os.Args[0], "-test.run=^TestNativeFailedCaptureBackingRetirement$", "-test.timeout=30s")
	child.Env = append(os.Environ(), "FACTORYBOX_FAILED_PAUSE_DELETE_FIXTURE="+status.Lifecycle.Root)
	if output, err := child.CombinedOutput(); err != nil {
		t.Fatal("abandon the actual guarded failed writer", string(output), err)
	}
	snapshot := strings.Repeat("b", 32) // Exact failedPauseDeleteFixture generation.
	generation := status.Lifecycle.Root + "/objects/failed-capture/captures/" + snapshot
	images := generation + "/images"
	if err := os.MkdirAll(images, 0700); err != nil {
		t.Fatal(err)
	}
	payload, err := os.OpenFile(images+"/pages-1.img", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	err = unix.Fallocate(int(payload.Fd()), 0, 0, 32<<20)
	if closeErr := payload.Close(); err != nil || closeErr != nil {
		t.Fatal("allocate actual failed capture backing", err, closeErr)
	}
	for _, path := range []string{images, images + "/pages-1.img"} {
		if err := os.Chown(path, 100000, 100000); err != nil {
			t.Fatal("preserve the failed writer's mapped guest ownership", err)
		}
	}
	deleted := boundedLossView(t, "delete", "failed-capture")
	if deleted.Record.Pause == nil || deleted.Record.Pause.SnapshotID != snapshot || deleted.Record.Phase != "deleted" {
		t.Fatal("failed Pause lacks confirmed native Delete")
	}
	evidence := map[string][]byte{}
	for _, name := range []string{"capture-delete-admission.json", "checkpoint-intent.json",
		"source-state.json", "guard/guard-intent.json", "guard/capture-worker-outcome.json"} {
		evidence[name], err = os.ReadFile(generation + "/" + name)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := unix.Unmount(bundle+"/rootfs", 0); err != nil {
		t.Fatal(err)
	}
	mounted = false
	request := SandboxBackingRequest{"failed-capture", started.Record.Owner, started.Record.InputSHA256}
	command := append([]string{"--worker-run-root", "/run/factorybox"}, backingRetirementArgs(request)...)
	cli := os.Getenv("FACTORYBOX_RETIREMENT_CLI")
	if cli == "" {
		cli = "/opt/factorybox/factorybox"
	}
	run := func() ([]byte, error) {
		return exec.Command(cli, command...).CombinedOutput()
	}
	admission := generation + "/capture-delete-admission.json"
	if err := os.Rename(admission, admission+".original"); err != nil {
		t.Fatal(err)
	}
	if output, err := run(); err == nil {
		t.Fatal("failed capture without its original admission was accepted", string(output))
	}
	if err := os.Rename(admission+".original", admission); err != nil {
		t.Fatal(err)
	}
	published := generation + "/checkpoint.json"
	if err := os.WriteFile(published, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := run(); err == nil {
		t.Fatal("a newly published or uncertain generation was accepted", string(output))
	}
	if err := os.Remove(published); err != nil {
		t.Fatal(err)
	}
	hardlink := status.Store + "/foreign-capture-hardlink"
	if err := os.Link(images+"/pages-1.img", hardlink); err != nil {
		t.Fatal(err)
	}
	if output, err := run(); err == nil {
		t.Fatal("a foreign failed-image hardlink was accepted", string(output))
	}
	if err := os.Remove(hardlink); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mount("tmpfs", images, "tmpfs", 0, "size=1m,mode=0700"); err != nil {
		t.Fatal(err)
	}
	_, mountErr := run()
	if err := unix.Unmount(images, 0); err != nil {
		t.Fatal(err)
	}
	if mountErr == nil {
		t.Fatal("a foreign mount covering failed images was accepted")
	}
	// Reproduce a real failed write in the same fixed filesystem. The small
	// cleanup journal must not depend on space in this exhausted guest store.
	filler, err := os.OpenFile(status.Store+"/failed-capture-pressure", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	probe, err := os.OpenFile(status.Store+"/opencode-copy-probe", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		filler.Close()
		probe.Close()
		os.Remove(filler.Name())
		os.Remove(probe.Name())
	})
	for _, step := range []int64{1 << 20, 4096} {
		info, err := filler.Stat()
		if err != nil {
			t.Fatal(err)
		}
		for offset := info.Size(); ; offset += step {
			err := unix.Fallocate(int(filler.Fd()), 0, offset, step)
			if errors.Is(err, unix.ENOSPC) {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := probe.Write(make([]byte, 1<<20)); !errors.Is(err, unix.ENOSPC) {
		t.Fatal("fixed store did not reproduce the original failed write", err)
	}
	output, err := run()
	if err != nil {
		t.Fatal("ordinary delivered backing retirement", string(output), err)
	}
	if _, err := os.Lstat(images); !os.IsNotExist(err) {
		t.Fatal("retire-sandbox left failed-capture images after confirmed Delete", err)
	}
	if _, err := probe.Write(make([]byte, 1<<20)); err != nil {
		t.Fatal("the same write still fails after ordinary failed-capture cleanup", err)
	}
	for name, before := range evidence {
		after, err := os.ReadFile(generation + "/" + name)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("retirement changed failed capture diagnostic metadata", name, err)
		}
	}
	if replay, err := run(); err != nil || !bytes.Equal(output, replay) {
		t.Fatal("ordinary exact retirement replay changed", string(replay), err)
	}
	if err := os.Mkdir(images, 0700); err != nil {
		t.Fatal(err)
	}
	successor := images + "/foreign-successor"
	if err := os.WriteFile(successor, []byte("preserve foreign successor\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if replay, err := run(); err == nil {
		t.Fatal("completed failed-capture retirement accepted a successor", string(replay))
	}
	if data, err := os.ReadFile(successor); err != nil || string(data) != "preserve foreign successor\n" {
		t.Fatal("retirement changed the foreign successor", err)
	}
	observed := boundedLossView(t, "state", "failed-capture")
	if !reflect.DeepEqual(observed.Record, deleted.Record) {
		t.Fatal("retirement rewrote the original failed Pause or Delete")
	}
	boundedLossSave(t, "failed-capture-retirement.json", map[string]any{
		"passed": true, "actual_guarded_failed_writer": true, "confirmed_delete": true,
		"ordinary_cleanup_removed_failed_images": true, "failed_evidence_preserved": true,
		"exact_completion_replayed": true, "foreign_successor_preserved": true,
		"retained_record_unchanged": true, "retired_payload_bytes": 32 << 20,
		"actual_enospc_before_cleanup": true, "same_write_succeeded_after_cleanup": true,
		"missing_admission_refused": true, "published_generation_preserved": true,
		"foreign_hardlink_preserved": true, "foreign_mount_refused": true,
		"mapped_guest_images_retired": true,
		"bundle":                      filepath.Base(bundle),
	})
}
