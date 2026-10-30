//go:build linux

package worker

import (
	"fmt"
	"strconv"
	"strings"
)

func validateWorkerResourceLimits(memory, cpu []byte) error {
	// The outer cgroup bounds a whole trust-domain worker, not one Sandbox.
	// Consumers choose that finite aggregate budget before worker admission.
	// The 3 CPU / 3 GiB native fixture is a qualification condition, not a
	// production maximum shared by every descendant Sandbox.
	maximum, err := strconv.ParseInt(strings.TrimSpace(string(memory)), 10, 64)
	if err != nil || maximum <= 0 {
		return fmt.Errorf("worker requires a finite positive cgroup memory limit")
	}
	parts := strings.Fields(string(cpu))
	if len(parts) != 2 {
		return fmt.Errorf("worker CPU limit missing")
	}
	quota, e1 := strconv.ParseInt(parts[0], 10, 64)
	period, e2 := strconv.ParseInt(parts[1], 10, 64)
	if e1 != nil || e2 != nil || quota <= 0 || period <= 0 {
		return fmt.Errorf("worker requires a finite positive cgroup CPU limit")
	}
	return nil
}
