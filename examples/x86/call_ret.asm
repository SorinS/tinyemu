; 32-bit calling convention via stack (cdecl-style).
; square(n) = n*n.  Caller pushes arg, callee pops via ESP.
; Expected: EAX = 49 (7*7)
bits 32
    push dword 7
    call square
    add esp, 4              ; clean up arg (cdecl)
    hlt

square:                     ; arg at [esp+4]
    mov eax, [esp + 4]
    imul eax, eax
    ret
