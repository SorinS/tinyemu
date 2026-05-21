; LEA — "load effective address" — is the fastest 3-operand arithmetic
; trick in x86: no memory access, just compute base+index*scale+disp.
; Expected:
;   RAX = 21    (RBX + RCX*2 + 5  with RBX=2 RCX=7)
;   RDX = 100   (RBX*4 + RCX*8 + 32 → 2*4 + 7*8 + 32 = 8 + 56 + 32 ... wait)
;   adjusted: RDX = 2*4 + 7*8 + 36 = 100
bits 64
    mov rbx, 2
    mov rcx, 7
    lea rax, [rbx + rcx*2 + 5]    ; = 2 + 14 + 5 = 21
    lea rdx, [rbx*4 + rcx*8 + 36] ; = 8 + 56 + 36 = 100
    ; rip-relative addressing (computes a pointer to data):
    lea rsi, [rel data]
    mov rdi, [rsi]                ; load the qword via the LEA'd pointer
    hlt

align 8
data: dq 0xDEADBEEFCAFEBABE
