; Three ways to swap two registers.
; All three end with RAX=B-original, RBX=A-original, RCX=A-original, RDX=B-original
bits 64
    ; ---- method 1: XCHG (atomic on memory; here just a 64-bit reg swap) ----
    mov rax, 0x1111
    mov rbx, 0x2222
    xchg rax, rbx           ; RAX=0x2222, RBX=0x1111

    ; ---- method 2: temp register ----
    mov rcx, 0x1111
    mov rdx, 0x2222
    mov rdi, rcx
    mov rcx, rdx
    mov rdx, rdi            ; RCX=0x2222, RDX=0x1111

    ; ---- method 3: XOR triple-swap (no temp) ----
    mov r8, 0xAAAA
    mov r9, 0xBBBB
    xor r8, r9
    xor r9, r8
    xor r8, r9              ; R8=0xBBBB, R9=0xAAAA
    hlt
