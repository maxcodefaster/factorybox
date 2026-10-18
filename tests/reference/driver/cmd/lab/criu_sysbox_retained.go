package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const criuOwnedSourceCgroup = "/sys/fs/cgroup/criu-probe-sysbox"

// Sysbox's general pause calls mgr.Pause, which chowns the overlay upper back
// to host root and syncs volumes out. A live fork needs the original ownership
// and registrations, so freeze only this already owned kernel cgroup.
func criuSysboxSetSourceFrozen(id string, frozen bool) error {
	if id != "source-sysbox" || os.Getenv("CRIU_SYSBOX_RETAIN_SOURCE") != "1" {
		return fmt.Errorf("kernel freeze is outside the owned retained-source fork")
	}
	identity, err := criuRuntimeIdentity(id)
	if err != nil || identity["host_cgroup"] != "0::/criu-probe-sysbox/init.scope" {
		return fmt.Errorf("kernel freeze source identity mismatch: %v", err)
	}
	value := "0"
	if frozen {
		value = "1"
	}
	if err := os.WriteFile(criuOwnedSourceCgroup+"/cgroup.freeze", []byte(value), 0644); err != nil {
		return err
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		request, readErr := os.ReadFile(criuOwnedSourceCgroup + "/cgroup.freeze")
		data, eventsErr := os.ReadFile(criuOwnedSourceCgroup + "/cgroup.events")
		events, parseErr := criuRAMCounters(string(data))
		want := uint64(0)
		if frozen {
			want = 1
		}
		if readErr == nil && eventsErr == nil && parseErr == nil && strings.TrimSpace(string(request)) == value && events["frozen"] == want && events["populated"] == 1 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("kernel source frozen=%t not confirmed: %s %v %v %v", frozen, data, readErr, eventsErr, parseErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func criuSysboxFrozenKernel() error {
	freeze, err := os.ReadFile(criuOwnedSourceCgroup + "/cgroup.freeze")
	if err != nil || strings.TrimSpace(string(freeze)) != "1" {
		return fmt.Errorf("owned source freeze not requested: %v", err)
	}
	data, err := os.ReadFile(criuOwnedSourceCgroup + "/cgroup.events")
	if err != nil {
		return err
	}
	events, err := criuRAMCounters(string(data))
	if err != nil || events["frozen"] != 1 || events["populated"] != 1 {
		return fmt.Errorf("kernel has not frozen the populated source: %s %v", data, err)
	}
	return nil
}

func criuSysboxThreadStart(pid int) (string, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", err
	}
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 {
		return "", fmt.Errorf("missing thread command boundary")
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) < 20 || fields[0] == "Z" || fields[0] == "X" {
		return "", fmt.Errorf("source thread %d is absent or dead", pid)
	}
	start, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil || start == 0 {
		return "", fmt.Errorf("invalid source start time: %v", err)
	}
	return fields[19], nil
}

func criuSysboxFrozenThreads() (map[int]string, error) {
	deadline := time.Now().Add(3 * time.Second)
	for {
		err := criuSysboxFrozenKernel()
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			return nil, err
		}
		time.Sleep(10 * time.Millisecond)
	}
	threads, err := criuSourceThreads("sysbox")
	if err != nil {
		return nil, err
	}
	identities := map[int]string{}
	for _, pid := range threads {
		if pid == os.Getpid() {
			return nil, fmt.Errorf("controller cannot belong to frozen source")
		}
		start, err := criuSysboxThreadStart(pid)
		if err != nil {
			return nil, err
		}
		identities[pid] = start
	}
	return identities, nil
}

func criuSysboxFrozenSourceProof(before map[int]string, phase string) error {
	start := time.Now()
	after, err := criuSysboxFrozenThreads()
	if err == nil && asJSON(before) != asJSON(after) {
		err = fmt.Errorf("original source thread identities changed while frozen")
	}
	record("sysbox-retained-source-"+phase, start, asJSON(map[string]any{
		"source_cgroup": criuOwnedSourceCgroup, "thread_start_times": after,
		"scope": "kernel reports populated/frozen; every original host TID and start time retained"}), err)
	return err
}
