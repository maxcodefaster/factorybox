//go:build linux

package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"factorybox/internal/sysboxcapture"
)

// Recovery imports own a canonical base outside the ordinary base registry.
// Discover it only through the complete recovery intent and private preparation;
// an arbitrary mount or a partial import never establishes release authority.
func (e *Engine) exitForkBases(ctx context.Context) ([]sysboxcapture.FilesystemTree, []sysboxcapture.FilesystemTree, error) {
	entries, err := e.exitEntries("forks")
	if err != nil {
		return nil, nil, err
	}
	var bases, aliases []sysboxcapture.FilesystemTree
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		record, err := e.read(entry.Name())
		if err != nil || record.Fork == nil {
			return nil, nil, fmt.Errorf("worker exit refuses an unconfirmed fork object: %v", err)
		}
		operation := "forks/" + record.ID
		intent, err := e.readForkIntent(operation, record.Fork.SourceID, record.Fork.CheckpointID, record.ID)
		if err != nil {
			return nil, nil, err
		}
		if intent.Owner != record.Owner || !reflect.DeepEqual(&intent.Origin, record.Fork) || record.InputSHA256 != sum(intent.OCI) {
			return nil, nil, fmt.Errorf("worker exit fork differs from its complete intent")
		}
		var prepared forkPreparation
		if err := e.readCheckpointJSON(operation+"/preparation.json", &prepared); err != nil {
			return nil, nil, err
		}
		identity, err := json.Marshal(intent)
		if err != nil || prepared.Version != 1 || prepared.Owner != record.Owner ||
			prepared.IntentSHA256 != sum(identity) || prepared.PreparedAt == "" || prepared.Filesystem == nil {
			return nil, nil, fmt.Errorf("worker exit fork preparation is unconfirmed: %v", err)
		}
		materializedLower := false
		for _, tree := range prepared.Filesystem.Roots {
			if tree.Immutable == nil {
				continue
			}
			if materializedLower || tree.Name != "lower" || tree.Saved != record.InputBundle+"/lower" ||
				record.Base == nil || !reflect.DeepEqual(tree, *record.Base) {
				return nil, nil, fmt.Errorf("worker exit fork base alias differs from its exact preparation")
			}
			materializedLower = true
			aliases = append(aliases, tree)
		}
		if (record.Base != nil) != materializedLower {
			return nil, nil, fmt.Errorf("worker exit fork lacks its prepared base alias")
		}
		if intent.Recovery == nil {
			continue
		}
		generation := forkInputGeneration(intent)
		if err := e.verifyCheckpointMetadata(ctx, intent.Source, intent.Checkpoint.ID, generation, intent.Checkpoint); err != nil {
			return nil, nil, fmt.Errorf("worker exit recovered input is unconfirmed: %w", err)
		}
		inputLower := false
		for _, tree := range intent.Checkpoint.Filesystem.Roots {
			if tree.Immutable == nil {
				continue
			}
			if inputLower || tree.Name != "lower" || tree.Saved != e.config.Root+"/"+generation+"/filesystem/lower" ||
				tree.Immutable.Root != e.config.Root+"/"+generation+"/base/rootfs" {
				return nil, nil, fmt.Errorf("worker exit recovered base is outside its exact completed import")
			}
			canonical := tree
			canonical.Saved, canonical.Alias = tree.Immutable.Root, nil
			if err := sysboxcapture.VerifyImmutableTree(canonical.Saved, canonical); err != nil {
				return nil, nil, err
			}
			inputLower = true
			bases = append(bases, canonical)
			aliases = append(aliases, tree)
		}
		if inputLower != materializedLower {
			return nil, nil, fmt.Errorf("worker exit recovery input and materialized base differ")
		}
	}
	return bases, aliases, nil
}
