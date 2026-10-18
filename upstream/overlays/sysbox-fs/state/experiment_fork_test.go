package state

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/nestybox/sysbox-fs/domain"
	"github.com/nestybox/sysbox-fs/mocks"
	libpidfd "github.com/nestybox/sysbox-libs/pidfd"
	"github.com/stretchr/testify/mock"
	"golang.org/x/sys/unix"
)

func TestExperimentForkRejectsStoppedLiveProcessOutsideFrozenSource(t *testing.T) {
	t.Setenv("CRIU_SYSBOX_RETAIN_SOURCE", "1")
	process := exec.Command("/bin/sleep", "60")
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { process.Process.Kill(); process.Wait() }()
	fd, err := libpidfd.Open(process.Process.Pid, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(int(fd))
	if err := unix.Kill(process.Process.Pid, unix.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", process.Process.Pid))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "State:\tT") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("native child did not stop")
		}
		time.Sleep(time.Millisecond)
	}
	original := &container{id: "source-sysbox", initPid: uint32(process.Process.Pid), initPidFd: fd, regCompleted: true, netnsInode: 55}
	css := &containerStateService{idTable: map[string]*container{"source-sysbox": original},
		netnsTable: map[domain.Inode][]*container{55: {original}}, fss: &mocks.FuseServerServiceIface{}}
	if err := css.ContainerForkPreRegister("copy-sysbox", "source-sysbox"); err == nil || !strings.Contains(err.Error(), "owned source cgroup") {
		t.Fatalf("SIGSTOP of a foreign original must not authorize a complete fork: %v", err)
	}
	if len(css.idTable) != 1 {
		t.Fatal("rejected live fork left target state")
	}
}

func TestExperimentForkCopiesVirtualStateWithoutSharing(t *testing.T) {
	process := exec.Command("/bin/sleep", "60")
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { process.Process.Kill(); process.Wait() }()
	fd, err := libpidfd.Open(process.Process.Pid, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(int(fd))
	original := &container{id: "source", initPidFd: fd, regCompleted: true, netnsInode: 55,
		ctime: time.Unix(123, 0), uidFirst: 100000, uidSize: 65536, gidFirst: 100000, gidSize: 65536,
		procRoPaths: []string{"/proc/bus"}, procMaskPaths: []string{"/proc/keys"},
		dataStore: map[string][]byte{"/proc/sys/kernel/hostname": []byte("saved")}}
	fuseMock := &mocks.FuseServerServiceIface{}
	css := &containerStateService{idTable: map[string]*container{"source": original},
		netnsTable: map[domain.Inode][]*container{55: {original}}, fss: fuseMock}
	if err := css.ContainerForkPreRegister("copy", "source"); err == nil || !strings.Contains(err.Error(), "live original") {
		t.Fatalf("expected live source rejection, got %v", err)
	}
	if _, exists := css.idTable["copy"]; exists {
		t.Fatal("rejected fork left a registration")
	}
	process.Process.Kill()
	process.Wait()
	var copyState *container
	fuseMock.On("CreateFuseServer", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		copyState = args.Get(0).(*container)
		if args.Get(0) != args.Get(1) || copyState == original {
			t.Fatal("fork FUSE server shares source state")
		}
	}).Return(nil).Once()
	if err := css.ContainerForkPreRegister("copy", "source"); err != nil {
		t.Fatal(err)
	}
	if copyState == nil || css.idTable["copy"] != copyState || copyState.forkSource != "source" || copyState.ctime != original.ctime {
		t.Fatal("fork identity or saved creation time lost")
	}
	if copyState.initPid != 0 || copyState.initPidFd != 0 || copyState.regCompleted {
		t.Fatal("fork reused source process state")
	}
	original.dataStore["/proc/sys/kernel/hostname"][0] = 'X'
	original.procRoPaths[0] = "/changed"
	if string(copyState.dataStore["/proc/sys/kernel/hostname"]) != "saved" || copyState.procRoPaths[0] != "/proc/bus" {
		t.Fatal("fork retains source byte/slice aliases")
	}
	copyState.dataStore["/proc/sys/kernel/hostname"][1] = 'Y'
	if string(original.dataStore["/proc/sys/kernel/hostname"]) != "Xaved" {
		t.Fatal("fork mutation changed source virtual file")
	}
	registration := &container{id: "copy", uidFirst: copyState.uidFirst + 1, uidSize: copyState.uidSize,
		gidFirst: copyState.gidFirst, gidSize: copyState.gidSize,
		procRoPaths: copyState.procRoPaths, procMaskPaths: copyState.procMaskPaths}
	if err := css.ContainerRegister(registration); err == nil || !strings.Contains(err.Error(), "saved ID mappings or proc protections") {
		t.Fatalf("changed fork mapping accepted: %v", err)
	}
	registration.uidFirst = copyState.uidFirst
	registration.procRoPaths = nil
	if err := css.ContainerRegister(registration); err == nil || !strings.Contains(err.Error(), "saved ID mappings or proc protections") {
		t.Fatalf("weakened fork proc protection accepted: %v", err)
	}
	if copyState.initPidFd != 0 || copyState.regCompleted {
		t.Fatal("rejected registration changed pending fork state")
	}
	before := css.idTable["copy"]
	if err := css.ContainerForkPreRegister("copy", "source"); err == nil || css.idTable["copy"] != before {
		t.Fatal("duplicate fork replaced existing identity")
	}
	fuseMock.AssertExpectations(t)
}

func TestExperimentForkFailureLeavesSourceAndNoTarget(t *testing.T) {
	process := exec.Command("/bin/sleep", "60")
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { process.Process.Kill(); process.Wait() }()
	fd, err := libpidfd.Open(process.Process.Pid, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(int(fd))
	process.Process.Kill()
	process.Wait()
	original := &container{id: "source", initPidFd: fd, regCompleted: true, netnsInode: 55,
		dataStore: map[string][]byte{"/proc/sys/kernel/hostname": []byte("saved")}}
	fuseMock := &mocks.FuseServerServiceIface{}
	css := &containerStateService{idTable: map[string]*container{"source": original},
		netnsTable: map[domain.Inode][]*container{55: {original}}, fss: fuseMock}
	for _, target := range []string{"", "source"} {
		if err := css.ContainerForkPreRegister(target, "source"); err == nil {
			t.Fatal("invalid fork identity accepted")
		}
	}
	if err := css.ContainerForkPreRegister("copy", "missing"); err == nil {
		t.Fatal("unknown source accepted")
	}
	css.netnsTable[55] = append(css.netnsTable[55], &container{id: "peer"})
	if err := css.ContainerForkPreRegister("copy", "source"); err == nil {
		t.Fatal("shared network namespace accepted")
	}
	css.netnsTable[55] = []*container{original}
	fuseMock.On("CreateFuseServer", mock.Anything, mock.Anything).Return(errors.New("FUSE setup failed")).Once()
	if err := css.ContainerForkPreRegister("copy", "source"); err == nil {
		t.Fatal("FUSE setup failure ignored")
	}
	if len(css.idTable) != 1 || css.idTable["source"] != original || string(original.dataStore["/proc/sys/kernel/hostname"]) != "saved" {
		t.Fatal("failed fork mutated retained source or left target state")
	}
	fuseMock.AssertExpectations(t)
}
