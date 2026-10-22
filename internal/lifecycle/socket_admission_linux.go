//go:build linux

package lifecycle

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// Inspect the exact frozen task's file table. PIDFD_THREAD is O_EXCL in Linux's
// pidfd UAPI; a group-leader handle could name another unshared file table.
func captureSocketBlocker(tid, descriptor int) (string, error) {
	process, err := unix.PidfdOpen(tid, unix.O_EXCL)
	if err != nil {
		return "", err
	}
	defer unix.Close(process)
	fd, err := unix.PidfdGetfd(process, descriptor, 0)
	if err != nil {
		return "", err
	}
	defer unix.Close(fd)
	domain, err := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_DOMAIN)
	if err != nil || (domain != unix.AF_INET && domain != unix.AF_INET6) {
		return "", err
	}
	protocol, err := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_PROTOCOL)
	if err != nil {
		return "", err
	}
	if protocol == 262 { // IPPROTO_MPTCP; explicitly unsupported by the pinned CRIU.
		return "mptcp", nil
	}
	if protocol != unix.IPPROTO_TCP {
		return "", nil
	}
	info, err := unix.GetsockoptTCPInfo(fd, unix.IPPROTO_TCP, unix.TCP_INFO)
	if err != nil {
		return "", err
	}
	// Linux tcp_get_info aliases tcpi_unacked to sk_ack_backlog for listeners.
	// Those children have not been accepted; CRIU refuses them rather than
	// silently dropping pending connections. Let source code accept them during
	// the existing bounded admission window, before entering process capture.
	if info.State == unix.BPF_TCP_LISTEN && info.Unacked != 0 {
		return "tcp-accept-queue", nil
	}
	switch info.State {
	case unix.BPF_TCP_LISTEN, unix.BPF_TCP_ESTABLISHED, unix.BPF_TCP_FIN_WAIT2,
		unix.BPF_TCP_FIN_WAIT1, unix.BPF_TCP_CLOSE_WAIT, unix.BPF_TCP_LAST_ACK,
		unix.BPF_TCP_CLOSING, unix.BPF_TCP_SYN_SENT, unix.BPF_TCP_CLOSE:
		return "", nil
	default:
		return fmt.Sprintf("tcp-state-%d", info.State), nil
	}
}
