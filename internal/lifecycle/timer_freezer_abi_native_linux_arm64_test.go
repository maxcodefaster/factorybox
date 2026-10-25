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

	"factorybox/internal/timerstate"
	"golang.org/x/sys/unix"
)

// Establish the kernel's restart-register ABI without any Factorybox timer
// preparation, checkpoint, restore or ptrace. The original strict raw fixture
// deliberately expects X8=clock_nanosleep, so its failure receipt identifies the
// native freezer's restart_syscall rewrite independently of the backend.
func TestNativeArm64TimerFreezerABI(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_TIMER_FREEZER_ABI") != "1" {
		t.Skip("owned native ARM64 freezer ABI control only")
	}
	status := boundedLossStatusNow(t)
	assembly := nativeARM64TimerAssembly(false, false, false)
	if err := os.WriteFile("/output/freezer-abi.S", []byte(assembly), 0644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("cc", "-nostdlib", "-static", "-Wl,--build-id=none",
		"/output/freezer-abi.S", "-o", "/output/freezer-abi").CombinedOutput(); err != nil {
		t.Fatal("compile unchanged raw ABI control", err, string(out))
	}
	program, err := os.ReadFile("/output/freezer-abi")
	if err != nil {
		t.Fatal(err)
	}
	source := timerReaderFixtureFiles(t, status, "freezer-abi", "exec /bin/freezer-abi", false,
		map[string][]byte{"freezer-abi": program})
	root, err := os.Open(fmt.Sprintf("/proc/%d/root", source.Runtime.PID))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	var before string
	for deadline := time.Now().Add(time.Second); ; {
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/syscall", source.Runtime.PID))
		if err == nil && strings.HasPrefix(string(data), strconv.Itoa(unix.SYS_CLOCK_NANOSLEEP)+" 0x1 0x0 ") {
			before = strings.TrimSpace(string(data))
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("native control never entered its original raw sleep", string(data), err)
		}
		time.Sleep(time.Millisecond)
	}
	birth, err := processBirth(source.Runtime.PID)
	if err != nil {
		t.Fatal(err)
	}
	ticks, err := strconv.ParseUint(birth, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	if err := captureNativeFreeze(source.Record, true); err != nil {
		t.Fatal(err)
	}
	frozen := true
	defer func() {
		if frozen {
			if err := captureNativeFreeze(source.Record, false); err != nil {
				t.Error("release only the original native control freezer", err)
			}
		}
	}()
	observation, err := timerstate.ReadWithRegisters([]timerstate.Task{{PID: source.Runtime.PID, StartTicks: ticks}})
	if err != nil || len(observation.States) != 1 || !observation.ProgramReleased || !observation.MapReleased {
		t.Fatal("native freezer timer read was not exact and released", err)
	}
	state := observation.States[0]
	if !state.RelativeTimer || state.Remainder != 0 || state.Registers == nil {
		t.Fatal("native control lacks its real NULL-remainder sleep", state)
	}
	offset, err := timerOffset(source.Runtime.PID, state.Clock)
	if err != nil {
		t.Fatal("read the original native control time namespace", err)
	}
	if err := captureNativeFreeze(source.Record, false); err != nil {
		t.Fatal(err)
	}
	frozen = false
	var after string
	for deadline := time.Now().Add(time.Second); ; {
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/syscall", source.Runtime.PID))
		if err == nil && strings.HasPrefix(string(data), strconv.Itoa(unix.SYS_RESTART_SYSCALL)+" 0x1 0x0 ") {
			after = strings.TrimSpace(string(data))
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("native freezer did not expose its actual restart_syscall", string(data), err)
		}
		time.Sleep(time.Millisecond)
	}
	var failure []byte
	for deadline := time.Now().Add(16 * time.Second); ; {
		data, err := os.ReadFile(fmt.Sprintf("/proc/self/fd/%d/tmp/timer-failed", root.Fd()))
		if err == nil && len(data) == 64 {
			failure = data
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("unchanged raw control did not publish its native register mismatch", err)
		}
		time.Sleep(time.Millisecond)
	}
	words := make([]uint64, 8)
	for index := range words {
		words[index] = binary.LittleEndian.Uint64(failure[index*8:])
	}
	finishedNS := int64(words[6])*int64(time.Second) + int64(words[7])
	if words[0] != uint64(unix.SYS_RESTART_SYSCALL) || words[1] != 0 ||
		words[2] != 0xa0000000 || words[3] != words[2] ||
		finishedNS < state.ExpiresNS+offset || finishedNS-state.ExpiresNS-offset > int64(250*time.Millisecond) {
		t.Fatal("native freezer mismatch was not exactly X8 with a successful on-time return", words, state, offset, err)
	}
	captures, err := os.ReadDir(status.Lifecycle.Root + "/objects/freezer-abi/captures")
	if err != nil && !os.IsNotExist(err) || len(captures) != 0 {
		t.Fatal("native kernel control unexpectedly entered Factorybox capture", err)
	}
	boundedLossSave(t, "timer-freezer-abi.json", map[string]any{
		"passed": true, "no_timer_preparation_or_capture": true,
		"native_x8_rewrite": true, "raw_return_zero": true, "original_deadline": true,
		"before": before, "after": after, "kernel_observation": observation,
		"failure_words": words, "assembly_sha256": sum([]byte(assembly)), "task_sha256": sum(program),
	})
}
