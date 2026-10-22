//go:build linux

package storefs

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

type Object struct {
	Device  uint64 `json:"device"`
	Inode   uint64 `json:"inode"`
	MountID uint64 `json:"mount_id"`
}

type Active struct {
	Image         Image  `json:"image"`
	Target        string `json:"target"`
	Mount         Object `json:"mount"`
	LoopNumber    int    `json:"loop_number"`
	BlockDevice   uint64 `json:"block_device"`
	FilesystemMax uint64 `json:"filesystem_capacity_bytes"`
	DirectIO      bool   `json:"direct_io"`
}

type deviceNode struct {
	path string
	stat unix.Stat_t
}

func newNode(number int) (deviceNode, error) {
	var node deviceNode
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return node, err
	}
	node.path = "/dev/factorybox-store-" + hex.EncodeToString(nonce[:])
	dev := unix.Mkdev(7, uint32(number))
	if err := unix.Mknod(node.path, unix.S_IFBLK|0600, int(dev)); err != nil {
		return node, err
	}
	if err := unix.Lstat(node.path, &node.stat); err != nil || uint64(node.stat.Rdev) != dev ||
		node.stat.Mode&unix.S_IFMT != unix.S_IFBLK || node.stat.Uid != 0 || node.stat.Nlink != 1 {
		return node, fmt.Errorf("created private block node could not be identified: %v", err)
	}
	return node, nil
}

func (node deviceNode) remove() error {
	var actual unix.Stat_t
	if err := unix.Lstat(node.path, &actual); err != nil || actual.Dev != node.stat.Dev || actual.Ino != node.stat.Ino ||
		actual.Rdev != node.stat.Rdev || actual.Mode != node.stat.Mode || actual.Uid != node.stat.Uid {
		return fmt.Errorf("refusing removal of changed private block node: %v", err)
	}
	return unix.Unlink(node.path)
}

// No existing association is adopted. Inspection uses temporary private nodes
// and never configures or detaches one of the inspected devices.
func rejectExistingAssociation(image Image) error {
	entries, err := os.ReadDir("/sys/block")
	if err != nil {
		return err
	}
	count := 0
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "loop") {
			continue
		}
		number, err := strconv.Atoi(strings.TrimPrefix(entry.Name(), "loop"))
		if err != nil || number < 0 || number > 1<<20 {
			return fmt.Errorf("invalid loop inventory")
		}
		count++
		if count > 4096 {
			return fmt.Errorf("loop inventory exceeds the inspection bound")
		}
		node, err := newNode(number)
		if err != nil {
			return err
		}
		fd, err := unix.Open(node.path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			return errors.Join(err, node.remove())
		}
		info, inspectErr := unix.IoctlLoopGetStatus64(fd)
		cleanupErr := errors.Join(unix.Close(fd), node.remove())
		if cleanupErr != nil {
			return cleanupErr
		}
		if errors.Is(inspectErr, unix.ENXIO) {
			continue
		}
		if inspectErr != nil {
			return inspectErr
		}
		if info.Device == image.Device && info.Inode == image.Inode {
			return fmt.Errorf("fixed image already has a loop association; adoption refused")
		}
	}
	return nil
}

func targetIdentity(path string) (Object, uint64, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return Object{}, 0, err
	}
	defer unix.Close(fd)
	var stat unix.Stat_t
	var extended unix.Statx_t
	var fs unix.Statfs_t
	if err := errors.Join(unix.Fstat(fd, &stat), unix.Fstatfs(fd, &fs),
		unix.Statx(fd, "", unix.AT_EMPTY_PATH, unix.STATX_MNT_ID, &extended)); err != nil {
		return Object{}, 0, err
	}
	if extended.Mask&unix.STATX_MNT_ID == 0 || extended.Mnt_id == 0 || fs.Bsize <= 0 ||
		fs.Blocks > ^uint64(0)/uint64(fs.Bsize) {
		return Object{}, 0, fmt.Errorf("fixed mount identity or capacity unavailable")
	}
	return Object{Device: uint64(stat.Dev), Inode: stat.Ino, MountID: extended.Mnt_id}, fs.Blocks * uint64(fs.Bsize), nil
}

type Attachment struct {
	Active     Active
	image      *os.File
	loop       int
	node       deviceNode
	unmounted  bool
	loopClosed bool
	closed     bool
}

func sameLoop(fd int, image Image) error {
	info, err := unix.IoctlLoopGetStatus64(fd)
	if err != nil {
		return err
	}
	if info.Device != image.Device || info.Inode != image.Inode || info.Offset != 0 ||
		info.Sizelimit != uint64(image.Bytes) ||
		info.Flags&(unix.LO_FLAGS_AUTOCLEAR|unix.LO_FLAGS_DIRECT_IO) != unix.LO_FLAGS_AUTOCLEAR|unix.LO_FLAGS_DIRECT_IO {
		return fmt.Errorf("loop association no longer matches the fixed image")
	}
	return nil
}

// Attach takes an exclusive lock on the exact image and mounts it once. It
// never formats, resizes, adopts or detaches an existing association.
func Attach(expected Image, target string) (_ *Attachment, resultErr error) {
	if os.Geteuid() != 0 {
		return nil, fmt.Errorf("fixed filesystem attachment requires worker root")
	}
	if err := privateDirectory(target); err != nil {
		return nil, err
	}
	parent, _, err := targetIdentity(filepath.Dir(target))
	if err != nil {
		return nil, err
	}
	before, _, err := targetIdentity(target)
	if err != nil || before.MountID != parent.MountID {
		return nil, fmt.Errorf("refusing an existing target mount: %v", err)
	}
	entries, err := os.ReadDir(target)
	if err != nil || len(entries) != 0 {
		return nil, fmt.Errorf("fixed filesystem target must be empty: %v", err)
	}
	image, err := openImage(expected)
	if err != nil {
		return nil, err
	}
	attachment := &Attachment{image: image, loop: -1, Active: Active{Image: expected, Target: target}}
	configured, mounted := false, false
	defer func() {
		if resultErr == nil {
			return
		}
		if mounted {
			actual, _, err := targetIdentity(target)
			if err != nil || actual != attachment.Active.Mount {
				resultErr = errors.Join(resultErr, fmt.Errorf("changed target during attachment failure, refusing unmount: %v", err))
			} else {
				resultErr = errors.Join(resultErr, unix.Unmount(target, 0))
			}
		}
		if configured {
			resultErr = errors.Join(resultErr, sameLoop(attachment.loop, expected))
		}
		if attachment.loop >= 0 {
			resultErr = errors.Join(resultErr, unix.Close(attachment.loop))
		}
		if attachment.node.path != "" {
			resultErr = errors.Join(resultErr, attachment.node.remove())
		}
		resultErr = errors.Join(resultErr, image.Close())
	}()
	if err := unix.Flock(int(image.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return nil, fmt.Errorf("fixed image already has an owner: %w", err)
	}
	if err := rejectExistingAssociation(expected); err != nil {
		return nil, err
	}
	control, err := unix.Open("/dev/loop-control", unix.O_RDWR|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	number, allocErr := unix.IoctlRetInt(control, unix.LOOP_CTL_GET_FREE)
	closeErr := unix.Close(control)
	if err := errors.Join(allocErr, closeErr); err != nil {
		return nil, err
	}
	if number < 0 || number > 1<<20 {
		return nil, fmt.Errorf("allocated loop number outside the bounded range")
	}
	attachment.node, err = newNode(number)
	if err != nil {
		return nil, err
	}
	attachment.loop, err = unix.Open(attachment.node.path, unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	config := unix.LoopConfig{Fd: uint32(image.Fd()), Size: 4096,
		Info: unix.LoopInfo64{Sizelimit: uint64(expected.Bytes), Flags: unix.LO_FLAGS_AUTOCLEAR | unix.LO_FLAGS_DIRECT_IO}}
	copy(config.Info.File_name[:], "factorybox-"+expected.Owner)
	if err := unix.IoctlLoopConfigure(attachment.loop, &config); err != nil {
		return nil, err // An EBUSY occupant is neither altered nor detached.
	}
	configured = true
	if err := sameLoop(attachment.loop, expected); err != nil {
		return nil, err
	}
	attachment.Active.LoopNumber = number
	attachment.Active.BlockDevice = unix.Mkdev(7, uint32(number))
	attachment.Active.DirectIO = true
	if err := unix.Mount(attachment.node.path, target, "ext4", unix.MS_NODEV, "data=ordered,nodiscard"); err != nil {
		return nil, err
	}
	mounted = true
	attachment.Active.Mount, attachment.Active.FilesystemMax, err = targetIdentity(target)
	if err != nil || attachment.Active.Mount.Device != attachment.Active.BlockDevice ||
		attachment.Active.FilesystemMax == 0 || attachment.Active.FilesystemMax > uint64(expected.Bytes) {
		return nil, fmt.Errorf("mounted fixed filesystem identity or capacity differs: %v", err)
	}
	if err := os.Chmod(target, 0700); err != nil {
		return nil, err
	}
	if err := attachment.Check(); err != nil {
		return nil, err
	}
	return attachment, nil
}

func (attachment *Attachment) Check() error {
	if attachment.closed || attachment.unmounted || attachment.loopClosed {
		return fmt.Errorf("fixed filesystem attachment is closed")
	}
	expected := attachment.Active.Image
	current, err := openImage(expected)
	if err != nil {
		return err
	}
	if err := current.Close(); err != nil {
		return err
	}
	if err := sameLoop(attachment.loop, expected); err != nil {
		return err
	}
	return verifyMount(attachment.Active)
}

func verifyMount(active Active) error {
	if !active.DirectIO || active.LoopNumber < 0 || active.LoopNumber > 1<<20 ||
		active.BlockDevice != unix.Mkdev(7, uint32(active.LoopNumber)) || active.Mount.Device != active.BlockDevice ||
		active.Mount.Inode == 0 || active.Mount.MountID == 0 || active.FilesystemMax == 0 ||
		active.FilesystemMax > uint64(active.Image.Bytes) {
		return fmt.Errorf("invalid active fixed filesystem identity")
	}
	if err := privateDirectory(active.Target); err != nil {
		return err
	}
	actual, capacity, err := targetIdentity(active.Target)
	if err != nil || actual != active.Mount || capacity != active.FilesystemMax {
		return fmt.Errorf("fixed filesystem mount changed: %v", err)
	}
	return nil
}

// Verify inspects an already owned attachment without adopting or changing it.
// The image lock must still be held by another open file description. Worker
// publication must additionally authenticate the live process holding that lease.
func Verify(active Active) (resultErr error) {
	if os.Geteuid() != 0 {
		return fmt.Errorf("fixed filesystem verification requires worker root")
	}
	image, err := openImage(active.Image)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, image.Close()) }()
	if err := unix.Flock(int(image.Fd()), unix.LOCK_EX|unix.LOCK_NB); err == nil {
		return fmt.Errorf("fixed filesystem no longer has an image owner")
	} else if !errors.Is(err, unix.EWOULDBLOCK) {
		return err
	}
	if err := verifyMount(active); err != nil {
		return err
	}
	node, err := newNode(active.LoopNumber)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, node.remove()) }()
	fd, err := unix.Open(node.path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, unix.Close(fd)) }()
	return sameLoop(fd, active.Image)
}

// Close refuses a changed target or association and never uses LOOP_CLR_FD.
// Its image lock is released only after unmount and exact autoclear completion.
func (attachment *Attachment) Close() error {
	if attachment.closed {
		return nil
	}
	if !attachment.unmounted {
		if err := attachment.Check(); err != nil {
			return err
		}
		if err := unix.Unmount(attachment.Active.Target, 0); err != nil {
			return err
		}
		attachment.unmounted = true
	}
	if !attachment.loopClosed {
		if err := sameLoop(attachment.loop, attachment.Active.Image); err != nil {
			return err
		}
		if err := unix.Close(attachment.loop); err != nil {
			return err
		}
		attachment.loopClosed = true
	}
	// The final kernel reference can outlive unmount/close briefly, including
	// with direct I/O. Observe bounded completion without detaching anything.
	deadline := time.Now().Add(500 * time.Millisecond)
	for {
		fd, err := unix.Open(attachment.node.path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			return err
		}
		info, statusErr := unix.IoctlLoopGetStatus64(fd)
		released := errors.Is(statusErr, unix.ENXIO) || (statusErr == nil &&
			(info.Device != attachment.Active.Image.Device || info.Inode != attachment.Active.Image.Inode))
		if err := unix.Close(fd); err != nil {
			return err
		}
		if released {
			break
		}
		if statusErr != nil || !time.Now().Before(deadline) {
			return fmt.Errorf("original loop release is unconfirmed; no association detached: status=%+v error=%v", info, statusErr)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := attachment.node.remove(); err != nil {
		return err
	}
	attachment.closed = true
	return attachment.image.Close()
}
