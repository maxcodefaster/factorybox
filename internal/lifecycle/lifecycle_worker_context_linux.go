//go:build linux

package lifecycle

import (
	"context"
	"time"
)

func lifecycleWorkerContext(parent context.Context) (context.Context, context.CancelFunc) {
	// Capture and restore include RAM and the complete private filesystem.
	// Their owner supplies one lifecycle budget, not a limit for each phase.
	if _, bounded := parent.Deadline(); bounded {
		return context.WithCancel(parent)
	}
	// Direct CLI callers have cancellation but no deadline. Keep their
	// operation finite without shortening an explicitly bounded caller.
	return context.WithTimeout(parent, 7*time.Minute)
}
