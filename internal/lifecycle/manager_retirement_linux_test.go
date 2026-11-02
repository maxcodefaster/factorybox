//go:build linux

package lifecycle

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func managerDependencyFixture(t *testing.T, id, owner, phase, bind string) (*Engine, Record) {
	t.Helper()
	directory := t.TempDir()
	for _, name := range []string{"objects/" + id, "forks", "bases"} {
		if err := os.MkdirAll(filepath.Join(directory, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	engine := &Engine{root: root, boot: "boot", config: Config{
		Root: directory, RuntimeSHA256: strings.Repeat("c", 64), ManagerRoot: "/private/manager",
	}}
	config, _ := json.Marshal(map[string]any{"mounts": []map[string]string{{"source": bind}}})
	record := Record{Version: 1, ID: id, Owner: owner, RuntimeID: "fbx-" + owner, BootID: engine.boot,
		RuntimeSHA256: engine.config.RuntimeSHA256, ConfigSHA256: sum(config), Phase: phase,
		InputBundle: "/private/bundle/" + id, Rootfs: "/private/bundle/" + id + "/rootfs"}
	if phase == "deleted" {
		record.Delete = &Operation{Attempted: true, CompletedAt: "deleted"}
	}
	if err := os.WriteFile(filepath.Join(directory, "objects", id, "config.json"), config, 0600); err != nil {
		t.Fatal(err)
	}
	if err := engine.write(record); err != nil {
		t.Fatal(err)
	}
	return engine, record
}

func TestManagerBackingChecksOtherEpochOwnersAndBindings(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root ownership; exercised in the native worker lane")
	}
	for _, mode := range []string{"independent", "same-owner", "live-bind", "unresolved-bind",
		"retained-pausing-independent", "retained-pausing-bind", "current-pausing-independent",
		"retained-pausing-no-start", "retained-pausing-unattempted", "retained-pausing-no-intent",
		"retained-pausing-bad-snapshot", "retained-pausing-unrequested", "retained-pausing-completed",
		"retained-pausing-resuming", "retained-pausing-deleting", "retained-pausing-relative",
		"retained-pausing-changed-root"} {
		t.Run(mode, func(t *testing.T) {
			engine, record := managerDependencyFixture(t, "source", strings.Repeat("a", 32), "deleted", "")
			owner, phase, bind := strings.Repeat("b", 32), "running", "/unrelated"
			switch mode {
			case "same-owner":
				owner, phase = record.Owner, "deleted"
			case "live-bind":
				bind = engine.config.ManagerRoot + "/docker/" + record.RuntimeID
			case "unresolved-bind":
				phase, bind = "starting", engine.config.ManagerRoot+"/docker/"+record.RuntimeID
			case "retained-pausing-independent", "current-pausing-independent":
				phase = "pausing"
			case "retained-pausing-bind":
				phase, bind = "pausing", engine.config.ManagerRoot+"/docker/"+record.RuntimeID
			}
			if strings.Contains(mode, "pausing") {
				phase = "pausing"
			}
			other, peer := managerDependencyFixture(t, "peer", owner, phase, bind)
			if phase == "pausing" {
				other.metadataOnly = strings.HasPrefix(mode, "retained-")
				peer.Start = Operation{Attempted: true, CompletedAt: "started"}
				peer.Pause = &PauseOperation{SnapshotID: strings.Repeat("c", 32), RequestedAt: "pause-requested"}
				switch mode {
				case "retained-pausing-no-start":
					peer.Start.CompletedAt = ""
				case "retained-pausing-unattempted":
					peer.Start.Attempted = false
				case "retained-pausing-no-intent":
					peer.Pause = nil
				case "retained-pausing-bad-snapshot":
					peer.Pause.SnapshotID = "unknown"
				case "retained-pausing-unrequested":
					peer.Pause.RequestedAt = ""
				case "retained-pausing-completed":
					peer.Pause.CompletedAt = "completed"
				case "retained-pausing-resuming":
					peer.Pause.Resume = &Operation{Attempted: true}
				case "retained-pausing-deleting":
					peer.Delete = &Operation{Attempted: true}
				case "retained-pausing-relative":
					peer.InputBundle = "relative"
				case "retained-pausing-changed-root":
					peer.Rootfs += "-other"
				}
				if err := other.write(peer); err != nil {
					t.Fatal(err)
				}
			}
			forest := &epochRetirementForest{controllers: map[string]*Engine{
				engine.config.Root: engine, other.config.Root: other,
			}}
			err := engine.managerBackingDependencies(context.Background(), forest, record, []string{"docker/" + record.RuntimeID})
			if (err == nil) != (mode == "independent" || mode == "retained-pausing-independent") {
				t.Fatal("retained epoch dependency result is wrong", err)
			}
		})
	}
}

func TestManagerBackingPathsAreBoundToOneNativeOwner(t *testing.T) {
	directory := t.TempDir()
	record := Record{Owner: strings.Repeat("a", 32), RuntimeID: "fbx-" + strings.Repeat("a", 32)}
	peer := "fbx-" + strings.Repeat("b", 32)
	for _, path := range []string{"docker/" + record.RuntimeID, "docker/" + peer, "kubelet/" + record.RuntimeID, "foreign"} {
		if err := os.MkdirAll(filepath.Join(directory, path), 0700); err != nil {
			t.Fatal(err)
		}
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	paths, err := managerBackingPayloads(root, record)
	if err != nil || !reflect.DeepEqual(paths, []string{"docker/" + record.RuntimeID, "kubelet/" + record.RuntimeID}) {
		t.Fatal("manager selection crossed native ownership", paths, err)
	}
	record.RuntimeID = peer
	if _, err := managerBackingPayloads(root, record); err == nil {
		t.Fatal("different native runtime accepted")
	}
}

func TestManagerBackingRejectsSymlinkKindsAndPayloads(t *testing.T) {
	for _, scope := range []string{"kind", "payload"} {
		t.Run(scope, func(t *testing.T) {
			directory := t.TempDir()
			record := Record{Owner: strings.Repeat("a", 32), RuntimeID: "fbx-" + strings.Repeat("a", 32)}
			if err := os.Mkdir(filepath.Join(directory, "foreign"), 0700); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(directory, "docker")
			if scope == "payload" {
				if err := os.Mkdir(link, 0700); err != nil {
					t.Fatal(err)
				}
				link = filepath.Join(link, record.RuntimeID)
			}
			if err := os.Symlink(filepath.Join(directory, "foreign"), link); err != nil {
				t.Fatal(err)
			}
			root, err := os.OpenRoot(directory)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			if _, err := managerBackingPayloads(root, record); err == nil {
				t.Fatal("foreign manager alias accepted")
			}
		})
	}
}

func TestManagerBackingEmptySelectionDoesNotSelectPeers(t *testing.T) {
	directory := t.TempDir()
	if err := os.MkdirAll(filepath.Join(directory, "docker", "fbx-"+strings.Repeat("b", 32)), 0700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	record := Record{Owner: strings.Repeat("a", 32), RuntimeID: "fbx-" + strings.Repeat("a", 32)}
	paths, err := managerBackingPayloads(root, record)
	if err != nil || len(paths) != 0 {
		t.Fatal("empty manager selection adopted another Sandbox", paths, err)
	}
}

func TestManagerBackingPreservesSysboxTraversalMode(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root ownership; exercised in the native worker lane")
	}
	directory := t.TempDir()
	if err := os.Chmod(directory, 0710); err != nil {
		t.Fatal(err)
	}
	if err := protectedManagerRoot(directory); err != nil {
		t.Fatal("original Sysbox traversal mode was refused", err)
	}
	if err := os.Chmod(directory, 0770); err != nil {
		t.Fatal(err)
	}
	if err := protectedManagerRoot(directory); err == nil {
		t.Fatal("group-writable manager root was accepted")
	}
}

func TestManagerBackingRejectsForeignOrWritableRoots(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root ownership; exercised in the native worker lane")
	}
	for _, scope := range []string{"kind", "runtime"} {
		for _, change := range []string{"owner", "writable"} {
			t.Run(scope+"/"+change, func(t *testing.T) {
				directory := t.TempDir()
				record := Record{Owner: strings.Repeat("a", 32), RuntimeID: "fbx-" + strings.Repeat("a", 32)}
				kind := directory + "/docker"
				payload := kind + "/" + record.RuntimeID
				if err := os.MkdirAll(payload, 0710); err != nil {
					t.Fatal(err)
				}
				root, err := os.OpenRoot(directory)
				if err != nil {
					t.Fatal(err)
				}
				defer root.Close()
				if _, err := verifiedManagerBackingPayloads(root, record); err != nil {
					t.Fatal("original Sysbox roots refused", err)
				}
				changed := payload
				if scope == "kind" {
					changed = kind
				}
				if change == "owner" {
					err = os.Chown(changed, 1000, 1000)
				} else {
					err = os.Chmod(changed, 0770)
				}
				if err != nil {
					t.Fatal(err)
				}
				if _, err := verifiedManagerBackingPayloads(root, record); err == nil {
					t.Fatal("foreign/writable manager root was accepted")
				}
			})
		}
	}
}
