; Function call via CALL/RET. SysV ABI: first arg in RDI, return in RAX.
; Function `square(n)` returns n*n.
; Expected: RAX = 49 (7*7)
bits 64
    mov rdi, 7
    call square
    hlt

square:                     ; RAX = RDI * RDI
    mov rax, rdi
    imul rax, rdi
    ret
