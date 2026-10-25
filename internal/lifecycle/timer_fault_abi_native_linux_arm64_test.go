//go:build linux && arm64

package lifecycle

import "testing"

func nativeTimerFaultContinuation(t *testing.T, pid int, address uint64) []byte {
	t.Helper()
	actual, frame, staged, err := timerPrivateFrame(pid, address)
	if err != nil || actual != address || !staged {
		t.Fatal("source lacks its exact original staged ARM64 code/frame mapping", actual, staged, err)
	}
	code, err := timerContinuationImage(address, frame)
	if err != nil {
		t.Fatal("original staged ARM64 continuation identity changed", err)
	}
	return code
}
