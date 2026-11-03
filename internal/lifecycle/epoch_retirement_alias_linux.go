//go:build linux

package lifecycle

import (
	"errors"
	"fmt"
	"os"
	"reflect"

	"factorybox/internal/sysboxcapture"
)

// New captures and materializations retain the pre-mount alias identity outside
// the restore image set. Match it to the published lower proof before admitting
// it as metadata. Historical copies without this optional writer receipt keep
// their existing independently verified lower/alias requirements.
func (b *epochInventoryBuilder) captureAlias(generation string, snapshot *sysboxcapture.FilesystemSnapshot) error {
	var intent sysboxcapture.FilesystemTree
	name := generation + "/" + sysboxcapture.CaptureLowerAliasIntent
	if err := b.engine.readCheckpointJSON(name, &intent); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	for _, lower := range snapshot.Roots {
		if lower.Name != "lower" || lower.Immutable == nil {
			continue
		}
		if intent.Name != lower.Name || intent.Source != lower.Source || intent.Saved != lower.Saved ||
			intent.Native != nil || intent.Alias == nil || !reflect.DeepEqual(intent.Alias, lower.Alias) ||
			!reflect.DeepEqual(intent.Immutable, lower.Immutable) || intent.SHA256 != lower.SHA256 ||
			intent.Entries != lower.Entries || intent.FileBytes != lower.FileBytes || intent.HashedBytes != lower.HashedBytes {
			return fmt.Errorf("epoch lower-alias writer receipt differs from its published filesystem")
		}
		b.file(name)
		return nil
	}
	return fmt.Errorf("epoch lower-alias writer receipt has no published immutable lower")
}
