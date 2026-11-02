//go:build linux

package lifecycle

import (
	"context"
	"time"
)

func captureWorkerContext(parent context.Context) (context.Context, context.CancelFunc) {
	// Capture includes RAM and the complete private filesystem. Its owner
	// supplies one lifecycle budget, not a second limit for each phase.
	if _, bounded := parent.Deadline(); bounded {
		return context.WithCancel(parent)
	}
	// Direct CLI callers have cancellation but no deadline. Keep their
	// capture finite without shortening an explicitly bounded caller.
	return context.WithTimeout(parent, 7*time.Minute)
}
