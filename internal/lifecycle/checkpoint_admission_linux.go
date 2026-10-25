//go:build linux

package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"time"

	"factorybox/internal/sysboxcapture"
)

type captureTimerAdmission struct {
	Version      int                       `json:"version"`
	Token        string                    `json:"token"`
	SourcePID    int                       `json:"source_pid"`
	SourceStart  string                    `json:"source_start"`
	Attempts     int                       `json:"attempts"`
	Seconds      float64                   `json:"seconds"`
	First        *timerPendingCaughtSignal `json:"first"`
	Last         *timerPendingCaughtSignal `json:"last"`
	ExpiredFirst *timerExpiredRestart      `json:"expired_first,omitempty"`
	ExpiredLast  *timerExpiredRestart      `json:"expired_last,omitempty"`
	Passed       bool                      `json:"passed"`
}

// Only an unmodified, pre-capture source may run to consume a pending caught
// signal. Every subsequent freeze repeats descriptor, namespace, filesystem
// and timer admission: source execution can change all four inventories.
// A pending signal during or after timer mutation remains a hard refusal.
func (e *Engine) admitCheckpointSource(ctx context.Context, guard *captureGuard, record Record,
	rootOwnedTimeNamespaces bool) (kind string, resultErr error) {
	observation := captureTimerAdmission{Version: 1, Token: guard.config.Token,
		SourcePID: guard.config.PID, SourceStart: guard.config.Start}
	var started, deadline time.Time
	defer func() {
		if observation.First == nil && observation.ExpiredFirst == nil {
			return
		}
		observation.Seconds, observation.Passed = time.Since(started).Seconds(), resultErr == nil
		resultErr = errors.Join(resultErr,
			captureGuardWrite(guard.anchor.evidence, "timer-admission.json", observation))
	}()
	for {
		if err := ctx.Err(); err != nil {
			return kind, err
		}
		observation.Attempts++
		kind = "descriptors"
		admissionErr := guard.admitCheckpointDescriptors(ctx)
		if admissionErr == nil {
			kind = ""
			admissionErr = guard.admitCheckpointNamespaces(rootOwnedTimeNamespaces)
		}
		if admissionErr == nil {
			kind = "filesystem"
			admissionErr = sysboxcapture.AdmitFilesystem(sysboxcapture.Request{
				RuntimeRoot: e.config.Root + "/runtime", RuntimeID: record.RuntimeID,
				Owner: record.Owner, Rootfs: record.Rootfs, Bundle: record.InputBundle,
				ManagerRoot: e.config.ManagerRoot, Base: record.Base, SharedSource: record.SharedSource,
			})
		}
		if admissionErr == nil {
			kind = "timers"
			if record.Network != nil {
				threads, err := captureGuardThreads(guard.config, guard.anchor)
				if err != nil {
					return "network", err
				}
				if err := record.Network.VerifySource(guard.config.PID, threads); err != nil {
					return "network", err
				}
			}
			admissionErr = guard.admitCheckpointTimers()
		}
		var pending *timerPendingCaughtSignal
		var expired *timerExpiredRestart
		isPending := errors.As(admissionErr, &pending)
		isExpired := errors.As(admissionErr, &expired)
		if kind != "timers" || !isPending && !isExpired {
			return kind, admissionErr
		}
		if isPending {
			if observation.First == nil {
				observation.First = pending
			}
			observation.Last = pending
		} else {
			if observation.ExpiredFirst == nil {
				observation.ExpiredFirst = expired
			}
			observation.ExpiredLast = expired
		}
		if started.IsZero() {
			started = time.Now()
			deadline = started.Add(2 * time.Second)
		}
		if time.Now().After(deadline) || observation.Attempts >= 32 {
			if isExpired {
				return kind, fmt.Errorf("pre-capture expired restart did not settle within the 2s/32-attempt bound: %w", expired)
			}
			return kind, fmt.Errorf("pre-capture caught signal did not settle within the 2s/32-attempt bound: %w", pending)
		}
		if guard.unsafe || guard.timerPlanSHA != "" {
			if isExpired {
				return kind, fmt.Errorf("expired restart cannot release a prepared or admitted timer source: %w", expired)
			}
			return kind, fmt.Errorf("pending signal cannot release a prepared or admitted timer source: %w", pending)
		}
		if err := captureGuardCaptureReturned(guard.config, guard.anchor); err != nil {
			return kind, err
		}
		if err := guard.freeze(false); err != nil {
			return kind, err
		}
		delay := 10 * time.Millisecond << min(observation.Attempts-1, 3)
		timer := time.NewTimer(min(delay, time.Until(deadline)))
		select {
		case <-ctx.Done():
			timer.Stop()
			return kind, ctx.Err()
		case <-timer.C:
		}
		if err := guard.freeze(true); err != nil {
			return kind, err
		}
	}
}
