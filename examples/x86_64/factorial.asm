; Iterative factorial: RAX = 10! = 3628800
bits 64
    mov rax, 1              ; accumulator
    mov rcx, 10             ; counter (multiplier)
loop_start:
    imul rax, rcx
    dec rcx
    jnz loop_start          ; loop while rcx != 0
    hlt
