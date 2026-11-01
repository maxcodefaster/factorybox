//go:build linux

package storefs

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

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
