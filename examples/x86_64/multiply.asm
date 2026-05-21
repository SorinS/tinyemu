; Signed and unsigned 64-bit multiplication.
; Expected results:
;   RAX = 6  * 7         = 42
;   RBX = -3 * 4 (s64)   = -12 (0xFFFFFFFFFFFFFFF4)
;   RCX = 0x100000000 * 2 = 0x200000000
bits 64
    mov rax, 6
    imul rax, 7

    mov rbx, -3
    imul rbx, 4

    mov rcx, 0x100000000
    imul rcx, 2
    hlt
