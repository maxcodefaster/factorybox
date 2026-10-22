//go:build linux

// Package storefs owns an explicitly created fixed-size persistent filesystem.
// Worker publication, admission policy and migration remain caller responsibilities.
package storefs

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const HostFreeFloor = 12 << 30

var ownerID = regexp.MustCompile(`^[0-9a-f]{32}$`)
var shaID = regexp.MustCompile(`^[0-9a-f]{64}$`)

type Image struct {
	Schema int    `json:"schema"`
	Owner  string `json:"owner"`
	Path   string `json:"path"`
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
	Bytes  int64  `json:"bytes"`
	UUID   string `json:"filesystem_uuid"`
}

type FormatTool struct {
	Path   string
	SHA256 string
}

func privateDirectory(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" {
		return fmt.Errorf("requires a canonical private directory")
	}
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		stat := info.Sys().(*syscall.Stat_t)
		if !info.IsDir() || stat.Uid != 0 || info.Mode().Perm()&0022 != 0 ||
			(current == path && info.Mode().Perm() != 0700) {
			return fmt.Errorf("unsafe private filesystem directory: %s", current)
		}
		if current == "/" {
			return nil
		}
	}
}

func validateImageRequest(path string, size int64, owner string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" ||
		!ownerID.MatchString(owner) || owner == "00000000000000000000000000000000" ||
		size < 64<<20 || size > 64<<30 || size%4096 != 0 {
		return fmt.Errorf("requires a canonical image, nonzero store owner and page-aligned capacity from 64 MiB to 64 GiB")
	}
	return nil
}

func imageIdentity(file *os.File, path, owner string, size int64) (Image, error) {
	image := Image{Schema: 1, Owner: owner, Path: path, Bytes: size, UUID: owner}
	var stat unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &stat); err != nil {
		return image, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Mode&0777 != 0600 || stat.Uid != 0 || stat.Nlink != 1 || stat.Size != size {
		return image, fmt.Errorf("image must be a private root-owned unshared regular file with the exact fixed size")
	}
	image.Device, image.Inode = uint64(stat.Dev), stat.Ino
	return image, nil
}

func verifySuperblock(file *os.File, expected Image) error {
	var header [120]byte
	if _, err := file.ReadAt(header[:], 1024); err != nil {
		return err
	}
	if binary.LittleEndian.Uint16(header[56:58]) != 0xef53 || hex.EncodeToString(header[104:120]) != expected.UUID {
		return fmt.Errorf("fixed filesystem magic or owner UUID differs")
	}
	return nil
}

func openImage(expected Image) (_ *os.File, resultErr error) {
	if err := validateImageRequest(expected.Path, expected.Bytes, expected.Owner); err != nil {
		return nil, err
	}
	if expected.Schema != 1 || expected.UUID != expected.Owner || expected.Device == 0 || expected.Inode == 0 {
		return nil, fmt.Errorf("invalid saved fixed filesystem identity")
	}
	if err := privateDirectory(filepath.Dir(expected.Path)); err != nil {
		return nil, err
	}
	fd, err := unix.Open(expected.Path, unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "owned-fixed-filesystem-image")
	defer func() {
		if resultErr != nil {
			file.Close()
		}
	}()
	actual, err := imageIdentity(file, expected.Path, expected.Owner, expected.Bytes)
	if err != nil || actual != expected {
		return nil, fmt.Errorf("fixed image identity differs: %v", err)
	}
	if err := verifySuperblock(file, expected); err != nil {
		return nil, err
	}
	return file, nil
}

func checkHostFloor(parent string, required uint64) error {
	var fs unix.Statfs_t
	if err := unix.Statfs(parent, &fs); err != nil {
		return err
	}
	if fs.Bsize <= 0 || fs.Bavail > fs.Blocks || fs.Blocks > ^uint64(0)/uint64(fs.Bsize) ||
		fs.Bavail*uint64(fs.Bsize) < required {
		return fmt.Errorf("fixed filesystem allocation would consume the host free-space floor")
	}
	return nil
}

// Create refuses existing files. A failed or interrupted initialization leaves
// its file for explicit owned reconciliation; no later call reformats it.
// The caller must persist the returned identity before publishing a worker.
func Create(ctx context.Context, path string, size int64, owner string, tool FormatTool) (Image, error) {
	var empty Image
	if os.Geteuid() != 0 {
		return empty, fmt.Errorf("fixed filesystem creation requires worker root")
	}
	if err := validateImageRequest(path, size, owner); err != nil {
		return empty, err
	}
	if err := privateDirectory(filepath.Dir(path)); err != nil {
		return empty, err
	}
	if !filepath.IsAbs(tool.Path) || filepath.Clean(tool.Path) != tool.Path || !shaID.MatchString(tool.SHA256) {
		return empty, fmt.Errorf("requires an exact formatter executable pin")
	}
	fd, err := unix.Open(tool.Path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return empty, err
	}
	formatter := os.NewFile(uintptr(fd), "pinned-filesystem-formatter")
	defer formatter.Close()
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG ||
		stat.Mode&0022 != 0 || stat.Mode&0111 == 0 || stat.Uid != 0 {
		return empty, fmt.Errorf("unsafe filesystem formatter: %v", err)
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, formatter); err != nil || hex.EncodeToString(hash.Sum(nil)) != tool.SHA256 {
		return empty, fmt.Errorf("filesystem formatter SHA-256 differs: %v", err)
	}
	if err := checkHostFloor(filepath.Dir(path), uint64(size)+HostFreeFloor); err != nil {
		return empty, err
	}
	fd, err = unix.Open(path, unix.O_RDWR|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return empty, err
	}
	image := os.NewFile(uintptr(fd), "new-fixed-filesystem-image")
	defer image.Close()
	if err := unix.Fallocate(fd, 0, 0, size); err != nil {
		return empty, err
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	uuid := owner[:8] + "-" + owner[8:12] + "-" + owner[12:16] + "-" + owner[16:20] + "-" + owner[20:]
	command := exec.CommandContext(ctx, "/proc/self/fd/3", "-t", "ext4", "-F", "-q", "-m", "0", "-U", uuid,
		"-E", "nodiscard,lazy_itable_init=0,lazy_journal_init=0", "/proc/self/fd/4")
	command.ExtraFiles = []*os.File{formatter, image}
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "HOME=/root"}
	command.WaitDelay = 2 * time.Second
	if output, err := command.CombinedOutput(); err != nil {
		return empty, fmt.Errorf("fixed image initialization failed: %w: %.4096s", err, output)
	}
	// Reestablish allocation after formatting as well. The image is never
	// mounted with automatic discard; future host checks are still separate.
	if err := errors.Join(unix.Fallocate(fd, 0, 0, size), image.Sync()); err != nil {
		return empty, err
	}
	identity, err := imageIdentity(image, path, owner, size)
	if err != nil {
		return empty, err
	}
	if err := verifySuperblock(image, identity); err != nil {
		return empty, err
	}
	if err := checkHostFloor(filepath.Dir(path), HostFreeFloor); err != nil {
		return empty, err
	}
	parent, err := os.Open(filepath.Dir(path))
	if err != nil {
		return empty, err
	}
	return identity, errors.Join(parent.Sync(), parent.Close())
}
