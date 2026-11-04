//go:build linux

package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestCaptureStopWaitUsesOriginalRemainingLifecycle(t *testing.T) {
	// A real pidfd stays live past the former local three-second cutoff.
	// This tests the stop-wait policy, not CRIU capture or cgroup ownership.
	source := exec.Command("/bin/sh", "-c", "exec sleep 60")
	if err := source.Start(); err != nil {
		t.Fatal(err)
	}
	fd, err := unix.PidfdOpen(source.Process.Pid, 0)
	if err != nil {
		_ = source.Process.Kill()
		_ = source.Wait()
		t.Fatal(err)
	}
	waited := make(chan error, 1)
	go func() { waited <- source.Wait() }()
	signaled := make(chan struct{})
	timer := time.AfterFunc(4*time.Second, func() {
		defer close(signaled)
		_ = unix.PidfdSendSignal(fd, unix.SIGKILL, nil, 0)
	})
	t.Cleanup(func() {
		if !timer.Stop() {
			<-signaled
		}
		_ = unix.PidfdSendSignal(fd, unix.SIGKILL, nil, 0)
		<-waited
		unix.Close(fd)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pending := &captureStopPending{errors.New("original pidfd has not reported exit")}
	validate := func() error {
		poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		for {
			n, err := unix.Poll(poll, 0)
			if errors.Is(err, unix.EINTR) {
				continue // Match the production guard's interrupted read-only probe.
			}
			if err != nil {
				return err
			}
			if n != 1 || poll[0].Revents&unix.POLLIN == 0 || poll[0].Revents&unix.POLLNVAL != 0 {
				return pending
			}
			return nil
		}
	}
	cleared := false
	err = captureWaitStopped(ctx, validate, func() error {
		if err := validate(); err != nil {
			return fmt.Errorf("attempted freezer clear before original pidfd exit: %w", err)
		}
		cleared = true
		return nil
	})
	if err != nil || !cleared {
		t.Fatal("stop wait replaced the admitted lifecycle with a local cutoff", err, cleared)
	}
}

func TestCaptureStopWaitRetainsCancellationAndNeverClearsUnconfirmedSource(t *testing.T) {
	pending := &captureStopPending{errors.New("original source remains unconfirmed")}
	for _, before := range []bool{false, true} {
		t.Run(fmt.Sprint(before), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			if before {
				cancel()
			}
			cleared := false
			started := time.Now()
			err := captureWaitStopped(ctx, func() error { return pending }, func() error {
				cleared = true
				return nil
			})
			if cleared || !errors.Is(err, ctx.Err()) || (!before && !errors.Is(err, pending)) ||
				time.Since(started) > time.Second {
				t.Fatal("original cancellation or unresolved source was replaced", err, cleared)
			}
		})
	}
}

func TestCaptureStopWaitRefusesChangedOwnershipImmediately(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	foreign := errors.New("original cgroup or record identity changed")
	cleared := false
	started := time.Now()
	err := captureWaitStopped(ctx, func() error { return foreign }, func() error {
		cleared = true
		return nil
	})
	if !errors.Is(err, foreign) || cleared || time.Since(started) > time.Second {
		t.Fatal("foreign identity was retried or authorized", err, cleared)
	}
}

func TestCaptureStopWaitRequiresItsExistingDeadline(t *testing.T) {
	called := false
	err := captureWaitStopped(context.Background(), func() error {
		called = true
		return nil
	}, func() error {
		called = true
		return nil
	})
	if err == nil || called {
		t.Fatal("stop wait invented an unbounded or renewed lifecycle", err, called)
	}
}

func TestCaptureStopWaitCannotClearAfterCancellationDuringObservation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cleared := false
	err := captureWaitStopped(ctx, func() error {
		cancel()
		return nil
	}, func() error {
		cleared = true
		return nil
	})
	if !errors.Is(err, context.Canceled) || cleared {
		t.Fatal("late observation cleared a source after its original cancellation", err, cleared)
	}
}
