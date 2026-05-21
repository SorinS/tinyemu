; Euclidean GCD in 32-bit. Expected: EAX = gcd(252, 105) = 21
bits 32
    mov eax, 252
    mov ebx, 105
loop_start:
    test ebx, ebx
    jz done
    xor edx, edx
    div ebx
    mov eax, ebx
    mov ebx, edx
    jmp loop_start
done:
    hlt
