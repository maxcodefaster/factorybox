//go:build linux

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"runtime"
	"time"

	"golang.org/x/sys/unix"
)

type workerCanarySample struct {
	Kind        string `json:"kind"`
	Repetition  int    `json:"repetition"`
	Operations  int    `json:"operations"`
	Bytes       int    `json:"bytes"`
	WallUS      int64  `json:"wall_us"`
	UserUS      int64  `json:"user_us"`
	SystemUS    int64  `json:"system_us"`
	MinorFaults int64  `json:"minor_faults"`
	MajorFaults int64  `json:"major_faults"`
	Voluntary   int64  `json:"voluntary_switches"`
	Involuntary int64  `json:"involuntary_switches"`
	Digest      string `json:"digest,omitempty"`
}

func workerCanaryDifference(kind string, repetition, operations, bytes int, wall time.Duration,
	before, after unix.Rusage) workerCanarySample {
	return workerCanarySample{
		Kind: kind, Repetition: repetition, Operations: operations, Bytes: bytes,
		WallUS:      wall.Microseconds(),
		UserUS:      after.Utime.Nano()/1000 - before.Utime.Nano()/1000,
		SystemUS:    after.Stime.Nano()/1000 - before.Stime.Nano()/1000,
		MinorFaults: after.Minflt - before.Minflt, MajorFaults: after.Majflt - before.Majflt,
		Voluntary: after.Nvcsw - before.Nvcsw, Involuntary: after.Nivcsw - before.Nivcsw,
	}
}

func workerCanarySamples() ([]workerCanarySample, error) {
	// One OS thread makes RUSAGE_THREAD refer to the exact measured work.
	// A private 256KiB buffer does not prefetch the saved runtime's pages.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	buffer := make([]byte, 256<<10)
	for i := range buffer {
		buffer[i] = byte(i*31 + i/251)
	}
	expected := sha256.Sum256(buffer)
	const hashOperations = 256
	const syscallOperations = 65536
	deadline := time.Now().Add(5 * time.Second)
	var samples []workerCanarySample
	for repetition := range 3 {
		for _, kind := range []string{"sha256", "raw_getpid"} {
			if time.Now().After(deadline) {
				return samples, fmt.Errorf("bounded worker canary exceeded its five-second deadline")
			}
			var before, after unix.Rusage
			if err := unix.Getrusage(unix.RUSAGE_THREAD, &before); err != nil {
				return samples, err
			}
			start := time.Now()
			operations, bytes := syscallOperations, 0
			digest := ""
			if kind == "sha256" {
				operations, bytes = hashOperations, hashOperations*len(buffer)
				var actual [sha256.Size]byte
				for range hashOperations {
					actual = sha256.Sum256(buffer)
					if actual != expected {
						return samples, fmt.Errorf("worker canary hash changed")
					}
				}
				digest = hex.EncodeToString(actual[:])
			} else {
				expectedPID := uintptr(os.Getpid())
				for i := range syscallOperations {
					pid, _, errno := unix.RawSyscall(unix.SYS_GETPID, 0, 0, 0)
					if errno != 0 || pid != expectedPID {
						return samples, fmt.Errorf("worker canary raw getpid: pid=%d errno=%d", pid, errno)
					}
					if i%1024 == 0 && time.Now().After(deadline) {
						return samples, fmt.Errorf("bounded worker syscall canary deadline")
					}
				}
			}
			wall := time.Since(start)
			if err := unix.Getrusage(unix.RUSAGE_THREAD, &after); err != nil {
				return samples, err
			}
			sample := workerCanaryDifference(kind, repetition, operations, bytes, wall, before, after)
			sample.Digest = digest
			samples = append(samples, sample)
		}
	}
	return samples, nil
}

func criuWorkerCanary(phase string) error {
	if os.Getenv("RUNTIME_FORK_WORKER_CANARY") != "1" {
		return fmt.Errorf("worker canary requires its explicit isolated experiment")
	}
	switch phase {
	case "before-case", "before-second-fork", "after-case":
	default:
		return fmt.Errorf("unexpected canary phase %q", phase)
	}
	start := time.Now()
	before, beforeErr := criuSysboxPhaseCountersRead()
	samples, sampleErr := workerCanarySamples()
	after, afterErr := criuSysboxPhaseCountersRead()
	err := beforeErr
	if err == nil {
		err = sampleErr
	}
	if err == nil {
		err = afterErr
	}
	record("sysbox-worker-canary-"+phase, start, asJSON(map[string]any{
		"samples": samples, "cgroup_before": before, "cgroup_after": after,
		"scope": "one sequential OS-thread worker; three fixed 64MiB SHA256 and 65536 raw-getpid samples; private 256KiB buffer; five-second cooperative deadline; outside second-fork readiness clock; diagnostic only; every full fork remains in latency analysis",
	}), err)
	return err
}
