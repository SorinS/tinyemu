; LEA arithmetic. Expected: EAX = 21, EDX = 100
bits 32
    mov ebx, 2
    mov ecx, 7
    lea eax, [ebx + ecx*2 + 5]      ; 2 + 14 + 5 = 21
    lea edx, [ebx*4 + ecx*8 + 36]   ; 8 + 56 + 36 = 100
    hlt
