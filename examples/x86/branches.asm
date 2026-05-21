; Classify into <0, ==0, >0. Expected: EAX=1, EBX=-1 (0xFFFFFFFF), ECX=0.
bits 32
    mov edi, 42
    call classify
    mov eax, esi

    mov edi, -7
    call classify
    mov ebx, esi

    xor edi, edi
    call classify
    mov ecx, esi
    hlt

classify:
    test edi, edi
    jz zero
    js neg
    mov esi, 1
    ret
neg:
    mov esi, -1
    ret
zero:
    xor esi, esi
    ret
