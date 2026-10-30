package storefs

import "fmt"

const HostFreeFloor = 12 << 30

// SelectCapacity chooses a fresh fixed image, never a resize of existing data.
// The caller observes the actual backing filesystem under its exclusive lease.
// Create still checks the floor before and after the real allocation.
func SelectCapacity(maximum, minimum int64, available uint64) (int64, error) {
	const gib = int64(1 << 30)
	if minimum < gib || maximum > 64*gib || minimum > maximum ||
		minimum%gib != 0 || maximum%gib != 0 {
		return 0, fmt.Errorf("automatic storage requires whole-GiB bounds from 1 to 64 GiB")
	}
	if available < HostFreeFloor+uint64(minimum) {
		return 0, fmt.Errorf("insufficient backing storage: %d bytes available, at least %d required",
			available, HostFreeFloor+uint64(minimum))
	}
	capacity := (available - HostFreeFloor) / uint64(gib) * uint64(gib)
	if capacity > uint64(maximum) {
		capacity = uint64(maximum)
	}
	return int64(capacity), nil
}
