//go:build linux

package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"time"

	"factorybox/internal/storefs"
	"golang.org/x/sys/unix"
)

var exitEpoch = regexp.MustCompile(`^[a-f0-9]{32}$`)
var mountNamespace = regexp.MustCompile(`^mnt:\[[1-9][0-9]*\]$`)
var processBirth = regexp.MustCompile(`^[1-9][0-9]*$`)
var bootIdentity = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)

type boundedExit struct {
	Schema     int            `json:"schema"`
	StoreID    string         `json:"store_id"`
	Epoch      string         `json:"epoch"`
	BootID     string         `json:"boot_id"`
	Supervisor Process        `json:"supervisor"`
	Namespace  string         `json:"mount_namespace"`
	Attachment storefs.Active `json:"attachment"`
}

type BoundedStorageRelease struct {
	StoreID               string        `json:"store_id"`
	Image                 storefs.Image `json:"image"`
	KernelReleaseVerified bool          `json:"kernel_release_verified"`
	CompletedEpochs       []string      `json:"completed_epochs"`
}

type orderlyStorageReleasePending struct{ cause error }

func (failure *orderlyStorageReleasePending) Error() string { return failure.cause.Error() }
func (failure *orderlyStorageReleasePending) Unwrap() error { return failure.cause }

// An accepted predecessor can leave the exact autoclear association alive until
// its mount namespace's final kernel reference closes. Observe that release
// before attaching. No readiness, new epoch, mount or completion is published
// while it remains held. Unknown ownership failures still return immediately.
func waitOrderlyStorageRelease(ctx context.Context, observe func() ([]string, error)) ([]string, error) {
	announced := false
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		completed, err := observe()
		if cause := ctx.Err(); cause != nil {
			return nil, cause
		}
		if _, exact := err.(*orderlyStorageReleasePending); !exact {
			return completed, err
		}
		if !announced {
			fmt.Fprintln(os.Stderr, "Waiting for previous orderly storage release")
			announced = true
		}
		timer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func boundedExitDirectory(root, epoch string) (string, error) {
	if !exitEpoch.MatchString(epoch) || epoch == "00000000000000000000000000000000" {
		return "", fmt.Errorf("bounded exit requires its original nonzero epoch")
	}
	return root + "/worker-exits/" + epoch, nil
}

func validateBoundedExit(root, epoch string, image storefs.Image, plan boundedExit) error {
	active := plan.Attachment
	if plan.Schema != 1 || plan.StoreID != image.Owner || plan.Epoch != epoch ||
		!bootIdentity.MatchString(plan.BootID) || plan.Supervisor.PID != 1 || !processBirth.MatchString(plan.Supervisor.Birth) ||
		!mountNamespace.MatchString(plan.Namespace) || active.Image != image ||
		image.Path != root+"/store.img" || active.Target != root+"/data" ||
		storefs.ValidateAttachmentIdentity(active) != nil {
		return fmt.Errorf("bounded exit differs from its original worker or attachment")
	}
	return nil
}

func readBoundedExit(path string) (boundedExit, error) {
	var raw json.RawMessage
	var plan boundedExit
	if err := readJSON(path, &raw); err != nil {
		return plan, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&plan)
	return plan, err
}

func publishBoundedExit(directory, name string, plan boundedExit) error {
	path := directory + "/" + name
	existing, err := readBoundedExit(path)
	if err == nil {
		if !reflect.DeepEqual(existing, plan) {
			return fmt.Errorf("bounded exit receipt changed: %s", name)
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return writeNew(path, plan)
}

func boundedExitPending(active storefs.Active, failure error) bool {
	pending, exact := failure.(*storefs.PendingReleaseError)
	if !exact || pending.Active != active || pending.Status == nil {
		return false
	}
	actual := pending.Status
	return actual.Device == active.Image.Device && actual.Inode == active.Image.Inode &&
		actual.Offset == 0 && actual.Sizelimit == uint64(active.Image.Bytes) &&
		int(actual.Number) == active.LoopNumber &&
		actual.Flags&(unix.LO_FLAGS_AUTOCLEAR|unix.LO_FLAGS_DIRECT_IO) == unix.LO_FLAGS_AUTOCLEAR|unix.LO_FLAGS_DIRECT_IO
}

func prepareBoundedExit(root string, status Status, active storefs.Active) (boundedExit, string, error) {
	namespace, err := os.Readlink("/proc/self/ns/mnt")
	if err != nil {
		return boundedExit{}, "", err
	}
	plan := boundedExit{Schema: 1, StoreID: status.StoreID, Epoch: status.Epoch, BootID: status.BootID,
		Supervisor: status.Supervisor, Namespace: namespace, Attachment: active}
	if err := validateBoundedExit(root, status.Epoch, active.Image, plan); err != nil {
		return plan, "", err
	}
	directory, err := boundedExitDirectory(root, status.Epoch)
	if err != nil {
		return plan, "", err
	}
	for _, path := range []string{root + "/worker-exits", directory} {
		if err := makePrivate(path); err != nil {
			return plan, "", err
		}
	}
	return plan, directory, publishBoundedExit(directory, "intent.json", plan)
}

// Only an accepted, otherwise successful worker shutdown may hand its final
// namespace reference to the container's teardown. All other Close errors fail.
// The pending receipt is deliberately not a kernel-release completion receipt.
func closeBoundedForExit(attachment *storefs.Attachment, root string, status Status, accepted bool, prior error) error {
	var plan boundedExit
	var directory string
	if accepted && prior == nil {
		var err error
		plan, directory, err = prepareBoundedExit(root, status, attachment.Active)
		if err != nil {
			prior = err
		}
	}
	closeErr := attachment.Close()
	if directory != "" && prior == nil {
		if closeErr == nil {
			return publishBoundedExit(directory, "released.json", plan)
		}
		if boundedExitPending(attachment.Active, closeErr) {
			if err := publishBoundedExit(directory, "namespace-exit-required.json", plan); err == nil {
				// PID 1 now exits. A separate observer must still prove release;
				// next admission performs the same exact check before mounting.
				return nil
			} else {
				closeErr = errors.Join(closeErr, err)
			}
		}
	}
	return errors.Join(prior, closeErr)
}

func completeBoundedExits(root string, image storefs.Image) ([]string, error) {
	home := root + "/worker-exits"
	if _, err := os.Lstat(home); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	if err := private(home); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		return nil, err
	}
	type pendingExit struct {
		plan      boundedExit
		directory string
	}
	var pending []pendingExit
	var completed []string
	orderly := true
	for _, entry := range entries {
		directory, err := boundedExitDirectory(root, entry.Name())
		if err != nil {
			return nil, err
		}
		if err := private(directory); err != nil {
			return nil, err
		}
		plan, err := readBoundedExit(directory + "/intent.json")
		if err != nil {
			return nil, err
		}
		if err := validateBoundedExit(root, entry.Name(), image, plan); err != nil {
			return nil, err
		}
		namespaceExitRequired := false
		for _, name := range []string{"namespace-exit-required.json", "released.json"} {
			receipt, err := readBoundedExit(directory + "/" + name)
			if err == nil {
				if !reflect.DeepEqual(plan, receipt) {
					return nil, fmt.Errorf("bounded exit outcome changed")
				}
				if name == "namespace-exit-required.json" {
					namespaceExitRequired = true
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
		}
		if _, err := os.Lstat(directory + "/released.json"); errors.Is(err, os.ErrNotExist) {
			pending = append(pending, pendingExit{plan, directory})
			orderly = orderly && namespaceExitRequired
		} else if err != nil {
			return nil, err
		} else {
			completed = append(completed, plan.Epoch)
		}
	}
	if len(pending) == 0 {
		return completed, nil
	}
	if err := storefs.VerifyReleased(image); err != nil {
		if orderly && errors.Is(err, unix.EWOULDBLOCK) {
			return nil, &orderlyStorageReleasePending{cause: err}
		}
		return nil, err
	}
	for _, item := range pending {
		if err := publishBoundedExit(item.directory, "released.json", item.plan); err != nil {
			return completed, err
		}
		completed = append(completed, item.plan.Epoch)
	}
	return completed, nil
}

// ReconcileBoundedStorage runs outside the stopped worker's namespace. It keeps
// the original image, refuses live ownership/associations, and records only
// positively observed release. It never unmounts, detaches or deletes backing.
func ReconcileBoundedStorage(root, storeID string) (BoundedStorageRelease, error) {
	result := BoundedStorageRelease{StoreID: storeID, CompletedEpochs: []string{}}
	if os.Geteuid() != 0 {
		return result, fmt.Errorf("bounded storage reconciliation requires worker root")
	}
	if err := private(root); err != nil {
		return result, err
	}
	lease, err := openLease(root, false)
	if err != nil {
		return result, err
	}
	defer lease.Close()
	if err := unix.Flock(int(lease.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return result, fmt.Errorf("bounded store still has a worker: %w", err)
	}
	var identity boundedIdentity
	if err := readJSON(root+"/bounded.json", &identity); err != nil {
		return result, err
	}
	if identity.Schema != 1 || identity.Image.Owner != storeID || identity.Image.Path != root+"/store.img" {
		return result, fmt.Errorf("bounded storage reconciliation owner changed")
	}
	if err := storefs.VerifyReleased(identity.Image); err != nil {
		return result, err
	}
	result.Image = identity.Image
	completed, err := completeBoundedExits(root, identity.Image)
	if err != nil {
		return result, err
	}
	result.CompletedEpochs = append(result.CompletedEpochs, completed...)
	result.KernelReleaseVerified = true
	return result, nil
}
