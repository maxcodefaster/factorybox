package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"
)

var criuComposeBefore map[string]any
var criuComposeMarker string
var criuComposeContainerID string

func criuComposeState(id, path string) (map[string]any, error) {
	deadline := time.Now().Add(10 * time.Second)
	var out string
	var err error
	for {
		out, err = criuRunc("exec", id, "docker", "compose", "-f", "/workspace/fixture/compose.yaml", "exec", "-T", "client", "wget", "-qO-", "-T", "3", "http://app:8080"+path)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("compose HTTP state: %s: %w", out, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	var state map[string]any
	if err := json.Unmarshal([]byte(out), &state); err != nil {
		return nil, fmt.Errorf("compose state decode: %s: %w", out, err)
	}
	return state, nil
}

func criuComposeDiagnostic(id, phase string) {
	start := time.Now()
	output := ""
	for _, command := range [][]string{
		{"docker", "compose", "-f", "/workspace/fixture/compose.yaml", "ps", "--all", "--format", "json"},
		{"docker", "compose", "-f", "/workspace/fixture/compose.yaml", "logs", "--no-color", "--tail", "20"},
		{"docker", "network", "inspect", "fixture_default"},
	} {
		out, err := criuRunc(append([]string{"exec", id}, command...)...)
		output += fmt.Sprintf("%v: %s error=%v\n", command, out, err)
	}
	record("sysbox-"+id+"-"+phase+"-compose-diagnostic", start, output, nil)
}

func criuComposeMarkerRead(id string) (string, error) {
	return criuComposeDevcontainerExec(id, "cat", "/workspace/marker.txt")
}

func criuComposeDevcontainerExec(id string, args ...string) (string, error) {
	if criuComposeContainerID == "" {
		return "", fmt.Errorf("Dev Containers up has not returned a container identity")
	}
	base := []string{"--workspace-folder", "/workspace/fixture", "--container-id", criuComposeContainerID, "--log-level", "info"}
	return criuComposeDevcontainerCommand(id, "exec", append(base, args...)...)
}

func criuComposeDevcontainerCommand(id, subcommand string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	command := []string{"--root", criuRoot + "/runtime", "exec", id, "devcontainer", subcommand}
	cmd := exec.CommandContext(ctx, criuRuntime, append(command, args...)...)
	cmd.WaitDelay = 2 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), fmt.Errorf("Dev Containers exec: %s: %w", stderr.String(), err)
	}
	return stdout.String(), nil
}

func criuSysboxComposePrepare(id string) error {
	if os.Getenv("CRIU_SYSBOX_COMPOSE") != "1" {
		return nil
	}
	commands := [][]string{
		{"devcontainer", "--version"},
		{"docker", "compose", "version"},
		{"docker", "load", "-i", "/inner-image.tar"},
		{"/bin/sh", "-c", "cp -a /fixture/. /workspace/fixture; cp /lab /workspace/fixture/lab; docker build --network=none -t experiment-build /workspace/fixture"},
		{"docker", "compose", "-f", "/workspace/fixture/compose.yaml", "up", "-d"},
		{"devcontainer", "up", "--workspace-folder", "/workspace/fixture", "--log-level", "info"},
	}
	for i, command := range commands {
		start := time.Now()
		var out string
		var err error
		if command[0] == "devcontainer" && command[1] == "up" {
			out, err = criuComposeDevcontainerCommand(id, "up", command[2:]...)
			if err == nil {
				var response struct {
					Outcome     string `json:"outcome"`
					ContainerID string `json:"containerId"`
				}
				err = json.Unmarshal([]byte(out), &response)
				if err == nil && (response.Outcome != "success" || response.ContainerID == "") {
					err = fmt.Errorf("Dev Containers up did not return a successful container identity: %s", out)
				}
				if err == nil {
					criuComposeContainerID = response.ContainerID
				}
			}
		} else {
			out, err = criuRunc(append([]string{"exec", id}, command...)...)
		}
		record(fmt.Sprintf("sysbox-compose-prepare-%d", i), start, out, err)
		if err != nil {
			return err
		}
	}
	start := time.Now()
	var err error
	criuComposeBefore, err = criuComposeState(id, "/state?value=947100")
	if err == nil {
		criuComposeMarker, err = criuComposeMarkerRead(id)
	}
	record("sysbox-compose-source-ram-and-devcontainer", start, asJSON(criuComposeBefore)+criuComposeMarker, err)
	if err != nil {
		criuComposeDiagnostic(id, "source")
		return err
	}
	return criuSysboxToolsPrepare(id)
}

func criuSysboxComposeProof(id, phase string) error {
	if os.Getenv("CRIU_SYSBOX_COMPOSE") != "1" {
		return nil
	}
	start := time.Now()
	after, err := criuComposeState(id, "/state")
	if err == nil {
		for _, field := range []string{"nonce", "value", "pid", "hostname", "proc_pid", "pid1_command", "proc_fd_pid", "proc_fd_pid1_command", "cgroup_view", "pids_limit", "cgroup_fd_pids_limit", "cpu_max", "cgroup_fd_cpu_max", "cgroup_readonly", "proc_sys_readonly", "proc_keys_masked", "branch"} {
			if after[field] != criuComposeBefore[field] {
				err = fmt.Errorf("restored compose %s field %s changed: before=%v after=%v", id, field, criuComposeBefore[field], after[field])
				break
			}
		}
	}
	if err == nil {
		oldTicks, oldOK := criuComposeBefore["ticks"].(float64)
		newTicks, newOK := after["ticks"].(float64)
		if !oldOK || !newOK || newTicks <= oldTicks {
			err = fmt.Errorf("restored compose counter did not advance for %s", id)
		}
	}
	marker := ""
	if err == nil {
		marker, err = criuComposeMarkerRead(id)
		if err == nil && marker != criuComposeMarker {
			err = fmt.Errorf("restored devcontainer marker differs for %s: %q", id, marker)
		}
	}
	record("sysbox-"+id+"-"+phase+"-compose-devcontainer", start, asJSON(after)+marker, err)
	if err != nil {
		criuComposeDiagnostic(id, phase)
		return err
	}
	return criuSysboxToolsProof(id, phase)
}

func criuSysboxComposeDivergence(sourceID, copyID string) error {
	if os.Getenv("CRIU_SYSBOX_COMPOSE") != "1" {
		return nil
	}
	start := time.Now()
	states := map[string]any{}
	var err error
	for branch, id := range []string{sourceID, copyID} {
		if _, err = criuComposeState(id, fmt.Sprintf("/state?value=%d", 948000+branch)); err != nil {
			break
		}
		_, err = criuComposeDevcontainerExec(id, "/bin/sh", "-c", fmt.Sprintf("printf 'devcontainer-branch-%d' > /workspace/marker.txt", branch))
		if err != nil {
			break
		}
	}
	if err == nil {
		for branch, id := range []string{sourceID, copyID} {
			var state map[string]any
			state, err = criuComposeState(id, "/state")
			if err == nil && (state["nonce"] != criuComposeBefore["nonce"] || state["value"] != float64(948000+branch)) {
				err = fmt.Errorf("compose RAM branches did not diverge for %s: %v", id, state)
			}
			marker := ""
			if err == nil {
				marker, err = criuComposeMarkerRead(id)
				if err == nil && marker != fmt.Sprintf("devcontainer-branch-%d", branch) {
					err = fmt.Errorf("devcontainer files did not diverge for %s: %q", id, marker)
				}
			}
			states[id] = map[string]any{"state": state, "marker": marker}
			if err != nil {
				break
			}
		}
	}
	record("sysbox-copy-compose-devcontainer-diverge", start, asJSON(states), err)
	if err != nil {
		return err
	}
	if err := criuSysboxToolsDivergence(sourceID, copyID); err != nil {
		return err
	}
	return criuSysboxTheiaProof(sourceID, copyID)
}
