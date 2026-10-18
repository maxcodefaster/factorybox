package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestDescendantFileLeaseNative(t *testing.T) {
	if os.Getenv("FILE_LEASE_NATIVE") != "1" {
		t.Skip("isolated Btrfs ARM64 native adapter test")
	}
	signal.Ignore(syscall.SIGIO)
	if os.Getenv("FILE_LEASE_ACTOR") == "1" {
		if os.Getenv("FILE_LEASE_DROP_CAPABILITY") == "1" {
			runtime.LockOSThread()
			header := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
			var capabilities [2]unix.CapUserData
			if err := unix.Capget(&header, &capabilities[0]); err != nil {
				t.Fatal(err)
			}
			mask := uint32(1) << uint(unix.CAP_LEASE%32)
			capabilities[unix.CAP_LEASE/32].Effective &^= mask
			capabilities[unix.CAP_LEASE/32].Permitted &^= mask
			if err := unix.Capset(&header, &capabilities[0]); err != nil {
				t.Fatal(err)
			}
		}
		lease, err := descendantAcquireFileLease(os.Getenv("FILE_LEASE_PATH"))
		ready := os.NewFile(3, "lease actor ready")
		defer ready.Close()
		if os.Getenv("FILE_LEASE_DROP_CAPABILITY") == "1" {
			if lease != nil || (!errors.Is(err, unix.EPERM) && !errors.Is(err, unix.EACCES)) ||
				!strings.Contains(err.Error(), "acquire read lease:") {
				t.Fatal("mapped owner admitted without capability", lease, err)
			}
			fmt.Fprintln(ready, "denied")
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		defer lease.close()
		fmt.Fprintln(ready, "leased")
		ready.Close()
		for {
			time.Sleep(time.Hour)
		}
	}
	backing := func(t *testing.T) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "backing")
		if err := os.WriteFile(path, bytes.Repeat([]byte("native-page-data"), 4096), 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	startActor := func(t *testing.T, path string, drop bool) *exec.Cmd {
		t.Helper()
		read, write, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		defer read.Close()
		command := exec.Command(os.Args[0], "-test.run=^TestDescendantFileLeaseNative$")
		command.Env = append(os.Environ(), "FILE_LEASE_ACTOR=1", "FILE_LEASE_PATH="+path)
		expected := "leased\n"
		if drop {
			command.Env = append(command.Env, "FILE_LEASE_DROP_CAPABILITY=1")
			expected = "denied\n"
		}
		command.ExtraFiles = []*os.File{write}
		var diagnostic bytes.Buffer
		command.Stdout, command.Stderr = &diagnostic, &diagnostic
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		write.Close()
		t.Cleanup(func() { command.Process.Kill(); command.Wait() })
		text, err := bufio.NewReader(read).ReadString('\n')
		if err != nil || text != expected {
			command.Wait()
			t.Fatal("lease actor failed", text, err, diagnostic.String())
		}
		return command
	}
	rejectWriter := func(t *testing.T, path string) {
		t.Helper()
		fd, err := unix.Open(path, unix.O_WRONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
		if err == nil {
			unix.Close(fd)
			t.Fatal("writer bypassed read lease")
		}
		if !errors.Is(err, unix.EAGAIN) {
			t.Fatal("writer failed for a different reason", err)
		}
	}
	t.Run("existing_writer_and_mapping_rejected", func(t *testing.T) {
		path := backing(t)
		writer, err := os.OpenFile(path, os.O_RDWR, 0)
		if err != nil {
			t.Fatal(err)
		}
		lease, err := descendantAcquireFileLease(path)
		if lease != nil || !errors.Is(err, unix.EAGAIN) {
			t.Fatal("writer admitted", lease, err)
		}
		mapping, err := unix.Mmap(int(writer.Fd()), 0, 4096, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
		if err != nil {
			t.Fatal(err)
		}
		defer unix.Munmap(mapping)
		writer.Close()
		lease, err = descendantAcquireFileLease(path)
		if lease != nil || !errors.Is(err, unix.EAGAIN) {
			t.Fatal("shared writable mapping admitted", lease, err)
		}
		unix.Munmap(mapping)
		lease, err = descendantAcquireFileLease(path)
		if err != nil {
			t.Fatal(err)
		}
		defer lease.close()
	})
	t.Run("alias_write_invalidates_without_changing_bytes", func(t *testing.T) {
		path := backing(t)
		if err := os.Link(path, path+"-alias"); err != nil {
			t.Fatal(err)
		}
		lease, err := descendantAcquireFileLease(path)
		if err != nil {
			t.Fatal(err)
		}
		defer lease.close()
		rejectWriter(t, path+"-alias")
		if err := lease.verify(); err == nil {
			t.Fatal("pending break retained certificate")
		}
		checksum, err := descendantLeaseHash(lease.File)
		if err != nil || checksum != lease.SHA256 {
			t.Fatal("blocked writer changed bytes", err)
		}
	})
	t.Run("metadata_and_replaced_path_rejected", func(t *testing.T) {
		for _, replace := range []bool{false, true} {
			path := backing(t)
			lease, err := descendantAcquireFileLease(path)
			if err != nil {
				t.Fatal(err)
			}
			defer lease.close()
			if replace {
				if err := os.Rename(path, path+"-original"); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("different inode"), 0600); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Chmod(path, 0640); err != nil {
				t.Fatal(err)
			}
			if err := lease.verify(); err == nil {
				t.Fatal("changed backing retained proof")
			}
		}
	})
	t.Run("actual_holder_crash_releases", func(t *testing.T) {
		path := backing(t)
		command := startActor(t, path, false)
		rejectWriter(t, path)
		if err := command.Process.Kill(); err != nil {
			t.Fatal(err)
		}
		var exited *exec.ExitError
		if err := command.Wait(); !errors.As(err, &exited) ||
			exited.ProcessState.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
			t.Fatal("holder did not terminate through SIGKILL", err)
		}
		file, err := os.OpenFile(path, os.O_WRONLY, 0)
		if err != nil {
			t.Fatal("holder death retained lease", err)
		}
		file.Close()
	})
	t.Run("mapped_owner_capability_rejected_and_admitted", func(t *testing.T) {
		path := backing(t)
		if err := os.Chmod(path, 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chown(path, 165536, 165536); err != nil {
			t.Fatal(err)
		}
		command := startActor(t, path, true)
		if err := command.Wait(); err != nil {
			t.Fatal(err)
		}
		lease, err := descendantAcquireFileLease(path)
		if err != nil {
			t.Fatal("explicit CAP_LEASE did not admit mapped owner", err)
		}
		defer lease.close()
	})
	t.Run("protected_manifest_reuse_and_invalidation", func(t *testing.T) {
		layout, err := descendantOwnedLayout("branch-0123456789abcdef0123456789abcdef")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Lstat(layout.Root); !os.IsNotExist(err) {
			t.Fatal("fixture must own an absent root")
		}
		t.Cleanup(func() { os.RemoveAll(layout.Root) })
		images := layout.Root + "/images-fedcba9876543210fedcba9876543210"
		if err := os.MkdirAll(images, 0700); err != nil {
			t.Fatal(err)
		}
		payload := map[string][]byte{
			"inventory.img": []byte("inventory"), "pstree.img": []byte("owned processes"),
			"runtime-config.json": []byte(`{"id":"owned"}`), "descriptors.json": []byte(`["/dev/null"]`),
			"pages-1.img": bytes.Repeat([]byte("full-original-writer"), 1<<18),
		}
		expected := map[string]string{}
		for name, data := range payload {
			path := filepath.Join(images, name)
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			expected[name] = fmt.Sprintf("%x", sha256.Sum256(data))
			if err := os.Chown(path, 165536, 165536); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Chown(images, 165536, 165536); err != nil {
			t.Fatal(err)
		}
		if _, err := descendantLeaseAdmission(layout); err != nil {
			t.Fatal(err)
		}
		protection, err := descendantProtectBacking(layout, images, expected)
		if err != nil {
			t.Fatal(err)
		}
		defer protection.close(false)
		for range 2 {
			got, err := descendantSnapshotManifestProtected(images, protection)
			if err != nil || !reflect.DeepEqual(got, expected) {
				t.Fatal("protected manifest differs from original complete writer", got, err)
			}
		}
		if len(protection.VerifyRuns) != 2 {
			t.Fatal("missing complete verification gates")
		}
		for _, gate := range protection.VerifyRuns {
			if gate["kernel_certified_page_bytes"] != int64(len(payload["pages-1.img"])) ||
				gate["metadata_hashed_bytes"].(int64) >= 4096 || gate["passed"] != true {
				t.Fatal("payload was not certified while metadata remained hashed", gate)
			}
		}
		rejectWriter(t, images+"/pages-1.img")
		if _, err := descendantSnapshotManifestProtected(images, protection); err == nil {
			t.Fatal("breaking protection silently fell back or retained reused digest")
		}
		if err := protection.close(false); err != nil {
			t.Fatal(err)
		}
		got, err := descendantSnapshotManifest(images)
		if err != nil || !reflect.DeepEqual(got, expected) {
			t.Fatal("complete original-byte oracle changed after rejected writer", err)
		}
	})
}
