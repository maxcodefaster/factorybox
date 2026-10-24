//go:build linux

package ownedtree

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestInventoryHashesIndependentCompleteFileContents(t *testing.T) {
	root, _ := fixture(t)
	if err := root.Mkdir("hashes", 0700); err != nil {
		t.Fatal(err)
	}
	large := make([]byte, 128<<10+13)
	for i := range large {
		large[i] = byte(i*17 + 3)
	}
	files := map[string][]byte{"hashes/a-large": large, "hashes/b-empty": {}, "hashes/c-short": []byte("independent short content\n")}
	expected := map[string]string{}
	for name, content := range files {
		if err := root.WriteFile(name, content, 0600); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(content)
		expected[name] = hex.EncodeToString(digest[:])
	}
	plan, err := Capture(context.Background(), root, []string{"hashes"})
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, entry := range plan.Entries {
		if digest, found := expected[entry.Path]; found {
			if entry.Content != digest || entry.Bytes != int64(len(files[entry.Path])) {
				t.Fatal("inventory did not hash each complete independent file", entry.Path, entry.Content)
			}
			seen++
		}
	}
	if seen != len(files) {
		t.Fatal("inventory omitted regular file content", seen)
	}
	if err := ValidateComplete(context.Background(), root, plan); err != nil {
		t.Fatal("independent content cannot be verified again", err)
	}
}
