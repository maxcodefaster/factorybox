//go:build linux

package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"factorybox/internal/sysboxcapture"
	"golang.org/x/sys/unix"
)

func TestNativeStoppedCaptureBackingRetirement(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_STOPPED_CAPTURE_RETIREMENT") != "1" {
		t.Skip("requires the owned stopped-capture bounded worker")
	}
	if root := os.Getenv("FACTORYBOX_STOPPED_CAPTURE_DELETE_FIXTURE"); root != "" {
		stoppedCaptureDeleteFixture(t, root)
		return
	}
	status := boundedLossStatusNow(t)
	cli := os.Getenv("FACTORYBOX_RETIREMENT_CLI")
	if cli == "" {
		cli = "/opt/factorybox/factorybox"
	}
	command := func(args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 420*time.Second)
		defer cancel()
		return exec.CommandContext(ctx, cli, append([]string{"--worker-run-root", "/run/factorybox"}, args...)...).CombinedOutput()
	}
	var started, deleted View
	var generation string
	if !t.Run("real-unpublished-capture", func(t *testing.T) {
		started = standbyFixture(t, status, "stopped-capture", nil)
		child := exec.Command(os.Args[0], "-test.run=^TestNativeStoppedCaptureBackingRetirement$", "-test.timeout=420s")
		child.Env = append(os.Environ(), "FACTORYBOX_STOPPED_CAPTURE_DELETE_FIXTURE="+status.Lifecycle.Root)
		if output, err := child.CombinedOutput(); err != nil {
			t.Fatal("actual capture and controller exit", string(output), err)
		}
		generation = status.Lifecycle.Root + "/objects/stopped-capture/captures/" + strings.Repeat("b", 32)
		deadline := time.Now().Add(10 * time.Second)
		for {
			output, err := command("delete", "stopped-capture")
			if err == nil {
				if err := json.Unmarshal(output, &deleted); err != nil {
					t.Fatal(err)
				}
				break
			}
			// A refused pre-admission Delete has not dispatched runtime deletion.
			if !bytes.Contains(output, []byte("live or unproved original actor")) || time.Now().After(deadline) {
				t.Fatal("Delete after original actor exit", string(output), err)
			}
			time.Sleep(10 * time.Millisecond)
		}
		if deleted.Record.Phase != "deleted" || deleted.Record.Delete == nil ||
			deleted.Record.Delete.CompletedAt == "" || deleted.Record.Pause == nil ||
			deleted.Record.Pause.CompletedAt != "" {
			t.Fatal("Delete completed an unpublished Pause or lacks its own completion")
		}
	}) {
		return
	}
	evidence := map[string][]byte{}
	for _, name := range []string{"capture-delete-admission.json", "checkpoint-intent.json", "source-state.json",
		"filesystem.json", "guard/guard-intent.json", "guard/guard-armed.json",
		"guard/capture-worker-intent.json", "guard/capture-process-complete.json",
		"guard/capture-stop-intent.json", "guard/capture-worker-outcome.json", "guard/guard-outcome.json",
		"guard/criu-retained-arm.json"} {
		data, err := os.ReadFile(generation + "/" + name)
		if err != nil {
			t.Fatal(err)
		}
		evidence[name] = data
	}
	request := SandboxBackingRequest{started.Record.ID, started.Record.Owner, started.Record.InputSHA256}
	run := func() ([]byte, error) { return command(backingRetirementArgs(request)...) }
	admission := generation + "/capture-delete-admission.json"
	if err := os.Rename(admission, admission+".original"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(); err == nil {
		t.Fatal("unproved stopped-capture backing was retired")
	}
	if err := os.Rename(admission+".original", admission); err != nil {
		t.Fatal(err)
	}
	pages, err := filepath.Glob(generation + "/images/pages-*.img")
	if err != nil || len(pages) == 0 {
		t.Fatal("real capture lacks its page images", err)
	}
	hardlink := status.Store + "/foreign-stopped-capture-image"
	if err := os.Link(pages[0], hardlink); err != nil {
		t.Fatal(err)
	}
	if _, err := run(); err == nil {
		t.Fatal("capture backing with a foreign hardlink was accepted")
	}
	if err := os.Remove(hardlink); err != nil {
		t.Fatal(err)
	}
	output, err := run()
	if err != nil {
		t.Fatal("ordinary stopped-capture retirement", string(output), err)
	}
	for _, path := range []string{"images", "filesystem"} {
		if _, err := os.Lstat(generation + "/" + path); !os.IsNotExist(err) {
			t.Fatal("retirement left private capture backing", path, err)
		}
	}
	for name, before := range evidence {
		after, err := os.ReadFile(generation + "/" + name)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("retirement changed original capture/failure evidence", name, err)
		}
	}
	if replay, err := run(); err != nil || !bytes.Equal(output, replay) {
		t.Fatal("completed retirement failed exact replay", string(replay), err)
	}
	observed := boundedLossView(t, "state", started.Record.ID)
	before, _ := json.Marshal(deleted.Record)
	after, _ := json.Marshal(observed.Record)
	if !bytes.Equal(before, after) {
		t.Fatal("backing retirement changed the original Pause/Delete")
	}
	for _, name := range []string{"checkpoint.json", "capture-result.json"} {
		if _, err := os.Lstat(generation + "/" + name); !os.IsNotExist(err) {
			t.Fatal("cleanup invented a committed checkpoint", name, err)
		}
	}
	successor := generation + "/filesystem"
	if err := os.Mkdir(successor, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(successor+"/foreign", []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := run(); err == nil {
		t.Fatal("completed retirement accepted a foreign successor")
	}
	if data, err := os.ReadFile(successor + "/foreign"); err != nil || string(data) != "preserve" {
		t.Fatal("cleanup damaged a foreign successor", err)
	}
	if err := os.Remove(successor + "/foreign"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(successor); err != nil {
		t.Fatal(err)
	}
	boundedLossSave(t, "stopped-capture-retirement.json", map[string]bool{
		"passed": true, "real_capture_and_stop": true, "confirmed_delete": true,
		"images_and_filesystem_retired": true, "original_evidence_preserved": true,
		"no_checkpoint_manufactured": true, "exact_completion_replayed": true,
		"missing_admission_refused": true, "foreign_successor_preserved": true,
		"original_record_unchanged":        true,
		"unaccepted_preparation_recovered": true,
	})
	t.Run("sealed-native-filesystem", stoppedCaptureNativeBackingRetirement)
}

// Keep the original copied-filesystem fixture above. A real common-subvolume
// capture must also retire its sealed roots without making them writable.
func stoppedCaptureNativeBackingRetirement(t *testing.T) {
	t.Setenv("FACTORYBOX_STOPPED_CAPTURE_NATIVE_LAYOUT", "1")
	status := boundedLossStatusNow(t)
	id := "stopped-capture-native"
	bundle := status.Store + "/" + id + "-bundle"
	for _, path := range []string{bundle, bundle + "/rootfs"} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if output, err := exec.Command("cp", "-a", "/busybox", bundle+"/lower-input").CombinedOutput(); err != nil {
		t.Fatal(string(output), err)
	}
	var base Base
	if err := json.Unmarshal(boundedLossAction(t, "prepare-base", "--source", bundle+"/lower-input", id+"-base"), &base); err != nil {
		t.Fatal(err)
	}
	lower, err := sysboxcapture.BindFilesystemBase(base.Tree, bundle+"/lower")
	if err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("btrfs", "subvolume", "create", "--", bundle+"/mutable").CombinedOutput(); err != nil {
		t.Fatal(string(output), err)
	}
	for _, name := range []string{"upper", "work"} {
		if err := os.Mkdir(bundle+"/mutable/"+name, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := unix.Mount("overlay", bundle+"/rootfs", "overlay", 0,
		"index=off,lowerdir="+bundle+"/lower,upperdir="+bundle+"/mutable/upper,workdir="+bundle+"/mutable/work"); err != nil {
		t.Fatal(err)
	}
	overlayMounted, lowerMounted := true, true
	t.Cleanup(func() {
		if _, err := boundedLossCommand("--worker-run-root", "/run/factorybox", "delete", id); err != nil {
			t.Error(err)
		}
		if overlayMounted {
			if err := unix.Unmount(bundle+"/rootfs", 0); err != nil {
				t.Error(err)
			}
		}
		if lowerMounted {
			if err := sysboxcapture.RetireImmutableAlias(bundle+"/lower", lower); err != nil {
				t.Error(err)
			}
		}
	})
	if output, err := exec.Command(status.Lifecycle.Runtime, "spec", "--bundle", bundle).CombinedOutput(); err != nil {
		t.Fatal(string(output), err)
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
	process["env"] = []string{"HOME=/root", "PATH=/bin:/sbin:/usr/bin:/usr/sbin"}
	spec["root"].(map[string]any)["readonly"] = false
	spec["annotations"] = map[string]string{baseAnnotation: id + "-base"}
	data, err = json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundle+"/config.json", data, 0600); err != nil {
		t.Fatal(err)
	}
	started := boundedLossView(t, "start", "--bundle", bundle, id)
	child := exec.Command(os.Args[0], "-test.run=^TestNativeStoppedCaptureBackingRetirement$", "-test.timeout=420s")
	child.Env = append(os.Environ(), "FACTORYBOX_STOPPED_CAPTURE_DELETE_FIXTURE="+status.Lifecycle.Root)
	if output, err := child.CombinedOutput(); err != nil {
		t.Fatal("actual native capture and controller exit", string(output), err)
	}
	generation := status.Lifecycle.Root + "/objects/" + id + "/captures/" + strings.Repeat("b", 32)
	cli := os.Getenv("FACTORYBOX_RETIREMENT_CLI")
	if cli == "" {
		cli = "/opt/factorybox/factorybox"
	}
	command := func(args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 420*time.Second)
		defer cancel()
		return exec.CommandContext(ctx, cli, append([]string{"--worker-run-root", "/run/factorybox"}, args...)...).CombinedOutput()
	}
	for deadline := time.Now().Add(10 * time.Second); ; {
		output, err := command("delete", id)
		if err == nil {
			break
		}
		if !bytes.Contains(output, []byte("live or unproved original actor")) || time.Now().After(deadline) {
			t.Fatal("Delete after native capture", string(output), err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := unix.Unmount(bundle+"/rootfs", 0); err != nil {
		t.Fatal(err)
	}
	overlayMounted = false
	if err := sysboxcapture.RetireImmutableAlias(bundle+"/lower", lower); err != nil {
		t.Fatal(err)
	}
	lowerMounted = false
	manifest, err := os.ReadFile(generation + "/filesystem.json")
	var filesystem sysboxcapture.FilesystemSnapshot
	if err != nil || json.Unmarshal(manifest, &filesystem) != nil || filesystem.Version != 2 {
		t.Fatal("fixture did not capture native snapshots", err)
	}
	nativeRoots := 0
	for _, tree := range filesystem.Roots {
		if tree.Native != nil {
			nativeRoots++
			if tree.Native.Saved.Flags != 1 {
				t.Fatal("capture root is not sealed")
			}
		}
	}
	if nativeRoots != 8 {
		t.Fatal("fixture omitted native mutable/manager roots", nativeRoots)
	}
	output, err := command(backingRetirementArgs(SandboxBackingRequest{id, started.Record.Owner, started.Record.InputSHA256})...)
	if err != nil {
		t.Fatal("retire actual sealed native capture", string(output), err)
	}
	for _, name := range []string{"filesystem", "images"} {
		if _, err := os.Lstat(generation + "/" + name); !os.IsNotExist(err) {
			t.Fatal("native capture payload remains", name, err)
		}
	}
	if after, err := os.ReadFile(generation + "/filesystem.json"); err != nil || !bytes.Equal(manifest, after) {
		t.Fatal("native retirement changed original manifest", err)
	}
	if replay, err := command(backingRetirementArgs(SandboxBackingRequest{id, started.Record.Owner, started.Record.InputSHA256})...); err != nil || !bytes.Equal(output, replay) {
		t.Fatal("native retirement replay changed", string(replay), err)
	}
	boundedLossSave(t, "stopped-capture-native-retirement.json", map[string]any{
		"passed": true, "sealed_native_roots": nativeRoots, "original_manifest_preserved": true,
		"native_payload_absent": true, "exact_completion_replayed": true,
	})
}
