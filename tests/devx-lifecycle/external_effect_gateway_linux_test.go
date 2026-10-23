//go:build linux

package devx_test

import (
	"context"
	"os"
	"time"

	"factorybox/internal/lifecycle"
	providerfixture "factorybox/tests/provider-fixture"
)

// Selected only by the declared patch of a new combined-test source copy.
// Both original inference and outside-effect leases must exit before capture.
func (f *fixture) externalEffectProvider(ctx context.Context, view lifecycle.View, operation string) func() {
	f.t.Helper()
	lease, err := providerfixture.Acquire(ctx, "/output/effect", operation, providerfixture.Target{
		RunOwner: os.Getenv("FACTORYBOX_DEVX_RUN"), Sandbox: view.Record.ID, Owner: view.Record.Owner,
		Runtime: view.Runtime.ID, PID: view.Runtime.PID, Created: view.Runtime.Created,
	})
	if err != nil {
		f.t.Fatal("attach exact outside-effect peer beside external inference", err)
	}
	return func() {
		closed, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := lease.Close(closed); err != nil {
			f.t.Fatal("outside-effect peer did not exit before capture", err)
		}
	}
}
