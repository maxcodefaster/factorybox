//go:build linux

package lifecycle

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"factorybox/internal/storefs"
	"factorybox/internal/sysboxcapture"
	"golang.org/x/sys/unix"
)

// The persistent worker envelope also carries ephemeral process/peer fields.
// Read only its immutable storage/recipe identity, never adopt those processes.
type recoveryStoreEpoch struct {
	Schema    int
	Store     string
	StoreID   string
	Epoch     string
	BootID    string
	Lifecycle Config
}

func validateRecoveryStoreEpoch(config Config, boot string, source Record, epoch recoveryStoreEpoch) (storefs.Active, error) {
	current, old := config.BoundedStore, epoch.Lifecycle.BoundedStore
	if current == nil || old == nil || source.Base == nil || source.Base.Immutable == nil {
		return storefs.Active{}, fmt.Errorf("device translation requires complete bounded source identities")
	}
	store := filepath.Dir(config.ManagerRoot)
	if epoch.Schema != 1 || !tokenID.MatchString(epoch.Epoch) || epoch.Store != store ||
		epoch.StoreID != current.Image.Owner || epoch.BootID != boot || source.BootID != boot ||
		config.Root != store+"/epochs/"+epoch.Epoch+"/controller" || epoch.Lifecycle.Root != config.Root ||
		epoch.Lifecycle.ManagerRoot != config.ManagerRoot || epoch.Lifecycle.Runtime != config.Runtime ||
		epoch.Lifecycle.RuntimeSHA256 != config.RuntimeSHA256 || source.RuntimeSHA256 != config.RuntimeSHA256 ||
		epoch.Lifecycle.Criu != config.Criu || epoch.Lifecycle.CriuSHA256 != config.CriuSHA256 ||
		old.Image != current.Image || old.Target != current.Target || !old.DirectIO ||
		old.FilesystemMax != current.FilesystemMax || old.LoopNumber < 0 ||
		old.BlockDevice != uint64(unix.Mkdev(7, uint32(old.LoopNumber))) ||
		old.Mount.Device != old.BlockDevice || old.Mount.MountID == 0 || old.Mount.Inode == 0 ||
		source.Base.Immutable.Device != old.BlockDevice || !withinStore(config.Root, source.Base.Immutable.Root) {
		return storefs.Active{}, fmt.Errorf("bounded recovery source epoch or retained image identity changed")
	}
	return *old, nil
}

func (e *Engine) recoveryBaseVerifier(source Record, saved *sysboxcapture.FilesystemSnapshot) (sysboxcapture.BaseRecoveryVerifier, error) {
	active := e.config.BoundedStore
	if active == nil || saved == nil {
		return nil, nil
	}
	remapped := false
	for _, tree := range saved.Roots {
		remapped = remapped || tree.Immutable != nil && tree.Immutable.Device != active.BlockDevice
	}
	if !remapped {
		return nil, nil // Preserve strict standalone and same-device recovery.
	}
	parent := filepath.Dir(e.config.Root)
	if err := canonical(parent); err != nil {
		return nil, err
	}
	if err := privateDirectory(parent); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	reader := Engine{root: root}
	var fields map[string]json.RawMessage
	if err := reader.readCheckpointJSON("worker.json", &fields); err != nil {
		return nil, err
	}
	var epoch recoveryStoreEpoch
	for name, value := range map[string]any{"schema": &epoch.Schema, "store": &epoch.Store,
		"store_id": &epoch.StoreID, "epoch": &epoch.Epoch, "boot_id": &epoch.BootID, "lifecycle": &epoch.Lifecycle} {
		if err := json.Unmarshal(fields[name], value); err != nil {
			return nil, fmt.Errorf("source epoch %s: %w", name, err)
		}
	}
	old, err := validateRecoveryStoreEpoch(e.config, e.boot, source, epoch)
	if err != nil {
		return nil, err
	}
	return func(tree sysboxcapture.FilesystemTree, currentDevice uint64) error {
		if tree.Immutable == nil || tree.Immutable.Device != old.BlockDevice || currentDevice != active.BlockDevice ||
			!reflect.DeepEqual(tree.Immutable, source.Base.Immutable) || !withinStore(e.config.Root, tree.Immutable.Root) {
			return fmt.Errorf("translated base differs from its exact committed source")
		}
		if err := storefs.Verify(*active); err != nil {
			return err
		}
		return e.boundedObject(tree.Immutable.Root)
	}, nil
}
