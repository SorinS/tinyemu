; POPCNT — hardware bit population count.
; Expected: RAX = 8, RBX = 64, RCX = 0
bits 64
    mov rdx, 0x0F0F0F0F0F0F0F0F   ; alternating-nibble pattern
    popcnt rax, rdx               ; 32 set bits? no — half of each nibble = 4 per byte * 8 bytes = 32?
    ; Actually 0x0F = 0b00001111 (4 bits), so per byte: 4. 8 bytes: 32.
    ; Pinned check below uses a clearer pattern.

    mov rdx, 0xFF
    popcnt rax, rdx               ; 8 ones in 0xFF

    mov rdx, 0xFFFFFFFFFFFFFFFF
    popcnt rbx, rdx               ; 64 ones

    mov rdx, 0
    popcnt rcx, rdx               ; 0 ones
    hlt
