//go:build linux

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestHotRewriteRequiresPrivateMetadata(t *testing.T) {
	root := t.TempDir()
	current := filepath.Join(root, "current.img")
	parent := filepath.Join(root, "parent.img")
	data, err := criuEncodeSavedMap(criuSavedMap{PageID: 8,
		Ranges: []criuSavedRange{{Address: 4096, Pages: 2, Flags: 6}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(parent, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(parent, current); err != nil {
		t.Fatal(err)
	}
	hot := []criuHotRange{{4096, 8192}}
	if _, err := criuHotRewriteMap(current, parent, hot, 4096); err == nil {
		t.Fatal("rewrote shared snapshot metadata")
	}
	os.Remove(current)
	if err := os.Symlink(parent, current); err != nil {
		t.Fatal(err)
	}
	if _, err := criuHotRewriteMap(current, parent, hot, 4096); err == nil {
		t.Fatal("followed child snapshot symlink")
	}
	os.Remove(current)
	if err := os.WriteFile(current, data, 0600); err != nil {
		t.Fatal(err)
	}
	pages, err := criuHotRewriteMap(current, parent, hot, 4096)
	if err != nil || pages != 1 {
		t.Fatalf("private rewrite: %d %v", pages, err)
	}
	original, err := os.ReadFile(parent)
	if err != nil || !bytes.Equal(original, data) {
		t.Fatal("modified immutable source")
	}
	changed, _, err := criuHotReadFile(current, 8<<20)
	if err != nil {
		t.Fatal(err)
	}
	got, err := criuReadSavedMap(bytes.NewReader(changed))
	if err != nil || len(got.Ranges) != 2 || got.Ranges[0].Flags != 4 || got.Ranges[1].Flags != 6 {
		t.Fatalf("wrong saved hints: %+v %v", got, err)
	}
}

// The native fixture invokes this test binary on actual captured pagemaps.
// This entry point is absent from production binaries.
func TestNativeHotsetRewrite(t *testing.T) {
	path := os.Getenv("RUNTIME_FORK_TEST_HOTSET_REQUEST")
	if path == "" {
		t.Skip("native fixture only")
	}
	if filepath.Dir(path) != "/experiment" {
		t.Fatal("native helper requires owned experiment request")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var request struct {
		Current  string         `json:"current"`
		Original string         `json:"original"`
		Hot      []criuHotRange `json:"hot"`
		Budget   int64          `json:"budget"`
	}
	if err := json.Unmarshal(data, &request); err != nil {
		t.Fatal(err)
	}
	pages, err := criuHotRewriteMap(request.Current, request.Original, request.Hot, request.Budget)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("NATIVE_HOTSET_RESULT %d\n", pages)
}
