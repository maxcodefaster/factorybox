package main

import (
	"encoding/json"
	"fmt"
	"time"
)

func criuLiveExecState(id, inner, path string) (map[string]any, error) {
	args := []string{"exec", id}
	if inner != "" {
		args = append(args, "docker", "exec", inner)
	}
	args = append(args, "env", "LAB_FIXTURE_PORT=8090", "/lab", "get", path)
	deadline := time.Now().Add(5 * time.Second)
	for {
		out, err := criuRunc(args...)
		if err == nil {
			var state map[string]any
			if err := json.Unmarshal([]byte(out), &state); err != nil {
				return nil, fmt.Errorf("decode live exec state: %s: %w", out, err)
			}
			return state, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("live exec %s/%s: %s: %w", id, inner, out, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Keep a Sysbox exec and a Docker exec running through checkpoint/fork. Each
// process owns a separate RAM nonce, mutable value and advancing counter.
func criuSysboxStartLiveExecs(id string) (map[string]map[string]any, error) {
	start := time.Now()
	states := map[string]map[string]any{}
	for i, inner := range []string{"", "alpha"} {
		args := []string{"exec", "--detach", id}
		if inner != "" {
			args = []string{"exec", id, "docker", "exec", "--detach", inner}
		}
		args = append(args, "/bin/sh", "-c", "exec env LAB_FIXTURE_PORT=8090 /lab fixture </dev/null >/workspace/live-exec.log 2>&1")
		out, err := criuRunc(args...)
		if err == nil {
			states[inner], err = criuLiveExecState(id, inner, fmt.Sprintf("/state?value=%d", 919000+i))
		}
		if err != nil {
			record("sysbox-live-execs-ready", start, out+asJSON(states), err)
			return nil, err
		}
	}
	record("sysbox-live-execs-ready", start, asJSON(states), nil)
	return states, nil
}

func criuSysboxLiveExecProof(sourceID, copyID string, before map[string]map[string]any) error {
	start := time.Now()
	states := map[string]map[string]map[string]any{}
	for _, id := range []string{sourceID, copyID} {
		states[id] = map[string]map[string]any{}
		for _, inner := range []string{"", "alpha"} {
			after, err := criuLiveExecState(id, inner, "/state")
			if err == nil {
				for _, field := range []string{"nonce", "value", "pid", "hostname", "proc_pid", "pid1_command", "proc_fd_pid", "proc_fd_pid1_command", "cgroup_view", "pids_limit", "cgroup_fd_pids_limit", "cpu_max", "cgroup_fd_cpu_max", "cgroup_readonly", "proc_sys_readonly", "proc_keys_masked", "branch"} {
					if after[field] != before[inner][field] {
						err = fmt.Errorf("live exec %s/%s field %s changed: before=%v after=%v", id, inner, field, before[inner][field], after[field])
						break
					}
				}
			}
			if err == nil {
				oldTicks, oldOK := before[inner]["ticks"].(float64)
				newTicks, newOK := after["ticks"].(float64)
				if !oldOK || !newOK || newTicks <= oldTicks {
					err = fmt.Errorf("live exec counter did not advance for %s/%s", id, inner)
				}
			}
			if err != nil {
				record("sysbox-live-execs-restored", start, asJSON(states), err)
				return err
			}
			states[id][inner] = after
		}
	}
	record("sysbox-live-execs-restored", start, asJSON(states), nil)
	start = time.Now()
	for branch, id := range []string{sourceID, copyID} {
		for i, inner := range []string{"", "alpha"} {
			if _, err := criuLiveExecState(id, inner, fmt.Sprintf("/state?value=%d", 881000+1000*branch+i)); err != nil {
				return err
			}
		}
	}
	for branch, id := range []string{sourceID, copyID} {
		for i, inner := range []string{"", "alpha"} {
			after, err := criuLiveExecState(id, inner, "/state")
			if err == nil && (after["value"] != float64(881000+1000*branch+i) || after["nonce"] != before[inner]["nonce"]) {
				err = fmt.Errorf("live exec branches failed to diverge for %s/%s: %v", id, inner, after)
			}
			if err != nil {
				record("sysbox-live-execs-diverge", start, asJSON(states), err)
				return err
			}
			states[id][inner] = after
		}
	}
	record("sysbox-live-execs-diverge", start, asJSON(states), nil)
	return nil
}
