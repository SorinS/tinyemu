; Conditional branches. Classifies a value into <0 / ==0 / >0 buckets.
; Expected: RAX = 1 (positive), RBX = -1 (negative), RCX = 0 (zero).
bits 64
    mov rdi, 42
    call classify
    mov rax, rsi            ; result of first call

    mov rdi, -7
    call classify
    mov rbx, rsi

    xor rdi, rdi
    call classify
    mov rcx, rsi
    hlt

classify:                   ; in: RDI; out: RSI = 1 / -1 / 0
    test rdi, rdi
    jz   zero
    js   negative
    mov rsi, 1
    ret
negative:
    mov rsi, -1
    ret
zero:
    xor rsi, rsi
    ret
