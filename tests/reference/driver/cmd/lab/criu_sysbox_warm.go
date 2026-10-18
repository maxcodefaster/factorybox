package main

import (
	"fmt"
	"os"
	"time"
)

// Closing done publishes the complete result to every joiner. Both the fork
// and error cleanup join it before accessing the base or deleting the source.
type criuPreparation struct {
	done     chan struct{}
	started  time.Time
	finished time.Time
	err      error
}

func criuStartPreparation(prepare func() error) *criuPreparation {
	job := &criuPreparation{done: make(chan struct{}), started: time.Now()}
	go func() {
		job.err = prepare()
		job.finished = time.Now()
		close(job.done)
	}()
	return job
}

func (job *criuPreparation) ready() bool {
	select {
	case <-job.done:
		return true
	default:
		return false
	}
}

func (job *criuPreparation) wait() error {
	<-job.done
	return job.err
}

func (job *criuPreparation) duration() time.Duration {
	<-job.done
	return job.finished.Sub(job.started)
}

func criuWarmPreparationGuard(root, id string) error {
	switch os.Getenv("CRIU_SYSBOX_WARM_PREPARE") {
	case "", "0":
		return nil
	case "1":
	default:
		return fmt.Errorf("CRIU_SYSBOX_WARM_PREPARE must be 0 or 1")
	}
	if root != "/experiment" || id != "source-sysbox" {
		return fmt.Errorf("warm preparation is outside the owned source")
	}
	for _, name := range []string{"CRIU_SYSBOX_PRE_DUMP", "CRIU_SYSBOX_RETAIN_SOURCE", "CRIU_SYSBOX_ASYNC_TRACK", "CRIU_SYSBOX_DELTA"} {
		if os.Getenv(name) != "1" {
			return fmt.Errorf("warm preparation requires %s=1", name)
		}
	}
	return nil
}

func criuSysboxStartWarmPreparation(id string) (*criuPreparation, error) {
	if err := criuWarmPreparationGuard(criuRoot, id); err != nil {
		return nil, err
	}
	if os.Getenv("CRIU_SYSBOX_WARM_PREPARE") != "1" {
		return nil, nil
	}
	job := criuStartPreparation(func() error {
		if err := criuSysboxMaterializeSource(id); err != nil {
			return err
		}
		return criuSysboxPreDumpSource(id)
	})
	record("sysbox-warm-preparation-started", job.started,
		"one owned materialization/pre-dump task after first service/IDE readiness; existing cumulative dirty tracking captures later writes; fork and cleanup join before using images or destroying source", nil)
	return job, nil
}
