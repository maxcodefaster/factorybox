//go:build linux

package lifecycle

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"

	"factorybox/internal/sandboxnetwork"
	"factorybox/internal/sysboxcapture"
)

func validateForkNetwork(intent forkIntent) error {
	if intent.Source.Network == nil {
		if intent.TargetNetwork != nil || len(intent.NetworkCopy) != 0 {
			return fmt.Errorf("fork added a network absent from its original checkpoint")
		}
		return nil
	}
	if intent.Recovery != nil || intent.TargetNetwork == nil || intent.TargetNetwork.Owner != intent.Owner {
		return fmt.Errorf("network fork requires its independently owned same-worker target")
	}
	if _, err := intent.TargetNetwork.CopyConfiguration(*intent.Source.Network, intent.CriuConfig); err != nil {
		return err
	}
	var proof sysboxcapture.NetworkCopyEvidence
	if err := json.Unmarshal(intent.NetworkCopy, &proof); err != nil {
		return fmt.Errorf("network fork lacks capture-time socket admission: %w", err)
	}
	if proof.Version != 1 || !proof.Allowed || proof.Reason != "" ||
		proof.SourcePID != intent.Checkpoint.SourcePID || proof.SourcePID <= 1 ||
		proof.SourceStart == "" || proof.Address != intent.Source.Network.Address ||
		proof.Namespaces < 1 || proof.Sockets < 0 {
		return fmt.Errorf("network fork socket admission refused: %s", proof.Reason)
	}
	for _, file := range intent.Checkpoint.Files {
		if file.Name == "images/"+sysboxcapture.NetworkCopyImage &&
			file.Bytes == int64(len(intent.NetworkCopy)) && file.SHA256 == sum(intent.NetworkCopy) {
			return nil
		}
	}
	return fmt.Errorf("network fork socket admission is not in the original hashed checkpoint")
}

func forkCRIUConfiguration(intent forkIntent) ([]byte, error) {
	if err := validateForkNetwork(intent); err != nil {
		return nil, err
	}
	if intent.TargetNetwork == nil {
		return intent.CriuConfig, nil
	}
	return intent.TargetNetwork.CopyConfiguration(*intent.Source.Network, intent.CriuConfig)
}

func (e *Engine) forkNetwork(origin ForkOrigin, id, owner string) (*sandboxnetwork.Config, error) {
	intent, err := e.readForkIntent("forks/"+id, origin.SourceID, origin.CheckpointID, id)
	if err != nil {
		return nil, err
	}
	if intent.Owner != owner || !reflect.DeepEqual(intent.Origin, origin) || intent.TargetNetwork == nil {
		return nil, fmt.Errorf("fork network differs from its durable original request")
	}
	return intent.TargetNetwork, nil
}

// Paused Sandboxes have no live route but still own their subnet. Reserve
// pending fork intents as well, before any backing or runtime is created.
func (e *Engine) admitNetwork(config sandboxnetwork.Config, id string) error {
	if err := config.Admit(); err != nil {
		return err
	}
	for _, directory := range []string{"objects", "forks"} {
		root, err := e.root.Open(directory)
		if err != nil {
			return err
		}
		entries, err := root.ReadDir(65537)
		err = errors.Join(err, root.Close())
		if err != nil && !errors.Is(err, io.EOF) || len(entries) > 65536 {
			return fmt.Errorf("network reservation inventory unavailable: %v", err)
		}
		for _, entry := range entries {
			if entry.Name() == id {
				continue
			}
			var network *sandboxnetwork.Config
			if directory == "objects" {
				record, err := e.read(entry.Name())
				if err != nil {
					return err
				}
				if record.Phase == "deleted" && record.Delete != nil && record.Delete.CompletedAt != "" {
					continue
				}
				network = record.Network
			} else {
				var intent forkIntent
				if err := e.readCheckpointJSON(directory+"/"+entry.Name()+"/intent.json", &intent); err != nil {
					return err
				}
				if intent.Version != 1 || intent.TargetID != entry.Name() {
					return fmt.Errorf("network reservation fork identity changed")
				}
				if record, err := e.read(intent.TargetID); err == nil {
					if record.Phase == "deleted" && record.Delete != nil && record.Delete.CompletedAt != "" {
						continue
					}
				} else if !errors.Is(err, os.ErrNotExist) {
					return err
				}
				network = intent.TargetNetwork
			}
			if network == nil {
				continue
			}
			if err := network.Validate(); err != nil {
				return err
			}
			if network.Owner == config.Owner || network.CIDR == config.CIDR ||
				network.Peer == config.Peer || network.Guest == config.Guest {
				return fmt.Errorf("network identity or subnet is reserved by another Sandbox")
			}
		}
	}
	return nil
}
