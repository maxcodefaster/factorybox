/* ARM64 native kernel wait. Signal-mask handoff occurs at the actual
 * clock_nanosleep entry under ptrace. The kernel owns remainder writes.
 * All original registers, NZCV and SP are restored without stack writes.
 */
.section .text
.global timer_restart_entry, timer_return_branch, timer_kernel_wait, timer_kernel_return, timer_code_end
.balign 4
timer_restart_entry:
    adr x17, frame
    ldr x16, [x17, #96]
    cmp x16, #1
    b.eq absolute_deadline
    ldr x0, [x17, #24]
    add x1, x17, #56
    mov x8, #113
    svc #0
    cbnz x0, invalid_clock
    adr x17, frame
    ldr x0, [x17, #56]
    mov x16, #0xca00
    movk x16, #0x3b9a, lsl #16
    mul x0, x0, x16
    ldr x1, [x17, #64]
    add x0, x0, x1
    ldr x1, [x17, #16]
    subs x0, x1, x0
    b.le already_elapsed
    b split_timespec
absolute_deadline:
    ldr x0, [x17, #16]
split_timespec:
    mov x16, #0xca00
    movk x16, #0x3b9a, lsl #16
    udiv x1, x0, x16
    msub x2, x1, x16, x0
    str x1, [x17, #40]
    str x2, [x17, #48]
    ldr x16, [x17, #376]
    mov sp, x16
    ldr x16, [x17, #392]
    msr nzcv, x16
    ldr x0, [x17, #24]
    ldr x1, [x17, #96]
    add x2, x17, #40
    ldr x3, [x17, #32]
    mov x8, #115
timer_kernel_wait:
    svc #0
timer_kernel_return:
    b finish
already_elapsed:
    mov x0, #0
finish:
    adr x17, frame
    str x0, [x17, #72]
    ldr x16, [x17, #376]
    mov sp, x16
    ldr x16, [x17, #392]
    msr nzcv, x16
    ldp x1, x2, [x17, #136]
    ldp x3, x4, [x17, #152]
    ldp x5, x6, [x17, #168]
    ldp x7, x8, [x17, #184]
    ldp x9, x10, [x17, #200]
    ldp x11, x12, [x17, #216]
    ldp x13, x14, [x17, #232]
    ldp x15, x16, [x17, #248]
    ldp x18, x19, [x17, #272]
    ldp x20, x21, [x17, #288]
    ldp x22, x23, [x17, #304]
    ldp x24, x25, [x17, #320]
    ldp x26, x27, [x17, #336]
    ldp x28, x29, [x17, #352]
    ldr x30, [x17, #368]
    ldr x0, [x17, #72]
    ldr x17, [x17, #264]
timer_return_branch:
    b .
invalid_clock:
    udf #0
timer_code_end:
.set frame, timer_restart_entry+4096
.section .note.GNU-stack,"",@progbits
