package asm

import (
	"fmt"

	"golang.org/x/arch/x86/x86asm"
)

// Disassemble decodes the first instruction in code for the given mode (16,
// 32, or 64) and returns its Intel-syntax text and length in bytes. Decoding
// is delegated to golang.org/x/arch/x86asm (the engine behind go tool
// objdump); this package contributes the assembler (text→bytes) half.
func Disassemble(code []byte, bits int) (text string, length int, err error) {
	inst, err := x86asm.Decode(code, bits)
	if err != nil {
		return "", 0, err
	}
	return x86asm.IntelSyntax(inst, 0, nil), inst.Len, nil
}

// DisassembleMode is Disassemble keyed by the assembler's Mode (Bits32 /
// Bits64) instead of a raw bit width, for symmetry with AssembleMode.
func DisassembleMode(code []byte, mode Mode) (text string, length int, err error) {
	return Disassemble(code, int(mode))
}

// DisassembleAll decodes every instruction in code, returning one Intel-syntax
// line per instruction. It stops and returns the lines decoded so far plus the
// error on the first byte it cannot decode.
func DisassembleAll(code []byte, bits int) ([]string, error) {
	var out []string
	for off := 0; off < len(code); {
		text, n, err := Disassemble(code[off:], bits)
		if err != nil {
			return out, fmt.Errorf("disasm at offset %d: %w", off, err)
		}
		out = append(out, text)
		off += n
	}
	return out, nil
}
