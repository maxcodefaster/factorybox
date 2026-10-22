/* Native AMD64 continuation. The private frame is exactly one page after
 * this RX page. Blocked entry plus ppoll's atomic signal-mask handoff keeps
 * a caught signal queued at capture from disappearing before the wait.
 * A bounded 25-ms poll slice rechecks the original MONOTONIC/BOOTTIME deadline
 * after a capture/restart or host suspend. EINTR is never retried.
 */
.section .text
.global timer_restart_entry
timer_restart_entry:
wait_again:
    mov $228, %eax
    mov frame+24(%rip), %rdi
    lea frame+56(%rip), %rsi
    syscall
    test %rax, %rax
    jne invalid_clock
    mov frame+56(%rip), %rax
    imul $1000000000, %rax
    add frame+64(%rip), %rax
    mov frame+16(%rip), %rdx
    sub %rax, %rdx
    jle already_elapsed
    mov %rdx, frame+112(%rip)
    cmp $25000000, %rdx
    jbe poll_slice
    mov $25000000, %edx
poll_slice:
    mov %rdx, frame+120(%rip)
    movq $0, frame+40(%rip)
    mov %rdx, frame+48(%rip)
    mov $271, %eax
    xor %edi, %edi
    xor %esi, %esi
    lea frame+40(%rip), %rdx
    lea frame+104(%rip), %r10
    mov $8, %r8d
    syscall
    test %rax, %rax
    je wait_again
    mov %rax, frame+72(%rip)
    cmp $-4, %rax
    jne restore_mask
    cmpq $0, frame+32(%rip)
    je restore_mask
    /* The kernel updates ppoll's remaining slice before running the caught
     * handler. Subtract only that elapsed slice from the original remaining
     * duration. Reading the clock here would incorrectly charge handler time
     * to the nanosleep remainder. */
    mov frame+40(%rip), %rax
    imul $1000000000, %rax
    add frame+48(%rip), %rax
    mov frame+120(%rip), %rdx
    sub %rax, %rdx
    js invalid_clock
    mov frame+112(%rip), %rax
    sub %rdx, %rax
    mov %rax, %rdx
    jg split_remainder
    xor %edx, %edx
split_remainder:
    mov %rdx, %rax
    xor %edx, %edx
    mov $1000000000, %ecx
    div %rcx
    mov %rax, frame+40(%rip)
    mov %rdx, frame+48(%rip)
    lea frame+40(%rip), %rax
    mov %rax, frame+360(%rip)
    movq $16, frame+368(%rip)
    mov frame+32(%rip), %rax
    mov %rax, frame+376(%rip)
    movq $16, frame+384(%rip)
    mov $39, %eax
    syscall
    mov %rax, %rdi
    mov $311, %eax
    lea frame+360(%rip), %rsi
    mov $1, %edx
    lea frame+376(%rip), %r10
    mov $1, %r8d
    xor %r9d, %r9d
    syscall
    cmp $16, %rax
    je restore_mask
    cmp $0, %rax
    jl copy_error
    mov $-14, %rax
copy_error:
    mov %rax, frame+72(%rip)
    jmp restore_mask
already_elapsed:
    movq $0, frame+72(%rip)
restore_mask:
    mov $14, %eax
    mov $2, %edi
    lea frame+104(%rip), %rsi
    xor %edx, %edx
    mov $8, %r10d
    syscall
    test %rax, %rax
    jne invalid_clock
    mov saved+152(%rip), %rsp
    pushq saved+144(%rip)
    popfq
    mov frame+80(%rip), %rax
    mov %rax, -8(%rsp)
    mov saved+0(%rip), %r15
    mov saved+8(%rip), %r14
    mov saved+16(%rip), %r13
    mov saved+24(%rip), %r12
    mov saved+32(%rip), %rbp
    mov saved+40(%rip), %rbx
    mov saved+48(%rip), %r11
    mov saved+56(%rip), %r10
    mov saved+64(%rip), %r9
    mov saved+72(%rip), %r8
    mov saved+88(%rip), %rcx
    mov saved+96(%rip), %rdx
    mov saved+104(%rip), %rsi
    mov saved+112(%rip), %rdi
    mov frame+72(%rip), %rax
    jmp *saved+128(%rip)
invalid_clock:
    ud2
.set frame, timer_restart_entry+4096
.set saved, frame+128
.section .note.GNU-stack,"",@progbits
