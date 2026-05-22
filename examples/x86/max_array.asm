; Find max value in a packed dword array. Expected: EAX = 97
bits 32
    ; Build array at 0x600: [13, 42, 7, 97, 88, 3, 64]
    mov edi, 0x600
    mov dword [edi+0],  13
    mov dword [edi+4],  42
    mov dword [edi+8],   7
    mov dword [edi+12], 97
    mov dword [edi+16], 88
    mov dword [edi+20],  3
    mov dword [edi+24], 64

    mov ecx, 7            ; count
    mov esi, 0x600        ; ptr
    mov eax, [esi]        ; max = first
    add esi, 4
    dec ecx
.loop:
    mov ebx, [esi]
    cmp ebx, eax
    jle .skip
    mov eax, ebx
.skip:
    add esi, 4
    dec ecx
    jnz .loop
    hlt
