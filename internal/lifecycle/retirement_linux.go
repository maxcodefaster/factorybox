//go:build linux

package lifecycle

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"

	"factorybox/internal/ownedtree"
	"factorybox/internal/sysboxcapture"
	"golang.org/x/sys/unix"
)

const retirementInventoryLimit = 256 << 20
const retirementEncodedLimit = 64 << 20
const retirementReserveBytes = 1 << 20

type retirementReserve struct {
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
}

type checkpointRetirementIntent struct {
	Version         int                            `json:"version"`
	PayloadKind     string                         `json:"payload_kind,omitempty"`
	Request         CheckpointRequest              `json:"request"`
	Directory       KernelObject                   `json:"directory"`
	InventorySHA256 string                         `json:"inventory_sha256"`
	Payloads        []string                       `json:"payloads"`
	Aliases         []sysboxcapture.FilesystemTree `json:"aliases"`
	Reserve         retirementReserve              `json:"reserve"`
	StartedAt       string                         `json:"started_at"`
	Before          StorageCapacity                `json:"before"`
}

type CheckpointRetirement struct {
	Version      int               `json:"version"`
	PayloadKind  string            `json:"payload_kind,omitempty"`
	Request      CheckpointRequest `json:"request"`
	IntentSHA256 string            `json:"intent_sha256"`
	CompletedAt  string            `json:"completed_at"`
	Before       StorageCapacity   `json:"before"`
	After        StorageCapacity   `json:"after"`
}

func (e *Engine) writeRetirementInventory(relative string, plan ownedtree.Plan) (string, error) {
	data, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	if len(data) > retirementInventoryLimit {
		return "", fmt.Errorf("retirement inventory exceeds decoded bound")
	}
	file, err := e.root.OpenFile(relative+"/retirement-payload.json.gz", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", fmt.Errorf("retirement preparation already exists or is uncertain: %w", err)
	}
	hash := sha256.New()
	encoded := gzip.NewWriter(io.MultiWriter(file, hash))
	_, writeErr := encoded.Write(data)
	if err := errors.Join(writeErr, encoded.Close(), file.Sync(), file.Close()); err != nil {
		return "", err
	}
	info, err := e.root.Lstat(relative + "/retirement-payload.json.gz")
	if err != nil {
		return "", err
	}
	if info.Size() > retirementEncodedLimit {
		return "", fmt.Errorf("retirement inventory exceeds encoded bound")
	}
	parent, err := e.root.Open(relative)
	if err != nil {
		return "", err
	}
	if err := errors.Join(parent.Sync(), parent.Close()); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (e *Engine) readRetirementInventory(relative, expected string) (plan ownedtree.Plan, resultErr error) {
	root, err := e.root.Open(".")
	if err != nil {
		return plan, err
	}
	defer root.Close()
	fd, err := unix.Openat2(int(root.Fd()), relative+"/retirement-payload.json.gz", &unix.OpenHow{
		Flags: unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS})
	if err != nil {
		return plan, err
	}
	file := os.NewFile(uintptr(fd), "retirement inventory")
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return plan, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Mode&0777 != 0600 || stat.Uid != 0 || stat.Nlink != 1 || stat.Size > retirementEncodedLimit {
		return plan, fmt.Errorf("retirement inventory file identity differs")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return plan, err
	}
	if hex.EncodeToString(hash.Sum(nil)) != expected {
		return plan, fmt.Errorf("retirement inventory SHA-256 changed")
	}
	if _, err := file.Seek(0, 0); err != nil {
		return plan, err
	}
	decoded, err := gzip.NewReader(file)
	if err != nil {
		return plan, err
	}
	defer decoded.Close()
	limited := &io.LimitedReader{R: decoded, N: retirementInventoryLimit + 1}
	decoder := json.NewDecoder(limited)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&plan); err != nil {
		return plan, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF || limited.N <= 0 {
		return plan, fmt.Errorf("retirement inventory has trailing or excessive decoded data")
	}
	return plan, nil
}

func (e *Engine) prepareRetirementReserve(relative string) (retirementReserve, error) {
	var reserve retirementReserve
	file, err := e.root.OpenFile(relative+"/.retirement-reserve", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return reserve, err
	}
	defer file.Close()
	if err := unix.Fallocate(int(file.Fd()), 0, 0, retirementReserveBytes); err != nil {
		return reserve, err
	}
	var stat unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &stat); err != nil {
		return reserve, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Mode&0777 != 0600 || stat.Uid != 0 || stat.Nlink != 1 ||
		stat.Size != retirementReserveBytes || stat.Blocks*512 < retirementReserveBytes {
		return reserve, fmt.Errorf("retirement reserve was not privately allocated")
	}
	reserve = retirementReserve{uint64(stat.Dev), stat.Ino}
	return reserve, file.Sync()
}

func (e *Engine) releaseRetirementReserve(relative string, expected retirementReserve) error {
	root, err := e.root.Open(".")
	if err != nil {
		return err
	}
	defer root.Close()
	fd, err := unix.Openat2(int(root.Fd()), relative+"/.retirement-reserve", &unix.OpenHow{Flags: unix.O_RDWR | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS})
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return err
	}
	if uint64(stat.Dev) != expected.Device || stat.Ino != expected.Inode || stat.Mode&unix.S_IFMT != unix.S_IFREG ||
		stat.Mode&0777 != 0600 || stat.Uid != 0 || stat.Nlink != 1 || (stat.Size != 0 && stat.Size != retirementReserveBytes) {
		return fmt.Errorf("retirement reserve identity changed")
	}
	return errors.Join(unix.Ftruncate(fd, 0), unix.Fsync(fd))
}

func (e *Engine) retirementAliases(aliases []sysboxcapture.FilesystemTree, remove bool) error {
	for _, tree := range aliases {
		if err := canonical(tree.Saved); err != nil {
			return err
		}
		if _, err := e.root.Lstat(strings.TrimPrefix(tree.Saved, e.config.Root+"/")); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		if _, err := sysboxcapture.CheckImmutableAliasRetirement(tree.Saved, tree); err != nil {
			return err
		}
	}
	if !remove {
		return nil
	}
	for _, tree := range aliases {
		relative := strings.TrimPrefix(tree.Saved, e.config.Root+"/")
		if _, err := e.root.Lstat(relative); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		if err := sysboxcapture.RetireImmutableAlias(tree.Saved, tree); err != nil {
			return err
		}
		if err := e.root.Remove(relative); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) retirementAbsent(intent checkpointRetirementIntent) error {
	for _, name := range intent.Payloads {
		if _, err := e.root.Lstat(name); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("retired payload has an unexplained successor: %s: %v", name, err)
		}
	}
	return nil
}

// RetireCheckpoint withdraws only complete, unselected backing. Metadata,
// original request identity and the exact deletion inventory remain durable.
func (e *Engine) RetireCheckpoint(ctx context.Context, request CheckpointRequest) (result CheckpointRetirement, resultErr error) {
	lock, err := e.lock()
	if err != nil {
		return result, err
	}
	defer lock.Close()
	if e.config.BoundedStore == nil {
		return result, fmt.Errorf("checkpoint retirement requires the explicit bounded-store profile")
	}
	record, saved, relative, err := e.checkpointRequest(request)
	if err != nil {
		return result, err
	}
	directory, err := directoryIdentity(e.config.Root + "/" + relative)
	if err != nil {
		return result, err
	}
	var intent checkpointRetirementIntent
	err = e.readCheckpointJSON(relative+"/retirement-intent.json", &intent)
	if errors.Is(err, os.ErrNotExist) {
		if err := e.checkpointSelections(relative, request); err != nil {
			return result, err
		}
		if err := e.checkpointDependencies(ctx, request, lock); err != nil {
			return result, err
		}
		published, err := e.publishedCheckpointMetadata(ctx, record, request.SnapshotID, relative)
		if err != nil {
			return result, err
		}
		if !reflect.DeepEqual(published, saved) {
			return result, fmt.Errorf("published checkpoint changed before retirement")
		}
		if err := sysboxcapture.VerifyFilesystemSnapshot(ctx, e.config.Root+"/"+relative, saved.Filesystem); err != nil {
			return result, err
		}
		if saved.Filesystem == nil {
			return result, fmt.Errorf("retirement has no complete filesystem ownership")
		}
		paths := []string{relative + "/images"}
		aliases := []sysboxcapture.FilesystemTree{}
		payloads := append([]string(nil), paths...)
		for _, tree := range saved.Filesystem.Roots {
			expected := e.config.Root + "/" + relative + "/filesystem/" + tree.Name
			if tree.Saved != expected {
				return result, fmt.Errorf("retirement tree is outside the exact generation")
			}
			name := strings.TrimPrefix(tree.Saved, e.config.Root+"/")
			payloads = append(payloads, name)
			if tree.Immutable != nil {
				if tree.Immutable.Root == e.config.Root+"/"+relative || strings.HasPrefix(tree.Immutable.Root, e.config.Root+"/"+relative+"/") {
					return result, fmt.Errorf("retirement would delete a retained immutable base")
				}
				aliases = append(aliases, tree)
			} else {
				paths = append(paths, name)
			}
		}
		plan, err := ownedtree.Capture(ctx, e.root, paths)
		if err != nil {
			return result, err
		}
		before, err := e.storageCapacity()
		if err != nil {
			return result, err
		}
		hash, err := e.writeRetirementInventory(relative, plan)
		if err != nil {
			return result, err
		}
		reserve, err := e.prepareRetirementReserve(relative)
		if err != nil {
			return result, err
		}
		intent = checkpointRetirementIntent{Version: 1, Request: request, Directory: directory, InventorySHA256: hash,
			Payloads: payloads, Aliases: aliases, Reserve: reserve, StartedAt: now(), Before: before}
		if err := e.publishCheckpoint(relative, "retirement-intent.json", intent); err != nil {
			return result, err
		}
	} else if err != nil {
		return result, err
	}
	if intent.Version != 1 || intent.PayloadKind != "" || intent.Request != request || intent.Directory != directory || !digest.MatchString(intent.InventorySHA256) || intent.StartedAt == "" {
		return result, fmt.Errorf("retirement intent differs from its exact request or generation")
	}
	identity, err := json.Marshal(intent)
	if err != nil {
		return result, err
	}
	intentHash := sum(identity)
	if err := e.readCheckpointJSON(relative+"/retirement-complete.json", &result); err == nil {
		if result.Version != 1 || result.PayloadKind != "" || result.Request != request || result.IntentSHA256 != intentHash || result.CompletedAt == "" || !reflect.DeepEqual(result.Before, intent.Before) {
			return result, fmt.Errorf("retirement completion differs from its original intent")
		}
		return result, e.retirementAbsent(intent)
	} else if !errors.Is(err, os.ErrNotExist) {
		return result, err
	}
	if err := e.checkpointSelections(relative, request); err != nil {
		return result, err
	}
	if err := e.checkpointDependencies(ctx, request, lock); err != nil {
		return result, err
	}
	plan, err := e.readRetirementInventory(relative, intent.InventorySHA256)
	if err != nil {
		return result, err
	}
	if err := ownedtree.ValidateRemaining(ctx, e.root, plan); err != nil {
		return result, err
	}
	if err := e.retirementAliases(intent.Aliases, true); err != nil {
		return result, err
	}
	if err := ownedtree.RemoveRemaining(ctx, e.root, plan); err != nil {
		return result, err
	}
	if err := e.retirementAbsent(intent); err != nil {
		return result, err
	}
	if err := e.releaseRetirementReserve(relative, intent.Reserve); err != nil {
		return result, err
	}
	after, err := e.storageCapacity()
	if err != nil {
		return result, err
	}
	result = CheckpointRetirement{Version: 1, Request: request, IntentSHA256: intentHash, CompletedAt: now(), Before: intent.Before, After: after}
	return result, e.publishCheckpoint(relative, "retirement-complete.json", result)
}
