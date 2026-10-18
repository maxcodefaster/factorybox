package main

import (
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func committedReadFixture(t *testing.T) (*captureStore, *os.File) {
	t.Helper()
	store, connection, done, file, pipe, source := captureFixture(t)
	if err := sendFixture(connection, captureMessage{op: 1, a: 7, b: uint64(source.Process.Pid), fds: []int{int(file.Fd())}}); err != nil {
		t.Fatal(err)
	}
	if err := sendFixture(connection, captureMessage{op: 2, b: 8192, c: 2, fds: []int{pipe[0]}, spans: []captureSpan{
		{0, 4096, 0, 0}, {4096, 4096, 4096, 1},
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

func nativeReadCommand(t *testing.T, socket, file, mode string) []byte {
	t.Helper()
	binary := os.Getenv("PIPE_READ_NATIVE")
	if binary == "" {
		t.Fatal("actual C reader binary required")
	}
	command := exec.Command(binary, socket, file, mode)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("actual C reader mode %s failed: %v %s", mode, err, output)
	}
	return output
}

func TestActualCReaderAndBrokerLeases(t *testing.T) {
	for _, mode := range []string{"normal", "fork", "die", "drained", "replaced", "short"} {
		t.Run(mode, func(t *testing.T) {
			store, file := committedReadFixture(t)
			socket := filepath.Join(t.TempDir(), "read.sock")
			service, err := startReadService(store, socket, true)
			if err != nil {
				t.Fatal(err)
			}
			nativeReadCommand(t, socket, file.Name(), mode)
			proof, stopErr := service.stop()
			bad := mode == "drained" || mode == "replaced" || mode == "short"
			if (stopErr != nil) != bad {
				t.Fatalf("native reader error accounting differs: %v %+v", stopErr, proof)
			}
			if proof["active_client_leases"] != 0 || proof["active_client_descriptors"] != 0 {
				t.Fatal("actual reader lease credits remained")
			}
			if !bad {
				if proof["calls"].(uint64) == 0 || proof["private_pipe_bytes"].(uint64) == 0 ||
					proof["eager_bytes"].(uint64) == 0 {
					t.Fatal("actual C reader did not traverse both real backing lanes")
				}
				lease := proof["leases"].([]map[string]any)[0]
				if mode == "fork" && (lease["explicit_closes"] != uint64(2) || lease["scratch_fork_resets"].(uint64) == 0) {
					t.Fatal("actual fork did not reset its private scratch pipe or close both descriptor copies")
				}
				if mode == "die" && lease["client_process_retired_without_explicit_close"] != true {
					t.Fatal("process-exit lease closure was not actually observed")
				}
			}
		})
	}
}

func TestCapturedIdentityChangesFailClosedToNormalFileReader(t *testing.T) {
	for _, change := range []string{"plain", "modified", "copied-token"} {
		t.Run(change, func(t *testing.T) {
			store, file := committedReadFixture(t)
			if change == "plain" {
				if err := unix.Fremovexattr(int(file.Fd()), readImageAttribute); err != nil {
					t.Fatal(err)
				}
			} else if change == "modified" {
				time.Sleep(time.Millisecond)
				if _, err := file.WriteAt([]byte{231}, 37); err != nil {
					t.Fatal(err)
				}
			} else {
				other, err := os.CreateTemp(filepath.Dir(file.Name()), "foreign-image-")
				if err != nil {
					t.Fatal(err)
				}
				defer other.Close()
				if err := other.Truncate(8192); err != nil {
					t.Fatal(err)
				}
				if err := unix.Fsetxattr(int(other.Fd()), readImageAttribute, store.images[0].token[:], 0); err != nil {
					t.Fatal(err)
				}
				file = other
			}
			socket := filepath.Join(t.TempDir(), "read.sock")
			service, err := startReadService(store, socket, false)
			if err != nil {
				t.Fatal(err)
			}
			nativeReadCommand(t, socket, file.Name(), "miss")
			proof, err := service.stop()
			if err != nil || proof["misses"] != uint64(1) || len(proof["leases"].([]map[string]any)) != 0 {
				t.Fatalf("foreign/changed snapshot identity was served: %v %+v", err, proof)
			}
		})
	}
}

func TestActualCConstructorRejectsBadWireAndClosesReceivedFDs(t *testing.T) {
	for _, bad := range []string{"gap", "overlap", "writer", "unsealed", "missing", "end-id", "fd-bound"} {
		t.Run(bad, func(t *testing.T) {
			store, file := committedReadFixture(t)
			image := store.images[0]
			socket := filepath.Join(t.TempDir(), "read.sock")
			listener, err := net.ListenUnix("unixpacket", &net.UnixAddr{Name: socket, Net: "unixpacket"})
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			done := make(chan error, 1)
			go func() {
				connection, err := listener.AcceptUnix()
				if err != nil {
					done <- err
					return
				}
				defer connection.Close()
				query, err := receiveCapture(connection)
				if err != nil {
					done <- err
					return
				}
				for _, fd := range query.fds {
					_ = unix.Close(fd)
				}
				fds := []int{image.reader.baseFD}
				var pipe *pipeLease
				for _, candidate := range image.reader.pipes {
					pipe = candidate
					break
				}
				fds = append(fds, pipe.fd)
				stats, err := unix.MemfdCreate("owned-counter-wire-control", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
				if err != nil {
					done <- err
					return
				}
				defer unix.Close(stats)
				_ = unix.Ftruncate(stats, 4096)
				_, _ = unix.FcntlInt(uintptr(stats), unix.F_ADD_SEALS, unix.F_SEAL_GROW|unix.F_SEAL_SHRINK|unix.F_SEAL_SEAL)
				fds = append(fds, stats)
				if bad == "writer" {
					// Reopen the real retained FIFO as write-only without
					// changing the caller's original read descriptor.
					writer, err := unix.Open(procFD(pipe.fd), unix.O_WRONLY|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
					if err != nil {
						done <- err
						return
					}
					defer unix.Close(writer)
					fds[1] = writer
				}
				if bad == "unsealed" {
					unsealed, err := unix.MemfdCreate("owned-unsealed-wire-control", unix.MFD_CLOEXEC)
					if err != nil {
						done <- err
						return
					}
					defer unix.Close(unsealed)
					_ = unix.Ftruncate(unsealed, 8192)
					fds[0] = unsealed
				}
				count := uint64(3)
				if bad == "fd-bound" {
					count = 4099
				}
				if err := sendReadPacket(connection, 11, [4]uint64{8192, 2, count, 1}, nil, nil); err != nil {
					done <- err
					return
				}
				spans := [][4]uint64{{0, 4096, 1, 0}, {4096, 4096, 0, 0}}
				if bad == "gap" {
					spans[0][0] = 4096
				}
				if bad == "overlap" {
					spans[1][0] = 0
				}
				_ = sendReadPacket(connection, 12, [4]uint64{0, 2}, spans, nil)
				_ = sendReadPacket(connection, 13, [4]uint64{0, 3}, nil, fds)
				if bad != "missing" {
					id := uint64(1)
					if bad == "end-id" {
						id = 2
					}
					_ = sendReadPacket(connection, 14, [4]uint64{8192, 2, 3, id}, nil, nil)
				}
				done <- nil
			}()
			nativeReadCommand(t, socket, file.Name(), "reject")
			if err := <-done; err != nil && !errors.Is(err, net.ErrClosed) {
				t.Fatal(err)
			}
			for _, fd := range []int{image.reader.baseFD, image.reader.spans[0].pipe.fd} {
				var stat unix.Stat_t
				if err := unix.Fstat(fd, &stat); err != nil {
					t.Fatal("C constructor rejection closed the sender's original descriptor")
				}
			}
		})
	}
}

func procFD(fd int) string {
	return "/proc/self/fd/" + strconv.Itoa(fd)
}

func TestActualCMetadataAndRightsBatchBoundaries(t *testing.T) {
	if os.Getenv("PIPE_STORE_TEST_OUTPUT") == "" {
		t.Skip("native Linux descriptor test only")
	}
	golden, err := os.CreateTemp(t.TempDir(), "batch-image-")
	if err != nil {
		t.Fatal(err)
	}
	defer golden.Close()
	const size = 520 * 4096
	fd, err := unix.MemfdCreate("owned-batch-eager-control", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	if err := unix.Ftruncate(fd, size); err != nil {
		t.Fatal(err)
	}
	var originals []int
	defer func() {
		for _, fd := range originals {
			_ = unix.Close(fd)
		}
	}()
	var specs []spanSpec
	for index := range 520 {
		offset := int64(index * 4096)
		page := make([]byte, 4096)
		for column := range page {
			page[column] = byte((index*37 + column*17 + column/31) % 251)
		}
		if n, err := golden.Write(page); err != nil || n != len(page) {
			t.Fatal("batch original write failed")
		}
		if index%2 == 1 {
			if n, err := unix.Pwrite(fd, page, offset); err != nil || n != len(page) {
				t.Fatal("batch eager write failed")
			}
			specs = append(specs, spanSpec{offset, 4096, -1, 0})
		} else {
			var pair [2]int
			if err := unix.Pipe2(pair[:], unix.O_CLOEXEC); err != nil {
				t.Fatal(err)
			}
			originals = append(originals, pair[0])
			if n, err := unix.Write(pair[1], page); err != nil || n != len(page) {
				t.Fatal("batch private write failed")
			}
			_ = unix.Close(pair[1])
			specs = append(specs, spanSpec{offset, 4096, pair[0], 0})
		}
	}
	if _, err := unix.FcntlInt(uintptr(fd), unix.F_ADD_SEALS,
		unix.F_SEAL_WRITE|unix.F_SEAL_GROW|unix.F_SEAL_SHRINK|unix.F_SEAL_SEAL); err != nil {
		t.Fatal(err)
	}
	reader, err := newPipeFile(fd, size, specs)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	image := &capturedImage{name: golden.Name(), bytes: size, reader: reader, token: [16]byte{71}}
	if err := unix.Fsetxattr(int(golden.Fd()), readImageAttribute, image.token[:], unix.XATTR_CREATE); err != nil {
		t.Fatal(err)
	}
	image.signature, err = signatureForFD(int(golden.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	store := &captureStore{images: []*capturedImage{image}}
	socket := filepath.Join(t.TempDir(), "read.sock")
	service, err := startReadService(store, socket, true)
	if err != nil {
		t.Fatal(err)
	}
	nativeReadCommand(t, socket, golden.Name(), "batch")
	proof, err := service.stop()
	if err != nil || proof["bytes"] != uint64(size+256*8192) ||
		proof["private_pipe_bytes"].(uint64) < uint64(size/2) || proof["eager_bytes"].(uint64) < uint64(size/2) {
		t.Fatalf("actual batch reader bytes differ: %v %+v", err, proof)
	}
	lease := proof["leases"].([]map[string]any)[0]
	if lease["leased_data_fds"] != 261 || lease["span_count"] != 520 ||
		lease["released_data_fds"] != uint64(522) || lease["explicit_closes"] != uint64(2) ||
		lease["scratch_fork_resets"].(uint64) == 0 {
		t.Fatal("actual multi-packet descriptor accounting differs")
	}
}
