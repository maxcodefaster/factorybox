package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestCommittedCaptureAllowsProtectedKernelClone(t *testing.T) {
	if os.Getenv("PIPE_STORE_REFLINK_NATIVE") != "1" {
		t.Skip("one owned Btrfs fixture")
	}
	store, connection, done, file, pipe, source := captureFixtureBackend(t, false, true)
	original := file.Name()
	var filesystem unix.Statfs_t
	if err := unix.Statfs(original, &filesystem); err != nil || filesystem.Type != unix.BTRFS_SUPER_MAGIC {
		t.Fatal("native capture/lease composition requires Btrfs", err)
	}
	if err := sendFixture(connection, captureMessage{op: 1, a: 7, b: uint64(source.Process.Pid),
		fds: []int{int(file.Fd())}}); err != nil {
		t.Fatal(err)
	}
	if err := sendFixture(connection, captureMessage{op: 2, b: 8192, c: 2, fds: []int{pipe[0]},
		spans: []captureSpan{{0, 4096, 0, 0}, {4096, 4096, 4096, 1}}}); err != nil {
		t.Fatal(err)
	}
	if err := file.Sync(); err != nil {
		t.Fatal(err)
	}
	premature, err := os.Open(original)
	if err != nil {
		t.Fatal(err)
	}
	_, prematureErr := unix.FcntlInt(premature.Fd(), unix.F_SETLEASE, unix.F_RDLCK)
	if prematureErr == nil {
		unix.FcntlInt(premature.Fd(), unix.F_SETLEASE, unix.F_UNLCK)
	}
	premature.Close()
	if !errors.Is(prematureErr, unix.EAGAIN) {
		t.Fatal("source protection admitted an original writer that was still open", prematureErr)
	}
	// CRIU closes its original writer before the controller protects the
	// capture. The actual keeper's reopened golden descriptor remains live.
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := sendFixture(connection, captureMessage{op: 3, a: 8192}); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(store.images) != 1 {
		t.Fatal("native capture was not committed")
	}
	image := store.images[0]
	flags, err := unix.FcntlInt(uintptr(image.reader.baseFD), unix.F_GETFL, 0)
	if err != nil || flags&unix.O_ACCMODE != unix.O_RDONLY {
		t.Fatal("committed capture kept a writable original descriptor", flags, err)
	}
	held, err := os.Open(original)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	if _, err := unix.FcntlInt(held.Fd(), unix.F_SETLEASE, unix.F_RDLCK); err != nil {
		t.Fatal("keeper's real descriptor prevented source protection", err)
	}
	leased := true
	defer func() {
		if leased {
			unix.FcntlInt(held.Fd(), unix.F_SETLEASE, unix.F_UNLCK)
		}
	}()
	target := filepath.Join(filepath.Dir(original), "independent-clone")
	clone, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer clone.Close()
	if err := unix.IoctlFileClone(int(clone.Fd()), int(held.Fd())); err != nil {
		t.Fatal("protected committed capture could not be cloned", err)
	}
	if err := clone.Sync(); err != nil {
		t.Fatal(err)
	}
	kind, err := unix.FcntlInt(held.Fd(), unix.F_GETLEASE, 0)
	if err != nil || kind != unix.F_RDLCK {
		t.Fatal("kernel clone broke the original read lease", kind, err)
	}
	a, aErr := held.Stat()
	b, bErr := clone.Stat()
	if aErr != nil || bErr != nil || os.SameFile(a, b) || a.Size() != b.Size() {
		t.Fatal("kernel clone reused the original inode", aErr, bErr)
	}
	expected := append(bytes.Repeat([]byte{17}, 4096), bytes.Repeat([]byte{29}, 4096)...)
	for _, reader := range []*os.File{held, clone} {
		actual := make([]byte, len(expected))
		if n, err := reader.ReadAt(actual, 0); err != nil || n != len(actual) || !bytes.Equal(actual, expected) {
			t.Fatal("independent original-writer byte oracle differs", n, err)
		}
	}
	if _, err := clone.WriteAt([]byte("independent clone mutation"), 0); err != nil {
		t.Fatal(err)
	}
	retained := make([]byte, len(expected))
	if n, err := image.reader.ReadAt(retained, 0); err != nil || n != len(retained) ||
		!bytes.Equal(retained, expected) {
		t.Fatal("clone mutation changed the actual keeper's retained bytes", n, err)
	}
	if _, err := unix.FcntlInt(held.Fd(), unix.F_SETLEASE, unix.F_UNLCK); err != nil {
		t.Fatal(err)
	}
	leased = false
	if err := source.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = source.Wait()
	proof, err := store.finish()
	if err != nil || proof["active_capture_credit_bytes"] != int64(0) ||
		proof["active_capture_credit_descriptors"] != 0 {
		t.Fatal("capture/reflink composition did not retire exact ownership", err)
	}
	t.Log("actual SCM_RIGHTS keeper capture, read-only golden FD, F_RDLCK, FICLONE, complete original bytes, clone mutation and credit retirement passed")
}
