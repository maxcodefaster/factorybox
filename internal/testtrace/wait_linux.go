//go:build linux

// Package testtrace contains native test instrumentation and is not linked
// into the lifecycle CLI.
package testtrace

import (
	"errors"
	"os"
	"os/signal"
	"time"

	"golang.org/x/sys/unix"
)

type Waiter struct {
	events  chan os.Signal
	expired error
}

// New registers before tracing starts. One notification can cover a stop
// between the exact-PID Wait4 and the event wait below.
func New(deadlineMessage string) *Waiter {
	waiter := &Waiter{events: make(chan os.Signal, 1), expired: errors.New(deadlineMessage)}
	signal.Notify(waiter.events, unix.SIGCHLD)
	return waiter
}

func (waiter *Waiter) Close() {
	signal.Stop(waiter.events)
}

func (waiter *Waiter) Wait(pid int, deadline time.Time) (unix.WaitStatus, error) {
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	for time.Now().Before(deadline) {
		var status unix.WaitStatus
		actual, err := unix.Wait4(pid, &status, unix.WALL|unix.WNOHANG, nil)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil || actual == pid {
			return status, err
		}
		select {
		case <-waiter.events:
		case <-timer.C:
			return 0, waiter.expired
		}
	}
	return 0, waiter.expired
}
