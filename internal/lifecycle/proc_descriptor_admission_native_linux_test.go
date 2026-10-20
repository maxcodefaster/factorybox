//go:build linux

package lifecycle

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestProcDirectoryAdmissionNative(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_FILESYSTEM_ADMISSION") != "1" {
		t.Skip("requires the owned pre-capture descriptor fixture")
	}
	for index, transient := range []bool{false, true} {
		name := "held-foreign-proc-directory"
		if transient {
			name = "transient-foreign-proc-directory"
		}
		if !t.Run(name, func(t *testing.T) {
			engine, bundle := nativeEngine(t, "/bin/env FACTORYBOX_MOUNT_FD_ACTOR=default "+
				"/bin/mount-fd-actor -test.run=^TestMountFDActor$ -test.timeout=0")
			binary, err := os.ReadFile(os.Args[0])
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(bundle+"/lower/bin/mount-fd-actor", binary, 0755); err != nil {
				t.Fatal("prepare owned proc actor", err)
			}
			engine.config.Criu, engine.config.CriuSHA256 = "/out/criu", os.Getenv("FACTORYBOX_CRIU_SHA256")
			engine.config.ManagerRoot = "/var/lib/sysbox/manager"
			source, err := engine.Start(context.Background(), "proc-descriptor", bundle)
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
					t.Log("unproved proc source retained for owned outer archival", err)
					return
				}
				if _, err := engine.Delete(context.Background(), source.Record.ID); err != nil {
					t.Error(err)
				}
			})
			var initial mountFDActorState
			deadline := time.Now().Add(5 * time.Second)
			for {
				initial, err = readMountFDActor(source.Runtime.PID, "get")
				if err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("proc actor did not become ready", err)
				}
				time.Sleep(5 * time.Millisecond)
			}
			request := "open-foreign-fds"
			if transient {
				request = "open-transient-foreign-fds"
			}
			opened, err := readMountFDActor(source.Runtime.PID, request)
			if err != nil || opened.DirectoryFD <= 2 || opened.Visible {
				t.Fatal("prepare the real detached foreign proc directory", err)
			}
			snapshot := strings.Repeat(fmt.Sprint(index+3), 32)
			relative, _ := checkpointPath(source.Record.ID, snapshot)
			command := func() error {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, "/factorybox", "--root", engine.config.Root,
					"--runtime", engine.config.Runtime, "--runtime-sha256", engine.config.RuntimeSHA256,
					"--criu", engine.config.Criu, "--criu-sha256", engine.config.CriuSHA256,
					"--manager-root", engine.config.ManagerRoot,
					"checkpoint", "--snapshot-id", snapshot, source.Record.ID)
				output, err := cmd.CombinedOutput()
				if writeErr := os.WriteFile(fmt.Sprintf("/output/proc-admission-%d-cli.txt", index), output, 0600); writeErr != nil {
					return writeErr
				}
				if transient {
					if err != nil {
						return fmt.Errorf("proc transaction did not settle before capture: %w: %s", err, output)
					}
				} else if err == nil || !strings.Contains(string(output), "detached-proc") {
					return fmt.Errorf("held proc directory was accepted or failed elsewhere: %v: %s", err, output)
				}
				return nil
			}
			if err := command(); err != nil {
				t.Fatal(err)
			}
			view, err := engine.Inspect(context.Background(), source.Record.ID)
			if err != nil || view.Runtime == nil || view.Runtime.PID != source.Runtime.PID ||
				view.Runtime.Created != source.Runtime.Created || view.Runtime.Status != "running" {
				t.Fatal("proc admission lost its original running source", err)
			}
			if !transient {
				for _, name := range []string{"images", "checkpoint.json", "guard/guard-capture-entered.json"} {
					if _, err := os.Stat(filepath.Join(engine.config.Root, relative, name)); !os.IsNotExist(err) {
						t.Fatal("unsupported proc directory entered CRIU", name, err)
					}
				}
				if err := command(); err != nil {
					t.Fatal("proc rejection did not replay its immutable outcome", err)
				}
				again, err := readMountFDActor(source.Runtime.PID, "get")
				if err != nil || !reflect.DeepEqual(again, opened) {
					t.Fatal("proc rejection changed original RAM or descriptors", err)
				}
				if _, err := readMountFDActor(source.Runtime.PID, "close-directory"); err != nil {
					t.Fatal(err)
				}
				snapshot = strings.Repeat("6", 32)
				if _, err := engine.Checkpoint(context.Background(), source.Record.ID, snapshot); err != nil {
					t.Fatal("source cannot capture after the unsupported directory closes", err)
				}
			} else {
				var evidence captureDescriptorAdmission
				err := engine.readCheckpointJSON(relative+"/guard/descriptor-admission.json", &evidence)
				if err != nil || !evidence.Passed || len(evidence.First) == 0 || evidence.Attempts < 2 {
					t.Fatal("transient proc proof did not observe its actual blocker", err, evidence)
				}
			}
			current, err := readMountFDActor(source.Runtime.PID, "get")
			if err != nil || !reflect.DeepEqual(initial, current) {
				t.Fatal("settled proc actor changed its original RAM", err)
			}
			fork, err := forkNativeCLI(engine, source.Record.ID, snapshot, "proc-fork")
			if err != nil {
				t.Fatal("fork after proc admission", err)
			}
			t.Cleanup(func() {
				if _, err := engine.Delete(context.Background(), "proc-fork"); err != nil {
					t.Error(err)
				}
			})
			forked, err := readMountFDActor(fork.Runtime.PID, "get")
			same := func(after mountFDActorState) bool {
				if after.MountID == initial.MountID || after.OtherMountID == initial.OtherMountID ||
					after.SharedMountID != after.MountID || after.OtherMountID == after.MountID {
					return false
				}
				expected := initial
				expected.MountID, expected.SharedMountID, expected.OtherMountID =
					after.MountID, after.SharedMountID, after.OtherMountID
				return reflect.DeepEqual(expected, after)
			}
			if err != nil || !same(forked) {
				t.Fatal("proc admission lost its independent fork", err)
			}
			if _, err := readMountFDActor(source.Runtime.PID, "advance"); err != nil {
				t.Fatal(err)
			}
			forked, err = readMountFDActor(fork.Runtime.PID, "get")
			if err != nil || !same(forked) {
				t.Fatal("settled proc fork shares mutable source RAM", err)
			}
			t.Log("original RAM, rejection replay, settled capture and independent fork passed")
		}) {
			return
		}
	}
}
