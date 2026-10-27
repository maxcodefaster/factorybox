//go:build linux

package sysboxcapture

import (
	"fmt"
	"net"
	"os"
	"runtime"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestNetworkCopySocketInspection(t *testing.T) {
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := listener.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	accepted := make(chan *net.TCPConn, 1)
	go func() {
		connection, err := listener.AcceptTCP()
		if err == nil {
			accepted <- connection
		}
	}()
	client, err := net.DialTimeout("tcp4", listener.Addr().String(), 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	var server *net.TCPConn
	select {
	case server = <-accepted:
		defer server.Close()
	case <-time.After(3 * time.Second):
		t.Fatal("private TCP accept did not finish")
	}
	var inventory []copySocket
	for _, connection := range []interface {
		SyscallConn() (syscall.RawConn, error)
	}{listener, client.(*net.TCPConn), server} {
		raw, err := connection.SyscallConn()
		if err != nil {
			t.Fatal(err)
		}
		var socket copySocket
		var relevant bool
		var inspectErr error
		if err := raw.Control(func(fd uintptr) { socket, relevant, inspectErr = inspectCopySocket(int(fd)) }); err != nil {
			t.Fatal(err)
		}
		if inspectErr != nil || !relevant || socket.Cookie == 0 || socket.Namespace == 0 {
			t.Fatal("actual socket inspection omitted kernel identity", socket, inspectErr)
		}
		inventory = append(inventory, socket)
	}
	root := inventory[0].Namespace
	if err := admitCopySockets(inventory, map[uint64]bool{root: true}, root, "100.127.1.2/30"); err != nil {
		t.Fatal("actual listener and both kernel TCP endpoints were refused", err)
	}
}

func TestNetworkCopyThreadFileTables(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_NETWORK_COPY_UNITS") != "1" {
		t.Skip("owned Linux syscall fixture with explicit ptrace admission only")
	}
	pid := unix.Getpid()
	done := make(chan error, 1)
	check := func() error {
		tid := unix.Gettid()
		if err := sharedCopyFiles(pid, tid); err != nil {
			return fmt.Errorf("shared file table was refused: %w", err)
		}
		if err := unix.Unshare(unix.CLONE_FILES); err != nil {
			return err
		}
		// Exiting while locked discards this OS thread and its private table.
		// Do not return an unshared file table to Go's scheduling pool.
		if err := sharedCopyFiles(pid, tid); err == nil {
			return fmt.Errorf("adopted an unshared thread descriptor table")
		}
		return nil
	}
	go func() {
		runtime.LockOSThread()
		if unix.Gettid() == pid {
			// Keep the leader occupied so the probe cannot compare it with
			// itself, which would remain equal even after CLONE_FILES.
			child := make(chan error, 1)
			go func() {
				runtime.LockOSThread()
				child <- check()
			}()
			outcome := <-child
			runtime.UnlockOSThread()
			done <- outcome
			return
		}
		done <- check()
	}()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestNetworkCopySocketAdmission(t *testing.T) {
	listener := copySocket{Cookie: 1, Namespace: 11, Family: unix.AF_INET, Protocol: unix.IPPROTO_TCP,
		Local: copyEndpoint{"0.0.0.0", 8080}, State: 10}
	client := copySocket{Cookie: 2, Namespace: 11, Family: unix.AF_INET, Protocol: unix.IPPROTO_TCP,
		Local: copyEndpoint{"127.0.0.1", 42000}, Peer: &copyEndpoint{"127.0.0.1", 8080}, State: 1}
	server := copySocket{Cookie: 3, Namespace: 11, Family: unix.AF_INET, Protocol: unix.IPPROTO_TCP,
		Local: *client.Peer, Peer: &client.Local, State: 1}
	for _, test := range []struct {
		name    string
		sockets []copySocket
		allowed bool
	}{
		{"wildcard-listener", []copySocket{listener}, true},
		{"both-loopback-endpoints", []copySocket{listener, client, server}, true},
		{"external-peer-not-captured", []copySocket{client}, false},
		{"another-namespace-is-not-a-loopback-peer", []copySocket{client, func() copySocket {
			other := server
			other.Namespace = 12
			return other
		}()}, false},
		{"duplicate-ip-pairs-are-ambiguous", []copySocket{client, server, func() copySocket {
			other := server
			other.Cookie = 4
			return other
		}()}, false},
		{"original-outer-ip-bound", []copySocket{func() copySocket {
			other := listener
			other.Local.Address = "100.127.1.2"
			return other
		}()}, false},
		{"socket-from-uncaptured-namespace", []copySocket{func() copySocket {
			other := listener
			other.Namespace = 99
			return other
		}()}, false},
		{"syn-sent-is-not-an-unconnected-socket", []copySocket{func() copySocket {
			other := listener
			other.State = 2
			return other
		}()}, false},
		{"raw-ip-needs-another-contract", []copySocket{func() copySocket {
			other := listener
			other.Protocol = unix.IPPROTO_RAW
			return other
		}()}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := admitCopySockets(test.sockets, map[uint64]bool{11: true, 12: true}, 11, "100.127.1.2/30")
			if (err == nil) != test.allowed {
				t.Fatal("network-copy admission", err)
			}
		})
	}
}

func TestNetworkCopyNestedConnectionRequiresRouteProof(t *testing.T) {
	client := copySocket{Cookie: 1, Namespace: 11, Family: unix.AF_INET, Protocol: unix.IPPROTO_TCP,
		Local: copyEndpoint{"172.19.0.1", 42000}, Peer: &copyEndpoint{"172.19.0.2", 5432}, State: 1}
	server := copySocket{Cookie: 2, Namespace: 12, Family: unix.AF_INET, Protocol: unix.IPPROTO_TCP,
		Local: *client.Peer, Peer: &client.Local, State: 1}
	known := map[uint64]bool{11: true, 12: true}
	if err := admitCopySockets([]copySocket{client, server}, known, 11, "100.127.1.2/30"); err == nil {
		t.Fatal("matching captured IP/port tuples are not proof of actual routed peers")
	}
	server.Namespace = 99
	if err := admitCopySockets([]copySocket{client, server}, known, 11, "100.127.1.2/30"); err == nil {
		t.Fatal("a matching IP/port is not proof of a captured network namespace")
	}
}
