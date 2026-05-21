; Unsigned 32-bit DIV: divides EDX:EAX by r/m32.
;   quotient → EAX, remainder → EDX
; Expected: EAX = 142857, EDX = 1
bits 32
    xor edx, edx
    mov eax, 1000000
    mov ecx, 7
    div ecx
    hlt
