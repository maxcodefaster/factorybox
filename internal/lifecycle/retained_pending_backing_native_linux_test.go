//go:build linux

package lifecycle

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"factorybox/internal/storefs"
)

func TestNativeRetainedPendingBackingRetirement(t *testing.T) {
	for _, native := range []bool{false, true} {
		name := "flat"
		if native {
			name = "native-mutable"
		}
		t.Run(name, func(t *testing.T) {
			nativeRetainedPendingBackingRetirement(t, native)
		})
	}
}

func nativeRetainedPendingBackingRetirement(t *testing.T, native bool) {
	storage := os.Getenv("FACTORYBOX_NATIVE_RETAINED_METADATA_ROOT")
	if storage == "" {
		t.Skip("requires separately owned native fixed-store fixture")
	}
	var ctx context.Context = context.Background()
	root, err := os.MkdirTemp(storage, "pending-retirement-")
	if err != nil {
		t.Fatal(err)
	}
	var owner [16]byte
	if _, err := rand.Read(owner[:]); err != nil {
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
	formatter, err := filepath.EvalSymlinks("/usr/sbin/mkfs.btrfs")
	if err != nil {
		t.Fatal(err)
	}
	image, err := storefs.Create(ctx, root+"/store.img", 512<<20,
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
	store := target + "/store"
	currentRoot := store + "/epochs/" + strings.Repeat("a", 32) + "/controller"
	historicalRoot := store + "/epochs/" + strings.Repeat("b", 32) + "/controller"
	for _, controller := range []string{currentRoot, historicalRoot} {
		for _, name := range []string{"objects", "forks", "bases", "runtime"} {
			if err := os.MkdirAll(controller+"/"+name, 0700); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(controller+"/.lock", nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		t.Fatal(err)
	}
	config := Config{
		Root: currentRoot, ManagerRoot: store + "/manager",
		Runtime: "/opt/factorybox/components/sysbox-runc", MinFreeBytes: 1 << 20, WriteReserve: 1 << 20,
		Criu: "/opt/factorybox/components/criu", BoundedStore: &attachment.Active,
	}
	config.RuntimeSHA256, config.CriuSHA256 = hashFile(config.Runtime), hashFile(config.Criu)
	previous := attachment.Active
	historical := config
	historical.Root, historical.BoundedStore = historicalRoot, &previous
	historical.RuntimeSHA256 = strings.Repeat("d", 64)
	header, _ := json.Marshal(map[string]any{
		"schema": 1, "store": store, "store_id": image.Owner,
		"epoch": strings.Repeat("b", 32), "boot_id": strings.TrimSpace(string(boot)),
		"lifecycle": historical,
	})
	epochPath := filepath.Dir(historicalRoot) + "/worker.json"
	if err := os.WriteFile(epochPath, header, 0600); err != nil {
		t.Fatal(err)
	}
	request := SandboxBackingRequest{"deleted-source", strings.Repeat("c", 32), strings.Repeat("e", 64)}
	bundle := store + "/input/" + request.SandboxID + "/bundle"
	for _, name := range []string{"lower", "rootfs"} {
		if err := os.MkdirAll(bundle+"/"+name, 0700); err != nil {
			t.Fatal(err)
		}
	}
	private := bundle
	if native {
		private = bundle + "/mutable"
		if output, err := exec.Command("btrfs", "subvolume", "create", private).CombinedOutput(); err != nil {
			t.Fatal("create owned mutable subvolume", string(output), err)
		}
	}
	for _, name := range []string{"upper", "work"} {
		if err := os.Mkdir(private+"/"+name, 0700); err != nil {
			t.Fatal(err)
		}
	}
	payload := private + "/upper/retained-project-dependency"
	if err := os.WriteFile(payload, []byte("owned synthetic private backing"), 0600); err != nil {
		t.Fatal(err)
	}
	managerPayload := store + "/manager/docker/fbx-" + request.SandboxOwner
	if err := os.MkdirAll(managerPayload, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(managerPayload+"/private-docker-data", []byte("owned"), 0600); err != nil {
		t.Fatal(err)
	}
	input, err := directoryIdentity(bundle)
	if err != nil {
		t.Fatal(err)
	}
	record := Record{
		Version: 1, ID: request.SandboxID, Owner: request.SandboxOwner,
		RuntimeID: "fbx-" + request.SandboxOwner, RuntimeSHA256: historical.RuntimeSHA256,
		BootID: strings.TrimSpace(string(boot)), Phase: "deleted",
		InputBundle: bundle, InputSHA256: request.InputSHA256, InputDirectory: &input,
		Rootfs: bundle + "/rootfs", ConfigSHA256: sum([]byte("{}")),
		Start:  Operation{Attempted: true, CompletedAt: "synthetic-start"},
		Delete: &Operation{Attempted: true, CompletedAt: "synthetic-delete"},
	}
	object := historicalRoot + "/objects/" + record.ID
	if err := os.Mkdir(object, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(object+"/config.json", []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	recordData, _ := json.Marshal(record)
	if err := os.WriteFile(object+"/record.json", recordData, 0600); err != nil {
		t.Fatal(err)
	}
	originalRecord, originalEpoch := hashFile(object+"/record.json"), hashFile(epochPath)
	// Remount the exact image before retiring: this is metadata recovery, not
	// a claim that the synthetic lifecycle above actually ran Sysbox or CRIU.
	remount := func() {
		t.Helper()
		if err := attachment.Close(); err != nil {
			t.Fatal(err)
		}
		var err error
		attachment, err = storefs.Attach(image, target)
		if err != nil {
			t.Fatal(err)
		}
		config.BoundedStore = &attachment.Active
	}
	remount()
	run := func(request SandboxBackingRequest) (SandboxBackingRetirement, error) {
		handle, err := os.OpenRoot(currentRoot)
		if err != nil {
			t.Fatal(err)
		}
		engine := &Engine{config: config, boot: record.BootID, root: handle}
		defer engine.Close()
		return engine.RetireRetainedDeletedBacking(ctx, historicalRoot, request)
	}
	foreign := request
	foreign.SandboxOwner = strings.Repeat("f", 32)
	if _, err := run(foreign); err == nil {
		t.Fatal("foreign owner authorized retained backing retirement")
	}
	if _, err := os.Stat(payload); err != nil {
		t.Fatal("refusal changed private backing", err)
	}
	for name, change := range map[string]func(*Record){
		"paused": func(r *Record) { r.Phase = "paused" },
		"unconfirmed-delete": func(r *Record) {
			r.Delete = &Operation{Attempted: true}
		},
		"failed-delete": func(r *Record) {
			r.Delete = &Operation{Attempted: true, CompletedAt: "synthetic-delete", Error: "failed"}
		},
		"replaced-bundle": func(r *Record) {
			changed := *r.InputDirectory
			changed.Inode++
			r.InputDirectory = &changed
		},
	} {
		t.Run(name, func(t *testing.T) {
			changed := record
			change(&changed)
			data, _ := json.Marshal(changed)
			if err := os.WriteFile(object+"/record.json", data, 0600); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := os.WriteFile(object+"/record.json", recordData, 0600); err != nil {
					t.Fatal(err)
				}
			}()
			if _, err := run(request); err == nil {
				t.Fatal("retained cleanup admitted unresolved or replaced execution")
			}
			for _, path := range []string{payload, managerPayload} {
				if _, err := os.Stat(path); err != nil {
					t.Fatal("refused cleanup changed retained data", err)
				}
			}
		})
	}
	if t.Failed() {
		t.FailNow()
	}
	runtimePath := historicalRoot + "/runtime/" + record.RuntimeID
	if err := os.Mkdir(runtimePath, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := run(request); err == nil {
		t.Fatal("retained runtime metadata did not fence cleanup")
	}
	if err := os.Remove(runtimePath); err != nil {
		t.Fatal(err)
	}
	peerPath := currentRoot + "/objects/independent-peer"
	if err := os.Mkdir(peerPath, 0700); err != nil {
		t.Fatal(err)
	}
	peerConfig, _ := json.Marshal(map[string]any{
		"mounts": []map[string]string{{"source": managerPayload}},
	})
	peer := record
	peer.ID, peer.Owner = "independent-peer", strings.Repeat("f", 32)
	peer.RuntimeID, peer.RuntimeSHA256 = "fbx-"+peer.Owner, config.RuntimeSHA256
	peer.Phase, peer.Delete, peer.ConfigSHA256 = "starting", nil, sum(peerConfig)
	peerBytes, _ := json.Marshal(peer)
	if err := os.WriteFile(peerPath+"/record.json", peerBytes, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(peerPath+"/config.json", peerConfig, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := run(request); err == nil {
		t.Fatal("another epoch's private manager dependency did not fence cleanup")
	}
	for _, path := range []string{payload, managerPayload} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal("protected manager cleanup changed retained data", err)
		}
	}
	if err := os.RemoveAll(peerPath); err != nil {
		t.Fatal(err)
	}
	// Interrupt actual manager unlinking, then change the attachment again.
	// The accepted inventory and its original stamp must survive unchanged.
	ctx = cancelResumeRemoval{Context: context.Background(), files: []string{managerPayload + "/private-docker-data"}}
	if _, err := run(request); !errors.Is(err, context.Canceled) {
		t.Fatal("owned manager retirement did not stop after partial unlink", err)
	}
	managerIntent := root + "/manager-retirements/" + record.Owner + "-" + record.InputSHA256 + "/retirement-intent.json"
	originalManagerIntent := hashFile(managerIntent)
	ctx = context.Background()
	remount()
	result, err := run(request)
	if err != nil {
		t.Fatal("confirmed deleted backing could not finish after worker replacement without an original retirement receipt", err)
	}
	if result.PayloadKind != "sandbox-backing" || result.Request != request || result.CompletedAt == "" {
		t.Fatal("retained retirement has no exact completion", result)
	}
	for _, path := range []string{payload, managerPayload, private + "/upper", private + "/work", bundle + "/lower"} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatal("confirmed deleted private payload remains", path, err)
		}
	}
	receipt := object + "/backing-retirement/retirement-complete.json"
	originalReceipt := hashFile(receipt)
	remount()
	replayed, err := run(request)
	if err != nil || !reflect.DeepEqual(replayed, result) {
		t.Fatal("completed retained retirement did not survive another attachment", err)
	}
	if hashFile(receipt) != originalReceipt || hashFile(object+"/record.json") != originalRecord ||
		hashFile(epochPath) != originalEpoch || hashFile(managerIntent) != originalManagerIntent {
		t.Fatal("retirement rewrote immutable lifecycle evidence")
	}
	if err := attachment.Close(); err != nil {
		t.Fatal(err)
	}
	if err := storefs.VerifyReleased(image); err != nil {
		t.Fatal("owned retirement fixture retained a kernel attachment", err)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal("remove exactly released owned fixture", err)
	}
}
