//go:build linux && amd64

package lifecycle

import "strconv"

// Observe the actual delivered native wait without interrupting it. Checking
// the exact private page keeps the prepared case distinct from its control.
func nativeTimerActivatedWait(pid int, fields []string) bool {
	if len(fields) != 9 || (fields[0] != "230" && fields[0] != "219") || fields[1] != "0x1" || fields[2] != "0x0" {
		return false
	}
	rip, err := strconv.ParseUint(fields[8], 0, 64)
	if err != nil {
		return false
	}
	address, _, staged, err := timerPrivateFrame(pid, rip)
	return err == nil && address != 0 && !staged && rip == address+170
}
