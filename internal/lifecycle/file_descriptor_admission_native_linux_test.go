//go:build linux

package lifecycle

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"golang.org/x/sys/unix"
)

func TestDetachedFileAdmissionNative(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_FILE_ADMISSION") != "1" &&
		os.Getenv("FACTORYBOX_NATIVE_FILESYSTEM_ADMISSION") != "1" {
		t.Skip("requires the owned private cgroup/mount fixture")
	}
	root := t.TempDir()
	path := filepath.Join(root, "runc")
	if err := os.WriteFile(path, []byte("original detached file\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"visible", "detached", "memfd"} {
		if !t.Run(name, func(t *testing.T) {
			var fd int
			var err error
			if name == "detached" {
				fd, err = unix.OpenTree(unix.AT_FDCWD, path, unix.OPEN_TREE_CLONE|unix.OPEN_TREE_CLOEXEC)
			} else if name == "memfd" {
				var memory int
				memory, err = unix.MemfdCreate("owned-supported-memfd", unix.MFD_CLOEXEC)
				if err == nil {
					fd, err = unix.Open(fmt.Sprintf("/proc/self/fd/%d", memory), unix.O_PATH|unix.O_CLOEXEC, 0)
					unix.Close(memory)
				}
			} else {
				fd, err = unix.Open(path, unix.O_PATH|unix.O_CLOEXEC, 0)
			}
			if err != nil {
				t.Fatal(err)
			}
			file := os.NewFile(uintptr(fd), "owned single-file handle")
			defer file.Close()
			group, err := os.MkdirTemp("/sys/fs/cgroup", "factorybox-file-admission-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.Remove(group)
			anchor, err := captureGuardOpen(group)
			if err != nil {
				t.Fatal(err)
			}
			defer anchor.Close()
			identity, err := cgroupFDIdentity(int(anchor.Fd()))
			if err != nil {
				t.Fatal(err)
			}
			child := exec.Command("/bin/sleep", "300")
			child.ExtraFiles = []*os.File{file}
			if err := child.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() {
				os.WriteFile(filepath.Join(group, "cgroup.freeze"), []byte("0"), 0600)
				child.Process.Kill()
				child.Wait()
			}()
			if err := os.WriteFile(filepath.Join(group, "cgroup.procs"),
				[]byte(strconv.Itoa(child.Process.Pid)), 0600); err != nil {
				t.Fatal(err)
			}
			config := captureGuardConfig{PID: child.Process.Pid, Source: Record{Cgroup: &identity}}
			blocked, err := captureDescriptorBlockers(config, captureGuardAnchors{group: anchor})
			if err != nil {
				t.Fatal(err)
			}
			if name != "detached" && len(blocked) != 0 {
				t.Fatal("supported file was refused", blocked)
			}
			if name == "detached" && (len(blocked) != 1 || blocked[0].FD != 3 || blocked[0].Kind != "detached-file") {
				t.Fatal("unsupported detached single-file mount entered capture admission", blocked)
			}
		}) {
			return
		}
	}
}
