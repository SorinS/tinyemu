; REP STOSB — block memset. Fill 64 bytes at 0x900 with 0xAB.
; Then check first and last byte:
;   RAX = 0xAB (last byte read)
;   RBX = 0xAB (first byte read)
;   RCX = 0    (counter exhausted)
bits 64
    mov rdi, 0x900
    mov rcx, 64
    mov al, 0xAB
    rep stosb             ; writes RCX bytes of AL starting at RDI

    movzx rax, byte [0x900 + 63]   ; last byte
    movzx rbx, byte [0x900]        ; first byte
    ; RCX = 0 after rep
    hlt
