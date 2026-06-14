package riscv

// pseudoNames lists the supported pseudo-instruction mnemonics (for tooling
// like completion). They expand to base instructions in expandPseudo.
var pseudoNames = []string{
	"nop", "ret", "mv", "not", "neg", "seqz", "snez", "li", "j", "jr", "beqz", "bnez",
	"csrr", "csrw", "csrs", "csrc", "csrwi", "csrsi", "csrci",
	"fmv.s", "fabs.s", "fneg.s", "fmv.d", "fabs.d", "fneg.d",
}

// Mnemonics returns every assemblable instruction mnemonic — base table, FP
// (F/D) table, and pseudo-instructions — for editor completion.
func Mnemonics() []string {
	out := make([]string, 0, len(table)+len(fpTable)+len(pseudoNames))
	for i := range table {
		out = append(out, table[i].name)
	}
	for i := range fpTable {
		out = append(out, fpTable[i].name)
	}
	return append(out, pseudoNames...)
}

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
	case "jal":
		if len(ops) == 1 { // jal target → jal ra, target
			return "jal", []string{"ra", ops[0]}
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
	case "csrr": // csrr rd, csr  →  csrrs rd, csr, zero
		if len(ops) == 2 {
			return "csrrs", []string{ops[0], ops[1], "zero"}
		}
	case "csrw": // csrw csr, rs  →  csrrw zero, csr, rs
		if len(ops) == 2 {
			return "csrrw", []string{"zero", ops[0], ops[1]}
		}
	case "csrs":
		if len(ops) == 2 {
			return "csrrs", []string{"zero", ops[0], ops[1]}
		}
	case "csrc":
		if len(ops) == 2 {
			return "csrrc", []string{"zero", ops[0], ops[1]}
		}
	case "csrwi":
		if len(ops) == 2 {
			return "csrrwi", []string{"zero", ops[0], ops[1]}
		}
	case "csrsi":
		if len(ops) == 2 {
			return "csrrsi", []string{"zero", ops[0], ops[1]}
		}
	case "csrci":
		if len(ops) == 2 {
			return "csrrci", []string{"zero", ops[0], ops[1]}
		}
	// FP register moves (fmv/fabs/fneg) are sign-injection idioms.
	case "fmv.s":
		if len(ops) == 2 {
			return "fsgnj.s", []string{ops[0], ops[1], ops[1]}
		}
	case "fabs.s":
		if len(ops) == 2 {
			return "fsgnjx.s", []string{ops[0], ops[1], ops[1]}
		}
	case "fneg.s":
		if len(ops) == 2 {
			return "fsgnjn.s", []string{ops[0], ops[1], ops[1]}
		}
	case "fmv.d":
		if len(ops) == 2 {
			return "fsgnj.d", []string{ops[0], ops[1], ops[1]}
		}
	case "fabs.d":
		if len(ops) == 2 {
			return "fsgnjx.d", []string{ops[0], ops[1], ops[1]}
		}
	case "fneg.d":
		if len(ops) == 2 {
			return "fsgnjn.d", []string{ops[0], ops[1], ops[1]}
		}
	}
	return mnem, ops
}
