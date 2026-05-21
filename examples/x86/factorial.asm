; Iterative factorial. Expected: EAX = 10! = 3628800
bits 32
    mov eax, 1
    mov ecx, 10
loop_start:
    imul eax, ecx
    dec ecx
    jnz loop_start
    hlt
