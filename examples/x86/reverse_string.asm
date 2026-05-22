; Reverse the bytes of a string in place. Expected after HLT:
;   buffer at 0x500 reads "olleh\0" (was "hello\0")
;   EAX = 5 (length used)
bits 32
    mov esi, 0x500
    ; write "hello\0"
    mov byte [esi+0], 'h'
    mov byte [esi+1], 'e'
    mov byte [esi+2], 'l'
    mov byte [esi+3], 'l'
    mov byte [esi+4], 'o'
    mov byte [esi+5], 0

    ; ECX = strlen
    mov edi, esi
    xor ecx, ecx
.scan:
    cmp byte [edi], 0
    je .done_scan
    inc edi
    inc ecx
    jmp .scan
.done_scan:
    mov eax, ecx          ; save length in EAX for the test
    dec edi               ; edi -> last char

.swap:
    cmp esi, edi
    jae .done
    mov al, [esi]
    mov bl, [edi]
    mov [esi], bl
    mov [edi], al
    inc esi
    dec edi
    jmp .swap
.done:
    mov eax, ecx          ; restore length
    hlt
