//go:build linux && arm64

package lifecycle

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func nativeRelativeTimerAssembly() string {
	// The shared natural-completion test first performs a native freezer/probe/
	// thaw before Factorybox capture. The independent freezer ABI control
	// proves that Linux has already rewritten X8 to restart_syscall there.
	// Check that exact admitted value; uncaptured and signal controls keep 115.
	return nativeARM64TimerAssemblyWithX8(false, false, false, unix.SYS_RESTART_SYSCALL)
}
func nativeTimerSignalAssembly() string { return nativeARM64TimerAssembly(true, false, false) }
func nativeTimerRemainderAssembly() string {
	return nativeARM64TimerAssembly(true, true, false)
}
func nativeTimerRemainderProtectionAssembly() string {
	return nativeARM64TimerAssembly(true, true, true)
}

func TestNativeTimerRemainderProtection(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_TIMER_REMAINDER_PROTECTION") == "1" &&
		os.Getenv("FACTORYBOX_TIMER_KERNEL_PROTOTYPE") == "1" {
		t.Fatal("the historical test-only kernel activation prototype is an AMD64 fixture, not ARM64 qualification")
	}
	nativeTimerRemainderProtection(t, nil)
}

// The request occupies its own page, protected before the real wait. The next
// page receives the raw return, NZCV, SP and all X1-X30 before any comparison
// changes flags or a saved register. No application stack memory is borrowed.
// A separate third page holds the kernel remainder, so the handler can protect
// it without protecting the captured registers, counter or completion marker.
func nativeARM64TimerAssembly(signal, remainder, protect bool) string {
	return nativeARM64TimerAssemblyWithX8(signal, remainder, protect, unix.SYS_CLOCK_NANOSLEEP)
}

func nativeARM64TimerAssemblyWithX8(signal, remainder, protect bool, expectedX8 uint64) string {
	if remainder && !signal || protect && !remainder {
		panic("invalid native ARM64 timer fixture")
	}
	if expectedX8 != unix.SYS_CLOCK_NANOSLEEP &&
		(expectedX8 != unix.SYS_RESTART_SYSCALL || signal || remainder || protect) {
		panic("invalid exact native ARM64 return syscall register")
	}
	var code strings.Builder
	code.WriteString(`.section .text
.global _start
.balign 4
_start:
    adrp x0, request
    add x0, x0, :lo12:request
    mov x1, #4096
    mov x2, #1
    mov x8, #226
    svc #0
    cbnz x0, failed
`)
	if signal {
		code.WriteString(`    mov x0, #10
    adrp x1, signal_action
    add x1, x1, :lo12:signal_action
    mov x2, #0
    mov x3, #8
    mov x8, #134
    svc #0
    cbnz x0, failed
`)
	}
	code.WriteString(`    mov x0, #1
    adrp x1, sleep_started
    add x1, x1, :lo12:sleep_started
    mov x8, #113
    svc #0
    cbnz x0, failed
    adrp x2, request
    add x2, x2, :lo12:request
    mov x0, sp
    str x0, [x2, #4360]
    mov x0, #1
    mov x1, #0
`)
	if remainder {
		code.WriteString("    adrp x3, remaining\n    add x3, x3, :lo12:remaining\n")
	} else {
		code.WriteString("    mov x3, #0\n")
	}
	for register := 4; register <= 30; register++ {
		value := 0x1100 + register
		if register == 8 {
			value = 115
		}
		fmt.Fprintf(&code, "    mov x%d, #%d\n", register, value)
	}
	code.WriteString(`    mov x16, #0xa0000000
    msr nzcv, x16
    mov x16, #4368
    svc #0
returned:
    str x0, [x2, #4096]
    mrs x0, nzcv
    str x0, [x2, #4104]
    mov x0, sp
    str x0, [x2, #4112]
`)
	for register := 1; register <= 30; register++ {
		fmt.Fprintf(&code, "    str x%d, [x2, #%d]\n", register, 4120+(register-1)*8)
	}
	code.WriteString(`    adrp x16, actual_result
    add x16, x16, :lo12:actual_result
    ldr x0, [x16, #8]
    mov x1, #0xa0000000
    cmp x0, x1
    b.ne failed
    ldr x0, [x16, #16]
    ldr x1, [x16, #264]
    cmp x0, x1
    b.ne failed
    ldr x0, [x16]
`)
	if signal {
		code.WriteString("    mov x1, #-4\n")
	} else {
		code.WriteString("    mov x1, #0\n")
	}
	code.WriteString("    cmp x0, x1\n    b.ne failed\n")
	for register := 1; register <= 30; register++ {
		fmt.Fprintf(&code, "    ldr x0, [x16, #%d]\n", 24+(register-1)*8)
		switch {
		case register == 1:
			code.WriteString("    mov x1, #0\n")
		case register == 2:
			code.WriteString("    adrp x1, request\n    add x1, x1, :lo12:request\n")
		case register == 3 && remainder:
			code.WriteString("    adrp x1, remaining\n    add x1, x1, :lo12:remaining\n")
		case register == 3:
			code.WriteString("    mov x1, #0\n")
		case register == 8:
			fmt.Fprintf(&code, "    mov x1, #%d\n", expectedX8)
		default:
			fmt.Fprintf(&code, "    mov x1, #%d\n", 0x1100+register)
		}
		code.WriteString("    cmp x0, x1\n    b.ne failed\n")
	}
	code.WriteString(`    adrp x0, request
    add x0, x0, :lo12:request
    ldr x1, [x0]
    cmp x1, #12
    b.ne failed
    ldr x1, [x0, #8]
    cbnz x1, failed
`)
	if signal {
		code.WriteString(`    adrp x0, signal_count
    add x0, x0, :lo12:signal_count
    ldr x1, [x0]
    cmp x1, #1
    b.ne failed
`)
	}
	code.WriteString(`    mov x0, #-100
    adrp x1, marker
    add x1, x1, :lo12:marker
    mov x2, #193
    mov x3, #384
    mov x8, #56
    svc #0
    tbnz x0, #63, failed
    mov x19, x0
    mov x0, #1
    adrp x1, finish
    add x1, x1, :lo12:finish
    mov x8, #113
    svc #0
    cbnz x0, failed
    mov x0, x19
    adrp x1, finish
    add x1, x1, :lo12:finish
    mov x2, #16
    mov x8, #64
    svc #0
    cmp x0, #16
    b.ne failed
    mov x0, x19
    mov x8, #82
    svc #0
    cbnz x0, failed
`)
	if signal {
		code.WriteString(`signal_success_hold:
    mov x0, #0
    mov x1, #0
    mov x2, #0
    mov x3, #0
    mov x4, #8
    mov x8, #73
    svc #0
    b signal_success_hold
`)
	} else {
		code.WriteString("    mov x0, #0\n    mov x8, #93\n    svc #0\n")
	}
	code.WriteString(`failed:
    adrp x16, failure_state
    add x16, x16, :lo12:failure_state
    str x0, [x16]
    adrp x17, actual_result
    add x17, x17, :lo12:actual_result
    ldr x0, [x17]
    str x0, [x16, #8]
    mov x0, #0xa0000000
    str x0, [x16, #16]
    ldr x0, [x17, #8]
    str x0, [x16, #24]
    mov x0, sp
    str x0, [x16, #32]
    mrs x0, nzcv
    str x0, [x16, #40]
    mov x0, #1
    add x1, x16, #48
    mov x8, #113
    svc #0
    mov x0, #-100
    adrp x1, failure_marker
    add x1, x1, :lo12:failure_marker
    mov x2, #193
    mov x3, #384
    mov x8, #56
    svc #0
    mov x19, x0
    adrp x1, failure_state
    add x1, x1, :lo12:failure_state
    mov x2, #64
    mov x8, #64
    svc #0
    mov x0, x19
    mov x8, #82
    svc #0
    mov x0, #79
    mov x8, #93
    svc #0
signal_handler:
    adrp x0, signal_count
    add x0, x0, :lo12:signal_count
    ldr x1, [x0]
    add x1, x1, #1
    str x1, [x0]
`)
	if remainder {
		code.WriteString(`    mov x0, #1
    mov x1, #0
    adrp x2, handler_delay
    add x2, x2, :lo12:handler_delay
    mov x3, #0
    mov x8, #115
    svc #0
`)
	}
	if protect {
		code.WriteString(`    adrp x0, remaining
    add x0, x0, :lo12:remaining
    mov x1, #4096
    mov x2, #1
    mov x8, #226
    svc #0
    cbnz x0, failed
`)
	}
	code.WriteString(`    ret
.section .rodata
marker: .asciz "/tmp/timer-finished"
failure_marker: .asciz "/tmp/timer-failed"
.balign 8
signal_action: .quad signal_handler, 0, 0, 0
.section .data
.balign 4096
request: .quad 12, 0
.zero 4080
actual_result: .quad 0
saved_flags: .quad 0
saved_sp: .quad 0
`)
	for register := 1; register <= 30; register++ {
		fmt.Fprintf(&code, "saved_x%d: .quad 0\n", register)
	}
	code.WriteString(`initial_sp: .quad 0
signal_count: .quad 0
finish: .quad 0, 0
sleep_started: .quad 0, 0
failure_state: .zero 64
handler_delay: .quad 1, 0
.balign 4096
remaining: .quad 0x11111111, 0x22222222
.zero 4080
.section .note.GNU-stack,"",@progbits
`)
	return code.String()
}
