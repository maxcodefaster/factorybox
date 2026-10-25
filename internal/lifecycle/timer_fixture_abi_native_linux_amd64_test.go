//go:build linux && amd64

package lifecycle

import (
	"os"
	"strings"
	"testing"
)

func nativeRelativeTimerAssembly() string { return timerNativeAssembly }

// Derive a separate signal fixture without changing the original required
// timer assembly. It requires one handler and raw -EINTR, retaining every
// register, stack, flags and read-only request assertion from that task.
func nativeTimerSignalAssembly() string {
	assembly := strings.Replace(timerNativeAssembly, "_start:\n", `_start:
    mov $13, %eax
    mov $10, %edi
    lea signal_action(%rip), %rsi
    xor %edx, %edx
    mov $8, %r10d
    syscall
    test %rax, %rax
    jne failed
`, 1)
	assembly = strings.Replace(assembly, "cmpq $0, actual_result(%rip)", "cmpq $-4, actual_result(%rip)", 1)
	assembly = strings.Replace(assembly, "    mov $257, %eax\n", "    cmpq $1, signal_count(%rip)\n    jne failed\n    mov $257, %eax\n", 1)
	// Keep the successful task alive after its existing assertions, marker
	// write and fsync. Otherwise an immediate correct EINTR can exit before
	// the delivered resume command publishes its live-process receipt.
	assembly = strings.Replace(assembly, "    xor %edi, %edi\n    jmp exit_task\n", "signal_success_hold:\n    mov $34, %eax\n    syscall\n    jmp signal_success_hold\n", 1)
	assembly = strings.Replace(assembly, ".section .rodata\n", `signal_handler:
    incq signal_count(%rip)
    ret
signal_restorer:
    mov $15, %eax
    syscall
.section .rodata
signal_action: .quad signal_handler, 0x04000000, signal_restorer, 0
`, 1)
	return assembly + ".section .bss\n.balign 8\nsignal_count: .skip 8\n"
}

// A one-second handler makes the interruption-time remainder distinguishable
// from a remainder incorrectly computed only after that handler returns.
func nativeTimerRemainderAssembly() string {
	assembly := nativeTimerSignalAssembly()
	assembly = strings.Replace(assembly, "    mov %rsp, initial_sp(%rip)\n", `    mov $228, %eax
    mov $1, %edi
    lea sleep_started(%rip), %rsi
    syscall
    test %rax, %rax
    jne failed
    mov %rsp, initial_sp(%rip)
`, 1)
	assembly = strings.Replace(assembly, "    xor %r10d, %r10d\n", "    lea remaining(%rip), %r10\n", 1)
	assembly = strings.Replace(assembly, "    test %r10, %r10\n    jne failed\n", "    lea remaining(%rip), %rax\n    cmp %rax, %r10\n    jne failed\n", 1)
	assembly = strings.Replace(assembly, "signal_handler:\n    incq signal_count(%rip)\n    ret\n", `signal_handler:
    incq signal_count(%rip)
    mov $230, %eax
    mov $1, %edi
    xor %esi, %esi
    lea handler_delay(%rip), %rdx
    xor %r10d, %r10d
    syscall
    ret
`, 1)
	return assembly + ".section .rodata\nhandler_delay: .quad 1, 0\n.section .data\n.balign 8\nremaining: .quad 0x11111111, 0x22222222\n.section .bss\n.balign 8\nsleep_started: .skip 16\n"
}

// Keep the remainder on its own page: protecting it must not protect the
// signal counter, return-result cell or the task's ordinary completion data.
func nativeTimerRemainderProtectionAssembly() string {
	assembly := nativeTimerRemainderAssembly()
	assembly = strings.Replace(assembly, ".balign 8\nremaining: .quad 0x11111111, 0x22222222\n", ".balign 4096\nremaining: .quad 0x11111111, 0x22222222\n.zero 4080\n", 1)
	assembly = strings.Replace(assembly, "    lea handler_delay(%rip), %rdx\n    xor %r10d, %r10d\n    syscall\n    ret\n", `    lea handler_delay(%rip), %rdx
    xor %r10d, %r10d
    syscall
    mov $10, %eax
    lea remaining(%rip), %rdi
    mov $4096, %esi
    mov $1, %edx
    syscall
    test %rax, %rax
    jne failed
    ret
`, 1)
	assembly = strings.Replace(assembly, "failed:\n    mov $79, %edi\n", timerNativeFailureReceipt, 1)
	return assembly + ".section .rodata\nfailure_marker: .asciz \"/tmp/timer-failed\"\n.section .bss\n.balign 8\nfailure_state: .skip 64\n"
}

func TestNativeTimerRemainderProtection(t *testing.T) {
	var activate func(*testing.T, boundedLossStatus, View, bool) map[string]any
	if os.Getenv("FACTORYBOX_TIMER_KERNEL_PROTOTYPE") == "1" {
		activate = runNativeKernelActivationPrototype
	}
	nativeTimerRemainderProtection(t, activate)
}
