package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
	"runtime-fork-experiment/pageview"
)

func deferredFixture(t *testing.T) (*captureStore, *os.File) {
	return deferredFixtureRoot(t, false)
}

func deferredFixtureRoot(t *testing.T, ownTmpfs bool) (*captureStore, *os.File) {
	return deferredFixtureBackend(t, ownTmpfs, false)
}

func deferredFixtureBackend(t *testing.T, ownTmpfs, rawBase bool) (*captureStore, *os.File) {
	t.Helper()
	store, connection, done, file, pipe, source := captureFixtureBackend(t, ownTmpfs, rawBase)
	// The actual raw image has a hole instead of the first private page.
	// The transferred FIFO remains the only source of its nonzero bytes.
	if err := unix.Fallocate(int(file.Fd()), unix.FALLOC_FL_PUNCH_HOLE|unix.FALLOC_FL_KEEP_SIZE, 0, 4096); err != nil {
		t.Fatal(err)
	}
	if err := unix.Fsetxattr(int(file.Fd()), deferredImageAttribute, []byte{1}, 0); err != nil {
		t.Fatal(err)
	}
	if err := sendFixture(connection, captureMessage{op: 1, a: 7, b: uint64(source.Process.Pid), c: 1, fds: []int{int(file.Fd())}}); err != nil {
		t.Fatal(err)
	}
	if err := sendFixture(connection, captureMessage{op: 2, b: 8192, c: 2, fds: []int{pipe[0]}, spans: []captureSpan{
		{0, 4096, 0, 6}, {4096, 4096, 4096, 1},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := sendFixture(connection, captureMessage{op: 3, a: 8192}); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	_ = source.Process.Kill()
	_ = source.Wait()
	t.Cleanup(func() {
		for _, image := range store.images {
			store.discard(image)
		}
	})
	return store, file
}

func TestDeferredRawHolesUseActualLogicalReadersAndExport(t *testing.T) {
	store, file := deferredFixture(t)
	raw := make([]byte, 8192)
	if _, err := file.ReadAt(raw, 0); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw[:4096], make([]byte, 4096)) {
		t.Fatal("raw private bytes were copied")
	}
	socket := filepath.Join(t.TempDir(), "read.sock")
	service, err := startReadService(store, socket, true)
	if err != nil {
		t.Fatal(err)
	}
	nativeReadCommand(t, socket, file.Name(), "normal")
	view, err := pageview.OpenAt(socket, file)
	if err != nil || view == nil {
		t.Fatalf("logical view: %v", err)
	}
	// Read unaligned through the real transferred FIFO and eager memfd.
	span := make([]byte, 29)
	if n, err := view.ReadAt(span, 4083); err != nil || n != 29 ||
		!bytes.Equal(span[:13], bytes.Repeat([]byte{17}, 13)) ||
		!bytes.Equal(span[13:], bytes.Repeat([]byte{29}, 16)) {
		t.Fatal("logical cross-span read differs")
	}
	expected := append(bytes.Repeat([]byte{17}, 4096), bytes.Repeat([]byte{29}, 4096)...)
	target, err := os.CreateTemp(filepath.Dir(store.output), "complete-export-")
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	if n, err := io.Copy(target, view); err != nil || n != 8192 {
		t.Fatalf("export: %d %v", n, err)
	}
	if err := view.Close(); err != nil {
		t.Fatal(err)
	}
	if err := view.Close(); err != nil {
		t.Fatal(err)
	}
	if err := target.Sync(); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, 8192)
	if _, err := target.ReadAt(got, 0); err != nil || !bytes.Equal(got, expected) {
		t.Fatal("complete export differs")
	}
	digest := sha256.Sum256(expected)
	if store.images[0].goldenHash != hex.EncodeToString(digest[:]) || store.images[0].deferredBytes != 4096 {
		t.Fatal("logical publication hash/count differs")
	}
	marked, err := pageview.Marked(target)
	if err != nil || marked {
		t.Fatal("complete export retained mandatory marker")
	}
	proof, err := service.stop()
	if err != nil {
		t.Fatalf("reader leases: %v %+v", err, proof)
	}
	if proof["active_client_leases"] != 0 || proof["active_client_descriptors"] != 0 {
		t.Fatal("logical client leaked")
	}
}

func TestDeferredMissingBrokerOrIdentityNeverReturnsRawHoles(t *testing.T) {
	for _, mode := range []string{"absent", "missing-token", "modified", "copied-token", "invalid-marker"} {
		t.Run(mode, func(t *testing.T) {
			store, file := deferredFixture(t)
			socket := filepath.Join(t.TempDir(), "read.sock")
			if mode == "absent" {
				if view, err := pageview.OpenAt(socket, file); err == nil || view != nil {
					t.Fatal("missing broker returned raw holes")
				}
				nativeReadCommand(t, socket, file.Name(), "reject")
				return
			}
			switch mode {
			case "missing-token":
				if err := unix.Fremovexattr(int(file.Fd()), readImageAttribute); err != nil {
					t.Fatal(err)
				}
			case "modified":
				time.Sleep(time.Millisecond)
				if _, err := file.WriteAt([]byte{211}, 7000); err != nil {
					t.Fatal(err)
				}
			case "copied-token":
				other, err := os.CreateTemp(filepath.Dir(file.Name()), "foreign-deferred-")
				if err != nil {
					t.Fatal(err)
				}
				defer other.Close()
				if err := other.Truncate(8192); err != nil {
					t.Fatal(err)
				}
				for attribute, value := range map[string][]byte{readImageAttribute: store.images[0].token[:], deferredImageAttribute: {1}} {
					if err := unix.Fsetxattr(int(other.Fd()), attribute, value, 0); err != nil {
						t.Fatal(err)
					}
				}
				file = other
			case "invalid-marker":
				if err := unix.Fsetxattr(int(file.Fd()), deferredImageAttribute, []byte{7}, 0); err != nil {
					t.Fatal(err)
				}
			}
			service, err := startReadService(store, socket, false)
			if err != nil {
				t.Fatal(err)
			}
			if view, err := pageview.OpenAt(socket, file); err == nil || view != nil {
				t.Fatal("unusable deferred identity returned raw holes")
			}
			nativeReadCommand(t, socket, file.Name(), "reject")
			proof, stopErr := service.stop()
			if stopErr == nil || proof["active_client_leases"] != 0 {
				t.Fatalf("bad identity accounting: %v %+v", stopErr, proof)
			}
		})
	}
}

func TestDeferredRetirementWaitsForRealReadersAndReleasesPins(t *testing.T) {
	store, file := deferredFixture(t)
	socket := filepath.Join(t.TempDir(), "read.sock")
	service, err := startReadService(store, socket, true)
	if err != nil {
		t.Fatal(err)
	}
	view, err := pageview.OpenAt(socket, file)
	if err != nil {
		t.Fatal(err)
	}
	backing, err := os.CreateTemp(filepath.Dir(store.output), "retirement-disk-")
	if err != nil {
		t.Fatal(err)
	}
	defer backing.Close()
	if _, err := io.Copy(backing, view); err != nil {
		t.Fatal(err)
	}
	if err := backing.Sync(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- pageview.RetireAt(socket, file, backing) }()
	select {
	case err := <-done:
		t.Fatalf("retired with a live logical reader: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	if err := view.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := pageview.RetireAt(socket, file, backing); err != nil {
		t.Fatal("idempotent retirement failed", err)
	}
	image := store.images[0]
	if !image.retired || image.active != 0 {
		t.Fatal("retirement retained active references")
	}
	if _, err := image.reader.ReadAt(make([]byte, 4096), 0); err == nil {
		t.Fatal("retired pin remained readable")
	}
	proof, err := service.stop()
	if err != nil {
		t.Fatalf("retirement leases: %v %+v", err, proof)
	}
}

func TestRetirementAckHasAlreadyClosedActualSCMRightsImageReferences(t *testing.T) {
	store, file := deferredFixture(t)
	backing, err := os.CreateTemp(filepath.Dir(store.output), "ack-complete-disk-")
	if err != nil {
		t.Fatal(err)
	}
	defer backing.Close()
	expected := append(bytes.Repeat([]byte{17}, 4096), bytes.Repeat([]byte{29}, 4096)...)
	if _, err := backing.Write(expected); err != nil {
		t.Fatal(err)
	}
	if err := backing.Sync(); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(t.TempDir(), "retirement.sock")
	listener, err := net.ListenUnix("unixpacket", &net.UnixAddr{Name: socket, Net: "unixpacket"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	type result struct {
		fds []int
		err error
	}
	done := make(chan result, 1)
	go func() {
		connection, err := listener.AcceptUnix()
		if err != nil {
			done <- result{err: err}
			return
		}
		defer connection.Close()
		message, err := receiveCapture(connection)
		if err == nil {
			err = (&pipeReadService{store: store}).retire(connection, message)
		}
		done <- result{message.fds, err}
	}()
	if err := pageview.RetireAt(socket, file, backing); err != nil {
		t.Fatal(err)
	}
	got := <-done
	if got.err != nil || len(got.fds) != 2 {
		t.Fatal(got.err)
	}
	for _, fd := range got.fds {
		var stat unix.Stat_t
		if err := unix.Fstat(fd, &stat); !errors.Is(err, unix.EBADF) {
			_ = unix.Close(fd)
			t.Error("retirement ACK preceded received original/backing descriptor closure")
		}
	}
	// The independent sender references must remain valid.
	if _, err := file.Stat(); err != nil {
		t.Fatal(err)
	}
	if _, err := backing.Stat(); err != nil {
		t.Fatal(err)
	}
}
