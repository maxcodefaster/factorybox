//go:build linux

package lifecycle

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestNativeBasePublicationRecovery(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_RETAINED_CAPTURE") != "1" {
		t.Skip("requires an owned native filesystem and private mount namespace")
	}
	for _, scenario := range []string{"complete-copy", "already-sealed", "truncated-copy", "shared-inode", "changed-intent"} {
		t.Run(scenario, func(t *testing.T) {
			e, _ := recordMetadataEngine(t)
			if err := os.Chmod(e.config.Root, 0700); err != nil {
				t.Fatal(err)
			}
			e.boot = "original-worker-boot"
			e.config.RuntimeSHA256 = strings.Repeat("a", 64)
			for _, name := range []string{"runtime", "forks", "bases", "input"} {
				if err := e.root.Mkdir(name, 0700); err != nil {
					t.Fatal(err)
				}
			}
			source := e.config.Root + "/input"
			payload := []byte("complete independent image backing\n")
			if err := os.WriteFile(source+"/payload", payload, 0600); err != nil {
				t.Fatal(err)
			}
			original, err := e.PrepareBase(context.Background(), "original", source)
			if err != nil {
				t.Fatal(err)
			}
			destination := original.Tree.Saved
			t.Cleanup(func() {
				if err := unix.Unmount(destination, 0); err != nil && !errors.Is(err, unix.EINVAL) {
					t.Error("unmount only this fixture's base", err)
				}
			})
			if scenario != "already-sealed" {
				if err := unix.Unmount(destination, 0); err != nil {
					t.Fatal(err)
				}
			}
			// Simulate lost publication after the complete independent copy,
			// without reusing any production directory or runtime identity.
			if err := e.root.Remove("bases/original/base.json"); err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "truncated-copy":
				if err := os.WriteFile(destination+"/payload", []byte("partial"), 0600); err != nil {
					t.Fatal(err)
				}
			case "shared-inode":
				if err := os.Remove(destination + "/payload"); err != nil {
					t.Fatal(err)
				}
				if err := os.Link(source+"/payload", destination+"/payload"); err != nil {
					t.Fatal(err)
				}
			case "changed-intent":
				e.boot = "another-worker-boot"
			}
			before, err := os.Stat(destination + "/payload")
			if err != nil {
				t.Fatal(err)
			}
			intent, err := os.ReadFile(e.config.Root + "/bases/original/intent.json")
			if err != nil {
				t.Fatal(err)
			}
			result, err := e.PrepareBase(context.Background(), "original", source)
			valid := scenario == "complete-copy" || scenario == "already-sealed"
			if valid {
				if err != nil || result.PreparedAt == "" || result.Tree.Immutable == nil {
					t.Fatalf("complete unpublished copy must be verified and published without copying again: %v", err)
				}
				replay, replayErr := e.PrepareBase(context.Background(), "original", source)
				if replayErr != nil || replay.PreparedAt != result.PreparedAt ||
					replay.Tree.Immutable.MountID != result.Tree.Immutable.MountID {
					t.Fatal("confirmed publication was not idempotent", replayErr)
				}
			} else {
				if err == nil {
					t.Fatal("incomplete, aliased or foreign import was accepted")
				}
				if _, err := e.root.Lstat("bases/original/base.json"); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("refused import published completion", err)
				}
			}
			after, statErr := os.Stat(destination + "/payload")
			savedIntent, readErr := os.ReadFile(e.config.Root + "/bases/original/intent.json")
			if statErr != nil || readErr != nil || !os.SameFile(before, after) || !bytes.Equal(intent, savedIntent) {
				t.Fatal("recovery replaced backing or rewrote its original intent", statErr, readErr)
			}
			data, err := os.ReadFile(destination + "/payload")
			expected := payload
			if scenario == "truncated-copy" {
				expected = []byte("partial")
			}
			if err != nil || !bytes.Equal(data, expected) {
				t.Fatal("recovery rewrote imported payload", err)
			}
		})
	}
}
