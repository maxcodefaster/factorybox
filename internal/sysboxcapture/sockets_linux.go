//go:build linux

package sysboxcapture

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

type socketProcess struct {
	PID   int    `json:"pid"`
	Start string `json:"start"`
	file  *os.File
}

type captureSocket struct {
	Process int    `json:"pid"`
	FD      int    `json:"fd"`
	Cookie  uint64 `json:"cookie"`
	Repair  int    `json:"repair"`
	Mark    int    `json:"mark"`
}

// TrackSockets records only TCP sockets from the guard's frozen source TIDs.
// Original process handles prevent a reused numeric PID from becoming a target.
func (n *CaptureNetwork) TrackSockets(threads map[int]string) error {
	if err := n.validate(); err != nil {
		return err
	}
	for tid, start := range threads {
		current, err := networkProcessStart(tid)
		if err != nil || current != start {
			return fmt.Errorf("socket source thread identity changed: %v", err)
		}
		status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", tid))
		if err != nil {
			return err
		}
		pid := 0
		for _, line := range strings.Split(string(status), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[0] == "Tgid:" {
				pid, err = strconv.Atoi(fields[1])
			}
		}
		if err != nil || pid <= 1 {
			return fmt.Errorf("socket source thread lacks a valid process")
		}
		if _, present := n.state.processes[pid]; present {
			continue
		}
		if len(n.state.processes) >= 4096 {
			return fmt.Errorf("socket source process inventory exceeds bound")
		}
		birth, err := networkProcessStart(pid)
		if err != nil {
			return err
		}
		pidfd, err := unix.PidfdOpen(pid, 0)
		if err != nil {
			return err
		}
		process := socketProcess{PID: pid, Start: birth, file: os.NewFile(uintptr(pidfd), "original source socket process")}
		n.state.processes[pid] = process
		directory, err := os.Open(fmt.Sprintf("/proc/%d/fd", pid))
		if err != nil {
			return err
		}
		entries, readErr := directory.ReadDir(65537)
		closeErr := directory.Close()
		if readErr != nil || closeErr != nil || len(entries) > 65536 {
			return fmt.Errorf("source descriptor inventory exceeds bound or cannot be read: %v", errors.Join(readErr, closeErr))
		}
		for _, entry := range entries {
			target, err := os.Readlink(fmt.Sprintf("/proc/%d/fd/%s", pid, entry.Name()))
			if err != nil {
				return err
			}
			if !strings.HasPrefix(target, "socket:[") {
				continue
			}
			fd, err := strconv.Atoi(entry.Name())
			if err != nil {
				return err
			}
			borrowed, err := unix.PidfdGetfd(pidfd, fd, 0)
			if err != nil {
				return err
			}
			socket, tcp, inspectErr := inspectCaptureSocket(borrowed)
			closeErr := unix.Close(borrowed)
			if inspectErr != nil || closeErr != nil {
				return errors.Join(inspectErr, closeErr)
			}
			if tcp {
				if len(n.state.sockets) >= 16384 {
					return fmt.Errorf("source TCP inventory exceeds bound")
				}
				socket.Process, socket.FD = pid, fd
				n.state.sockets = append(n.state.sockets, socket)
			}
		}
	}
	data, err := json.Marshal(map[string]any{"version": 1, "sockets": n.state.sockets})
	if err != nil {
		return err
	}
	return writePrivate(n.state.generation+"/sockets-before.json", append(data, '\n'))
}

func inspectCaptureSocket(fd int) (captureSocket, bool, error) {
	var result captureSocket
	protocol, err := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_PROTOCOL)
	if err != nil {
		return result, false, err
	}
	domain, err := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_DOMAIN)
	if err != nil {
		return result, false, err
	}
	if protocol != unix.IPPROTO_TCP || (domain != unix.AF_INET && domain != unix.AF_INET6) {
		return result, false, nil
	}
	result.Cookie, err = unix.GetsockoptUint64(fd, unix.SOL_SOCKET, unix.SO_COOKIE)
	if err != nil {
		return result, false, err
	}
	result.Repair, err = unix.GetsockoptInt(fd, unix.IPPROTO_TCP, unix.TCP_REPAIR)
	if err != nil {
		return result, false, err
	}
	result.Mark, err = unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_MARK)
	return result, true, err
}

type socketRollback struct {
	original captureSocket
	file     *os.File
}

func closeSocketRollback(plans []socketRollback) error {
	var result error
	for _, plan := range plans {
		result = errors.Join(result, plan.file.Close())
	}
	return result
}

func (n *CaptureNetwork) prepareSocketRollback() (_ []socketRollback, resultErr error) {
	var plans []socketRollback
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, closeSocketRollback(plans))
		}
	}()
	for _, original := range n.state.sockets {
		process := n.state.processes[original.Process]
		birth, err := networkProcessStart(process.PID)
		if err != nil || birth != process.Start {
			return nil, fmt.Errorf("socket source process no longer has its original birth")
		}
		fd, err := unix.PidfdGetfd(int(process.file.Fd()), original.FD, 0)
		if err != nil {
			return nil, err
		}
		file := os.NewFile(uintptr(fd), "verified original source TCP socket")
		plans = append(plans, socketRollback{original, file})
		current, tcp, err := inspectCaptureSocket(fd)
		if err != nil || !tcp || !admitSocketRollback(original, current) {
			return nil, fmt.Errorf("socket cookie or properties changed outside the known CRIU delta: %v", err)
		}
	}
	return plans, nil
}

func admitSocketRollback(original, current captureSocket) bool {
	return original.Cookie != 0 && original.Cookie == current.Cookie &&
		(current.Repair == original.Repair || original.Repair == 0 && current.Repair == 1) &&
		current.Mark == original.Mark
}

func restoreCaptureSockets(plans []socketRollback) error {
	for _, plan := range plans {
		fd := int(plan.file.Fd())
		current, tcp, err := inspectCaptureSocket(fd)
		if err != nil || !tcp || !admitSocketRollback(plan.original, current) {
			return fmt.Errorf("source TCP socket changed after rollback admission: %v", err)
		}
		if current.Repair != plan.original.Repair {
			if err := unix.SetsockoptInt(fd, unix.IPPROTO_TCP, unix.TCP_REPAIR, plan.original.Repair); err != nil {
				return err
			}
		}
		final, _, err := inspectCaptureSocket(fd)
		if err != nil || final.Cookie != plan.original.Cookie ||
			final.Repair != plan.original.Repair || final.Mark != plan.original.Mark {
			return fmt.Errorf("source TCP socket did not return to its captured properties: %v", err)
		}
	}
	return nil
}
