package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// Optional diagnostics only. These reads neither change worker settings nor
// exclude their own cost from the complete-readiness clock.
type descendantResourceSample struct {
	Phase          string                       `json:"phase"`
	ObservedUS     int64                        `json:"observed_us"`
	CollectionUS   int64                        `json:"collection_us"`
	CPUMax         string                       `json:"cpu_max"`
	CPU            map[string]uint64            `json:"cpu"`
	IO             map[string]map[string]uint64 `json:"io"`
	Memory         map[string]uint64            `json:"memory"`
	Pressure       map[string]map[string]uint64 `json:"pressure"`
	WorkerPressure map[string]map[string]uint64 `json:"worker_pressure"`
	WorkerMemory   map[string]uint64            `json:"worker_memory"`
	WorkerCPU      map[string]uint64            `json:"worker_cpu_ticks"`
	Self           map[string]uint64            `json:"self"`
	Children       map[string]uint64            `json:"reaped_children"`
	Unavailable    []string                     `json:"unavailable"`
	Errors         []string                     `json:"errors"`
}

func descendantResourceCounters(data string) (map[string]uint64, error) {
	result := map[string]uint64{}
	for _, line := range strings.Split(strings.TrimSpace(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return nil, fmt.Errorf("invalid resource counter")
		}
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return nil, err
		}
		if _, exists := result[fields[0]]; exists {
			return nil, fmt.Errorf("duplicate resource counter")
		}
		result[fields[0]] = value
	}
	return result, nil
}

func descendantResourcePressure(data string) (map[string]uint64, error) {
	result := map[string]uint64{}
	for _, line := range strings.Split(strings.TrimSpace(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 5 || (fields[0] != "some" && fields[0] != "full") {
			return nil, fmt.Errorf("invalid resource pressure row")
		}
		if _, exists := result[fields[0]]; exists || !strings.HasPrefix(fields[4], "total=") {
			return nil, fmt.Errorf("invalid resource pressure total")
		}
		value, err := strconv.ParseUint(strings.TrimPrefix(fields[4], "total="), 10, 64)
		if err != nil {
			return nil, err
		}
		result[fields[0]] = value
	}
	if _, exists := result["some"]; !exists {
		return nil, fmt.Errorf("resource pressure lacks some total")
	}
	return result, nil
}

func descendantReadResources(start time.Time, phase string) descendantResourceSample {
	begin := time.Now()
	sample := descendantResourceSample{Phase: phase,
		IO: map[string]map[string]uint64{}, Memory: map[string]uint64{},
		Pressure: map[string]map[string]uint64{}, WorkerPressure: map[string]map[string]uint64{},
		WorkerMemory: map[string]uint64{}, Errors: []string{}}
	read := func(path string) string {
		data, err := os.ReadFile(path)
		if err != nil || len(data) > 64<<10 {
			sample.Errors = append(sample.Errors, path)
			return ""
		}
		return string(data)
	}
	parse := func(path string, pressure bool) map[string]uint64 {
		var data string
		if pressure {
			raw, err := os.ReadFile(path)
			if os.IsNotExist(err) || os.IsPermission(err) {
				sample.Unavailable = append(sample.Unavailable, path)
				return nil
			}
			if err != nil || len(raw) > 64<<10 {
				sample.Errors = append(sample.Errors, path)
				return nil
			}
			data = string(raw)
		} else {
			data = read(path)
		}
		if data == "" {
			return nil
		}
		decode := descendantResourceCounters
		if pressure {
			decode = descendantResourcePressure
		}
		value, err := decode(data)
		if err != nil {
			sample.Errors = append(sample.Errors, path+":parse")
		}
		return value
	}
	group, err := criuRAMCgroupPath(read("/proc/self/cgroup"))
	if err != nil {
		sample.Errors = append(sample.Errors, "self-cgroup:parse")
	} else {
		sample.CPUMax = strings.TrimSpace(read(filepath.Join(group, "cpu.max")))
		sample.CPU = parse(filepath.Join(group, "cpu.stat"), false)
		memory := parse(filepath.Join(group, "memory.stat"), false)
		for _, key := range []string{"anon", "file", "shmem", "file_dirty", "file_writeback",
			"pgmajfault", "pgscan", "pgsteal", "workingset_refault_file"} {
			sample.Memory[key] = memory[key]
		}
		for _, key := range []string{"memory.current", "memory.peak"} {
			value, err := strconv.ParseUint(strings.TrimSpace(read(filepath.Join(group, key))), 10, 64)
			if err != nil {
				sample.Errors = append(sample.Errors, key+":parse")
			} else {
				sample.Memory[key] = value
			}
		}
		sample.IO, err = criuPhaseIO(read(filepath.Join(group, "io.stat")))
		if err != nil {
			sample.Errors = append(sample.Errors, "io.stat:parse")
		}
		for _, resource := range []string{"cpu", "io", "memory"} {
			sample.Pressure[resource] = parse(filepath.Join(group, resource+".pressure"), true)
		}
	}
	for _, resource := range []string{"cpu", "io", "memory"} {
		sample.WorkerPressure[resource] = parse("/proc/pressure/"+resource, true)
	}
	for _, line := range strings.Split(read("/proc/meminfo"), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		key := strings.TrimSuffix(fields[0], ":")
		if key != "MemTotal" && key != "MemAvailable" && key != "Dirty" && key != "Writeback" {
			continue
		}
		if len(fields) != 3 || fields[2] != "kB" {
			sample.Errors = append(sample.Errors, "meminfo:"+key)
			continue
		}
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			sample.Errors = append(sample.Errors, "meminfo:"+key+":parse")
		} else {
			sample.WorkerMemory[key] = value * 1024
		}
	}
	sample.WorkerCPU = map[string]uint64{}
	lines := strings.SplitN(read("/proc/stat"), "\n", 2)
	fields := strings.Fields(lines[0])
	if len(fields) < 9 || fields[0] != "cpu" {
		sample.Errors = append(sample.Errors, "worker-cpu:parse")
	} else {
		for index, key := range []string{"user", "nice", "system", "idle", "iowait", "irq", "softirq", "steal"} {
			value, err := strconv.ParseUint(fields[index+1], 10, 64)
			if err != nil {
				sample.Errors = append(sample.Errors, "worker-cpu:"+key)
			} else {
				sample.WorkerCPU[key] = value
			}
		}
	}
	usage := func(who int) map[string]uint64 {
		var value unix.Rusage
		if err := unix.Getrusage(who, &value); err != nil {
			sample.Errors = append(sample.Errors, "rusage")
			return nil
		}
		return map[string]uint64{
			"user_us":      uint64(value.Utime.Sec)*1_000_000 + uint64(value.Utime.Usec),
			"system_us":    uint64(value.Stime.Sec)*1_000_000 + uint64(value.Stime.Usec),
			"major_faults": uint64(value.Majflt), "in_blocks": uint64(value.Inblock),
			"out_blocks": uint64(value.Oublock), "voluntary_switches": uint64(value.Nvcsw),
			"involuntary_switches": uint64(value.Nivcsw),
		}
	}
	sample.Self, sample.Children = usage(unix.RUSAGE_SELF), usage(unix.RUSAGE_CHILDREN)
	sample.CollectionUS, sample.ObservedUS = time.Since(begin).Microseconds(), time.Since(start).Microseconds()
	return sample
}
