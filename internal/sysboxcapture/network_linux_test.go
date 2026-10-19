//go:build linux

package sysboxcapture

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func TestNetworkRollbackAdmission(t *testing.T) {
	before := "*filter\n:INPUT ACCEPT [1:2]\n:FORWARD ACCEPT [0:0]\n:OUTPUT ACCEPT [0:0]\n" +
		"-A INPUT -s 192.0.2.1/32 -j ACCEPT\nCOMMIT\n"
	after := "*filter\n:INPUT ACCEPT [3:4]\n:FORWARD ACCEPT [0:0]\n:OUTPUT ACCEPT [0:0]\n:CRIU - [0:0]\n" +
		"-A INPUT -j CRIU\n-A INPUT -s 192.0.2.1/32 -j ACCEPT\n-A OUTPUT -j CRIU\n" +
		"-A CRIU -m mark --mark 0xc114 -j ACCEPT\n-A CRIU -j DROP\nCOMMIT\n"
	for _, test := range []struct {
		name    string
		before  string
		current string
		allowed bool
	}{
		{"exact-owned-delta", before, after, true},
		{"foreign-chain-already-present", after, after, false},
		{"changed-original-policy", before, strings.Replace(after, ":INPUT ACCEPT", ":INPUT DROP", 1), false},
		{"changed-original-rule", before, strings.Replace(after, "192.0.2.1", "192.0.2.2", 1), false},
		{"changed-criu-rule", before, strings.Replace(after, "0xc114", "0xc115", 1), false},
		{"duplicate-jump", before, strings.Replace(after, "-A OUTPUT -j CRIU", "-A OUTPUT -j CRIU\n-A OUTPUT -j CRIU", 1), false},
		{"missing-jump", before, strings.Replace(after, "-A OUTPUT -j CRIU\n", "", 1), false},
		{"foreign-comment-is-not-a-counter", strings.Replace(before, "-j ACCEPT", `-m comment --comment "[1:2]" -j ACCEPT`, 1),
			strings.Replace(after, "-s 192.0.2.1/32 -j ACCEPT", `-s 192.0.2.1/32 -m comment --comment "[3:4]" -j ACCEPT`, 1), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if actual := knownCRIUNetworkDelta([]byte(test.before), []byte(test.current)); actual != test.allowed {
				t.Fatal("network undo admission disagrees with its exact ownership proof", actual)
			}
		})
	}
	if os.Getenv("FACTORYBOX_NATIVE_RETAINED_CAPTURE") == "1" {
		t.Run("private-kernel-namespaces", testPrivateNetworkRollback)
	}
}

func testPrivateNetworkRollback(t *testing.T) {
	lock := []byte("*filter\n:CRIU - [0:0]\n-I INPUT -j CRIU\n-I OUTPUT -j CRIU\n" +
		"-A CRIU -m mark --mark 0xc114 -j ACCEPT\n-A CRIU -j DROP\nCOMMIT\n")
	for _, mode := range []string{"owned-v4-v6", "foreign-v4-change", "foreign-v6-before-v4-undo", "preexisting-name"} {
		t.Run(mode, func(t *testing.T) {
			process := exec.Command("/bin/sleep", "30")
			process.SysProcAttr = &syscall.SysProcAttr{Cloneflags: unix.CLONE_NEWNET}
			if err := process.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { process.Process.Kill(); process.Wait() }()
			start, err := networkProcessStart(process.Process.Pid)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "preexisting-name" {
				namespace, err := os.Open("/proc/" + strconv.Itoa(process.Process.Pid) + "/ns/net")
				if err != nil {
					t.Fatal(err)
				}
				probe := &CaptureNetwork{state: captureNetwork{namespace: namespace}}
				defer probe.Close()
				if _, err := probe.command("iptables-restore", lock, "--noflush"); err != nil {
					t.Fatal(err)
				}
				before, err := probe.command("iptables-save", nil, "-t", "filter")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := BeginCaptureNetwork(process.Process.Pid, start, t.TempDir()); err == nil {
					t.Fatal("adopted a pre-existing chain")
				}
				after, err := probe.command("iptables-save", nil, "-t", "filter")
				if err != nil || strings.Join(networkLines(before), "\n") != strings.Join(networkLines(after), "\n") {
					t.Fatal("admission changed the pre-existing chain", err)
				}
				return
			}
			guard, err := BeginCaptureNetwork(process.Process.Pid, start, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer guard.Close()
			for _, program := range []string{"iptables", "ip6tables"} {
				if _, err := guard.command(program+"-restore", lock, "--noflush"); err != nil {
					t.Fatal(err)
				}
			}
			if mode != "owned-v4-v6" {
				program, address := "iptables", "192.0.2.99"
				if mode == "foreign-v6-before-v4-undo" {
					program, address = "ip6tables", "2001:db8::99"
				}
				if _, err := guard.command(program, nil, "-A", "FORWARD", "-s", address, "-j", "DROP"); err != nil {
					t.Fatal(err)
				}
				before := map[string][]byte{}
				for _, family := range []string{"iptables", "ip6tables"} {
					before[family], err = guard.command(family+"-save", nil, "-t", "filter")
					if err != nil {
						t.Fatal(err)
					}
				}
				if err := guard.Rollback(); err == nil {
					t.Fatal("network undo accepted a foreign rule")
				}
				for _, family := range []string{"iptables", "ip6tables"} {
					after, err := guard.command(family+"-save", nil, "-t", "filter")
					if err != nil || strings.Join(networkLines(before[family]), "\n") != strings.Join(networkLines(after), "\n") {
						t.Fatal("network undo changed a family before complete validation", family, err)
					}
				}
				return
			}
			if err := guard.Rollback(); err != nil {
				t.Fatal("owned dual-stack rollback", err)
			}
			if err := guard.Rollback(); err != nil {
				t.Fatal("completed rollback is not idempotent", err)
			}
		})
	}
}
