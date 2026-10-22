//go:build linux && factorybox_worker_probe

package lifecycle

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func recoverNativeCLI(engine *Engine, request RecoveryRequest, target string) (View, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := recoverProbeCommand(ctx, engine, request, target)
	command.WaitDelay = 2 * time.Second
	output, err := command.CombinedOutput()
	var view View
	if err != nil {
		return view, fmt.Errorf("public recovery: %w: %s", err, output)
	}
	err = json.Unmarshal(output, &view)
	return view, err
}

func recoverProbeCommand(ctx context.Context, engine *Engine, request RecoveryRequest, target string) *exec.Cmd {
	options := []string{"--root", engine.config.Root,
		"--runtime", engine.config.Runtime, "--runtime-sha256", engine.config.RuntimeSHA256,
		"--criu", engine.config.Criu, "--criu-sha256", engine.config.CriuSHA256,
		"--manager-root", engine.config.ManagerRoot}
	if os.Getenv("FACTORYBOX_NATIVE_WORKER_SUPERVISOR") == "1" {
		options = []string{"--worker-run-root", "/run/factorybox"}
	}
	options = append(options, "recover-paused", "--source", request.SourceID,
		"--snapshot-id", request.SnapshotID, "--source-root", request.SourceRoot,
		"--source-owner", request.SourceOwner, "--checkpoint-sha256", request.CheckpointSHA256, target)
	return exec.CommandContext(ctx, "/factorybox", options...)
}

func checkRecoveryRejectionsNative(t *testing.T, fresh, old *Engine, generation string, saved Checkpoint, request RecoveryRequest) {
	t.Helper()
	t.Run("reject-foreign-owner", func(t *testing.T) {
		wrong := request
		wrong.SourceOwner = strings.Repeat("0", 32)
		if _, err := recoverNativeCLI(fresh, wrong, "cold-foreign"); err == nil {
			t.Fatal("foreign source owner was accepted")
		}
	})
	t.Run("reject-wrong-checkpoint", func(t *testing.T) {
		wrong := request
		wrong.CheckpointSHA256 = strings.Repeat("0", 64)
		if _, err := recoverNativeCLI(fresh, wrong, "cold-wrong-generation"); err == nil {
			t.Fatal("foreign checkpoint digest was accepted")
		}
	})
	t.Run("reject-missing-publication", func(t *testing.T) {
		path := old.config.Root + "/" + generation + "/checkpoint.json"
		if err := os.Rename(path, path+".withheld"); err != nil {
			t.Fatal(err)
		}
		_, rejected := recoverNativeCLI(fresh, request, "cold-unpublished")
		if err := os.Rename(path+".withheld", path); err != nil {
			t.Fatal(err)
		}
		if rejected == nil {
			t.Fatal("unpublished checkpoint was accepted")
		}
	})
	t.Run("reject-corrupt-ram", func(t *testing.T) {
		for _, file := range saved.Files {
			if strings.HasPrefix(file.Name, "images/pages-") && file.Bytes > 0 {
				path := old.config.Root + "/" + generation + "/" + file.Name
				original, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				bad := append([]byte(nil), original...)
				bad[len(bad)/2] ^= 1
				if err := os.WriteFile(path, bad, 0600); err != nil {
					t.Fatal(err)
				}
				_, rejected := recoverNativeCLI(fresh, request, "cold-corrupt-ram")
				if err := os.WriteFile(path, original, 0600); err != nil {
					t.Fatal(err)
				}
				if rejected == nil {
					t.Fatal("corrupt anonymous RAM image was accepted")
				}
				return
			}
		}
		t.Fatal("complete anonymous RAM image was not found")
	})
	t.Run("reject-partial-import-replay", func(t *testing.T) {
		if err := fresh.root.Mkdir("forks/cold-partial", 0700); err != nil {
			t.Fatal(err)
		}
		for attempt := 0; attempt < 2; attempt++ {
			if _, err := recoverNativeCLI(fresh, request, "cold-partial"); err == nil {
				t.Fatal("partial import was silently replaced")
			}
		}
	})
	t.Run("kill-import-copy", func(t *testing.T) {
		var payload string
		for _, file := range saved.Files {
			if strings.HasPrefix(file.Name, "images/pages-") && file.Bytes > 0 {
				payload = old.config.Root + "/" + generation + "/" + file.Name
				break
			}
		}
		if payload == "" {
			t.Fatal("RAM copy input absent")
		}
		fan, err := unix.FanotifyInit(unix.FAN_CLASS_CONTENT|unix.FAN_CLOEXEC|unix.FAN_NONBLOCK, unix.O_RDONLY|unix.O_CLOEXEC)
		if err != nil {
			t.Fatal(err)
		}
		defer unix.Close(fan)
		if err := unix.FanotifyMark(fan, unix.FAN_MARK_ADD, unix.FAN_OPEN_PERM, unix.AT_FDCWD, payload); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		command := recoverProbeCommand(ctx, fresh, request, "cold-killed-copy")
		command.SysProcAttr = &unix.SysProcAttr{Setpgid: true}
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		defer func() { command.Process.Kill(); command.Wait() }()
		killed := false
		for !killed && ctx.Err() == nil {
			poll := []unix.PollFd{{Fd: int32(fan), Events: unix.POLLIN}}
			count, err := unix.Poll(poll, 1000)
			if err != nil {
				t.Fatal(err)
			}
			if count == 0 {
				continue
			}
			buffer := make([]byte, 4096)
			n, err := unix.Read(fan, buffer)
			if err != nil || n < unix.FAN_EVENT_METADATA_LEN {
				t.Fatal("invalid copy permission event", n, err)
			}
			for offset := 0; offset < n; {
				event := buffer[offset:]
				length := int(binary.NativeEndian.Uint32(event))
				if length < unix.FAN_EVENT_METADATA_LEN || offset+length > n || event[4] != unix.FANOTIFY_METADATA_VERSION {
					t.Fatal("invalid event layout")
				}
				fd := int(int32(binary.NativeEndian.Uint32(event[16:])))
				pid := int(int32(binary.NativeEndian.Uint32(event[20:])))
				if fd < 0 {
					t.Fatal("permission event omitted its file")
				}
				comm, _ := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/comm")
				if strings.TrimSpace(string(comm)) == "cp" {
					stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
					if err != nil {
						t.Fatal(err)
					}
					fields := strings.Fields(string(stat)[strings.LastIndex(string(stat), ")")+1:])
					if len(fields) < 2 || fields[1] != strconv.Itoa(command.Process.Pid) {
						t.Fatal("copy helper is outside our recovery process")
					}
					if err := unix.Kill(-command.Process.Pid, unix.SIGKILL); err != nil {
						t.Fatal(err)
					}
					killed = true
				}
				response := make([]byte, 8)
				binary.NativeEndian.PutUint32(response, uint32(fd))
				binary.NativeEndian.PutUint32(response[4:], unix.FAN_ALLOW)
				_, writeErr := unix.Write(fan, response)
				unix.Close(fd)
				if writeErr != nil {
					t.Fatal(writeErr)
				}
				offset += length
			}
		}
		if !killed {
			t.Fatal("did not interrupt the actual RAM copy helper")
		}
		if err := command.Wait(); err == nil {
			t.Fatal("killed import returned success")
		}
		for deadline := time.Now().Add(3 * time.Second); ; {
			lock, err := fresh.lock()
			if err == nil {
				lock.Close()
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("killed helper retained controller ownership", err)
			}
			time.Sleep(10 * time.Millisecond)
		}
		if _, err := recoverNativeCLI(fresh, request, "cold-killed-copy"); err == nil {
			t.Fatal("killed copy was silently repeated")
		}
		if _, err := fresh.root.Lstat("objects/cold-killed-copy"); !os.IsNotExist(err) {
			t.Fatal("interrupted import created a runtime target", err)
		}
	})
}
