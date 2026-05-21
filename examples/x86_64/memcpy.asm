; REP MOVSB demo: copy 16 bytes from src to dst, then compare.
; Expected: RAX = 0 (zero diff via REPE CMPSB), RCX = 0 (loop done)
bits 64
    lea rsi, [rel src]
    lea rdi, [rel dst]
    mov rcx, 16
    cld
    rep movsb               ; copy 16 bytes

    ; Verify with REPE CMPSB
    lea rsi, [rel src]
    lea rdi, [rel dst]
    mov rcx, 16
    repe cmpsb              ; sets ZF=1 if all equal
    setz al                 ; AL = 1 if equal
    movzx rax, al           ; RAX = 1
    hlt

src:
    db "AAAABBBBCCCCDDDD"
dst:
    db "0000000000000000"
