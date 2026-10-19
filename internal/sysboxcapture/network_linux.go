//go:build linux

package sysboxcapture

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

const networkOutputLimit = 1 << 20

type captureNetwork struct {
	namespace  *os.File
	pid        int
	start      string
	before     map[string][]byte
	generation string
	processes  map[int]socketProcess
	sockets    []captureSocket
}

// CaptureNetwork holds the original private namespace across the dump. It
// never joins or changes the worker's namespace.
type CaptureNetwork struct{ state captureNetwork }

type networkOutput struct {
	bytes.Buffer
	exceeded bool
}

func (b *networkOutput) Write(data []byte) (int, error) {
	n := len(data)
	remaining := networkOutputLimit - b.Len()
	if len(data) > remaining {
		b.exceeded = true
		data = data[:remaining]
	}
	b.Buffer.Write(data)
	return n, nil
}

func (n *CaptureNetwork) command(program string, input []byte, arguments ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	args := []string{"--net=/proc/self/fd/3", "--", program}
	args = append(args, arguments...)
	command := exec.CommandContext(ctx, "nsenter", args...)
	command.ExtraFiles = []*os.File{n.state.namespace}
	if input != nil {
		command.Stdin = bytes.NewReader(input)
	}
	var output, diagnostic networkOutput
	command.Stdout, command.Stderr = &output, &diagnostic
	err := command.Run()
	if output.exceeded || diagnostic.exceeded {
		return nil, fmt.Errorf("private network command output exceeds bound")
	}
	if err != nil {
		return nil, fmt.Errorf("private network %s: %w", program, err)
	}
	return output.Bytes(), nil
}

func networkProcessStart(pid int) (string, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", err
	}
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 {
		return "", fmt.Errorf("network source process has no command boundary")
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) < 20 || fields[0] == "Z" || fields[0] == "X" {
		return "", fmt.Errorf("network source is absent or dead")
	}
	return fields[19], nil
}

func (n *CaptureNetwork) validate() error {
	start, err := networkProcessStart(n.state.pid)
	if err != nil || start != n.state.start {
		return fmt.Errorf("private network source process identity changed: %v", err)
	}
	original, err := n.state.namespace.Stat()
	current, currentErr := os.Stat(fmt.Sprintf("/proc/%d/ns/net", n.state.pid))
	if err != nil || currentErr != nil || !os.SameFile(original, current) {
		return fmt.Errorf("private network source namespace changed: %v", errors.Join(err, currentErr))
	}
	return nil
}

var networkCounters = regexp.MustCompile(`\[[0-9]+:[0-9]+\]`)

func networkLines(data []byte) []string {
	var result []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			fields := strings.Fields(line)
			if len(fields) == 3 && strings.HasPrefix(fields[0], ":") &&
				networkCounters.FindString(fields[2]) == fields[2] {
				line = fields[0] + " " + fields[1] + " [counters]"
			}
			result = append(result, line)
		}
	}
	return result
}

func networkHasCRIU(lines []string) bool {
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == ":CRIU" {
			return true
		}
	}
	return false
}

// A baseline without CRIU plus exactly its pinned lock is the only state
// from which the first undo transaction is admitted. No whole-table restore.
func knownCRIUNetworkDelta(before, current []byte) bool {
	baseline := networkLines(before)
	if networkHasCRIU(baseline) {
		return false
	}
	expected := map[string]int{
		":CRIU - [counters]":                      1,
		"-A INPUT -j CRIU":                        1,
		"-A OUTPUT -j CRIU":                       1,
		"-A CRIU -m mark --mark 0xc114 -j ACCEPT": 1,
		"-A CRIU -j DROP":                         1,
	}
	var remaining []string
	for _, line := range networkLines(current) {
		if _, present := expected[line]; present {
			expected[line]--
		} else {
			remaining = append(remaining, line)
		}
	}
	for _, count := range expected {
		if count != 0 {
			return false
		}
	}
	return strings.Join(remaining, "\n") == strings.Join(baseline, "\n")
}

func criuEmptyNetworkChain(before, current []byte) bool {
	var remaining []string
	found := false
	for _, line := range networkLines(current) {
		if line == ":CRIU - [counters]" && !found {
			found = true
			continue
		}
		remaining = append(remaining, line)
	}
	return found && !networkHasCRIU(remaining) &&
		strings.Join(remaining, "\n") == strings.Join(networkLines(before), "\n")
}

// BeginCaptureNetwork is called only after the lifecycle guard has verified
// the original frozen source. The snapshot lives in its private generation.
func BeginCaptureNetwork(pid int, start, generation string) (_ *CaptureNetwork, resultErr error) {
	n := &CaptureNetwork{state: captureNetwork{pid: pid, start: start, generation: generation,
		before: map[string][]byte{}, processes: map[int]socketProcess{}}}
	var err error
	n.state.namespace, err = os.Open(fmt.Sprintf("/proc/%d/ns/net", pid))
	if err != nil {
		return nil, err
	}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, n.Close())
		}
	}()
	original, err := n.state.namespace.Stat()
	worker, workerErr := os.Stat("/proc/self/ns/net")
	if err != nil || workerErr != nil || os.SameFile(original, worker) {
		return nil, fmt.Errorf("network recovery requires a separate private source namespace")
	}
	if err := n.validate(); err != nil {
		return nil, err
	}
	for _, program := range []string{"iptables", "ip6tables"} {
		rules, err := n.command(program+"-save", nil, "-t", "filter")
		if err != nil {
			return nil, err
		}
		if networkHasCRIU(networkLines(rules)) {
			return nil, fmt.Errorf("source network already contains a CRIU chain or reference")
		}
		n.state.before[program] = append([]byte(nil), rules...)
	}
	data, err := json.Marshal(map[string]any{
		"version": 1, "source_pid": pid, "source_start": start,
		"filter_rules": n.state.before,
	})
	if err != nil {
		return nil, err
	}
	if err := writePrivate(generation+"/network-before.json", append(data, '\n')); err != nil {
		return nil, err
	}
	return n, nil
}

func (n *CaptureNetwork) Close() error {
	result := n.state.namespace.Close()
	for _, process := range n.state.processes {
		result = errors.Join(result, process.file.Close())
	}
	return result
}

// Rollback removes only an exactly recognized CRIU delta after the capture
// worker has returned and the caller has revalidated frozen, untraced source
// ownership. This does not establish complete process rollback or thaw it.
func (n *CaptureNetwork) Rollback() (resultErr error) {
	if err := n.validate(); err != nil {
		return err
	}
	current := map[string][]byte{}
	for _, program := range []string{"iptables", "ip6tables"} {
		rules, err := n.command(program+"-save", nil, "-t", "filter")
		if err != nil {
			return err
		}
		before := n.state.before[program]
		if strings.Join(networkLines(before), "\n") != strings.Join(networkLines(rules), "\n") &&
			!knownCRIUNetworkDelta(before, rules) && !criuEmptyNetworkChain(before, rules) {
			return fmt.Errorf("private %s rules have changes outside the owned CRIU delta", program)
		}
		current[program] = rules
	}
	sockets, err := n.prepareSocketRollback()
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, closeSocketRollback(sockets)) }()
	for _, program := range []string{"iptables", "ip6tables"} {
		before := n.state.before[program]
		rules := current[program]
		if knownCRIUNetworkDelta(before, rules) {
			if err := n.validate(); err != nil {
				return err
			}
			removeJumps := []byte("*filter\n:CRIU - [0:0]\n-D INPUT -j CRIU\n-D OUTPUT -j CRIU\nCOMMIT\n")
			if _, err := n.command(program+"-restore", removeJumps, "--noflush", "--wait", "2"); err != nil {
				return err
			}
			var err error
			rules, err = n.command(program+"-save", nil, "-t", "filter")
			if err != nil {
				return err
			}
		}
		if criuEmptyNetworkChain(before, rules) {
			if err := n.validate(); err != nil {
				return err
			}
			removeChain := []byte("*filter\n:CRIU - [0:0]\n-X CRIU\nCOMMIT\n")
			if _, err := n.command(program+"-restore", removeChain, "--noflush", "--wait", "2"); err != nil {
				return err
			}
		}
		final, err := n.command(program+"-save", nil, "-t", "filter")
		if err != nil {
			return err
		}
		if strings.Join(networkLines(before), "\n") != strings.Join(networkLines(final), "\n") {
			return fmt.Errorf("private %s rules did not return to their original baseline", program)
		}
	}
	if err := n.validate(); err != nil {
		return err
	}
	return restoreCaptureSockets(sockets)
}

var _ io.Writer = (*networkOutput)(nil)
