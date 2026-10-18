//go:build linux

package main

import (
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestWorkerCanaryNative(t *testing.T) {
	samples, err := workerCanarySamples()
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 6 {
		t.Fatalf("samples=%d", len(samples))
	}
	var digest string
	for index, sample := range samples {
		if sample.Repetition != index/2 || sample.WallUS <= 0 ||
			sample.UserUS < 0 || sample.SystemUS < 0 ||
			sample.MinorFaults < 0 || sample.MajorFaults < 0 {
			t.Fatalf("invalid actual measurement: %+v", sample)
		}
		if index%2 == 0 {
			if sample.Kind != "sha256" || sample.Operations != 256 ||
				sample.Bytes != 64<<20 || len(sample.Digest) != 64 {
				t.Fatalf("invalid hash work: %+v", sample)
			}
			if digest != "" && sample.Digest != digest {
				t.Fatalf("hash differs across repetitions")
			}
			digest = sample.Digest
		} else if sample.Kind != "raw_getpid" || sample.Operations != 65536 ||
			sample.Bytes != 0 || sample.Digest != "" {
			t.Fatalf("invalid syscall work: %+v", sample)
		}
	}
}

func TestWorkerCanaryDifference(t *testing.T) {
	before := unix.Rusage{Utime: unix.Timeval{Sec: 1, Usec: 9}, Stime: unix.Timeval{Usec: 10},
		Minflt: 3, Majflt: 4, Nvcsw: 5, Nivcsw: 6}
	after := unix.Rusage{Utime: unix.Timeval{Sec: 2, Usec: 10}, Stime: unix.Timeval{Usec: 13},
		Minflt: 10, Majflt: 11, Nvcsw: 12, Nivcsw: 13}
	got := workerCanaryDifference("sha256", 2, 256, 64<<20, 123*time.Microsecond, before, after)
	if got.UserUS != 1000001 || got.SystemUS != 3 || got.WallUS != 123 ||
		got.MinorFaults != 7 || got.MajorFaults != 7 || got.Voluntary != 7 || got.Involuntary != 7 {
		t.Fatalf("rusage difference: %+v", got)
	}
}
