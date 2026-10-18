package main

import (
	"fmt"
	"strconv"
	"strings"
)

type criuPhaseCounters struct {
	Cgroup              string                       `json:"cgroup"`
	Device              uint64                       `json:"device"`
	Inode               uint64                       `json:"inode"`
	CPU                 map[string]uint64            `json:"cpu"`
	IO                  map[string]map[string]uint64 `json:"io"`
	Pressure            map[string]map[string]uint64 `json:"pressure_total_us"`
	PressureUnavailable map[string]string            `json:"pressure_unavailable,omitempty"`
	Memory              map[string]uint64            `json:"memory"`
	MemoryEvents        map[string]uint64            `json:"memory_events"`
	Current             uint64                       `json:"memory_current_bytes"`
	Swap                uint64                       `json:"memory_swap_bytes"`
	CollectionUS        int64                        `json:"collection_us"`
}

type criuPhaseDelta struct {
	CPU          map[string]uint64            `json:"cpu"`
	IO           map[string]map[string]uint64 `json:"io"`
	Pressure     map[string]map[string]uint64 `json:"pressure_total_us"`
	Memory       map[string]uint64            `json:"memory_counters"`
	MemoryEvents map[string]uint64            `json:"memory_events"`
}

var criuPhaseMemoryCounters = []string{"pgfault", "pgmajfault", "pgscan_direct", "pgscan_kswapd", "pswpin", "pswpout", "workingset_refault_file"}
var criuPhaseMemoryGauges = []string{"anon", "file", "shmem", "swapcached"}

func criuPhaseIO(data string) (map[string]map[string]uint64, error) {
	result := map[string]map[string]uint64{}
	for _, line := range strings.Split(strings.TrimSpace(data), "\n") {
		if line == "" { // An idle cgroup may have no accounted device.
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			return nil, fmt.Errorf("invalid I/O device counters: %q", line)
		}
		device := strings.Split(fields[0], ":")
		if len(device) != 2 {
			return nil, fmt.Errorf("invalid I/O device counters: %q", line)
		}
		for _, part := range device {
			if _, err := strconv.ParseUint(part, 10, 32); err != nil {
				return nil, fmt.Errorf("invalid I/O device: %q", fields[0])
			}
		}
		if _, exists := result[fields[0]]; exists {
			return nil, fmt.Errorf("duplicate I/O device: %q", fields[0])
		}
		values := map[string]uint64{}
		// The kernel can emit only an idle device header. Preserve its
		// empty counter map instead of inventing zero-valued measurements.
		for _, field := range fields[1:] {
			pair := strings.Split(field, "=")
			if len(pair) != 2 || pair[0] == "" {
				return nil, fmt.Errorf("invalid I/O counter: %q", field)
			}
			if _, exists := values[pair[0]]; exists {
				return nil, fmt.Errorf("duplicate I/O counter: %q", pair[0])
			}
			value, err := strconv.ParseUint(pair[1], 10, 64)
			if err != nil {
				return nil, err
			}
			values[pair[0]] = value
		}
		result[fields[0]] = values
	}
	return result, nil
}

// Retain cumulative stall time, not rolling averages from earlier phases.
func criuPhasePressure(data string) (map[string]uint64, error) {
	result := map[string]uint64{}
	for _, line := range strings.Split(strings.TrimSpace(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || (fields[0] != "some" && fields[0] != "full") {
			return nil, fmt.Errorf("invalid pressure class: %q", line)
		}
		if _, exists := result[fields[0]]; exists {
			return nil, fmt.Errorf("duplicate pressure class: %q", fields[0])
		}
		found := false
		for _, field := range fields[1:] {
			if !strings.HasPrefix(field, "total=") {
				continue
			}
			if found {
				return nil, fmt.Errorf("duplicate pressure total")
			}
			value, err := strconv.ParseUint(strings.TrimPrefix(field, "total="), 10, 64)
			if err != nil {
				return nil, err
			}
			result[fields[0]], found = value, true
		}
		if !found {
			return nil, fmt.Errorf("missing pressure total")
		}
	}
	if _, exists := result["some"]; !exists {
		return nil, fmt.Errorf("missing pressure some class")
	}
	return result, nil
}

func criuPhaseCounterDelta(before, after map[string]uint64) (map[string]uint64, error) {
	result := map[string]uint64{}
	for key, value := range before {
		current, exists := after[key]
		if !exists || current < value {
			return nil, fmt.Errorf("counter disappeared or reset: %s", key)
		}
		result[key] = current - value
	}
	for key, value := range after {
		if _, exists := before[key]; !exists {
			result[key] = value // Newly accounted device/field starts at zero.
		}
	}
	return result, nil
}

func criuPhaseNestedDelta(before, after map[string]map[string]uint64) (map[string]map[string]uint64, error) {
	result := map[string]map[string]uint64{}
	for key, previous := range before {
		current, exists := after[key]
		if !exists {
			return nil, fmt.Errorf("counter group disappeared: %s", key)
		}
		values, err := criuPhaseCounterDelta(previous, current)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		result[key] = values
	}
	for key, current := range after {
		if _, exists := before[key]; !exists {
			values, err := criuPhaseCounterDelta(nil, current)
			if err != nil {
				return nil, err
			}
			result[key] = values
		}
	}
	return result, nil
}

func criuPhaseDifference(before, after criuPhaseCounters) (result criuPhaseDelta, err error) {
	if before.Cgroup != after.Cgroup || before.Device != after.Device || before.Inode != after.Inode {
		return result, fmt.Errorf("owned cgroup identity changed between samples")
	}
	if len(before.PressureUnavailable) != len(after.PressureUnavailable) {
		return result, fmt.Errorf("pressure counter availability changed between samples")
	}
	for name, reason := range before.PressureUnavailable {
		if after.PressureUnavailable[name] != reason {
			return result, fmt.Errorf("%s pressure availability changed between samples", name)
		}
	}
	if result.CPU, err = criuPhaseCounterDelta(before.CPU, after.CPU); err != nil {
		return result, err
	}
	if result.IO, err = criuPhaseNestedDelta(before.IO, after.IO); err != nil {
		return result, err
	}
	if result.Pressure, err = criuPhaseNestedDelta(before.Pressure, after.Pressure); err != nil {
		return result, err
	}
	if result.MemoryEvents, err = criuPhaseCounterDelta(before.MemoryEvents, after.MemoryEvents); err != nil {
		return result, err
	}
	previous, current := map[string]uint64{}, map[string]uint64{}
	for _, key := range criuPhaseMemoryCounters {
		if value, exists := before.Memory[key]; exists {
			previous[key] = value
		}
		if value, exists := after.Memory[key]; exists {
			current[key] = value
		}
	}
	result.Memory, err = criuPhaseCounterDelta(previous, current)
	return result, err
}
