; Bit-test family: BT, BTS, BTR, BTC and bit-scan BSF/BSR.
; Expected:
;   RAX=1  (BT  of bit 4 in 0x10 → CF=1)
;   RBX=0x14 (BTS  bit 2 in 0x10 → 0x14)
;   RCX=0x18 (BTR  bit 2 in 0x1c → 0x18)
;   RDX=0x10 (BTC  bit 2 in 0x14 → 0x10)
;   RDI=4    (BSF  of 0x10 → bit 4 is lowest set)
;   RSI=4    (BSR  of 0x10 → bit 4 is highest set)
bits 64
    mov r8, 0x10
    bt  r8, 4
    setc al
    movzx rax, al

    mov rbx, 0x10
    bts rbx, 2

    mov rcx, 0x1C
    btr rcx, 2

    mov rdx, 0x14
    btc rdx, 2

    bsf rdi, r8
    bsr rsi, r8
    hlt
