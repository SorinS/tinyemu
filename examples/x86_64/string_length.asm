; strlen-equivalent: count bytes from RDI until a NUL byte.
; RAX = length on exit. Uses REPNE SCASB.
bits 64
    lea rdi, [rel my_string]
    mov rcx, -1             ; max iterations (effectively unlimited)
    xor al, al              ; byte to scan for
    cld                     ; forward direction
    repne scasb             ; while *rdi != AL, rcx--, rdi++
    ; RCX was decremented by (length+1); compute length:
    mov rax, -2
    sub rax, rcx            ; length = -2 - RCX  (= original -1 - RCX - 1)
    hlt

my_string:
    db "Hello, World!", 0
