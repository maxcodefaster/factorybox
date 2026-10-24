//go:build linux

package lifecycle

import (
	"context"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"

	"factorybox/internal/ownedtree"
	"golang.org/x/sys/unix"
)

// epochPayloadPlan selects only authorized payloads from the complete witness
// just captured by inventoryEpochPayloads. The witness supplies observed bytes,
// not deletion authority. Independently validate every selected object again,
// including hardlinks outside the selected payloads, before returning a plan.
func epochPayloadPlan(ctx context.Context, root *os.Root, witness ownedtree.Plan, paths []string) (ownedtree.Plan, error) {
	plan := ownedtree.Plan{Version: witness.Version, Root: witness.Root, Paths: append([]string(nil), paths...)}
	if witness.Version != 1 || len(witness.Entries) > ownedtree.MaximumEntries || len(paths) == 0 {
		return plan, fmt.Errorf("epoch payload selection requires a complete witness and payload roots")
	}
	sort.Strings(plan.Paths)
	roots := map[string]bool{}
	parents := map[string]ownedtree.Entry{}
	for _, name := range plan.Paths {
		if name == "." || name == "" || path.Clean(name) != name || path.IsAbs(name) ||
			name == ".." || strings.HasPrefix(name, "../") || strings.ContainsRune(name, 0) || roots[name] {
			return plan, fmt.Errorf("epoch payload roots are invalid or overlap")
		}
		roots[name] = true
	}
	for _, name := range plan.Paths {
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			if roots[parent] {
				return plan, fmt.Errorf("epoch payload roots are invalid or overlap")
			}
			parents[parent] = ownedtree.Entry{}
		}
	}
	parentEntry := func(entry ownedtree.Entry) error {
		previous, needed := parents[entry.Path]
		if !needed {
			return nil
		}
		if previous.Path != "" || entry.Object.MountID != plan.Root.MountID || entry.Object.Mode&unix.S_IFMT != unix.S_IFDIR {
			return fmt.Errorf("epoch payload parent is missing, repeated or outside its root")
		}
		// Anchors identify directories, without granting authority over their
		// retained contents. Keep independent entry values for later rebinds.
		parents[entry.Path] = ownedtree.Entry{Path: entry.Path, Object: entry.Object}
		return nil
	}
	for _, entry := range witness.Anchors {
		if err := parentEntry(entry); err != nil {
			return plan, err
		}
	}
	// Capture's depth-first walk stores each complete subtree contiguously.
	// Save ranges first: sorted selected roots can have a different order from
	// their ancestors' walk (for example "a-b" and "a/child").
	selected := map[string][]ownedtree.Entry{}
	count := 0
	for i := 0; i < len(witness.Entries); {
		entry := witness.Entries[i]
		if err := parentEntry(entry); err != nil {
			return plan, err
		}
		if !roots[entry.Path] {
			i++
			continue
		}
		if _, repeated := selected[entry.Path]; repeated {
			return plan, fmt.Errorf("epoch witness repeats a payload root")
		}
		end := i + 1
		for end < len(witness.Entries) && below(witness.Entries[end].Path, entry.Path) {
			end++
		}
		selected[entry.Path] = witness.Entries[i:end]
		count += end - i
		i = end
	}
	for _, entry := range parents {
		if entry.Path == "" {
			return plan, fmt.Errorf("epoch witness omits a payload parent")
		}
		plan.Anchors = append(plan.Anchors, entry)
	}
	sort.Slice(plan.Anchors, func(i, j int) bool { return plan.Anchors[i].Path < plan.Anchors[j].Path })
	plan.Entries = make([]ownedtree.Entry, 0, count)
	for _, name := range plan.Paths {
		entries, found := selected[name]
		if !found {
			return plan, fmt.Errorf("epoch witness omits a payload root")
		}
		plan.Entries = append(plan.Entries, entries...)
	}
	if err := ownedtree.ValidateComplete(ctx, root, plan); err != nil {
		return plan, err
	}
	return plan, nil
}
