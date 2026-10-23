//go:build linux

package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"factorybox/internal/ownedtree"
	"factorybox/internal/storefs"
	"golang.org/x/sys/unix"
)

// EpochPreparationInspection identifies only disposable, unaccepted preparation
// metadata. It never authorizes withdrawal of checkpoint or base payloads.
type EpochPreparationInspection struct {
	Version         int            `json:"version"`
	SourceRoot      string         `json:"source_root"`
	BootID          string         `json:"boot_id"`
	Attachment      storefs.Active `json:"attachment"`
	Inventory       ownedtree.Plan `json:"inventory"`
	InventorySHA256 string         `json:"inventory_sha256"`
	InspectedAt     string         `json:"inspected_at"`
	DirectoryBirth  string         `json:"directory_birth"`
	DirectoryChange string         `json:"directory_change"`
}

type EpochPreparationCancellation struct {
	Version          int    `json:"version"`
	SourceRoot       string `json:"source_root"`
	InspectionSHA256 string `json:"inspection_sha256"`
	CompletedAt      string `json:"completed_at"`
}

func epochPreparationTimes(source *Engine) (birth, change string, resultErr error) {
	anchor, err := source.root.Open(".")
	if err != nil {
		return "", "", err
	}
	defer anchor.Close()
	fd, err := unix.Openat2(int(anchor.Fd()), epochRetirementDirectory, &unix.OpenHow{
		Flags:   unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS})
	if err != nil {
		return "", "", err
	}
	defer func() { resultErr = errors.Join(resultErr, unix.Close(fd)) }()
	var stat unix.Statx_t
	if err := unix.Statx(fd, "", unix.AT_EMPTY_PATH, unix.STATX_BTIME|unix.STATX_CTIME, &stat); err != nil {
		return "", "", err
	}
	if stat.Mask&(unix.STATX_BTIME|unix.STATX_CTIME) != unix.STATX_BTIME|unix.STATX_CTIME || stat.Btime.Sec <= 0 || stat.Ctime.Sec <= 0 {
		return "", "", fmt.Errorf("preparation directory generation is unavailable")
	}
	return fmt.Sprintf("%d.%09d", stat.Btime.Sec, stat.Btime.Nsec), fmt.Sprintf("%d.%09d", stat.Ctime.Sec, stat.Ctime.Nsec), nil
}

func epochPreparationUnaccepted(source, store *Engine) error {
	for _, name := range []string{"intent.json", "complete.json"} {
		if _, err := source.root.Lstat(epochRetirementDirectory + "/" + name); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("accepted or uncertain epoch preparation cannot be cancelled: %s: %v", name, err)
		}
	}
	if _, err := store.root.Lstat(epochRetirementFenceName); !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("accepted or uncertain store retirement cannot be cancelled: %v", err)
	}
	return nil
}

func validateEpochPreparationPlan(plan ownedtree.Plan) error {
	if plan.Version != 1 || len(plan.Paths) != 1 || plan.Paths[0] != epochRetirementDirectory || len(plan.Anchors) != 0 ||
		len(plan.Entries) < 1 || len(plan.Entries) > 8 || plan.Root.Inode == 0 || plan.Root.MountID == 0 ||
		plan.Root.UID != 0 || plan.Root.GID != 0 || plan.Root.Mode != unix.S_IFDIR|0700 {
		return fmt.Errorf("preparation cancellation requires one exact private metadata inventory")
	}
	seen := map[string]bool{}
	temporaries := 0
	for _, entry := range plan.Entries {
		if seen[entry.Path] || entry.Object.Inode == 0 || entry.Object.Device != plan.Root.Device ||
			entry.Object.MountID != plan.Root.MountID || entry.Object.UID != 0 || entry.Object.GID != 0 || entry.Rdev != 0 || entry.Link != "" {
			return fmt.Errorf("preparation cancellation inventory identity is unknown or duplicated")
		}
		seen[entry.Path] = true
		mode, maximum := uint32(unix.S_IFREG|0600), int64(0)
		switch entry.Path {
		case epochRetirementDirectory, epochRetirementDirectory + "/witness":
			mode = unix.S_IFDIR | 0700
		case epochRetirementDirectory + "/retirement-payload.json.gz", epochRetirementDirectory + "/witness/retirement-payload.json.gz":
			maximum = retirementEncodedLimit
		case epochRetirementDirectory + "/.retirement-reserve":
			maximum = retirementReserveBytes
		case epochRetirementDirectory + "/prepare.json":
			maximum = 4 << 20
		default:
			if !strings.HasPrefix(entry.Path, epochRetirementDirectory+"/.checkpoint-") ||
				!tokenID.MatchString(strings.TrimPrefix(entry.Path, epochRetirementDirectory+"/.checkpoint-")) {
				return fmt.Errorf("unknown preparation cancellation metadata: %s", entry.Path)
			}
			temporaries++
			maximum = 4 << 20
		}
		if entry.Object.Mode != mode || entry.Bytes < 0 || entry.Bytes > maximum ||
			mode&unix.S_IFMT == unix.S_IFDIR && (entry.Content != "" || entry.Links != 0) ||
			mode&unix.S_IFMT == unix.S_IFREG && (!digest.MatchString(entry.Content) || entry.Links != 1) {
			return fmt.Errorf("preparation metadata mode, size, content or ownership is unsafe: %s", entry.Path)
		}
	}
	if !seen[epochRetirementDirectory] || temporaries > 1 ||
		seen[epochRetirementDirectory+"/witness/retirement-payload.json.gz"] && !seen[epochRetirementDirectory+"/witness"] {
		return fmt.Errorf("preparation cancellation inventory omits a required parent or repeats publication")
	}
	return nil
}

// InspectEpochPreparation needs no spare store allocation. It can identify an
// incomplete gzip/reserve after ENOSPC without treating those bytes as a valid
// deletion plan. All source payloads and acceptance records remain untouched.
func (e *Engine) InspectEpochPreparation(ctx context.Context, sourceRoot string) (EpochPreparationInspection, error) {
	var report EpochPreparationInspection
	if err := ctx.Err(); err != nil {
		return report, err
	}
	if err := retainedControllerPath(e.config, sourceRoot); err != nil {
		return report, err
	}
	forest, err := e.retirementForest()
	if err != nil {
		return report, err
	}
	defer forest.close()
	source := forest.controllers[sourceRoot]
	if source == nil {
		return report, fmt.Errorf("preparation source epoch is missing")
	}
	store, err := e.epochRetirementStore(source)
	if err != nil {
		return report, err
	}
	defer store.Close()
	if err := epochPreparationUnaccepted(source, store); err != nil {
		return report, err
	}
	if err := e.epochMountDependencies(sourceRoot + "/" + epochRetirementDirectory); err != nil {
		return report, err
	}
	birth, change, err := epochPreparationTimes(source)
	if err != nil {
		return report, err
	}
	plan, err := ownedtree.Capture(ctx, source.root, []string{epochRetirementDirectory})
	if err != nil {
		return report, err
	}
	if err := validateEpochPreparationPlan(plan); err != nil {
		return report, err
	}
	currentBirth, currentChange, err := epochPreparationTimes(source)
	if err != nil || currentBirth != birth || currentChange != change {
		return report, fmt.Errorf("preparation changed during inspection: %v", err)
	}
	data, err := json.Marshal(plan)
	if err != nil {
		return report, err
	}
	return EpochPreparationInspection{Version: 1, SourceRoot: sourceRoot, BootID: e.boot,
		Attachment: *e.config.BoundedStore, Inventory: plan, InventorySHA256: sum(data), InspectedAt: now(),
		DirectoryBirth: birth, DirectoryChange: change}, nil
}

// CancelEpochPreparation requires a complete fresh inspection, including every
// original inode and byte. If interruption removes only part of the stage,
// obtain a new inspection before cancelling its remainder. This deliberately
// grants no missing-entry replay authority to an unaccepted operation.
func (e *Engine) CancelEpochPreparation(ctx context.Context, inspected EpochPreparationInspection) (EpochPreparationCancellation, error) {
	var report EpochPreparationCancellation
	if err := ctx.Err(); err != nil {
		return report, err
	}
	if err := retainedControllerPath(e.config, inspected.SourceRoot); err != nil {
		return report, err
	}
	data, err := json.Marshal(inspected.Inventory)
	if err != nil || inspected.Version != 1 || inspected.BootID != e.boot || inspected.InspectedAt == "" ||
		!digest.MatchString(inspected.InventorySHA256) || sum(data) != inspected.InventorySHA256 {
		return report, fmt.Errorf("preparation inspection does not identify exact saved metadata")
	}
	if err := validateEpochPreparationPlan(inspected.Inventory); err != nil {
		return report, err
	}
	forest, err := e.retirementForest()
	if err != nil {
		return report, err
	}
	defer forest.close()
	source := forest.controllers[inspected.SourceRoot]
	if source == nil {
		return report, fmt.Errorf("preparation source epoch is missing")
	}
	store, err := e.epochRetirementStore(source)
	if err != nil {
		return report, err
	}
	defer store.Close()
	if err := epochPreparationUnaccepted(source, store); err != nil {
		return report, err
	}
	birth, change, err := epochPreparationTimes(source)
	if err != nil || birth != inspected.DirectoryBirth || change != inspected.DirectoryChange {
		return report, fmt.Errorf("preparation directory generation differs; inspect it again: %v", err)
	}
	plan, err := rebindEpochRetirementPlan(inspected.Inventory, inspected.Attachment, *e.config.BoundedStore)
	if err != nil {
		return report, err
	}
	if err := ownedtree.ValidateComplete(ctx, source.root, plan); err != nil {
		return report, err
	}
	if err := e.epochMountDependencies(inspected.SourceRoot + "/" + epochRetirementDirectory); err != nil {
		return report, err
	}
	if err := ownedtree.RemoveRemaining(ctx, source.root, plan); err != nil {
		return report, err
	}
	if _, err := source.root.Lstat(epochRetirementDirectory); !errors.Is(err, os.ErrNotExist) {
		return report, fmt.Errorf("cancelled preparation remains or was replaced: %v", err)
	}
	data, err = json.Marshal(inspected)
	if err != nil {
		return report, err
	}
	return EpochPreparationCancellation{Version: 1, SourceRoot: inspected.SourceRoot, InspectionSHA256: sum(data), CompletedAt: now()}, nil
}
