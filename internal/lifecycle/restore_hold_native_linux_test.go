//go:build linux && amd64

package lifecycle

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

const nativeRestoreHoldAssembly = `.section .text
.global _start
_start:
    incq counter(%rip)
    jmp _start
.section .bss
.balign 8
counter: .skip 8
.section .note.GNU-stack,"",@progbits
`

func nativeRestoreHoldFork(t *testing.T, e *Engine, lock *os.File, source, snapshot, target string) (Record, restoreRequest) {
	t.Helper()
	ctx := context.Background()
	intent, err := e.newForkIntent(ctx, source, snapshot, target)
	if err != nil {
		t.Fatal(err)
	}
	operation := "forks/" + target
	if err := e.root.Mkdir(operation, 0700); err != nil {
		t.Fatal(err)
	}
	if err := e.publishCheckpoint(operation, "intent.json", intent); err != nil {
		t.Fatal(err)
	}
	if err := e.prepareForkBacking(ctx, lock, operation, intent, true); err != nil {
		t.Fatal(err)
	}
	bundle := e.config.Root + "/" + operation + "/materialized/filesystem"
	if err := e.forkOverlay(bundle, operation, intent); err != nil {
		t.Fatal(err)
	}
	object, err := objectPath(target)
	if err != nil {
		t.Fatal(err)
	}
	record, err := e.prepareRecord(target, bundle, object, intent.OCI, intent.Owner, &intent.Origin)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.prepareCgroup(&record); err != nil {
		t.Fatal(err)
	}
	record.Start.Attempted = true
	if err := e.write(record); err != nil {
		t.Fatal(err)
	}
	request := e.forkRestoreRequest(intent, record)
	request.HoldForActivation = true
	return record, request
}

func nativeRestoreHoldResume(t *testing.T, e *Engine, record Record) (Record, resumeRequest) {
	t.Helper()
	record.Phase = "resuming"
	record.Pause.Resume = &Operation{RequestedAt: now()}
	if err := e.write(record); err != nil {
		t.Fatal(err)
	}
	request := e.resumeRequest(record)
	request.HoldForActivation = true
	relative := "objects/" + record.ID + "/resumes/" + record.Pause.SnapshotID
	lock, err := e.lock()
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := e.prepareResume(context.Background(), lock, record, relative, request); err != nil {
		t.Fatal(err)
	}
	if err := e.prepareEmptyResumeCgroup(record); err != nil {
		t.Fatal(err)
	}
	record.Pause.Resume.Attempted = true
	if err := e.write(record); err != nil {
		t.Fatal(err)
	}
	return record, request
}

func nativeRestoreHoldCounter(t *testing.T, pid int, address uint64) uint64 {
	t.Helper()
	data, err := timerMemoryRead(pid, address, 8)
	if err != nil {
		t.Fatal("read exact restored application counter", err)
	}
	return binary.LittleEndian.Uint64(data)
}

func TestNativeRestoreHold(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_RESTORE_HOLD") != "1" {
		t.Skip("owned pre-resume restore hold fixture only")
	}
	status := boundedLossStatusNow(t)
	if err := os.WriteFile("/output/restore-hold.S", []byte(nativeRestoreHoldAssembly), 0644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("cc", "-nostdlib", "-static", "-Wl,--build-id=none", "/output/restore-hold.S", "-o", "/output/restore-hold").CombinedOutput(); err != nil {
		t.Fatal("assemble running counter fixture", err, string(out))
	}
	program, err := os.ReadFile("/output/restore-hold")
	if err != nil {
		t.Fatal(err)
	}
	nm, err := exec.Command("nm", "-n", "/output/restore-hold").Output()
	if err != nil {
		t.Fatal(err)
	}
	var address uint64
	for _, line := range strings.Split(string(nm), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[2] == "counter" {
			address, err = strconv.ParseUint(fields[0], 16, 64)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if address == 0 {
		t.Fatal("running counter fixture has no counter")
	}
	proofs := map[string]any{}
	for _, mode := range []string{"fork", "resumed", "fork-disconnect", "resumed-disconnect", "wrong-ack"} {
		if !t.Run(mode, func(t *testing.T) {
			id := "restore-hold-" + mode
			var cleanup func(*testing.T, View)
			if mode == "resumed-disconnect" {
				cleanup = func(t *testing.T, view View) {
					path := status.Lifecycle.Root + "/objects/" + id + "/record.json"
					before, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					_, rejected := boundedLossCommand("--worker-run-root", "/run/factorybox", "delete", id)
					after, err := os.ReadFile(path)
					if rejected == nil || !strings.Contains(rejected.Error(), "pause/resume is unresolved") || err != nil || !bytes.Equal(before, after) {
						t.Fatal("failed resume evidence was discarded or changed", rejected, err)
					}
					boundedLossSave(t, "restore-hold-"+mode+"-retained.json", map[string]any{"record": json.RawMessage(before), "delete_refused": true, "record_unchanged": true, "cleanup": "Intentionally unresolved record and mounts retained until the unique owned outer worker/volume teardown."})
				}
			}
			source := timerReaderFixtureFilesWithCleanup(t, status, id, "exec /bin/restore-hold", false, map[string][]byte{"restore-hold": program}, cleanup)
			first := nativeRestoreHoldCounter(t, source.Runtime.PID, address)
			time.Sleep(10 * time.Millisecond)
			if nativeRestoreHoldCounter(t, source.Runtime.PID, address) == first {
				t.Fatal("original application counter is not actually running")
			}
			resumed := strings.HasPrefix(mode, "resumed")
			snapshot := strings.Repeat("b", 32)
			if resumed {
				boundedLossAction(t, "pause", "--snapshot-id", snapshot, id)
			} else {
				boundedLossAction(t, "checkpoint", "--snapshot-id", snapshot, id)
			}
			e, err := Open(status.Lifecycle)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { e.Close() })
			var record Record
			var request any
			var destination, receiptName, commandName string
			retainFailedFork := false
			if resumed {
				record, err = e.read(id)
				if err != nil {
					t.Fatal(err)
				}
				record, request = nativeRestoreHoldResume(t, e, record)
				destination = e.config.Root + "/objects/" + id + "/resumes/" + snapshot
				receiptName, commandName = "resume-held.json", "factorybox-resume"
			} else {
				lock, err := e.lock()
				if err != nil {
					t.Fatal(err)
				}
				record, request = nativeRestoreHoldFork(t, e, lock, id, snapshot, id+"-target")
				lock.Close()
				t.Cleanup(func() {
					if retainFailedFork {
						path := e.config.Root + "/objects/" + record.ID + "/record.json"
						before, readErr := os.ReadFile(path)
						_, rejected := e.Delete(context.Background(), record.ID)
						after, afterErr := os.ReadFile(path)
						if readErr != nil || afterErr != nil || rejected == nil || !strings.Contains(rejected.Error(), "unresolved create") || !bytes.Equal(before, after) {
							t.Fatal("failed creation evidence was discarded or changed", rejected, readErr, afterErr)
						}
						boundedLossSave(t, "restore-hold-"+mode+"-retained.json", map[string]any{"record": json.RawMessage(before), "delete_refused": true, "record_unchanged": true, "cleanup": "Intentionally unresolved record and mounts retained until the unique owned outer worker/volume teardown."})
						return
					}
					if _, err := e.Delete(context.Background(), record.ID); err != nil {
						t.Error("delete exact held fork", err)
					}
				})
				destination = e.config.Root + "/objects/" + record.ID
				receiptName, commandName = "restore-held.json", "factorybox-restore"
			}
			data, err := json.Marshal(request)
			if err != nil {
				t.Fatal(err)
			}
			requestSHA := sum(data)
			fd, err := unix.MemfdCreate("sealed native held restore", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
			if err != nil {
				t.Fatal(err)
			}
			sealed := os.NewFile(uintptr(fd), "sealed native held restore")
			defer sealed.Close()
			if _, err := sealed.Write(data); err != nil {
				t.Fatal(err)
			}
			if _, err := sealed.Seek(0, 0); err != nil {
				t.Fatal(err)
			}
			if _, err := unix.FcntlInt(sealed.Fd(), unix.F_ADD_SEALS, captureRequestSeals); err != nil {
				t.Fatal(err)
			}
			sockets, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
			if err != nil {
				t.Fatal(err)
			}
			client, child := os.NewFile(uintptr(sockets[0]), "restore hold native controller"), os.NewFile(uintptr(sockets[1]), "restore hold runtime endpoint")
			defer client.Close()
			defer child.Close()
			lock, err := e.lock()
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				_, err := e.commandFiles(ctx, lock, []*os.File{sealed, child}, "--criu", e.config.Criu, commandName, record.RuntimeID)
				done <- err
			}()
			packet, err := captureGuardRead(int(client.Fd()), 15000)
			if err != nil {
				select {
				case commandErr := <-done:
					t.Fatal("restore never entered the held pre-resume notification", err, commandErr)
				default:
					t.Fatal("restore never entered the held pre-resume notification", err)
				}
			}
			child.Close() // The runtime has inherited it; retain no spare endpoint.
			var ready struct {
				Version    int    `json:"version"`
				RequestSHA string `json:"request_sha256"`
				RuntimeID  string `json:"runtime_id"`
				PID        int    `json:"pid"`
				Start      string `json:"process_start"`
				Tracer     int    `json:"criu_tracer_pid"`
				Frozen     bool   `json:"frozen_before_resume"`
				CRIUOrigin bool   `json:"criu_returned_to_original_worker"`
			}
			if err := json.Unmarshal(packet, &ready); err != nil || ready.Version != 1 || ready.RequestSHA != requestSHA || ready.RuntimeID != record.RuntimeID || ready.PID <= 1 || ready.Tracer <= 1 || !ready.Frozen || !ready.CRIUOrigin {
				t.Fatal("held notification changed its original sealed identity", string(packet), err)
			}
			workerGroup, err := os.ReadFile("/proc/self/cgroup")
			criuGroup, criuGroupErr := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", ready.Tracer))
			if err != nil || criuGroupErr != nil || !bytes.Equal(workerGroup, criuGroup) {
				t.Fatal("temporary CRIU child did not return to the original worker cgroup", string(criuGroup), err, criuGroupErr)
			}
			birth, err := processBirth(ready.PID)
			if err != nil || birth != ready.Start {
				t.Fatal("restored task birth changed before controller acknowledgement", err)
			}
			pidfd, err := unix.PidfdOpen(ready.PID, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer unix.Close(pidfd)
			traced, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", ready.PID))
			if err != nil || !bytes.Contains(traced, []byte(fmt.Sprintf("TracerPid:\t%d\n", ready.Tracer))) {
				t.Fatal("restored task was already released before controller acknowledgement", err)
			}
			before := nativeRestoreHoldCounter(t, ready.PID, address)
			time.Sleep(100 * time.Millisecond)
			if nativeRestoreHoldCounter(t, ready.PID, address) != before {
				t.Fatal("application ran while CRIU awaited held-source acknowledgement")
			}
			observe := func(name string) {
				row := map[string]any{"ready": ready, "application_counter": nativeRestoreHoldCounter(t, ready.PID, address)}
				for _, part := range []string{"status", "cgroup", "wchan", "syscall"} {
					data, err := os.ReadFile(fmt.Sprintf("/proc/%d/%s", ready.Tracer, part))
					row["criu_"+part] = map[string]any{"data": string(data), "error": fmt.Sprint(err)}
				}
				data, err := os.ReadFile(guardGroupPath(record) + "/cgroup.events")
				row["group_events"], row["group_error"] = string(data), fmt.Sprint(err)
				boundedLossSave(t, "restore-hold-"+mode+"-"+name+".json", row)
			}
			observe("before-ack")
			failedControl := strings.HasSuffix(mode, "disconnect") || mode == "wrong-ack"
			if strings.HasSuffix(mode, "disconnect") {
				client.Close()
			} else {
				ack := "held:" + requestSHA
				if mode == "wrong-ack" {
					ack = "held:" + strings.Repeat("0", 64)
				}
				if _, err := client.Write([]byte(ack)); err != nil {
					t.Fatal(err)
				}
			}
			var commandErr error
			select {
			case commandErr = <-done:
			case <-time.After(5 * time.Second):
				observe("after-ack-live")
				select {
				case commandErr = <-done:
				case <-ctx.Done():
					t.Fatal("owned restore did not terminate after hold acknowledgement", ctx.Err())
				}
			case <-ctx.Done():
				t.Fatal("owned restore did not terminate after hold acknowledgement", ctx.Err())
			}
			if failedControl {
				if commandErr == nil {
					t.Fatal("restore accepted a missing or foreign acknowledgement")
				}
				if data, err := os.ReadFile(destination + "/" + receiptName); err == nil {
					t.Fatal("rejected hold published a completion receipt", string(data))
				}
				live := false
				if birth, err := processBirth(ready.PID); err == nil && birth == ready.Start {
					live = true
					group, err := os.ReadFile(guardGroupPath(record) + "/cgroup.events")
					if err != nil || !bytes.Contains(group, []byte("frozen 1\n")) || nativeRestoreHoldCounter(t, ready.PID, address) != before {
						t.Fatal("rejected restore exposed its original application", err)
					}
					if err := unix.PidfdSendSignal(pidfd, unix.SIGKILL, nil, 0); err != nil {
						t.Fatal("stop only the pinned rejected native fixture task", err)
					}
					poll := []unix.PollFd{{Fd: int32(pidfd), Events: unix.POLLIN}}
					if n, err := unix.Poll(poll, 2000); err != nil || n != 1 || poll[0].Revents&unix.POLLIN == 0 {
						t.Fatal("pinned rejected fixture task remains alive", err)
					}
				}
				retainFailedFork = !resumed
				proofs[mode] = map[string]any{"failed_closed": true, "held_before_ack": true, "application_counter_unchanged": true, "live_but_held": live, "controller_error": commandErr.Error()}
				return
			}
			if commandErr != nil {
				t.Fatal("held restore failed", commandErr)
			}
			traced, err = os.ReadFile(fmt.Sprintf("/proc/%d/status", ready.PID))
			if err != nil || !bytes.Contains(traced, []byte("TracerPid:\t0\n")) {
				t.Fatal("held restore returned before CRIU detached its original task", err)
			}
			var receipt restoreResult
			data, err = os.ReadFile(destination + "/" + receiptName)
			if err != nil || json.Unmarshal(data, &receipt) != nil || receipt.RequestSHA256 != requestSHA || receipt.PID != ready.PID || receipt.ProcessStart != ready.Start || receipt.CompletedAt == "" {
				t.Fatal("held restore has no exact persisted outcome", err, string(data))
			}
			group, err := os.ReadFile(guardGroupPath(record) + "/cgroup.events")
			if err != nil || !bytes.Contains(group, []byte("frozen 1\n")) {
				t.Fatal("restore released its original cgroup before activation", err)
			}
			time.Sleep(200 * time.Millisecond)
			if nativeRestoreHoldCounter(t, ready.PID, address) != before {
				t.Fatal("application ran between CRIU detach and explicit activation release")
			}
			if err := captureNativeFreeze(record, false); err != nil {
				t.Fatal(err)
			}
			for deadline := time.Now().Add(time.Second); ; {
				if nativeRestoreHoldCounter(t, ready.PID, address) != before {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("held application did not resume after exact explicit thaw")
				}
				time.Sleep(time.Millisecond)
			}
			record.Phase, record.RuntimeCreated = "running", receipt.Created
			if resumed {
				record.Pause.Resume.CompletedAt = receipt.CompletedAt
			} else {
				record.Start.CompletedAt = receipt.CompletedAt
			}
			if err := e.write(record); err != nil {
				t.Fatal(err)
			}
			proofs[mode] = map[string]any{"held_before_ack": true, "held_after_criu_detach": true, "application_counter_unchanged": true, "explicit_thaw_runs_application": true, "counter_at_pre_resume": before, "request_sha256": requestSHA, "process_start": ready.Start}
		}) {
			return
		}
	}
	boundedLossSave(t, "restore-hold.json", map[string]any{"passed": true, "production_qualified": false, "cases": proofs, "task_sha256": sum(program), "scope": "Exact pinned Sysbox restore/resume pre-detach cgroup hold through sealed optional private requests; no application counter progress before ACK or after detach until explicit thaw. Socket disconnect and foreign ACK refuse. Production activation is not enabled and no timer activation or full lifecycle qualification is claimed."})
}
