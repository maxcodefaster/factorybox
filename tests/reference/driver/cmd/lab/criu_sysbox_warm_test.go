package main

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestWarmPreparationMustJoinAndRetainFailure(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	want := errors.New("incomplete parent")
	var calls atomic.Int32
	job := criuStartPreparation(func() error {
		calls.Add(1)
		close(entered)
		<-release
		return want
	})
	<-entered
	if job.ready() {
		t.Fatal("unfinished preparation was advertised as ready")
	}
	joined := make(chan error, 1)
	go func() { joined <- job.wait() }()
	select {
	case <-joined:
		t.Fatal("fork could use a parent before preparation completed")
	case <-time.After(10 * time.Millisecond):
	}
	close(release)
	if err := <-joined; !errors.Is(err, want) {
		t.Fatalf("lost preparation failure: %v", err)
	}
	if !job.ready() || !errors.Is(job.wait(), want) || !errors.Is(job.wait(), want) || calls.Load() != 1 {
		t.Fatal("completion must retain its failure and never repeat preparation")
	}
}

func TestWarmPreparationCleanupJoinsOwnedWork(t *testing.T) {
	release := make(chan struct{})
	job := criuStartPreparation(func() error { <-release; return nil })
	joined := make(chan struct{})
	go func() { _ = job.wait(); close(joined) }()
	select {
	case <-joined:
		t.Fatal("cleanup may not destroy source while preparation is active")
	case <-time.After(10 * time.Millisecond):
	}
	close(release)
	<-joined
	if job.wait() != nil || job.duration() <= 0 {
		t.Fatal("successful completion or its paid preparation time was lost")
	}
}

func TestWarmPreparationGuard(t *testing.T) {
	t.Setenv("CRIU_SYSBOX_WARM_PREPARE", "1")
	for _, name := range []string{"CRIU_SYSBOX_PRE_DUMP", "CRIU_SYSBOX_RETAIN_SOURCE", "CRIU_SYSBOX_ASYNC_TRACK", "CRIU_SYSBOX_DELTA"} {
		t.Setenv(name, "1")
	}
	if err := criuWarmPreparationGuard("/experiment", "source-sysbox"); err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{"/elsewhere", "source-sysbox"}, {"/experiment", "copy-sysbox"}} {
		if criuWarmPreparationGuard(pair[0], pair[1]) == nil {
			t.Fatal("accepted a source outside the owned tracked experiment")
		}
	}
	for _, name := range []string{"CRIU_SYSBOX_PRE_DUMP", "CRIU_SYSBOX_RETAIN_SOURCE", "CRIU_SYSBOX_ASYNC_TRACK", "CRIU_SYSBOX_DELTA"} {
		t.Setenv(name, "0")
		if criuWarmPreparationGuard("/experiment", "source-sysbox") == nil {
			t.Fatalf("accepted missing %s", name)
		}
		t.Setenv(name, "1")
	}
	t.Setenv("CRIU_SYSBOX_WARM_PREPARE", "invalid")
	if criuWarmPreparationGuard("/experiment", "source-sysbox") == nil {
		t.Fatal("accepted invalid switch")
	}
}
