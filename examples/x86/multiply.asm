; 32-bit signed multiplication.
; Expected: EAX = 6*7 = 42, EBX = -3*4 = -12 (0xFFFFFFF4)
bits 32
    mov eax, 6
    imul eax, 7

    mov ebx, -3
    imul ebx, 4
    hlt
