package riscv

import (
	"testing"

	"github.com/jtolio/tinyemu-go/mem"
)

// TestExpandCompressedQuadrant tests that quadrant detection works correctly
func TestExpandCompressedQuadrant(t *testing.T) {
	// Quadrant 3 (bits 1:0 = 11) is not a compressed instruction
	_, err := ExpandCompressed(0x0003, XLEN64)
	if err != ErrIllegalCompressedInsn {
		t.Errorf("Quadrant 3 should be illegal, got err=%v", err)
	}

	_, err = ExpandCompressed(0xFFFF, XLEN64)
	if err != ErrIllegalCompressedInsn {
		t.Errorf("Quadrant 3 (0xFFFF) should be illegal, got err=%v", err)
	}
}

// TestExpandC0_ADDI4SPN tests C.ADDI4SPN expansion
func TestExpandC0_ADDI4SPN(t *testing.T) {
	// C.ADDI4SPN: adds a zero-extended non-zero immediate, scaled by 4, to sp
	// Expands to: ADDI rd', x2, nzuimm
	// Format: funct3[15:13]=000, imm[5:4|9:6|2|3][12:5], rd'[4:2], op[1:0]=00

	tests := []struct {
		name    string
		insn    uint16
		wantErr bool
		wantRd  uint32
		wantImm int32
	}{
		{
			// C.ADDI4SPN: imm[5:4|9:6|2|3] = insn[12:11|10:7|6|5]
			// For imm=8: need bit 3 set, so insn[5]=1
			name:    "C.ADDI4SPN rd'=x8, imm=8",
			insn:    0x0020, // imm=8, rd'=0 -> x8
			wantRd:  8,
			wantImm: 8,
		},
		{
			name:    "C.ADDI4SPN rd'=x15, imm=1020",
			insn:    0x1FFC, // max immediate with rd'=7->x15
			wantRd:  15,
			wantImm: 1020,
		},
		{
			name:    "C.ADDI4SPN with imm=0 is illegal",
			insn:    0x0000, // funct3=0, imm=0, rd'=0
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expanded, err := ExpandCompressed(tc.insn, XLEN64)
			if tc.wantErr {
				if err != ErrIllegalCompressedInsn {
					t.Errorf("expected illegal insn error, got err=%v, expanded=0x%08x", err, expanded)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Check that it's an ADDI instruction
			if ExtractOpcode(expanded) != OpcodeOpImm {
				t.Errorf("expected OpcodeOpImm, got 0x%02x", ExtractOpcode(expanded))
			}
			if ExtractFunct3(expanded) != Funct3ADDI {
				t.Errorf("expected funct3=ADDI, got %d", ExtractFunct3(expanded))
			}
			if ExtractRd(expanded) != tc.wantRd {
				t.Errorf("expected rd=%d, got %d", tc.wantRd, ExtractRd(expanded))
			}
			// rs1 should be x2 (sp)
			if ExtractRs1(expanded) != 2 {
				t.Errorf("expected rs1=2 (sp), got %d", ExtractRs1(expanded))
			}
			// Check immediate
			gotImm := ExtractIImm(expanded)
			if gotImm != int64(tc.wantImm) {
				t.Errorf("expected imm=%d, got %d", tc.wantImm, gotImm)
			}
		})
	}
}

// TestExpandC0_LW tests C.LW expansion
func TestExpandC0_LW(t *testing.T) {
	// C.LW: loads a 32-bit value from memory into rd'
	// Expands to: LW rd', offset(rs1')
	// Format: funct3[15:13]=010, imm[5:3][12:10], rs1'[9:7], imm[2|6][6:5], rd'[4:2], op[1:0]=00

	tests := []struct {
		name    string
		insn    uint16
		wantRd  uint32
		wantRs1 uint32
		wantImm int32
	}{
		{
			name:    "C.LW x8, 0(x8)",
			insn:    0x4000, // funct3=010, imm=0, rs1'=0->x8, rd'=0->x8
			wantRd:  8,
			wantRs1: 8,
			wantImm: 0,
		},
		{
			name:    "C.LW x15, 124(x15)",
			insn:    0x5FFC, // funct3=010, various imm bits, rs1'=7->x15, rd'=7->x15
			wantRd:  15,
			wantRs1: 15,
			wantImm: 124,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expanded, err := ExpandCompressed(tc.insn, XLEN64)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if ExtractOpcode(expanded) != OpcodeLoad {
				t.Errorf("expected OpcodeLoad, got 0x%02x", ExtractOpcode(expanded))
			}
			if ExtractFunct3(expanded) != Funct3LW {
				t.Errorf("expected funct3=LW, got %d", ExtractFunct3(expanded))
			}
			if ExtractRd(expanded) != tc.wantRd {
				t.Errorf("expected rd=%d, got %d", tc.wantRd, ExtractRd(expanded))
			}
			if ExtractRs1(expanded) != tc.wantRs1 {
				t.Errorf("expected rs1=%d, got %d", tc.wantRs1, ExtractRs1(expanded))
			}
			gotImm := ExtractIImm(expanded)
			if gotImm != int64(tc.wantImm) {
				t.Errorf("expected imm=%d, got %d", tc.wantImm, gotImm)
			}
		})
	}
}

// TestExpandC0_LD tests C.LD expansion (RV64)
func TestExpandC0_LD(t *testing.T) {
	// C.LD: loads a 64-bit value from memory into rd'
	// Expands to: LD rd', offset(rs1')
	// Format: funct3[15:13]=011, imm[5:3][12:10], rs1'[9:7], imm[7:6][6:5], rd'[4:2], op[1:0]=00

	tests := []struct {
		name    string
		insn    uint16
		wantRd  uint32
		wantRs1 uint32
		wantImm int32
	}{
		{
			name:    "C.LD x8, 0(x8)",
			insn:    0x6000, // funct3=011, imm=0, rs1'=0->x8, rd'=0->x8
			wantRd:  8,
			wantRs1: 8,
			wantImm: 0,
		},
		{
			name:    "C.LD x9, 8(x10)",
			insn:    0x6504, // funct3=011, imm=8, rs1'=2->x10, rd'=1->x9
			wantRd:  9,
			wantRs1: 10,
			wantImm: 8,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expanded, err := ExpandCompressed(tc.insn, XLEN64)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if ExtractOpcode(expanded) != OpcodeLoad {
				t.Errorf("expected OpcodeLoad, got 0x%02x", ExtractOpcode(expanded))
			}
			if ExtractFunct3(expanded) != Funct3LD {
				t.Errorf("expected funct3=LD, got %d", ExtractFunct3(expanded))
			}
			if ExtractRd(expanded) != tc.wantRd {
				t.Errorf("expected rd=%d, got %d", tc.wantRd, ExtractRd(expanded))
			}
			if ExtractRs1(expanded) != tc.wantRs1 {
				t.Errorf("expected rs1=%d, got %d", tc.wantRs1, ExtractRs1(expanded))
			}
			gotImm := ExtractIImm(expanded)
			if gotImm != int64(tc.wantImm) {
				t.Errorf("expected imm=%d, got %d", tc.wantImm, gotImm)
			}
		})
	}
}

// TestExpandC0_SW tests C.SW expansion
func TestExpandC0_SW(t *testing.T) {
	// C.SW: stores a 32-bit value to memory
	// Expands to: SW rs2', offset(rs1')
	// Format: funct3[15:13]=110, imm[5:3][12:10], rs1'[9:7], imm[2|6][6:5], rs2'[4:2], op[1:0]=00

	tests := []struct {
		name    string
		insn    uint16
		wantRs1 uint32
		wantRs2 uint32
		wantImm int32
	}{
		{
			name:    "C.SW x8, 0(x8)",
			insn:    0xC000, // funct3=110, imm=0, rs1'=0->x8, rs2'=0->x8
			wantRs1: 8,
			wantRs2: 8,
			wantImm: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expanded, err := ExpandCompressed(tc.insn, XLEN64)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if ExtractOpcode(expanded) != OpcodeStore {
				t.Errorf("expected OpcodeStore, got 0x%02x", ExtractOpcode(expanded))
			}
			if ExtractFunct3(expanded) != 2 { // SW funct3 = 2
				t.Errorf("expected funct3=2 (SW), got %d", ExtractFunct3(expanded))
			}
			if ExtractRs1(expanded) != tc.wantRs1 {
				t.Errorf("expected rs1=%d, got %d", tc.wantRs1, ExtractRs1(expanded))
			}
			if ExtractRs2(expanded) != tc.wantRs2 {
				t.Errorf("expected rs2=%d, got %d", tc.wantRs2, ExtractRs2(expanded))
			}
			gotImm := ExtractSImm(expanded)
			if gotImm != int64(tc.wantImm) {
				t.Errorf("expected imm=%d, got %d", tc.wantImm, gotImm)
			}
		})
	}
}

// TestExpandC0_SD tests C.SD expansion (RV64)
func TestExpandC0_SD(t *testing.T) {
	// C.SD: stores a 64-bit value to memory
	// Expands to: SD rs2', offset(rs1')
	// Format: funct3[15:13]=111, imm[5:3][12:10], rs1'[9:7], imm[7:6][6:5], rs2'[4:2], op[1:0]=00

	tests := []struct {
		name    string
		insn    uint16
		wantRs1 uint32
		wantRs2 uint32
		wantImm int32
	}{
		{
			name:    "C.SD x8, 0(x8)",
			insn:    0xE000, // funct3=111, imm=0, rs1'=0->x8, rs2'=0->x8
			wantRs1: 8,
			wantRs2: 8,
			wantImm: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expanded, err := ExpandCompressed(tc.insn, XLEN64)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if ExtractOpcode(expanded) != OpcodeStore {
				t.Errorf("expected OpcodeStore, got 0x%02x", ExtractOpcode(expanded))
			}
			if ExtractFunct3(expanded) != 3 { // SD funct3 = 3
				t.Errorf("expected funct3=3 (SD), got %d", ExtractFunct3(expanded))
			}
			if ExtractRs1(expanded) != tc.wantRs1 {
				t.Errorf("expected rs1=%d, got %d", tc.wantRs1, ExtractRs1(expanded))
			}
			if ExtractRs2(expanded) != tc.wantRs2 {
				t.Errorf("expected rs2=%d, got %d", tc.wantRs2, ExtractRs2(expanded))
			}
			gotImm := ExtractSImm(expanded)
			if gotImm != int64(tc.wantImm) {
				t.Errorf("expected imm=%d, got %d", tc.wantImm, gotImm)
			}
		})
	}
}

// TestExpandC1_ADDI tests C.ADDI and C.NOP expansion
func TestExpandC1_ADDI(t *testing.T) {
	// C.ADDI: adds the non-zero sign-extended 6-bit immediate to rd
	// Expands to: ADDI rd, rd, imm
	// Format: funct3[15:13]=000, imm[5][12], rd[11:7], imm[4:0][6:2], op[1:0]=01

	tests := []struct {
		name    string
		insn    uint16
		wantRd  uint32
		wantImm int32
	}{
		{
			name:    "C.NOP (rd=0, imm=0)",
			insn:    0x0001, // funct3=000, imm=0, rd=0
			wantRd:  0,
			wantImm: 0,
		},
		{
			name:    "C.ADDI x1, 1",
			insn:    0x0085, // funct3=000, imm=1, rd=1
			wantRd:  1,
			wantImm: 1,
		},
		{
			name:    "C.ADDI x1, -1",
			insn:    0x1085 | 0x007C, // funct3=000, imm=-1, rd=1
			wantRd:  1,
			wantImm: -1,
		},
		{
			name:    "C.ADDI x15, 31",
			insn:    0x07FD, // funct3=000, imm=31, rd=15
			wantRd:  15,
			wantImm: 31,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expanded, err := ExpandCompressed(tc.insn, XLEN64)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if ExtractOpcode(expanded) != OpcodeOpImm {
				t.Errorf("expected OpcodeOpImm, got 0x%02x", ExtractOpcode(expanded))
			}
			if ExtractFunct3(expanded) != Funct3ADDI {
				t.Errorf("expected funct3=ADDI, got %d", ExtractFunct3(expanded))
			}
			if ExtractRd(expanded) != tc.wantRd {
				t.Errorf("expected rd=%d, got %d", tc.wantRd, ExtractRd(expanded))
			}
			gotImm := ExtractIImm(expanded)
			if gotImm != int64(tc.wantImm) {
				t.Errorf("expected imm=%d, got %d", tc.wantImm, gotImm)
			}
		})
	}
}

// TestExpandC1_ADDIW tests C.ADDIW expansion (RV64)
func TestExpandC1_ADDIW(t *testing.T) {
	// C.ADDIW: adds the sign-extended 6-bit immediate to rd (word)
	// Expands to: ADDIW rd, rd, imm
	// Format: funct3[15:13]=001, imm[5][12], rd[11:7], imm[4:0][6:2], op[1:0]=01

	tests := []struct {
		name    string
		insn    uint16
		wantErr bool
		wantRd  uint32
		wantImm int32
	}{
		{
			name:    "C.ADDIW x1, 1",
			insn:    0x2085, // funct3=001, imm=1, rd=1
			wantRd:  1,
			wantImm: 1,
		},
		{
			name:    "C.ADDIW with rd=0 is reserved",
			insn:    0x2001, // funct3=001, imm=0, rd=0
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expanded, err := ExpandCompressed(tc.insn, XLEN64)
			if tc.wantErr {
				if err != ErrIllegalCompressedInsn {
					t.Errorf("expected illegal insn error, got err=%v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if ExtractOpcode(expanded) != OpcodeOpImm32 {
				t.Errorf("expected OpcodeOpImm32, got 0x%02x", ExtractOpcode(expanded))
			}
			if ExtractRd(expanded) != tc.wantRd {
				t.Errorf("expected rd=%d, got %d", tc.wantRd, ExtractRd(expanded))
			}
			gotImm := ExtractIImm(expanded)
			if gotImm != int64(tc.wantImm) {
				t.Errorf("expected imm=%d, got %d", tc.wantImm, gotImm)
			}
		})
	}
}

// TestExpandC1_LI tests C.LI expansion
func TestExpandC1_LI(t *testing.T) {
	// C.LI: loads the sign-extended 6-bit immediate into rd
	// Expands to: ADDI rd, x0, imm
	// Format: funct3[15:13]=010, imm[5][12], rd[11:7], imm[4:0][6:2], op[1:0]=01

	tests := []struct {
		name    string
		insn    uint16
		wantRd  uint32
		wantImm int32
	}{
		{
			name:    "C.LI x1, 0",
			insn:    0x4081, // funct3=010, imm=0, rd=1
			wantRd:  1,
			wantImm: 0,
		},
		{
			name:    "C.LI x1, 31",
			insn:    0x40FD, // funct3=010, imm=31, rd=1
			wantRd:  1,
			wantImm: 31,
		},
		{
			name:    "C.LI x1, -1",
			insn:    0x50FD, // funct3=010, imm=-1 (sign bit set), rd=1
			wantRd:  1,
			wantImm: -1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expanded, err := ExpandCompressed(tc.insn, XLEN64)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if ExtractOpcode(expanded) != OpcodeOpImm {
				t.Errorf("expected OpcodeOpImm, got 0x%02x", ExtractOpcode(expanded))
			}
			if ExtractFunct3(expanded) != Funct3ADDI {
				t.Errorf("expected funct3=ADDI, got %d", ExtractFunct3(expanded))
			}
			if ExtractRd(expanded) != tc.wantRd {
				t.Errorf("expected rd=%d, got %d", tc.wantRd, ExtractRd(expanded))
			}
			// rs1 should be x0
			if ExtractRs1(expanded) != 0 {
				t.Errorf("expected rs1=0, got %d", ExtractRs1(expanded))
			}
			gotImm := ExtractIImm(expanded)
			if gotImm != int64(tc.wantImm) {
				t.Errorf("expected imm=%d, got %d", tc.wantImm, gotImm)
			}
		})
	}
}

// TestExpandC1_LUI tests C.LUI expansion
func TestExpandC1_LUI(t *testing.T) {
	// C.LUI: loads the sign-extended upper immediate into rd
	// Expands to: LUI rd, imm
	// Format: funct3[15:13]=011, imm[17][12], rd[11:7], imm[16:12][6:2], op[1:0]=01
	// Note: rd=0 is HINT, rd=2 is C.ADDI16SP

	tests := []struct {
		name    string
		insn    uint16
		wantErr bool
		wantRd  uint32
		wantImm int32 // The upper 20-bit immediate (including bits 12-31)
	}{
		{
			name:    "C.LUI x1, 1 (imm=0x1000)",
			insn:    0x6085, // funct3=011, imm[16:12]=1, rd=1
			wantRd:  1,
			wantImm: 0x1000,
		},
		{
			name:    "C.LUI x1, 31 (imm=0x1F000)",
			insn:    0x60FD, // funct3=011, imm[16:12]=31, rd=1
			wantRd:  1,
			wantImm: 0x1F000,
		},
		{
			// NOTE: RISC-V spec says nzimm=0 is reserved, but C TinyEMU allows it.
			// We match C behavior for compatibility.
			// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:502-507
			name:    "C.LUI with nzimm=0 allowed (match C TinyEMU)",
			insn:    0x6081, // funct3=011, imm=0, rd=1
			wantRd:  1,
			wantImm: 0, // LUI with imm=0 effectively loads 0 into rd
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expanded, err := ExpandCompressed(tc.insn, XLEN64)
			if tc.wantErr {
				if err != ErrIllegalCompressedInsn {
					t.Errorf("expected illegal insn error, got err=%v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if ExtractOpcode(expanded) != OpcodeLUI {
				t.Errorf("expected OpcodeLUI, got 0x%02x", ExtractOpcode(expanded))
			}
			if ExtractRd(expanded) != tc.wantRd {
				t.Errorf("expected rd=%d, got %d", tc.wantRd, ExtractRd(expanded))
			}
			gotImm := ExtractUImm(expanded)
			if gotImm != int64(tc.wantImm) {
				t.Errorf("expected imm=%d, got %d", tc.wantImm, gotImm)
			}
		})
	}
}

// TestExpandC1_ADDI16SP tests C.ADDI16SP expansion
func TestExpandC1_ADDI16SP(t *testing.T) {
	// C.ADDI16SP: adds the sign-extended 10-bit immediate to sp
	// Expands to: ADDI x2, x2, imm
	// Format: funct3[15:13]=011, imm[9][12], rd=2, imm[4|6|8:7|5][6:2], op[1:0]=01

	tests := []struct {
		name    string
		insn    uint16
		wantErr bool
		wantImm int32
	}{
		{
			name:    "C.ADDI16SP imm=16",
			insn:    0x6141, // funct3=011, rd=2, imm=16
			wantImm: 16,
		},
		{
			// C.ADDI16SP: imm[9|4|6|8:7|5] = insn[12|6|5|4:3|2]
			// For imm=-16: -16 as 10-bit signed = 1008 = 0b1111110000
			// bit 9=1, bits 8:7=11, bit 6=1, bit 5=1, bit 4=1
			name:    "C.ADDI16SP imm=-16",
			insn:    0x717D, // funct3=011, bit12=1, rd=2, bits[6:2]=11111
			wantImm: -16,
		},
		{
			name:    "C.ADDI16SP with imm=0 is reserved",
			insn:    0x6101, // funct3=011, rd=2, imm=0
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expanded, err := ExpandCompressed(tc.insn, XLEN64)
			if tc.wantErr {
				if err != ErrIllegalCompressedInsn {
					t.Errorf("expected illegal insn error, got err=%v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if ExtractOpcode(expanded) != OpcodeOpImm {
				t.Errorf("expected OpcodeOpImm, got 0x%02x", ExtractOpcode(expanded))
			}
			if ExtractRd(expanded) != 2 {
				t.Errorf("expected rd=2, got %d", ExtractRd(expanded))
			}
			if ExtractRs1(expanded) != 2 {
				t.Errorf("expected rs1=2, got %d", ExtractRs1(expanded))
			}
			gotImm := ExtractIImm(expanded)
			if gotImm != int64(tc.wantImm) {
				t.Errorf("expected imm=%d, got %d", tc.wantImm, gotImm)
			}
		})
	}
}

// TestExpandC1_Arith tests C.SRLI, C.SRAI, C.ANDI, C.SUB, C.XOR, C.OR, C.AND, C.SUBW, C.ADDW
func TestExpandC1_Arith(t *testing.T) {
	tests := []struct {
		name       string
		insn       uint16
		wantOpcode uint32
		wantFunct3 uint32
		wantFunct7 uint32 // For R-type
		wantRd     uint32
		wantRs1    uint32
		wantRs2    uint32
		wantImm    int32 // For I-type (shift amount or AND immediate)
	}{
		{
			name:       "C.SRLI x8, 1",
			insn:       0x8005, // funct2=00, rd'=0->x8, shamt=1
			wantOpcode: OpcodeOpImm,
			wantFunct3: Funct3SRLI,
			wantRd:     8,
			wantRs1:    8,
			wantImm:    1,
		},
		{
			name:       "C.SRAI x8, 1",
			insn:       0x8405, // funct2=01, rd'=0->x8, shamt=1
			wantOpcode: OpcodeOpImm,
			wantFunct3: Funct3SRLI, // SRAI uses same funct3 as SRLI
			wantRd:     8,
			wantRs1:    8,
			wantImm:    0x401, // shamt with bit 10 set for SRAI
		},
		{
			name:       "C.ANDI x8, -1",
			insn:       0x987D, // funct2=10, rd'=0->x8, imm=-1
			wantOpcode: OpcodeOpImm,
			wantFunct3: Funct3ANDI,
			wantRd:     8,
			wantRs1:    8,
			wantImm:    -1,
		},
		{
			name:       "C.SUB x8, x9",
			insn:       0x8C05, // funct2=11, funct=000, rd'=0->x8, rs2'=1->x9
			wantOpcode: OpcodeOp,
			wantFunct3: Funct3ADD, // SUB uses same funct3 as ADD
			wantFunct7: 0x20,      // SUB has funct7=0x20
			wantRd:     8,
			wantRs1:    8,
			wantRs2:    9,
		},
		{
			name:       "C.XOR x8, x9",
			insn:       0x8C25, // funct2=11, funct=001, rd'=0->x8, rs2'=1->x9
			wantOpcode: OpcodeOp,
			wantFunct3: Funct3XOR,
			wantFunct7: 0,
			wantRd:     8,
			wantRs1:    8,
			wantRs2:    9,
		},
		{
			name:       "C.OR x8, x9",
			insn:       0x8C45, // funct2=11, funct=010, rd'=0->x8, rs2'=1->x9
			wantOpcode: OpcodeOp,
			wantFunct3: Funct3OR,
			wantFunct7: 0,
			wantRd:     8,
			wantRs1:    8,
			wantRs2:    9,
		},
		{
			name:       "C.AND x8, x9",
			insn:       0x8C65, // funct2=11, funct=011, rd'=0->x8, rs2'=1->x9
			wantOpcode: OpcodeOp,
			wantFunct3: Funct3AND,
			wantFunct7: 0,
			wantRd:     8,
			wantRs1:    8,
			wantRs2:    9,
		},
		{
			name:       "C.SUBW x8, x9",
			insn:       0x9C05, // funct2=11, funct=100, rd'=0->x8, rs2'=1->x9
			wantOpcode: OpcodeOp32,
			wantFunct3: 0,
			wantFunct7: 0x20,
			wantRd:     8,
			wantRs1:    8,
			wantRs2:    9,
		},
		{
			name:       "C.ADDW x8, x9",
			insn:       0x9C25, // funct2=11, funct=101, rd'=0->x8, rs2'=1->x9
			wantOpcode: OpcodeOp32,
			wantFunct3: 0,
			wantFunct7: 0,
			wantRd:     8,
			wantRs1:    8,
			wantRs2:    9,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expanded, err := ExpandCompressed(tc.insn, XLEN64)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if ExtractOpcode(expanded) != tc.wantOpcode {
				t.Errorf("expected opcode=0x%02x, got 0x%02x", tc.wantOpcode, ExtractOpcode(expanded))
			}
			if ExtractFunct3(expanded) != tc.wantFunct3 {
				t.Errorf("expected funct3=%d, got %d", tc.wantFunct3, ExtractFunct3(expanded))
			}
			if ExtractRd(expanded) != tc.wantRd {
				t.Errorf("expected rd=%d, got %d", tc.wantRd, ExtractRd(expanded))
			}
			if ExtractRs1(expanded) != tc.wantRs1 {
				t.Errorf("expected rs1=%d, got %d", tc.wantRs1, ExtractRs1(expanded))
			}

			// Check R-type specific fields
			if tc.wantOpcode == OpcodeOp || tc.wantOpcode == OpcodeOp32 {
				if ExtractRs2(expanded) != tc.wantRs2 {
					t.Errorf("expected rs2=%d, got %d", tc.wantRs2, ExtractRs2(expanded))
				}
				if ExtractFunct7(expanded) != tc.wantFunct7 {
					t.Errorf("expected funct7=0x%02x, got 0x%02x", tc.wantFunct7, ExtractFunct7(expanded))
				}
			}

			// Check I-type immediate for shifts and ANDI
			if tc.wantOpcode == OpcodeOpImm && tc.wantImm != 0 {
				gotImm := ExtractIImm(expanded)
				if gotImm != int64(tc.wantImm) {
					t.Errorf("expected imm=%d, got %d", tc.wantImm, gotImm)
				}
			}
		})
	}
}

// TestExpandC1_J tests C.J expansion
func TestExpandC1_J(t *testing.T) {
	// C.J: unconditional jump
	// Expands to: JAL x0, offset
	// Format: funct3[15:13]=101, imm[11|4|9:8|10|6|7|3:1|5][12:2], op[1:0]=01

	tests := []struct {
		name    string
		insn    uint16
		wantImm int32
	}{
		{
			name:    "C.J offset=0",
			insn:    0xA001, // funct3=101, imm=0
			wantImm: 0,
		},
		{
			name:    "C.J offset=2",
			insn:    0xA009, // funct3=101, imm=2
			wantImm: 2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expanded, err := ExpandCompressed(tc.insn, XLEN64)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if ExtractOpcode(expanded) != OpcodeJAL {
				t.Errorf("expected OpcodeJAL, got 0x%02x", ExtractOpcode(expanded))
			}
			// rd should be x0
			if ExtractRd(expanded) != 0 {
				t.Errorf("expected rd=0, got %d", ExtractRd(expanded))
			}
			gotImm := ExtractJImm(expanded)
			if gotImm != int64(tc.wantImm) {
				t.Errorf("expected imm=%d, got %d", tc.wantImm, gotImm)
			}
		})
	}
}

// TestExpandC1_BEQZ tests C.BEQZ expansion
func TestExpandC1_BEQZ(t *testing.T) {
	// C.BEQZ: branch if rs1' == 0
	// Expands to: BEQ rs1', x0, offset
	// Format: funct3[15:13]=110, imm[8|4:3][12:10], rs1'[9:7], imm[7:6|2:1|5][6:2], op[1:0]=01

	tests := []struct {
		name    string
		insn    uint16
		wantRs1 uint32
		wantImm int32
	}{
		{
			name:    "C.BEQZ x8, 0",
			insn:    0xC001, // funct3=110, rs1'=0->x8, imm=0
			wantRs1: 8,
			wantImm: 0,
		},
		{
			name:    "C.BEQZ x9, 2",
			insn:    0xC089, // funct3=110, rs1'=1->x9, imm=2
			wantRs1: 9,
			wantImm: 2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expanded, err := ExpandCompressed(tc.insn, XLEN64)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if ExtractOpcode(expanded) != OpcodeBranch {
				t.Errorf("expected OpcodeBranch, got 0x%02x", ExtractOpcode(expanded))
			}
			if ExtractFunct3(expanded) != Funct3BEQ {
				t.Errorf("expected funct3=BEQ, got %d", ExtractFunct3(expanded))
			}
			if ExtractRs1(expanded) != tc.wantRs1 {
				t.Errorf("expected rs1=%d, got %d", tc.wantRs1, ExtractRs1(expanded))
			}
			// rs2 should be x0
			if ExtractRs2(expanded) != 0 {
				t.Errorf("expected rs2=0, got %d", ExtractRs2(expanded))
			}
			gotImm := ExtractBImm(expanded)
			if gotImm != int64(tc.wantImm) {
				t.Errorf("expected imm=%d, got %d", tc.wantImm, gotImm)
			}
		})
	}
}

// TestExpandC1_BNEZ tests C.BNEZ expansion
func TestExpandC1_BNEZ(t *testing.T) {
	// C.BNEZ: branch if rs1' != 0
	// Expands to: BNE rs1', x0, offset
	// Format: funct3[15:13]=111, imm[8|4:3][12:10], rs1'[9:7], imm[7:6|2:1|5][6:2], op[1:0]=01

	tests := []struct {
		name    string
		insn    uint16
		wantRs1 uint32
		wantImm int32
	}{
		{
			name:    "C.BNEZ x8, 0",
			insn:    0xE001, // funct3=111, rs1'=0->x8, imm=0
			wantRs1: 8,
			wantImm: 0,
		},
		{
			name:    "C.BNEZ x9, 2",
			insn:    0xE089, // funct3=111, rs1'=1->x9, imm=2
			wantRs1: 9,
			wantImm: 2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expanded, err := ExpandCompressed(tc.insn, XLEN64)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if ExtractOpcode(expanded) != OpcodeBranch {
				t.Errorf("expected OpcodeBranch, got 0x%02x", ExtractOpcode(expanded))
			}
			if ExtractFunct3(expanded) != Funct3BNE {
				t.Errorf("expected funct3=BNE, got %d", ExtractFunct3(expanded))
			}
			if ExtractRs1(expanded) != tc.wantRs1 {
				t.Errorf("expected rs1=%d, got %d", tc.wantRs1, ExtractRs1(expanded))
			}
			// rs2 should be x0
			if ExtractRs2(expanded) != 0 {
				t.Errorf("expected rs2=0, got %d", ExtractRs2(expanded))
			}
			gotImm := ExtractBImm(expanded)
			if gotImm != int64(tc.wantImm) {
				t.Errorf("expected imm=%d, got %d", tc.wantImm, gotImm)
			}
		})
	}
}

// TestExpandC2_SLLI tests C.SLLI expansion
func TestExpandC2_SLLI(t *testing.T) {
	// C.SLLI: shifts rd left by shamt
	// Expands to: SLLI rd, rd, shamt
	// Format: funct3[15:13]=000, shamt[5][12], rd[11:7], shamt[4:0][6:2], op[1:0]=10

	tests := []struct {
		name      string
		insn      uint16
		wantRd    uint32
		wantShamt int32
	}{
		{
			name:      "C.SLLI x1, 1",
			insn:      0x0086, // funct3=000, shamt=1, rd=1
			wantRd:    1,
			wantShamt: 1,
		},
		{
			name:      "C.SLLI x1, 63 (RV64)",
			insn:      0x10FE, // funct3=000, shamt=63, rd=1
			wantRd:    1,
			wantShamt: 63,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expanded, err := ExpandCompressed(tc.insn, XLEN64)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if ExtractOpcode(expanded) != OpcodeOpImm {
				t.Errorf("expected OpcodeOpImm, got 0x%02x", ExtractOpcode(expanded))
			}
			if ExtractFunct3(expanded) != Funct3SLLI {
				t.Errorf("expected funct3=SLLI, got %d", ExtractFunct3(expanded))
			}
			if ExtractRd(expanded) != tc.wantRd {
				t.Errorf("expected rd=%d, got %d", tc.wantRd, ExtractRd(expanded))
			}
			if ExtractRs1(expanded) != tc.wantRd {
				t.Errorf("expected rs1=%d, got %d", tc.wantRd, ExtractRs1(expanded))
			}
			gotImm := ExtractIImm(expanded)
			if gotImm != int64(tc.wantShamt) {
				t.Errorf("expected shamt=%d, got %d", tc.wantShamt, gotImm)
			}
		})
	}
}

// TestExpandC2_LWSP tests C.LWSP expansion
func TestExpandC2_LWSP(t *testing.T) {
	// C.LWSP: loads a 32-bit value from stack pointer + offset
	// Expands to: LW rd, offset(x2)
	// Format: funct3[15:13]=010, imm[5][12], rd[11:7], imm[4:2|7:6][6:2], op[1:0]=10

	tests := []struct {
		name    string
		insn    uint16
		wantErr bool
		wantRd  uint32
		wantImm int32
	}{
		{
			name:    "C.LWSP x1, 0(sp)",
			insn:    0x4082, // funct3=010, rd=1, imm=0
			wantRd:  1,
			wantImm: 0,
		},
		{
			name:    "C.LWSP x1, 4(sp)",
			insn:    0x4092, // funct3=010, rd=1, imm=4
			wantRd:  1,
			wantImm: 4,
		},
		{
			// NOTE: RISC-V spec says rd=0 is reserved, but C TinyEMU allows it.
			// C performs the load (which can fault) then discards result.
			// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:650-661
			name:    "C.LWSP with rd=0 allowed (match C TinyEMU)",
			insn:    0x4002, // funct3=010, rd=0
			wantRd:  0,
			wantImm: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expanded, err := ExpandCompressed(tc.insn, XLEN64)
			if tc.wantErr {
				if err != ErrIllegalCompressedInsn {
					t.Errorf("expected illegal insn error, got err=%v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if ExtractOpcode(expanded) != OpcodeLoad {
				t.Errorf("expected OpcodeLoad, got 0x%02x", ExtractOpcode(expanded))
			}
			if ExtractFunct3(expanded) != Funct3LW {
				t.Errorf("expected funct3=LW, got %d", ExtractFunct3(expanded))
			}
			if ExtractRd(expanded) != tc.wantRd {
				t.Errorf("expected rd=%d, got %d", tc.wantRd, ExtractRd(expanded))
			}
			// rs1 should be x2 (sp)
			if ExtractRs1(expanded) != 2 {
				t.Errorf("expected rs1=2, got %d", ExtractRs1(expanded))
			}
			gotImm := ExtractIImm(expanded)
			if gotImm != int64(tc.wantImm) {
				t.Errorf("expected imm=%d, got %d", tc.wantImm, gotImm)
			}
		})
	}
}

// TestExpandC2_LDSP tests C.LDSP expansion (RV64)
func TestExpandC2_LDSP(t *testing.T) {
	// C.LDSP: loads a 64-bit value from stack pointer + offset
	// Expands to: LD rd, offset(x2)
	// Format: funct3[15:13]=011, imm[5][12], rd[11:7], imm[4:3|8:6][6:2], op[1:0]=10

	tests := []struct {
		name    string
		insn    uint16
		wantErr bool
		wantRd  uint32
		wantImm int32
	}{
		{
			name:    "C.LDSP x1, 0(sp)",
			insn:    0x6082, // funct3=011, rd=1, imm=0
			wantRd:  1,
			wantImm: 0,
		},
		{
			// C.LDSP: imm[5|4:3|8:6] = insn[12|6:5|4:2]
			// For imm=8: imm[3]=1, rest=0, so insn[6:5]=01, insn[4:2]=000
			name:    "C.LDSP x1, 8(sp)",
			insn:    0x60A2, // funct3=011, rd=1, bits[6:2]=01000
			wantRd:  1,
			wantImm: 8,
		},
		{
			// NOTE: RISC-V spec says rd=0 is reserved, but C TinyEMU allows it.
			// C performs the load (which can fault) then discards result.
			// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:663-675
			name:    "C.LDSP with rd=0 allowed (match C TinyEMU)",
			insn:    0x6002, // funct3=011, rd=0
			wantRd:  0,
			wantImm: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expanded, err := ExpandCompressed(tc.insn, XLEN64)
			if tc.wantErr {
				if err != ErrIllegalCompressedInsn {
					t.Errorf("expected illegal insn error, got err=%v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if ExtractOpcode(expanded) != OpcodeLoad {
				t.Errorf("expected OpcodeLoad, got 0x%02x", ExtractOpcode(expanded))
			}
			if ExtractFunct3(expanded) != Funct3LD {
				t.Errorf("expected funct3=LD, got %d", ExtractFunct3(expanded))
			}
			if ExtractRd(expanded) != tc.wantRd {
				t.Errorf("expected rd=%d, got %d", tc.wantRd, ExtractRd(expanded))
			}
			if ExtractRs1(expanded) != 2 {
				t.Errorf("expected rs1=2, got %d", ExtractRs1(expanded))
			}
			gotImm := ExtractIImm(expanded)
			if gotImm != int64(tc.wantImm) {
				t.Errorf("expected imm=%d, got %d", tc.wantImm, gotImm)
			}
		})
	}
}

// TestExpandC2_JR tests C.JR expansion
func TestExpandC2_JR(t *testing.T) {
	// C.JR: jump to address in rs1
	// Expands to: JALR x0, rs1, 0
	// Format: funct3[15:13]=100, 0[12], rs1[11:7], 0[6:2], op[1:0]=10

	tests := []struct {
		name    string
		insn    uint16
		wantErr bool
		wantRs1 uint32
	}{
		{
			name:    "C.JR x1",
			insn:    0x8082, // funct3=100, bit12=0, rs1=1, rs2=0
			wantRs1: 1,
		},
		{
			name:    "C.JR with rs1=0 is reserved",
			insn:    0x8002, // funct3=100, bit12=0, rs1=0, rs2=0
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expanded, err := ExpandCompressed(tc.insn, XLEN64)
			if tc.wantErr {
				if err != ErrIllegalCompressedInsn {
					t.Errorf("expected illegal insn error, got err=%v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if ExtractOpcode(expanded) != OpcodeJALR {
				t.Errorf("expected OpcodeJALR, got 0x%02x", ExtractOpcode(expanded))
			}
			// rd should be x0
			if ExtractRd(expanded) != 0 {
				t.Errorf("expected rd=0, got %d", ExtractRd(expanded))
			}
			if ExtractRs1(expanded) != tc.wantRs1 {
				t.Errorf("expected rs1=%d, got %d", tc.wantRs1, ExtractRs1(expanded))
			}
			// imm should be 0
			gotImm := ExtractIImm(expanded)
			if gotImm != 0 {
				t.Errorf("expected imm=0, got %d", gotImm)
			}
		})
	}
}

// TestExpandC2_MV tests C.MV expansion
func TestExpandC2_MV(t *testing.T) {
	// C.MV: copies rs2 to rd
	// Expands to: ADD rd, x0, rs2
	// Format: funct3[15:13]=100, 0[12], rd[11:7], rs2[6:2], op[1:0]=10

	tests := []struct {
		name    string
		insn    uint16
		wantRd  uint32
		wantRs2 uint32
	}{
		{
			name:    "C.MV x1, x2",
			insn:    0x808A, // funct3=100, bit12=0, rd=1, rs2=2
			wantRd:  1,
			wantRs2: 2,
		},
		{
			name:    "C.MV x15, x31",
			insn:    0x87FE, // funct3=100, bit12=0, rd=15, rs2=31
			wantRd:  15,
			wantRs2: 31,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expanded, err := ExpandCompressed(tc.insn, XLEN64)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if ExtractOpcode(expanded) != OpcodeOp {
				t.Errorf("expected OpcodeOp, got 0x%02x", ExtractOpcode(expanded))
			}
			if ExtractFunct3(expanded) != Funct3ADD {
				t.Errorf("expected funct3=ADD, got %d", ExtractFunct3(expanded))
			}
			if ExtractRd(expanded) != tc.wantRd {
				t.Errorf("expected rd=%d, got %d", tc.wantRd, ExtractRd(expanded))
			}
			// rs1 should be x0
			if ExtractRs1(expanded) != 0 {
				t.Errorf("expected rs1=0, got %d", ExtractRs1(expanded))
			}
			if ExtractRs2(expanded) != tc.wantRs2 {
				t.Errorf("expected rs2=%d, got %d", tc.wantRs2, ExtractRs2(expanded))
			}
		})
	}
}

// TestExpandC2_JALR tests C.JALR expansion
func TestExpandC2_JALR(t *testing.T) {
	// C.JALR: jump to address in rs1 and link
	// Expands to: JALR x1, rs1, 0
	// Format: funct3[15:13]=100, 1[12], rs1[11:7], 0[6:2], op[1:0]=10

	tests := []struct {
		name    string
		insn    uint16
		wantRs1 uint32
	}{
		{
			name:    "C.JALR x1",
			insn:    0x9082, // funct3=100, bit12=1, rs1=1, rs2=0
			wantRs1: 1,
		},
		{
			name:    "C.JALR x15",
			insn:    0x9782, // funct3=100, bit12=1, rs1=15, rs2=0
			wantRs1: 15,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expanded, err := ExpandCompressed(tc.insn, XLEN64)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if ExtractOpcode(expanded) != OpcodeJALR {
				t.Errorf("expected OpcodeJALR, got 0x%02x", ExtractOpcode(expanded))
			}
			// rd should be x1 (ra)
			if ExtractRd(expanded) != 1 {
				t.Errorf("expected rd=1, got %d", ExtractRd(expanded))
			}
			if ExtractRs1(expanded) != tc.wantRs1 {
				t.Errorf("expected rs1=%d, got %d", tc.wantRs1, ExtractRs1(expanded))
			}
			gotImm := ExtractIImm(expanded)
			if gotImm != 0 {
				t.Errorf("expected imm=0, got %d", gotImm)
			}
		})
	}
}

// TestExpandC2_ADD tests C.ADD expansion
func TestExpandC2_ADD(t *testing.T) {
	// C.ADD: adds rs2 to rd
	// Expands to: ADD rd, rd, rs2
	// Format: funct3[15:13]=100, 1[12], rd[11:7], rs2[6:2], op[1:0]=10

	tests := []struct {
		name    string
		insn    uint16
		wantRd  uint32
		wantRs2 uint32
	}{
		{
			name:    "C.ADD x1, x2",
			insn:    0x908A, // funct3=100, bit12=1, rd=1, rs2=2
			wantRd:  1,
			wantRs2: 2,
		},
		{
			name:    "C.ADD x15, x31",
			insn:    0x97FE, // funct3=100, bit12=1, rd=15, rs2=31
			wantRd:  15,
			wantRs2: 31,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expanded, err := ExpandCompressed(tc.insn, XLEN64)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if ExtractOpcode(expanded) != OpcodeOp {
				t.Errorf("expected OpcodeOp, got 0x%02x", ExtractOpcode(expanded))
			}
			if ExtractFunct3(expanded) != Funct3ADD {
				t.Errorf("expected funct3=ADD, got %d", ExtractFunct3(expanded))
			}
			if ExtractRd(expanded) != tc.wantRd {
				t.Errorf("expected rd=%d, got %d", tc.wantRd, ExtractRd(expanded))
			}
			if ExtractRs1(expanded) != tc.wantRd {
				t.Errorf("expected rs1=%d, got %d", tc.wantRd, ExtractRs1(expanded))
			}
			if ExtractRs2(expanded) != tc.wantRs2 {
				t.Errorf("expected rs2=%d, got %d", tc.wantRs2, ExtractRs2(expanded))
			}
		})
	}
}

// TestExpandC2_EBREAK tests C.EBREAK expansion
func TestExpandC2_EBREAK(t *testing.T) {
	// C.EBREAK: generates a breakpoint exception
	// Expands to: EBREAK
	// Format: funct3[15:13]=100, 1[12], 0[11:7], 0[6:2], op[1:0]=10

	insn := uint16(0x9002) // C.EBREAK
	expanded, err := ExpandCompressed(insn, XLEN64)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ExtractOpcode(expanded) != OpcodeSystem {
		t.Errorf("expected OpcodeSystem, got 0x%02x", ExtractOpcode(expanded))
	}
	// EBREAK has imm=1
	gotImm := ExtractIImm(expanded)
	if gotImm != 1 {
		t.Errorf("expected imm=1 (EBREAK), got %d", gotImm)
	}
}

// TestExpandC2_SWSP tests C.SWSP expansion
func TestExpandC2_SWSP(t *testing.T) {
	// C.SWSP: stores a 32-bit value to stack pointer + offset
	// Expands to: SW rs2, offset(x2)
	// Format: funct3[15:13]=110, imm[5:2|7:6][12:7], rs2[6:2], op[1:0]=10

	tests := []struct {
		name    string
		insn    uint16
		wantRs2 uint32
		wantImm int32
	}{
		{
			name:    "C.SWSP x1, 0(sp)",
			insn:    0xC006, // funct3=110, imm=0, rs2=1
			wantRs2: 1,
			wantImm: 0,
		},
		{
			name:    "C.SWSP x1, 4(sp)",
			insn:    0xC206, // funct3=110, imm=4, rs2=1
			wantRs2: 1,
			wantImm: 4,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expanded, err := ExpandCompressed(tc.insn, XLEN64)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if ExtractOpcode(expanded) != OpcodeStore {
				t.Errorf("expected OpcodeStore, got 0x%02x", ExtractOpcode(expanded))
			}
			if ExtractFunct3(expanded) != 2 { // SW funct3 = 2
				t.Errorf("expected funct3=2 (SW), got %d", ExtractFunct3(expanded))
			}
			// rs1 should be x2 (sp)
			if ExtractRs1(expanded) != 2 {
				t.Errorf("expected rs1=2, got %d", ExtractRs1(expanded))
			}
			if ExtractRs2(expanded) != tc.wantRs2 {
				t.Errorf("expected rs2=%d, got %d", tc.wantRs2, ExtractRs2(expanded))
			}
			gotImm := ExtractSImm(expanded)
			if gotImm != int64(tc.wantImm) {
				t.Errorf("expected imm=%d, got %d", tc.wantImm, gotImm)
			}
		})
	}
}

// TestExpandC2_SDSP tests C.SDSP expansion (RV64)
func TestExpandC2_SDSP(t *testing.T) {
	// C.SDSP: stores a 64-bit value to stack pointer + offset
	// Expands to: SD rs2, offset(x2)
	// Format: funct3[15:13]=111, imm[5:3|8:6][12:7], rs2[6:2], op[1:0]=10

	tests := []struct {
		name    string
		insn    uint16
		wantRs2 uint32
		wantImm int32
	}{
		{
			name:    "C.SDSP x1, 0(sp)",
			insn:    0xE006, // funct3=111, imm=0, rs2=1
			wantRs2: 1,
			wantImm: 0,
		},
		{
			name:    "C.SDSP x1, 8(sp)",
			insn:    0xE406, // funct3=111, imm=8, rs2=1
			wantRs2: 1,
			wantImm: 8,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expanded, err := ExpandCompressed(tc.insn, XLEN64)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if ExtractOpcode(expanded) != OpcodeStore {
				t.Errorf("expected OpcodeStore, got 0x%02x", ExtractOpcode(expanded))
			}
			if ExtractFunct3(expanded) != 3 { // SD funct3 = 3
				t.Errorf("expected funct3=3 (SD), got %d", ExtractFunct3(expanded))
			}
			if ExtractRs1(expanded) != 2 {
				t.Errorf("expected rs1=2, got %d", ExtractRs1(expanded))
			}
			if ExtractRs2(expanded) != tc.wantRs2 {
				t.Errorf("expected rs2=%d, got %d", tc.wantRs2, ExtractRs2(expanded))
			}
			gotImm := ExtractSImm(expanded)
			if gotImm != int64(tc.wantImm) {
				t.Errorf("expected imm=%d, got %d", tc.wantImm, gotImm)
			}
		})
	}
}

// TestHelperGetField1 tests the getField1 helper function
func TestHelperGetField1(t *testing.T) {
	tests := []struct {
		insn   uint16
		srcBit int
		dstLo  int
		dstHi  int
		want   uint32
	}{
		{0xFFFF, 0, 0, 0, 1},     // Extract bit 0, put at bit 0
		{0xFFFF, 0, 0, 3, 0xF},   // Extract 4 bits from bit 0
		{0x0080, 7, 0, 0, 1},     // Extract bit 7, put at bit 0
		{0x0080, 7, 4, 4, 0x10},  // Extract bit 7, put at bit 4
		{0x1000, 12, 5, 5, 0x20}, // Extract bit 12, put at bit 5
		{0x0E00, 9, 6, 8, 0x1C0}, // Extract 3 bits from bit 9, put at bits 6-8
	}

	for _, tc := range tests {
		got := getField1(tc.insn, tc.srcBit, tc.dstLo, tc.dstHi)
		if got != tc.want {
			t.Errorf("getField1(0x%04x, %d, %d, %d) = 0x%x, want 0x%x",
				tc.insn, tc.srcBit, tc.dstLo, tc.dstHi, got, tc.want)
		}
	}
}

// TestHelperSextC tests the sextC helper function
func TestHelperSextC(t *testing.T) {
	tests := []struct {
		val      uint32
		fromBits int
		want     int32
	}{
		{0, 6, 0},
		{31, 6, 31},       // Max positive 6-bit
		{32, 6, -32},      // Min negative 6-bit
		{63, 6, -1},       // All 1s in 6 bits
		{0x1FF, 10, 511},  // Max positive 10-bit
		{0x200, 10, -512}, // Min negative 10-bit
	}

	for _, tc := range tests {
		got := sextC(tc.val, tc.fromBits)
		if got != tc.want {
			t.Errorf("sextC(0x%x, %d) = %d, want %d",
				tc.val, tc.fromBits, got, tc.want)
		}
	}
}

// TestInstructionEncoders tests the instruction encoding helpers
func TestInstructionEncoders(t *testing.T) {
	t.Run("encodeRType", func(t *testing.T) {
		// ADD x1, x2, x3
		encoded := encodeRType(OpcodeOp, 1, Funct3ADD, 2, 3, 0)
		if ExtractOpcode(encoded) != OpcodeOp {
			t.Errorf("opcode mismatch")
		}
		if ExtractRd(encoded) != 1 {
			t.Errorf("rd mismatch")
		}
		if ExtractFunct3(encoded) != Funct3ADD {
			t.Errorf("funct3 mismatch")
		}
		if ExtractRs1(encoded) != 2 {
			t.Errorf("rs1 mismatch")
		}
		if ExtractRs2(encoded) != 3 {
			t.Errorf("rs2 mismatch")
		}
		if ExtractFunct7(encoded) != 0 {
			t.Errorf("funct7 mismatch")
		}
	})

	t.Run("encodeIType", func(t *testing.T) {
		// ADDI x1, x2, 100
		encoded := encodeIType(OpcodeOpImm, 1, Funct3ADDI, 2, 100)
		if ExtractOpcode(encoded) != OpcodeOpImm {
			t.Errorf("opcode mismatch")
		}
		if ExtractRd(encoded) != 1 {
			t.Errorf("rd mismatch")
		}
		if ExtractFunct3(encoded) != Funct3ADDI {
			t.Errorf("funct3 mismatch")
		}
		if ExtractRs1(encoded) != 2 {
			t.Errorf("rs1 mismatch")
		}
		if ExtractIImm(encoded) != 100 {
			t.Errorf("imm mismatch: got %d, want 100", ExtractIImm(encoded))
		}
	})

	t.Run("encodeSType", func(t *testing.T) {
		// SW x3, 8(x2)
		encoded := encodeSType(OpcodeStore, 2, 2, 3, 8)
		if ExtractOpcode(encoded) != OpcodeStore {
			t.Errorf("opcode mismatch")
		}
		if ExtractFunct3(encoded) != 2 {
			t.Errorf("funct3 mismatch")
		}
		if ExtractRs1(encoded) != 2 {
			t.Errorf("rs1 mismatch")
		}
		if ExtractRs2(encoded) != 3 {
			t.Errorf("rs2 mismatch")
		}
		if ExtractSImm(encoded) != 8 {
			t.Errorf("imm mismatch: got %d, want 8", ExtractSImm(encoded))
		}
	})

	t.Run("encodeBType", func(t *testing.T) {
		// BEQ x1, x2, 8
		encoded := encodeBType(OpcodeBranch, Funct3BEQ, 1, 2, 8)
		if ExtractOpcode(encoded) != OpcodeBranch {
			t.Errorf("opcode mismatch")
		}
		if ExtractFunct3(encoded) != Funct3BEQ {
			t.Errorf("funct3 mismatch")
		}
		if ExtractRs1(encoded) != 1 {
			t.Errorf("rs1 mismatch")
		}
		if ExtractRs2(encoded) != 2 {
			t.Errorf("rs2 mismatch")
		}
		if ExtractBImm(encoded) != 8 {
			t.Errorf("imm mismatch: got %d, want 8", ExtractBImm(encoded))
		}
	})

	t.Run("encodeUType", func(t *testing.T) {
		// LUI x1, 0x12345
		encoded := encodeUType(OpcodeLUI, 1, 0x12345000)
		if ExtractOpcode(encoded) != OpcodeLUI {
			t.Errorf("opcode mismatch")
		}
		if ExtractRd(encoded) != 1 {
			t.Errorf("rd mismatch")
		}
		if ExtractUImm(encoded) != 0x12345000 {
			t.Errorf("imm mismatch: got 0x%x, want 0x12345000", ExtractUImm(encoded))
		}
	})

	t.Run("encodeJType", func(t *testing.T) {
		// JAL x1, 8
		encoded := encodeJType(OpcodeJAL, 1, 8)
		if ExtractOpcode(encoded) != OpcodeJAL {
			t.Errorf("opcode mismatch")
		}
		if ExtractRd(encoded) != 1 {
			t.Errorf("rd mismatch")
		}
		if ExtractJImm(encoded) != 8 {
			t.Errorf("imm mismatch: got %d, want 8", ExtractJImm(encoded))
		}
	})
}

// TestIllegalInstructions verifies that reserved/illegal encodings return errors
func TestIllegalInstructions(t *testing.T) {
	// NOTE: C.LUI nzimm=0 is NOT in this list because C TinyEMU allows it.
	// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:502-507
	illegals := []struct {
		name string
		insn uint16
	}{
		{"C.ADDI4SPN imm=0", 0x0000},
		{"C.ADDIW rd=0", 0x2001},
		{"C.ADDI16SP imm=0", 0x6101},
		// C.LUI nzimm=0 is allowed to match C TinyEMU behavior
		// C.LWSP rd=0 and C.LDSP rd=0 are allowed to match C TinyEMU behavior
		// (C performs the load, can fault, then discards result)
		// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:650-675
		{"C.JR rs1=0", 0x8002},
		{"Quadrant 3", 0x0003},
	}

	for _, tc := range illegals {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ExpandCompressed(tc.insn, XLEN64)
			if err != ErrIllegalCompressedInsn {
				t.Errorf("expected illegal insn error for %s (0x%04x), got %v",
					tc.name, tc.insn, err)
			}
		})
	}
}

// TestCompressedJump4Regression is a regression test for a bug where C.J +4
// (a compressed jump that happens to jump exactly 4 bytes forward) had its
// PC incorrectly adjusted. The old code used a heuristic that if PC == originalPC+4
// after execution, it must have been a non-jump instruction that added 4, so it
// "fixed" it to +2. But a C.J +4 legitimately jumps to originalPC+4.
//
// This also tests that C.J does NOT modify ra (x1), unlike C.JAL.
//
// Reference: RV64UC rvc compliance test case 30
func TestCompressedJump4Regression(t *testing.T) {
	// This test matches rv64uc-p-rvc test case 30:
	// li ra, 0
	// c.j +4      # jumps over next instruction
	// c.j +4      # skipped
	// c.j +4      # executed, jumps forward
	// c.j fail    # skipped
	// nop
	// Check ra is still 0

	// Create memory and CPU
	memMap := mem.NewPhysMemoryMap()
	defer memMap.Close()

	ram, err := memMap.RegisterRAM(0x80000000, 0x10000, 0)
	if err != nil {
		t.Fatalf("RegisterRAM failed: %v", err)
	}

	// Write the test program at offset 0x100
	offset := 0x100
	putU16(ram.PhysMem, offset, 0x4081)    // c.li ra, 0 (offset+0)
	putU16(ram.PhysMem, offset+2, 0xa011)  // c.j +4 (offset+2 -> offset+6)
	putU16(ram.PhysMem, offset+4, 0xa011)  // c.j +4 (skipped)
	putU16(ram.PhysMem, offset+6, 0xa011)  // c.j +4 (offset+6 -> offset+10)
	putU16(ram.PhysMem, offset+8, 0xa011)  // c.j +4 (skipped, would be fail)
	putU16(ram.PhysMem, offset+10, 0x0001) // c.nop

	cpu := NewCPU(memMap, XLEN64)
	cpu.PC = 0x80000000 + uint64(offset)

	// Execute c.li ra, 0
	cpu.Step()
	if cpu.GetReg(1) != 0 {
		t.Errorf("after c.li ra,0: ra=%d, want 0", cpu.GetReg(1))
	}
	if cpu.PC != 0x80000000+uint64(offset)+2 {
		t.Errorf("after c.li: PC=0x%x, want 0x%x", cpu.PC, 0x80000000+uint64(offset)+2)
	}

	// Execute first c.j +4 (should jump from offset+2 to offset+6)
	cpu.Step()
	if cpu.PC != 0x80000000+uint64(offset)+6 {
		t.Errorf("after first c.j +4: PC=0x%x, want 0x%x", cpu.PC, 0x80000000+uint64(offset)+6)
	}
	if cpu.GetReg(1) != 0 {
		t.Errorf("c.j modified ra: ra=%d, want 0", cpu.GetReg(1))
	}

	// Execute second c.j +4 (should jump from offset+6 to offset+10)
	cpu.Step()
	if cpu.PC != 0x80000000+uint64(offset)+10 {
		t.Errorf("after second c.j +4: PC=0x%x, want 0x%x", cpu.PC, 0x80000000+uint64(offset)+10)
	}
	if cpu.GetReg(1) != 0 {
		t.Errorf("c.j modified ra: ra=%d, want 0", cpu.GetReg(1))
	}
}

// TestCompressedBranchNotTaken tests that a compressed branch that is not taken
// correctly increments PC by 2 (not 4).
//
// Reference: RV64UC rvc compliance test case 33
func TestCompressedBranchNotTaken(t *testing.T) {
	memMap := mem.NewPhysMemoryMap()
	defer memMap.Close()

	ram, err := memMap.RegisterRAM(0x80000000, 0x10000, 0)
	if err != nil {
		t.Fatalf("RegisterRAM failed: %v", err)
	}

	// Write test program
	offset := 0x100
	putU16(ram.PhysMem, offset, 0x4505)   // c.li a0, 1 (a0 = 1)
	putU16(ram.PhysMem, offset+2, 0xc101) // c.beqz a0, +0 (branch not taken since a0 != 0)
	putU16(ram.PhysMem, offset+4, 0x0001) // c.nop

	cpu := NewCPU(memMap, XLEN64)
	cpu.PC = 0x80000000 + uint64(offset)

	// Execute c.li a0, 1
	cpu.Step()
	if cpu.GetReg(10) != 1 {
		t.Errorf("after c.li a0,1: a0=%d, want 1", cpu.GetReg(10))
	}

	// Execute c.beqz a0, +0 (should NOT branch since a0 == 1)
	oldPC := cpu.PC
	cpu.Step()
	// PC should increment by 2 (compressed instruction size), not 4
	if cpu.PC != oldPC+2 {
		t.Errorf("after c.beqz (not taken): PC=0x%x, want 0x%x (oldPC+2)", cpu.PC, oldPC+2)
	}
}

// Helper to write a uint16 to memory
func putU16(mem []byte, offset int, val uint16) {
	mem[offset] = byte(val)
	mem[offset+1] = byte(val >> 8)
}
