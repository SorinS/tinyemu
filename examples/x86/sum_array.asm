; Sum a dword array. Expected: EAX = 1+2+3+...+10 = 55
bits 32
    ; Array of 10 dwords: 1..10 at 0x700
    mov edi, 0x700
    mov ecx, 1
.fill:
    mov [edi], ecx
    add edi, 4
    inc ecx
    cmp ecx, 11
    jl .fill

    mov esi, 0x700
    mov ecx, 10
    xor eax, eax
.sum:
    add eax, [esi]
    add esi, 4
    dec ecx
    jnz .sum
    hlt
