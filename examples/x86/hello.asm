; Basic 32-bit arithmetic.  Expected: EAX = 142 = 0x8E
bits 32
    mov eax, 42
    mov ebx, 100
    add eax, ebx
    hlt
