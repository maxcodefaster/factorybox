//go:build linux

package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestInterruptedCaptureNative(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_INTERRUPTION") != "1" {
		t.Skip("requires the owned real-CRIU interruption fixture")
	}
	engine, bundle := nativeEngine(t, "/bin/env FACTORYBOX_CAPTURE_ACTOR=1 FACTORYBOX_CAPTURE_BALLAST=1 /bin/capture-actor -test.run=^TestCaptureActor$ -test.timeout=0")
	binary, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundle+"/lower/bin/capture-actor", binary, 0755); err != nil {
		t.Fatal(err)
	}
	engine.config.Criu, engine.config.CriuSHA256 = "/out/criu", os.Getenv("FACTORYBOX_CRIU_SHA256")
	engine.config.ManagerRoot = "/var/lib/sysbox/manager"
	ctx := context.Background()
	source, err := engine.Start(ctx, "interrupted", bundle)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := engine.Delete(ctx, "interrupted"); err != nil {
			t.Error("delete exact interrupted fixture", err)
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	var before captureActorState
	for {
		before, err = readCaptureActor(source.Runtime.PID, "get")
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("interruption actor is not ready", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	network, err := os.Open(fmt.Sprintf("/proc/%d/ns/net", source.Runtime.PID))
	if err != nil {
		t.Fatal(err)
	}
	defer network.Close()
	privateNetwork, err := network.Stat()
	workerNetwork, workerErr := os.Stat("/proc/self/ns/net")
	if err != nil || workerErr != nil || os.SameFile(privateNetwork, workerNetwork) {
		t.Fatal("interruption requires the source's separate private network namespace", err, workerErr)
	}
	loopback := exec.Command("nsenter", "--net=/proc/self/fd/3", "--", "ip", "link", "set", "dev", "lo", "up")
	loopback.ExtraFiles = []*os.File{network}
	if output, err := loopback.CombinedOutput(); err != nil {
		t.Fatalf("prepare only the private source loopback: %v: %s", err, output)
	}
	before, err = readCaptureActor(source.Runtime.PID, "make-tcp-pair")
	if err != nil || before.TCPClient <= 2 || before.TCPServer <= 2 || before.TCPListener <= 2 {
		t.Fatal("prepare real established TCP sockets", err)
	}
	type socketState struct {
		FD     int    `json:"fd"`
		Cookie uint64 `json:"cookie"`
		Repair int    `json:"repair"`
		Mark   int    `json:"mark"`
	}
	sockets := func() ([]socketState, error) {
		pidfd, err := unix.PidfdOpen(source.Runtime.PID, 0)
		if err != nil {
			return nil, err
		}
		defer unix.Close(pidfd)
		var result []socketState
		for _, original := range []int{before.TCPClient, before.TCPServer, before.TCPListener} {
			fd, err := unix.PidfdGetfd(pidfd, original, 0)
			if err != nil {
				return nil, err
			}
			cookie, cookieErr := unix.GetsockoptUint64(fd, unix.SOL_SOCKET, unix.SO_COOKIE)
			repair, repairErr := unix.GetsockoptInt(fd, unix.IPPROTO_TCP, unix.TCP_REPAIR)
			mark, markErr := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_MARK)
			closeErr := unix.Close(fd)
			if err := errors.Join(cookieErr, repairErr, markErr, closeErr); err != nil {
				return nil, err
			}
			result = append(result, socketState{original, cookie, repair, mark})
		}
		return result, nil
	}
	committed := strings.Repeat("b", 32)
	if _, err := engine.Checkpoint(ctx, "interrupted", committed); err != nil {
		t.Fatal("create the committed recovery point before fault injection", err)
	}
	beforeSockets, err := sockets()
	if err != nil {
		t.Fatal("observe original TCP socket properties", err)
	}
	exchange := func() error {
		pidfd, err := unix.PidfdOpen(source.Runtime.PID, 0)
		if err != nil {
			return err
		}
		defer unix.Close(pidfd)
		client, err := unix.PidfdGetfd(pidfd, before.TCPClient, 0)
		if err != nil {
			return err
		}
		defer unix.Close(client)
		server, err := unix.PidfdGetfd(pidfd, before.TCPServer, 0)
		if err != nil {
			return err
		}
		defer unix.Close(server)
		message := []byte("owned-socket-round-trip\n")
		transfer := func(send, receive int) error {
			n, err := unix.SendmsgN(send, message, nil, nil, unix.MSG_DONTWAIT|unix.MSG_NOSIGNAL)
			if err != nil || n != len(message) {
				return fmt.Errorf("send original socket probe: %d: %w", n, err)
			}
			deadline := time.Now().Add(time.Second)
			var received []byte
			for len(received) < len(message) {
				var buffer [64]byte
				n, _, err := unix.Recvfrom(receive, buffer[:], unix.MSG_DONTWAIT)
				if err != nil && !errors.Is(err, unix.EAGAIN) {
					return err
				}
				if errors.Is(err, unix.EAGAIN) {
					n = 0
				}
				received = append(received, buffer[:n]...)
				if time.Now().After(deadline) {
					return fmt.Errorf("original socket probe timed out")
				}
				if n == 0 {
					time.Sleep(time.Millisecond)
				}
			}
			if !bytes.Equal(received, message) {
				return fmt.Errorf("original socket changed probe data")
			}
			return nil
		}
		return errors.Join(transfer(client, server), transfer(server, client))
	}
	if err := exchange(); err != nil {
		t.Fatal("committed TCP fixture is not usable before interruption", err)
	}
	networkRules := func() ([]byte, error) {
		command := exec.Command("nsenter", "--net=/proc/self/fd/3", "--", "iptables-save")
		command.ExtraFiles = []*os.File{network}
		data, err := command.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("read only original private network rules: %w", err)
		}
		var rules []string
		for _, line := range strings.Split(string(data), "\n") {
			if line != "" && !strings.HasPrefix(line, "#") {
				rules = append(rules, line)
			}
		}
		return []byte(regexp.MustCompile(`\[[0-9]+:[0-9]+\]`).ReplaceAllString(strings.Join(rules, "\n"), "[counters]")), nil
	}
	beforeNetwork, err := networkRules()
	if err != nil {
		t.Fatal(err)
	}
	snapshot := strings.Repeat("c", 32)
	relative, _ := checkpointPath("interrupted", snapshot)
	command := exec.Command("/factorybox", "--root", engine.config.Root,
		"--runtime", engine.config.Runtime, "--runtime-sha256", engine.config.RuntimeSHA256,
		"--criu", engine.config.Criu, "--criu-sha256", engine.config.CriuSHA256,
		"--manager-root", engine.config.ManagerRoot,
		"checkpoint", "--snapshot-id", snapshot, "interrupted")
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	defer func() {
		if !waited {
			command.Process.Kill()
			command.Wait()
		}
	}()
	deadline = time.Now().Add(15 * time.Second)
	var worker struct {
		Token      string `json:"token"`
		PID        int    `json:"worker_pid"`
		Start      string `json:"worker_start"`
		SourcePID  int    `json:"source_pid"`
		CriuSHA256 string `json:"criu_sha256"`
		Scope      string `json:"scope"`
	}
	var interruptedPID int
	for interruptedPID == 0 {
		if err := engine.readCheckpointJSON(relative+"/guard/capture-worker-intent.json", &worker); err == nil &&
			worker.Token == snapshot && worker.SourcePID == source.Runtime.PID &&
			worker.CriuSHA256 == engine.config.CriuSHA256 {
			birth, err := processBirth(worker.PID)
			if err == nil && birth == worker.Start {
				status, _ := os.ReadFile(fmt.Sprintf("/proc/%d/status", source.Runtime.PID))
				tracer := 0
				for _, line := range strings.Split(string(status), "\n") {
					fields := strings.Fields(line)
					if len(fields) == 2 && fields[0] == "TracerPid:" {
						tracer, _ = strconv.Atoi(fields[1])
					}
				}
				pagesWritten := false
				images, _ := os.ReadDir(filepath.Join(engine.config.Root, relative, "images"))
				for _, image := range images {
					if strings.HasPrefix(image.Name(), "pages-") && strings.HasSuffix(image.Name(), ".img") {
						if info, err := image.Info(); err == nil && info.Size() >= 8<<20 {
							pagesWritten = true
						}
					}
				}
				if tracer > 1 && pagesWritten {
					fd, err := unix.PidfdOpen(tracer, 0)
					if err == nil {
						executable, statErr := os.Stat(fmt.Sprintf("/proc/%d/exe", tracer))
						expected, expectedErr := os.Stat(engine.config.Criu)
						processStatus, statusErr := os.ReadFile(fmt.Sprintf("/proc/%d/status", tracer))
						ownedParent := false
						for _, line := range strings.Split(string(processStatus), "\n") {
							fields := strings.Fields(line)
							if len(fields) == 2 && fields[0] == "PPid:" {
								ownedParent = fields[1] == strconv.Itoa(worker.PID)
							}
						}
						if statErr == nil && expectedErr == nil && statusErr == nil &&
							os.SameFile(executable, expected) && ownedParent {
							err = unix.PidfdSendSignal(fd, unix.SIGKILL, nil, 0)
							if err == nil {
								interruptedPID = tracer
							}
						}
						unix.Close(fd)
					}
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("did not observe the pinned CRIU child actively tracing the original source")
		}
		if interruptedPID == 0 {
			time.Sleep(time.Millisecond)
		}
	}
	t.Logf("interrupted pinned CRIU PID %d after at least 8 MiB of image data while tracing original source PID %d", interruptedPID, source.Runtime.PID)
	if err := command.Wait(); err == nil {
		t.Fatal("interrupted CRIU published a successful checkpoint")
	}
	waited = true
	if _, err := os.Stat(filepath.Join(engine.config.Root, relative, "checkpoint.json")); !os.IsNotExist(err) {
		t.Fatal("interrupted capture published complete backing", err)
	}
	for _, name := range []string{"capture-process-complete.json", "capture-abort-intent.json", "guard-capture-aborted.json"} {
		if _, err := os.Stat(filepath.Join(engine.config.Root, relative, "guard", name)); !os.IsNotExist(err) {
			t.Fatal("interrupted CRIU gained a process-completion or safe-abort receipt", name, err)
		}
	}
	afterNetwork, networkErr := networkRules()
	for name, data := range map[string][]byte{
		"interruption-network-before.txt": beforeNetwork,
		"interruption-network-after.txt":  afterNetwork,
	} {
		if err := os.WriteFile("/output/"+name, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if networkErr != nil || !bytes.Equal(beforeNetwork, afterNetwork) {
		t.Errorf("interrupted capture did not restore its original private network rules: %v", networkErr)
	}
	afterSockets, socketErr := sockets()
	socketData, marshalErr := json.Marshal(map[string]any{"before": beforeSockets, "after": afterSockets})
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	if err := os.WriteFile("/output/interruption-sockets.json", append(socketData, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if socketErr != nil || !reflect.DeepEqual(beforeSockets, afterSockets) {
		t.Errorf("interrupted capture changed original socket identity or properties: %v; before=%+v after=%+v",
			socketErr, beforeSockets, afterSockets)
	}
	if err := exchange(); err != nil {
		t.Error("rolled-back original TCP sockets are not bidirectionally usable", err)
	}
	recovered, recoverErr := forkNativeCLI(engine, "interrupted", committed, "committed-recovery")
	if recoverErr != nil {
		t.Fatal("interruption also made the previous committed recovery point unusable", recoverErr)
	}
	t.Cleanup(func() {
		if _, err := engine.Delete(ctx, "committed-recovery"); err != nil {
			t.Error("delete exact committed recovery target", err)
		}
	})
	recoveredRAM, recoverErr := readCaptureActor(recovered.Runtime.PID, "get")
	if recoverErr != nil || !reflect.DeepEqual(before, recoveredRAM) {
		t.Fatal("previous committed checkpoint lost its complete independent RAM", recoverErr)
	}
	if _, err := readCaptureActor(recovered.Runtime.PID, "advance"); err != nil {
		t.Fatal("previous committed checkpoint cannot continue useful work", err)
	}
	t.Log("previous committed checkpoint remained independently usable after actual CRIU interruption")
	state, err := engine.Inspect(ctx, "interrupted")
	if err != nil || state.Runtime == nil || state.Runtime.Status != "running" || state.Runtime.PID != source.Runtime.PID {
		t.Fatalf("interrupted capture did not recover its original running source: %v; state=%+v; stderr=%s",
			err, state.Runtime, stderr.String())
	}
	after, err := readCaptureActor(source.Runtime.PID, "get")
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("interruption changed original private RAM", err)
	}
	if _, err := readCaptureActor(source.Runtime.PID, "advance"); err != nil {
		t.Fatal("interrupted source cannot continue useful work", err)
	}
}
