//go:build linux && arm64

package lifecycle

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestNativeArm64TimerFixtureABI(t *testing.T) {
	if os.Getenv("FACTORYBOX_TIMER_ARM64_NATIVE") != "1" {
		t.Skip("explicit owned native ARM64 fixture control only")
	}
	for _, mode := range []string{"relative", "signal", "remainder", "protected"} {
		t.Run(mode, func(t *testing.T) {
			signal := mode != "relative"
			remainder := mode == "remainder" || mode == "protected"
			assembly := nativeARM64TimerAssembly(signal, remainder, mode == "protected")
			base := "/output/arm64-timer-fixture-" + mode
			if err := os.WriteFile(base+".S", []byte(assembly), 0644); err != nil {
				t.Fatal(err)
			}
			output, err := exec.Command("cc", "-nostdlib", "-static", "-Wl,--build-id=none",
				base+".S", "-o", base).CombinedOutput()
			if writeErr := os.WriteFile(base+".build.log", output, 0644); writeErr != nil {
				t.Fatal(writeErr)
			}
			if err != nil {
				t.Fatal("assemble actual ARM64 ABI control", err, string(output))
			}
			nm, err := exec.Command("nm", "-n", base).Output()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(base+".symbols.txt", nm, 0644); err != nil {
				t.Fatal(err)
			}
			symbols := map[string]uint64{}
			for _, line := range strings.Split(string(nm), "\n") {
				fields := strings.Fields(line)
				if len(fields) != 3 {
					continue
				}
				address, err := strconv.ParseUint(fields[0], 16, 64)
				if err != nil {
					t.Fatal(err)
				}
				symbols[fields[2]] = address
			}
			request := symbols["request"]
			if request == 0 || request%4096 != 0 || symbols["actual_result"] != request+4096 ||
				symbols["initial_sp"] != request+4360 || symbols["remaining"] != request+8192 {
				t.Fatal("compiled ARM64 fixture pages differ from the exact ABI", symbols)
			}
			for register := 1; register <= 30; register++ {
				if symbols[fmt.Sprintf("saved_x%d", register)] != request+4120+uint64(register-1)*8 {
					t.Fatal("compiled ARM64 register slot changed", register, symbols)
				}
			}
			for _, marker := range []string{"/tmp/timer-finished", "/tmp/timer-failed"} {
				if _, err := os.Stat(marker); !os.IsNotExist(err) {
					t.Fatal("private control marker already exists", marker, err)
				}
			}
			child := exec.Command(base)
			if err := child.Start(); err != nil {
				t.Fatal(err)
			}
			pidfd, err := unix.PidfdOpen(child.Process.Pid, 0)
			if err != nil {
				child.Process.Kill()
				child.Wait()
				t.Fatal(err)
			}
			waited := false
			t.Cleanup(func() {
				if !waited {
					unix.PidfdSendSignal(pidfd, unix.SIGKILL, nil, 0)
					child.Wait()
				}
				unix.Close(pidfd)
				for _, marker := range []string{"/tmp/timer-finished", "/tmp/timer-failed"} {
					if err := os.Remove(marker); err != nil && !os.IsNotExist(err) {
						t.Error("remove only this private control marker", err)
					}
				}
			})
			for deadline := time.Now().Add(time.Second); ; {
				call, err := os.ReadFile(fmt.Sprintf("/proc/%d/syscall", child.Process.Pid))
				fields := strings.Fields(string(call))
				if err == nil && len(fields) > 4 && fields[0] == strconv.Itoa(unix.SYS_CLOCK_NANOSLEEP) &&
					fields[1] == "0x1" && fields[2] == "0x0" {
					break
				}
				if time.Now().After(deadline) {
					failure, _ := os.ReadFile("/tmp/timer-failed")
					t.Fatalf("raw ARM64 control never entered its exact original wait: %s %v failure=%x", call, err, failure)
				}
				time.Sleep(time.Millisecond)
			}
			start, err := timerMemoryRead(child.Process.Pid, symbols["sleep_started"], 16)
			if err != nil {
				t.Fatal(err)
			}
			started := int64(binary.LittleEndian.Uint64(start))*1e9 + int64(binary.LittleEndian.Uint64(start[8:]))
			var sent unix.Timespec
			if signal {
				if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &sent); err != nil {
					t.Fatal(err)
				}
				if err := unix.PidfdSendSignal(pidfd, unix.SIGUSR1, nil, 0); err != nil {
					t.Fatal(err)
				}
			}
			wait := 15 * time.Second
			if signal {
				wait = 3 * time.Second
			}
			var finished []byte
			for deadline := time.Now().Add(wait); ; {
				if failure, err := os.ReadFile("/tmp/timer-failed"); err == nil {
					t.Fatalf("raw ARM64 control failed its original return/register/SP/NZCV/request assertions: %x", failure)
				}
				finished, err = os.ReadFile("/tmp/timer-finished")
				if err == nil && len(finished) == 16 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("raw ARM64 control did not finish its exact ABI assertions", err)
				}
				time.Sleep(time.Millisecond)
			}
			finishedNS := int64(binary.LittleEndian.Uint64(finished))*1e9 + int64(binary.LittleEndian.Uint64(finished[8:]))
			if !signal {
				if delta := finishedNS - started - int64(12*time.Second); delta < 0 || delta > int64(250*time.Millisecond) {
					t.Fatal("uncaptured relative control changed its original deadline", delta)
				}
				if err := child.Wait(); err != nil {
					t.Fatal("relative control did not really exit successfully", err)
				}
				waited = true
			} else if remainder {
				value, err := timerMemoryRead(child.Process.Pid, symbols["remaining"], 16)
				if err != nil {
					t.Fatal(err)
				}
				seconds, nanos := binary.LittleEndian.Uint64(value), binary.LittleEndian.Uint64(value[8:])
				if seconds > 12 || nanos >= 1e9 {
					t.Fatal("raw control has an invalid actual kernel remainder", seconds, nanos)
				}
				expected := started + int64(12*time.Second) - sent.Nano()
				if delta := int64(seconds)*1e9 + int64(nanos) - expected; delta < -int64(250*time.Millisecond) || delta > int64(250*time.Millisecond) {
					t.Fatal("raw control lost the interruption-time remainder", delta)
				}
				if finishedNS-sent.Nano() < int64(950*time.Millisecond) {
					t.Fatal("raw handler did not impose its actual one-second delay")
				}
				if mode == "protected" {
					mappings, err := timerMappings(child.Process.Pid)
					if err != nil {
						t.Fatal(err)
					}
					readOnly := false
					for _, mapping := range mappings {
						if mapping.start <= symbols["remaining"] && symbols["remaining"]+16 <= mapping.end {
							readOnly = strings.HasPrefix(mapping.permissions, "r") && !strings.Contains(mapping.permissions, "w")
						}
					}
					if !readOnly {
						t.Fatal("raw handler did not actually protect its separate remainder page")
					}
				}
			}
			t.Log("actual native raw return, X1-X30, SP, NZCV and unchanged read-only request passed")
		})
	}
}
