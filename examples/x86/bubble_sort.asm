; Bubble sort 6 dwords ascending. After HLT, [0x800..0x800+24] is sorted.
; Expected (sanity): EAX = 1 (smallest), EDX = 64 (largest).
bits 32
    ; Build input [42, 7, 97, 1, 64, 13] at 0x800
    mov edi, 0x800
    mov dword [edi+0],  42
    mov dword [edi+4],   7
    mov dword [edi+8],  97
    mov dword [edi+12],  1
    mov dword [edi+16], 64
    mov dword [edi+20], 13

    mov ecx, 6            ; n
    dec ecx               ; passes = n-1
.pass:
    mov esi, 0x800
    mov ebx, ecx          ; inner = ecx
.inner:
    mov eax, [esi]
    mov edx, [esi+4]
    cmp eax, edx
    jle .noswap
    mov [esi],   edx
    mov [esi+4], eax
.noswap:
    add esi, 4
    dec ebx
    jnz .inner
    dec ecx
    jnz .pass

    mov eax, [0x800]       ; min in EAX
    mov edx, [0x800+20]    ; max in EDX
    hlt
