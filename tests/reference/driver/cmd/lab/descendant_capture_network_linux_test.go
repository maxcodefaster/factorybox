package main

import (
	"slices"
	"strings"
	"testing"
)

func TestCaptureNetworkOwnership(t *testing.T) {
	before := []string{"*filter", ":INPUT ACCEPT", ":OUTPUT ACCEPT", ":CUSTOM -",
		"-A INPUT -j CUSTOM", "-A CUSTOM -p tcp --dport 9443 -j ACCEPT", "COMMIT"}
	locked := []string{"*filter", ":INPUT ACCEPT", ":OUTPUT ACCEPT", ":CUSTOM -", ":CRIU -",
		"-A INPUT -j CRIU", "-A INPUT -j CUSTOM", "-A OUTPUT -j CRIU",
		"-A CUSTOM -p tcp --dport 9443 -j ACCEPT",
		"-A CRIU -m mark --mark 0xc114 -j ACCEPT", "-A CRIU -j DROP", "COMMIT"}
	if owned, err := captureNetworkLocked(before, locked); err != nil || !owned {
		t.Fatalf("exact owned additions refused: %v", err)
	}
	if owned, err := captureNetworkLocked(before, before); err != nil || owned {
		t.Fatalf("unchanged original firewall should need no cleanup: %v", err)
	}
	for name, mutation := range map[string][]string{
		"foreign_custom_rule": append(slices.Clone(locked[:len(locked)-1]), "-A CUSTOM -j DROP", "COMMIT"),
		"changed_custom_port": strings.Split(strings.ReplaceAll(strings.Join(locked, "\n"), "9443", "9444"), "\n"),
		"foreign_criu_rule":   append(slices.Clone(locked[:len(locked)-1]), "-A CRIU -p tcp -j ACCEPT", "COMMIT"),
		"duplicate_jump":      append(slices.Clone(locked[:len(locked)-1]), "-A INPUT -j CRIU", "COMMIT"),
		"missing_output":      slices.Delete(slices.Clone(locked), 7, 8),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := captureNetworkLocked(before, mutation); err == nil {
				t.Fatal("uncertain ownership was accepted")
			}
		})
	}
	if _, err := captureNetworkLocked(locked, locked); err == nil {
		t.Fatal("preexisting CRIU rules were accepted as owned")
	}
}

func TestCaptureNetworkCounters(t *testing.T) {
	a, err := captureNetworkRules("# generated\n*filter\n:INPUT ACCEPT [13:927]\nCOMMIT\n")
	if err != nil {
		t.Fatal(err)
	}
	b, err := captureNetworkRules("# later\n*filter\n:INPUT ACCEPT [19:1766]\nCOMMIT\n")
	if err != nil || !slices.Equal(a, b) {
		t.Fatalf("ordinary counters changed the firewall policy: %v", err)
	}
	for _, text := range []string{"", "*filter\n:INPUT ACCEPT [0:0]", "*nat\nCOMMIT\n"} {
		if _, err := captureNetworkRules(text); err == nil {
			t.Fatalf("incomplete or different table accepted: %q", text)
		}
	}
}
