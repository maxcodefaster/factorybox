//go:build linux

package lifecycle

import "testing"

func TestCaptureStopThreadGroup(t *testing.T) {
	for _, status := range []string{"Name:\tactor\nTgid:\t42\nPid:\t43\n", "Tgid: 99\n"} {
		if group, err := captureStopThreadGroup(status); err != nil || group <= 1 {
			t.Fatal("valid kernel thread-group identity refused", group, err)
		}
	}
	for _, status := range []string{
		"Pid: 42\n", "Tgid:\n", "Tgid: 0\n", "Tgid: 1\n",
		"Tgid: -42\n", "Tgid: invalid\n", "Tgid: 42 extra\n", "Tgid: 42\nTgid: 43\n",
	} {
		if _, err := captureStopThreadGroup(status); err == nil {
			t.Fatal("unproved process ownership accepted", status)
		}
	}
}
