; Shifts and rotates on 64-bit registers.
; Expected:
;   RAX = 0x80      (1 << 7)
;   RBX = 0x1       (0x80 >> 7, logical)
;   RCX = -1        (0xFFFFFFFFFFFFFFFF; arithmetic shift right of -8 by 3)
;   RDX = 0x8000000000000001  (ROR 1 of 3)
bits 64
    mov rax, 1
    shl rax, 7              ; left shift

    mov rbx, 0x80
    shr rbx, 7              ; logical right shift

    mov rcx, -8
    sar rcx, 3              ; arithmetic right shift

    mov rdx, 3
    ror rdx, 1              ; rotate right by 1 → top bit set
    hlt
