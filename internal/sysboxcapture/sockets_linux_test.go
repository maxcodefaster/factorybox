//go:build linux

package sysboxcapture

import "testing"

func TestSocketRollbackAdmission(t *testing.T) {
	original := captureSocket{Cookie: 42, Repair: 0, Mark: 17}
	for _, test := range []struct {
		name    string
		current captureSocket
		allowed bool
	}{
		{"owned-repair-delta", captureSocket{Cookie: 42, Repair: 1, Mark: 17}, true},
		{"reused-descriptor-cookie", captureSocket{Cookie: 43, Repair: 1, Mark: 17}, false},
		{"foreign-mark", captureSocket{Cookie: 42, Repair: 1, Mark: 18}, false},
		{"unknown-repair-state", captureSocket{Cookie: 42, Repair: 2, Mark: 17}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if actual := admitSocketRollback(original, test.current); actual != test.allowed {
				t.Fatal("socket rollback admission lost original ownership or properties", actual)
			}
		})
	}
}
