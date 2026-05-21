; Exercise the extended 64-bit registers R8-R15. These require REX.B
; (or REX.R) to encode, so this also verifies the REX prefix path.
; Expected: each R8..R15 holds a distinctive constant.
bits 64
    mov r8,  0x0808080808080808
    mov r9,  0x0909090909090909
    mov r10, 0x0A0A0A0A0A0A0A0A
    mov r11, 0x0B0B0B0B0B0B0B0B
    mov r12, 0x0C0C0C0C0C0C0C0C
    mov r13, 0x0D0D0D0D0D0D0D0D
    mov r14, 0x0E0E0E0E0E0E0E0E
    mov r15, 0x0F0F0F0F0F0F0F0F
    ; A few ops across them:
    add r8, r15             ; r8 = 0x0808.. + 0x0F0F.. = 0x1717..
    sub r9, r10             ; r9 = 0x0909.. - 0x0A0A.. = 0xFEFEFEFEFEFEFEFF
    xor r11, r12            ; r11 = 0x0B... XOR 0x0C... = 0x0707..
    hlt
