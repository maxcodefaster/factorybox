//go:build linux && (amd64 || arm64)

package timerstate

import (
	"encoding/binary"
	"errors"
	"testing"

	"golang.org/x/sys/unix"
)

func TestReleaseObservationDoesNotReopenAnObject(t *testing.T) {
	for _, next := range []uint32{43, 44} {
		calls := 0
		err := waitReleased(42, func(attr []byte) error {
			calls++
			if len(attr) != 12 || binary.LittleEndian.Uint32(attr) != 41 {
				t.Fatal("release did not enumerate immediately before the exact object")
			}
			binary.LittleEndian.PutUint32(attr[4:], next)
			return nil
		})
		if err != nil || calls != 1 {
			t.Fatal("release adopted an unrelated object", calls, err)
		}
	}
	if err := waitReleased(42, func([]byte) error { return unix.ENOENT }); err != nil {
		t.Fatal("absent final ID did not settle release", err)
	}
	if err := waitReleased(42, func([]byte) error { return unix.EPERM }); !errors.Is(err, unix.EPERM) {
		t.Fatal("failed observation was treated as release", err)
	}
	if err := waitReleased(0, func([]byte) error { t.Fatal("invalid ID reached kernel"); return nil }); err == nil {
		t.Fatal("unidentified object release was admitted")
	}
	if err := waitReleased(42, func(attr []byte) error {
		binary.LittleEndian.PutUint32(attr[4:], 41)
		return nil
	}); err == nil {
		t.Fatal("invalid enumeration was treated as release")
	}
}
