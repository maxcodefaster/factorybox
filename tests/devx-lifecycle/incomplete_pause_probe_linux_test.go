//go:build linux && factorybox_worker_probe

package devx_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"factorybox/internal/lifecycle"
	"golang.org/x/sys/unix"
)

type interruptedConsumerPause struct {
	Record              lifecycle.Record `json:"record"`
	Snapshot            string           `json:"snapshot"`
	PriorMetadataSHA256 string           `json:"prior_metadata_sha256"`
	BlockedCopyPID      int              `json:"blocked_copy_pid"`
	CaptureWorkerPID    int              `json:"capture_worker_pid"`
	PayloadSHA256       string           `json:"payload_sha256"`
}

func consumerFileSHA(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) > 16<<20 {
		t.Fatal("read bounded owned proof file", err)
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func holdIncompleteConsumerPause(t *testing.T, f *fixture, proof *consumerPauseProof, path string) {
	t.Helper()
	branch := f.view("fork", "--source", "source", "--snapshot-id", proof.Paused.Pause.SnapshotID, "interrupted")
	if branch.Runtime == nil || branch.Runtime.PID <= 1 {
		t.Fatal("incomplete-pause source lacks its live owned identity")
	}
	if proof.Minimal != nil {
		if actual := minimalWorkerState(t, branch, "", 60*time.Second); !reflect.DeepEqual(actual, *proof.Minimal) {
			t.Fatal("earlier committed minimal state changed before interrupted pause")
		}
	} else {
		if actual := f.useful("before-incomplete-pause", branch, time.Now(), ""); !reflect.DeepEqual(actual, *proof.Factory) {
			t.Fatal("earlier committed Factory state changed before interrupted pause")
		}
	}
	payload := bytes.Repeat([]byte("factorybox-owned-incomplete-pause\n"), 2048)
	name := "/var/lib/factorybox-incomplete-pause"
	if err := os.WriteFile(fmt.Sprintf("/proc/%d/root%s", branch.Runtime.PID, name), payload, 0600); err != nil {
		t.Fatal(err)
	}
	// Capture copies the backing upper, rather than opening the merged overlay.
	backing := branch.Record.InputBundle + "/upper" + name
	payloadInfo, err := os.Stat(backing)
	if err != nil || consumerFileSHA(t, backing) != fmt.Sprintf("%x", sha256.Sum256(payload)) {
		t.Fatal("incomplete-pause payload is not the exact owned upper file", err)
	}
	metadata := proof.SourceRoot + "/objects/source/captures/" + proof.Paused.Pause.SnapshotID + "/checkpoint.json"
	proof.Interrupted = &interruptedConsumerPause{Record: branch.Record, Snapshot: strings.Repeat("d", 32),
		PriorMetadataSHA256: consumerFileSHA(t, metadata), PayloadSHA256: consumerFileSHA(t, backing)}
	fan, err := unix.FanotifyInit(unix.FAN_CLASS_CONTENT|unix.FAN_CLOEXEC|unix.FAN_NONBLOCK, unix.O_RDONLY|unix.O_CLOEXEC)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fan)
	if err := unix.FanotifyMark(fan, unix.FAN_MARK_ADD, unix.FAN_OPEN_PERM, unix.AT_FDCWD, backing); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/factorybox", "--worker-run-root", "/run/factorybox", "pause", "--snapshot-id", proof.Interrupted.Snapshot, "interrupted")
	output, err := os.OpenFile("/output/incomplete-pause-response.txt", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	command.Stdout, command.Stderr = output, output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer command.Process.Kill()
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	generation := f.root + "/objects/interrupted/captures/" + proof.Interrupted.Snapshot
	for ctx.Err() == nil {
		select {
		case err := <-done:
			t.Fatal("pause finished before its actual copy boundary", err)
		default:
		}
		poll := []unix.PollFd{{Fd: int32(fan), Events: unix.POLLIN}}
		n, err := unix.Poll(poll, 100)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			continue
		}
		buffer := make([]byte, 4096)
		n, err = unix.Read(fan, buffer)
		if err != nil || n < unix.FAN_EVENT_METADATA_LEN {
			t.Fatal("invalid owned copy event", n, err)
		}
		for offset := 0; offset < n; {
			event := buffer[offset:]
			length := int(binary.NativeEndian.Uint32(event))
			if length < unix.FAN_EVENT_METADATA_LEN || offset+length > n || event[4] != unix.FANOTIFY_METADATA_VERSION {
				t.Fatal("invalid owned event layout")
			}
			fd := int(int32(binary.NativeEndian.Uint32(event[16:])))
			pid := int(int32(binary.NativeEndian.Uint32(event[20:])))
			if fd < 0 || pid <= 1 {
				t.Fatal("owned event has no process/file identity")
			}
			file := os.NewFile(uintptr(fd), "owned permission event")
			actual, err := file.Stat()
			if err != nil || !os.SameFile(payloadInfo, actual) {
				file.Close()
				t.Fatal("copy boundary changed its exact file", err)
			}
			executable, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
			if err != nil {
				file.Close()
				t.Fatal(err)
			}
			if executable == "/usr/bin/cp" || executable == "/bin/cp" {
				defer file.Close() // Keep the event pending until the whole worker dies.
				var worker struct {
					PID       int    `json:"worker_pid"`
					Token     string `json:"token"`
					SourcePID int    `json:"source_pid"`
				}
				raw, err := os.ReadFile(generation + "/guard/capture-worker-intent.json")
				if err != nil || json.Unmarshal(raw, &worker) != nil || worker.Token != proof.Interrupted.Snapshot || worker.SourcePID != branch.Runtime.PID {
					t.Fatal("copy does not have the exact capture intent", err)
				}
				stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
				fields := strings.Fields(string(stat)[strings.LastIndex(string(stat), ")")+1:])
				if err != nil || len(fields) < 2 || fields[1] != strconv.Itoa(worker.PID) {
					t.Fatal("copy helper is not the exact capture worker child", err)
				}
				var record lifecycle.Record
				raw, err = os.ReadFile(f.root + "/objects/interrupted/record.json")
				if err != nil || json.Unmarshal(raw, &record) != nil || record.Owner != branch.Record.Owner || record.Phase != "pausing" || record.Pause == nil || record.Pause.CompletedAt != "" {
					t.Fatal("actual copy lacks an unconfirmed pause", err)
				}
				if _, err := os.Lstat(generation + "/checkpoint.json"); !os.IsNotExist(err) {
					t.Fatal("incomplete pause already published a checkpoint", err)
				}
				proof.Interrupted.BlockedCopyPID, proof.Interrupted.CaptureWorkerPID = pid, worker.PID
				writeConsumerProof(t, path, *proof)
				readableConsumerOutput(t)
				fmt.Println("FACTORYBOX_CONSUMER_PAUSE_INCOMPLETE=" + proof.Owner)
				// Hold this kernel permission event. The outer owner kills the
				// entire worker; no old file descriptor/helper survives recovery.
				select {}
			}
			response := make([]byte, 8)
			binary.NativeEndian.PutUint32(response, uint32(fd))
			binary.NativeEndian.PutUint32(response[4:], unix.FAN_ALLOW)
			_, err = unix.Write(fan, response)
			file.Close()
			if err != nil {
				t.Fatal(err)
			}
			offset += length
		}
	}
	t.Fatal("actual filesystem-copy pause boundary was not reached", ctx.Err())
}

func writeConsumerProof(t *testing.T, path string, proof consumerPauseProof) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(file).Encode(proof); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func rejectIncompleteConsumerPause(t *testing.T, f *fixture, proof consumerPauseProof) {
	t.Helper()
	interrupted := proof.Interrupted
	if interrupted.Record.ID != "interrupted" || interrupted.BlockedCopyPID <= 1 || interrupted.CaptureWorkerPID <= 1 {
		t.Fatal("missing actual interrupted-copy identities")
	}
	var record lifecycle.Record
	raw, err := os.ReadFile(proof.SourceRoot + "/objects/interrupted/record.json")
	if err != nil || json.Unmarshal(raw, &record) != nil || record.Owner != interrupted.Record.Owner || record.Phase != "pausing" || record.Pause == nil || record.Pause.CompletedAt != "" {
		t.Fatal("worker loss promoted incomplete pause", err)
	}
	if _, err := os.Lstat(proof.SourceRoot + "/objects/interrupted/captures/" + interrupted.Snapshot + "/checkpoint.json"); !os.IsNotExist(err) {
		t.Fatal("worker loss published an incomplete checkpoint", err)
	}
	arguments := []string{"--worker-run-root", "/run/factorybox", "recover-paused", "--source-root", proof.SourceRoot, "--source", "interrupted", "--source-owner", record.Owner, "--snapshot-id", interrupted.Snapshot, "--checkpoint-sha256", strings.Repeat("0", 64), "uncommitted"}
	for attempt := 0; attempt < 2; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		out, err := exec.CommandContext(ctx, "/factorybox", arguments...).CombinedOutput()
		cancel()
		if err == nil || !strings.Contains(string(out), "source is not the exact completed, released and unresumed pause") {
			t.Fatalf("incomplete pause was accepted or failed for another reason: %v: %.4096s", err, out)
		}
	}
	for _, name := range []string{"objects/uncommitted", "forks/uncommitted"} {
		if _, err := os.Lstat(f.root + "/" + name); !os.IsNotExist(err) {
			t.Fatal("incomplete recovery created a target", name, err)
		}
	}
	metadata := proof.SourceRoot + "/objects/source/captures/" + proof.Paused.Pause.SnapshotID + "/checkpoint.json"
	if consumerFileSHA(t, metadata) != interrupted.PriorMetadataSHA256 {
		t.Fatal("incomplete pause damaged earlier committed snapshot metadata")
	}
	t.Log("actual incomplete filesystem-copy pause refused twice after whole-worker SIGKILL; earlier committed pause metadata unchanged")
}
