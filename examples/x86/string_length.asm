; strlen via REPNE SCASB. Expected: EAX = 13 (length of "Hello, World!")
bits 32
    mov edi, my_string
    mov ecx, -1             ; max iterations
    xor al, al
    cld
    repne scasb
    ; ECX was decremented (length+1) times; length = -2 - ECX
    mov eax, -2
    sub eax, ecx
    hlt

my_string: db "Hello, World!", 0
