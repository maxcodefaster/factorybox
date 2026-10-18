package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"time"
)

type captureNetwork struct {
	anchor *os.File
	before map[string][]string
}

var captureChainCounters = regexp.MustCompile(`^(:[^ ]+ [^ ]+) \[[0-9]+:[0-9]+\]$`)
var captureCRIUReference = regexp.MustCompile(`(?:^| )-(?:j|g) CRIU(?: |$)`)

func captureNetworkRules(text string) ([]string, error) {
	if len(text) > 1<<20 {
		return nil, fmt.Errorf("capture firewall table exceeds its bound")
	}
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines = append(lines, captureChainCounters.ReplaceAllString(line, "$1"))
	}
	if len(lines) < 2 || lines[0] != "*filter" || lines[len(lines)-1] != "COMMIT" {
		return nil, fmt.Errorf("capture firewall filter table is incomplete")
	}
	return lines, nil
}

func captureNetworkLocked(before, current []string) (bool, error) {
	for _, line := range before {
		if strings.HasPrefix(line, ":CRIU ") || captureCRIUReference.MatchString(line) {
			return false, fmt.Errorf("original firewall already uses the reserved CRIU chain")
		}
	}
	if slices.Equal(before, current) {
		return false, nil
	}
	expected := map[string]int{
		":CRIU -": 1, "-A INPUT -j CRIU": 1, "-A OUTPUT -j CRIU": 1,
		"-A CRIU -m mark --mark 0xc114 -j ACCEPT": 1, "-A CRIU -j DROP": 1,
	}
	var original []string
	for _, line := range current {
		if count, ok := expected[line]; ok {
			if count != 1 {
				return false, fmt.Errorf("capture network lock contains a duplicate owned rule")
			}
			expected[line] = 0
		} else {
			original = append(original, line)
		}
	}
	for _, count := range expected {
		if count != 0 {
			return false, fmt.Errorf("capture network lock is incomplete or unknown")
		}
	}
	if !slices.Equal(before, original) {
		return false, fmt.Errorf("capture network lock includes unrelated firewall changes")
	}
	return true, nil
}

func (network *captureNetwork) command(binary string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	argv := append([]string{"--net=/proc/self/fd/3", "--", binary}, args...)
	command := exec.CommandContext(ctx, "nsenter", argv...)
	command.ExtraFiles = []*os.File{network.anchor}
	command.WaitDelay = time.Second
	out, err := command.CombinedOutput()
	return string(out), err
}

func captureNetworkOpen(guard *descendantGuard) (*captureNetwork, error) {
	if err := descendantGuardValidate(guard.config, guard.anchor); err != nil {
		return nil, err
	}
	return captureNetworkOriginal(guard.config.PID)
}

func captureNetworkOriginal(pid int) (*captureNetwork, error) {
	anchor, err := os.Open(fmt.Sprintf("/proc/%d/ns/net", pid))
	if err != nil {
		return nil, err
	}
	network := &captureNetwork{anchor: anchor, before: map[string][]string{}}
	original, err := anchor.Stat()
	self, selfErr := os.Stat("/proc/self/ns/net")
	if err != nil || selfErr != nil || os.SameFile(original, self) {
		anchor.Close()
		return nil, fmt.Errorf("capture recovery requires a private source network namespace: %v %v", err, selfErr)
	}
	for _, binary := range []string{"iptables", "ip6tables"} {
		text, err := network.command(binary+"-save", "-t", "filter")
		if err == nil {
			network.before[binary], err = captureNetworkRules(text)
		}
		if err == nil {
			_, err = captureNetworkLocked(network.before[binary], network.before[binary])
		}
		if err != nil {
			anchor.Close()
			return nil, fmt.Errorf("capture original %s rules: %s %w", binary, text, err)
		}
	}
	return network, nil
}

func (network *captureNetwork) recover(guard *descendantGuard, prefix string) error {
	if err := descendantGuardValidate(guard.config, guard.anchor); err != nil {
		return err
	}
	events, err := descendantGuardEvents(guard.anchor.group)
	if err != nil || events["frozen"] != 1 || events["populated"] != 1 {
		return fmt.Errorf("capture network recovery requires its original frozen source: %v", err)
	}
	current, err := os.Stat(fmt.Sprintf("/proc/%d/ns/net", guard.config.PID))
	original, anchorErr := network.anchor.Stat()
	if err != nil || anchorErr != nil || !os.SameFile(current, original) {
		return fmt.Errorf("original capture network namespace changed: %v %v", err, anchorErr)
	}
	locked := map[string]bool{}
	observed := map[string][]string{}
	for _, binary := range []string{"iptables", "ip6tables"} {
		text, err := network.command(binary+"-save", "-t", "filter")
		if err == nil {
			observed[binary], err = captureNetworkRules(text)
		}
		if err == nil {
			locked[binary], err = captureNetworkLocked(network.before[binary], observed[binary])
		}
		if err != nil {
			return fmt.Errorf("capture %s recovery refused: %s %w", binary, text, err)
		}
	}
	if err := descendantGuardWrite(guard.anchor.evidence, prefix+"crash-network-recovery-intent.json",
		map[string]any{"before": network.before, "observed": observed, "owned_lock": locked}); err != nil {
		return err
	}
	for _, binary := range []string{"iptables", "ip6tables"} {
		if !locked[binary] {
			continue
		}
		for _, args := range [][]string{
			{"-D", "INPUT", "-j", "CRIU"}, {"-D", "OUTPUT", "-j", "CRIU"},
			{"-F", "CRIU"}, {"-X", "CRIU"},
		} {
			if out, err := network.command(binary, append([]string{"-w", "2"}, args...)...); err != nil {
				return fmt.Errorf("owned %s network unlock failed: %s %w", binary, out, err)
			}
		}
	}
	after := map[string][]string{}
	for _, binary := range []string{"iptables", "ip6tables"} {
		text, err := network.command(binary+"-save", "-t", "filter")
		if err == nil {
			after[binary], err = captureNetworkRules(text)
		}
		if err != nil || !slices.Equal(network.before[binary], after[binary]) {
			return fmt.Errorf("capture %s firewall differs after owned unlock: %v", binary, err)
		}
	}
	return descendantGuardWrite(guard.anchor.evidence, prefix+"crash-network-recovered.json",
		map[string]any{"before": network.before, "after": after, "owned_lock_removed": locked,
			"original_rules_preserved": true})
}
