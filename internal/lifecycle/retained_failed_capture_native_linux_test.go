//go:build linux

package lifecycle

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestNativeRetainedFailedCapturePrepare(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_RETAINED_FAILED_CAPTURE_PREPARE") != "1" {
		t.Skip("owned legacy retirement and same-store replacement fixture only")
	}
	if root := os.Getenv("FACTORYBOX_FAILED_PAUSE_DELETE_FIXTURE"); root != "" {
		failedPauseDeleteFixture(t, root)
		return
	}
	status := boundedLossStatusNow(t)
	var started View
	var deleted Record
	snapshot := strings.Repeat("b", 32)
	generation := status.Lifecycle.Root + "/objects/failed-capture/captures/" + snapshot
	images := generation + "/images"
	if !t.Run("original-guarded-source", func(t *testing.T) {
		started = standbyFixture(t, status, "failed-capture", nil)
		child := exec.Command(os.Args[0], "-test.run=^TestNativeRetainedFailedCapturePrepare$", "-test.timeout=30s")
		child.Env = append(os.Environ(), "FACTORYBOX_FAILED_PAUSE_DELETE_FIXTURE="+status.Lifecycle.Root)
		if output, err := child.CombinedOutput(); err != nil {
			t.Fatal("abandon the actual guarded writer", string(output), err)
		}
		if err := os.MkdirAll(images, 0700); err != nil {
			t.Fatal(err)
		}
		payload, err := os.OpenFile(images+"/pages-1.img", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
		if err != nil {
			t.Fatal(err)
		}
		if err := unix.Fallocate(int(payload.Fd()), 0, 0, 32<<20); err != nil {
			payload.Close()
			t.Fatal(err)
		}
		if err := payload.Close(); err != nil {
			t.Fatal(err)
		}
		for _, path := range []string{images, images + "/pages-1.img"} {
			if err := os.Chown(path, 100000, 100000); err != nil {
				t.Fatal(err)
			}
		}
		deleted = boundedLossView(t, "delete", "failed-capture").Record
		if deleted.Phase != "deleted" || deleted.Pause == nil || deleted.Pause.SnapshotID != snapshot ||
			deleted.Delete == nil || deleted.Delete.CompletedAt == "" {
			t.Fatal("original failed capture has no confirmed Delete")
		}
	}) {
		return
	}
	// The shared standby fixture has now released only its exact input mount.
	// An older CLI legitimately completed the original Sandbox stage without
	// knowing how to retire this failed capture. Keep that receipt unchanged.
	request := SandboxBackingRequest{"failed-capture", started.Record.Owner, started.Record.InputSHA256}
	command := append([]string{"--worker-run-root", "/run/factorybox"}, backingRetirementArgs(request)...)
	output, err := exec.Command(os.Getenv("FACTORYBOX_RETIREMENT_CLI"), command...).CombinedOutput()
	if err != nil {
		t.Fatal("original legacy backing retirement", string(output), err)
	}
	var receipt SandboxBackingRetirement
	if err := json.Unmarshal(output, &receipt); err != nil ||
		receipt.Request != request || receipt.PayloadKind != "sandbox-backing" {
		t.Fatal("legacy retirement lost its original request", err)
	}
	if info, err := os.Stat(images + "/pages-1.img"); err != nil || info.Size() != 32<<20 {
		t.Fatal("legacy retirement did not preserve the failed payload for the additional proof", err)
	}
	originalRecord, err := os.ReadFile(status.Lifecycle.Root + "/objects/failed-capture/record.json")
	if err != nil {
		t.Fatal(err)
	}
	hashes := map[string]string{}
	for _, name := range []string{"capture-delete-admission.json", "checkpoint-intent.json",
		"source-state.json", "guard/guard-intent.json", "guard/capture-worker-outcome.json"} {
		data, err := os.ReadFile(generation + "/" + name)
		if err != nil {
			t.Fatal(err)
		}
		hashes[name] = sum(data)
	}
	boundedLossSave(t, "retained-failed-capture-prepare.json", map[string]any{
		"request": request, "receipt": receipt, "source_root": status.Lifecycle.Root,
		"snapshot_id": snapshot, "record_sha256": sum(originalRecord),
		"metadata_sha256": hashes, "retained_payload_bytes": 32 << 20,
	})
}
