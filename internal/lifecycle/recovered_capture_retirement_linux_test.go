//go:build linux

package lifecycle

import (
	"context"
	"os"
	"strings"
	"testing"

	"factorybox/internal/sysboxcapture"
)

func TestRecoveredCaptureRetirementProofPreservesOriginalMetadata(t *testing.T) {
	engine, record, relative := abortedExitCaptureFixture(t)
	before, err := engine.root.ReadFile("objects/" + record.ID + "/record.json")
	if err != nil {
		t.Fatal(err)
	}
	original, err := engine.recoveredCaptureProofSHA(record, record.PauseHistory[0].SnapshotID, relative)
	if err != nil || !digest.MatchString(original) {
		t.Fatal("proved original rollback has no exact retirement witness", original, err)
	}
	for _, path := range []string{"images/partial.img", "filesystem/upper/partial-copy"} {
		if err := engine.root.WriteFile(relative+"/"+path, []byte("private disposable backing"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	// Payload changes do not become proof changes. The accepted owned-tree
	// inventory, not this compact rollback witness, binds the bytes to unlink.
	again, err := engine.recoveredCaptureProofSHA(record, record.PauseHistory[0].SnapshotID, relative)
	if err != nil || again != original {
		t.Fatal("rollback witness depends on mutable payload bytes", again, err)
	}
	if err := engine.root.WriteFile(relative+"/"+sysboxcapture.CaptureLowerAliasIntent, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	withAlias, err := engine.recoveredCaptureProofSHA(record, record.PauseHistory[0].SnapshotID, relative)
	if err != nil || withAlias == original {
		t.Fatal("new alias evidence was omitted from the retirement witness", err)
	}
	after, err := engine.root.ReadFile("objects/" + record.ID + "/record.json")
	if err != nil || string(before) != string(after) {
		t.Fatal("retirement proof changed original lifecycle metadata", err)
	}
}

func TestRecoveredCaptureRetirementProofRejectsChangedOrUnclosedSources(t *testing.T) {
	for _, fault := range []string{"delete", "phase", "history", "source-state", "source-symlink", "completion", "checkpoint"} {
		t.Run(fault, func(t *testing.T) {
			engine, record, relative := abortedExitCaptureFixture(t)
			snapshot := record.PauseHistory[0].SnapshotID
			switch fault {
			case "delete":
				record.Delete.CompletedAt = ""
			case "phase":
				record.Phase, record.Delete = "running", nil
			case "history":
				record.PauseHistory[0].Error = "different"
			case "source-state":
				if err := engine.root.WriteFile(relative+"/source-state.json", []byte("changed"), 0600); err != nil {
					t.Fatal(err)
				}
			case "source-symlink":
				name := relative + "/source-state.json"
				if err := engine.root.Rename(name, name+".original"); err != nil {
					t.Fatal(err)
				}
				if err := engine.root.Symlink("source-state.json.original", name); err != nil {
					t.Fatal(err)
				}
			case "completion":
				if err := engine.root.Remove(relative + "/guard/capture-process-complete.json"); err != nil {
					t.Fatal(err)
				}
			case "checkpoint":
				if err := engine.root.WriteFile(relative+"/checkpoint.json", []byte("{}"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := engine.recoveredCaptureProofSHA(record, snapshot, relative); err == nil {
				t.Fatal("retirement accepted an unproved or changed rollback")
			}
		})
	}
}

func TestRecoveredCaptureBackingRequiresExplicitConfirmedDelete(t *testing.T) {
	engine, record, _ := abortedExitCaptureFixture(t)
	record.InputSHA256 = strings.Repeat("a", 64)
	record.InputDirectory = &KernelObject{Device: 1, Inode: 2, MountID: 3}
	request := SandboxBackingRequest{record.ID, record.Owner, record.InputSHA256}
	record.Phase, record.Delete = "running", nil
	if err := engine.retireRecoveredCaptureBacking(context.Background(), record, request, nil); err == nil {
		t.Fatal("recovered process state granted retirement without native Delete")
	}
	if _, err := os.Stat(engine.config.Root + "/objects/" + record.ID); err != nil {
		t.Fatal("refusal changed the original object", err)
	}
}
