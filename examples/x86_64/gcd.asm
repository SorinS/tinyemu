; Euclidean GCD: RAX = gcd(252, 105) = 21
bits 64
    mov rax, 252
    mov rbx, 105
loop_start:
    test rbx, rbx
    jz done
    xor rdx, rdx            ; clear high half
    div rbx                 ; quotient → RAX, remainder → RDX
    mov rax, rbx            ; a = b
    mov rbx, rdx            ; b = a mod b
    jmp loop_start
done:
    hlt                     ; RAX = gcd
