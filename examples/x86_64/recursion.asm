; Recursive sum(n) = n + sum(n-1), sum(0)=0
; Expected: RAX = sum(10) = 55
bits 64
    mov rdi, 10
    call sum
    hlt

sum:
    test rdi, rdi
    jz base                 ; sum(0) = 0 (RAX already 0 on first call)
    push rdi
    dec rdi
    call sum                ; RAX = sum(n-1)
    pop rdi
    add rax, rdi            ; RAX = sum(n-1) + n
    ret
base:
    xor rax, rax
    ret
