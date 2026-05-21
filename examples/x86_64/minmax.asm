; Find min and max in an array of qwords using CMOV.
; Array = {17, -3, 42, -100, 256, 0, 99}
; Expected: RAX = max = 256,  RBX = min = -100
bits 64
    lea rsi, [rel arr]
    mov rcx, count          ; loop counter
    mov rax, [rsi]          ; init max with arr[0]
    mov rbx, rax            ; init min
    add rsi, 8
    dec rcx
loop_start:
    test rcx, rcx
    jz done
    mov rdx, [rsi]
    cmp rdx, rax
    cmovg rax, rdx          ; rax = max(rax, rdx)
    cmp rdx, rbx
    cmovl rbx, rdx          ; rbx = min(rbx, rdx)
    add rsi, 8
    dec rcx
    jmp loop_start
done:
    hlt

count equ 7
arr:    dq 17, -3, 42, -100, 256, 0, 99
