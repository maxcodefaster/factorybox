/* Native AMD64 continuation. One RX page, followed by one private RW page.
 * Only the original kernel deadline is retained. The original request, stack,
 * register values and raw syscall return contract remain the application's.
 * Activation restores the original signal mask at the native syscall entry.
 * The kernel writes the remainder before delivering an application handler.
 */
.section .text
.global timer_restart_entry
timer_restart_entry:
    cmpq $1, frame+96(%rip)
    je absolute_deadline
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
    mov %rdx, %rax
    jmp split_timespec
absolute_deadline:
    mov frame+16(%rip), %rax
split_timespec:
    xor %edx, %edx
    mov $1000000000, %ecx
    div %rcx
    mov %rax, frame+40(%rip)
    mov %rdx, frame+48(%rip)
    mov saved+152(%rip), %rsp
    pushq saved+144(%rip)
    popfq
    mov frame+80(%rip), %rax
    mov %rax, -8(%rsp)
    mov $230, %eax
    mov frame+24(%rip), %rdi
    mov frame+96(%rip), %rsi
    lea frame+40(%rip), %rdx
    mov frame+32(%rip), %r10
    syscall
    jmp restore_context
already_elapsed:
    mov $0, %eax
restore_context:
    mov %rax, frame+72(%rip)
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
