; 32-bit shifts and rotates.
; Expected: EAX=0x80, EBX=1, ECX=-1 (0xFFFFFFFF), EDX=0x80000001
bits 32
    mov eax, 1
    shl eax, 7              ; 1 << 7 = 0x80

    mov ebx, 0x80
    shr ebx, 7              ; logical right

    mov ecx, -8
    sar ecx, 3              ; arithmetic right shift keeps the sign

    mov edx, 3
    ror edx, 1              ; rotate right by 1
    hlt
