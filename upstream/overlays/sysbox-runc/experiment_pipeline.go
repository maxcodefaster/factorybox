//go:build linux

package main

import (
	"fmt"
	"net"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// Keep the original FD 3 for both ordered handshakes. Each call owns and
// closes only its duplicate. The sealed restore-child FD is created later.
func experimentCopyPipelineConnection(timeout time.Duration) (*net.UnixConn, error) {
	if os.Getenv("RUNTIME_FORK_COPY_PIPELINE") != "1" {
		return nil, nil
	}
	if os.Getenv("CRIU_SYSBOX_COPY_PIPELINE") != "1" ||
		os.Getenv("CRIU_SYSBOX_RETAIN_SOURCE") != "1" {
		return nil, fmt.Errorf("copy pipeline is outside the owned retained source")
	}
	kind, err := unix.GetsockoptInt(3, unix.SOL_SOCKET, unix.SO_TYPE)
	if err != nil || kind != unix.SOCK_SEQPACKET {
		return nil, fmt.Errorf("copy pipeline requires a seqpacket descriptor: %v", err)
	}
	peer, err := unix.GetsockoptUcred(3, unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil || int(peer.Pid) != os.Getppid() || peer.Uid != uint32(os.Geteuid()) {
		return nil, fmt.Errorf("copy pipeline controller identity mismatch: %v", err)
	}
	unix.CloseOnExec(3)
	fd, err := unix.Dup(3)
	if err != nil {
		return nil, err
	}
	unix.CloseOnExec(fd)
	file := os.NewFile(uintptr(fd), "owned-copy-pipeline")
	connection, err := net.FileConn(file)
	file.Close()
	if err != nil {
		return nil, err
	}
	if err := connection.SetDeadline(time.Now().Add(timeout)); err != nil {
		connection.Close()
		return nil, err
	}
	return connection.(*net.UnixConn), nil
}

// Sysbox Fs verifies the real cgroup freezer here. CRIU later transfers that
// freeze to ptrace and temporarily clears cgroup.freeze for its parasites.
func experimentCopyPipelineRegistered(timeout time.Duration) error {
	connection, err := experimentCopyPipelineConnection(timeout)
	if err != nil || connection == nil {
		return err
	}
	defer connection.Close()
	if count, err := connection.Write([]byte("F")); err != nil || count != 1 {
		return fmt.Errorf("copy pipeline registration handshake failed: %v", err)
	}
	return nil
}

// No guest-visible file can release this barrier. The original descriptor is
// closed before the child gets a different, sealed configuration FD 3.
func experimentCopyPipelineBarrier(timeout time.Duration) error {
	connection, err := experimentCopyPipelineConnection(timeout)
	if err != nil || connection == nil {
		return err
	}
	defer unix.Close(3)
	defer connection.Close()
	if count, err := connection.Write([]byte("P")); err != nil || count != 1 {
		return fmt.Errorf("copy pipeline readiness failed: %v", err)
	}
	var message [8]byte
	count, _, flags, _, err := connection.ReadMsgUnix(message[:], nil)
	if err != nil || count != 1 || flags&unix.MSG_TRUNC != 0 || message[0] != 'R' {
		return fmt.Errorf("copy pipeline cancelled before restore: %v", err)
	}
	return nil
}
