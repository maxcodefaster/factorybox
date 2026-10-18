package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestVerifiedParentDigestRejectsContentAndIdentityChanges(t *testing.T) {
	directory := t.TempDir()
	name := "pages-1.img"
	path := filepath.Join(directory, name)
	content := []byte("verified original bytes")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	identity, err := criuParentFileIdentity(path)
	if err != nil {
		t.Fatal(err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(content))
	old := criuVerifiedParentFiles
	t.Cleanup(func() { criuVerifiedParentFiles = old })
	manifest := map[string]criuRAMFile{name: {
		Name: name, SHA256: digest, Bytes: identity.Size,
		UID: identity.UID, GID: identity.GID, Mode: identity.Mode & 07777,
	}}
	if err := criuRegisterVerifiedParentFiles(directory, manifest); err != nil {
		t.Fatal(err)
	}
	if value, hit, err := criuVerifiedParentDigest(path); err != nil || !hit || value != digest {
		t.Fatalf("unchanged verified payload miss: %s %v %v", value, hit, err)
	}
	if _, err := os.ReadFile(path); err != nil {
		t.Fatal(err)
	}
	if _, hit, err := criuVerifiedParentDigest(path); err != nil || !hit {
		t.Fatalf("ordinary read invalidated immutable digest: %v %v", hit, err)
	}
	changed := append([]byte(nil), content...)
	changed[0] ^= 1
	if err := os.WriteFile(path, changed, 0600); err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(identity.Modified.Sec, identity.Modified.Nsec)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if _, hit, err := criuVerifiedParentDigest(path); err != nil || hit {
		t.Fatalf("same-size changed payload with reset mtime reused old digest: %v %v", hit, err)
	}
	replacement := filepath.Join(directory, "replacement")
	if err := os.WriteFile(replacement, content, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	if _, hit, err := criuVerifiedParentDigest(path); err != nil || hit {
		t.Fatalf("replacement inode reused old digest: %v %v", hit, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("replacement", path); err != nil {
		t.Fatal(err)
	}
	if _, hit, err := criuVerifiedParentDigest(path); err == nil || hit {
		t.Fatalf("symlink accepted by verified parent cache: %v %v", hit, err)
	}
}

func TestVerifiedParentDigestSurvivesReadonlyAlias(t *testing.T) {
	directory, alias := t.TempDir(), t.TempDir()
	content := []byte("verified bind payload")
	path := filepath.Join(directory, "inventory.img")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	identity, err := criuParentFileIdentity(path)
	if err != nil {
		t.Fatal(err)
	}
	old := criuVerifiedParentFiles
	t.Cleanup(func() { criuVerifiedParentFiles = old })
	manifest := map[string]criuRAMFile{"inventory.img": {
		Name: "inventory.img", Bytes: identity.Size, SHA256: fmt.Sprintf("%x", sha256.Sum256(content)),
		UID: identity.UID, GID: identity.GID, Mode: identity.Mode & 07777,
	}}
	if err := criuRegisterVerifiedParentFiles(directory, manifest); err != nil {
		t.Fatal(err)
	}
	if err := criuParentReadonlyBind(directory, directory); err != nil {
		t.Fatal(err)
	}
	defer unix.Unmount(directory, 0)
	if err := criuParentReadonlyBind(directory, alias); err != nil {
		t.Fatal(err)
	}
	defer unix.Unmount(alias, 0)
	if _, hit, err := criuVerifiedParentDigest(filepath.Join(alias, "inventory.img")); err != nil || !hit {
		current, _ := criuParentFileIdentity(filepath.Join(alias, "inventory.img"))
		t.Fatalf("readonly bind changed file identity: hit=%v err=%v before=%+v after=%+v", hit, err, identity, current)
	}
}
