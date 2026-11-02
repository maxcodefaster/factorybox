//go:build linux

package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"factorybox/internal/storefs"
	"factorybox/internal/sysboxcapture"
)

func TestRetainedBaseRetirementRejectsUnboundRequests(t *testing.T) {
	engine := &Engine{}
	for _, request := range []RetainedBaseRequest{
		{},
		{"/foreign", "../base", strings.Repeat("a", 64)},
		{"/foreign", "base", "unknown"},
		{"/foreign", "base", strings.Repeat("a", 64)},
	} {
		if _, err := engine.RetireRetainedBase(context.Background(), request); err == nil {
			t.Fatal("retirement admitted an unbound identity", request)
		}
	}
}

func TestRetainedBaseRetirementProtectsCheckpointAndUncertainCapture(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-owned metadata; run in the native Linux lane")
	}
	for _, mode := range []string{"checkpoint", "checkpoint-redirection", "partial-target", "partial-unrelated", "empty-failure", "changed-record"} {
		t.Run(mode, func(t *testing.T) {
			engine, record := managerDependencyFixture(t, "source", strings.Repeat("a", 32), "running", "/unrelated")
			engine.metadataOnly = true
			engine.config.BoundedStore = &storefs.Active{}
			target := "/private/epochs/base/rootfs"
			record.Base = &sysboxcapture.FilesystemTree{Immutable: &sysboxcapture.ImmutableTree{Root: target}}
			if mode == "partial-unrelated" {
				record.Base.Immutable.Root = "/another/epochs/base/rootfs"
			}
			if err := engine.write(record); err != nil {
				t.Fatal(err)
			}
			generation := "objects/source/captures/" + strings.Repeat("b", 32)
			if err := os.MkdirAll(filepath.Join(engine.config.Root, generation), 0700); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "checkpoint", "checkpoint-redirection":
				saved := Checkpoint{Filesystem: &sysboxcapture.FilesystemSnapshot{
					Roots: []sysboxcapture.FilesystemTree{{Saved: "/private/capture/lower",
						Immutable: &sysboxcapture.ImmutableTree{Root: target}}},
				}}
				if mode == "checkpoint-redirection" {
					saved.Filesystem.Roots[0].Immutable.Root = "/changed/checkpoint/base"
				}
				if err := engine.publishCheckpoint(generation, "checkpoint.json", saved); err != nil {
					t.Fatal(err)
				}
			case "partial-target", "partial-unrelated":
				if err := engine.root.Mkdir(generation+"/images", 0700); err != nil {
					t.Fatal(err)
				}
			case "changed-record":
				if err := os.WriteFile(filepath.Join(engine.config.Root, "objects/source/config.json"), []byte("{}"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.ReadFile(filepath.Join(engine.config.Root, "objects/source/record.json"))
			if err != nil {
				t.Fatal(err)
			}
			err = engine.baseCheckpointDependencies(context.Background(), target)
			allowed := mode == "partial-unrelated" || mode == "empty-failure"
			if (err == nil) != allowed {
				t.Fatalf("dependency admission mode=%s allowed=%v error=%v", mode, allowed, err)
			}
			after, _ := os.ReadFile(filepath.Join(engine.config.Root, "objects/source/record.json"))
			if string(before) != string(after) {
				t.Fatal("dependency observation changed original history")
			}
		})
	}
}

func TestRetainedBaseRetirementDoesNotWithdrawDeletedCheckpoint(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-owned metadata; run in the native Linux lane")
	}
	engine, record := managerDependencyFixture(t, "source", strings.Repeat("a", 32), "deleted", "/unrelated")
	engine.config.BoundedStore = &storefs.Active{}
	target := "/private/epochs/base/rootfs"
	generation := "objects/source/captures/" + strings.Repeat("b", 32)
	if err := os.MkdirAll(filepath.Join(engine.config.Root, generation), 0700); err != nil {
		t.Fatal(err)
	}
	saved := Checkpoint{Filesystem: &sysboxcapture.FilesystemSnapshot{
		Roots: []sysboxcapture.FilesystemTree{{Saved: "/private/capture/lower",
			Immutable: &sysboxcapture.ImmutableTree{Root: target}}},
	}}
	if err := engine.publishCheckpoint(generation, "checkpoint.json", saved); err != nil {
		t.Fatal(err)
	}
	original, _ := json.Marshal(record)
	if err := engine.baseCheckpointDependencies(context.Background(), target); err == nil {
		t.Fatal("Delete alone withdrew retained checkpoint backing")
	}
	current, err := engine.read(record.ID)
	after, _ := json.Marshal(current)
	if err != nil || string(original) != string(after) {
		t.Fatal("retirement observation changed deleted history", err)
	}
}

func TestRetainedBaseAllowsUnrelatedRunningSourceWithoutCaptures(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-owned metadata; run in the native Linux lane")
	}
	for _, matching := range []bool{false, true} {
		t.Run(fmt.Sprint("matching-base-", matching), func(t *testing.T) {
			engine, record := managerDependencyFixture(t, "source", strings.Repeat("a", 32), "running", "/unrelated")
			engine.metadataOnly = true
			engine.config.BoundedStore = &storefs.Active{}
			target := "/private/epochs/base/rootfs"
			root := "/another/epochs/base/rootfs"
			if matching {
				root = target
			}
			record.Base = &sysboxcapture.FilesystemTree{Immutable: &sysboxcapture.ImmutableTree{Root: root}}
			if err := engine.write(record); err != nil {
				t.Fatal(err)
			}
			forest := &epochRetirementForest{controllers: map[string]*Engine{engine.config.Root: engine}}
			base := Base{Tree: sysboxcapture.FilesystemTree{Saved: target}}
			err := engine.retainedBaseDependencies(context.Background(), forest, base)
			if (err == nil) == matching {
				t.Fatalf("never-captured source matching=%v error=%v", matching, err)
			}
			if _, err := engine.root.Lstat("objects/source/captures"); !os.IsNotExist(err) {
				t.Fatal("dependency observation manufactured checkpoint metadata", err)
			}
		})
	}
}
