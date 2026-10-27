//go:build linux && (amd64 || arm64)

package timerstate

import (
	"bytes"
	"errors"
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
