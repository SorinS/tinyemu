; 32-bit REP MOVSB copy + REPE CMPSB verify.
; Expected: EAX = 1 (verify ZF set)
bits 32
    mov esi, src
    mov edi, dst
    mov ecx, 16
    cld
    rep movsb

    mov esi, src
    mov edi, dst
    mov ecx, 16
    repe cmpsb
    setz al
    movzx eax, al
    hlt

src: db "AAAABBBBCCCCDDDD"
dst: db "0000000000000000"
