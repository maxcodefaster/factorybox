//go:build linux

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

func criuSysboxPhaseCountersRead() (result criuPhaseCounters, err error) {
	start := time.Now()
	defer func() { result.CollectionUS = time.Since(start).Microseconds() }()
	membership, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return result, err
	}
	result.Cgroup, err = criuRAMCgroupPath(string(membership))
	if err != nil {
		return result, err
	}
	info, err := os.Stat(result.Cgroup)
	if err != nil {
		return result, err
	}
	identity := info.Sys().(*syscall.Stat_t)
	result.Device, result.Inode = uint64(identity.Dev), identity.Ino
	var fs unix.Statfs_t
	if err = unix.Statfs(result.Cgroup, &fs); err != nil || fs.Type != unix.CGROUP2_SUPER_MAGIC {
		return result, fmt.Errorf("phase counters require the actual cgroup-v2 mount: %v", err)
	}
	read := func(name string) (string, error) {
		data, readErr := os.ReadFile(filepath.Join(result.Cgroup, name))
		return string(data), readErr
	}
	data, err := read("cpu.stat")
	if err != nil {
		return result, err
	}
	result.CPU, err = criuRAMCounters(data)
	if err != nil {
		return result, err
	}
	if _, exists := result.CPU["usage_usec"]; !exists {
		return result, fmt.Errorf("missing cumulative cgroup CPU usage")
	}
	data, err = read("io.stat")
	if err != nil {
		return result, err
	}
	result.IO, err = criuPhaseIO(data)
	if err != nil {
		return result, err
	}
	result.Pressure = map[string]map[string]uint64{}
	result.PressureUnavailable = map[string]string{}
	for _, name := range []string{"cpu", "io", "memory"} {
		data, err = read(name + ".pressure")
		if os.IsNotExist(err) {
			// PSI is optional on Linux workers. Absence is not zero stalls.
			result.PressureUnavailable[name] = "worker exposes no cgroup " + name + ".pressure"
			continue
		}
		if err != nil {
			return result, err
		}
		result.Pressure[name], err = criuPhasePressure(data)
		if err != nil {
			return result, fmt.Errorf("%s pressure: %w", name, err)
		}
	}
	data, err = read("memory.stat")
	if err != nil {
		return result, err
	}
	memory, err := criuRAMCounters(data)
	if err != nil {
		return result, err
	}
	result.Memory = map[string]uint64{}
	keys := append(append([]string{}, criuPhaseMemoryCounters...), criuPhaseMemoryGauges...)
	for _, name := range keys {
		if value, exists := memory[name]; exists {
			result.Memory[name] = value
		}
	}
	data, err = read("memory.events")
	if err != nil {
		return result, err
	}
	result.MemoryEvents, err = criuRAMCounters(data)
	if err != nil {
		return result, err
	}
	for name, target := range map[string]*uint64{"memory.current": &result.Current, "memory.swap.current": &result.Swap} {
		data, err = read(name)
		if err != nil {
			return result, err
		}
		*target, err = strconv.ParseUint(strings.TrimSpace(data), 10, 64)
		if err != nil {
			return result, err
		}
	}
	return result, nil
}
