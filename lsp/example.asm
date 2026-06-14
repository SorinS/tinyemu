; asm-lsp demo. Open this in Neovim with the server attached:
;   - valid lines: hover (K) shows the encoded bytes + operand forms
;   - `mov rax, rbx` -> hover shows "48 89 d8 (3 bytes)"
;   - the typo below gets a red diagnostic
;   - start typing a mnemonic and trigger completion

BITS 64

start:
    xor   eax, eax
    mov   rax, rbx
    add   rax, [rbx + rcx*4 + 8]
    lea   rdi, [rbx + 8]
    push  r12
    mov   dword [rax], 1
    cmp   rax, rbx
    je    done
    inc   rcx
    jmp   start
done:
    mov rax, rbx          ; <-- typo: unknown instruction (red error)
    ret
