; Iterative Fibonacci. Expected: EAX = fib(20) = 6765
bits 32
    xor eax, eax            ; a = 0
    mov ebx, 1              ; b = 1
    mov ecx, 20
loop_start:
    test ecx, ecx
    jz done
    mov edx, ebx
    add ebx, eax
    mov eax, edx
    dec ecx
    jmp loop_start
done:
    hlt
