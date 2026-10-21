//go:build linux

package lifecycle

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestDetachedExecutableAdmissionHelper(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_EXEC_ADMISSION_CHILD") != "1" {
		t.Skip("only the owned executable-admission child")
	}
	unix.CloseOnExec(3)
	if err := unix.Exec("/proc/self/fd/3", []string{"factorybox-mapped-executable", "300"}, os.Environ()); err != nil {
		t.Fatal("execute the original file and close its descriptor", err)
	}
}

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
	for _, name := range []string{"visible", "detached", "memfd", "exec-visible", "exec-detached", "exec-memfd"} {
		if !t.Run(name, func(t *testing.T) {
			var fd int
			var err error
			executable := strings.HasPrefix(name, "exec-")
			input := path
			if executable {
				input = "/bin/sleep"
			}
			if name == "detached" || name == "exec-detached" {
				fd, err = unix.OpenTree(unix.AT_FDCWD, input, unix.OPEN_TREE_CLONE|unix.OPEN_TREE_CLOEXEC)
			} else if name == "memfd" || name == "exec-memfd" {
				var memory int
				memory, err = unix.MemfdCreate("owned-supported-memfd", unix.MFD_CLOEXEC)
				if err == nil {
					if executable {
						data, readErr := os.ReadFile(input)
						if readErr != nil {
							t.Fatal(readErr)
						}
						if count, writeErr := unix.Write(memory, data); writeErr != nil || count != len(data) {
							t.Fatal("copy the actual executable to the owned memfd", count, writeErr)
						}
					}
					fd, err = unix.Open(fmt.Sprintf("/proc/self/fd/%d", memory), unix.O_PATH|unix.O_CLOEXEC, 0)
					unix.Close(memory)
				}
			} else {
				fd, err = unix.Open(input, unix.O_PATH|unix.O_CLOEXEC, 0)
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
			if executable {
				testBinary, err := os.Executable()
				if err != nil {
					t.Fatal(err)
				}
				child = exec.Command(testBinary, "-test.run=^TestDetachedExecutableAdmissionHelper$", "-test.timeout=10s")
				child.Env = append(os.Environ(), "FACTORYBOX_NATIVE_EXEC_ADMISSION_CHILD=1")
				child.Stdout, child.Stderr = os.Stdout, os.Stderr
			}
			child.ExtraFiles = []*os.File{file}
			if err := child.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() {
				os.WriteFile(filepath.Join(group, "cgroup.freeze"), []byte("0"), 0600)
				child.Process.Kill()
				child.Wait()
			}()
			if executable {
				var wanted unix.Statx_t
				if err := unix.Statx(fd, "", unix.AT_EMPTY_PATH, unix.STATX_INO|unix.STATX_MNT_ID, &wanted); err != nil {
					t.Fatal(err)
				}
				ready := false
				for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
					var actual unix.Statx_t
					err := unix.Statx(unix.AT_FDCWD, fmt.Sprintf("/proc/%d/exe", child.Process.Pid), 0,
						unix.STATX_INO|unix.STATX_MNT_ID, &actual)
					_, descriptorErr := os.Stat(fmt.Sprintf("/proc/%d/fd/3", child.Process.Pid))
					if err == nil && actual.Ino == wanted.Ino && actual.Mnt_id == wanted.Mnt_id &&
						actual.Dev_major == wanted.Dev_major && actual.Dev_minor == wanted.Dev_minor &&
						os.IsNotExist(descriptorErr) {
						ready = true
						break
					}
					time.Sleep(time.Millisecond)
				}
				if !ready {
					t.Fatal("original executable did not replace the helper and close its descriptor")
				}
			}
			if err := os.WriteFile(filepath.Join(group, "cgroup.procs"),
				[]byte(strconv.Itoa(child.Process.Pid)), 0600); err != nil {
				t.Fatal(err)
			}
			config := captureGuardConfig{PID: child.Process.Pid, Source: Record{Cgroup: &identity}}
			if err := os.WriteFile(filepath.Join(group, "cgroup.freeze"), []byte("1"), 0600); err != nil {
				t.Fatal(err)
			}
			for deadline := time.Now().Add(2 * time.Second); ; {
				events, err := captureGuardEvents(anchor)
				if err == nil && events["frozen"] == 1 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("owned source did not freeze", err)
				}
				time.Sleep(time.Millisecond)
			}
			if executable {
				var actual unix.Statx_t
				if err := unix.Statx(unix.AT_FDCWD, fmt.Sprintf("/proc/%d/exe", child.Process.Pid), 0,
					unix.STATX_MNT_ID, &actual); err != nil {
					t.Fatal(err)
				}
				threads, err := captureGuardThreads(config, captureGuardAnchors{group: anchor})
				if err != nil {
					t.Fatal(err)
				}
				mounts, err := captureMountIDs(threads)
				if err != nil || mounts[actual.Mnt_id] != (name == "exec-visible") {
					t.Fatal("executable source does not have the intended visible/detached mount", err, actual.Mnt_id)
				}
			}
			blocked, err := captureDescriptorBlockers(config, captureGuardAnchors{group: anchor})
			if err != nil {
				t.Fatal(err)
			}
			if name != "detached" && name != "exec-detached" && len(blocked) != 0 {
				t.Fatal("supported file was refused", blocked)
			}
			if name == "detached" && (len(blocked) != 1 || blocked[0].FD != 3 || blocked[0].Kind != "detached-file") {
				t.Fatal("unsupported detached single-file mount entered capture admission", blocked)
			}
			if name == "exec-detached" &&
				(len(blocked) != 1 || blocked[0].FD != -3 || blocked[0].Kind != "detached-executable") {
				t.Fatal("unsupported executable mapping with no open descriptor entered capture admission", blocked)
			}
		}) {
			return
		}
	}
}
