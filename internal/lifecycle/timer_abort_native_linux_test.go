//go:build linux && amd64

package lifecycle

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestNativeTimerAbortProtection(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_TIMER_ABORT") != "1" {
		t.Skip("owned actual ENOSPC timer-continuation regression only")
	}
	assembly := nativeTimerRemainderProtectionAssembly()
	if err := os.WriteFile("/output/timer-abort.S", []byte(assembly), 0644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("cc", "-nostdlib", "-static", "-Wl,--build-id=none", "/output/timer-abort.S", "-o", "/output/timer-abort").CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	program, err := os.ReadFile("/output/timer-abort")
	if err != nil {
		t.Fatal(err)
	}
	nm, err := exec.Command("nm", "-n", "/output/timer-abort").Output()
	if err != nil {
		t.Fatal(err)
	}
	symbols := map[string]uint64{}
	for _, line := range strings.Split(string(nm), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 {
			value, err := strconv.ParseUint(fields[0], 16, 64)
			if err != nil {
				t.Fatal(err)
			}
			symbols[fields[2]] = value
		}
	}
	if symbols["remaining"] == 0 || symbols["sleep_started"] == 0 {
		t.Fatal("missing original timer symbols")
	}
	proofs := map[string]any{}
	for _, mode := range []string{"uncaptured", "aborted"} {
		if !t.Run(mode, func(t *testing.T) {
			original, bundle := nativeEngine(t, "/bin/timer-abort")
			if err := os.WriteFile(bundle+"/lower/bin/timer-abort", program, 0755); err != nil {
				t.Fatal(err)
			}
			config := original.config
			if err := unix.Mount("tmpfs", config.Root, "tmpfs", unix.MS_NOSUID|unix.MS_NODEV, "size=128m,mode=0700"); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if t.Failed() {
					out, err := exec.Command("tar", "--sparse", "-C", config.Root, "-cpf", filepath.Dir(config.Root)+"/timer-abort-controller.tar", ".").CombinedOutput()
					if err != nil {
						t.Error("retain exact failed tmpfs", err, string(out))
					}
				}
				if err := unix.Unmount(config.Root, 0); err != nil {
					t.Error("unmount owned timer tmpfs", err)
				}
			})
			config.MinFreeBytes, config.WriteReserve = 8<<20, 8<<20
			config.Criu, config.CriuSHA256, config.ManagerRoot = "/out/criu", os.Getenv("FACTORYBOX_CRIU_SHA256"), "/var/lib/sysbox/store/manager"
			engine, err := Open(config)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { engine.Close() })
			if mode == "aborted" {
				large, err := os.OpenFile(bundle+"/upper/large-timer-pressure", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
				if err != nil {
					t.Fatal(err)
				}
				chunk := []byte(strings.Repeat("x", 1<<20))
				for range 160 {
					if _, err := large.Write(chunk); err != nil {
						large.Close()
						t.Fatal(err)
					}
				}
				if err := large.Close(); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			view, err := engine.Start(ctx, "source", bundle)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				// Cleanup headroom follows every behavioral assertion. It cannot
				// rescue the failed checkpoint or alter its observed return.
				if err := unix.Mount("tmpfs", config.Root, "tmpfs", unix.MS_REMOUNT|unix.MS_NOSUID|unix.MS_NODEV, "size=144m,mode=0700"); err != nil {
					t.Error(err)
				}
				if _, err := engine.Delete(context.Background(), "source"); err != nil {
					t.Error("delete exact timer source", err)
				}
			})
			root, err := os.Open(fmt.Sprintf("/proc/%d/root", view.Runtime.PID))
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			for deadline := time.Now().Add(time.Second); ; {
				data, err := os.ReadFile(fmt.Sprintf("/proc/%d/syscall", view.Runtime.PID))
				fields := strings.Fields(string(data))
				if err == nil && len(fields) == 9 && fields[0] == "230" && fields[1] == "0x1" && fields[2] == "0x0" && fields[4] != "0x0" {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("original protected timer did not enter its native wait", string(data), err)
				}
				time.Sleep(time.Millisecond)
			}
			started, err := timerMemoryRead(view.Runtime.PID, symbols["sleep_started"], 16)
			if err != nil {
				t.Fatal(err)
			}
			startedNS := int64(binary.LittleEndian.Uint64(started))*1e9 + int64(binary.LittleEndian.Uint64(started[8:]))
			if offset, err := timerOffset(view.Runtime.PID, 1); err != nil || offset != 0 {
				t.Fatal("control requires exact zero source offset", offset, err)
			}
			row := map[string]any{"mode": mode, "task_sha256": sum(program), "assembly_sha256": sum([]byte(assembly)), "source_pid": view.Runtime.PID}
			if mode == "aborted" {
				capacity, err := engine.Capacity()
				if err != nil || !capacity.WriteAdmitted {
					t.Fatal("pressure case did not pass initial admission", capacity, err)
				}
				_, failure := engine.Checkpoint(ctx, "source", strings.Repeat("e", 32))
				var aborted *checkpointCaptureError
				if failure == nil || !strings.Contains(strings.ToLower(failure.Error()), "no space left") || !errors.As(failure, &aborted) || !aborted.recovered {
					t.Fatal("actual ENOSPC did not confirm source rollback", failure)
				}
				current, err := engine.Inspect(ctx, "source")
				if err != nil || current.Runtime == nil || current.Runtime.PID != view.Runtime.PID || current.Runtime.Status != "running" {
					t.Fatal("confirmed abort did not release its original source", current, err)
				}
				row["actual_enospc"], row["source_recovered_before_signal"] = failure.Error(), true
				row["capacity_before"], row["snapshot_id"] = capacity, strings.Repeat("e", 32)
			}
			call, err := os.ReadFile(fmt.Sprintf("/proc/%d/syscall", view.Runtime.PID))
			if err != nil {
				t.Fatal(err)
			}
			row["syscall_before_signal"] = string(call)
			pidfd, err := unix.PidfdOpen(view.Runtime.PID, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer unix.Close(pidfd)
			var sent unix.Timespec
			if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &sent); err != nil {
				t.Fatal(err)
			}
			if err := unix.PidfdSendSignal(pidfd, unix.SIGUSR1, nil, 0); err != nil {
				t.Fatal(err)
			}
			for deadline := time.Now().Add(3 * time.Second); ; {
				base := fmt.Sprintf("/proc/self/fd/%d/tmp/", root.Fd())
				if data, err := os.ReadFile(base + "timer-failed"); err == nil && len(data) == 64 {
					row["actual_raw_result"], row["expected_raw_result"], row["completed_controls"] = int64(binary.LittleEndian.Uint64(data[8:16])), -4, proofs
					if err := os.WriteFile("/output/timer-abort-failure.bin", data, 0600); err != nil {
						t.Fatal(err)
					}
					boundedLossSave(t, "timer-abort-failed.json", row)
					t.Fatal("confirmed ENOSPC rollback changed the original protected-remainder EINTR/ABI result", row)
				}
				if data, err := os.ReadFile(base + "timer-finished"); err == nil && len(data) == 16 {
					break
				}
				if time.Now().After(deadline) {
					boundedLossSave(t, "timer-abort-failed.json", row)
					t.Fatal("aborted timer did not complete original EINTR/ABI assertions")
				}
				time.Sleep(time.Millisecond)
			}
			readOnly := false
			maps, err := timerMappings(view.Runtime.PID)
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range maps {
				if m.start <= symbols["remaining"] && symbols["remaining"]+16 <= m.end {
					readOnly = strings.HasPrefix(m.permissions, "r") && !strings.Contains(m.permissions, "w")
				}
			}
			if !readOnly {
				t.Fatal("handler did not actually protect its remainder page")
			}
			remaining, err := timerMemoryRead(view.Runtime.PID, symbols["remaining"], 16)
			if err != nil {
				t.Fatal(err)
			}
			sec, nsec := binary.LittleEndian.Uint64(remaining), binary.LittleEndian.Uint64(remaining[8:])
			if sec > 12 || nsec >= 1e9 {
				t.Fatal("invalid original remainder")
			}
			difference := int64(sec)*1e9 + int64(nsec) - (startedNS + int64(12*time.Second) - sent.Nano())
			if difference < -int64(250*time.Millisecond) || difference > int64(250*time.Millisecond) {
				t.Fatal("aborted source lost original interruption-time deadline", difference)
			}
			row["raw_eintr_and_abi"], row["remainder_actually_read_only"], row["difference_ns"] = true, true, difference
			proofs[mode] = row
		}) {
			return
		}
	}
	boundedLossSave(t, "timer-abort.json", map[string]any{"passed": true, "production_qualified": false, "cases": proofs, "task_sha256": sum(program), "assembly_sha256": sum([]byte(assembly))})
}
