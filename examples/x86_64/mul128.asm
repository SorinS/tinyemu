; 128-bit multiply via MUL: RDX:RAX = RAX * operand.
; 0xFFFFFFFFFFFFFFFF * 2 = 0x1_FFFFFFFFFFFFFFFE
; Expected: RAX = 0xFFFFFFFFFFFFFFFE, RDX = 0x0000000000000001
bits 64
    mov rax, 0xFFFFFFFFFFFFFFFF
    mov rcx, 2
    mul rcx               ; RDX:RAX = unsigned RAX * RCX
    hlt
