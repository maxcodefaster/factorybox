//go:build linux && amd64

package lifecycle

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

// This diagnostic observes two paths of an actual owned Sysbox guest. Success
// means the observation completed, not that either path has a hard quota.
type guestStorageObservation struct {
	Path          string `json:"path"`
	Filesystem    int64  `json:"filesystem_type"`
	ControlBytes  int    `json:"control_bytes"`
	CreateErrno   int    `json:"create_errno"`
	Created       bool   `json:"subvolume_created"`
	SubvolumeID   uint64 `json:"subvolume_id,omitempty"`
	SubvolumeUUID string `json:"subvolume_uuid,omitempty"`
	WrittenBytes  int    `json:"subvolume_written_bytes"`
	Error         string `json:"error,omitempty"`
}

func storageGuestIOCTL(fd int, number uintptr, data []byte) error {
	// BTRFS_IOC_SUBVOL_CREATE and SNAP_DESTROY use struct btrfs_ioctl_vol_args.
	// GET_SUBVOL_INFO uses the 504-byte btrfs_ioctl_get_subvol_info_args.
	direction := uintptr(1)
	if number == 60 {
		direction = 2
	}
	request := direction<<30 | uintptr(len(data))<<16 | 0x94<<8 | number
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), request, uintptr(unsafe.Pointer(&data[0])))
	runtime.KeepAlive(data)
	if errno != 0 {
		return errno
	}
	return nil
}

func storageGuestSubvolume(path string) (uint64, string, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return 0, "", err
	}
	defer unix.Close(fd)
	data := make([]byte, 504)
	if err := storageGuestIOCTL(fd, 60, data); err != nil {
		return 0, "", err
	}
	return binary.LittleEndian.Uint64(data), hex.EncodeToString(data[296:312]), nil
}

func storageGuestObserve(path, owner string) (result guestStorageObservation) {
	result.Path = path
	err := func() error {
		parent, err := os.Open(path)
		if err != nil {
			return err
		}
		defer parent.Close()
		var fs unix.Statfs_t
		if err := unix.Fstatfs(int(parent.Fd()), &fs); err != nil {
			return err
		}
		result.Filesystem = fs.Type
		// A successful ordinary write prevents permission/setup failure from
		// being mistaken for denial of the Btrfs operation.
		control, err := os.OpenFile(filepath.Join(path, "control-"+owner), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		result.ControlBytes, err = control.Write([]byte(owner))
		if err := errors.Join(err, control.Sync(), control.Close()); err != nil {
			return err
		}
		data := make([]byte, 4096)
		copy(data[8:], "child-"+owner)
		if err := storageGuestIOCTL(int(parent.Fd()), 14, data); err != nil {
			var errno unix.Errno
			if !errors.As(err, &errno) {
				return err
			}
			result.CreateErrno = int(errno)
			return nil
		}
		result.Created = true
		child := filepath.Join(path, "child-"+owner)
		result.SubvolumeID, result.SubvolumeUUID, err = storageGuestSubvolume(child)
		if err != nil {
			return err
		}
		file, err := os.OpenFile(filepath.Join(child, "bounded-data"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		result.WrittenBytes, err = file.Write(make([]byte, 1<<20))
		return errors.Join(err, file.Sync(), file.Close())
	}()
	if err != nil {
		result.Error = err.Error()
	}
	return result
}

func TestNativeStorageGuestFilesystemHelper(t *testing.T) {
	argument := os.Args[len(os.Args)-1]
	const prefix = "factorybox-storage-observe-"
	if !strings.HasPrefix(argument, prefix) || !sandboxID.MatchString(strings.TrimPrefix(argument, prefix)) {
		t.Skip("only the explicitly invoked guest observation helper")
	}
	owner := strings.TrimPrefix(argument, prefix)
	results := []guestStorageObservation{
		storageGuestObserve("/tmp/storage-boundary", owner),
		storageGuestObserve("/quota-bind", owner),
	}
	if err := json.NewEncoder(os.Stdout).Encode(results); err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

func TestNativeStorageGuestFilesystemBoundary(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_STORAGE_GUEST") != "1" {
		t.Skip("requires the explicit owned guest storage diagnostic")
	}
	engine, bundle := nativeEngine(t, "sleep 300")
	owner := filepath.Base(filepath.Dir(bundle))
	guestRoot := filepath.Join(bundle, "rootfs")
	rootParent := filepath.Join(guestRoot, "tmp/storage-boundary")
	bindParent := filepath.Join(filepath.Dir(bundle), "bound-storage")
	for _, path := range []string{rootParent, bindParent} {
		if err := os.MkdirAll(path, 0777); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0777); err != nil {
			t.Fatal(err)
		}
	}
	var fs unix.Statfs_t
	if err := unix.Statfs(bindParent, &fs); err != nil || fs.Type != unix.BTRFS_SUPER_MAGIC {
		t.Fatalf("owned direct bind source must be Btrfs: type=%x: %v", fs.Type, err)
	}
	input, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(guestRoot, "storage-guest-probe"), input, 0755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(bundle + "/config.json")
	if err != nil {
		t.Fatal(err)
	}
	var spec map[string]any
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatal(err)
	}
	spec["mounts"] = append(spec["mounts"].([]any), map[string]any{
		"destination": "/quota-bind", "type": "none", "source": bindParent,
		"options": []string{"bind", "rw"},
	})
	data, err = json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundle+"/config.json", data, 0600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	source, err := engine.Start(ctx, "storage-observation", bundle)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := engine.Delete(ctx, source.Record.ID); err != nil {
			t.Errorf("delete exact guest: %v", err)
		}
	})
	lock, err := engine.lock()
	if err != nil {
		t.Fatal(err)
	}
	output, execErr := engine.command(ctx, lock, "exec", source.Record.RuntimeID, "/storage-guest-probe",
		"-test.run=^TestNativeStorageGuestFilesystemHelper$", "-test.timeout=20s", "factorybox-storage-observe-"+owner)
	lockErr := lock.Close()
	if execErr != nil || lockErr != nil {
		t.Fatalf("guest observation did not execute: %s: %v", output, errors.Join(execErr, lockErr))
	}
	var observations []guestStorageObservation
	if err := json.Unmarshal(output, &observations); err != nil || len(observations) != 2 {
		t.Fatalf("guest observation incomplete: %s: %v", output, err)
	}
	for index, observation := range observations {
		if observation.Error != "" || observation.ControlBytes != len(owner) ||
			(observation.Created && (observation.SubvolumeID <= 256 || observation.WrittenBytes != 1<<20)) ||
			(!observation.Created && observation.CreateErrno == 0) {
			t.Fatalf("missing ordinary write or bounded observation: %+v", observation)
		}
		parent := []string{rootParent, bindParent}[index]
		if observation.Path != []string{"/tmp/storage-boundary", "/quota-bind"}[index] {
			t.Fatal("guest observation path changed")
		}
		if observation.Created {
			child := filepath.Join(parent, "child-"+owner)
			id, uuid, err := storageGuestSubvolume(child)
			if err != nil || id != observation.SubvolumeID || uuid != observation.SubvolumeUUID {
				t.Fatalf("refusing cleanup of changed guest subvolume: %v", err)
			}
			fd, err := unix.Open(parent, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
			if err != nil {
				t.Fatal(err)
			}
			data := make([]byte, 4096)
			copy(data[8:], "child-"+owner)
			deleteErr := storageGuestIOCTL(fd, 15, data)
			closeErr := unix.Close(fd)
			if err := errors.Join(deleteErr, closeErr); err != nil {
				t.Fatalf("delete verified own guest subvolume: %v", err)
			}
			if _, err := os.Lstat(child); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("deleted exact guest subvolume still exists: %v", err)
			}
		}
		if control, err := os.ReadFile(filepath.Join(parent, "control-"+owner)); err != nil || string(control) != owner {
			t.Fatalf("guest's ordinary write is not visible at its own source: %v", err)
		}
	}
	proof := map[string]any{"schema": 1, "passed": true, "owner": owner, "observations": observations,
		"exact_created_subvolumes_removed": true, "production_qualified": false,
		"scope": "Bounded actual Sysbox guest observation on its overlay root and an explicitly supplied own Btrfs bind. No quota or complete storage boundary qualification."}
	data, err = json.MarshalIndent(proof, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("/output/storage-guest.json", append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("actual guest storage observations: %s", output)
}
