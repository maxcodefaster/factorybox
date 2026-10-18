package main

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const dirtyBrokerSocket = "/experiment/dirty-tracking.sock"

type dirtyIdentity struct {
	pid   int
	ticks uint64
}

type dirtyDescriptor struct {
	uffd  int
	pidfd int
}

func dirtyProcessIdentity(pid int) (dirtyIdentity, error) {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return dirtyIdentity{}, err
	}
	end := strings.LastIndexByte(string(b), ')')
	if end < 0 {
		return dirtyIdentity{}, fmt.Errorf("invalid owned process stat")
	}
	fields := strings.Fields(string(b[end+1:]))
	if len(fields) < 20 {
		return dirtyIdentity{}, fmt.Errorf("short owned process stat")
	}
	ticks, err := strconv.ParseUint(fields[19], 10, 64)
	return dirtyIdentity{pid: pid, ticks: ticks}, err
}

func dirtyOwnedProcess(key dirtyIdentity) error {
	actual, err := dirtyProcessIdentity(key.pid)
	if err != nil || actual != key || key.pid <= 1 || key.ticks == 0 {
		return fmt.Errorf("owned process identity changed: %v", err)
	}
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", key.pid))
	if err != nil {
		return err
	}
	owned := false
	for _, line := range strings.Split(string(b), "\n") {
		const boundary = "0::/criu-probe-sysbox"
		if line == boundary || strings.HasPrefix(line, boundary+"/") {
			owned = true
		}
	}
	if !owned {
		return fmt.Errorf("process is outside the owned source cgroup")
	}
	b, err = os.ReadFile(fmt.Sprintf("/proc/%d/uid_map", key.pid))
	if err != nil || strings.Join(strings.Fields(string(b)), " ") != "0 165536 65536" {
		return fmt.Errorf("source user namespace is outside the owned mapping: %v", err)
	}
	return nil
}

func dirtyBrokerRun() error {
	if os.Getenv("CRIU_SYSBOX_ASYNC_TRACK") != "1" || os.Getenv("CRIU_SYSBOX_PRE_DUMP") != "1" || os.Getenv("CRIU_SYSBOX_RETAIN_SOURCE") != "1" {
		return fmt.Errorf("tracking broker is outside the owned pre-dump experiment")
	}
	if _, err := os.Lstat(dirtyBrokerSocket); !os.IsNotExist(err) {
		return fmt.Errorf("tracking socket already exists or cannot be inspected: %v", err)
	}
	l, err := net.ListenUnix("unixpacket", &net.UnixAddr{Name: dirtyBrokerSocket, Net: "unixpacket"})
	if err != nil {
		return err
	}
	defer l.Close()
	if err := os.Chmod(dirtyBrokerSocket, 0600); err != nil {
		return err
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(stop)
	go func() { <-stop; _ = l.Close() }()
	entries := map[dirtyIdentity]dirtyDescriptor{}
	defer func() {
		for _, fd := range entries {
			unix.Close(fd.uffd)
			unix.Close(fd.pidfd)
		}
	}()
	fmt.Println("owned asynchronous tracking broker ready")
	for {
		c, err := l.AcceptUnix()
		if err != nil {
			if err == net.ErrClosed || strings.Contains(err.Error(), "use of closed network connection") {
				return nil
			}
			return err
		}
		// Serial, bounded requests. No request mutates the captured workload.
		_ = c.SetDeadline(time.Now().Add(2 * time.Second))
		err = dirtyBrokerRequest(c, entries)
		c.Close()
		if err != nil {
			fmt.Fprintln(os.Stderr, "tracking request rejected:", err)
		}
	}
}

func dirtyBrokerRequest(c *net.UnixConn, entries map[dirtyIdentity]dirtyDescriptor) error {
	var peer *unix.Ucred
	raw, err := c.SyscallConn()
	if err != nil {
		return err
	}
	if err := raw.Control(func(fd uintptr) { peer, err = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED) }); err != nil {
		return err
	}
	if err != nil || peer == nil || peer.Uid != 0 {
		return fmt.Errorf("unexpected tracking controller peer: %v", err)
	}
	packet, ancillary := make([]byte, 24), make([]byte, unix.CmsgSpace(4))
	n, oobn, flags, _, err := c.ReadMsgUnix(packet, ancillary)
	var rights []int
	if oobn > 0 {
		messages, parseErr := unix.ParseSocketControlMessage(ancillary[:oobn])
		if parseErr != nil {
			return parseErr
		}
		for _, message := range messages {
			fds, parseErr := unix.ParseUnixRights(&message)
			if parseErr != nil {
				for _, fd := range rights {
					unix.Close(fd)
				}
				return parseErr
			}
			rights = append(rights, fds...)
		}
	}
	defer func() {
		for _, fd := range rights {
			unix.Close(fd)
		}
	}()
	if err != nil || n != 24 || flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 || binary.LittleEndian.Uint64(packet[16:]) != 0 {
		return fmt.Errorf("invalid tracking request: %v", err)
	}
	op := binary.LittleEndian.Uint32(packet)
	key := dirtyIdentity{pid: int(binary.LittleEndian.Uint32(packet[4:])), ticks: binary.LittleEndian.Uint64(packet[8:])}
	if err := dirtyOwnedProcess(key); err != nil {
		return err
	}
	// Exited process descriptors cannot make a reused PID appear tracked.
	for identity, fd := range entries {
		poll := []unix.PollFd{{Fd: int32(fd.pidfd), Events: unix.POLLIN}}
		if n, err := unix.Poll(poll, 0); err != nil || n > 0 {
			unix.Close(fd.uffd)
			unix.Close(fd.pidfd)
			delete(entries, identity)
		}
	}
	response := append([]byte(nil), packet...)
	binary.LittleEndian.PutUint32(response, 0)
	var output []byte
	switch op {
	case 1: // Get the original mm's retained descriptor.
		if len(rights) != 0 {
			return fmt.Errorf("get request contains descriptors")
		}
		if fd, ok := entries[key]; ok {
			binary.LittleEndian.PutUint32(response, 1)
			output = unix.UnixRights(fd.uffd)
		}
	case 2: // Keep tracking after the CRIU invocation exits.
		if len(rights) != 1 || len(entries) >= 512 {
			return fmt.Errorf("invalid tracking descriptor count or limit")
		}
		fd := rights[0]
		target, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", fd))
		if err != nil || target != "anon_inode:[userfaultfd]" {
			return fmt.Errorf("descriptor is not userfaultfd: %q %v", target, err)
		}
		pidfd, err := unix.PidfdOpen(key.pid, 0)
		if err != nil {
			return err
		}
		if err := dirtyOwnedProcess(key); err != nil {
			unix.Close(pidfd)
			return err
		}
		unix.CloseOnExec(fd)
		if old, ok := entries[key]; ok {
			unix.Close(old.uffd)
			unix.Close(old.pidfd)
		}
		entries[key] = dirtyDescriptor{uffd: fd, pidfd: pidfd}
		rights = nil // Ownership moved to the keeper.
		binary.LittleEndian.PutUint32(response, 1)
	default:
		return fmt.Errorf("unknown tracking operation")
	}
	_, _, err = c.WriteMsgUnix(response, output, nil)
	return err
}
