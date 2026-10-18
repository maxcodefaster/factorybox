//go:build linux

package main

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// This first integration changes only the source pager's immutable parent.
// The existing full fork, source identity, service, IDE and release gates follow.
func criuRichEpochRebase(id, images string, retained map[int]string) (resultErr error) {
	start := time.Now()
	proof := map[string]any{
		"images": images,
		"scope":  "complete captured parent made readonly and published under uninterrupted kernel freeze; RAM parent remains explicitly charged and is persisted/released during owned teardown; repeated comparisons still required",
	}
	defer func() { record("sysbox-source-epoch-rebase", start, asJSON(proof), resultErr) }()
	if os.Getenv("RUNTIME_FORK_RICH_EPOCH") != "1" || id != "source-sysbox" ||
		criuRoot != "/experiment" || images != "/experiment/sysbox/images-again" ||
		!criuSysboxInheritEnabled() || !criuLazyWPEnabled() || criuCopyPipelineEnabled() {
		return fmt.Errorf("rich epoch is outside its owned frozen fixture")
	}
	if err := criuSysboxFrozenSourceProof(retained, "epoch-before"); err != nil {
		return err
	}
	source, err := criuInheritSourceDaemon()
	if err != nil {
		return err
	}
	identity, err := criuRuntimeIdentity(id)
	if err != nil {
		return err
	}
	pid, ok := identity["init_pid"].(int)
	if !ok || retained[pid] == "" {
		return fmt.Errorf("epoch root does not have its retained PID/start time")
	}
	// The new pager must not retain an extra tmpfs mount after suspend.
	// Back and release this complete capture before acquiring the new reader.
	if os.Getenv("RUNTIME_FORK_EPOCH_RAM_PARENT") != "1" {
		if err := criuSysboxReleaseRAMCapture(images); err != nil {
			return err
		}
	} else if criuRAMSnapshotFind(images) == nil {
		return fmt.Errorf("RAM parent has no owned charged snapshot")
	}
	if err := criuParentReadonlyBind(images, images); err != nil {
		return err
	}
	criuParentBindings = append(criuParentBindings, images)
	if err := criuSysboxParentLink(images); err != nil {
		return err
	}
	var old, next unix.Stat_t
	if err := unix.Stat("/experiment/sysbox/images-base", &old); err != nil {
		return err
	}
	if err := unix.Stat(images, &next); err != nil {
		return err
	}
	nonce := make([]byte, 8)
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	frame := struct {
		Magic, Nonce, Generation, OldDev, OldIno, NewDev, NewIno uint64
		PID                                                      int32
		Flags                                                    uint32
	}{0x45504f43484c415a, binary.LittleEndian.Uint64(nonce), 0, old.Dev, old.Ino, next.Dev, next.Ino, 0, 1}
	packet := new(bytes.Buffer)
	if err := binary.Write(packet, binary.LittleEndian, frame); err != nil || packet.Len() != 64 {
		return fmt.Errorf("invalid epoch request: %v", err)
	}
	proof["request"] = frame
	if err := os.WriteFile("/experiment/source-epoch-intent.json", []byte(asJSON(proof)+"\n"), 0600); err != nil {
		return err
	}
	pidfd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		return err
	}
	defer unix.Close(pidfd)
	directory, err := unix.Open(images, unix.O_DIRECTORY|unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(directory)
	cgroup, err := unix.Open(criuOwnedSourceCgroup, unix.O_DIRECTORY|unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(cgroup)
	socket := "/experiment/sysbox/lazy-epoch.socket"
	info, err := os.Lstat(socket)
	if err != nil || info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0600 {
		return fmt.Errorf("invalid owned epoch endpoint: %v", err)
	}
	connection, err := net.DialUnix("unixpacket", nil, &net.UnixAddr{Name: socket, Net: "unixpacket"})
	if err != nil {
		return err
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	raw, err := connection.SyscallConn()
	if err != nil {
		return err
	}
	var peer *unix.Ucred
	var peerErr error
	if err := raw.Control(func(fd uintptr) { peer, peerErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED) }); err != nil {
		return err
	}
	if peerErr != nil || peer == nil || int(peer.Pid) != source.command.Process.Pid || int(peer.Uid) != os.Geteuid() {
		return fmt.Errorf("epoch endpoint belongs to a different process: %v", peerErr)
	}
	ancillary := unix.UnixRights(pidfd, directory, cgroup)
	n, oobn, err := connection.WriteMsgUnix(packet.Bytes(), ancillary, nil)
	if err != nil || n != 64 || oobn != len(ancillary) {
		return fmt.Errorf("sending epoch command: bytes=%d rights=%d: %v", n, oobn, err)
	}
	var response [65]byte
	n, oobn, flags, _, err := connection.ReadMsgUnix(response[:], nil)
	if err != nil || n != 64 || oobn != 0 || flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 {
		return fmt.Errorf("reading epoch acknowledgement: bytes=%d rights=%d flags=%d: %v", n, oobn, flags, err)
	}
	var reply struct {
		Magic, Nonce, Generation               uint64
		Status, ImagePID                       int32
		ResetPages, MissingPages, CopiedRanges uint64
		TrackingDisabled, Roots                uint32
	}
	if err := binary.Read(bytes.NewReader(response[:64]), binary.LittleEndian, &reply); err != nil {
		return err
	}
	proof["reply"] = reply
	if reply.Magic != frame.Magic || reply.Nonce != frame.Nonce {
		return fmt.Errorf("epoch acknowledgement identity mismatch")
	}
	if reply.Status != 0 || reply.Generation != 1 || reply.ImagePID <= 0 || reply.Roots == 0 || reply.TrackingDisabled != 0 {
		return fmt.Errorf("epoch rebase refused or incomplete: %s", asJSON(reply))
	}
	if err := criuSysboxFrozenSourceProof(retained, "epoch-after"); err != nil {
		return err
	}
	return os.WriteFile("/experiment/source-epoch-result.json", []byte(asJSON(proof)+"\n"), 0600)
}
