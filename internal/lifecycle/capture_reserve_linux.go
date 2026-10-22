//go:build linux

package lifecycle

import (
	"errors"
	"fmt"
	"os"
	"reflect"

	"golang.org/x/sys/unix"
)

const captureRollbackBytes = 8 << 20

type captureRollbackReserve struct {
	Version int    `json:"version"`
	Token   string `json:"token"`
	Owner   string `json:"owner"`
	Device  uint64 `json:"device"`
	Inode   uint64 `json:"inode"`
	Bytes   int64  `json:"bytes"`
}

// Allocate actual blocks before the source is frozen. This small private
// reserve is for rollback receipts, not a bound on capture/application writes.
func prepareCaptureReserve(anchor *os.File, config captureGuardConfig) error {
	fd, err := unix.Openat(int(anchor.Fd()), ".rollback-reserve", unix.O_CREAT|unix.O_EXCL|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), "capture rollback reserve")
	defer file.Close()
	if err := unix.Fallocate(fd, 0, 0, captureRollbackBytes); err != nil {
		return fmt.Errorf("allocate capture rollback blocks before freeze: %w", err)
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return err
	}
	if stat.Size != captureRollbackBytes || stat.Blocks*512 < captureRollbackBytes || stat.Uid != 0 ||
		stat.Nlink != 1 || stat.Mode&0777 != 0600 || stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return fmt.Errorf("capture rollback reserve is not completely allocated private storage")
	}
	if err := file.Sync(); err != nil {
		return err
	}
	return captureGuardWrite(anchor, "rollback-reserve.json", captureRollbackReserve{Version: 1, Token: config.Token,
		Owner: config.Source.Owner, Device: uint64(stat.Dev), Inode: stat.Ino, Bytes: captureRollbackBytes})
}

// Keep the inode and receipt for exact replay. Truncate only the saved private
// file, never unlink an unknown replacement or free any checkpoint payload.
func releaseCaptureReserve(anchor *os.File, config captureGuardConfig) error {
	var reserve captureRollbackReserve
	if err := captureCompletionRead(anchor, "rollback-reserve.json", &reserve); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// Historical/debug guards have no reserve. An unexplained reserve
			// file is not adopted if its creation receipt is absent.
			var stat unix.Stat_t
			if err := unix.Fstatat(int(anchor.Fd()), ".rollback-reserve", &stat, unix.AT_SYMLINK_NOFOLLOW); errors.Is(err, unix.ENOENT) {
				return nil
			}
		}
		return err
	}
	var intent captureGuardConfig
	if err := captureCompletionRead(anchor, "guard-intent.json", &intent); err != nil {
		return err
	}
	if reserve.Version != 1 || reserve.Token != config.Token || reserve.Owner != config.Source.Owner ||
		reserve.Bytes != captureRollbackBytes || !reflect.DeepEqual(intent, config) {
		return fmt.Errorf("capture rollback reserve belongs to a different guard")
	}
	fd, err := unix.Openat(int(anchor.Fd()), ".rollback-reserve", unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), "original capture rollback reserve")
	defer file.Close()
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return err
	}
	if uint64(stat.Dev) != reserve.Device || stat.Ino != reserve.Inode || stat.Uid != 0 || stat.Nlink != 1 ||
		stat.Mode&0777 != 0600 || stat.Mode&unix.S_IFMT != unix.S_IFREG || (stat.Size != 0 && stat.Size != reserve.Bytes) {
		return fmt.Errorf("capture rollback reserve inode or ownership changed")
	}
	if err := unix.Ftruncate(fd, 0); err != nil {
		return err
	}
	return errors.Join(file.Sync(), anchor.Sync())
}
