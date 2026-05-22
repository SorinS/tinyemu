; Primality test by trial division up to sqrt(n).
; Test value: 97. Expected: EAX = 1 (prime).
;   EAX = 0 means composite; EAX = 1 means prime.
bits 32
    mov edx, 97           ; n
    cmp edx, 2
    jl .composite
    mov ecx, 2
.loop:
    ; if ecx*ecx > n: prime
    mov eax, ecx
    mul ecx               ; EDX:EAX = ecx*ecx — but mul clobbers EDX!
    ; Use a sandbox: save n in EBP first.
    ; (Restart with saner allocation.)
    jmp .restart
.restart:
    mov ebp, 97           ; n preserved
    mov ecx, 2            ; divisor
.try:
    ; if ecx*ecx > n -> prime
    mov eax, ecx
    imul eax, ecx
    cmp eax, ebp
    jg .prime
    ; n % ecx == 0 -> composite
    mov eax, ebp
    xor edx, edx
    div ecx
    test edx, edx
    je .composite
    inc ecx
    jmp .try
.prime:
    mov eax, 1
    hlt
.composite:
    xor eax, eax
    hlt
