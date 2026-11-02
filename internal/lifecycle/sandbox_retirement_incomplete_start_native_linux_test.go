//go:build linux

package lifecycle

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestNativeDeletedIncompleteStartBackingRetirement(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_BACKING_RETIREMENT") != "1" {
		t.Skip("owned Sandbox backing retirement fixture only")
	}
	status := boundedLossStatusNow(t)
	bundle := status.Store + "/incomplete-start-bundle"
	for _, name := range []string{"", "/upper", "/work", "/rootfs"} {
		mode := os.FileMode(0755)
		if name == "" {
			mode = 0700
		}
		if err := os.Mkdir(bundle+name, mode); err != nil {
			t.Fatal(err)
		}
	}
	if output, err := exec.Command("cp", "-a", "/busybox", bundle+"/lower").CombinedOutput(); err != nil {
		t.Fatal(string(output), err)
	}
	if err := os.WriteFile(bundle+"/upper/private-window", bytes.Repeat([]byte{7}, 32<<20), 0600); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mount("overlay", bundle+"/rootfs", "overlay", 0,
		"index=off,lowerdir="+bundle+"/lower,upperdir="+bundle+"/upper,workdir="+bundle+"/work"); err != nil {
		t.Fatal(err)
	}
	mounted := true
	t.Cleanup(func() {
		if mounted {
			if err := unix.Unmount(bundle+"/rootfs", 0); err != nil {
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
	process["args"] = []string{"/bin/sh", "-c", "exec sleep 600"}
	process["env"] = []string{"HOME=/root", "PATH=/bin:/sbin:/usr/bin:/usr/sbin"}
	spec["root"].(map[string]any)["readonly"] = false
	spec["linux"].(map[string]any)["resources"] = map[string]any{
		"pids":    map[string]any{"limit": 128},
		"devices": []map[string]any{{"allow": true, "access": "rwm"}},
	}
	data, err = json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundle+"/config.json", data, 0600); err != nil {
		t.Fatal(err)
	}
	const id = "incomplete-start"
	// The owned init keeps inherited stdout open. The actual Sysbox command
	// returns but its I/O acknowledgement fails, leaving a running native
	// runtime with an unfinished Start. Do not edit any operation metadata.
	_, startErr := boundedLossCommand("--worker-run-root", "/run/factorybox",
		"start", "--bundle", bundle, id)
	if startErr == nil || !strings.Contains(startErr.Error(), "WaitDelay expired before I/O complete") {
		t.Fatal("fixture did not reach its real incomplete-Start boundary", startErr)
	}
	t.Cleanup(func() { boundedLossAction(t, "delete", id) })
	started := boundedLossView(t, "state", id)
	if started.Runtime == nil || started.Runtime.Status != "running" ||
		started.Record.Fork != nil || !started.Record.Start.Attempted ||
		started.Record.Start.CompletedAt != "" || started.Record.Phase != "starting" {
		t.Fatal("fixture needs the real original incomplete Start, not a restored Fork")
	}
	boundedLossSave(t, "incomplete-start-original.json", started)
	request := SandboxBackingRequest{id, started.Record.Owner, started.Record.InputSHA256}
	command := append([]string{"--worker-run-root", "/run/factorybox"}, backingRetirementArgs(request)...)
	if _, err := boundedLossCommand(command...); err == nil {
		t.Fatal("unconfirmed Delete authorized retirement")
	}
	deleted := boundedLossView(t, "delete", id)
	if deleted.Record.Start.CompletedAt != "" || deleted.Record.RuntimeCreated == "" ||
		deleted.Record.RuntimeCreated != started.Runtime.Created || deleted.Record.Delete == nil ||
		!deleted.Record.Delete.Attempted || deleted.Record.Delete.CompletedAt == "" {
		t.Fatal("ordinary Delete did not settle the real incomplete Start")
	}
	if err := unix.Unmount(bundle+"/rootfs", 0); err != nil {
		t.Fatal(err)
	}
	mounted = false
	response := boundedLossAction(t, backingRetirementArgs(request)...)
	var retired SandboxBackingRetirement
	if err := json.Unmarshal(response, &retired); err != nil || retired.Request != request ||
		retired.CompletedAt == "" || retired.After.AvailableBytes <= retired.Before.AvailableBytes {
		t.Fatal("incomplete Start backing did not return real capacity", err, retired)
	}
	if replay := boundedLossAction(t, backingRetirementArgs(request)...); !bytes.Equal(response, replay) {
		t.Fatal("completed incomplete-Start retirement replay changed")
	}
	after := boundedLossView(t, "state", id)
	if after.Record.Start.CompletedAt != "" || after.Record.Delete == nil ||
		after.Record.Delete.CompletedAt != deleted.Record.Delete.CompletedAt {
		t.Fatal("retirement invented Start completion or changed native Delete")
	}
	boundedLossSave(t, "incomplete-start-backing-retirement.json", map[string]any{
		"passed": true, "actual_native_start_attempt": true, "missing_start_acknowledgement_preserved": true,
		"actual_native_delete": true, "unconfirmed_delete_refused": true,
		"actual_capacity_returned": true, "exact_completion_replayed": true,
		"retirement": retired,
	})
}
