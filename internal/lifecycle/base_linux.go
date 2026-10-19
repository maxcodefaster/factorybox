//go:build linux

package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"os"

	"factorybox/internal/sysboxcapture"
)

const baseAnnotation = "io.factorybox.base"

type Base struct {
	Version       int                          `json:"version"`
	ID            string                       `json:"id"`
	Source        string                       `json:"source"`
	BootID        string                       `json:"boot_id"`
	RuntimeSHA256 string                       `json:"runtime_sha256"`
	PreparedAt    string                       `json:"prepared_at"`
	Tree          sysboxcapture.FilesystemTree `json:"tree"`
}

// PrepareBase imports and verifies a complete base before application startup.
// The immutable caller-selected ID owns one import request. Interrupted imports
// remain retained and cannot be overwritten by repeating that ID.
func (e *Engine) PrepareBase(ctx context.Context, id, source string) (Base, error) {
	lock, err := e.lock()
	if err != nil {
		return Base{}, err
	}
	defer lock.Close()
	if !sandboxID.MatchString(id) || canonical(source) != nil {
		return Base{}, fmt.Errorf("base preparation requires an immutable ID and canonical source")
	}
	base, err := e.readBase(id)
	if err == nil {
		if base.Source != source {
			return base, fmt.Errorf("base ID already belongs to a different import request")
		}
		return base, sysboxcapture.VerifyImmutableTree(base.Tree.Saved, base.Tree)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Base{}, err
	}
	relative := "bases/" + id
	if err := e.root.Mkdir(relative, 0700); err != nil {
		return Base{}, fmt.Errorf("base import is incomplete or already owned; refusing replacement: %w", err)
	}
	base = Base{Version: 1, ID: id, Source: source, BootID: e.boot, RuntimeSHA256: e.config.RuntimeSHA256}
	if err := e.publishCheckpoint(relative, "intent.json", base); err != nil {
		return base, err
	}
	parent, err := e.root.Open("bases")
	if err != nil {
		return base, err
	}
	if err := errors.Join(parent.Sync(), parent.Close()); err != nil {
		return base, err
	}
	base.Tree, err = sysboxcapture.PrepareFilesystemBase(ctx, source, e.config.Root+"/"+relative+"/rootfs")
	if err != nil {
		return base, err
	}
	base.PreparedAt = now()
	return base, e.publishCheckpoint(relative, "base.json", base)
}

func (e *Engine) readBase(id string) (Base, error) {
	var base Base
	if !sandboxID.MatchString(id) {
		return base, fmt.Errorf("invalid immutable base ID")
	}
	path := "bases/" + id
	if err := e.readCheckpointJSON(path+"/base.json", &base); err != nil {
		return base, err
	}
	root := e.config.Root + "/" + path + "/rootfs"
	if base.Version != 1 || base.ID != id || base.BootID != e.boot ||
		base.RuntimeSHA256 != e.config.RuntimeSHA256 || base.PreparedAt == "" ||
		base.Tree.Name != "lower" || base.Tree.Source != base.Source || base.Tree.Saved != root ||
		base.Tree.Immutable == nil || base.Tree.Immutable.Root != root {
		return base, fmt.Errorf("prepared base differs from its import identity or worker boot")
	}
	var intent Base
	if err := e.readCheckpointJSON(path+"/intent.json", &intent); err != nil {
		return base, err
	}
	if intent.Version != base.Version || intent.ID != base.ID || intent.Source != base.Source ||
		intent.BootID != base.BootID || intent.RuntimeSHA256 != base.RuntimeSHA256 ||
		intent.PreparedAt != "" || intent.Tree.SHA256 != "" || intent.Tree.Immutable != nil {
		return base, fmt.Errorf("prepared base differs from its original durable intent")
	}
	return base, nil
}

func (e *Engine) recordBase(id, bundle string, origin *ForkOrigin, annotations map[string]string) (*sysboxcapture.FilesystemTree, error) {
	if origin != nil {
		var prepared forkPreparation
		if err := e.readCheckpointJSON("forks/"+id+"/preparation.json", &prepared); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				// No sharing shortcut is inferred from incomplete backing.
				// The ordinary full-copy path remains safe without a base.
				return nil, nil
			}
			return nil, err
		}
		if prepared.Filesystem == nil {
			return nil, fmt.Errorf("fork preparation omitted its filesystem")
		}
		for _, tree := range prepared.Filesystem.Roots {
			if tree.Name == "lower" && tree.Immutable != nil {
				return &tree, sysboxcapture.VerifyImmutableTree(bundle+"/lower", tree)
			}
		}
		return nil, nil
	}
	baseID, present := annotations[baseAnnotation]
	if !present {
		return nil, nil
	}
	base, err := e.readBase(baseID)
	if err != nil {
		return nil, err
	}
	if err := sysboxcapture.VerifyImmutableTree(bundle+"/lower", base.Tree); err != nil {
		return nil, err
	}
	return &base.Tree, nil
}
