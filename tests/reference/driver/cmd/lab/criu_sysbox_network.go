package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Resolve alpha through Docker's embedded DNS from beta and reach it across
// their real veth/bridge network. Repeat after divergent branch mutations so a
// copied peer accidentally pointing into the original cannot pass this gate.
func criuSysboxNetworkProof(id, phase string) error {
	if os.Getenv("CRIU_SYSBOX_NETWORK") != "1" {
		return nil
	}
	start := time.Now()
	local, err := criuState(id, "alpha", "/state")
	output := ""
	var remote map[string]any
	if err == nil {
		output, err = criuRunc("exec", id, "docker", "exec", "beta", "wget", "-qO-", "-T", "3", "http://alpha:8080/state")
	}
	if err == nil {
		err = json.Unmarshal([]byte(output), &remote)
	}
	if err == nil {
		for _, field := range []string{"nonce", "value", "branch", "hostname", "pids_limit", "cgroup_fd_pids_limit"} {
			if remote[field] != local[field] {
				err = fmt.Errorf("Docker DNS/bridge reached wrong branch for %s, field %s: local=%v remote=%v", id, field, local[field], remote[field])
				break
			}
		}
	}
	record("sysbox-"+id+"-"+phase+"-docker-dns-bridge", start, output, err)
	return err
}
