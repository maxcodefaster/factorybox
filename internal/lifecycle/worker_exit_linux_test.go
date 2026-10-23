//go:build linux

package lifecycle

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestWorkerExitRecoverySelection(t *testing.T) {
	config := Config{Root: "/store/old", RuntimeSHA256: strings.Repeat("a", 64),
		CriuSHA256: strings.Repeat("b", 64), ManagerRoot: "/store/mgr"}
	source := Record{Version: 1, ID: "source", Owner: strings.Repeat("c", 32),
		RuntimeID: "fbx-" + strings.Repeat("c", 32), RuntimeSHA256: config.RuntimeSHA256,
		BootID: "boot", Phase: "paused", RuntimeCreated: "created", Start: Operation{CompletedAt: "started"},
		Pause: &PauseOperation{SnapshotID: strings.Repeat("d", 32), CheckpointSHA256: strings.Repeat("e", 64),
			RequestedAt: "requested", CompletedAt: "complete", SourcePID: 123}}
	request := RecoveryRequest{SourceRoot: config.Root, SourceID: source.ID, SourceOwner: source.Owner,
		SnapshotID: source.Pause.SnapshotID, CheckpointSHA256: source.Pause.CheckpointSHA256}
	original := workerBaseRelease{Version: 1, Root: config.Root, BootID: "boot", RuntimeSHA256: config.RuntimeSHA256,
		CriuSHA256: config.CriuSHA256, ManagerRoot: config.ManagerRoot, RequestedAt: "exit-request", Paused: []Record{source}}
	if err := original.recoverySelection(config, "boot", request, source); err != nil {
		t.Fatal("complete selected pause rejected during interrupted drain", err)
	}
	for name, alter := range map[string]func(*workerBaseRelease){
		"version":           func(i *workerBaseRelease) { i.Version++ },
		"root":              func(i *workerBaseRelease) { i.Root += "/other" },
		"boot":              func(i *workerBaseRelease) { i.BootID += "other" },
		"runtime":           func(i *workerBaseRelease) { i.RuntimeSHA256 = strings.Repeat("f", 64) },
		"criu":              func(i *workerBaseRelease) { i.CriuSHA256 = strings.Repeat("f", 64) },
		"manager":           func(i *workerBaseRelease) { i.ManagerRoot += "/other" },
		"intent":            func(i *workerBaseRelease) { i.RequestedAt = "" },
		"completed-intent":  func(i *workerBaseRelease) { i.CompletedAt = "unexpected" },
		"missing":           func(i *workerBaseRelease) { i.Paused = nil },
		"duplicate":         func(i *workerBaseRelease) { i.Paused = append(i.Paused, i.Paused[0]) },
		"owner":             func(i *workerBaseRelease) { i.Paused[0].Owner = strings.Repeat("f", 32) },
		"snapshot":          func(i *workerBaseRelease) { i.Paused[0].Pause.SnapshotID = strings.Repeat("f", 32) },
		"checkpoint":        func(i *workerBaseRelease) { i.Paused[0].Pause.CheckpointSHA256 = strings.Repeat("f", 64) },
		"changed-record":    func(i *workerBaseRelease) { i.Paused[0].InputSHA256 = strings.Repeat("f", 64) },
		"uncommitted":       func(i *workerBaseRelease) { i.Paused[0].Pause.CompletedAt = "" },
		"rejected":          func(i *workerBaseRelease) { i.Paused[0].Pause.RejectedAt = "rejected" },
		"failed":            func(i *workerBaseRelease) { i.Paused[0].Pause.Error = "failed" },
		"resuming":          func(i *workerBaseRelease) { i.Paused[0].Pause.Resume = &Operation{} },
		"anonymous-memory":  func(i *workerBaseRelease) { i.Paused[0].Pause.After.Anon = 1 },
		"checkpoint-memory": func(i *workerBaseRelease) { i.Paused[0].Pause.Snapshot.ResidentBytesAfter = 1 },
		"deleting":          func(i *workerBaseRelease) { i.Paused[0].Delete = &Operation{} },
		"phase":             func(i *workerBaseRelease) { i.Paused[0].Phase = "pausing" },
		"pid":               func(i *workerBaseRelease) { i.Paused[0].Pause.SourcePID = 0 },
		"runtime-created":   func(i *workerBaseRelease) { i.Paused[0].RuntimeCreated = "" },
		"start":             func(i *workerBaseRelease) { i.Paused[0].Start.CompletedAt = "" },
	} {
		t.Run(name, func(t *testing.T) {
			data, err := json.Marshal(original)
			if err != nil {
				t.Fatal(err)
			}
			var changed workerBaseRelease
			if err := json.Unmarshal(data, &changed); err != nil {
				t.Fatal(err)
			}
			alter(&changed)
			if err := changed.recoverySelection(config, "boot", request, source); err == nil {
				t.Fatal("unconfirmed or changed recovery selection accepted")
			}
		})
	}
}
