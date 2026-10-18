package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

type rootfsCacheProvenance struct {
	Version      int    `json:"version"`
	BaseImageID  string `json:"base_image_id"`
	Source       string `json:"source"`
	Device       uint64 `json:"source_device"`
	Inode        uint64 `json:"source_inode"`
	Mode         uint32 `json:"source_mode"`
	UID          uint32 `json:"source_uid"`
	GID          uint32 `json:"source_gid"`
	MtimeNS      int64  `json:"source_mtime_ns"`
	CtimeNS      int64  `json:"source_ctime_ns"`
	Entries      int    `json:"complete_entries"`
	PayloadBytes int64  `json:"regular_payload_bytes"`
	ManifestSHA  string `json:"complete_manifest_sha256"`
}

func criuUnpackTemplate(destination string, timeout time.Duration) (bool, error) {
	root := os.Getenv("RUNTIME_FORK_ROOTFS_CACHE")
	if root == "" {
		return false, nil
	}
	start := time.Now()
	finish := func(output string, err error) (bool, error) {
		record("rootfs-verified-template-clone", start, output, err)
		return true, err
	}
	if root != "/opt/probe-rootfs-cache" {
		return finish("", fmt.Errorf("rootfs template must use the owned readonly fixture mount"))
	}
	manifest := filepath.Join(root, "template.json")
	file, err := os.OpenFile(manifest, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return finish("", err)
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 16384 {
		file.Close()
		return finish("", fmt.Errorf("invalid rootfs template provenance"))
	}
	data := make([]byte, info.Size())
	_, err = file.ReadAt(data, 0)
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		return finish("", fmt.Errorf("read rootfs template provenance: %v %v", err, closeErr))
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != os.Getenv("RUNTIME_FORK_ROOTFS_CACHE_SHA256") {
		return finish("", fmt.Errorf("rootfs template provenance does not match the pinned native build"))
	}
	var saved rootfsCacheProvenance
	if err := json.Unmarshal(data, &saved); err != nil || saved.Version != 1 || saved.Source != "source" ||
		saved.BaseImageID != "sha256:041140e968e8efdbc6b2f33651f84bc19c13593c6d3f527e90af648f967dd52f" ||
		saved.Entries <= 0 || saved.PayloadBytes <= 0 || len(saved.ManifestSHA) != 64 ||
		saved.UID != 0 || saved.GID != 0 || saved.Mode&unix.S_IFMT != unix.S_IFDIR {
		return finish("", fmt.Errorf("unsupported rootfs template provenance: %v", err))
	}
	if _, err := hex.DecodeString(saved.ManifestSHA); err != nil {
		return finish("", fmt.Errorf("invalid rootfs template manifest digest"))
	}
	source := filepath.Join(root, saved.Source)
	fd, err := unix.Open(source, unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return finish("", err)
	}
	defer unix.Close(fd)
	verify := func() error {
		var filesystem unix.Statfs_t
		var actual, named unix.Stat_t
		if err := unix.Fstatfs(fd, &filesystem); err != nil {
			return err
		}
		if filesystem.Type != unix.BTRFS_SUPER_MAGIC || filesystem.Flags&unix.ST_RDONLY == 0 {
			return fmt.Errorf("rootfs template requires the tested Btrfs readonly mount")
		}
		if err := unix.Fstat(fd, &actual); err != nil {
			return err
		}
		if err := unix.Lstat(source, &named); err != nil {
			return err
		}
		if uint64(actual.Dev) != saved.Device || actual.Ino != saved.Inode || named.Dev != actual.Dev ||
			named.Ino != actual.Ino || actual.Mode != saved.Mode || actual.Uid != saved.UID ||
			actual.Gid != saved.GID || actual.Mtim.Sec*1e9+actual.Mtim.Nsec != saved.MtimeNS ||
			actual.Ctim.Sec*1e9+actual.Ctim.Nsec != saved.CtimeNS {
			return fmt.Errorf("rootfs template identity changed")
		}
		return nil
	}
	if err := verify(); err != nil {
		return finish("", err)
	}
	// All regular files must use FICLONE. An unsupported seam fails before a
	// sandbox can start; the cache never silently changes to a complete copy.
	remaining := timeout - time.Since(start)
	if remaining <= 0 {
		return finish("", fmt.Errorf("rootfs template admission exhausted the preparation deadline"))
	}
	output, err := criuCommand(remaining, "cp", "-a", "--reflink=always", source+"/.", destination)
	if err == nil {
		err = verify()
	}
	return finish(asJSON(map[string]any{
		"template": root, "manifest_sha256": saved.ManifestSHA,
		"complete_entries": saved.Entries, "regular_payload_bytes": saved.PayloadBytes,
		"kernel_reflink_required": true, "output": output,
		"scope": "Full public rootfs template and exact overrides; preparation charged to cold start. Trusted worker/root ownership required.",
	}), err)
}
