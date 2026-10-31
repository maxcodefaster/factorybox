//go:build linux && (amd64 || arm64)

package timerstate

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestProgramLoadRetriesOnlyRejectedInterruptedLoads(t *testing.T) {
	attr := []byte{1, 2, 3, 4}
	original := append([]byte(nil), attr...)
	calls := 0
	fd, err := loadProgram(attr, func(command uintptr, actual []byte) (int, error) {
		calls++
		if command != 5 || &actual[0] != &attr[0] || !bytes.Equal(actual, original) {
			t.Fatal("retry changed the program load or its exact parameters")
		}
		if calls == 1 {
			return -1, unix.EAGAIN
		}
		return 42, nil
	})
	if err != nil || fd != 42 || calls != 2 {
		t.Fatal("interrupted unpublished program was not loaded", fd, calls, err)
	}
}

func TestProgramLoadNeverRetriesAcceptedOrPermanentOutcomes(t *testing.T) {
	for _, row := range []struct {
		name string
		fd   int
		err  error
	}{
		{"accepted", 42, nil},
		{"accepted-fd-zero", 0, nil},
		{"permission", -1, unix.EPERM},
		{"unsafe-program", -1, unix.EACCES},
		{"invalid-program", -1, unix.EINVAL},
		{"memory", -1, unix.ENOMEM},
		{"log-buffer", -1, unix.ENOSPC},
		{"other-signal-error", -1, unix.EINTR},
	} {
		t.Run(row.name, func(t *testing.T) {
			calls := 0
			fd, err := loadProgram([]byte{1}, func(command uintptr, attr []byte) (int, error) {
				calls++
				if command != 5 || len(attr) != 1 || attr[0] != 1 {
					t.Fatal("program load changed its request")
				}
				return row.fd, row.err
			})
			if fd != row.fd || !errors.Is(err, row.err) || calls != 1 {
				t.Fatal("accepted or permanent outcome was repeated or changed", fd, calls, err)
			}
		})
	}
}

func TestNativeRejectedTimerProgramRetainsDiagnostic(t *testing.T) {
	if os.Getenv("FACTORYBOX_TIMERSTATE_NATIVE") != "1" {
		t.Skip("explicit owned native timer reader proof only")
	}
	// Returning an uninitialized R0 must fail verification. Statistics-only
	// logging must preserve the kernel's reason rather than hide that failure.
	code := []byte{0x95, 0, 0, 0, 0, 0, 0, 0}
	license, verifier, attr := []byte("GPL\x00"), make([]byte, 65536), make([]byte, 144)
	var pinned runtime.Pinner
	defer pinned.Unpin()
	binary.LittleEndian.PutUint32(attr, 1)
	binary.LittleEndian.PutUint32(attr[4:], uint32(len(code)/8))
	binary.LittleEndian.PutUint64(attr[8:], address(&pinned, code))
	binary.LittleEndian.PutUint64(attr[16:], address(&pinned, license))
	binary.LittleEndian.PutUint32(attr[24:], verifierLogStats)
	binary.LittleEndian.PutUint32(attr[28:], uint32(len(verifier)))
	binary.LittleEndian.PutUint64(attr[32:], address(&pinned, verifier))
	fd, err := loadProgram(attr, bpf)
	runtime.KeepAlive(code)
	runtime.KeepAlive(license)
	runtime.KeepAlive(verifier)
	if fd >= 0 {
		_ = unix.Close(fd)
		t.Fatal("kernel accepted an invalid owned test program")
	}
	diagnostic := strings.TrimRight(string(verifier), "\x00")
	if !errors.Is(err, unix.EACCES) || !strings.Contains(diagnostic, "R0 !read_ok") {
		t.Fatal("rejected program lost its verification failure", err, diagnostic)
	}
}

func TestProgramLoadInterruptedRetriesAreBounded(t *testing.T) {
	calls := 0
	fd, err := loadProgram([]byte{1}, func(uintptr, []byte) (int, error) {
		calls++
		return -1, unix.EAGAIN
	})
	if fd != -1 || !errors.Is(err, unix.EAGAIN) || calls != 5 {
		t.Fatal("persistent interruption lost its bounded refusal", fd, calls, err)
	}
}

func TestProgramLoadStopsOnPermanentFailureAfterInterruption(t *testing.T) {
	calls := 0
	fd, err := loadProgram([]byte{1}, func(uintptr, []byte) (int, error) {
		calls++
		if calls == 1 {
			return -1, unix.EAGAIN
		}
		return -1, unix.EPERM
	})
	if fd != -1 || !errors.Is(err, unix.EPERM) || calls != 2 {
		t.Fatal("permanent failure after interruption was hidden or repeated", fd, calls, err)
	}
}
