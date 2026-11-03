//go:build linux

package lifecycle

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"factorybox/internal/sandboxnetwork"
	"factorybox/internal/sysboxcapture"
)

func TestRetainedForkMetadataAcrossNetworkNamespaceReplacement(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires an owned root-only metadata fixture")
	}
	sourceOwner, targetOwner := strings.Repeat("1", 32), strings.Repeat("2", 32)
	sourceNetwork, err := sandboxnetwork.Plan(sourceOwner)
	if err != nil {
		t.Fatal(err)
	}
	targetNetwork, err := sandboxnetwork.Plan(targetOwner)
	if err != nil {
		t.Fatal(err)
	}
	// Both declarations belong to one previous namespace, not this worker.
	sourceNetwork.NamespaceInode++
	targetNetwork.NamespaceInode++
	root := t.TempDir()
	handle, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	config := Config{Root: root, ManagerRoot: root + "/manager",
		RuntimeSHA256: strings.Repeat("a", 64), CriuSHA256: strings.Repeat("b", 64)}
	engine := &Engine{config: config, root: handle, boot: "same-node-boot", metadataOnly: true}
	intent := forkIntent{
		Version: 1, TargetID: "fork-target", Owner: targetOwner, BootID: engine.boot,
		RuntimeSHA256: config.RuntimeSHA256, CriuSHA256: config.CriuSHA256,
		ManagerRoot: config.ManagerRoot, OCI: []byte("{}"), State: []byte("{}"), FsState: []byte("{}"),
		TargetNetwork: targetNetwork,
		Source: Record{ID: "fork-source", Owner: sourceOwner, RuntimeID: "fbx-" + sourceOwner,
			ConfigSHA256: strings.Repeat("c", 64), BootID: engine.boot, Network: sourceNetwork},
	}
	intent.CriuConfig = []byte("nested-ns\nveth-pair " + sourceNetwork.Guest + "=" + sourceNetwork.Peer +
		"\nexternal netdev[" + sourceNetwork.Guest + "]:fbg" + sourceOwner[:12] + "\n")
	intent.NetworkCopy, _ = json.Marshal(sysboxcapture.NetworkCopyEvidence{
		Version: 1, SourcePID: 42, SourceStart: "original-task-start", Address: sourceNetwork.Address,
		Namespaces: 1, Allowed: true,
	})
	intent.Checkpoint = Checkpoint{
		Version: 1, ID: strings.Repeat("d", 32), SourceID: intent.Source.ID,
		Owner: sourceOwner, RuntimeID: intent.Source.RuntimeID, ConfigSHA256: intent.Source.ConfigSHA256,
		BootID: engine.boot, RuntimeSHA256: config.RuntimeSHA256, CriuSHA256: config.CriuSHA256,
		ManagerRoot: config.ManagerRoot, ReleasedAt: "original-release", SourcePID: 42,
	}
	for name, blob := range map[string][]byte{
		"source-state.json": intent.State, "sysbox-state.json": intent.FsState,
		"criu.conf": intent.CriuConfig, "images/" + sysboxcapture.NetworkCopyImage: intent.NetworkCopy,
	} {
		intent.Checkpoint.Files = append(intent.Checkpoint.Files,
			CheckpointFile{Name: name, Bytes: int64(len(blob)), SHA256: sum(blob)})
	}
	checkpoint, _ := json.Marshal(intent.Checkpoint)
	intent.Origin = ForkOrigin{SourceID: intent.Source.ID, CheckpointID: intent.Checkpoint.ID,
		CheckpointSHA256: sum(checkpoint), RequestedAt: "original-fork"}
	operation := "forks/" + intent.TargetID
	if err := os.MkdirAll(root+"/"+operation, 0700); err != nil {
		t.Fatal(err)
	}
	path := root + "/" + operation + "/intent.json"
	write := func(value forkIntent) {
		t.Helper()
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	read := func() error {
		_, err := engine.readForkIntent(operation, intent.Source.ID, intent.Checkpoint.ID, intent.TargetID)
		return err
	}
	write(intent)
	before, _ := os.ReadFile(path)
	if err := read(); err != nil {
		t.Fatal("metadata-only dependency inventory required the previous live network namespace", err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("metadata inspection rewrote the original fork declaration")
	}
	engine.metadataOnly = false
	if err := read(); err == nil {
		t.Fatal("historical metadata authorized live network execution")
	}
	engine.metadataOnly = true
	for name, change := range map[string]func(*forkIntent){
		"target-owner":      func(v *forkIntent) { v.TargetNetwork.Owner = sourceOwner },
		"target-subnet":     func(v *forkIntent) { v.TargetNetwork.CIDR = v.Source.Network.CIDR },
		"target-peer":       func(v *forkIntent) { v.TargetNetwork.Peer = "foreign" },
		"namespace":         func(v *forkIntent) { v.TargetNetwork.NamespaceInode++ },
		"missing-namespace": func(v *forkIntent) { v.TargetNetwork.NamespaceInode = 0 },
		"mtu":               func(v *forkIntent) { v.TargetNetwork.MTU++ },
		"invalid-mtu":       func(v *forkIntent) { v.Source.Network.MTU = 1; v.TargetNetwork.MTU = 1 },
		"mapping":           func(v *forkIntent) { v.CriuConfig = []byte("foreign mapping") },
		"capture-proof":     func(v *forkIntent) { v.NetworkCopy = []byte("{}") },
	} {
		t.Run(name, func(t *testing.T) {
			data, _ := json.Marshal(intent)
			var changed forkIntent
			if err := json.Unmarshal(data, &changed); err != nil {
				t.Fatal(err)
			}
			change(&changed)
			write(changed)
			if err := read(); err == nil {
				t.Fatal("metadata inventory accepted changed fork/network ownership")
			}
		})
	}
}
