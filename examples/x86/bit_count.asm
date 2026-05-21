; Count set bits in 0xABCD1234 (popcount). Expected: EAX = 14
bits 32
    mov ebx, 0xABCD1234
    xor eax, eax
    mov ecx, 32
loop_start:
    shl ebx, 1
    adc eax, 0
    loop loop_start
    hlt
