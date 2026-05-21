; CMOVcc — conditional move. Branchless absolute value.
;   abs(x) = (x < 0) ? -x : x
; Expected: RAX = 42 (from -42), RBX = 42 (from 42), RCX = 0 (from 0).
bits 64
    mov rdi, -42
    call myabs
    mov rax, rdi            ; result in RDI

    mov rdi, 42
    call myabs
    mov rbx, rdi

    xor rdi, rdi
    call myabs
    mov rcx, rdi
    hlt

myabs:                      ; in/out: RDI
    mov rsi, rdi
    neg rsi                 ; rsi = -rdi
    test rdi, rdi
    cmovs rdi, rsi          ; if rdi was negative (SF=1), rdi = rsi
    ret
