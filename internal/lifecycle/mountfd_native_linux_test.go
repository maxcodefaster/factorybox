//go:build linux

package lifecycle

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type mountFDActorState struct {
	Payload          []byte `json:"payload"`
	Counter          int    `json:"counter"`
	FD               int    `json:"fd"`
	MountID          int    `json:"mount_id"`
	Visible          bool   `json:"visible"`
	FSType           int64  `json:"fs_type"`
	Flags            int    `json:"flags"`
	SelfPID          int    `json:"self_pid"`
	RootInode        uint64 `json:"root_inode"`
	MountFlags       int64  `json:"mount_flags"`
	HasUptime        bool   `json:"has_uptime"`
	SharedFD         int    `json:"shared_fd"`
	SharedMountID    int    `json:"shared_mount_id"`
	OtherFD          int    `json:"other_fd"`
	OtherMountID     int    `json:"other_mount_id"`
	ProcOptions      string `json:"proc_options"`
	DirectoryFD      int    `json:"directory_fd,omitempty"`
	DirectoryMountID int    `json:"directory_mount_id,omitempty"`
	DirectoryFlags   int    `json:"directory_flags,omitempty"`
	DirectoryTarget  string `json:"directory_target,omitempty"`
	Error            string `json:"error,omitempty"`
}

func TestMountFDInspector(t *testing.T) {
	if os.Getenv("FACTORYBOX_MOUNT_FD_INSPECTOR") != "1" {
		t.Skip("owned private mount-namespace inspector only")
	}
	if err := unix.Mount("", "/", "", unix.MS_PRIVATE|unix.MS_REC, ""); err != nil {
		t.Fatal(err)
	}
	target, err := os.MkdirTemp("/tmp", "mount-fd-inspection-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Remove(target); err != nil {
			t.Error("remove owned inspection directory", err)
		}
	}()
	clone, err := unix.OpenTree(3, "", unix.OPEN_TREE_CLONE|unix.OPEN_TREE_CLOEXEC|unix.AT_EMPTY_PATH)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(clone)
	if err := unix.MoveMount(clone, "", unix.AT_FDCWD, target, unix.MOVE_MOUNT_F_EMPTY_PATH); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := unix.Unmount(target, unix.MNT_DETACH); err != nil {
			t.Error("detach owned inspection clone", err)
		}
	}()
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 10 || fields[4] != target {
			continue
		}
		for index, field := range fields {
			if field == "-" && index+3 < len(fields) && fields[index+1] == "proc" {
				if err := json.NewEncoder(os.Stdout).Encode(fields[index+3]); err != nil {
					t.Fatal(err)
				}
				return
			}
		}
	}
	t.Fatal("detached proc clone missing from private mountinfo")
}

func inspectDetachedProcOptions(fd int) (string, error) {
	duplicate, err := unix.Dup(fd)
	if err != nil {
		return "", err
	}
	file := os.NewFile(uintptr(duplicate), "owned-detached-proc")
	defer file.Close()
	inspector := exec.Command(os.Args[0], "-test.run=^TestMountFDInspector$", "-test.timeout=5s")
	inspector.Env = append(os.Environ(), "FACTORYBOX_MOUNT_FD_INSPECTOR=1")
	inspector.ExtraFiles = []*os.File{file}
	inspector.SysProcAttr = &syscall.SysProcAttr{Cloneflags: unix.CLONE_NEWNS}
	output, err := inspector.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("private mount option inspection: %w: %s", err, output)
	}
	var options string
	if err := json.NewDecoder(strings.NewReader(string(output))).Decode(&options); err != nil {
		return "", err
	}
	return options, nil
}

func createDetachedProc(mode string) (int, error) {
	fs, err := unix.Fsopen("proc", unix.FSOPEN_CLOEXEC)
	if err != nil {
		return -1, err
	}
	defer unix.Close(fs)
	attrs := 0
	if mode == "restricted" {
		for key, value := range map[string]string{"hidepid": "ptraceable", "subset": "pid"} {
			if err := unix.FsconfigSetString(fs, key, value); err != nil {
				return -1, err
			}
		}
		attrs = unix.MOUNT_ATTR_NODEV | unix.MOUNT_ATTR_NOEXEC | unix.MOUNT_ATTR_NOSUID
	}
	if err := unix.FsconfigCreate(fs); err != nil {
		return -1, err
	}
	return unix.Fsmount(fs, unix.FSMOUNT_CLOEXEC, attrs)
}

func mountIDForFD(fd int) (int, error) {
	info, err := os.ReadFile(fmt.Sprintf("/proc/self/fdinfo/%d", fd))
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(info), "\n") {
		if strings.HasPrefix(line, "mnt_id:") {
			return strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "mnt_id:")))
		}
	}
	return 0, fmt.Errorf("descriptor has no mount ID")
}

func TestMountFDActor(t *testing.T) {
	mode := os.Getenv("FACTORYBOX_MOUNT_FD_ACTOR")
	if mode != "default" && mode != "restricted" {
		t.Skip("owned detached mount descriptor workload only")
	}
	fd, err := createDetachedProc(mode)
	if err != nil {
		t.Fatal("create detached proc mount", err)
	}
	defer unix.Close(fd)
	shared, err := unix.Openat(fd, ".", unix.O_PATH|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(shared)
	other, err := createDetachedProc("default")
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(other)
	directory := -1
	var foreign *exec.Cmd
	var mutex sync.Mutex
	closeDirectory := func() error {
		var result error
		if directory >= 0 {
			result = unix.Close(directory)
			directory = -1
		}
		if foreign != nil {
			killErr := foreign.Process.Kill()
			waitErr := foreign.Wait()
			if killErr != nil || waitErr == nil {
				result = errors.Join(result, fmt.Errorf("retire owned proc directory child: %v %v", killErr, waitErr))
			}
			foreign = nil
		}
		return result
	}
	defer func() {
		if directory >= 0 {
			unix.Close(directory)
		}
	}()
	state := mountFDActorState{Payload: make([]byte, 8192), FD: fd, SharedFD: shared, OtherFD: other}
	if _, err := rand.Read(state.Payload); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir("/state", 0700); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", "/state/mount-fd.sock")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	for {
		client, err := listener.Accept()
		if err != nil {
			t.Fatal(err)
		}
		func() {
			defer client.Close()
			mutex.Lock()
			defer mutex.Unlock()
			var command string
			if err := json.NewDecoder(io.LimitReader(client, 1024)).Decode(&command); err != nil {
				t.Error(err)
				return
			}
			transient := false
			if command == "advance" {
				state.Counter++
				state.Payload[0] ^= 0xff
			} else if command == "open-self-fds" {
				if directory >= 0 {
					t.Error("owned proc directory already open")
					return
				}
				directory, err = unix.Openat(fd, "self/task/"+strconv.Itoa(os.Getpid())+"/fd",
					unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
				if err != nil {
					t.Error("open directory in detached proc", err)
					return
				}
				state.DirectoryFD = directory
			} else if command == "open-foreign-fds" || command == "open-transient-foreign-fds" {
				if directory >= 0 || foreign != nil {
					t.Error("owned proc directory child already exists")
					return
				}
				foreign = exec.Command("/bin/sleep", "300")
				for index := 3; index <= fd; index++ {
					duplicate, err := unix.Dup(fd)
					if err != nil {
						t.Error(err)
						return
					}
					foreign.ExtraFiles = append(foreign.ExtraFiles, os.NewFile(uintptr(duplicate), "owned proc root"))
				}
				err = foreign.Start()
				for _, inherited := range foreign.ExtraFiles {
					inherited.Close()
				}
				if err != nil {
					t.Error(err)
					return
				}
				pid := foreign.Process.Pid
				directory, err = unix.Openat(fd, fmt.Sprintf("%d/task/%d/fd", pid, pid),
					unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
				if err != nil {
					t.Error(err)
					return
				}
				state.DirectoryFD = directory
				transient = command == "open-transient-foreign-fds"
			} else if command == "close-directory" {
				if err := closeDirectory(); err != nil {
					t.Error(err)
					return
				}
				state.DirectoryFD, state.DirectoryMountID, state.DirectoryFlags, state.DirectoryTarget = 0, 0, 0, ""
			} else if command != "get" {
				t.Error("invalid mount actor command")
				return
			}
			for descriptor, target := range map[int]*int{fd: &state.MountID, shared: &state.SharedMountID, other: &state.OtherMountID} {
				*target, err = mountIDForFD(descriptor)
				if err != nil {
					t.Error(err)
					return
				}
			}
			mounts, err := os.ReadFile("/proc/self/mountinfo")
			if err != nil {
				t.Error(err)
				return
			}
			if directory >= 0 {
				state.DirectoryMountID, err = mountIDForFD(directory)
				if err == nil {
					state.DirectoryFlags, err = unix.FcntlInt(uintptr(directory), unix.F_GETFL, 0)
				}
				if err == nil {
					var target [64]byte
					count, readErr := unix.Readlinkat(directory, strconv.Itoa(fd), target[:])
					err = readErr
					if err == nil {
						state.DirectoryTarget = string(target[:count])
					}
				}
				if err != nil {
					t.Error("read retained proc directory", err)
					return
				}
			}
			state.Visible = false
			for _, line := range strings.Split(string(mounts), "\n") {
				state.Visible = state.Visible || strings.HasPrefix(line, strconv.Itoa(state.MountID)+" ")
			}
			var stat unix.Stat_t
			var fsstat unix.Statfs_t
			if err := unix.Fstat(fd, &stat); err != nil {
				t.Error(err)
				return
			}
			if err := unix.Fstatfs(fd, &fsstat); err != nil {
				t.Error(err)
				return
			}
			state.RootInode, state.FSType = stat.Ino, fsstat.Type
			state.MountFlags = fsstat.Flags
			uptimeErr := unix.Fstatat(fd, "uptime", &stat, 0)
			if uptimeErr != nil && !errors.Is(uptimeErr, unix.ENOENT) {
				t.Error(uptimeErr)
				return
			}
			state.HasUptime = uptimeErr == nil
			state.ProcOptions, err = inspectDetachedProcOptions(fd)
			if err != nil {
				state.Error = err.Error()
				json.NewEncoder(client).Encode(state)
				return
			}
			state.Flags, err = unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
			if err != nil {
				t.Error(err)
				return
			}
			link := make([]byte, 32)
			count, err := unix.Readlinkat(fd, "self", link)
			if err == nil {
				state.SelfPID, err = strconv.Atoi(string(link[:count]))
			}
			if err != nil {
				t.Error(err)
				return
			}
			if transient {
				originalDirectory, originalChild := directory, foreign
				time.AfterFunc(1500*time.Millisecond, func() {
					mutex.Lock()
					defer mutex.Unlock()
					if directory != originalDirectory || foreign != originalChild {
						return
					}
					if err := closeDirectory(); err != nil {
						t.Error(err)
						return
					}
					state.DirectoryFD, state.DirectoryMountID, state.DirectoryFlags, state.DirectoryTarget = 0, 0, 0, ""
				})
			}
			if err := json.NewEncoder(client).Encode(state); err != nil {
				t.Error(err)
			}
		}()
	}
}

func readMountFDActor(pid int, command string) (mountFDActorState, error) {
	var result mountFDActorState
	client, err := net.DialTimeout("unix",
		fmt.Sprintf("/proc/%d/root/state/mount-fd.sock", pid), time.Second)
	if err != nil {
		return result, err
	}
	defer client.Close()
	client.SetDeadline(time.Now().Add(6 * time.Second))
	if err := json.NewEncoder(client).Encode(command); err != nil {
		return result, err
	}
	data, err := io.ReadAll(io.LimitReader(client, 32<<10))
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return result, err
	}
	if result.Error != "" {
		return result, fmt.Errorf("mount actor: %s", result.Error)
	}
	return result, nil
}

func TestDetachedMountForkNative(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_MOUNT_FD") != "1" {
		t.Skip("requires the bounded detached-mount checkpoint fixture")
	}
	for _, mode := range []string{"default", "restricted"} {
		t.Run(mode, func(t *testing.T) { detachedMountForkNative(t, mode) })
	}
}

func detachedMountForkNative(t *testing.T, mode string) {
	engine, bundle := nativeEngine(t,
		"/bin/env FACTORYBOX_MOUNT_FD_ACTOR="+mode+" /bin/mount-fd-actor -test.run=^TestMountFDActor$ -test.timeout=0")
	binary, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundle+"/lower/bin/mount-fd-actor", binary, 0755); err != nil {
		t.Fatal(err)
	}
	config, err := os.ReadFile(bundle + "/config.json")
	if err != nil {
		t.Fatal(err)
	}
	var spec map[string]any
	if err := json.Unmarshal(config, &spec); err != nil {
		t.Fatal(err)
	}
	args := spec["process"].(map[string]any)["args"].([]any)
	args[2] = strings.Replace(args[2].(string), ">/dev/null", ">/mount-fd-actor.log", 1)
	config, err = json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundle+"/config.json", config, 0600); err != nil {
		t.Fatal(err)
	}
	engine.config.Criu, engine.config.CriuSHA256 = "/out/criu", os.Getenv("FACTORYBOX_CRIU_SHA256")
	engine.config.ManagerRoot = "/var/lib/sysbox/manager"
	ctx := context.Background()
	source, err := engine.Start(ctx, "mount-source", bundle)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		group, err := captureGuardOpen(cgroupRoot + "/" + source.Record.RuntimeID)
		if err != nil {
			t.Error(err)
			return
		}
		events, err := captureGuardEvents(group)
		group.Close()
		if err != nil || events["frozen"] != 0 {
			t.Log("retain failed mount source for exact outer archive", err)
			return
		}
		if _, err := engine.Delete(ctx, "mount-source"); err != nil {
			t.Error(err)
		}
	})
	var before mountFDActorState
	deadline := time.Now().Add(5 * time.Second)
	for {
		before, err = readMountFDActor(source.Runtime.PID, "get")
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			log, _ := os.ReadFile(bundle + "/rootfs/mount-fd-actor.log")
			t.Fatalf("detached mount actor unavailable: %v: %s", err, log)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if before.Visible || before.MountID <= 0 || before.FSType != unix.PROC_SUPER_MAGIC ||
		before.Flags&unix.O_PATH == 0 || before.SelfPID != 1 || len(before.Payload) != 8192 ||
		before.SharedMountID != before.MountID || before.OtherMountID == before.MountID ||
		before.HasUptime != (mode == "default") {
		t.Fatal("actor lacks the expected detached proc descriptor, sharing or mount options")
	}
	if mode == "restricted" && (!strings.Contains(before.ProcOptions, "hidepid=ptraceable") ||
		!strings.Contains(before.ProcOptions, "subset=pid")) {
		t.Fatal("restricted proc mount lacks the actual kernel security options")
	}
	t.Logf("live detached proc root fd=%d mount=%d absent from mountinfo; flags=%#o inode=%d self=%d",
		before.FD, before.MountID, before.Flags, before.RootInode, before.SelfPID)
	snapshot := strings.Repeat("d", 32)
	started := time.Now()
	_, checkpointErr := engine.Checkpoint(ctx, "mount-source", snapshot)
	generation := engine.config.Root + "/objects/mount-source/captures/" + snapshot
	logs, _ := filepath.Glob(generation + "/*/dump.log")
	for _, path := range logs {
		if data, err := os.ReadFile(path); err == nil {
			if err := os.WriteFile("/output/mount-fd-dump.log", data, 0600); err != nil {
				t.Error(err)
			}
		}
	}
	if checkpointErr != nil {
		t.Fatalf("detached proc root checkpoint after %s: %v", time.Since(started), checkpointErr)
	}
	branch, err := forkNativeCLI(engine, "mount-source", snapshot, "mount-fork")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := engine.Delete(ctx, "mount-fork"); err != nil {
			t.Error(err)
		}
	})
	after, err := readMountFDActor(branch.Runtime.PID, "get")
	if err != nil {
		t.Fatal("restored detached proc descriptor unusable", err)
	}
	if after.Visible || after.MountID == before.MountID || after.FD != before.FD ||
		after.FSType != before.FSType || after.SelfPID != before.SelfPID || after.Flags != before.Flags ||
		after.Counter != before.Counter || !reflect.DeepEqual(after.Payload, before.Payload) ||
		after.RootInode != before.RootInode || after.MountFlags != before.MountFlags ||
		after.ProcOptions != before.ProcOptions ||
		after.HasUptime != before.HasUptime || after.SharedFD != before.SharedFD || after.OtherFD != before.OtherFD ||
		after.SharedMountID != after.MountID || after.OtherMountID == after.MountID ||
		after.OtherMountID == before.OtherMountID {
		t.Fatal("restored detached descriptor or RAM changed", before.MountID, after.MountID)
	}
	advanced, err := readMountFDActor(branch.Runtime.PID, "advance")
	if err != nil || advanced.Counter != before.Counter+1 {
		t.Fatal("fork did not continue its detached-mount actor", err)
	}
	retained, err := readMountFDActor(source.Runtime.PID, "get")
	if err != nil || !reflect.DeepEqual(before, retained) {
		t.Fatal("fork changed the retained source mount or RAM", err)
	}
	// runc retains a readable thread-self/fd directory inside its private
	// proc mount. A fork must remain capturable after it opens this handle.
	withDirectory, err := readMountFDActor(branch.Runtime.PID, "open-self-fds")
	if err != nil || withDirectory.DirectoryFD <= 2 || withDirectory.DirectoryMountID != withDirectory.MountID ||
		withDirectory.DirectoryFlags&unix.O_DIRECTORY == 0 || withDirectory.DirectoryFlags&unix.O_PATH != 0 ||
		withDirectory.DirectoryTarget != "/" {
		t.Fatal("fork lacks the real runc-style readable proc descriptor", err, withDirectory.DirectoryTarget)
	}
	descendantSnapshot := strings.Repeat("e", 32)
	if _, err := engine.Checkpoint(ctx, "mount-fork", descendantSnapshot); err != nil {
		t.Fatal("recapture fork with readable directory in detached proc", err)
	}
	// Keep the old mount object alive only in the test process. Otherwise
	// its numeric mount ID can be recycled after deleting the parent.
	// The restorer never receives this handle; the unchanged different-ID
	// assertion below proves separation while both mount objects exist.
	parentFD, err := unix.PidfdOpen(branch.Runtime.PID, 0)
	if err != nil {
		t.Fatal(err)
	}
	originalMount, err := unix.PidfdGetfd(parentFD, withDirectory.FD, 0)
	unix.Close(parentFD)
	if err != nil {
		t.Fatal("pin only the original captured mount identity", err)
	}
	defer unix.Close(originalMount)
	// Keep the two-sandbox ceiling and prove restore without the captured parent.
	if _, err := engine.Delete(ctx, "mount-fork"); err != nil {
		t.Fatal(err)
	}
	descendant, err := forkNativeCLI(engine, "mount-fork", descendantSnapshot, "mount-descendant")
	if err != nil {
		t.Fatal("restore readable proc descriptor without original process", err)
	}
	t.Cleanup(func() {
		if _, err := engine.Delete(ctx, "mount-descendant"); err != nil {
			t.Error(err)
		}
	})
	restoredDirectory, err := readMountFDActor(descendant.Runtime.PID, "get")
	if err != nil || restoredDirectory.DirectoryFD != withDirectory.DirectoryFD ||
		restoredDirectory.DirectoryFlags != withDirectory.DirectoryFlags ||
		restoredDirectory.DirectoryTarget != "/" ||
		restoredDirectory.DirectoryMountID != restoredDirectory.MountID ||
		restoredDirectory.MountID == withDirectory.MountID ||
		restoredDirectory.ProcOptions != withDirectory.ProcOptions ||
		restoredDirectory.Counter != withDirectory.Counter ||
		!reflect.DeepEqual(restoredDirectory.Payload, withDirectory.Payload) {
		beforeDetails, afterDetails := withDirectory, restoredDirectory
		beforeDetails.Payload, afterDetails.Payload = nil, nil
		t.Fatal("descendant lost independent proc descriptor or RAM", err, beforeDetails, afterDetails)
	}
}
