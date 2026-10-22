//go:build linux

package lifecycle

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// This native task has a read-only relative request and no remainder pointer.
// It also checks the syscall register contract before writing its completion
// timestamp. An EINTR, altered argument/register or restarted full duration must
// not satisfy the complete checkpoint/fork/deadline assertions below.
const timerNativeAssembly = `.section .text
.global _start
_start:
    mov %rsp, initial_sp(%rip)
    mov $0x31113333, %ebx
    mov $0x21112222, %ebp
    mov $0x41114444, %r13d
    mov $0x51115555, %r14d
    mov $0x61116666, %r15d
    lea request(%rip), %rdx
    mov $0x11223344, %r8d
    mov $0x55667788, %r9d
    mov $230, %eax
    mov $1, %edi
    xor %esi, %esi
    xor %r10d, %r10d
    pushfq
    pop %r12
    syscall
returned:
	 mov %rax, actual_result(%rip)
	 pushfq
	 pop %rax
	 cmp %r12, %rax
	 jne failed
	 cmpq $0, actual_result(%rip)
    jne failed
    cmp $1, %rdi
    jne failed
    test %rsi, %rsi
    jne failed
    lea request(%rip), %rax
    cmp %rax, %rdx
    jne failed
    test %r10, %r10
    jne failed
    cmp $0x11223344, %r8
    jne failed
    cmp $0x55667788, %r9
    jne failed
    lea returned(%rip), %rax
    cmp %rax, %rcx
    jne failed
    cmp %r12, %r11
    jne failed
    cmp initial_sp(%rip), %rsp
    jne failed
    cmp $0x31113333, %rbx
    jne failed
    cmp $0x21112222, %rbp
    jne failed
    cmp $0x41114444, %r13
    jne failed
    cmp $0x51115555, %r14
    jne failed
    cmp $0x61116666, %r15
    jne failed
    cmpq $12, request(%rip)
    jne failed
    cmpq $0, request+8(%rip)
    jne failed
    mov $257, %eax
    mov $-100, %edi
    lea marker(%rip), %rsi
    mov $193, %edx
    mov $384, %r10d
    syscall
    test %rax, %rax
    js failed
    mov %rax, %r13
    mov $228, %eax
    mov $1, %edi
    lea finish(%rip), %rsi
    syscall
    test %rax, %rax
    jne failed
    mov $1, %eax
    mov %r13, %rdi
    lea finish(%rip), %rsi
    mov $16, %edx
    syscall
    cmp $16, %rax
    jne failed
    mov $74, %eax
    mov %r13, %rdi
    syscall
    test %rax, %rax
    jne failed
    xor %edi, %edi
    jmp exit_task
failed:
    mov $79, %edi
exit_task:
    mov $60, %eax
    syscall
.section .rodata
request: .quad 12, 0
marker: .asciz "/tmp/timer-finished"
.section .bss
.balign 8
finish: .skip 16
initial_sp: .skip 8
actual_result: .skip 8
.section .note.GNU-stack,"",@progbits
`

// Only the prototype adds a failure receipt after an existing assertion has
// failed. Every assertion, the successful path and exit status 79 are retained.
const timerNativeFailureReceipt = `failed:
    mov %rax, failure_state(%rip)
    mov actual_result(%rip), %rax
    mov %rax, failure_state+8(%rip)
    mov %r12, failure_state+16(%rip)
    mov %r11, failure_state+24(%rip)
    mov %rsp, failure_state+32(%rip)
    pushfq
    pop %rax
    mov %rax, failure_state+40(%rip)
    mov $228, %eax
    mov $1, %edi
    lea failure_state+48(%rip), %rsi
    syscall
    mov $257, %eax
    mov $-100, %edi
    lea failure_marker(%rip), %rsi
    mov $193, %edx
    mov $384, %r10d
    syscall
    mov %rax, %r13
    mov %rax, %rdi
    mov $1, %eax
    lea failure_state(%rip), %rsi
    mov $64, %edx
    syscall
    mov $74, %eax
    mov %r13, %rdi
    syscall
    mov $79, %edi
`

func TestNativeCheckpointRetirementTimerState(t *testing.T) {
	timerNativeState(t, false)
}

func TestNativeTimerRestartPrototype(t *testing.T) {
	if os.Getenv("FACTORYBOX_TIMER_RESTART_PROTOTYPE") != "1" {
		t.Skip("single-thread native timer continuation experiment only")
	}
	timerNativeState(t, true)
}

func timerNativeState(t *testing.T, prototype bool) {
	if os.Getenv("FACTORYBOX_NATIVE_RETIREMENT") != "1" {
		t.Skip("owned delivered worker relative-wait fixture only")
	}
	if _, err := os.Stat("/timer-probe.py"); err != nil {
		t.Fatal("required relative-wait regression needs the explicit read-only timer probe")
	}
	status := boundedLossStatusNow(t)
	var prototypePause *PauseOperation
	if prototype {
		compile := exec.Command("cc", "-nostdlib", "-static", "-Wl,--build-id=none,-Ttext=0,-e,timer_restart_entry", "/timer-restart.S", "-o", "/output/timer-stub.elf")
		if output, err := compile.CombinedOutput(); err != nil {
			t.Fatal("compile native continuation", err, string(output))
		}
		if output, err := exec.Command("objcopy", "-O", "binary", "--only-section=.text", "/output/timer-stub.elf", "/output/timer-stub.bin").CombinedOutput(); err != nil {
			t.Fatal("extract native continuation", err, string(output))
		}
	}
	assembly := timerNativeAssembly
	if prototype {
		assembly = strings.Replace(assembly, "failed:\n    mov $79, %edi\n", timerNativeFailureReceipt, 1)
		assembly += ".section .rodata\nfailure_marker: .asciz \"/tmp/timer-failed\"\n.section .bss\n.balign 8\nfailure_state: .skip 64\n"
	}
	if err := os.WriteFile("/output/relative-wait.S", []byte(assembly), 0644); err != nil {
		t.Fatal(err)
	}
	compile := exec.Command("cc", "-nostdlib", "-static", "-Wl,--build-id=none", "/output/relative-wait.S", "-o", "/output/relative-wait")
	output, err := compile.CombinedOutput()
	if err := os.WriteFile("/output/relative-wait-build.log", output, 0644); err != nil {
		t.Fatal(err)
	}
	if err != nil {
		t.Fatal("compile native AMD64 relative-wait task", err, string(output))
	}
	bundle := status.Store + "/timer-bundle"
	for _, name := range []string{"", "/upper", "/work", "/rootfs"} {
		mode := os.FileMode(0755)
		if name == "" {
			mode = 0700
		}
		if err := os.Mkdir(bundle+name, mode); err != nil {
			t.Fatal(err)
		}
	}
	if output, err := exec.Command("cp", "-a", "/busybox", bundle+"/lower").CombinedOutput(); err != nil {
		t.Fatal(string(output), err)
	}
	binaryData, err := os.ReadFile("/output/relative-wait")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundle+"/lower/bin/relative-wait", binaryData, 0755); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mount("overlay", bundle+"/rootfs", "overlay", 0, "index=off,lowerdir="+bundle+"/lower,upperdir="+bundle+"/upper,workdir="+bundle+"/work"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := unix.Unmount(bundle+"/rootfs", 0); err != nil {
			t.Error("unmount exact timer fixture overlay", err)
		}
	})
	if output, err := exec.Command(status.Lifecycle.Runtime, "spec", "--bundle", bundle).CombinedOutput(); err != nil {
		t.Fatal(string(output), err)
	}
	data, err := os.ReadFile(bundle + "/config.json")
	if err != nil {
		t.Fatal(err)
	}
	var spec map[string]any
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatal(err)
	}
	process := spec["process"].(map[string]any)
	process["terminal"], process["cwd"] = false, "/"
	process["args"] = []string{"/bin/sh", "-c", "exec /bin/relative-wait </dev/null >/dev/null 2>&1"}
	process["env"] = []string{"HOME=/root", "PATH=/bin:/sbin:/usr/bin:/usr/sbin"}
	spec["root"].(map[string]any)["readonly"] = false
	spec["linux"].(map[string]any)["resources"] = map[string]any{"pids": map[string]any{"limit": 64}, "devices": []map[string]any{{"allow": true, "access": "rwm"}}}
	data, err = json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundle+"/config.json", data, 0600); err != nil {
		t.Fatal("write private timer OCI configuration", err)
	}
	source := boundedLossView(t, "start", "--bundle", bundle, "timer-source")
	// Pin the owned task's actual overlay view while its PID is still live.
	// Sysbox creates another mount of the same upper/work directories. An outer
	// overlay's cached negative lookup is not an authoritative guest read.
	sourceRoot, err := os.Open("/proc/" + strconv.Itoa(source.Runtime.PID) + "/root")
	if err != nil {
		t.Fatal("pin original timer root", err)
	}
	defer sourceRoot.Close()
	t.Cleanup(func() {
		for _, id := range []string{"timer-child", "timer-source"} {
			if _, err := os.Stat(status.Lifecycle.Root + "/objects/" + id); os.IsNotExist(err) {
				continue
			}
			if _, err := boundedLossCommand("--worker-run-root", "/run/factorybox", "delete", id); err != nil {
				t.Error("delete exact timer fixture sandbox", id, err)
			}
		}
	})
	var requestAddress uint64
	for deadline := time.Now().Add(2 * time.Second); ; {
		data, err := os.ReadFile("/proc/" + strconv.Itoa(source.Runtime.PID) + "/syscall")
		fields := strings.Fields(string(data))
		if err == nil && len(fields) >= 5 && fields[0] == "230" && fields[1] == "0x1" && fields[2] == "0x0" && fields[4] == "0x0" {
			requestAddress, err = strconv.ParseUint(fields[3], 0, 64)
			if err != nil {
				t.Fatal(err)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("native task did not reach its exact relative wait", string(data), err)
		}
		time.Sleep(time.Millisecond)
	}
	maps, err := os.ReadFile("/proc/" + strconv.Itoa(source.Runtime.PID) + "/maps")
	if err != nil {
		t.Fatal(err)
	}
	readOnlyRequest := false
	for _, line := range strings.Split(string(maps), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		rangeParts := strings.Split(fields[0], "-")
		if len(rangeParts) != 2 {
			t.Fatal("invalid native mapping")
		}
		begin, beginErr := strconv.ParseUint(rangeParts[0], 16, 64)
		end, endErr := strconv.ParseUint(rangeParts[1], 16, 64)
		if beginErr != nil || endErr != nil {
			t.Fatal("invalid native mapping range", beginErr, endErr)
		}
		if requestAddress >= begin && requestAddress+16 <= end {
			readOnlyRequest = !strings.Contains(fields[1], "w") && strings.HasPrefix(fields[1], "r")
		}
	}
	if !readOnlyRequest {
		t.Fatal("native wait request is not actually mapped read-only")
	}
	if err := captureNativeFreeze(source.Record, true); err != nil {
		t.Fatal(err)
	}
	frozen := true
	defer func() {
		if frozen {
			if err := captureNativeFreeze(source.Record, false); err != nil {
				t.Error("release exact timer diagnostic freeze", err)
			}
		}
	}()
	probe := exec.Command("python3", "/timer-probe.py", strconv.Itoa(source.Runtime.PID))
	output, err = probe.CombinedOutput()
	t.Logf("read-only NULL-remainder timer: %s err=%v", output, err)
	var state struct {
		Names     []string `json:"restart_function_names"`
		Type      int      `json:"timespec_type"`
		Remainder string   `json:"remaining_user_pointer"`
		Expires   int64    `json:"expires_ns"`
		Now       int64    `json:"observed_monotonic_ns"`
		Birth     bool     `json:"birth_matches"`
		Released  bool     `json:"program_released"`
	}
	if err != nil || json.Unmarshal(output, &state) != nil || !state.Birth || !state.Released ||
		len(state.Names) != 1 || state.Names[0] != "hrtimer_nanosleep_restart" || state.Type != 0 || state.Remainder != "0x0" ||
		state.Expires-state.Now < int64(8*time.Second) || state.Expires-state.Now > int64(12*time.Second) {
		t.Fatal("read-only probe lacks the exact NULL-remainder timer", err)
	}
	if err := captureNativeFreeze(source.Record, false); err != nil {
		t.Fatal(err)
	}
	frozen = false
	snapshot := strings.Repeat("e", 32)
	if prototype {
		stub, err := os.ReadFile("/output/timer-stub.bin")
		if err != nil {
			t.Fatal(err)
		}
		birth, err := processBirth(source.Runtime.PID)
		if err != nil {
			t.Fatal(err)
		}
		boundedLossSave(t, "timer-prepare-request.json", map[string]any{"pid": source.Runtime.PID,
			"start": birth, "runtime_id": source.Record.RuntimeID, "stub_sha256": sum(stub)})
		engine, err := Open(status.Lifecycle)
		if err != nil {
			t.Fatal(err)
		}
		defer engine.Close()
		if _, err := engine.Checkpoint(context.Background(), "timer-source", snapshot); err != nil {
			t.Fatal("experimental prepared checkpoint", err)
		}
	} else {
		boundedLossAction(t, "checkpoint", "--snapshot-id", snapshot, "timer-source")
	}
	after := boundedLossView(t, "state", "timer-source")
	if after.Runtime == nil || after.Runtime.PID != source.Runtime.PID || after.Runtime.Status != "running" {
		t.Fatal("checkpoint prematurely returned from the original NULL-remainder wait", after.Runtime)
	}
	if prototype {
		// No test preparer participates in this second capture. The delivered
		// CLI and unchanged CRIU must preserve the absolute continuation.
		boundedLossAction(t, "checkpoint", "--snapshot-id", strings.Repeat("f", 32), "timer-source")
		after := boundedLossView(t, "state", "timer-source")
		if after.Runtime == nil || after.Runtime.PID != source.Runtime.PID || after.Runtime.Status != "running" {
			t.Fatal("second delivered capture prematurely returned from prepared wait", after.Runtime)
		}
	}
	child := boundedLossView(t, "fork", "--source", "timer-source", "--snapshot-id", snapshot, "timer-child")
	if child.Runtime == nil || child.Runtime.Status != "running" {
		t.Fatal("fork prematurely returned from its saved NULL-remainder wait", child.Runtime)
	}
	if prototype {
		paused := boundedLossView(t, "pause", "--snapshot-id", strings.Repeat("d", 32), "timer-child")
		prototypePause = paused.Record.Pause
		if paused.Record.Pause != nil && paused.Record.Pause.After.Anon != 0 {
			samples := []map[string]any{}
			for deadline := time.Now().Add(3 * time.Second); ; {
				data, err := os.ReadFile(cgroupRoot + "/" + child.Record.RuntimeID + "/memory.stat")
				if err != nil {
					t.Fatal("observe paused timer memory accounting", err)
				}
				counters, err := parseCgroupCounters(string(data))
				if err != nil {
					t.Fatal(err)
				}
				events, err := os.ReadFile(cgroupRoot + "/" + child.Record.RuntimeID + "/cgroup.events")
				if err != nil {
					t.Fatal(err)
				}
				samples = append(samples, map[string]any{"at": time.Now().UTC(), "counters": counters, "events": string(events)})
				if counters["anon"] == 0 || time.Now().After(deadline) {
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
			boundedLossSave(t, "timer-paused-memory-observations.json", map[string]any{"recorded": paused.Record.Pause, "samples": samples})
		}
		if paused.Record.Pause == nil || paused.Record.Pause.After.Anon != 0 || paused.Record.Pause.Snapshot.ResidentBytesAfter != 0 {
			t.Fatal("prepared restored wait retained snapshot RAM after pause", paused.Record.Pause)
		}
		child = boundedLossView(t, "resume", "--snapshot-id", strings.Repeat("d", 32), "timer-child")
		if child.Runtime == nil || child.Runtime.Status != "running" {
			t.Fatal("resumed prepared wait prematurely returned", child.Runtime)
		}
	}
	childRoot, err := os.Open("/proc/" + strconv.Itoa(child.Runtime.PID) + "/root")
	if err != nil {
		t.Fatal("pin restored timer root", err)
	}
	defer childRoot.Close()
	if prototype && os.Getenv("FACTORYBOX_TIMER_OBSERVE_EXIT") == "1" {
		events, err := observeTimerNativeExits([]int{source.Runtime.PID, child.Runtime.PID})
		boundedLossSave(t, "timer-natural-exits.json", events)
		if err != nil {
			t.Fatal("observe natural timer completion", err)
		}
	}
	finished := map[string]int64{}
	outerReads := map[string]any{}
	for deadline := time.Now().Add(16 * time.Second); len(finished) != 2; {
		for id, root := range map[string]*os.File{"timer-source": sourceRoot, "timer-child": childRoot} {
			path := "/proc/self/fd/" + strconv.Itoa(int(root.Fd()))
			if _, exists := finished[id]; exists {
				continue
			}
			data, err := os.ReadFile(path + "/tmp/timer-finished")
			if os.IsNotExist(err) {
				if prototype {
					outerPath := source.Record.Rootfs
					if id == "timer-child" {
						outerPath = child.Record.Rootfs
					}
					_, _ = os.ReadFile(outerPath + "/tmp/timer-finished")
					failure, failureErr := os.ReadFile(path + "/tmp/timer-failed")
					if failureErr == nil {
						if err := os.WriteFile("/output/"+id+"-timer-failed.bin", failure, 0644); err != nil {
							t.Fatal(err)
						}
						words := []string{}
						for offset := 0; offset+8 <= len(failure); offset += 8 {
							words = append(words, strconv.FormatUint(binary.LittleEndian.Uint64(failure[offset:offset+8]), 16))
						}
						t.Fatal("unchanged timer assertion failed after continuation", id, words)
					}
				}
				continue
			}
			if err != nil || len(data) != 16 {
				t.Fatal("timer completion or syscall register contract failed", id, err)
			}
			timestamp := int64(binary.LittleEndian.Uint64(data[:8]))*int64(time.Second) + int64(binary.LittleEndian.Uint64(data[8:]))
			if timestamp < state.Expires || timestamp-state.Expires > int64(250*time.Millisecond) {
				t.Fatal("restored relative wait changed its original deadline", id, timestamp, state.Expires)
			}
			finished[id] = timestamp
			if prototype {
				outerPath := source.Record.Rootfs
				if id == "timer-child" {
					outerPath = child.Record.Rootfs
				}
				outer, outerErr := os.ReadFile(outerPath + "/tmp/timer-finished")
				outerReads[id] = map[string]any{"not_found": os.IsNotExist(outerErr), "bytes": len(outer), "sha256": sum(outer), "matches_guest": outerErr == nil && bytes.Equal(outer, data)}
			}
		}
		if time.Now().After(deadline) {
			if prototype {
				for _, id := range []string{"timer-source", "timer-child"} {
					data, err := boundedLossCommand("--worker-run-root", "/run/factorybox", "state", id)
					t.Logf("failed continuation state %s: %s err=%v", id, data, err)
				}
			}
			t.Fatal("waits did not complete their syscall/register/deadline contract", finished)
		}
		time.Sleep(5 * time.Millisecond)
	}
	boundedLossSave(t, "timer-state.json", map[string]any{"schema": 1, "passed": true, "production_qualified": false,
		"read_only_request": true, "null_remainder": true, "syscall_registers_preserved": true, "source_and_fork_original_deadline": true,
		"task_sha256": sum(binaryData), "source_sha256": sum([]byte(assembly)), "finished": finished,
		"probe": json.RawMessage(bytes.TrimSpace(output))})
	if prototype {
		boundedLossSave(t, "timer-prototype.json", map[string]any{"schema": 1, "passed": true, "production_qualified": false,
			"second_checkpoint_delivered_cli": true, "natural_exit_observer": os.Getenv("FACTORYBOX_TIMER_OBSERVE_EXIT") == "1",
			"restored_child_pause_resume": true, "paused_snapshot_resident_bytes": 0,
			"pause":             prototypePause,
			"guest_root_pinned": true, "private_bytes_per_task": 8192, "outer_mount_reads": outerReads})
	}
}
