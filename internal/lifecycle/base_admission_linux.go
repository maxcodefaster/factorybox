//go:build linux

package lifecycle

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"factorybox/internal/sysboxcapture"
)

// Canonical bases may consume at most half the exit record's encoded bound.
// This is not a reservation for other work: checkpoint aliases, recovered
// bases and paused records still need their own admission. The complete exit
// preflight remains authoritative before any fence or unmount.
const baseExitMetadataLimit = checkpointManifestLimit / 2

// Account for the exact paths and maximum JSON widths of every field an import
// can publish. No source is copied or mounted to obtain this bound. The negative
// int64 extreme also covers the minus sign of saved filesystem timestamps.
func (e *Engine) baseImportBound(id, source string) (int, error) {
	root := e.config.Root + "/bases/" + id + "/rootfs"
	hash := strings.Repeat("f", 64)
	tree := sysboxcapture.FilesystemTree{
		Name: "lower", Source: source, Saved: root, SHA256: hash,
		Entries: math.MinInt, FileBytes: math.MinInt64, HashedBytes: math.MinInt64,
		CaptureUS: math.MinInt64, SourceReadUS: math.MinInt64, CopyUS: math.MinInt64, TargetReadUS: math.MinInt64,
		Immutable: &sysboxcapture.ImmutableTree{
			Root: root, Device: math.MaxUint64, Inode: math.MaxUint64, MountID: math.MaxUint64,
			Mode: math.MaxUint32, UID: math.MaxUint32, GID: math.MaxUint32,
			MtimeSec: math.MinInt64, MtimeNano: math.MinInt64, CtimeSec: math.MinInt64, CtimeNano: math.MinInt64,
			SHA256: hash, Entries: math.MinInt, FileBytes: math.MinInt64, HashedBytes: math.MinInt64,
		},
	}
	base := Base{Version: 1, ID: id, Source: source, BootID: e.boot,
		RuntimeSHA256: e.config.RuntimeSHA256, PreparedAt: "9999-12-31T23:59:59.999999999Z", Tree: tree}
	encoded, err := json.Marshal(base)
	return len(encoded) + 1, err // Include the separating comma in the exit array.
}

// Hold the controller operation lock. Complete immutable replays bypass this
// admission; failed imports and changed records are never silently discounted.
func (e *Engine) admitBaseImport(id, source string) error {
	if e.config.BoundedStore == nil {
		return nil
	}
	prospective, err := e.baseImportBound(id, source)
	if err != nil {
		return err
	}
	entries, err := e.exitEntries("bases")
	if err != nil {
		return fmt.Errorf("new base imports refused: inventory unavailable: %w", err)
	}
	used := 0
	for _, entry := range entries {
		base, err := e.readBase(entry.Name())
		if err != nil {
			return fmt.Errorf("new base imports refused: existing import is unconfirmed: %w", err)
		}
		encoded, err := json.Marshal(base)
		if err != nil {
			return err
		}
		used += len(encoded) + 1
		if used > baseExitMetadataLimit {
			break
		}
	}
	if prospective > baseExitMetadataLimit-used {
		return fmt.Errorf("new base imports refused: canonical exit metadata=%d bytes, new import bound=%d, budget=%d; retain existing bases and drain to a new worker epoch",
			used, prospective, baseExitMetadataLimit)
	}
	return nil
}
