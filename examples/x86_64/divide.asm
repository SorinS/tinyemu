; Unsigned division: DIV r/m64 divides RDX:RAX by operand.
;   quotient → RAX, remainder → RDX
; Compute 1000000 / 7  → quotient 142857, remainder 1
bits 64
    xor rdx, rdx            ; clear high half of dividend
    mov rax, 1000000        ; low half = 1,000,000
    mov rcx, 7
    div rcx                 ; RAX = quotient (142857), RDX = remainder (1)
    hlt
