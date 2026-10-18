//go:build linux

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Only the owned copy commands call this timer. Disabled runs do no clock
// reads or reporting. These diagnostic spans are excluded from comparisons.
type experimentCopyTiming struct {
	phase  string
	start  time.Time
	last   time.Time
	stages []experimentCopyStage
}

type experimentCopyStage struct {
	Name       string `json:"name"`
	DurationUS int64  `json:"duration_us"`
	EndUS      int64  `json:"end_us"`
}

func newExperimentCopyTiming(phase string) *experimentCopyTiming {
	if os.Getenv("CRIU_SYSBOX_COPY_TIMING") != "1" {
		return nil
	}
	now := time.Now()
	return &experimentCopyTiming{phase: phase, start: now, last: now}
}

func (timing *experimentCopyTiming) mark(name string) {
	if timing == nil {
		return
	}
	now := time.Now()
	timing.stages = append(timing.stages, experimentCopyStage{
		Name: name, DurationUS: now.Sub(timing.last).Microseconds(),
		EndUS: now.Sub(timing.start).Microseconds(),
	})
	timing.last = now
}

func (timing *experimentCopyTiming) finish(complete *bool) {
	if timing == nil {
		return
	}
	timing.mark("return")
	data, err := json.Marshal(map[string]any{
		"version": 1, "phase": timing.phase, "pid": os.Getpid(),
		"complete": *complete, "total_us": timing.last.Sub(timing.start).Microseconds(),
		"stages": timing.stages,
	})
	if err == nil {
		fmt.Fprintf(os.Stderr, "RUNTIME_FORK_COPY_TIMING %s\n", data)
	}
}
