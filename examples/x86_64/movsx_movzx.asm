; Sign-extension vs zero-extension. 0x80 = -128 signed, 128 unsigned.
;   MOVSX -> RAX = 0xFFFFFFFFFFFFFF80 (sign-extended)
;   MOVZX -> RBX = 0x0000000000000080 (zero-extended)
; Expected: RAX = 0xFFFFFFFFFFFFFF80, RBX = 0x80
bits 64
    mov rcx, 0x80
    movsx rax, cl         ; sign-extend byte -> qword
    movzx rbx, cl         ; zero-extend byte -> qword
    hlt
