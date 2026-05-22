; BSF = bit scan forward (lowest set bit index).
; BSR = bit scan reverse (highest set bit index).
; Input  0x0000000100000080
;   BSF -> bit 7  (lowest)
;   BSR -> bit 32 (highest)
; Expected: RAX = 7, RBX = 32
bits 64
    mov rdx, 0x0000000100000080
    bsf rax, rdx
    bsr rbx, rdx
    hlt
