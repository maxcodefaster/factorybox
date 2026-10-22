//go:build linux

package lifecycle

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func assertSocketForkNative(t *testing.T, engine *Engine, source View, snapshot, target string, expected captureActorState) {
	t.Helper()
	view, err := forkNativeCLI(engine, source.Record.ID, snapshot, target)
	if err != nil || view.Runtime == nil || view.Runtime.PID <= 1 || view.Runtime.ID == source.Runtime.ID {
		t.Fatal("independent socket admission fork", err, view)
	}
	t.Cleanup(func() {
		if _, err := engine.Delete(context.Background(), target); err != nil {
			t.Error(err)
		}
	})
	state, err := readCaptureActor(view.Runtime.PID, "get")
	if err != nil || !reflect.DeepEqual(state, expected) {
		t.Fatal("socket fork lost exact captured RAM", err)
	}
	before, err := readCaptureActor(source.Runtime.PID, "get")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := readCaptureActor(view.Runtime.PID, "advance"); err != nil {
		t.Fatal(err)
	}
	after, err := readCaptureActor(source.Runtime.PID, "get")
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("socket fork shares mutable source RAM", err)
	}
	if err := os.WriteFile(fmt.Sprintf("/proc/%d/root/state/socket-marker", view.Runtime.PID), []byte("branch"), 0600); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/root/state/socket-marker", source.Runtime.PID))
	if err != nil || string(data) != "source" {
		t.Fatal("socket fork shares mutable source files", err)
	}
}

func TestCaptureSocketDescriptorNative(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_FILESYSTEM_ADMISSION") != "1" {
		t.Skip("requires the owned native admission fixture")
	}
	listener, err := unix.Socket(unix.AF_INET, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, unix.IPPROTO_TCP)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(listener)
	if err := unix.Bind(listener, &unix.SockaddrInet4{Addr: [4]byte{127, 0, 0, 1}}); err != nil {
		t.Fatal(err)
	}
	if err := unix.Listen(listener, 1); err != nil {
		t.Fatal(err)
	}
	t.Run("empty-listener", func(t *testing.T) {
		if kind, err := captureSocketBlocker(os.Getpid(), listener); err != nil || kind != "" {
			t.Fatal("ordinary empty listener rejected", kind, err)
		}
	})
	t.Run("real-accept-queue", func(t *testing.T) {
		client, err := unix.Socket(unix.AF_INET, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, unix.IPPROTO_TCP)
		if err != nil {
			t.Fatal(err)
		}
		defer unix.Close(client)
		address, err := unix.Getsockname(listener)
		if err != nil {
			t.Fatal(err)
		}
		if err := unix.Connect(client, address); err != nil {
			t.Fatal(err)
		}
		if _, err := unix.Write(client, []byte("queued")); err != nil {
			t.Fatal(err)
		}
		if kind, err := captureSocketBlocker(os.Getpid(), listener); err != nil || kind != "tcp-accept-queue" {
			t.Fatal("actual pending connection was admitted", kind, err)
		}
		accepted, _, err := unix.Accept4(listener, unix.SOCK_CLOEXEC)
		if err != nil {
			t.Fatal(err)
		}
		defer unix.Close(accepted)
		data := make([]byte, 6)
		if n, err := unix.Read(accepted, data); err != nil || n != len(data) || string(data) != "queued" {
			t.Fatal("admission changed pending connection data", n, err)
		}
		if kind, err := captureSocketBlocker(os.Getpid(), listener); err != nil || kind != "" {
			t.Fatal("accepted connection did not settle", kind, err)
		}
	})
	t.Run("unsupported-mptcp", func(t *testing.T) {
		fd, err := unix.Socket(unix.AF_INET, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 262)
		if err != nil {
			t.Fatal("this native worker must reproduce its actual MPTCP socket", err)
		}
		defer unix.Close(fd)
		if kind, err := captureSocketBlocker(os.Getpid(), fd); err != nil || kind != "mptcp" {
			t.Fatal("unsupported MPTCP descriptor was admitted", kind, err)
		}
	})
}

func TestSocketQueueAdmissionNative(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_FILESYSTEM_ADMISSION") != "1" {
		t.Skip("requires the owned real-Sysbox admission fixture")
	}
	for _, scenario := range []struct {
		name             string
		child, transient bool
	}{
		{"held-root-checkpoint", false, false},
		{"held-child-pause", true, false},
		{"transient-checkpoint", false, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
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
			source, err := engine.Start(ctx, "socket", bundle)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if _, err := engine.Delete(ctx, "socket"); err != nil {
					t.Error("delete exact socket source", err)
				}
			})
			deadline := time.Now().Add(5 * time.Second)
			var initial captureActorState
			for {
				initial, err = readCaptureActor(source.Runtime.PID, "get")
				if err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("socket actor readiness", err)
				}
				time.Sleep(5 * time.Millisecond)
			}
			statePath := fmt.Sprintf("/proc/%d/root/state", source.Runtime.PID)
			var owner unix.Stat_t
			if err := unix.Stat(statePath, &owner); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(statePath+"/socket-marker", []byte("source"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chown(statePath+"/socket-marker", int(owner.Uid), int(owner.Gid)); err != nil {
				t.Fatal(err)
			}
			prior := strings.Repeat("a", 32)
			if _, err := engine.Checkpoint(ctx, "socket", prior); err != nil {
				t.Fatal(err)
			}
			directory := "/state"
			if scenario.child {
				if _, err := readCaptureActor(source.Runtime.PID, "make-memory-child"); err != nil {
					t.Fatal(err)
				}
				directory = "/child-state"
				deadline = time.Now().Add(5 * time.Second)
				for {
					if _, err := readCaptureActorAt(source.Runtime.PID, directory, "get"); err == nil {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("socket child readiness")
					}
					time.Sleep(5 * time.Millisecond)
				}
			}
			request := "make-tcp-pending"
			if scenario.transient {
				request = "make-transient-tcp-pending"
			}
			opened, err := readCaptureActorAt(source.Runtime.PID, directory, request)
			if err != nil || opened.TCPClient <= 2 || opened.TCPListener <= 2 || opened.TCPServer != 0 {
				t.Fatal("pending socket setup", err)
			}
			snapshot := strings.Repeat("b", 32)
			if scenario.child {
				_, err = pauseNativeCLI(engine, "socket", snapshot)
			} else {
				_, err = engine.Checkpoint(ctx, "socket", snapshot)
			}
			relative, _ := checkpointPath("socket", snapshot)
			if scenario.transient {
				if err != nil {
					t.Fatal("settled TCP checkpoint", err)
				}
				current, err := readCaptureActor(source.Runtime.PID, "get")
				if err != nil || current.TCPServer <= 2 {
					t.Fatal("source never accepted its connection", err)
				}
				opened.TCPServer = current.TCPServer
				if !reflect.DeepEqual(opened, current) {
					t.Fatal("settling changed original RAM")
				}
				var observation captureDescriptorAdmission
				if err := engine.readCheckpointJSON(relative+"/guard/descriptor-admission.json", &observation); err != nil || !observation.Passed || observation.Attempts < 2 || len(observation.First) == 0 || observation.First[0].Kind != "tcp-accept-queue" {
					t.Fatal("missing actual queue settlement evidence", err, observation)
				}
				assertSocketForkNative(t, engine, source, snapshot, "settled-tcp", current)
				return
			}
			if err == nil || !strings.Contains(err.Error(), "tcp-accept-queue") {
				t.Fatal("held TCP queue did not reject before CRIU", err)
			}
			view, err := engine.Inspect(ctx, "socket")
			if err != nil || view.Runtime == nil || view.Runtime.PID != source.Runtime.PID || view.Runtime.Created != source.Runtime.Created || view.Runtime.Status != "running" || view.Record.Phase != "running" {
				t.Fatal("queue rejection lost original running source", err, view)
			}
			current, err := readCaptureActorAt(source.Runtime.PID, directory, "get")
			if err != nil || !reflect.DeepEqual(opened, current) {
				t.Fatal("queue rejection changed original RAM/socket state", err)
			}
			for _, name := range []string{"images", "checkpoint.json", "capture-result.json", "guard/guard-capture-entered.json", "guard/capture-worker-intent.json"} {
				if _, err := os.Stat(filepath.Join(engine.config.Root, relative, name)); !os.IsNotExist(err) {
					t.Fatal("rejection entered capture or published backing", name, err)
				}
			}
			assertSocketForkNative(t, engine, source, prior, "prior-tcp", initial)
			if scenario.child && (view.Record.Pause != nil || len(view.Record.PauseHistory) != 1 || view.Record.PauseHistory[0].RejectedAt == "") {
				t.Fatal("queue rejection left unresolved pause")
			}
			if _, err := readCaptureActorAt(source.Runtime.PID, directory, "close-tcp-pair"); err != nil {
				t.Fatal(err)
			}
			if _, err := readCaptureActorAt(source.Runtime.PID, directory, "advance"); err != nil {
				t.Fatal("original source lost useful work", err)
			}
		})
	}
}
