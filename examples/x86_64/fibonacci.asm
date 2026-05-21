; Iterative Fibonacci: RAX = fib(20) = 6765
;
; fib(n): a, b = 0, 1; repeat n times: a, b = b, a+b; return a.
bits 64
    xor rax, rax            ; a = 0
    mov rbx, 1              ; b = 1
    mov rcx, 20             ; iterations (n)
loop_start:
    test rcx, rcx
    jz done
    mov rdx, rbx            ; tmp = b
    add rbx, rax            ; b = a + b
    mov rax, rdx            ; a = tmp
    dec rcx
    jmp loop_start
done:
    hlt
