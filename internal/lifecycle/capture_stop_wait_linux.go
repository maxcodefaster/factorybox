//go:build linux

package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type captureStopPending struct{ cause error }

func (pending *captureStopPending) Error() string { return pending.cause.Error() }
func (pending *captureStopPending) Unwrap() error { return pending.cause }

// SIGKILL delivery is not proof of kernel process/cgroup release. Observe that
// same original source within the caller's remaining lifecycle, without another
// local cutoff, deadline renewal or repeated signal.
func captureWaitStopped(ctx context.Context, validate, clearEmptyFreezer func() error) error {
	if _, finite := ctx.Deadline(); !finite {
		return fmt.Errorf("captured source stop requires its original finite lifecycle deadline")
	}
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	var observed error
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("captured original source stop remains unconfirmed: %w",
				errors.Join(err, observed))
		}
		observed = validate()
		if err := ctx.Err(); err != nil {
			return errors.Join(err, observed)
		}
		if observed == nil {
			return clearEmptyFreezer()
		}
		var pending *captureStopPending
		if !errors.As(observed, &pending) {
			return observed
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("captured original source stop remains unconfirmed: %w",
				errors.Join(ctx.Err(), observed))
		case <-tick.C:
		}
	}
}
