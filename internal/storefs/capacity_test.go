package storefs

import (
	"math"
	"testing"
)

func TestCapacitySelectionFitsActualBackingWithoutConsumingHostFloor(t *testing.T) {
	const gib = int64(1 << 30)
	for _, test := range []struct {
		name      string
		maximum   int64
		minimum   int64
		available uint64
		want      int64
	}{
		{"reported failure fits 17 instead of fixed 32", 32 * gib, 8 * gib, 29 * uint64(gib), 17 * gib},
		{"available fraction rounds down", 32 * gib, 8 * gib, 29*uint64(gib) + 999, 17 * gib},
		{"minimum exact boundary", 32 * gib, 8 * gib, 20 * uint64(gib), 8 * gib},
		{"maximum exact boundary", 32 * gib, 8 * gib, 44 * uint64(gib), 32 * gib},
		{"extra host capacity never raises requested maximum", 32 * gib, 8 * gib, math.MaxUint64, 32 * gib},
		{"equal range retains requested capacity", 8 * gib, 8 * gib, 29 * uint64(gib), 8 * gib},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := SelectCapacity(test.maximum, test.minimum, test.available)
			if err != nil || got != test.want {
				t.Fatalf("selected capacity=%d, error=%v; want %d", got, err, test.want)
			}
			if uint64(got)+HostFreeFloor > test.available {
				t.Fatal("selected capacity consumed the native host free-space floor")
			}
		})
	}
}

func TestCapacitySelectionRefusesInsufficientOrUnadmittedRanges(t *testing.T) {
	const gib = int64(1 << 30)
	for _, test := range []struct {
		name      string
		maximum   int64
		minimum   int64
		available uint64
	}{
		{"below minimum", 32 * gib, 8 * gib, 20*uint64(gib) - 1},
		{"below host floor", 32 * gib, 8 * gib, 1},
		{"unknown capacity", 32 * gib, 8 * gib, 0},
		{"zero minimum is not automatic selection", 32 * gib, 0, math.MaxUint64},
		{"negative minimum", 32 * gib, -gib, math.MaxUint64},
		{"inverted range", 8 * gib, 32 * gib, math.MaxUint64},
		{"unaligned maximum", 32*gib + 1, 8 * gib, math.MaxUint64},
		{"unaligned minimum", 32 * gib, 8*gib + 1, math.MaxUint64},
		{"native maximum exceeded", 65 * gib, 8 * gib, math.MaxUint64},
	} {
		t.Run(test.name, func(t *testing.T) {
			if capacity, err := SelectCapacity(test.maximum, test.minimum, test.available); err == nil || capacity != 0 {
				t.Fatal("invalid or insufficient range selected storage", capacity, err)
			}
		})
	}
}
