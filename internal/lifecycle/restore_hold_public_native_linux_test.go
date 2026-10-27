//go:build linux && (amd64 || arm64)

package lifecycle

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestNativeRestoreHoldPublicResume(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_RESTORE_HOLD") != "1" {
		t.Skip("owned public Resume control only")
	}
	nativeRestoreHoldPublicResume(t, boundedLossStatusNow(t))
}

// Exercise the public Resume path without Docker, Connector, a shared Source or
// a second Sandbox. The consumer failure also occurred for its sleep-only peer.
func nativeRestoreHoldPublicResume(t *testing.T, status boundedLossStatus) map[string]any {
	t.Helper()
	const id = "restore-hold-public-sleep"
	unresolved := false
	source := timerReaderFixtureFilesWithCleanup(t, status, id, "exec /bin/sleep 300", false, nil, func(t *testing.T, _ View) {
		if unresolved {
			path := status.Lifecycle.Root + "/objects/" + id + "/record.json"
			before, err := os.ReadFile(path)
			if err != nil {
				t.Error("retain failed public Resume record", err)
				return
			}
			_, rejected := boundedLossCommand("--worker-run-root", "/run/factorybox", "delete", id)
			after, readErr := os.ReadFile(path)
			if rejected == nil || readErr != nil || string(before) != string(after) {
				t.Error("unresolved public Resume lost its original record", rejected, readErr)
			}
			return // Only the exact-owned outer fixture may remove unresolved state.
		}
		if _, err := boundedLossCommand("--worker-run-root", "/run/factorybox", "delete", id); err != nil {
			t.Error("delete exact public Resume fixture", err)
			return
		}
		if err := unix.Unmount(status.Store+"/"+id+"-bundle/rootfs", 0); err != nil {
			t.Error("release exact public Resume fixture overlay", err)
		}
	})
	observations := []map[string]any{}
	proof := map[string]any{"passed": false, "cycles": 20, "no_consumer_or_peer": true}
	defer func() {
		proof["observations"] = observations
		boundedLossSave(t, "restore-hold-public-sleep.json", proof)
	}()
	for cycle := 1; cycle <= 20; cycle++ {
		snapshot := fmt.Sprintf("%032x", cycle)
		paused := boundedLossView(t, "pause", "--snapshot-id", snapshot, id)
		if paused.Record.Phase != "paused" || paused.Record.Pause == nil || paused.Record.Owner != source.Record.Owner ||
			paused.Record.RuntimeID != source.Record.RuntimeID || paused.Record.Pause.SnapshotID != snapshot ||
			paused.Record.Pause.After.Anon != 0 || paused.Record.Pause.Snapshot.ResidentBytesAfter != 0 {
			t.Fatal("public Pause changed its identity or retained resident memory")
		}
		entry := map[string]any{"cycle": cycle, "snapshot_id": snapshot}
		observations = append(observations, entry)
		data, err := boundedLossCommand("--worker-run-root", "/run/factorybox", "resume", "--snapshot-id", snapshot, id)
		if err != nil {
			unresolved = true
			entry["error"] = err.Error()
			samples := []map[string]any{}
			for index := 0; index < 3; index++ {
				row := map[string]any{"index": index}
				for _, name := range []string{"cgroup.freeze", "cgroup.events"} {
					value, readErr := os.ReadFile(guardGroupPath(source.Record) + "/" + name)
					row[name] = map[string]any{"value": string(value), "error": fmt.Sprint(readErr)}
				}
				samples = append(samples, row)
				time.Sleep(10 * time.Millisecond)
			}
			entry["read_only_after_failure"] = samples
			t.Fatal("public sleep-only Resume failed", err)
		}
		var resumed View
		if err := json.Unmarshal(data, &resumed); err != nil {
			t.Fatal("decode exact public Resume", err)
		}
		if resumed.Record.Phase != "running" || resumed.Record.Owner != source.Record.Owner ||
			resumed.Record.RuntimeID != source.Record.RuntimeID ||
			resumed.Record.RuntimeCreated != source.Record.RuntimeCreated ||
			resumed.Record.Pause.SnapshotID != snapshot || resumed.Record.Pause.Resume == nil ||
			strings.TrimSpace(resumed.Record.Pause.Resume.CompletedAt) == "" {
			t.Fatal("public Resume replaced its original Sandbox or lacks exact completion")
		}
		entry["passed"] = true
	}
	proof["passed"] = true
	return proof
}
