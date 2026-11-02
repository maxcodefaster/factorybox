//go:build linux

package storefs

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

// AttachmentPendingError identifies one exact original kernel association.
// It grants no mount, adoption, detach, release receipt or readiness. A caller
// already holding the outer worker lease may observe its eventual withdrawal.
type AttachmentPendingError struct {
	Image          Image
	LoopNumber     int
	Status         unix.LoopInfo64
	imageOwnerHeld bool
}

func (failure *AttachmentPendingError) Error() string {
	if failure.imageOwnerHeld {
		return "fixed image already has an owner; original kernel association is still attached"
	}
	return "fixed image already has a loop association; adoption refused"
}

func (failure *AttachmentPendingError) Unwrap() error { return unix.EWOULDBLOCK }

func originalAssociation(image Image, number int, status *unix.LoopInfo64) *AttachmentPendingError {
	if status == nil || number < 0 || status.Number != uint32(number) ||
		status.Device != image.Device || status.Inode != image.Inode || status.Offset != 0 ||
		status.Sizelimit != uint64(image.Bytes) ||
		status.Flags != unix.LO_FLAGS_AUTOCLEAR|unix.LO_FLAGS_DIRECT_IO ||
		status.Encrypt_type != 0 || status.Encrypt_key_size != 0 ||
		strings.TrimRight(string(status.File_name[:]), "\x00") != "factorybox-"+image.Owner {
		return nil
	}
	return &AttachmentPendingError{Image: image, LoopNumber: number, Status: *status}
}

// PendingReleaseError means the exact mount was unmounted and this attachment's
// loop descriptor was closed, but the original kernel association still exists.
// It is not a release receipt and never authorizes reuse, deletion or detachment.
type PendingReleaseError struct {
	Active Active
	Status *unix.LoopInfo64
}

func (failure *PendingReleaseError) Error() string {
	return fmt.Sprintf("original loop release is unconfirmed; no association detached: status=%+v error=<nil>", failure.Status)
}

// VerifyReleased verifies the original image, obtains its exclusive lease and
// refuses every remaining loop association to that exact inode. It neither
// adopts a device nor changes kernel state.
func VerifyReleased(expected Image) (resultErr error) {
	if os.Geteuid() != 0 {
		return fmt.Errorf("fixed filesystem release verification requires worker root")
	}
	image, err := openImage(expected)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, image.Close()) }()
	if err := unix.Flock(int(image.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return fmt.Errorf("fixed filesystem still has an image owner: %w", err)
	}
	return rejectExistingAssociation(expected)
}
