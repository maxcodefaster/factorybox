//go:build linux

package lifecycle

import (
	"errors"
	"testing"
)

func TestCaptureCompletionPendingFreezerIsExact(t *testing.T) {
	for _, test := range []struct {
		name    string
		events  map[string]uint64
		readErr error
		pending bool
		passed  bool
	}{
		{"confirmed", map[string]uint64{"populated": 1, "frozen": 1}, nil, false, true},
		{"pending", map[string]uint64{"populated": 1, "frozen": 0}, nil, true, false},
		{"read-failed", map[string]uint64{"populated": 1, "frozen": 0}, errors.New("unavailable"), false, false},
		{"missing-frozen", map[string]uint64{"populated": 1}, nil, false, false},
		{"missing-populated", map[string]uint64{"frozen": 0}, nil, false, false},
		{"empty", map[string]uint64{"populated": 0, "frozen": 0}, nil, false, false},
		{"invalid-frozen", map[string]uint64{"populated": 1, "frozen": 2}, nil, false, false},
		{"invalid-populated", map[string]uint64{"populated": 2, "frozen": 0}, nil, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := captureCompletionFrozenState(test.events, test.readErr)
			var pending *captureFreezerPending
			if (err == nil) != test.passed || errors.As(err, &pending) != test.pending {
				t.Fatalf("completion classification: err=%v, pending=%v", err, pending != nil)
			}
		})
	}
}
