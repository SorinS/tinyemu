; CPUID — feature/vendor probe. Leaf 0 returns:
;   EAX = max input leaf
;   EBX, EDX, ECX = "vendor" string (12 bytes, swapped order)
; Our emulator returns "GenuineIntel" by convention.
; Expected: EBX = 0x756E6547 ("Genu"), EDX = 0x49656E69 ("ineI"), ECX = 0x6C65746E ("ntel")
bits 64
    xor eax, eax          ; leaf 0
    cpuid
    hlt
