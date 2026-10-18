//go:build linux

package libcontainer

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	criurpc "github.com/checkpoint-restore/go-criu/v4/rpc"
	"github.com/nestybox/sysbox-ipc/sysboxFsGrpc"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"golang.org/x/sys/unix"
)

// The inherited socket is private to one restore. Kernel credentials identify
// senders in the controller PID namespace, even for inner PID-1 processes.
type experimentSeccompBridge struct {
	conn      *net.UnixConn
	child     *os.File
	finished  chan error
	criuPID   atomic.Int32
	container *linuxContainer
}

func (c *linuxContainer) experimentCgroupRoot(req *criurpc.CriuReq) error {
	root := strings.TrimPrefix(c.cgroupManager.GetPaths()[""], "/sys/fs/cgroup")
	if root != "/criu-probe-sysbox" && root != "/criu-probe-sysbox-copy" &&
		!regexp.MustCompile(`^/criu-probe-branch-[0-9a-f]{32}$`).MatchString(root) {
		return fmt.Errorf("restore cgroup root is outside the owned fixture: %q", root)
	}
	// Sysbox puts CRIU in init.scope. Its cgroup namespace belongs to the
	// entire parent sandbox, including Docker and external-parent exec jobs.
	controller := ""
	req.Opts.CgRoot = append(req.Opts.CgRoot, &criurpc.CgroupRoot{Ctrl: &controller, Path: &root})
	return nil
}

func (c *linuxContainer) newExperimentSeccompBridge() (*experimentSeccompBridge, error) {
	if !c.sysbox.Fs.Enabled() || len(c.config.UidMappings) == 0 {
		return nil, fmt.Errorf("Sysbox fs and saved UID mappings are required")
	}
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	parent := os.NewFile(uintptr(fds[0]), "sysbox-restore-listeners")
	child := os.NewFile(uintptr(fds[1]), "sysbox-restore-listeners-child")
	defer parent.Close()
	if err := unix.SetsockoptInt(fds[0], unix.SOL_SOCKET, unix.SO_PASSCRED, 1); err != nil {
		child.Close()
		return nil, err
	}
	connection, err := net.FileConn(parent)
	if err != nil {
		child.Close()
		return nil, err
	}
	b := &experimentSeccompBridge{conn: connection.(*net.UnixConn), child: child, finished: make(chan error, 1), container: c}
	go func() {
		defer b.conn.Close()
		err := b.receive()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Sysbox restore listener receiver failed: %v\n", err)
		}
		b.finished <- err
	}()
	return b, nil
}

func (b *experimentSeccompBridge) receive() error {
	listeners := 0
	for {
		if err := b.conn.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
			return err
		}
		payload := make([]byte, 16)
		control := make([]byte, unix.CmsgSpace(4)+unix.CmsgSpace(unix.SizeofUcred))
		n, nc, flags, _, err := b.conn.ReadMsgUnix(payload, control)
		if err != nil {
			return fmt.Errorf("read restore listener handoff: %w", err)
		}
		messages, err := unix.ParseSocketControlMessage(control[:nc])
		if err != nil {
			return err
		}
		var descriptors []int
		var credentials *unix.Ucred
		for _, msg := range messages {
			if msg.Header.Level != unix.SOL_SOCKET {
				continue
			}
			switch msg.Header.Type {
			case unix.SCM_RIGHTS:
				fds, parseErr := unix.ParseUnixRights(&msg)
				if parseErr != nil {
					err = parseErr
				} else {
					descriptors = append(descriptors, fds...)
				}
			case unix.SCM_CREDENTIALS:
				credentials, err = unix.ParseUnixCredentials(&msg)
			}
		}
		packetErr := err
		if n != 16 || flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 || credentials == nil || credentials.Pid <= 0 {
			packetErr = fmt.Errorf("invalid restore listener packet or credentials")
		}
		if packetErr == nil {
			switch binary.LittleEndian.Uint32(payload[:4]) {
			case 1:
				if len(descriptors) != 1 || credentials.Uid != uint32(b.container.config.UidMappings[0].HostID) {
					packetErr = fmt.Errorf("listener sender or descriptor count does not match the owned Sysbox tree: pid=%d uid=%d expected_uid=%d descriptors=%d", credentials.Pid, credentials.Uid, b.container.config.UidMappings[0].HostID, len(descriptors))
				} else {
					packetErr = b.container.sysbox.Fs.SendSeccompInit(int(credentials.Pid), b.container.id, int32(descriptors[0]))
					if packetErr == nil {
						listeners++
						fmt.Fprintf(os.Stderr, "Sysbox restore listener attached: pid=%d image_tid=%d filter=%d\n", credentials.Pid, binary.LittleEndian.Uint32(payload[4:8]), binary.LittleEndian.Uint32(payload[8:12]))
					}
				}
			case 2:
				if len(descriptors) != 0 || credentials.Pid != b.criuPID.Load() || listeners == 0 {
					packetErr = fmt.Errorf("invalid listener completion marker")
				} else {
					fmt.Fprintf(os.Stderr, "Sysbox restore listener handoff completed: count=%d\n", listeners)
					return nil
				}
			default:
				packetErr = fmt.Errorf("unknown restore listener packet")
			}
		}
		for _, fd := range descriptors {
			unix.Close(fd)
		}
		if packetErr != nil {
			return packetErr
		}
	}
}

func (b *experimentSeccompBridge) wait() error {
	select {
	case err := <-b.finished:
		return err
	case <-time.After(10 * time.Second):
		return fmt.Errorf("Sysbox listener handoff was not acknowledged before resume")
	}
}

func (c *linuxContainer) experimentRebindFs(pid int) error {
	uid, gid := c.config.UidMappings[0], c.config.GidMappings[0]
	data := &sysboxFsGrpc.ContainerData{Id: c.id, InitPid: int32(pid), Hostname: c.config.Hostname,
		UidFirst: int32(uid.HostID), UidSize: int32(uid.Size), GidFirst: int32(gid.HostID), GidSize: int32(gid.Size)}
	for _, path := range c.config.ReadonlyPaths {
		if strings.HasPrefix(path, "/proc") {
			data.ProcRoPaths = append(data.ProcRoPaths, path)
		}
	}
	for _, path := range c.config.MaskPaths {
		if strings.HasPrefix(path, "/proc") {
			data.ProcMaskPaths = append(data.ProcMaskPaths, path)
		}
	}
	if err := sysboxFsGrpc.SendContainerRegistration(data); err != nil {
		return err
	}
	if !c.sysbox.Fs.Reg {
		// A fresh fork has no manager process/namespace identity yet. Bind it
		// before CRIU releases any task or the copied Docker daemon resumes.
		if err := c.sysbox.Mgr.Update(fmt.Sprintf("/proc/%d/ns/user", pid), fmt.Sprintf("/proc/%d/ns/net", pid),
			[]specs.LinuxIDMapping{{ContainerID: uint32(uid.ContainerID), HostID: uint32(uid.HostID), Size: uint32(uid.Size)}},
			[]specs.LinuxIDMapping{{ContainerID: uint32(gid.ContainerID), HostID: uint32(gid.HostID), Size: uint32(gid.Size)}}, c.config.RootfsUidShiftType); err != nil {
			return err
		}
		c.sysbox.Fs.Reg = true
	}
	return nil
}
