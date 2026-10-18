//go:build !linux

package main

import "fmt"

func criuSysboxPhaseCountersRead() (criuPhaseCounters, error) {
	return criuPhaseCounters{}, fmt.Errorf("owned phase counters require native Linux cgroup-v2")
}
