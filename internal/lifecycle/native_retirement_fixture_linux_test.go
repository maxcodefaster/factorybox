//go:build linux

package lifecycle

import (
	"os"
	"testing"
	"time"

	"factorybox/internal/storefs"
)

func closeNativeRetirementStore(t *testing.T, attachment *storefs.Attachment) error {
	t.Helper()
	deadline, bounded := t.Deadline()
	for {
		err := attachment.Close()
		pending, exact := err.(*storefs.PendingReleaseError)
		if !exact || !bounded || pending.Active != attachment.Active || pending.Status == nil ||
			!time.Now().Before(deadline) {
			return err
		}
		// The production release contract distinguishes pending autoclear
		// from failure. Observe this same attachment under the fixture's
		// existing deadline; never detach, adopt or fabricate release.
	}
}

// The native fixture renames a deleted runtime's bundle to test inode
// protection. Sysbox observes that rename asynchronously and destroys its old
// manager volumes. Observe that actual withdrawal before adding the fixture's
// deliberately retained manager payload; otherwise Sysbox, not Factorybox,
// can remove the test data while the preservation assertion is running.
func awaitNativeManagerWithdrawal(t *testing.T, root, runtimeID string) {
	t.Helper()
	deadline, ok := t.Deadline()
	if !ok {
		t.Fatal("native fixture requires its existing lifecycle deadline")
	}
	for {
		absent := true
		for _, kind := range retiredManagerKinds {
			_, err := os.Lstat(root + "/" + kind + "/" + runtimeID)
			if err == nil {
				absent = false
			} else if !os.IsNotExist(err) {
				t.Fatal("observe original Sysbox manager withdrawal", err)
			}
		}
		if absent {
			return
		}
		if !time.Now().Before(deadline) {
			t.Fatal("Sysbox did not withdraw its original manager volumes")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
