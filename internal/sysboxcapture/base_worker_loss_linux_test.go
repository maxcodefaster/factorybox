//go:build linux && factorybox_worker_probe

package sysboxcapture

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"factorybox/internal/platform"
	"golang.org/x/sys/unix"
)

// The fixture-only build tag adds this two-process orchestration entry point.
// It changes no runtime code or platform admission.
func TestFilesystemBaseWorkerLossNative(t *testing.T) {
	owner := os.Getenv("FACTORYBOX_BASE_WORKER_OWNER")
	if len(owner) != 32 {
		t.Fatal("requires an explicitly owned cross-worker fixture")
	}
	info, err := platform.Current()
	if err != nil || info.CheckExecution() != nil {
		t.Fatal("requires the ordinary qualified execution tuple", info, err)
	}
	const home = "/var/lib/sysbox/cold-base"
	type proof struct {
		Version int            `json:"version"`
		Owner   string         `json:"owner"`
		Tree    FilesystemTree `json:"tree"`
	}
	ctx := context.Background()
	switch os.Getenv("FACTORYBOX_BASE_WORKER_PHASE") {
	case "prepare":
		for _, name := range []string{"input", "old", "new"} {
			if err := os.MkdirAll(home+"/"+name, 0700); err != nil {
				t.Fatal(err)
			}
		}
		payload := bytes.Repeat([]byte("complete persistent worker backing\n"), 65536)
		if err := os.WriteFile(home+"/input/payload", payload, 0640); err != nil {
			t.Fatal(err)
		}
		if err := unix.Setxattr(home+"/input/payload", "user.factorybox", []byte("worker-loss-metadata"), 0); err != nil {
			t.Fatal(err)
		}
		tree, err := PrepareFilesystemBase(ctx, home+"/input", home+"/old/rootfs")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.RemoveAll(home + "/input"); err != nil {
			t.Fatal(err)
		}
		file, err := os.OpenFile(home+"/proof.json", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.NewEncoder(file).Encode(proof{Version: 1, Owner: owner, Tree: tree}); err != nil {
			t.Fatal(err)
		}
		if err := file.Sync(); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		parent, err := os.Open(home)
		if err != nil {
			t.Fatal(err)
		}
		if err := parent.Sync(); err != nil {
			t.Fatal(err)
		}
		parent.Close()
		fmt.Println("FACTORYBOX_BASE_COMMITTED=" + owner)
		t.Log("complete base and proof durable; original input removed")
		if os.Getenv("FACTORYBOX_BASE_WORKER_ABRUPT") == "1" {
			select {} // The owner kills and removes this entire container.
		}
	case "recover":
		data, err := os.ReadFile(home + "/proof.json")
		var saved proof
		if err != nil || json.Unmarshal(data, &saved) != nil || saved.Version != 1 || saved.Owner != owner ||
			saved.Tree.Immutable == nil || saved.Tree.Immutable.Root != home+"/old/rootfs" {
			t.Fatal("persistent base proof belongs to another fixture", err)
		}
		if err := VerifyImmutableTree(saved.Tree.Saved, saved.Tree); err == nil {
			t.Fatal("fresh worker accepted the previous worker's lost immutable mount")
		}
		recovered, err := RecoverFilesystemBase(ctx, saved.Tree, home+"/new/rootfs")
		if err != nil || !sameFilesystemTree(recovered, saved.Tree) {
			t.Fatal("fresh worker did not reimport complete persistent base", err)
		}
		if err := os.RemoveAll(saved.Tree.Immutable.Root); err != nil {
			t.Fatal(err)
		}
		if err := VerifyImmutableTree(recovered.Saved, recovered); err != nil {
			t.Fatal("fresh base depended on old canonical backing", err)
		}
		t.Log("new worker independently verified/copied base; old worker, mount, source and raw backing unavailable")
	default:
		t.Fatal("requires prepare or recover phase")
	}
}
