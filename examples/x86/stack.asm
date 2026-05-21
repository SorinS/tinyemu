; Stack PUSH/POP LIFO. Expected: EAX=3, EBX=2, ECX=1.
bits 32
    mov eax, 1
    mov ebx, 2
    mov ecx, 3
    push eax
    push ebx
    push ecx
    pop eax
    pop ebx
    pop ecx
    hlt
