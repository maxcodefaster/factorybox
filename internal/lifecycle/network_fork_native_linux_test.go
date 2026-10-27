//go:build linux && (amd64 || arm64)

package lifecycle

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"factorybox/internal/sandboxnetwork"
)

// An independent Staging-derived process copy must have its own outer network.
// The original and child may run together; identical worker routes are unsafe.
func TestNativeNetworkFork(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_NETWORK_FORK") != "1" {
		t.Skip("owned network-copy lifecycle fixture only")
	}
	t.Setenv("FACTORYBOX_STANDBY_PEERS_NETWORK", "1")
	status := boundedLossStatusNow(t)
	standbyPeersUplink(t, status.StoreID)
	listener, err := net.Listen("tcp4", "192.0.2.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Connection", "close")
		fmt.Fprintln(w, r.RemoteAddr)
	})}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	source := standbyRepeatFixture(t, status, "network-source", false, true, false)
	initial := standbyRepeatRequest(t, status, source, "ready", false)
	checkEcho := func(response map[string]any) {
		t.Helper()
		if response["tcp_echo"] != fmt.Sprint(response["nonce"])+":"+fmt.Sprint(response["count"]) {
			t.Fatal("snapshot-internal TCP connection was not preserved", response)
		}
	}
	checkEcho(initial)
	egress := func(view View) string {
		t.Helper()
		if view.Record.Network == nil {
			t.Fatal("missing independently owned network")
		}
		out, err := exec.Command(status.Lifecycle.Runtime, "--root", status.Lifecycle.Root+"/runtime",
			"exec", view.Record.RuntimeID, "/bin/wget", "-qO-", "http://"+listener.Addr().String()).CombinedOutput()
		if err != nil {
			t.Fatal("useful outbound traffic", string(out), err)
		}
		host, _, err := net.SplitHostPort(strings.TrimSpace(string(out)))
		if err != nil || host != strings.Split(view.Record.Network.Address, "/")[0] {
			t.Fatal("outbound traffic has another Sandbox's address", string(out), view.Record.Network, err)
		}
		return host
	}
	originalIP := egress(source)
	inbound := func(view View) {
		t.Helper()
		port, err := os.ReadFile(view.Record.Rootfs + "/network-port")
		if err != nil {
			t.Fatal(err)
		}
		connection, err := net.DialTimeout("tcp4",
			net.JoinHostPort(strings.Split(view.Record.Network.Address, "/")[0], string(port)), 3*time.Second)
		if err != nil {
			t.Fatal("restored wildcard listener is unreachable on its own address", err)
		}
		defer connection.Close()
		if err := connection.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
			t.Fatal(err)
		}
		if _, err := fmt.Fprintln(connection, initial["nonce"]); err != nil {
			t.Fatal(err)
		}
		if err := connection.(*net.TCPConn).CloseWrite(); err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(connection)
		if err != nil || string(data) != fmt.Sprintln(initial["nonce"]) {
			t.Fatal("restored wildcard listener lost useful inbound traffic", string(data), err)
		}
	}
	inbound(source)
	snapshot := strings.Repeat("a", 32)
	paused := boundedLossView(t, "pause", "--snapshot-id", snapshot, source.Record.ID)
	if paused.Record.Pause == nil || paused.Record.Pause.After.Anon != 0 ||
		paused.Record.Pause.Snapshot.ResidentBytesAfter != 0 {
		t.Fatal("network source Pause retained anonymous RAM or snapshot pages")
	}
	engine, err := Open(status.Lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	collidingOwner, _ := hex.DecodeString(source.Record.Owner)
	collidingOwner[2] ^= 128 // Keep the derived /30, change both interface names.
	collision, err := sandboxnetwork.Plan(hex.EncodeToString(collidingOwner))
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.admitNetwork(*collision, "network-collision"); err == nil ||
		!strings.Contains(err.Error(), "reserved by another Sandbox") {
		t.Fatal("paused source subnet was not reserved without a live route", err)
	}
	child := boundedLossView(t, "fork", "--source", source.Record.ID, "--snapshot-id", snapshot, "network-child")
	t.Cleanup(func() { boundedLossAction(t, "delete", "network-child") })
	if child.Record.Network == nil || child.Record.Network.Owner != child.Record.Owner ||
		child.Record.Network.CIDR == source.Record.Network.CIDR || child.Record.Network.Peer == source.Record.Network.Peer {
		t.Fatal("fork did not receive an independent outer network", source.Record.Network, child.Record.Network)
	}
	replayed := boundedLossView(t, "fork", "--source", source.Record.ID, "--snapshot-id", snapshot, child.Record.ID)
	if replayed.Record.Owner != child.Record.Owner || replayed.Runtime == nil || replayed.Runtime.PID != child.Runtime.PID {
		t.Fatal("exact fork replay replaced its original restored target", replayed)
	}
	childBefore := standbyRepeatRequest(t, status, child, "child-before", false)
	checkEcho(childBefore)
	if childBefore["nonce"] != initial["nonce"] || childBefore["count"] != float64(2) {
		t.Fatal("fork replaced the prepared process instead of copying its RAM", initial, childBefore)
	}
	childIP := egress(child)
	inbound(child)
	source = boundedLossView(t, "resume", "--snapshot-id", snapshot, source.Record.ID)
	sourceAfter := standbyRepeatRequest(t, status, source, "source-after", false)
	checkEcho(sourceAfter)
	if sourceAfter["nonce"] != initial["nonce"] || sourceAfter["count"] != float64(2) || egress(source) != originalIP || childIP == originalIP {
		t.Fatal("child changed original RAM or networking", sourceAfter)
	}
	inbound(source)
	childPause := strings.Repeat("b", 32)
	childPaused := boundedLossView(t, "pause", "--snapshot-id", childPause, child.Record.ID)
	if childPaused.Record.Pause.After.Anon != 0 || childPaused.Record.Pause.Snapshot.ResidentBytesAfter != 0 {
		t.Fatal("paused copy retained anonymous RAM or resident snapshot pages")
	}
	egress(source)
	child = boundedLossView(t, "resume", "--snapshot-id", childPause, child.Record.ID)
	after := standbyRepeatRequest(t, status, child, "child-after", false)
	checkEcho(after)
	if after["nonce"] != initial["nonce"] || after["count"] != float64(3) || egress(child) != childIP {
		t.Fatal("child Pause/Resume lost its independent memory or network", after)
	}
	inbound(child)
	boundedLossAction(t, "delete", child.Record.ID)
	egress(source)
	external, err := net.Listen("tcp4", "192.0.2.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer external.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		connection, err := external.Accept()
		if err == nil {
			accepted <- connection
			io.Copy(connection, connection)
		}
	}()
	out, err := exec.Command(status.Lifecycle.Runtime, "--root", status.Lifecycle.Root+"/runtime",
		"exec", source.Record.RuntimeID, "/bin/sh", "-ec",
		`printf '%s' "$1" > /network-external`, "external", external.Addr().String()).CombinedOutput()
	if err != nil {
		t.Fatal(string(out), err)
	}
	checkEcho(standbyRepeatRequest(t, status, source, "external-connected", false))
	var connection net.Conn
	select {
	case connection = <-accepted:
		defer connection.Close()
	case <-time.After(5 * time.Second):
		t.Fatal("external control was not actually connected")
	}
	unsafeSnapshot := strings.Repeat("c", 32)
	boundedLossView(t, "pause", "--snapshot-id", unsafeSnapshot, source.Record.ID)
	out, err = boundedLossCommand("--worker-run-root", "/run/factorybox", "fork",
		"--source", source.Record.ID, "--snapshot-id", unsafeSnapshot, "network-unsafe")
	if err == nil || !strings.Contains(err.Error()+string(out), "network fork socket admission refused") {
		t.Fatal("external connection was duplicated or refused for another reason", string(out), err)
	}
	for _, path := range []string{"forks/network-unsafe", "objects/network-unsafe"} {
		if _, err := os.Lstat(status.Lifecycle.Root + "/" + path); !os.IsNotExist(err) {
			t.Fatal("unsafe fork created an intent or target before socket admission", path, err)
		}
	}
	source = boundedLossView(t, "resume", "--snapshot-id", unsafeSnapshot, source.Record.ID)
	checkEcho(standbyRepeatRequest(t, status, source, "external-after-resume", false))
	egress(source)
	boundedLossSave(t, "network-fork.json", map[string]any{
		"passed": true, "independent_addresses": true, "copied_original_ram": true,
		"source_and_child_run_together": true, "child_pause_resume": true,
		"source_survives_child_delete": true, "source_ip": originalIP, "child_ip": childIP,
		"snapshot_internal_tcp_preserved": true, "external_connection_fork_refused_before_intent": true,
		"wildcard_listener_reachable_on_independent_addresses": true,
		"same_identity_external_resume":                        true,
		"paused_subnet_reserved":                               true, "exact_fork_replay": true, "child_pause_releases_memory": true,
	})
}

// Keep both ends of a private loopback connection in the captured task set.
// The optional external control must survive Resume but must forbid a copy.
func networkForkActorConnections(t *testing.T) func(string) string {
	t.Helper()
	listener, err := net.Listen("tcp4", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("/network-port", []byte(port), 0644); err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer connection.Close()
				io.Copy(connection, connection)
			}()
		}
	}()
	client, err := net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", port), 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(client)
	var external net.Conn
	var externalReader *bufio.Reader
	return func(message string) string {
		t.Helper()
		request := func(connection net.Conn, reader *bufio.Reader) string {
			t.Helper()
			if err := connection.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
				t.Fatal(err)
			}
			if _, err := fmt.Fprintln(connection, message); err != nil {
				t.Fatal(err)
			}
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			if err := connection.SetDeadline(time.Time{}); err != nil {
				t.Fatal(err)
			}
			return strings.TrimSuffix(line, "\n")
		}
		if address, err := os.ReadFile("/network-external"); err == nil {
			if external == nil {
				external, err = net.DialTimeout("tcp4", string(address), 3*time.Second)
				if err != nil {
					t.Fatal(err)
				}
				externalReader = bufio.NewReader(external)
			}
			if request(external, externalReader) != message {
				t.Fatal("original external connection lost its echo")
			}
		}
		return request(client, reader)
	}
}
