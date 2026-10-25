//go:build linux && amd64

package lifecycle

const nativeRestoreHoldAssembly = `.section .text
.global _start
_start:
    incq counter(%rip)
    jmp _start
.section .bss
.balign 8
counter: .skip 8
.section .note.GNU-stack,"",@progbits
`
