//go:build linux

package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"factorybox/internal/ownedtree"
	"golang.org/x/sys/unix"
)

func retirementPreparationLimit(name string) (int64, bool) {
	switch name {
	case "retirement-payload.json.gz":
		return retirementEncodedLimit, true
	case ".retirement-reserve":
		return retirementReserveBytes, true
	default:
		if strings.HasPrefix(name, ".checkpoint-") && tokenID.MatchString(strings.TrimPrefix(name, ".checkpoint-")) {
			return checkpointManifestLimit, true
		}
		return 0, false
	}
}

func unacceptedRetirementPreparation(root *os.Root) error {
	for _, name := range []string{"retirement-intent.json", "retirement-complete.json"} {
		if _, err := root.Lstat(name); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("accepted or uncertain retirement preparation must be continued, not reset: %s: %v", name, err)
		}
	}
	return nil
}

// resetUnacceptedRetirementPreparation runs under the lifecycle locks before
// publishing a new intent. ENOSPC or a killed writer can leave an incomplete
// inventory/reserve. These private metadata files grant no payload authority.
// Inspect their exact objects and bytes afresh; never interpret their contents
// as paths to remove, and never reset an accepted deletion inventory.
func (e *Engine) resetUnacceptedRetirementPreparation(ctx context.Context, relative string) error {
	if err := privateDirectory(e.config.Root + "/" + relative); err != nil {
		return err
	}
	root, err := e.root.OpenRoot(relative)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := unacceptedRetirementPreparation(root); err != nil {
		return err
	}
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()
	entries, err := directory.ReadDir(5)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if len(entries) > 3 {
		return fmt.Errorf("unaccepted retirement contains unknown or excessive preparation metadata")
	}
	var paths []string
	temporaries := 0
	for _, entry := range entries {
		limit, known := retirementPreparationLimit(entry.Name())
		info, err := entry.Info()
		if !known || err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > limit {
			return fmt.Errorf("unaccepted retirement contains unsafe preparation metadata: %s: %v", entry.Name(), err)
		}
		if strings.HasPrefix(entry.Name(), ".checkpoint-") {
			temporaries++
		}
		paths = append(paths, entry.Name())
	}
	if temporaries > 1 {
		return fmt.Errorf("unaccepted retirement has more than one interrupted publication")
	}
	if len(paths) == 0 {
		return nil
	}
	plan, err := ownedtree.Capture(ctx, root, paths)
	if err != nil {
		return err
	}
	if plan.Root.Mode != unix.S_IFDIR|0700 || plan.Root.UID != 0 || plan.Root.GID != 0 ||
		len(plan.Anchors) != 0 || len(plan.Entries) != len(paths) {
		return fmt.Errorf("unaccepted retirement preparation is not one private metadata directory")
	}
	for _, entry := range plan.Entries {
		limit, known := retirementPreparationLimit(entry.Path)
		if !known || entry.Object.Mode != unix.S_IFREG|0600 || entry.Object.UID != 0 || entry.Object.GID != 0 ||
			entry.Object.Device != plan.Root.Device || entry.Object.MountID != plan.Root.MountID ||
			entry.Links != 1 || entry.Bytes < 0 || entry.Bytes > limit ||
			!digest.MatchString(entry.Content) || entry.Link != "" || entry.Rdev != 0 {
			return fmt.Errorf("unaccepted retirement metadata identity is unsafe: %s", entry.Path)
		}
	}
	if err := unacceptedRetirementPreparation(root); err != nil {
		return err
	}
	if err := ownedtree.ValidateComplete(ctx, root, plan); err != nil {
		return err
	}
	if err := ownedtree.RemoveRemaining(ctx, root, plan); err != nil {
		return err
	}
	return directory.Sync()
}
