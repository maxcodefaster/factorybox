//go:build linux && amd64

package lifecycle

import "testing"

func nativeTimerFaultContinuation(t *testing.T, pid int, address uint64) []byte {
	t.Helper()
	return timerContinuationAMD64
}
