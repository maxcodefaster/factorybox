//go:build linux

package worker

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"factorybox/internal/storefs"
	"golang.org/x/sys/unix"
)

func TestOrderlyStorageReleaseWaitsForObservedCompletion(t *testing.T) {
	calls := 0
	expected := []string{"original-epoch"}
	completed, err := waitOrderlyStorageRelease(context.Background(), func() ([]string, error) {
		calls++
		if calls < 3 {
			return nil, &orderlyStorageReleasePending{cause: unix.EWOULDBLOCK}
		}
		return expected, nil
	})
	if err != nil || calls != 3 || !reflect.DeepEqual(completed, expected) {
		t.Fatal("orderly pending release did not retain the same observation", completed, calls, err)
	}
}

func TestOrderlyStorageReleaseDoesNotRetryUnknownOwnership(t *testing.T) {
	pending := &orderlyStorageReleasePending{cause: unix.EWOULDBLOCK}
	for _, cause := range []error{unix.EWOULDBLOCK, errors.New("changed image"), errors.New("invalid exit receipt"),
		fmt.Errorf("wrapped observation: %w", pending), errors.Join(pending, errors.New("another failure"))} {
		calls := 0
		_, err := waitOrderlyStorageRelease(context.Background(), func() ([]string, error) {
			calls++
			return nil, cause
		})
		if calls != 1 || err != cause {
			t.Fatal("unknown ownership was turned into orderly release waiting", calls, err)
		}
	}
}

func TestOrderlyStorageReleaseCancellationPublishesNoCompletion(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	completed, err := waitOrderlyStorageRelease(ctx, func() ([]string, error) {
		calls++
		cancel()
		return []string{"not-a-completion"}, &orderlyStorageReleasePending{cause: unix.EWOULDBLOCK}
	})
	if calls != 1 || completed != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled release observation published an outcome", completed, calls, err)
	}
	completed, err = waitOrderlyStorageRelease(ctx, func() ([]string, error) {
		t.Fatal("already cancelled admission observed storage")
		return nil, nil
	})
	if completed != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("already cancelled admission continued", completed, err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	completed, err = waitOrderlyStorageRelease(ctx, func() ([]string, error) {
		cancel()
		return []string{"released-but-cancelled"}, nil
	})
	if completed != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled admission continued after its last storage observation", completed, err)
	}
}

func boundedExitFixture() boundedExit {
	owner := "11111111111111111111111111111111"
	return boundedExit{Schema: 1, StoreID: owner, Epoch: "22222222222222222222222222222222",
		BootID: "12345678-1234-1234-1234-123456789abc", Supervisor: Process{PID: 1, Birth: "123"},
		Namespace: "mnt:[456]", Attachment: storefs.Active{
			Image: storefs.Image{Schema: 1, Owner: owner, UUID: owner, Path: "/private/fixed/store.img",
				Device: 41, Inode: 42, Bytes: 512 << 20},
			Target: "/private/fixed/data", Mount: storefs.Object{Device: unix.Mkdev(7, 3), Inode: 2, MountID: 45},
			LoopNumber: 3, BlockDevice: unix.Mkdev(7, 3), FilesystemMax: 500 << 20, DirectIO: true,
		}}
}

func TestBoundedExitKeepsExactWorkerAndAttachmentIdentity(t *testing.T) {
	original := boundedExitFixture()
	if err := validateBoundedExit("/private/fixed", original.Epoch, original.Attachment.Image, original); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*boundedExit){
		func(value *boundedExit) { value.Schema++ },
		func(value *boundedExit) { value.StoreID = "33333333333333333333333333333333" },
		func(value *boundedExit) { value.Epoch = "33333333333333333333333333333333" },
		func(value *boundedExit) { value.BootID = "unknown" },
		func(value *boundedExit) { value.Supervisor.PID++ },
		func(value *boundedExit) { value.Supervisor.Birth = "" },
		func(value *boundedExit) { value.Namespace = "net:[456]" },
		func(value *boundedExit) { value.Attachment.Image.Inode++ },
		func(value *boundedExit) { value.Attachment.Target += "/changed" },
		func(value *boundedExit) { value.Attachment.LoopNumber++ },
		func(value *boundedExit) { value.Attachment.Mount.MountID = 0 },
		func(value *boundedExit) { value.Attachment.Mount.Device++ },
		func(value *boundedExit) { value.Attachment.FilesystemMax = 1 << 30 },
		func(value *boundedExit) { value.Attachment.DirectIO = false },
	} {
		changed := original
		change(&changed)
		if err := validateBoundedExit("/private/fixed", original.Epoch, original.Attachment.Image, changed); err == nil {
			t.Fatal("changed worker or attachment granted exit completion", changed)
		}
	}
}

func TestBoundedExitDefersOnlyTheExactUnconfirmedAssociation(t *testing.T) {
	active := boundedExitFixture().Attachment
	original := unix.LoopInfo64{Device: active.Image.Device, Inode: active.Image.Inode,
		Sizelimit: uint64(active.Image.Bytes), Number: uint32(active.LoopNumber),
		Flags: unix.LO_FLAGS_AUTOCLEAR | unix.LO_FLAGS_DIRECT_IO}
	pending := &storefs.PendingReleaseError{Active: active, Status: &original}
	if !boundedExitPending(active, pending) {
		t.Fatal("exact pending kernel association was not distinguished")
	}
	for _, failure := range []error{
		nil, errors.New(pending.Error()), fmt.Errorf("other error: %w", pending),
		errors.Join(pending, errors.New("owned process did not stop")),
		&storefs.PendingReleaseError{Active: active},
	} {
		if boundedExitPending(active, failure) {
			t.Fatal("uncertain or additional failure was suppressed", failure)
		}
	}
	for _, change := range []func(*unix.LoopInfo64){
		func(value *unix.LoopInfo64) { value.Device++ },
		func(value *unix.LoopInfo64) { value.Inode++ },
		func(value *unix.LoopInfo64) { value.Offset++ },
		func(value *unix.LoopInfo64) { value.Sizelimit++ },
		func(value *unix.LoopInfo64) { value.Number++ },
		func(value *unix.LoopInfo64) { value.Flags = unix.LO_FLAGS_DIRECT_IO },
	} {
		changed := original
		change(&changed)
		if boundedExitPending(active, &storefs.PendingReleaseError{Active: active, Status: &changed}) {
			t.Fatal("changed kernel association was considered an orderly pending release")
		}
	}
	changed := active
	changed.Image.Inode++
	if boundedExitPending(active, &storefs.PendingReleaseError{Active: changed, Status: &original}) {
		t.Fatal("different attachment was considered the original pending release")
	}
}
