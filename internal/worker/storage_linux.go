//go:build linux

package worker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"factorybox/internal/storefs"
	"golang.org/x/sys/unix"
)

const (
	boundedMinFreeBytes = 64 << 20
	boundedWriteReserve = 128 << 20
)

type boundedIdentity struct {
	Schema          int           `json:"schema"`
	Image           storefs.Image `json:"image"`
	Formatter       string        `json:"formatter"`
	FormatterSHA256 string        `json:"formatter_sha256"`
}

func boundedConfig(config *Config) error {
	if config.BoundedRoot == "" && config.BoundedBytes == 0 {
		if config.Store == "" {
			config.Store = "/var/lib/sysbox/store"
		}
		return nil
	}
	if !filepath.IsAbs(config.BoundedRoot) || filepath.Clean(config.BoundedRoot) != config.BoundedRoot ||
		config.BoundedRoot == "/" || config.BoundedBytes < 512<<20 || config.BoundedBytes > 64<<30 || config.BoundedBytes%4096 != 0 {
		return fmt.Errorf("bounded store requires a canonical root and a page-aligned capacity from 512 MiB to 64 GiB")
	}
	store := config.BoundedRoot + "/data/store"
	if config.Store != "" && config.Store != store {
		return fmt.Errorf("bounded store must be exactly %s; existing stores are never migrated", store)
	}
	config.Store = store
	return nil
}

// The outer lease and identity live outside the data filesystem. A partial
// initialization is retained and refused; no existing image is reformatted.
func attachBounded(ctx context.Context, config Config) (_ *storefs.Attachment, _ *os.File, resultErr error) {
	if err := makePrivate(config.BoundedRoot); err != nil {
		return nil, nil, err
	}
	lease, err := openLease(config.BoundedRoot, true)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, lease.Close())
		}
	}()
	if err := unix.Flock(int(lease.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return nil, nil, fmt.Errorf("bounded store already has a worker: %w", err)
	}
	var identity boundedIdentity
	if err := readJSON(config.BoundedRoot+"/bounded.json", &identity); errors.Is(err, os.ErrNotExist) {
		entries, err := os.ReadDir(config.BoundedRoot)
		if err != nil {
			return nil, nil, err
		}
		for _, entry := range entries {
			if entry.Name() != ".worker.lock" {
				return nil, nil, fmt.Errorf("refusing unidentified or interrupted bounded-store initialization")
			}
		}
		formatter, err := filepath.EvalSymlinks("/usr/sbin/mkfs.ext4")
		if err != nil {
			return nil, nil, err
		}
		hash, err := hashFile(formatter)
		if err != nil {
			return nil, nil, err
		}
		image, err := storefs.Create(ctx, config.BoundedRoot+"/store.img", config.BoundedBytes, config.StoreID,
			storefs.FormatTool{Path: formatter, SHA256: hash})
		if err != nil {
			return nil, nil, err
		}
		identity = boundedIdentity{Schema: 1, Image: image, Formatter: formatter, FormatterSHA256: hash}
		if err := writeNew(config.BoundedRoot+"/bounded.json", identity); err != nil {
			return nil, nil, err
		}
	} else if err != nil {
		return nil, nil, err
	}
	if identity.Schema != 1 || identity.Image.Owner != config.StoreID || identity.Image.Bytes != config.BoundedBytes ||
		identity.Image.Path != config.BoundedRoot+"/store.img" || len(identity.FormatterSHA256) != 64 {
		return nil, nil, fmt.Errorf("bounded store owner, capacity or saved image identity changed")
	}
	if err := makePrivate(config.BoundedRoot + "/data"); err != nil {
		return nil, nil, err
	}
	attachment, err := storefs.Attach(identity.Image, config.BoundedRoot+"/data")
	if err != nil {
		return nil, nil, err
	}
	return attachment, lease, nil
}

func verifyBoundedStatus(status Status) error {
	active := status.Lifecycle.BoundedStore
	if active == nil {
		if status.Lifecycle.MinFreeBytes != localMinFreeBytes || status.Lifecycle.WriteReserve != localWriteReserve {
			return fmt.Errorf("worker storage admission recipe changed")
		}
		return nil
	}
	root := filepath.Dir(active.Image.Path)
	if active.Image.Path != root+"/store.img" || active.Target != root+"/data" ||
		status.Store != active.Target+"/store" || active.Image.Owner != status.StoreID ||
		status.Lifecycle.MinFreeBytes != boundedMinFreeBytes || status.Lifecycle.WriteReserve != boundedWriteReserve {
		return fmt.Errorf("bounded worker storage recipe changed")
	}
	if err := private(root); err != nil {
		return err
	}
	var identity boundedIdentity
	if err := readJSON(root+"/bounded.json", &identity); err != nil || identity.Schema != 1 || identity.Image != active.Image {
		return fmt.Errorf("bounded worker saved image changed: %v", err)
	}
	lease, err := openLease(root, false)
	if err != nil {
		return err
	}
	defer lease.Close()
	if err := unix.Flock(int(lease.Fd()), unix.LOCK_EX|unix.LOCK_NB); err == nil {
		return fmt.Errorf("bounded worker no longer holds its outer lease")
	} else if !errors.Is(err, unix.EWOULDBLOCK) {
		return err
	}
	return storefs.Verify(*active)
}
