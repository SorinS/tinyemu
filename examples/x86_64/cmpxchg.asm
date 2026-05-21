; CMPXCHG demo — the atomic compare-and-swap.
;   If RAX == [mem], then [mem] = RBX  and  ZF = 1.
;   Else                RAX = [mem]    and  ZF = 0.
; Expected: lock_val = 42 (CAS succeeded), R8 = 1 (ZF flag captured).
bits 64
    mov rax, 0              ; expected = 0
    mov rbx, 42             ; new      = 42
    lea rdi, [rel lock_val]
    lock cmpxchg [rdi], rbx ; lock_val was 0, becomes 42, ZF=1
    setz r8b                ; capture ZF into R8B
    movzx r8, r8b
    hlt

lock_val: dq 0
