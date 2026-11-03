//go:build linux

package lifecycle

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"factorybox/internal/storefs"
)

func TestNativeRetainedManagerRootWithdrawal(t *testing.T) {
	storage := os.Getenv("FACTORYBOX_NATIVE_RETAINED_METADATA_ROOT")
	if storage == "" {
		t.Skip("requires separately owned native fixed-store fixture")
	}
	root, err := os.MkdirTemp(storage, "manager-root-withdrawal-")
	if err != nil {
		t.Fatal(err)
	}
	hashFile := func(path string) string {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return sum(data)
	}
	var owner [16]byte
	if _, err := rand.Read(owner[:]); err != nil {
		t.Fatal(err)
	}
	id := hex.EncodeToString(owner[:])
	formatter, err := filepath.EvalSymlinks("/usr/sbin/mkfs.btrfs")
	if err != nil {
		t.Fatal(err)
	}
	image, err := storefs.Create(context.Background(), root+"/store.img", 512<<20, id,
		storefs.FormatTool{Path: formatter, SHA256: hashFile(formatter), Schema: 2})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root+"/data", 0700); err != nil {
		t.Fatal(err)
	}
	attachment, err := storefs.Attach(image, root+"/data")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := closeNativeRetirementStore(t, attachment); err != nil {
			t.Error(err)
		}
	})
	store := root + "/data/store"
	currentRoot := store + "/epochs/" + strings.Repeat("a", 32) + "/controller"
	oldRoot := store + "/epochs/" + strings.Repeat("b", 32) + "/controller"
	for _, path := range []string{currentRoot, oldRoot} {
		for _, name := range []string{"objects", "forks", "bases", "runtime"} {
			if err := os.MkdirAll(path+"/"+name, 0700); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(path+"/.lock", nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		t.Fatal(err)
	}
	config := Config{Root: currentRoot, ManagerRoot: store + "/manager",
		Runtime: "/opt/factorybox/components/sysbox-runc", Criu: "/opt/factorybox/components/criu",
		MinFreeBytes: 1 << 20, WriteReserve: 1 << 20, BoundedStore: &attachment.Active}
	config.RuntimeSHA256, config.CriuSHA256 = hashFile(config.Runtime), hashFile(config.Criu)
	old := config
	old.Root, old.RuntimeSHA256 = oldRoot, strings.Repeat("d", 64)
	header, _ := json.Marshal(map[string]any{
		"schema": 1, "store": store, "store_id": id, "epoch": strings.Repeat("b", 32),
		"boot_id": strings.TrimSpace(string(boot)), "lifecycle": old,
	})
	headerPath := filepath.Dir(oldRoot) + "/worker.json"
	if err := os.WriteFile(headerPath, header, 0600); err != nil {
		t.Fatal(err)
	}
	request := SandboxBackingRequest{"deleted-root-source", id, strings.Repeat("e", 64)}
	bundle := store + "/input/" + request.SandboxID + "/bundle"
	for _, name := range []string{"lower", "upper", "work", "rootfs"} {
		if err := os.MkdirAll(bundle+"/"+name, 0700); err != nil {
			t.Fatal(err)
		}
	}
	private := bundle + "/upper/dependency"
	if err := os.WriteFile(private, []byte("original private data"), 0600); err != nil {
		t.Fatal(err)
	}
	managerPayload := config.ManagerRoot + "/docker/fbx-" + id
	if err := os.MkdirAll(managerPayload, 0700); err != nil {
		t.Fatal(err)
	}
	marker := managerPayload + "/manager-data"
	if err := os.WriteFile(marker, []byte("original manager data"), 0600); err != nil {
		t.Fatal(err)
	}
	input, err := directoryIdentity(bundle)
	if err != nil {
		t.Fatal(err)
	}
	record := Record{Version: 1, ID: request.SandboxID, Owner: id, RuntimeID: "fbx-" + id,
		RuntimeSHA256: old.RuntimeSHA256, BootID: strings.TrimSpace(string(boot)),
		Phase: "deleted", InputBundle: bundle, InputSHA256: request.InputSHA256,
		InputDirectory: &input, Rootfs: bundle + "/rootfs", ConfigSHA256: sum([]byte("{}")),
		Start:  Operation{Attempted: true, CompletedAt: "synthetic-start"},
		Delete: &Operation{Attempted: true, CompletedAt: "synthetic-delete"}}
	object := oldRoot + "/objects/" + record.ID
	if err := os.Mkdir(object, 0700); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(record)
	for name, contents := range map[string][]byte{"record.json": data, "config.json": []byte("{}")} {
		if err := os.WriteFile(object+"/"+name, contents, 0600); err != nil {
			t.Fatal(err)
		}
	}
	recordHash, headerHash := hashFile(object+"/record.json"), hashFile(headerPath)
	run := func(ctx context.Context) (SandboxBackingRetirement, error) {
		handle, err := os.OpenRoot(currentRoot)
		if err != nil {
			t.Fatal(err)
		}
		engine := &Engine{config: config, root: handle, boot: record.BootID}
		defer engine.Close()
		return engine.RetireRetainedDeletedBacking(ctx, oldRoot, request)
	}
	// Accept the original manager inventory and interrupt its actual unlinking.
	if _, err := run(cancelResumeRemoval{Context: context.Background(), files: []string{marker}}); !errors.Is(err, context.Canceled) {
		t.Fatal("original manager unlink was not interrupted", err)
	}
	journal := root + "/manager-retirements/" + id + "-" + request.InputSHA256
	intentHash := hashFile(journal + "/retirement-intent.json")
	moved := store + "/still-linked-original-manager"
	if err := os.Rename(config.ManagerRoot, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(managerPayload, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, []byte("unrelated replacement manager data"), 0600); err != nil {
		t.Fatal(err)
	}
	replacementHash := hashFile(marker)
	if _, err := run(context.Background()); err == nil {
		t.Fatal("a moved original manager directory was treated as withdrawn")
	}
	if hashFile(marker) != replacementHash || hashFile(private) != sum([]byte("original private data")) {
		t.Fatal("refusal changed original or replacement data")
	}
	// Model the pinned manager's removal of its old root, not mere renaming.
	if err := os.RemoveAll(moved); err != nil {
		t.Fatal(err)
	}
	result, err := run(context.Background())
	if err != nil {
		t.Fatal("kernel-proven withdrawal of the original manager root did not finish its exact retained request", err)
	}
	if result.PayloadKind != "sandbox-backing" || result.Request != request || result.CompletedAt == "" {
		t.Fatal("original private backing has no exact completion")
	}
	if hashFile(marker) != replacementHash {
		t.Fatal("retained cleanup deleted or changed the replacement manager")
	}
	if _, err := os.Lstat(private); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("original private backing remained after confirmed manager withdrawal", err)
	}
	var receipt map[string]any
	bytes, err := os.ReadFile(journal + "/retirement-complete.json")
	if err != nil || json.Unmarshal(bytes, &receipt) != nil || receipt["directory_withdrawal"] == nil {
		t.Fatal("manager completion lost its independent original-directory withdrawal proof", err)
	}
	replayed, err := run(context.Background())
	if err != nil || !reflect.DeepEqual(replayed, result) ||
		hashFile(marker) != replacementHash || hashFile(journal+"/retirement-intent.json") != intentHash ||
		hashFile(object+"/record.json") != recordHash || hashFile(headerPath) != headerHash {
		t.Fatal("retained replay changed original evidence or successor data", err)
	}
	if err := closeNativeRetirementStore(t, attachment); err != nil {
		t.Fatal(err)
	}
	if err := storefs.VerifyReleased(image); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
}
