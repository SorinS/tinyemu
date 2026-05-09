package riscv

// RISC-V instruction opcodes (bits 6:0)
const (
	OpcodeLUI     = 0x37 // LUI
	OpcodeAUIPC   = 0x17 // AUIPC
	OpcodeJAL     = 0x6F // JAL
	OpcodeJALR    = 0x67 // JALR
	OpcodeBranch  = 0x63 // BEQ, BNE, BLT, BGE, BLTU, BGEU
	OpcodeLoad    = 0x03 // LB, LH, LW, LD, LBU, LHU, LWU
	OpcodeStore   = 0x23 // SB, SH, SW, SD
	OpcodeOpImm   = 0x13 // ADDI, SLTI, SLTIU, XORI, ORI, ANDI, SLLI, SRLI, SRAI
	OpcodeOpImm32 = 0x1B // ADDIW, SLLIW, SRLIW, SRAIW (RV64)
	OpcodeOp      = 0x33 // ADD, SUB, SLL, SLT, SLTU, XOR, SRL, SRA, OR, AND
	OpcodeOp32    = 0x3B // ADDW, SUBW, SLLW, SRLW, SRAW (RV64)
	OpcodeMiscMem = 0x0F // FENCE, FENCE.I
	OpcodeSystem  = 0x73 // ECALL, EBREAK, CSR*, xRET, WFI, SFENCE.VMA
	OpcodeAMO     = 0x2F // Atomic operations
	OpcodeFPLoad  = 0x07 // FLW, FLD
	OpcodeFPStore = 0x27 // FSW, FSD
	OpcodeFMADD   = 0x43 // FMADD
	OpcodeFMSUB   = 0x47 // FMSUB
	OpcodeFNMSUB  = 0x4B // FNMSUB
	OpcodeFNMADD  = 0x4F // FNMADD
	OpcodeFPOp    = 0x53 // Floating-point operations
)

// Branch funct3 values
const (
	Funct3BEQ  = 0
	Funct3BNE  = 1
	Funct3BLT  = 4
	Funct3BGE  = 5
	Funct3BLTU = 6
	Funct3BGEU = 7
)

// Load/store funct3 values
const (
	Funct3LB  = 0
	Funct3LH  = 1
	Funct3LW  = 2
	Funct3LD  = 3
	Funct3LBU = 4
	Funct3LHU = 5
	Funct3LWU = 6
)

// OP-IMM funct3 values
const (
	Funct3ADDI  = 0
	Funct3SLTI  = 2
	Funct3SLTIU = 3
	Funct3XORI  = 4
	Funct3ORI   = 6
	Funct3ANDI  = 7
	Funct3SLLI  = 1
	Funct3SRLI  = 5 // Also SRAI when funct7[5] = 1
)

// OP funct3 values
const (
	Funct3ADD  = 0 // Also SUB when funct7[5] = 1
	Funct3SLL  = 1
	Funct3SLT  = 2
	Funct3SLTU = 3
	Funct3XOR  = 4
	Funct3SRL  = 5 // Also SRA when funct7[5] = 1
	Funct3OR   = 6
	Funct3AND  = 7
)

// M-extension funct3 values (when funct7 = 1)
const (
	Funct3MUL    = 0
	Funct3MULH   = 1
	Funct3MULHSU = 2
	Funct3MULHU  = 3
	Funct3DIV    = 4
	Funct3DIVU   = 5
	Funct3REM    = 6
	Funct3REMU   = 7
)

// SYSTEM funct3 values
const (
	Funct3PRIV   = 0 // ECALL, EBREAK, xRET, WFI, SFENCE.VMA
	Funct3CSRRW  = 1
	Funct3CSRRS  = 2
	Funct3CSRRC  = 3
	Funct3CSRRWI = 5
	Funct3CSRRSI = 6
	Funct3CSRRCI = 7
)

// SYSTEM imm[11:0] values for funct3 = PRIV
const (
	PrivECALL     = 0x000
	PrivEBREAK    = 0x001
	PrivSRET      = 0x102
	PrivMRET      = 0x302
	PrivWFI       = 0x105
	PrivSFENCEVMA = 0x120 // Base value, bits 24:20 contain rs2
)

// Atomic funct5 values (bits 31:27)
const (
	Funct5LR      = 0x02
	Funct5SC      = 0x03
	Funct5AMOSWAP = 0x01
	Funct5AMOADD  = 0x00
	Funct5AMOXOR  = 0x04
	Funct5AMOAND  = 0x0C
	Funct5AMOOR   = 0x08
	Funct5AMOMIN  = 0x10
	Funct5AMOMAX  = 0x14
	Funct5AMOMINU = 0x18
	Funct5AMOMAXU = 0x1C
)

// Instruction field extraction functions

// ExtractOpcode returns the opcode field (bits 6:0)
func ExtractOpcode(insn uint32) uint32 {
	return insn & 0x7F
}

// ExtractRd returns the destination register (bits 11:7)
func ExtractRd(insn uint32) uint32 {
	return (insn >> 7) & 0x1F
}

// ExtractFunct3 returns the funct3 field (bits 14:12)
func ExtractFunct3(insn uint32) uint32 {
	return (insn >> 12) & 0x7
}

// ExtractRs1 returns the first source register (bits 19:15)
func ExtractRs1(insn uint32) uint32 {
	return (insn >> 15) & 0x1F
}

// ExtractRs2 returns the second source register (bits 24:20)
func ExtractRs2(insn uint32) uint32 {
	return (insn >> 20) & 0x1F
}

// ExtractFunct7 returns the funct7 field (bits 31:25)
func ExtractFunct7(insn uint32) uint32 {
	return insn >> 25
}

// ExtractFunct5 returns the funct5 field for AMO (bits 31:27)
func ExtractFunct5(insn uint32) uint32 {
	return insn >> 27
}

// ExtractRs3 returns the third source register for R4-type (bits 31:27)
func ExtractRs3(insn uint32) uint32 {
	return insn >> 27
}

// Immediate extraction functions

// ExtractIImm returns the I-type immediate (sign-extended)
// Format: imm[11:0] = insn[31:20]
func ExtractIImm(insn uint32) int64 {
	return int64(int32(insn) >> 20)
}

// ExtractSImm returns the S-type immediate (sign-extended)
// Format: imm[11:5] = insn[31:25], imm[4:0] = insn[11:7]
func ExtractSImm(insn uint32) int64 {
	imm := (insn >> 7) & 0x1F         // bits 4:0
	imm |= ((insn >> 25) & 0x7F) << 5 // bits 11:5
	// Sign extend from bit 11
	return int64(int32(imm<<20) >> 20)
}

// ExtractBImm returns the B-type immediate (sign-extended)
// Format: imm[12|10:5] = insn[31:25], imm[4:1|11] = insn[11:7]
func ExtractBImm(insn uint32) int64 {
	imm := uint32(0)
	imm |= ((insn >> 8) & 0xF) << 1   // bits 4:1
	imm |= ((insn >> 25) & 0x3F) << 5 // bits 10:5
	imm |= ((insn >> 7) & 0x1) << 11  // bit 11
	imm |= ((insn >> 31) & 0x1) << 12 // bit 12 (sign)
	// Sign extend from bit 12
	return int64(int32(imm<<19) >> 19)
}

// ExtractUImm returns the U-type immediate (already shifted, sign-extended to 64 bits)
// Format: imm[31:12] = insn[31:12]
func ExtractUImm(insn uint32) int64 {
	return int64(int32(insn) & ^int32(0xFFF))
}

// ExtractJImm returns the J-type immediate (sign-extended)
// Format: imm[20|10:1|11|19:12] = insn[31:12]
func ExtractJImm(insn uint32) int64 {
	imm := uint32(0)
	imm |= ((insn >> 21) & 0x3FF) << 1 // bits 10:1
	imm |= ((insn >> 20) & 0x1) << 11  // bit 11
	imm |= (insn & 0xFF000)            // bits 19:12
	imm |= ((insn >> 31) & 0x1) << 20  // bit 20 (sign)
	// Sign extend from bit 20
	return int64(int32(imm<<11) >> 11)
}

// ExtractShamt returns the shift amount for shift instructions
// For RV32: 5 bits (insn[24:20])
// For RV64: 6 bits (insn[25:20])
func ExtractShamt32(insn uint32) uint32 {
	return (insn >> 20) & 0x1F
}

func ExtractShamt64(insn uint32) uint32 {
	return (insn >> 20) & 0x3F
}

// ExtractCSR returns the CSR address (bits 31:20)
func ExtractCSR(insn uint32) uint32 {
	return insn >> 20
}

// IsCompressed returns true if the instruction is a 16-bit compressed instruction
// Compressed instructions have bits 1:0 != 11
func IsCompressed(insn uint16) bool {
	return (insn & 0x3) != 0x3
}

// GetInsnSize returns the size of the instruction in bytes
// Returns 2 for compressed instructions, 4 for standard instructions
func GetInsnSize(insnWord uint32) int {
	if (insnWord & 0x3) != 0x3 {
		return 2 // Compressed instruction
	}
	return 4 // Standard 32-bit instruction
}

// SignExtend32 sign-extends a value from fromBits to int64.
// Reference: tinyemu-2019-12-21/riscv_cpu.c:1199-1202 (sext)
func SignExtend32(val uint32, fromBits int) int64 {
	shift := 32 - fromBits
	return int64(int32(val<<shift) >> shift)
}

// GetField1 extracts a field from instruction and shifts it to target position.
// Reference: tinyemu-2019-12-21/riscv_cpu.c:1204-1214 (get_field1)
// srcBit: source bit position in instruction
// dstLo: lowest bit position in destination
// dstHi: highest bit position in destination
func GetField1(insn uint32, srcBit, dstLo, dstHi int) uint32 {
	width := dstHi - dstLo + 1
	mask := uint32((1 << width) - 1)
	return ((insn >> srcBit) & mask) << dstLo
}
