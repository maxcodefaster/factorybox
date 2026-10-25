//go:build linux && arm64

package lifecycle

const nativeRestoreHoldAssembly = `.section .text
.global _start
.balign 4
_start:
    adrp x1, counter
    add x1, x1, :lo12:counter
counter_loop:
    ldr x0, [x1]
    add x0, x0, #1
    str x0, [x1]
    b counter_loop
.section .bss
.balign 8
counter: .skip 8
.section .note.GNU-stack,"",@progbits
`
