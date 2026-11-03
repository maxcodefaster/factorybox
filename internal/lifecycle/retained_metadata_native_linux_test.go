//go:build linux

package lifecycle

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"factorybox/internal/storefs"
)

func TestNativeRetainedMetadataAcrossRuntimeUpgrade(t *testing.T) {
	storage := os.Getenv("FACTORYBOX_NATIVE_RETAINED_METADATA_ROOT")
	if storage == "" {
		t.Skip("requires separately owned native fixed-store fixture")
	}
	root, err := os.MkdirTemp(storage, "metadata-upgrade-")
	if err != nil {
		t.Fatal(err)
	}
	var owner [16]byte
	if _, err := rand.Read(owner[:]); err != nil {
		t.Fatal(err)
	}
	formatter, err := filepath.EvalSymlinks("/usr/sbin/mkfs.btrfs")
	if err != nil {
		t.Fatal(err)
	}
	hashFile := func(path string) string {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		return hex.EncodeToString(sum[:])
	}
	image, err := storefs.Create(context.Background(), root+"/store.img", 512<<20,
		hex.EncodeToString(owner[:]), storefs.FormatTool{
			Path: formatter, SHA256: hashFile(formatter), Schema: 2,
		})
	if err != nil {
		t.Fatal(err)
	}
	target := root + "/data"
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	attachment, err := storefs.Attach(image, target)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := attachment.Close(); err != nil {
			t.Error(err)
		}
	})
	func() {
		store := target + "/store"
		currentID, historicalID := strings.Repeat("a", 32), strings.Repeat("b", 32)
		currentRoot := store + "/epochs/" + currentID + "/controller"
		historicalRoot := store + "/epochs/" + historicalID + "/controller"
		for _, path := range []string{currentRoot, historicalRoot, store + "/manager"} {
			if err := os.MkdirAll(path, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path+"/.lock", nil, 0600); err != nil {
				t.Fatal(err)
			}
		}
		for _, controller := range []string{currentRoot, historicalRoot} {
			for _, name := range []string{"objects", "forks", "bases", "runtime"} {
				if err := os.Mkdir(controller+"/"+name, 0700); err != nil {
					t.Fatal(err)
				}
			}
		}
		boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
		if err != nil {
			t.Fatal(err)
		}
		config := Config{
			Root: currentRoot, ManagerRoot: store + "/manager",
			Runtime: "/opt/factorybox/components/sysbox-runc",
			Criu:    "/opt/factorybox/components/criu", BoundedStore: &attachment.Active,
		}
		config.RuntimeSHA256, config.CriuSHA256 = hashFile(config.Runtime), hashFile(config.Criu)
		historical := config
		historical.Root = historicalRoot
		// Synthetic, explicitly owned epoch metadata reproduces the observed
		// historical Sysbox digest. No old execution or worker loss is claimed.
		historical.RuntimeSHA256 = "f2dff633157c3e447bd78fd804b202f28ec98ef813b3151c333e46cc3a605dfb"
		if historical.RuntimeSHA256 == config.RuntimeSHA256 {
			t.Fatal("fixture did not exercise a runtime recipe upgrade")
		}
		header, err := json.Marshal(map[string]any{
			"schema": 1, "store": store, "store_id": image.Owner,
			"epoch": historicalID, "boot_id": strings.TrimSpace(string(boot)),
			"lifecycle": historical,
		})
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Dir(historicalRoot) + "/worker.json"
		if err := os.WriteFile(path, header, 0600); err != nil {
			t.Fatal(err)
		}
		original := hashFile(path)
		handle, err := os.OpenRoot(currentRoot)
		if err != nil {
			t.Fatal(err)
		}
		engine := &Engine{config: config, boot: strings.TrimSpace(string(boot)), root: handle}
		defer engine.Close()
		forest, err := engine.deletedBackingForest()
		if err != nil {
			t.Fatal("native backing cleanup could not inventory the historical runtime recipe", err)
		}
		defer forest.close()
		old := forest.controllers[historicalRoot]
		if old == nil || !old.metadataOnly || old.config.RuntimeSHA256 != historical.RuntimeSHA256 {
			t.Fatal("dependency inventory did not retain the historical metadata-only recipe")
		}
		if _, err := old.lock(); err == nil || !strings.Contains(err.Error(), "metadata handle") {
			t.Fatal("historical dependency reader acquired lifecycle authority")
		}
		if _, err := old.commandFiles(context.Background(), nil, nil, "state", "old-peer"); err == nil ||
			!strings.Contains(err.Error(), "metadata handle") {
			t.Fatal("historical dependency reader executed a runtime command")
		}
		if old, err := engine.openRetainedController(historicalRoot); err == nil {
			old.Close()
			t.Fatal("metadata-only compatibility authorized historical execution")
		}
		if hashFile(path) != original {
			t.Fatal("dependency inventory changed historical epoch metadata")
		}
		if err := storefs.Verify(attachment.Active); err != nil {
			t.Fatal("dependency inventory changed the current fixed attachment", err)
		}
	}()
	if err := attachment.Close(); err != nil {
		t.Fatal(err)
	}
	if err := storefs.VerifyReleased(image); err != nil {
		t.Fatal("exact metadata fixture kernel attachment did not release", err)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal("remove exactly released owned metadata fixture", err)
	}
}
