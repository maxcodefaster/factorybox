//go:build linux

package sysboxcapture

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
)

func TestSharedSourceAdmissionAndReplacement(t *testing.T) {
	root := t.TempDir()
	source, err := SharedSourcePaths(map[string]string{SharedSourceAnnotation: `{"source":"` + root + `","destination":"/home/coder/project"}`})
	if err != nil {
		t.Fatal(err)
	}
	pinned, err := IdentifySharedSource(*source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root+"/new-file", []byte("accepted editor change"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := VerifySharedSource(pinned); err != nil {
		t.Fatal("a content change must not replace the source identity", err)
	}
	if err := os.Rename(root, root+"-original"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root + "-original") })
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := VerifySharedSource(pinned); err == nil {
		t.Fatal("adopted a replacement project directory")
	}
}

func TestSharedSourcePathsRejectMalformedAndManagedDestinations(t *testing.T) {
	for _, encoded := range []string{
		`null`, `{}`, `{"source":"/source","destination":"/workspace","extra":true}`,
		`{"source":"/source","destination":"/workspace"} {}`,
		`{"source":"/source/../other","destination":"/workspace"}`,
		`{"source":"/source","destination":"/var"}`,
		`{"source":"/source","destination":"/proc/project"}`,
		`{"source":"/source","destination":"/"}`,
	} {
		if _, err := SharedSourcePaths(map[string]string{SharedSourceAnnotation: encoded}); err == nil {
			t.Fatalf("admitted malformed shared source: %s", encoded)
		}
	}
	root := t.TempDir()
	if err := os.Symlink(root, root+"-link"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(root + "-link") })
	if _, err := IdentifySharedSource(SharedSource{Source: root + "-link"}); err == nil {
		t.Fatal("followed a shared source symlink")
	}
}

func TestSharedSourceCannotBecomeIndependentBacking(t *testing.T) {
	snapshot := &FilesystemSnapshot{SharedSource: &SharedSource{Source: "/source", Destination: "/project"}}
	if _, err := MaterializeFilesystemSnapshot(context.Background(), "/generation", "/new", snapshot); !errors.Is(err, ErrSharedSourceCopy) {
		t.Fatal("materialization did not refuse shared source before writing", err)
	}
	if _, err := RecoverFilesystemSnapshot(context.Background(), "/generation", "/new", snapshot); !errors.Is(err, ErrSharedSourceCopy) {
		t.Fatal("recovery did not refuse shared source before writing", err)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var restored FilesystemSnapshot
	if err := json.Unmarshal(encoded, &restored); err != nil || *restored.SharedSource != *snapshot.SharedSource {
		t.Fatal("snapshot lost its external-source contract", err)
	}
}
