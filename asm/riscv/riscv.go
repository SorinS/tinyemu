// Package riscv is a small, hand-written RISC-V assembler and disassembler for
// the RV64I base integer set plus the M (mul/div) extension. Unlike the x86
// assembler (data-driven from NASM's large irregular table), RISC-V's encoding
// is small and ruthlessly regular — six instruction formats with fixed field
// positions — so a compact Go table is the maintainable choice. Byte-exactness
// is checked differentially against llvm-mc.
package riscv

import (
	"fmt"
	"strings"
)

// format is a RISC-V base instruction format.
type format int

const (
	fmtR      format = iota // rd, rs1, rs2
	fmtI                    // rd, rs1, imm12
	fmtIShift               // rd, rs1, shamt (I-type, shift amount in imm, funct6/7 in top bits)
	fmtILoad                // rd, imm(rs1)
	fmtIJalr                // rd, imm(rs1)  — same as load syntax
	fmtS                    // rs2, imm(rs1)
	fmtB                    // rs1, rs2, imm (branch, PC-relative)
	fmtU                    // rd, imm20
	fmtJ                    // rd, imm (jump, PC-relative)
	fmtNone                 // no operands (ecall/ebreak)
)

// insn is one instruction's encoding template.
type insn struct {
	name   string
	format format
	opcode uint32 // bits [6:0]
	funct3 uint32 // bits [14:12]
	funct7 uint32 // bits [31:25] (R-type; for shifts, the funct6/7 that selects logical vs arithmetic)
}

// table is the RV64I + M instruction set. Opcodes/functs from the RISC-V
// unprivileged spec.
var table = []insn{
	// --- R-type (OP, opcode 0x33) ---
	{"add", fmtR, 0x33, 0x0, 0x00}, {"sub", fmtR, 0x33, 0x0, 0x20},
	{"sll", fmtR, 0x33, 0x1, 0x00}, {"slt", fmtR, 0x33, 0x2, 0x00},
	{"sltu", fmtR, 0x33, 0x3, 0x00}, {"xor", fmtR, 0x33, 0x4, 0x00},
	{"srl", fmtR, 0x33, 0x5, 0x00}, {"sra", fmtR, 0x33, 0x5, 0x20},
	{"or", fmtR, 0x33, 0x6, 0x00}, {"and", fmtR, 0x33, 0x7, 0x00},
	// --- R-type word ops (OP-32, opcode 0x3B) — RV64 ---
	{"addw", fmtR, 0x3B, 0x0, 0x00}, {"subw", fmtR, 0x3B, 0x0, 0x20},
	{"sllw", fmtR, 0x3B, 0x1, 0x00}, {"srlw", fmtR, 0x3B, 0x5, 0x00},
	{"sraw", fmtR, 0x3B, 0x5, 0x20},
	// --- M extension (OP / OP-32) ---
	{"mul", fmtR, 0x33, 0x0, 0x01}, {"mulh", fmtR, 0x33, 0x1, 0x01},
	{"mulhsu", fmtR, 0x33, 0x2, 0x01}, {"mulhu", fmtR, 0x33, 0x3, 0x01},
	{"div", fmtR, 0x33, 0x4, 0x01}, {"divu", fmtR, 0x33, 0x5, 0x01},
	{"rem", fmtR, 0x33, 0x6, 0x01}, {"remu", fmtR, 0x33, 0x7, 0x01},
	{"mulw", fmtR, 0x3B, 0x0, 0x01}, {"divw", fmtR, 0x3B, 0x4, 0x01},
	{"divuw", fmtR, 0x3B, 0x5, 0x01}, {"remw", fmtR, 0x3B, 0x6, 0x01},
	{"remuw", fmtR, 0x3B, 0x7, 0x01},
	// --- I-type arithmetic (OP-IMM, opcode 0x13) ---
	{"addi", fmtI, 0x13, 0x0, 0x00}, {"slti", fmtI, 0x13, 0x2, 0x00},
	{"sltiu", fmtI, 0x13, 0x3, 0x00}, {"xori", fmtI, 0x13, 0x4, 0x00},
	{"ori", fmtI, 0x13, 0x6, 0x00}, {"andi", fmtI, 0x13, 0x7, 0x00},
	{"addiw", fmtI, 0x1B, 0x0, 0x00}, // RV64 OP-IMM-32
	// --- I-type shifts (RV64: 6-bit shamt, funct6) ---
	{"slli", fmtIShift, 0x13, 0x1, 0x00}, {"srli", fmtIShift, 0x13, 0x5, 0x00},
	{"srai", fmtIShift, 0x13, 0x5, 0x10}, // funct6 0x10 → arithmetic
	{"slliw", fmtIShift, 0x1B, 0x1, 0x00}, {"srliw", fmtIShift, 0x1B, 0x5, 0x00},
	{"sraiw", fmtIShift, 0x1B, 0x5, 0x20},
	// --- Loads (opcode 0x03) ---
	{"lb", fmtILoad, 0x03, 0x0, 0x00}, {"lh", fmtILoad, 0x03, 0x1, 0x00},
	{"lw", fmtILoad, 0x03, 0x2, 0x00}, {"ld", fmtILoad, 0x03, 0x3, 0x00},
	{"lbu", fmtILoad, 0x03, 0x4, 0x00}, {"lhu", fmtILoad, 0x03, 0x5, 0x00},
	{"lwu", fmtILoad, 0x03, 0x6, 0x00},
	// --- jalr (opcode 0x67) ---
	{"jalr", fmtIJalr, 0x67, 0x0, 0x00},
	// --- Stores (opcode 0x23) ---
	{"sb", fmtS, 0x23, 0x0, 0x00}, {"sh", fmtS, 0x23, 0x1, 0x00},
	{"sw", fmtS, 0x23, 0x2, 0x00}, {"sd", fmtS, 0x23, 0x3, 0x00},
	// --- Branches (opcode 0x63) ---
	{"beq", fmtB, 0x63, 0x0, 0x00}, {"bne", fmtB, 0x63, 0x1, 0x00},
	{"blt", fmtB, 0x63, 0x4, 0x00}, {"bge", fmtB, 0x63, 0x5, 0x00},
	{"bltu", fmtB, 0x63, 0x6, 0x00}, {"bgeu", fmtB, 0x63, 0x7, 0x00},
	// --- U-type ---
	{"lui", fmtU, 0x37, 0x0, 0x00}, {"auipc", fmtU, 0x17, 0x0, 0x00},
	// --- J-type ---
	{"jal", fmtJ, 0x6F, 0x0, 0x00},
	// --- System ---
	{"ecall", fmtNone, 0x73, 0x0, 0x000}, {"ebreak", fmtNone, 0x73, 0x0, 0x001},
}

var byName = func() map[string]*insn {
	m := map[string]*insn{}
	for i := range table {
		m[table[i].name] = &table[i]
	}
	return m
}()

// abiNames maps each x-register number to its ABI name (index = register).
var abiNames = [32]string{
	"zero", "ra", "sp", "gp", "tp", "t0", "t1", "t2",
	"s0", "s1", "a0", "a1", "a2", "a3", "a4", "a5",
	"a6", "a7", "s2", "s3", "s4", "s5", "s6", "s7",
	"s8", "s9", "s10", "s11", "t3", "t4", "t5", "t6",
}

// regByName resolves an x-register or ABI name to its number 0–31. "fp" is an
// accepted alias for s0/x8.
var regByName = func() map[string]int {
	m := map[string]int{"fp": 8}
	for i := 0; i < 32; i++ {
		m[fmt.Sprintf("x%d", i)] = i
		m[abiNames[i]] = i
	}
	return m
}()

// parseReg resolves a register operand to its number.
func parseReg(s string) (int, bool) {
	r, ok := regByName[strings.TrimSpace(strings.ToLower(s))]
	return r, ok
}
