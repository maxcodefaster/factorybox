//go:build linux

package testtrace

import (
	"errors"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

type Cgroup struct {
	Device, Inode, MountID uint64
}

// HoldCure stops the pinned retained helper at its first source-freezer open
// after accepting the original guard's grant. The caller checks birth,
// executable and transaction identities and holds its OS thread throughout.
// Only the test helper is traced; no production hook or instruction changes.
func HoldCure(t *testing.T, helper, pidfd int, source Cgroup, resumeGuard func()) func() {
	t.Helper()
	fd, err := unix.Dup(pidfd)
	if err != nil {
		t.Fatal(err)
	}
	waiter := New("original cure helper did not reach its authorized syscall boundary")
	attached := false
	finish := func() {
		if attached {
			if err := unix.PtraceDetach(helper); err != nil && !errors.Is(err, unix.ESRCH) {
				t.Error("detach only the original retained cure helper", err)
			}
			attached = false
		}
		if fd >= 0 {
			unix.Close(fd)
			fd = -1
			waiter.Close()
		}
	}
	t.Cleanup(finish)
	held := false
	defer func() {
		if !held {
			finish()
		}
	}()
	if err := unix.PtraceSeize(helper); err != nil {
		t.Fatal("trace only the original retained helper", err)
	}
	attached = true
	if err := unix.PtraceInterrupt(helper); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	status, err := waiter.Wait(helper, deadline)
	if err != nil || !status.Stopped() {
		t.Fatal("retained helper did not enter its initial trace stop", err, status)
	}
	if err := unix.PtraceSetOptions(helper, unix.PTRACE_O_TRACESYSGOOD); err != nil {
		t.Fatal(err)
	}
	resumeGuard()
	for {
		deliver := int(status.StopSignal())
		if deliver == int(unix.SIGTRAP) || deliver == int(unix.SIGTRAP)|0x80 {
			deliver = 0
		}
		if err := unix.PtraceSyscall(helper, deliver); err != nil {
			t.Fatal(err)
		}
		status, err = waiter.Wait(helper, deadline)
		if err != nil || !status.Stopped() {
			t.Fatal("retained helper left before authorized cure entry", err, status)
		}
		var info struct {
			Operation uint8
			Padding   [3]byte
			Arch      uint32
			IP, SP    uint64
			Number    uint64
			Args      [6]uint64
		}
		_, _, errno := unix.Syscall6(unix.SYS_PTRACE, unix.PTRACE_GET_SYSCALL_INFO,
			uintptr(helper), unsafe.Sizeof(info), uintptr(unsafe.Pointer(&info)), 0, 0)
		if errno != 0 {
			t.Fatal("read actual helper syscall boundary", errno)
		}
		if info.Operation != unix.PTRACE_SYSCALL_INFO_ENTRY || info.Number != unix.SYS_OPENAT {
			continue
		}
		var name [32]byte
		if _, err := unix.PtracePeekData(helper, uintptr(info.Args[1]), name[:]); err != nil {
			t.Fatal("read only the owned helper's syscall filename", err)
		}
		if string(name[:14]) != "cgroup.freeze\x00" {
			continue
		}
		pinned, err := unix.PidfdGetfd(fd, int(info.Args[0]), 0)
		if err != nil {
			t.Fatal("pin actual cure target directory", err)
		}
		var stat unix.Stat_t
		var filesystem unix.Statfs_t
		var extended unix.Statx_t
		statErr := unix.Fstat(pinned, &stat)
		fsErr := unix.Fstatfs(pinned, &filesystem)
		mountErr := unix.Statx(pinned, "", unix.AT_EMPTY_PATH, unix.STATX_MNT_ID, &extended)
		unix.Close(pinned)
		if err := errors.Join(statErr, fsErr, mountErr); err != nil ||
			filesystem.Type != unix.CGROUP2_SUPER_MAGIC || extended.Mask&unix.STATX_MNT_ID == 0 ||
			extended.Mnt_id == 0 || source != (Cgroup{uint64(stat.Dev), stat.Ino, extended.Mnt_id}) {
			t.Fatal("cure helper targeted a different source group", err)
		}
		held = true
		return finish
	}
}
