//go:build linux

package testtrace

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// RequestPageHold uses only the owned transaction's private guard directory.
// It leaves the production worker environment unchanged.
func RequestPageHold(guard, token string, virtualPID int) error {
	if len(token) != 32 || strings.Trim(token, "0123456789abcdef") != "" || virtualPID <= 0 {
		return fmt.Errorf("invalid owned page-hold request")
	}
	temporary := filepath.Join(guard, ".criu-retained-test-request")
	file, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := fmt.Fprintf(file, "{\"version\":1,\"token\":\"%s\",\"active_vpid\":%d}\n", token, virtualPID)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	return os.Rename(temporary, filepath.Join(guard, "criu-retained-test-request.json"))
}

// PageHeld requires the exact armed helper to own the tracer's job-control
// stop, plus the private CRIU acknowledgement at the requested page boundary.
func PageHeld(guard, token string, tracer, helper, active, virtualPID int, nonce string) (bool, error) {
	var stopped struct {
		Version int    `json:"version"`
		Token   string `json:"token"`
		Tracer  int    `json:"tracer_pid"`
		Helper  int    `json:"helper_pid"`
		Active  int    `json:"active_pid"`
		Virtual int    `json:"active_vpid"`
		Nonce   string `json:"nonce"`
	}
	data, err := os.ReadFile(filepath.Join(guard, "criu-retained-test-stop.json"))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(data, &stopped); err != nil {
		return false, err
	}
	if stopped.Version != 1 || stopped.Token != token || stopped.Tracer != tracer ||
		stopped.Helper != helper || helper <= 1 || stopped.Active != active ||
		stopped.Virtual != virtualPID || stopped.Nonce != nonce || nonce == "" {
		return false, fmt.Errorf("page-hold acknowledgement differs from the exact armed transaction")
	}
	status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", tracer))
	if err != nil {
		return false, err
	}
	stoppedState, owned := false, false
	for _, line := range strings.Split(string(status), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "State:" {
			stoppedState = fields[1] == "T" || fields[1] == "t"
		}
		if len(fields) == 2 && fields[0] == "TracerPid:" {
			owned = fields[1] == fmt.Sprint(helper)
		}
	}
	return stoppedState && owned, nil
}

// WaitDead observes the pinned process without consuming its real parent's or
// the retained observer's wait status.
func WaitDead(pidfd int, deadline time.Time) error {
	for time.Now().Before(deadline) {
		remaining := time.Until(deadline)
		timeout := int((remaining + time.Millisecond - 1) / time.Millisecond)
		events := []unix.PollFd{{Fd: int32(pidfd), Events: unix.POLLIN}}
		count, err := unix.Poll(events, timeout)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return err
		}
		if count == 1 && events[0].Revents&unix.POLLIN != 0 {
			return nil
		}
		if count != 0 {
			return fmt.Errorf("unexpected owned pidfd event %d", events[0].Revents)
		}
	}
	return fmt.Errorf("owned tracer did not exit within its original deadline")
}
