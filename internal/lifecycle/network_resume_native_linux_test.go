//go:build linux && (amd64 || arm64)

package lifecycle

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// One source pauses while another remains useful on the same worker. Neither
// external peer is captured; both must retain their original live sockets.
func TestNativeExternalTCPResume(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_NETWORK_RESUME") != "1" {
		t.Skip("owned external TCP Pause/Resume diagnostic only")
	}
	t.Setenv("FACTORYBOX_STANDBY_PEERS_NETWORK", "1")
	status := boundedLossStatusNow(t)
	standbyPeersUplink(t, status.StoreID)
	listener, err := net.Listen("tcp4", "192.0.2.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan net.Conn, 2)
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			accepted <- connection
			go io.Copy(connection, connection)
		}
	}()
	source := standbyRepeatFixture(t, status, "network-resume-source", false, true, false)
	initial := standbyRepeatRequest(t, status, source, "ready", false)
	connect := func(view View) {
		t.Helper()
		out, err := exec.Command(status.Lifecycle.Runtime, "--root", status.Lifecycle.Root+"/runtime",
			"exec", view.Record.RuntimeID, "/bin/sh", "-ec",
			`printf '%s' "$1" > /network-external`, "external", listener.Addr().String()).CombinedOutput()
		if err != nil {
			t.Fatal(string(out), err)
		}
	}
	connect(source)
	before := standbyRepeatRequest(t, status, source, "external-connected", false)
	if before["nonce"] != initial["nonce"] {
		t.Fatal("external control replaced the source")
	}
	var peer net.Conn
	select {
	case peer = <-accepted:
		defer peer.Close()
	case <-time.After(5 * time.Second):
		t.Fatal("external peer was not actually connected")
	}
	control := standbyRepeatFixture(t, status, "network-resume-control", false, true, false)
	controlInitial := standbyRepeatRequest(t, status, control, "control-ready", false)
	connect(control)
	standbyRepeatRequest(t, status, control, "control-connected", false)
	select {
	case connection := <-accepted:
		defer connection.Close()
	case <-time.After(5 * time.Second):
		t.Fatal("control external peer was not actually connected")
	}
	for _, foreignPID := range []int{os.Getpid(), control.Runtime.PID} {
		if err := source.Record.Network.SetPeerActive(foreignPID, false); err == nil {
			t.Fatal("network isolation accepted the worker or another Sandbox namespace")
		}
	}
	component := func(action string) {
		t.Helper()
		if output, err := exec.Command(status.Lifecycle.Runtime, "--root", status.Lifecycle.Root+"/runtime",
			action, control.Record.RuntimeID).CombinedOutput(); err != nil {
			t.Fatal("owned control component transition", action, string(output), err)
		}
	}
	component("pause")
	if err := control.Record.Network.SetPeerActive(control.Runtime.PID, false); err != nil {
		t.Fatal("isolate the frozen original control", err)
	}
	if err := control.Record.Network.SetPeerActive(control.Runtime.PID, true); err != nil {
		t.Fatal("reactivate the same frozen original after rollback", err)
	}
	component("resume")
	checkControl := func(request string, count float64) {
		t.Helper()
		response := standbyRepeatRequest(t, status, control, request, false)
		if response["nonce"] != controlInitial["nonce"] || response["count"] != count ||
			response["tcp_echo"] != fmt.Sprint(controlInitial["nonce"])+":"+fmt.Sprint(count) {
			t.Fatal("another Sandbox's live memory or external TCP was changed", response)
		}
	}
	checkControl("control-after-peer-reactivation", 3)
	observations := map[string]any{}
	defer func() { boundedLossSave(t, "network-resume-observation.json", observations) }()
	observe := func(phase string) {
		t.Helper()
		raw, err := peer.(*net.TCPConn).SyscallConn()
		if err != nil {
			t.Fatal(err)
		}
		var info *unix.TCPInfo
		var inspectErr error
		if err := raw.Control(func(fd uintptr) {
			info, inspectErr = unix.GetsockoptTCPInfo(int(fd), unix.IPPROTO_TCP, unix.TCP_INFO)
		}); err != nil {
			t.Fatal(err)
		}
		if inspectErr != nil {
			t.Fatal(inspectErr)
		}
		observations[phase] = map[string]any{"state": info.State, "unacked": info.Unacked, "retrans": info.Retrans}
		if info.State != 1 { // Linux TCP_ESTABLISHED, on the same original peer.
			t.Fatal("Pause/Resume closed the uncaptured external peer", phase, info.State)
		}
	}
	observe("before_pause")
	snapshot := strings.Repeat("a", 32)
	paused := boundedLossView(t, "pause", "--snapshot-id", snapshot, source.Record.ID)
	observe("after_pause")
	checkControl("control-during-pause", 4)
	if paused.Record.Pause == nil || paused.Record.Pause.After.Anon != 0 ||
		paused.Record.Pause.Snapshot.ResidentBytesAfter != 0 {
		t.Fatal("external TCP Pause did not release its memory")
	}
	source = boundedLossView(t, "resume", "--snapshot-id", snapshot, source.Record.ID)
	observe("after_resume")
	after := standbyRepeatRequest(t, status, source, "external-after-resume", false)
	if after["nonce"] != initial["nonce"] || after["count"] != float64(3) ||
		after["tcp_echo"] != fmt.Sprint(initial["nonce"])+":3" {
		t.Fatal("Resume lost RAM, internal loopback or the original external connection", after)
	}
	checkControl("control-after-resume", 5)
	boundedLossSave(t, "network-resume.json", map[string]any{
		"passed": true, "same_nonce": true, "same_external_tcp_connection": true,
		"internal_loopback": true, "paused_memory_released": true,
		"other_sandbox_preserved": true, "foreign_namespace_refused": true,
		"frozen_peer_reactivated_without_restart": true,
	})
}
