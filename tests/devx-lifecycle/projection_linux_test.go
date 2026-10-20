//go:build linux

package devx_test

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestIDEProjectionPeer(t *testing.T) {
	if os.Getenv("FACTORYBOX_IDE_PROJECTION_CHILD") != "1" {
		t.Skip("requires its exact owned namespace parent")
	}
	root := os.Getenv("FACTORYBOX_IDE_PROJECTION_ROOT")
	if !filepath.IsAbs(root) {
		t.Fatal("projection peer root is not absolute")
	}
	if err := unix.Mount("", "/", "", unix.MS_REC|unix.MS_PRIVATE, ""); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/bin", "/usr", "/lib", "/lib64"} {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		}
		target := root + path
		if err := os.MkdirAll(target, 0755); err != nil {
			t.Fatal(err)
		}
		if err := unix.Mount(path, target, "", unix.MS_BIND|unix.MS_REC, ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(root+"/proc", 0755); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mount("proc", root+"/proc", "proc", unix.MS_NOSUID|unix.MS_NODEV|unix.MS_NOEXEC, ""); err != nil {
		t.Fatal(err)
	}
	if err := unix.Chroot(root); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir("/"); err != nil {
		t.Fatal(err)
	}
	ready := os.NewFile(3, "owned projection readiness")
	if _, err := ready.WriteString("ready\n"); err != nil {
		t.Fatal(err)
	}
	ready.Close()
	for {
		time.Sleep(time.Second)
	}
}

func TestNativeIDEProjectionNamespace(t *testing.T) {
	if os.Getenv("FACTORYBOX_DEVX_PROJECTION") != "1" {
		t.Skip("requires its bounded native namespace fixture")
	}
	base := t.TempDir()
	root, sourceRoot := base+"/peer", base+"/source-peer"
	source := sourceRoot + "/workspace/project"
	for _, path := range []string{root, source} {
		if err := os.MkdirAll(path, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(source+"/marker", []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	// The source also owns a user/PID namespace, like the real Sysbox tree.
	// Neither process root is the controller's root.
	for _, path := range []string{filepath.Dir(base), base, sourceRoot, sourceRoot + "/workspace", source, source + "/marker"} {
		if err := os.Chown(path, 165536, 165536); err != nil {
			t.Fatal(err)
		}
	}
	startPeer := func(peerRoot string, isolated bool) int {
		reader, writer, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		defer reader.Close()
		command := exec.Command(os.Args[0], "-test.run=^TestIDEProjectionPeer$", "-test.timeout=20s")
		command.Env = append(os.Environ(), "FACTORYBOX_IDE_PROJECTION_CHILD=1",
			"FACTORYBOX_IDE_PROJECTION_ROOT="+peerRoot)
		command.SysProcAttr = &syscall.SysProcAttr{Cloneflags: unix.CLONE_NEWNS}
		if isolated {
			command.SysProcAttr.Cloneflags |= unix.CLONE_NEWUSER | unix.CLONE_NEWPID
			command.SysProcAttr.UidMappings = []syscall.SysProcIDMap{{ContainerID: 0, HostID: 165536, Size: 65536}}
			command.SysProcAttr.GidMappings = []syscall.SysProcIDMap{{ContainerID: 0, HostID: 165536, Size: 65536}}
			command.SysProcAttr.GidMappingsEnableSetgroups = false
			command.SysProcAttr.Credential = &syscall.Credential{Uid: 0, Gid: 0, NoSetGroups: true}
		}
		command.ExtraFiles = []*os.File{writer}
		command.Stdout, command.Stderr = os.Stdout, os.Stderr
		if err := command.Start(); err != nil {
			writer.Close()
			t.Fatal(err)
		}
		writer.Close()
		t.Cleanup(func() { command.Process.Kill(); command.Wait() })
		if err := reader.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
			t.Fatal(err)
		}
		message, err := bufio.NewReader(reader).ReadString('\n')
		if err != nil || message != "ready\n" {
			t.Fatal("projection peer did not initialize its separate root", err, message)
		}
		return command.Process.Pid
	}
	sourcePID, pid := startPeer(sourceRoot, true), startPeer(root, false)
	namespace, err := os.Open("/proc/" + strconv.Itoa(pid) + "/ns/mnt")
	if err != nil {
		t.Fatal(err)
	}
	defer namespace.Close()
	peerRoot, err := os.Open("/proc/" + strconv.Itoa(pid) + "/root")
	if err != nil {
		t.Fatal(err)
	}
	defer peerRoot.Close()
	ide := &nativeIDE{namespace: namespace, root: peerRoot}
	target := "/factorybox-ide-view-" + strconv.Itoa(pid)
	defer os.Remove(target)
	if _, err := ide.command("mkdir", "-p", "--", target); err != nil {
		t.Fatal(err)
	}
	procSource := "/proc/" + strconv.Itoa(sourcePID) + "/root/workspace/project"
	if _, err := ide.command("mount", "--bind", procSource, target); err != nil {
		t.Fatal("bind the original project into the peer's actual filesystem root", err)
	}
	defer func() {
		if _, err := ide.command("umount", "--", target); err != nil {
			t.Error(err)
		}
	}()
	peerPath := "/proc/" + strconv.Itoa(pid) + "/root"
	value, err := os.ReadFile(peerPath + target + "/marker")
	if err != nil || string(value) != "original" {
		t.Fatal("IDE namespace binding missed the selected peer root", err, string(value))
	}
	if _, err := ide.command("write", target+"/marker", "changed"); err != nil {
		t.Fatal(err)
	}
	value, err = os.ReadFile(source + "/marker")
	if err != nil || string(value) != "changed" {
		t.Fatal("IDE namespace edit did not reach its original live project", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("IDE helper created its target in the controller's root", err)
	}
}
