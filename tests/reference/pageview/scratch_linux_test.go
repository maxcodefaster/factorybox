package pageview

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync"
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

func scratchFixture(t *testing.T) (*pipeFile, []byte) {
	t.Helper()
	const chunk = 1 << 20
	data := make([]byte, 4*chunk)
	for i := range data {
		data[i] = byte((i*17 + i/31) % 251)
	}
	copy(data[2*chunk:3*chunk], data[:chunk])
	base, err := unix.MemfdCreate("owned-reader-sparse-base", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { unix.Close(base) })
	if err := unix.Ftruncate(base, int64(len(data))); err != nil {
		t.Fatal(err)
	}
	// Private chunks are deliberately absent from the raw eager file.
	for _, offset := range []int{chunk, 3 * chunk} {
		if n, err := unix.Pwrite(base, data[offset:offset+chunk], int64(offset)); err != nil || n != chunk {
			t.Fatal("complete eager write", n, err)
		}
	}
	if _, err := unix.FcntlInt(uintptr(base), unix.F_ADD_SEALS,
		unix.F_SEAL_WRITE|unix.F_SEAL_GROW|unix.F_SEAL_SHRINK|unix.F_SEAL_SEAL); err != nil {
		t.Fatal(err)
	}
	var pipe [2]int
	if err := unix.Pipe2(pipe[:], unix.O_CLOEXEC|unix.O_NONBLOCK); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { unix.Close(pipe[0]); unix.Close(pipe[1]) })
	if _, err := unix.FcntlInt(uintptr(pipe[1]), unix.F_SETPIPE_SZ, chunk); err != nil {
		t.Fatal(err)
	}
	if n, err := unix.Write(pipe[1], data[:chunk]); err != nil || n != chunk {
		t.Fatal("complete private pipe write", n, err)
	}
	file, err := newPipeFile(base, int64(len(data)), []spanSpec{
		{0, chunk, pipe[0], 0}, {chunk, chunk, -1, 0},
		{2 * chunk, chunk, pipe[0], 0}, {3 * chunk, chunk, -1, 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { file.Close() })
	return file, data
}

func scratchFDs(t *testing.T) map[int]fileIdentity {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	result := map[int]fileIdentity{}
	for _, entry := range entries {
		var fd int
		if _, err := fmt.Sscan(entry.Name(), &fd); err != nil {
			continue
		}
		key, _, err := identity(fd)
		if err == nil {
			result[fd] = key
		}
	}
	return result
}

func requireNative(t *testing.T) {
	t.Helper()
	if os.Getenv("DESCENDANT_READER_NATIVE") != "1" {
		t.Skip("owned native Linux test container only")
	}
}

func denyScratchAllocation(t *testing.T, operation string) {
	t.Helper()
	filter := []unix.SockFilter{
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 0},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, Jf: 1, K: unix.SYS_PIPE2},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO | uint32(unix.ENOMEM)},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ALLOW},
	}
	if operation == "resize" {
		filter = []unix.SockFilter{
			{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 0},
			{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, Jf: 3, K: unix.SYS_FCNTL},
			{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 24},
			{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, Jf: 1, K: unix.F_SETPIPE_SZ},
			{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO | uint32(unix.ENOMEM)},
			{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ALLOW},
		}
	}
	program := unix.SockFprog{Len: uint16(len(filter)), Filter: &filter[0]}
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := unix.Prctl(unix.PR_SET_SECCOMP, unix.SECCOMP_MODE_FILTER,
		uintptr(unsafe.Pointer(&program)), 0, 0); err != nil {
		t.Fatal(err)
	}
	runtime.KeepAlive(filter)
	var err error
	if operation == "resize" {
		var pipe [2]int
		if err := unix.Pipe2(pipe[:], unix.O_CLOEXEC); err != nil {
			t.Fatal(err)
		}
		_, err = unix.FcntlInt(uintptr(pipe[1]), unix.F_SETPIPE_SZ, 1<<20)
		unix.Close(pipe[0])
		unix.Close(pipe[1])
	} else {
		var pipe [2]int
		err = unix.Pipe2(pipe[:], unix.O_CLOEXEC)
		if err == nil {
			unix.Close(pipe[0])
			unix.Close(pipe[1])
		}
	}
	if !errors.Is(err, unix.ENOMEM) {
		t.Fatal("test did not inject exact scratch allocation error", err)
	}
}

func TestReaderWarmExportSurvivesScratchAllocationFailure(t *testing.T) {
	requireNative(t)
	for _, operation := range []string{"pipe2", "resize"} {
		t.Run(operation, func(t *testing.T) {
			// The filter applies only to this test thread. Let Go destroy the
			// thread when this goroutine exits, so it is never reused.
			runtime.LockOSThread()
			file, data := scratchFixture(t)
			buffer := make([]byte, len(data))
			if n, err := file.ReadAt(buffer, 0); err != nil || n != len(data) || !bytes.Equal(buffer, data) {
				t.Fatal("initial complete logical export differs", n, err)
			}
			denyScratchAllocation(t, operation)
			for repetition := 0; repetition < 16; repetition++ {
				clear(buffer)
				if n, err := file.ReadAt(buffer, 0); err != nil || n != len(data) || !bytes.Equal(buffer, data) {
					t.Fatalf("warm complete export needs fresh %s allocation: repetition=%d bytes=%d error=%v",
						operation, repetition, n, err)
				}
			}
		})
	}
}

func TestReaderScratchReuseAndOwnership(t *testing.T) {
	requireNative(t)
	file, data := scratchFixture(t)
	before := scratchFDs(t)
	buffer := make([]byte, 4096)
	for i := 0; i < 256; i++ {
		offset := (i * 31337) % (1<<20 - len(buffer))
		if n, err := file.ReadAt(buffer, int64(offset)); err != nil || n != len(buffer) ||
			!bytes.Equal(buffer, data[offset:offset+len(buffer)]) {
			t.Fatal("private random read differs", i, n, err)
		}
	}
	cached := map[int]fileIdentity{}
	for fd, key := range scratchFDs(t) {
		if before[fd] != key {
			cached[fd] = key
		}
	}
	if len(cached) != 3 {
		t.Fatalf("reader did not reuse one owned pipe pair and null FD: retained=%d want=3", len(cached))
	}
	for i := 0; i < 256; i++ {
		if _, err := file.ReadAt(buffer, int64((i*37139)%(1<<20-len(buffer)))); err != nil {
			t.Fatal(err)
		}
	}
	for fd, key := range cached {
		current, _, err := identity(fd)
		if err != nil || current != key {
			t.Fatal("scratch descriptor replaced instead of reused")
		}
	}
	var group sync.WaitGroup
	failures := make(chan error, 4)
	for worker := 0; worker < 4; worker++ {
		group.Add(1)
		go func(worker int) {
			defer group.Done()
			buffer := make([]byte, len(data))
			for repetition := 0; repetition < 8; repetition++ {
				if n, err := file.ReadAt(buffer, 0); err != nil || n != len(data) || !bytes.Equal(buffer, data) {
					failures <- fmt.Errorf("concurrent scratch reader %d differs: %d %v", worker, n, err)
					return
				}
			}
		}(worker)
	}
	group.Wait()
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	owned := map[int]fileIdentity{}
	for fd, key := range scratchFDs(t) {
		if before[fd] != key {
			owned[fd] = key
		}
	}
	if len(owned) < 3 || len(owned) > 12 || len(owned)%3 != 0 {
		t.Fatal("scratch descriptors exceed four bounded windows", len(owned))
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	for fd := range owned {
		if _, _, err := identity(fd); err != unix.EBADF {
			t.Fatal("closed reader retained owned scratch FD", fd, err)
		}
	}
}

func TestReaderScratchRejectsContaminationAndForeignFD(t *testing.T) {
	requireNative(t)
	for _, mode := range []string{"contaminated", "replaced"} {
		t.Run(mode, func(t *testing.T) {
			file, data := scratchFixture(t)
			before := scratchFDs(t)
			buffer := make([]byte, 4096)
			if _, err := file.ReadAt(buffer, 8192); err != nil {
				t.Fatal(err)
			}
			cached := map[int]fileIdentity{}
			for fd, key := range scratchFDs(t) {
				if before[fd] != key {
					cached[fd] = key
				}
			}
			readFD, writeFD := -1, -1
			for fd := range cached {
				_, kind, err := identity(fd)
				if err != nil {
					t.Fatal(err)
				}
				if kind != unix.S_IFIFO {
					continue
				}
				flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
				if err != nil {
					t.Fatal(err)
				}
				if flags&unix.O_ACCMODE == unix.O_RDONLY {
					readFD = fd
				} else {
					writeFD = fd
				}
			}
			if readFD < 0 || writeFD < 0 {
				t.Fatal("actual cached pipe pair absent")
			}
			if mode == "contaminated" {
				if _, err := unix.Write(writeFD, []byte{193}); err != nil {
					t.Fatal(err)
				}
			} else {
				foreign, err := unix.MemfdCreate("owned-foreign-scratch-control", unix.MFD_CLOEXEC)
				if err != nil {
					t.Fatal(err)
				}
				defer unix.Close(foreign)
				if err := unix.Dup3(foreign, readFD, unix.O_CLOEXEC); err != nil {
					t.Fatal(err)
				}
			}
			if n, err := file.ReadAt(buffer, 12288); err == nil || n != 0 {
				t.Fatal("invalid scratch was silently reused", n, err)
			}
			for fd := range cached {
				_, _, err := identity(fd)
				if mode == "replaced" && fd == readFD {
					if err != nil {
						t.Fatal("foreign replacement descriptor was closed", err)
					}
					if err := unix.Close(fd); err != nil {
						t.Fatal(err)
					}
				} else if !errors.Is(err, unix.EBADF) {
					t.Fatal("failed scratch was retained", fd, err)
				}
			}
			if n, err := file.ReadAt(buffer, 16384); err != nil || n != len(buffer) ||
				!bytes.Equal(buffer, data[16384:16384+len(buffer)]) {
				t.Fatal("reader failed to rebuild private scratch", n, err)
			}
		})
	}
}
