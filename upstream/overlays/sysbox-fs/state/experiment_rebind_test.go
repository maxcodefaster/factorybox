package state

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/nestybox/sysbox-fs/domain"
	"github.com/nestybox/sysbox-fs/mocks"
	libpidfd "github.com/nestybox/sysbox-libs/pidfd"
	"golang.org/x/sys/unix"
)

func TestExperimentRetainedNetworkNamespaceLifetime(t *testing.T) {
	process := exec.Command("/bin/sleep", "60")
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { process.Process.Kill(); process.Wait() }()
	path := "/proc/" + fmt.Sprint(process.Process.Pid) + "/ns/net"
	var stat unix.Stat_t
	if err := unix.Stat(path, &stat); err != nil {
		t.Fatal(err)
	}
	pin, err := pinContainerNetns(uint32(process.Process.Pid), stat.Ino)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.Close()
	flags, err := unix.FcntlInt(pin.Fd(), unix.F_GETFD, 0)
	if err != nil || flags&unix.FD_CLOEXEC == 0 {
		t.Fatalf("namespace reference can leak through exec: flags=%d error=%v", flags, err)
	}
	process.Process.Kill()
	process.Wait()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("dead process still exposes its namespace: %v", err)
	}
	if err := unix.Fstat(int(pin.Fd()), &stat); err != nil || stat.Ino == 0 {
		t.Fatalf("namespace reference did not survive process exit: %v", err)
	}
	if bad, err := pinContainerNetns(uint32(os.Getpid()), stat.Ino+1); err == nil {
		bad.Close()
		t.Fatal("changed namespace identity was accepted")
	}
	peerPin, err := pinContainerNetns(uint32(os.Getpid()), stat.Ino)
	if err != nil {
		t.Fatal(err)
	}
	defer peerPin.Close()
	owned := &container{id: "owned", netnsInode: stat.Ino, netnsPin: pin}
	peer := &container{id: "peer", netnsInode: stat.Ino, netnsPin: peerPin}
	fuseMock := &mocks.FuseServerServiceIface{}
	fuseMock.On("DestroyFuseServer", "owned").Return(nil).Once()
	css := &containerStateService{idTable: map[string]*container{"owned": owned, "peer": peer},
		netnsTable: map[domain.Inode][]*container{stat.Ino: {owned, peer}}, fss: fuseMock}
	if err := css.ContainerUnregister(owned); err != nil {
		t.Fatal(err)
	}
	if _, err := pin.Stat(); err == nil || owned.netnsPin != nil {
		t.Fatal("unregistration leaked its namespace reference")
	}
	if _, err := peerPin.Stat(); err != nil || len(css.netnsTable[stat.Ino]) != 1 || css.netnsTable[stat.Ino][0] != peer {
		t.Fatal("unregistration released another container's namespace identity")
	}
	fuseMock.AssertExpectations(t)
}

func TestExperimentRetainedRejectsLiveTakeover(t *testing.T) {
	fd, err := libpidfd.Open(os.Getpid(), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(int(fd))
	curr := &container{id: "owned", initPid: uint32(os.Getpid()), initPidFd: fd, regCompleted: true,
		uidFirst: 100000, uidSize: 65536, gidFirst: 100000, gidSize: 65536,
		procRoPaths: []string{"/proc/sys"}, dataStore: map[string][]byte{"/proc/sys/kernel/hostname": []byte("retained")}}
	css := &containerStateService{idTable: map[string]*container{"owned": curr}}
	next := &container{id: curr.id, initPid: 999999, uidFirst: curr.uidFirst, uidSize: curr.uidSize,
		gidFirst: curr.gidFirst, gidSize: curr.gidSize, procRoPaths: curr.procRoPaths}
	err = css.ContainerRegister(next)
	if err == nil || !strings.Contains(err.Error(), "live original") {
		t.Fatalf("expected live takeover rejection, got %v", err)
	}
	if curr.initPid != uint32(os.Getpid()) || curr.initPidFd != fd || string(curr.dataStore["/proc/sys/kernel/hostname"]) != "retained" {
		t.Fatal("rejected registration mutated retained state")
	}
	next.uidFirst++
	err = css.ContainerRegister(next)
	if err == nil || !strings.Contains(err.Error(), "ID mappings or proc protections") {
		t.Fatalf("expected ID map rejection, got %v", err)
	}
	next.uidFirst = curr.uidFirst
	next.procRoPaths = nil
	err = css.ContainerRegister(next)
	if err == nil || !strings.Contains(err.Error(), "ID mappings or proc protections") {
		t.Fatalf("expected proc protection rejection, got %v", err)
	}
}
