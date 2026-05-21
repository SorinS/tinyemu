; Stack push/pop demo.
; After: stack returns to start, RAX=3, RBX=2, RCX=1 (LIFO order).
bits 64
    mov rax, 1
    mov rbx, 2
    mov rcx, 3
    push rax
    push rbx
    push rcx
    ; Now pop in reverse — values come out LIFO
    pop rax                 ; rax = 3
    pop rbx                 ; rbx = 2
    pop rcx                 ; rcx = 1
    hlt
