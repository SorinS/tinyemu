; POPCNT-style bit counting via shift loop (we don't advertise POPCNT
; in CPUID, so use the manual variant).
; Expected: RAX = popcount(0xABCD1234ABCD1234) = 32
bits 64
    mov rbx, 0xABCD1234ABCD1234
    xor rax, rax            ; counter
    mov rcx, 64
loop_start:
    shl rbx, 1              ; CF = top bit
    adc rax, 0              ; RAX += CF (1 if set, 0 if clear)
    loop loop_start         ; dec RCX, jump if RCX != 0
    hlt
