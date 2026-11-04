//go:build linux

package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"factorybox/internal/ownedtree"
	"factorybox/internal/sysboxcapture"
	"golang.org/x/sys/unix"
)

// The caller has accepted this exact payload inventory after confirmed Delete
// and excluded live dependencies. Keep its journal unchanged on continuation.
// Sealed native roots cannot be unlinked file by file. Withdraw their exact
// kernel identities first, then let the original inventory remove the remaining
// plain directories and images. Interrupted withdrawal permits only absence of
// original objects, never a successor at one of their names.
func (e *Engine) retireCaptureNativePayload(ctx context.Context, generation string, plan ownedtree.Plan) error {
	var filesystem sysboxcapture.FilesystemSnapshot
	if err := e.readCheckpointJSON(generation+"/filesystem.json", &filesystem); errors.Is(err, os.ErrNotExist) {
		return nil // An interrupted earlier capture may have no filesystem result.
	} else if err != nil {
		return err
	}
	var native []sysboxcapture.FilesystemTree
	entries := make(map[string]ownedtree.Entry, len(plan.Entries))
	for _, entry := range plan.Entries {
		entries[entry.Path] = entry
	}
	seen := map[string]bool{}
	for _, tree := range filesystem.Roots {
		if tree.Native == nil {
			continue
		}
		expected := e.config.Root + "/" + generation + "/filesystem/" + tree.Name
		if tree.Name == "upper" {
			expected = e.config.Root + "/" + generation + "/filesystem/mutable/upper"
		}
		if filesystem.Version != 2 || filesystem.Writable || !epochFilesystemName(tree.Name) || tree.Saved != expected {
			return fmt.Errorf("failed capture native root escaped its original sealed generation")
		}
		name := strings.TrimPrefix(epochNativeRoot(tree), e.config.Root+"/")
		entry, inventoried := entries[name]
		if !inventoried || seen[name] || entry.Object.Mode&unix.S_IFMT != unix.S_IFDIR {
			return fmt.Errorf("failed capture native root lacks its accepted inventory")
		}
		seen[name] = true
		native = append(native, tree)
	}
	if len(native) == 0 {
		return nil
	}
	if err := ownedtree.ValidateRemaining(ctx, e.root, plan); err != nil {
		return err
	}
	for _, tree := range native {
		if err := sysboxcapture.CheckNativeFilesystemRetirement(ctx, tree); err != nil {
			return err
		}
	}
	for _, tree := range native {
		if err := sysboxcapture.RetireNativeFilesystemTree(ctx, tree); err != nil {
			return err
		}
	}
	for _, tree := range native {
		if err := sysboxcapture.WaitNativeFilesystemRetirement(ctx, tree); err != nil {
			return err
		}
	}
	return nil
}
