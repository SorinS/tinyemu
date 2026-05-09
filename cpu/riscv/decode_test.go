package riscv

import "testing"

func TestExtractOpcode(t *testing.T) {
	tests := []struct {
		insn     uint32
		expected uint32
	}{
		{0x00000037, OpcodeLUI},     // LUI
		{0x00000017, OpcodeAUIPC},   // AUIPC
		{0x0000006F, OpcodeJAL},     // JAL
		{0x00000067, OpcodeJALR},    // JALR
		{0x00000063, OpcodeBranch},  // Branch
		{0x00000003, OpcodeLoad},    // Load
		{0x00000023, OpcodeStore},   // Store
		{0x00000013, OpcodeOpImm},   // OP-IMM
		{0x00000033, OpcodeOp},      // OP
		{0x0000000F, OpcodeMiscMem}, // MISC-MEM
		{0x00000073, OpcodeSystem},  // SYSTEM
	}

	for _, tc := range tests {
		got := ExtractOpcode(tc.insn)
		if got != tc.expected {
			t.Errorf("ExtractOpcode(0x%08x) = 0x%02x, want 0x%02x", tc.insn, got, tc.expected)
		}
	}
}

func TestExtractRd(t *testing.T) {
	tests := []struct {
		insn     uint32
		expected uint32
	}{
		{0x00000000, 0},
		{0x00000080, 1},  // rd = 1 (bits 11:7 = 00001)
		{0x00000F80, 31}, // rd = 31 (bits 11:7 = 11111)
		{0x00000500, 10}, // rd = 10
	}

	for _, tc := range tests {
		got := ExtractRd(tc.insn)
		if got != tc.expected {
			t.Errorf("ExtractRd(0x%08x) = %d, want %d", tc.insn, got, tc.expected)
		}
	}
}

func TestExtractRs1(t *testing.T) {
	tests := []struct {
		insn     uint32
		expected uint32
	}{
		{0x00000000, 0},
		{0x00008000, 1},  // rs1 = 1 (bits 19:15)
		{0x000F8000, 31}, // rs1 = 31
		{0x00050000, 10}, // rs1 = 10
	}

	for _, tc := range tests {
		got := ExtractRs1(tc.insn)
		if got != tc.expected {
			t.Errorf("ExtractRs1(0x%08x) = %d, want %d", tc.insn, got, tc.expected)
		}
	}
}

func TestExtractRs2(t *testing.T) {
	tests := []struct {
		insn     uint32
		expected uint32
	}{
		{0x00000000, 0},
		{0x00100000, 1},  // rs2 = 1 (bits 24:20)
		{0x01F00000, 31}, // rs2 = 31
		{0x00A00000, 10}, // rs2 = 10
	}

	for _, tc := range tests {
		got := ExtractRs2(tc.insn)
		if got != tc.expected {
			t.Errorf("ExtractRs2(0x%08x) = %d, want %d", tc.insn, got, tc.expected)
		}
	}
}

func TestExtractFunct3(t *testing.T) {
	tests := []struct {
		insn     uint32
		expected uint32
	}{
		{0x00000000, 0},
		{0x00001000, 1}, // funct3 = 1 (bits 14:12)
		{0x00007000, 7}, // funct3 = 7
		{0x00003000, 3}, // funct3 = 3
	}

	for _, tc := range tests {
		got := ExtractFunct3(tc.insn)
		if got != tc.expected {
			t.Errorf("ExtractFunct3(0x%08x) = %d, want %d", tc.insn, got, tc.expected)
		}
	}
}

func TestExtractFunct7(t *testing.T) {
	tests := []struct {
		insn     uint32
		expected uint32
	}{
		{0x00000000, 0},
		{0x02000000, 1},   // funct7 = 1 (bits 31:25)
		{0xFE000000, 127}, // funct7 = 127
		{0x40000000, 32},  // funct7 = 32 (for SUB/SRA)
	}

	for _, tc := range tests {
		got := ExtractFunct7(tc.insn)
		if got != tc.expected {
			t.Errorf("ExtractFunct7(0x%08x) = %d, want %d", tc.insn, got, tc.expected)
		}
	}
}

func TestExtractIImm(t *testing.T) {
	tests := []struct {
		insn     uint32
		expected int64
	}{
		{0x00000000, 0},
		{0x00100000, 1},     // imm = 1
		{0x7FF00000, 2047},  // imm = 2047 (max positive)
		{0x80000000, -2048}, // imm = -2048 (min negative)
		{0xFFF00000, -1},    // imm = -1
		{0x12300000, 0x123}, // imm = 0x123
	}

	for _, tc := range tests {
		got := ExtractIImm(tc.insn)
		if got != tc.expected {
			t.Errorf("ExtractIImm(0x%08x) = %d, want %d", tc.insn, got, tc.expected)
		}
	}
}

func TestExtractSImm(t *testing.T) {
	// S-type: imm[11:5] = insn[31:25], imm[4:0] = insn[11:7]
	tests := []struct {
		insn     uint32
		expected int64
	}{
		{0x00000000, 0},
		{0x00000080, 1},     // imm[0] = 1
		{0x02000000, 32},    // imm[5] = 1
		{0xFE000F80, -1},    // imm = -1 (all 1s)
		{0x80000000, -2048}, // imm = -2048
	}

	for _, tc := range tests {
		got := ExtractSImm(tc.insn)
		if got != tc.expected {
			t.Errorf("ExtractSImm(0x%08x) = %d, want %d", tc.insn, got, tc.expected)
		}
	}
}

func TestExtractBImm(t *testing.T) {
	// B-type: imm[12|10:5] = insn[31:25], imm[4:1|11] = insn[11:7]
	tests := []struct {
		insn     uint32
		expected int64
	}{
		{0x00000000, 0},
		{0x00000100, 2},       // imm[1] = 1 -> imm = 2
		{0x00000080, 1 << 11}, // imm[11] = 1 -> imm = 2048
		{0x80000000, -4096},   // imm[12] = 1 (sign) -> negative
		{0xFE000F80, -2},      // imm = -2
	}

	for _, tc := range tests {
		got := ExtractBImm(tc.insn)
		if got != tc.expected {
			t.Errorf("ExtractBImm(0x%08x) = %d, want %d", tc.insn, got, tc.expected)
		}
	}
}

func TestExtractUImm(t *testing.T) {
	// U-type: imm[31:12] = insn[31:12], lower 12 bits are 0
	tests := []struct {
		insn     uint32
		expected int64
	}{
		{0x00000000, 0},
		{0x00001000, 0x1000},      // imm = 0x1000
		{0x12345000, 0x12345000},  // imm = 0x12345000
		{0x80000000, -0x80000000}, // Negative (sign extended)
		{0xFFFFF000, -0x1000},     // imm = -0x1000
	}

	for _, tc := range tests {
		got := ExtractUImm(tc.insn)
		if got != tc.expected {
			t.Errorf("ExtractUImm(0x%08x) = %d (0x%x), want %d (0x%x)",
				tc.insn, got, got, tc.expected, tc.expected)
		}
	}
}

func TestExtractJImm(t *testing.T) {
	// J-type: imm[20|10:1|11|19:12] = insn[31:12]
	tests := []struct {
		insn     uint32
		expected int64
	}{
		{0x00000000, 0},
		{0x00200000, 2},        // imm[1] = 1 -> imm = 2
		{0x00100000, 1 << 11},  // imm[11] = 1 -> imm = 2048
		{0x000FF000, 0xFF000},  // imm[19:12] -> imm = 0xFF000
		{0x80000000, -1048576}, // imm[20] = 1 (sign) -> negative max
	}

	for _, tc := range tests {
		got := ExtractJImm(tc.insn)
		if got != tc.expected {
			t.Errorf("ExtractJImm(0x%08x) = %d, want %d", tc.insn, got, tc.expected)
		}
	}
}

func TestExtractShamt(t *testing.T) {
	tests := []struct {
		insn       uint32
		expected32 uint32
		expected64 uint32
	}{
		{0x00000000, 0, 0},
		{0x00100000, 1, 1},
		{0x01F00000, 31, 31},
		{0x02000000, 0, 32},  // Bit 5 set - valid for RV64 only
		{0x03F00000, 31, 63}, // Max shift for RV64
	}

	for _, tc := range tests {
		got32 := ExtractShamt32(tc.insn)
		got64 := ExtractShamt64(tc.insn)
		if got32 != tc.expected32 {
			t.Errorf("ExtractShamt32(0x%08x) = %d, want %d", tc.insn, got32, tc.expected32)
		}
		if got64 != tc.expected64 {
			t.Errorf("ExtractShamt64(0x%08x) = %d, want %d", tc.insn, got64, tc.expected64)
		}
	}
}

func TestExtractCSR(t *testing.T) {
	tests := []struct {
		insn     uint32
		expected uint32
	}{
		{0x00000000, 0},
		{0x00100000, 0x001}, // fflags
		{0x00200000, 0x002}, // frm
		{0x00300000, 0x003}, // fcsr
		{0x30000000, 0x300}, // mstatus
		{0xF1400000, 0xF14}, // mhartid
	}

	for _, tc := range tests {
		got := ExtractCSR(tc.insn)
		if got != tc.expected {
			t.Errorf("ExtractCSR(0x%08x) = 0x%03x, want 0x%03x", tc.insn, got, tc.expected)
		}
	}
}

func TestIsCompressed(t *testing.T) {
	tests := []struct {
		insn     uint16
		expected bool
	}{
		{0x0000, true},  // Quadrant 0
		{0x0001, true},  // Quadrant 1
		{0x0002, true},  // Quadrant 2
		{0x0003, false}, // Not compressed (bits 1:0 = 11)
		{0x0013, false}, // ADDI-like opcode
		{0x4501, true},  // C.LI
	}

	for _, tc := range tests {
		got := IsCompressed(tc.insn)
		if got != tc.expected {
			t.Errorf("IsCompressed(0x%04x) = %v, want %v", tc.insn, got, tc.expected)
		}
	}
}

func TestGetInsnSize(t *testing.T) {
	tests := []struct {
		insn     uint32
		expected int
	}{
		{0x00000000, 2}, // Compressed (bits 1:0 = 00)
		{0x00000001, 2}, // Compressed (bits 1:0 = 01)
		{0x00000002, 2}, // Compressed (bits 1:0 = 10)
		{0x00000003, 4}, // Standard 32-bit (bits 1:0 = 11)
		{0x00000013, 4}, // ADDI
		{0x00000033, 4}, // ADD
	}

	for _, tc := range tests {
		got := GetInsnSize(tc.insn)
		if got != tc.expected {
			t.Errorf("GetInsnSize(0x%08x) = %d, want %d", tc.insn, got, tc.expected)
		}
	}
}

func TestSignExtend32(t *testing.T) {
	tests := []struct {
		val      uint32
		fromBits int
		expected int64
	}{
		{0x00, 8, 0},
		{0x7F, 8, 127},
		{0x80, 8, -128},
		{0xFF, 8, -1},
		{0x07FF, 12, 2047},  // Max positive 12-bit
		{0x0800, 12, -2048}, // Min negative 12-bit
		{0x0FFF, 12, -1},    // All 1s in 12 bits = -1
		{0x7FFFFFFF, 32, 0x7FFFFFFF},
		{0x80000000, 32, -0x80000000},
	}

	for _, tc := range tests {
		got := SignExtend32(tc.val, tc.fromBits)
		if got != tc.expected {
			t.Errorf("SignExtend32(0x%x, %d) = %d, want %d", tc.val, tc.fromBits, got, tc.expected)
		}
	}
}

func TestGetField1(t *testing.T) {
	// GetField1 extracts bits and shifts them to a destination position
	tests := []struct {
		insn   uint32
		srcBit int
		dstLo  int
		dstHi  int
		expect uint32
	}{
		{0xFFFFFFFF, 0, 0, 0, 1},    // Extract 1 bit from position 0
		{0xFFFFFFFF, 0, 0, 3, 0xF},  // Extract 4 bits from position 0
		{0x00000080, 7, 0, 0, 1},    // Extract bit 7, put at bit 0
		{0x00000080, 7, 4, 4, 0x10}, // Extract bit 7, put at bit 4
	}

	for _, tc := range tests {
		got := GetField1(tc.insn, tc.srcBit, tc.dstLo, tc.dstHi)
		if got != tc.expect {
			t.Errorf("GetField1(0x%08x, %d, %d, %d) = 0x%x, want 0x%x",
				tc.insn, tc.srcBit, tc.dstLo, tc.dstHi, got, tc.expect)
		}
	}
}

// Test real instruction encodings (R-type only, where all fields are meaningful)
func TestRealInstructionsRType(t *testing.T) {
	tests := []struct {
		name   string
		insn   uint32
		opcode uint32
		rd     uint32
		rs1    uint32
		rs2    uint32
		funct3 uint32
		funct7 uint32
	}{
		// ADD x1, x2, x3 -> 0x003100B3
		{"ADD x1,x2,x3", 0x003100B3, OpcodeOp, 1, 2, 3, 0, 0},
		// SUB x1, x2, x3 -> 0x403100B3
		{"SUB x1,x2,x3", 0x403100B3, OpcodeOp, 1, 2, 3, 0, 0x20},
		// AND x1, x2, x3 -> 0x003170B3
		{"AND x1,x2,x3", 0x003170B3, OpcodeOp, 1, 2, 3, 7, 0},
		// OR x1, x2, x3 -> 0x003160B3
		{"OR x1,x2,x3", 0x003160B3, OpcodeOp, 1, 2, 3, 6, 0},
		// XOR x1, x2, x3 -> 0x003140B3
		{"XOR x1,x2,x3", 0x003140B3, OpcodeOp, 1, 2, 3, 4, 0},
		// SLL x1, x2, x3 -> 0x003110B3
		{"SLL x1,x2,x3", 0x003110B3, OpcodeOp, 1, 2, 3, 1, 0},
		// SRL x1, x2, x3 -> 0x003150B3
		{"SRL x1,x2,x3", 0x003150B3, OpcodeOp, 1, 2, 3, 5, 0},
		// SRA x1, x2, x3 -> 0x403150B3
		{"SRA x1,x2,x3", 0x403150B3, OpcodeOp, 1, 2, 3, 5, 0x20},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExtractOpcode(tc.insn); got != tc.opcode {
				t.Errorf("opcode: got 0x%02x, want 0x%02x", got, tc.opcode)
			}
			if got := ExtractRd(tc.insn); got != tc.rd {
				t.Errorf("rd: got %d, want %d", got, tc.rd)
			}
			if got := ExtractRs1(tc.insn); got != tc.rs1 {
				t.Errorf("rs1: got %d, want %d", got, tc.rs1)
			}
			if got := ExtractRs2(tc.insn); got != tc.rs2 {
				t.Errorf("rs2: got %d, want %d", got, tc.rs2)
			}
			if got := ExtractFunct3(tc.insn); got != tc.funct3 {
				t.Errorf("funct3: got %d, want %d", got, tc.funct3)
			}
			if got := ExtractFunct7(tc.insn); got != tc.funct7 {
				t.Errorf("funct7: got %d, want %d", got, tc.funct7)
			}
		})
	}
}

// Test I-type instruction field extraction
func TestRealInstructionsIType(t *testing.T) {
	tests := []struct {
		name   string
		insn   uint32
		opcode uint32
		rd     uint32
		rs1    uint32
		funct3 uint32
		imm    int64
	}{
		// ADDI x1, x2, 100 -> 0x06410093
		{"ADDI x1,x2,100", 0x06410093, OpcodeOpImm, 1, 2, 0, 100},
		// ADDI x1, x0, -1 -> 0xFFF00093
		{"ADDI x1,x0,-1", 0xFFF00093, OpcodeOpImm, 1, 0, 0, -1},
		// LW x1, 0(x2) -> 0x00012083
		{"LW x1,0(x2)", 0x00012083, OpcodeLoad, 1, 2, 2, 0},
		// LW x1, 4(x2) -> 0x00412083
		{"LW x1,4(x2)", 0x00412083, OpcodeLoad, 1, 2, 2, 4},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExtractOpcode(tc.insn); got != tc.opcode {
				t.Errorf("opcode: got 0x%02x, want 0x%02x", got, tc.opcode)
			}
			if got := ExtractRd(tc.insn); got != tc.rd {
				t.Errorf("rd: got %d, want %d", got, tc.rd)
			}
			if got := ExtractRs1(tc.insn); got != tc.rs1 {
				t.Errorf("rs1: got %d, want %d", got, tc.rs1)
			}
			if got := ExtractFunct3(tc.insn); got != tc.funct3 {
				t.Errorf("funct3: got %d, want %d", got, tc.funct3)
			}
			if got := ExtractIImm(tc.insn); got != tc.imm {
				t.Errorf("imm: got %d, want %d", got, tc.imm)
			}
		})
	}
}

// Test U-type instruction field extraction
func TestRealInstructionsUType(t *testing.T) {
	tests := []struct {
		name   string
		insn   uint32
		opcode uint32
		rd     uint32
		imm    int64
	}{
		// LUI x1, 0x12345 -> 0x123450B7
		{"LUI x1,0x12345", 0x123450B7, OpcodeLUI, 1, 0x12345000},
		// AUIPC x1, 0x12345 -> 0x12345097
		{"AUIPC x1,0x12345", 0x12345097, OpcodeAUIPC, 1, 0x12345000},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExtractOpcode(tc.insn); got != tc.opcode {
				t.Errorf("opcode: got 0x%02x, want 0x%02x", got, tc.opcode)
			}
			if got := ExtractRd(tc.insn); got != tc.rd {
				t.Errorf("rd: got %d, want %d", got, tc.rd)
			}
			if got := ExtractUImm(tc.insn); got != tc.imm {
				t.Errorf("imm: got 0x%x, want 0x%x", got, tc.imm)
			}
		})
	}
}

// Test immediate extraction with real instructions
func TestRealImmediates(t *testing.T) {
	tests := []struct {
		name string
		insn uint32
		iImm int64
		sImm int64
		bImm int64
		uImm int64
		jImm int64
	}{
		// ADDI x1, x0, 42 -> imm = 42
		{"ADDI imm=42", 0x02A00093, 42, 0, 0, 0, 0},
		// ADDI x1, x0, -1 -> imm = -1
		{"ADDI imm=-1", 0xFFF00093, -1, 0, 0, 0, 0},
		// LUI x1, 0x12345 -> upper imm = 0x12345000
		{"LUI 0x12345", 0x123450B7, 0, 0, 0, 0x12345000, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.iImm != 0 {
				if got := ExtractIImm(tc.insn); got != tc.iImm {
					t.Errorf("I-imm: got %d, want %d", got, tc.iImm)
				}
			}
			if tc.uImm != 0 {
				if got := ExtractUImm(tc.insn); got != tc.uImm {
					t.Errorf("U-imm: got %d (0x%x), want %d (0x%x)", got, got, tc.uImm, tc.uImm)
				}
			}
		})
	}
}
