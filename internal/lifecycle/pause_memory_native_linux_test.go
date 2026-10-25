//go:build linux && (amd64 || arm64)

package lifecycle

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestNativeStoppedMemoryAccounting(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_STOPPED_MEMORY") != "1" {
		t.Skip("owned stopped-memory accounting fixture only")
	}
	status := boundedLossStatusNow(t)
	source := timerReaderFixture(t, status, "stopped-memory", "exec /bin/sleep 60", false)
	e := &Engine{}
	if err := e.refreshStoppedMemoryAccounting(source.Record); err == nil {
		t.Fatal("accounting refresh accepted a populated original cgroup")
	}
	if _, err := e.stoppedSandboxMemory(context.Background(), source.Record); err == nil {
		t.Fatal("stopped memory observation accepted a live source")
	}
	paused := boundedLossView(t, "pause", "--snapshot-id", strings.Repeat("d", 32), source.Record.ID)
	if paused.Record.Phase != "paused" || paused.Record.Pause == nil || paused.Record.Pause.After.Anon != 0 {
		t.Fatal("actual pause did not confirm release from its own memory.stat", paused.Record.Pause)
	}
	if err := e.refreshStoppedMemoryAccounting(paused.Record); err != nil {
		t.Fatal("exact bounded worker accounting read failed", err)
	}
	after, err := e.stoppedSandboxMemory(context.Background(), paused.Record)
	if err != nil || after.Anon != 0 {
		t.Fatal("stopped source did not retain actual zero anonymous memory", after, err)
	}
	foreignParent := paused.Record
	foreignParent.CgroupParent.Inode++
	if err := e.refreshStoppedMemoryAccounting(foreignParent); err == nil {
		t.Fatal("accounting refresh adopted another worker cgroup")
	}
	foreignSandbox := paused.Record
	changed := *paused.Record.Cgroup
	changed.Inode++
	foreignSandbox.Cgroup = &changed
	if err := e.refreshStoppedMemoryAccounting(foreignSandbox); err == nil {
		t.Fatal("accounting refresh adopted another sandbox cgroup")
	}
	boundedLossSave(t, "stopped-memory.json", map[string]any{
		"passed": true, "live_source_refused": true, "foreign_worker_refused": true,
		"foreign_sandbox_refused": true, "actual_pause": paused.Record.Pause, "after": after,
	})
}
