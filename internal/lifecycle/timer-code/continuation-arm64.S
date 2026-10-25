/* ARM64 staged continuation. One RX page and one private RW frame.
 * Frame registers use user_pt_regs order at +128. No application stack
 * memory is borrowed. The final direct B is patched only after verifying
 * its exact template and a return PC within its signed 26-bit range.
 */
.section .text
.global timer_restart_entry, timer_return_branch, timer_code_end
.balign 4
timer_restart_entry:
wait_again:
    adr x17, frame
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
    str x0, [x17, #112]
    mov x16, #0x7840
    movk x16, #0x17d, lsl #16
    cmp x0, x16
    csel x0, x0, x16, ls
    str x0, [x17, #120]
    str xzr, [x17, #40]
    str x0, [x17, #48]
    mov x0, #0
    mov x1, #0
    add x2, x17, #40
    add x3, x17, #104
    mov x4, #8
    mov x8, #73
    svc #0
    cbz x0, wait_again
    adr x17, frame
    str x0, [x17, #72]
    cmn x0, #4
    b.ne restore_mask
    ldr x0, [x17, #32]
    cbz x0, restore_mask
    ldr x0, [x17, #40]
    mov x16, #0xca00
    movk x16, #0x3b9a, lsl #16
    mul x0, x0, x16
    ldr x1, [x17, #48]
    add x0, x0, x1
    ldr x1, [x17, #120]
    subs x1, x1, x0
    b.mi invalid_clock
    ldr x0, [x17, #112]
    subs x0, x0, x1
    csel x0, x0, xzr, gt
    udiv x1, x0, x16
    msub x2, x1, x16, x0
    str x1, [x17, #40]
    str x2, [x17, #48]
    add x0, x17, #40
    mov x1, #16
    stp x0, x1, [x17, #416]
    ldr x0, [x17, #32]
    stp x0, x1, [x17, #432]
    mov x8, #172
    svc #0
    adr x17, frame
    add x1, x17, #416
    mov x2, #1
    add x3, x17, #432
    mov x4, #1
    mov x5, #0
    mov x8, #271
    svc #0
    adr x17, frame
    cmp x0, #16
    b.eq restore_mask
    cmp x0, #0
    b.lt copy_error
    mov x0, #-14
copy_error:
    str x0, [x17, #72]
    b restore_mask
already_elapsed:
    str xzr, [x17, #72]
restore_mask:
    mov x0, #2
    add x1, x17, #104
    mov x2, #0
    mov x3, #8
    mov x8, #135
    svc #0
    cbnz x0, invalid_clock
restore_context:
    adr x17, frame
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
