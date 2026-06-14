package riscv

// expandPseudo rewrites a common RISC-V pseudo-instruction into its base
// instruction (mnemonic + operands). It returns the inputs unchanged if mnem
// is not a recognized pseudo. Only single-instruction expansions are handled
// (e.g. a wide "li" needing lui+addi is left to the caller/assembler to
// reject for now).
func expandPseudo(mnem string, ops []string) (string, []string) {
	switch mnem {
	case "nop":
		if len(ops) == 0 {
			return "addi", []string{"zero", "zero", "0"}
		}
	case "ret":
		if len(ops) == 0 {
			return "jalr", []string{"zero", "0(ra)"}
		}
	case "mv":
		if len(ops) == 2 {
			return "addi", []string{ops[0], ops[1], "0"}
		}
	case "not":
		if len(ops) == 2 {
			return "xori", []string{ops[0], ops[1], "-1"}
		}
	case "neg":
		if len(ops) == 2 {
			return "sub", []string{ops[0], "zero", ops[1]}
		}
	case "seqz":
		if len(ops) == 2 {
			return "sltiu", []string{ops[0], ops[1], "1"}
		}
	case "snez":
		if len(ops) == 2 {
			return "sltu", []string{ops[0], "zero", ops[1]}
		}
	case "li":
		// Only the addi-representable range (signed 12-bit) for now.
		if len(ops) == 2 {
			if v, err := parseImm(ops[1]); err == nil && fits(v, 12) {
				return "addi", []string{ops[0], "zero", ops[1]}
			}
		}
	case "j":
		if len(ops) == 1 {
			return "jal", []string{"zero", ops[0]}
		}
	case "jr":
		if len(ops) == 1 {
			return "jalr", []string{"zero", "0(" + ops[0] + ")"}
		}
	case "beqz":
		if len(ops) == 2 {
			return "beq", []string{ops[0], "zero", ops[1]}
		}
	case "bnez":
		if len(ops) == 2 {
			return "bne", []string{ops[0], "zero", ops[1]}
		}
	}
	return mnem, ops
}
