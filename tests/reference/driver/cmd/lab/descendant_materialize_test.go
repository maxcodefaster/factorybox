package main

import (
	"errors"
	"testing"
	"time"
)

func TestDescendantMaterializationRejectsFailedCompletedPager(t *testing.T) {
	saved := criuLazyDaemons
	t.Cleanup(func() { criuLazyDaemons = saved })
	failure := errors.New("owned page daemon failed before recapture")
	for _, consumed := range []bool{false, true} {
		done := make(chan error, 1)
		done <- failure
		close(done)
		daemon := &criuLazyDaemon{done: done}
		if consumed {
			daemon.pollCompletion()
		}
		criuLazyDaemons = []*criuLazyDaemon{daemon}
		for attempt := 0; attempt < 2; attempt++ {
			if err := descendantMaterializeDaemons(25 * time.Millisecond); !errors.Is(err, failure) {
				t.Fatalf("failed pager accepted, consumed=%t attempt=%d error=%v", consumed, attempt, err)
			}
		}
	}
}
