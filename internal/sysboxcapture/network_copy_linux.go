//go:build linux

package sysboxcapture

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

const NetworkCopyImage = "network-copy.json"

// NetworkCopyEvidence is made while the original complete task set is frozen.
// It becomes part of the checkpoint's hashed image inventory, not a mutable
// annotation supplied by a consumer. It grants no cross-worker recovery.
type NetworkCopyEvidence struct {
	Version     int    `json:"version"`
	SourcePID   int    `json:"source_pid"`
	SourceStart string `json:"source_start"`
	Address     string `json:"address"`
	Sockets     int    `json:"sockets"`
	Namespaces  int    `json:"namespaces"`
	Allowed     bool   `json:"allowed"`
	Reason      string `json:"reason,omitempty"`
}

type copyEndpoint struct {
	Address string
	Port    int
}

type copySocket struct {
	Cookie    uint64
	Namespace uint64
	Family    int
	Protocol  int
	Local     copyEndpoint
	Peer      *copyEndpoint
	State     uint8
}

// A refusal only excludes independent copies. Same-identity Pause/Resume must
// still preserve external connections and must not silently close a socket.
func (n *CaptureNetwork) CopyEvidence(threads map[int]string, address string) []byte {
	proof := NetworkCopyEvidence{Version: 1, SourcePID: n.state.pid, SourceStart: n.state.start, Address: address}
	sockets, namespaces, root, err := n.copyInventory(threads)
	proof.Sockets, proof.Namespaces = len(sockets), len(namespaces)
	if err == nil {
		err = admitCopySockets(sockets, namespaces, root, address)
	}
	proof.Allowed = err == nil
	if err != nil {
		proof.Reason = err.Error()
	}
	data, _ := json.Marshal(proof)
	return data
}

// Use a disposable locked thread. Never return an OS thread to the Go runtime
// in another namespace, including when restoring its original namespace fails.
func networkCookie(namespace *os.File) (uint64, error) {
	type result struct {
		cookie uint64
		err    error
	}
	done := make(chan result, 1)
	go func() {
		runtime.LockOSThread()
		original, err := os.Open("/proc/thread-self/ns/net")
		if err != nil {
			runtime.UnlockOSThread()
			done <- result{err: err}
			return
		}
		defer original.Close()
		if err := unix.Setns(int(namespace.Fd()), unix.CLONE_NEWNET); err != nil {
			runtime.UnlockOSThread()
			done <- result{err: err}
			return
		}
		fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, 0)
		var cookie uint64
		if err == nil {
			cookie, err = unix.GetsockoptUint64(fd, unix.SOL_SOCKET, unix.SO_NETNS_COOKIE)
			err = errors.Join(err, unix.Close(fd))
		}
		back := unix.Setns(int(original.Fd()), unix.CLONE_NEWNET)
		if back == nil {
			runtime.UnlockOSThread()
		}
		done <- result{cookie, errors.Join(err, back)}
	}()
	observed := <-done
	return observed.cookie, observed.err
}

func (n *CaptureNetwork) copyInventory(threads map[int]string) ([]copySocket, map[uint64]bool, uint64, error) {
	if err := n.validate(); err != nil {
		return nil, nil, 0, err
	}
	root, err := networkCookie(n.state.namespace)
	if err != nil {
		return nil, nil, 0, err
	}
	namespaces := map[uint64]bool{}
	seen := map[[2]uint64]bool{}
	for tid, start := range threads {
		birth, err := networkProcessStart(tid)
		if err != nil || birth != start {
			return nil, nil, 0, fmt.Errorf("network-copy task birth changed")
		}
		status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", tid))
		if err != nil {
			return nil, nil, 0, err
		}
		pid := 0
		for _, line := range strings.Split(string(status), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[0] == "Tgid:" {
				pid, _ = strconv.Atoi(fields[1])
			}
		}
		if _, known := n.state.processes[pid]; !known || sharedCopyFiles(pid, tid) != nil {
			return nil, nil, 0, fmt.Errorf("network-copy thread has an unproved independent descriptor table")
		}
		namespace, err := os.Open(fmt.Sprintf("/proc/%d/ns/net", tid))
		if err != nil {
			return nil, nil, 0, err
		}
		var stat unix.Stat_t
		err = unix.Fstat(int(namespace.Fd()), &stat)
		key := [2]uint64{uint64(stat.Dev), stat.Ino}
		if err == nil && !seen[key] {
			seen[key] = true
			var cookie uint64
			cookie, err = networkCookie(namespace)
			if err == nil && cookie != 0 {
				namespaces[cookie] = true
			}
		}
		err = errors.Join(err, namespace.Close())
		if err != nil {
			return nil, nil, 0, err
		}
	}
	var sockets []copySocket
	cookies := map[uint64]bool{}
	for _, process := range n.state.processes {
		birth, err := networkProcessStart(process.PID)
		if err != nil || birth != process.Start {
			return nil, nil, 0, fmt.Errorf("network-copy process birth changed")
		}
		directory, err := os.Open(fmt.Sprintf("/proc/%d/fd", process.PID))
		if err != nil {
			return nil, nil, 0, err
		}
		entries, readErr := directory.ReadDir(65537)
		closeErr := directory.Close()
		if readErr != nil && !errors.Is(readErr, io.EOF) || closeErr != nil || len(entries) > 65536 {
			return nil, nil, 0, fmt.Errorf("network-copy descriptor inventory unavailable")
		}
		for _, entry := range entries {
			target, err := os.Readlink(fmt.Sprintf("/proc/%d/fd/%s", process.PID, entry.Name()))
			if err != nil {
				return nil, nil, 0, err
			}
			if len(target) < 8 || target[:8] != "socket:[" {
				continue
			}
			number, err := strconv.Atoi(entry.Name())
			if err != nil {
				return nil, nil, 0, err
			}
			fd, err := unix.PidfdGetfd(int(process.file.Fd()), number, 0)
			if err != nil {
				return nil, nil, 0, err
			}
			socket, relevant, inspectErr := inspectCopySocket(fd)
			err = errors.Join(inspectErr, unix.Close(fd))
			if err != nil {
				return nil, nil, 0, err
			}
			if relevant && !cookies[socket.Cookie] {
				if len(sockets) >= 16384 {
					return nil, nil, 0, fmt.Errorf("network-copy socket inventory exceeds bound")
				}
				cookies[socket.Cookie] = true
				sockets = append(sockets, socket)
			}
		}
	}
	return sockets, namespaces, root, nil
}

// The process pidfd inventory is complete only if every captured thread
// shares its leader's file table. Equal fd numbers or /proc symlinks do not
// establish that after a thread has unshared CLONE_FILES.
func sharedCopyFiles(pid, tid int) error {
	const kcmpFiles = 2
	result, _, errno := unix.Syscall6(unix.SYS_KCMP, uintptr(pid), uintptr(tid), kcmpFiles, 0, 0, 0)
	if errno != 0 {
		return errno
	}
	if result != 0 {
		return fmt.Errorf("thread has another file table")
	}
	return nil
}

func copySocketEndpoint(value unix.Sockaddr) (copyEndpoint, error) {
	switch endpoint := value.(type) {
	case *unix.SockaddrInet4:
		return copyEndpoint{net.IP(endpoint.Addr[:]).String(), endpoint.Port}, nil
	case *unix.SockaddrInet6:
		if endpoint.ZoneId != 0 {
			return copyEndpoint{}, fmt.Errorf("network-copy scoped IPv6 socket needs a separate contract")
		}
		return copyEndpoint{net.IP(endpoint.Addr[:]).String(), endpoint.Port}, nil
	default:
		return copyEndpoint{}, fmt.Errorf("network-copy socket has another address family")
	}
}

func inspectCopySocket(fd int) (copySocket, bool, error) {
	var socket copySocket
	var err error
	socket.Family, err = unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_DOMAIN)
	if err != nil {
		return socket, false, err
	}
	if socket.Family == unix.AF_UNIX || socket.Family == unix.AF_NETLINK {
		return socket, false, nil
	}
	if socket.Family != unix.AF_INET && socket.Family != unix.AF_INET6 {
		return socket, false, fmt.Errorf("network-copy socket family needs a separate contract")
	}
	socket.Protocol, err = unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_PROTOCOL)
	if err != nil {
		return socket, false, err
	}
	kind, err := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_TYPE)
	if err != nil {
		return socket, false, err
	}
	if socket.Protocol == unix.IPPROTO_TCP && kind != unix.SOCK_STREAM ||
		socket.Protocol == unix.IPPROTO_UDP && kind != unix.SOCK_DGRAM {
		return socket, false, fmt.Errorf("network-copy raw socket needs a separate contract")
	}
	socket.Cookie, err = unix.GetsockoptUint64(fd, unix.SOL_SOCKET, unix.SO_COOKIE)
	if err != nil {
		return socket, false, err
	}
	socket.Namespace, err = unix.GetsockoptUint64(fd, unix.SOL_SOCKET, unix.SO_NETNS_COOKIE)
	if err != nil {
		return socket, false, err
	}
	local, err := unix.Getsockname(fd)
	if err != nil {
		return socket, false, err
	}
	socket.Local, err = copySocketEndpoint(local)
	if err != nil {
		return socket, false, err
	}
	peer, err := unix.Getpeername(fd)
	if err == nil {
		endpoint, err := copySocketEndpoint(peer)
		if err != nil {
			return socket, false, err
		}
		socket.Peer = &endpoint
	} else if !errors.Is(err, unix.ENOTCONN) {
		return socket, false, err
	}
	if socket.Protocol == unix.IPPROTO_TCP {
		info, err := unix.GetsockoptTCPInfo(fd, unix.IPPROTO_TCP, unix.TCP_INFO)
		if err != nil {
			return socket, false, err
		}
		socket.State = info.State
	}
	return socket, true, nil
}

func admitCopySockets(sockets []copySocket, namespaces map[uint64]bool, root uint64, address string) error {
	outer, _, err := net.ParseCIDR(address)
	if err != nil || root == 0 || !namespaces[root] {
		return fmt.Errorf("network-copy original namespace or IPv4 identity is unproved")
	}
	type connection struct {
		namespace uint64
		family    int
		protocol  int
		local     copyEndpoint
		peer      copyEndpoint
	}
	connections := map[connection]int{}
	for _, socket := range sockets {
		if socket.Peer != nil {
			connections[connection{socket.Namespace, socket.Family, socket.Protocol, socket.Local, *socket.Peer}]++
		}
	}
	for _, socket := range sockets {
		ip := net.ParseIP(socket.Local.Address)
		if socket.Cookie == 0 || socket.Namespace == 0 || !namespaces[socket.Namespace] || ip == nil {
			return fmt.Errorf("network-copy socket belongs to an uncaptured namespace")
		}
		if socket.Protocol != unix.IPPROTO_TCP && socket.Protocol != unix.IPPROTO_UDP {
			return fmt.Errorf("network-copy raw socket needs a separate contract")
		}
		if ip.Equal(outer) || socket.Namespace == root && ip.To4() == nil && !ip.IsUnspecified() && !ip.IsLoopback() {
			return fmt.Errorf("network-copy socket is bound to the original outer address")
		}
		if socket.Peer == nil {
			// Linux TCP_CLOSE and TCP_LISTEN are the only unconnected states
			// that do not retain an in-flight connection to an external peer.
			if socket.Protocol == unix.IPPROTO_TCP && socket.State != 7 && socket.State != 10 {
				return fmt.Errorf("network-copy TCP connection is not quiescent")
			}
			continue
		}
		if socket.Protocol == unix.IPPROTO_TCP && socket.State != 1 {
			return fmt.Errorf("network-copy TCP peer is not fully established")
		}
		peerIP := net.ParseIP(socket.Peer.Address)
		if peerIP == nil || peerIP.Equal(outer) {
			return fmt.Errorf("network-copy peer uses the original outer address")
		}
		// Matching IPs/ports in captured tasks do not prove a routed peer:
		// overlapping nested networks can have identical tuples. Only
		// loopback within the exact same captured namespace is admitted.
		if !ip.IsLoopback() || !peerIP.IsLoopback() {
			return fmt.Errorf("network-copy routed connection needs a separate peer-ownership proof")
		}
		reverse := connection{socket.Namespace, socket.Family, socket.Protocol, *socket.Peer, socket.Local}
		matches := connections[reverse]
		if socket.Local == *socket.Peer {
			matches-- // A self-connected socket is not two captured endpoints.
		}
		if matches != 1 {
			return fmt.Errorf("network-copy connection lacks both unique captured endpoints")
		}
	}
	return nil
}
