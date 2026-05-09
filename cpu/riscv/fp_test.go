package riscv

import (
	"math"
	"testing"
)

// Helper to convert float32 to its bit representation
func f32bits(f float32) uint32 {
	return math.Float32bits(f)
}

// Helper to convert float64 to its bit representation
func f64bits(f float64) uint64 {
	return math.Float64bits(f)
}

// encodeFLW encodes FLW instruction: FLW fd, offset(rs1)
// Format: imm[11:0] | rs1 | 010 | rd | 0000111
func encodeFLW(fd, rs1 int, offset int16) uint32 {
	return (uint32(offset&0xFFF) << 20) | (uint32(rs1) << 15) | (2 << 12) | (uint32(fd) << 7) | 0x07
}

// encodeFSW encodes FSW instruction: FSW fs2, offset(rs1)
// Format: imm[11:5] | rs2 | rs1 | 010 | imm[4:0] | 0100111
func encodeFSW(fs2, rs1 int, offset int16) uint32 {
	imm := uint32(offset & 0xFFF)
	return ((imm >> 5) << 25) | (uint32(fs2) << 20) | (uint32(rs1) << 15) | (2 << 12) | ((imm & 0x1F) << 7) | 0x27
}

// encodeFLD encodes FLD instruction: FLD fd, offset(rs1)
func encodeFLD(fd, rs1 int, offset int16) uint32 {
	return (uint32(offset&0xFFF) << 20) | (uint32(rs1) << 15) | (3 << 12) | (uint32(fd) << 7) | 0x07
}

// encodeFSD encodes FSD instruction: FSD fs2, offset(rs1)
func encodeFSD(fs2, rs1 int, offset int16) uint32 {
	imm := uint32(offset & 0xFFF)
	return ((imm >> 5) << 25) | (uint32(fs2) << 20) | (uint32(rs1) << 15) | (3 << 12) | ((imm & 0x1F) << 7) | 0x27
}

// encodeFADDS encodes FADD.S instruction: FADD.S fd, fs1, fs2
// Format: 0000000 | rs2 | rs1 | rm | rd | 1010011
func encodeFADDS(fd, fs1, fs2, rm int) uint32 {
	return (0 << 25) | (uint32(fs2) << 20) | (uint32(fs1) << 15) | (uint32(rm) << 12) | (uint32(fd) << 7) | 0x53
}

// encodeFSUBS encodes FSUB.S instruction
func encodeFSUBS(fd, fs1, fs2, rm int) uint32 {
	return (0x04 << 25) | (uint32(fs2) << 20) | (uint32(fs1) << 15) | (uint32(rm) << 12) | (uint32(fd) << 7) | 0x53
}

// encodeFMULS encodes FMUL.S instruction
func encodeFMULS(fd, fs1, fs2, rm int) uint32 {
	return (0x08 << 25) | (uint32(fs2) << 20) | (uint32(fs1) << 15) | (uint32(rm) << 12) | (uint32(fd) << 7) | 0x53
}

// encodeFDIVS encodes FDIV.S instruction
func encodeFDIVS(fd, fs1, fs2, rm int) uint32 {
	return (0x0C << 25) | (uint32(fs2) << 20) | (uint32(fs1) << 15) | (uint32(rm) << 12) | (uint32(fd) << 7) | 0x53
}

// encodeFSQRTS encodes FSQRT.S instruction
func encodeFSQRTS(fd, fs1, rm int) uint32 {
	return (0x2C << 25) | (0 << 20) | (uint32(fs1) << 15) | (uint32(rm) << 12) | (uint32(fd) << 7) | 0x53
}

// encodeFMADDS encodes FMADD.S instruction
func encodeFMADDS(fd, fs1, fs2, fs3, rm int) uint32 {
	return (uint32(fs3) << 27) | (0 << 25) | (uint32(fs2) << 20) | (uint32(fs1) << 15) | (uint32(rm) << 12) | (uint32(fd) << 7) | 0x43
}

// encodeFMSUBS encodes FMSUB.S instruction
func encodeFMSUBS(fd, fs1, fs2, fs3, rm int) uint32 {
	return (uint32(fs3) << 27) | (0 << 25) | (uint32(fs2) << 20) | (uint32(fs1) << 15) | (uint32(rm) << 12) | (uint32(fd) << 7) | 0x47
}

// encodeFNMSUBS encodes FNMSUB.S instruction
func encodeFNMSUBS(fd, fs1, fs2, fs3, rm int) uint32 {
	return (uint32(fs3) << 27) | (0 << 25) | (uint32(fs2) << 20) | (uint32(fs1) << 15) | (uint32(rm) << 12) | (uint32(fd) << 7) | 0x4B
}

// encodeFNMADDS encodes FNMADD.S instruction
func encodeFNMADDS(fd, fs1, fs2, fs3, rm int) uint32 {
	return (uint32(fs3) << 27) | (0 << 25) | (uint32(fs2) << 20) | (uint32(fs1) << 15) | (uint32(rm) << 12) | (uint32(fd) << 7) | 0x4F
}

// encodeFSGNJS encodes FSGNJ.S instruction (rm=0)
func encodeFSGNJS(fd, fs1, fs2 int) uint32 {
	return (0x10 << 25) | (uint32(fs2) << 20) | (uint32(fs1) << 15) | (0 << 12) | (uint32(fd) << 7) | 0x53
}

// encodeFSGNJNS encodes FSGNJN.S instruction (rm=1)
func encodeFSGNJNS(fd, fs1, fs2 int) uint32 {
	return (0x10 << 25) | (uint32(fs2) << 20) | (uint32(fs1) << 15) | (1 << 12) | (uint32(fd) << 7) | 0x53
}

// encodeFSGNJXS encodes FSGNJX.S instruction (rm=2)
func encodeFSGNJXS(fd, fs1, fs2 int) uint32 {
	return (0x10 << 25) | (uint32(fs2) << 20) | (uint32(fs1) << 15) | (2 << 12) | (uint32(fd) << 7) | 0x53
}

// encodeFMINS encodes FMIN.S instruction
func encodeFMINS(fd, fs1, fs2 int) uint32 {
	return (0x14 << 25) | (uint32(fs2) << 20) | (uint32(fs1) << 15) | (0 << 12) | (uint32(fd) << 7) | 0x53
}

// encodeFMAXS encodes FMAX.S instruction
func encodeFMAXS(fd, fs1, fs2 int) uint32 {
	return (0x14 << 25) | (uint32(fs2) << 20) | (uint32(fs1) << 15) | (1 << 12) | (uint32(fd) << 7) | 0x53
}

// encodeFLES encodes FLE.S instruction (writes to integer register)
func encodeFLES(rd, fs1, fs2 int) uint32 {
	return (0x50 << 25) | (uint32(fs2) << 20) | (uint32(fs1) << 15) | (0 << 12) | (uint32(rd) << 7) | 0x53
}

// encodeFLTS encodes FLT.S instruction
func encodeFLTS(rd, fs1, fs2 int) uint32 {
	return (0x50 << 25) | (uint32(fs2) << 20) | (uint32(fs1) << 15) | (1 << 12) | (uint32(rd) << 7) | 0x53
}

// encodeFEQS encodes FEQ.S instruction
func encodeFEQS(rd, fs1, fs2 int) uint32 {
	return (0x50 << 25) | (uint32(fs2) << 20) | (uint32(fs1) << 15) | (2 << 12) | (uint32(rd) << 7) | 0x53
}

// encodeFCVTWS encodes FCVT.W.S instruction (float to signed int32)
func encodeFCVTWS(rd, fs1, rm int) uint32 {
	return (0x60 << 25) | (0 << 20) | (uint32(fs1) << 15) | (uint32(rm) << 12) | (uint32(rd) << 7) | 0x53
}

// encodeFCVTWUS encodes FCVT.WU.S instruction (float to unsigned int32)
func encodeFCVTWUS(rd, fs1, rm int) uint32 {
	return (0x60 << 25) | (1 << 20) | (uint32(fs1) << 15) | (uint32(rm) << 12) | (uint32(rd) << 7) | 0x53
}

// encodeFCVTLS encodes FCVT.L.S instruction (float to signed int64)
func encodeFCVTLS(rd, fs1, rm int) uint32 {
	return (0x60 << 25) | (2 << 20) | (uint32(fs1) << 15) | (uint32(rm) << 12) | (uint32(rd) << 7) | 0x53
}

// encodeFCVTLUS encodes FCVT.LU.S instruction (float to unsigned int64)
func encodeFCVTLUS(rd, fs1, rm int) uint32 {
	return (0x60 << 25) | (3 << 20) | (uint32(fs1) << 15) | (uint32(rm) << 12) | (uint32(rd) << 7) | 0x53
}

// encodeFCVTSW encodes FCVT.S.W instruction (signed int32 to float)
func encodeFCVTSW(fd, rs1, rm int) uint32 {
	return (0x68 << 25) | (0 << 20) | (uint32(rs1) << 15) | (uint32(rm) << 12) | (uint32(fd) << 7) | 0x53
}

// encodeFCVTSWU encodes FCVT.S.WU instruction (unsigned int32 to float)
func encodeFCVTSWU(fd, rs1, rm int) uint32 {
	return (0x68 << 25) | (1 << 20) | (uint32(rs1) << 15) | (uint32(rm) << 12) | (uint32(fd) << 7) | 0x53
}

// encodeFCVTSL encodes FCVT.S.L instruction (signed int64 to float)
func encodeFCVTSL(fd, rs1, rm int) uint32 {
	return (0x68 << 25) | (2 << 20) | (uint32(rs1) << 15) | (uint32(rm) << 12) | (uint32(fd) << 7) | 0x53
}

// encodeFCVTSLU encodes FCVT.S.LU instruction (unsigned int64 to float)
func encodeFCVTSLU(fd, rs1, rm int) uint32 {
	return (0x68 << 25) | (3 << 20) | (uint32(rs1) << 15) | (uint32(rm) << 12) | (uint32(fd) << 7) | 0x53
}

// encodeFMVXW encodes FMV.X.W instruction (move FP to integer register)
func encodeFMVXW(rd, fs1 int) uint32 {
	return (0x70 << 25) | (0 << 20) | (uint32(fs1) << 15) | (0 << 12) | (uint32(rd) << 7) | 0x53
}

// encodeFCLASSS encodes FCLASS.S instruction
func encodeFCLASSS(rd, fs1 int) uint32 {
	return (0x70 << 25) | (0 << 20) | (uint32(fs1) << 15) | (1 << 12) | (uint32(rd) << 7) | 0x53
}

// encodeFMVWX encodes FMV.W.X instruction (move integer to FP register)
func encodeFMVWX(fd, rs1 int) uint32 {
	return (0x78 << 25) | (0 << 20) | (uint32(rs1) << 15) | (0 << 12) | (uint32(fd) << 7) | 0x53
}

// TestFPLoadStore tests FLW and FSW instructions
func TestFPLoadStore(t *testing.T) {
	cpu := testCPU(t)

	// Test FLW: Load 3.14 from memory
	floatVal := f32bits(3.14)
	addr := uint64(0x80000100)
	cpu.Mem.Write32(addr, floatVal)

	cpu.Reset()
	cpu.FS = FSDirty // Enable FP
	cpu.PC = 0x80000000
	cpu.SetReg(1, addr) // x1 = address

	// FLW f2, 0(x1)
	writeInsn(cpu, cpu.PC, encodeFLW(2, 1, 0))

	err := cpu.Step()
	if err != nil {
		t.Fatalf("FLW Step failed: %v", err)
	}

	// Check FP register - should have NaN-boxed value
	gotF32 := cpu.GetFPRegF32(2)
	if gotF32 != floatVal {
		t.Errorf("FLW: f2 = 0x%08x, want 0x%08x", gotF32, floatVal)
	}

	// Test FSW: Store to different location
	storeAddr := uint64(0x80000200)
	cpu.SetReg(3, storeAddr)

	// FSW f2, 0(x3)
	writeInsn(cpu, cpu.PC, encodeFSW(2, 3, 0))

	err = cpu.Step()
	if err != nil {
		t.Fatalf("FSW Step failed: %v", err)
	}

	storedVal, _ := cpu.Mem.Read32(storeAddr)
	if storedVal != floatVal {
		t.Errorf("FSW: mem[0x%x] = 0x%08x, want 0x%08x", storeAddr, storedVal, floatVal)
	}
}

// TestFPLoadStoreDouble tests FLD and FSD instructions
func TestFPLoadStoreDouble(t *testing.T) {
	cpu := testCPU(t)
	cpu.FS = FSDirty

	// Test FLD: Load 3.14159265359 from memory
	doubleVal := f64bits(3.14159265359)
	addr := uint64(0x80000100)
	cpu.Mem.Write64(addr, doubleVal)

	cpu.Reset()
	cpu.FS = FSDirty
	cpu.PC = 0x80000000
	cpu.SetReg(1, addr)

	// FLD f2, 0(x1)
	writeInsn(cpu, cpu.PC, encodeFLD(2, 1, 0))

	err := cpu.Step()
	if err != nil {
		t.Fatalf("FLD Step failed: %v", err)
	}

	if cpu.FPReg[2] != doubleVal {
		t.Errorf("FLD: f2 = 0x%016x, want 0x%016x", cpu.FPReg[2], doubleVal)
	}

	// Test FSD
	storeAddr := uint64(0x80000200)
	cpu.SetReg(3, storeAddr)

	writeInsn(cpu, cpu.PC, encodeFSD(2, 3, 0))

	err = cpu.Step()
	if err != nil {
		t.Fatalf("FSD Step failed: %v", err)
	}

	storedVal, _ := cpu.Mem.Read64(storeAddr)
	if storedVal != doubleVal {
		t.Errorf("FSD: mem = 0x%016x, want 0x%016x", storedVal, doubleVal)
	}
}

// TestFADDS tests FADD.S instruction
func TestFADDS(t *testing.T) {
	cpu := testCPU(t)
	cpu.FS = FSDirty

	tests := []struct {
		name     string
		a, b     float32
		expected float32
	}{
		{"2.0 + 3.0", 2.0, 3.0, 5.0},
		{"1.5 + 2.5", 1.5, 2.5, 4.0},
		{"-1.0 + 1.0", -1.0, 1.0, 0.0},
		{"0.0 + 0.0", 0.0, 0.0, 0.0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.FS = FSDirty
			cpu.PC = 0x80000000
			cpu.SetFPRegF32(1, f32bits(tc.a))
			cpu.SetFPRegF32(2, f32bits(tc.b))

			// FADD.S f3, f1, f2 (rm=0 RNE)
			writeInsn(cpu, cpu.PC, encodeFADDS(3, 1, 2, 0))

			err := cpu.Step()
			if err != nil {
				t.Fatalf("Step failed: %v", err)
			}

			result := math.Float32frombits(cpu.GetFPRegF32(3))
			if result != tc.expected {
				t.Errorf("FADD.S: got %v, want %v", result, tc.expected)
			}
		})
	}
}

// TestFSUBS tests FSUB.S instruction
func TestFSUBS(t *testing.T) {
	cpu := testCPU(t)
	cpu.FS = FSDirty

	tests := []struct {
		name     string
		a, b     float32
		expected float32
	}{
		{"5.0 - 3.0", 5.0, 3.0, 2.0},
		{"1.0 - 1.0", 1.0, 1.0, 0.0},
		{"-2.0 - 3.0", -2.0, 3.0, -5.0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.FS = FSDirty
			cpu.PC = 0x80000000
			cpu.SetFPRegF32(1, f32bits(tc.a))
			cpu.SetFPRegF32(2, f32bits(tc.b))

			writeInsn(cpu, cpu.PC, encodeFSUBS(3, 1, 2, 0))

			err := cpu.Step()
			if err != nil {
				t.Fatalf("Step failed: %v", err)
			}

			result := math.Float32frombits(cpu.GetFPRegF32(3))
			if result != tc.expected {
				t.Errorf("FSUB.S: got %v, want %v", result, tc.expected)
			}
		})
	}
}

// TestFMULS tests FMUL.S instruction
func TestFMULS(t *testing.T) {
	cpu := testCPU(t)
	cpu.FS = FSDirty

	tests := []struct {
		name     string
		a, b     float32
		expected float32
	}{
		{"2.0 * 3.0", 2.0, 3.0, 6.0},
		{"2.5 * 4.0", 2.5, 4.0, 10.0},
		{"-2.0 * 3.0", -2.0, 3.0, -6.0},
		{"0.0 * 5.0", 0.0, 5.0, 0.0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.FS = FSDirty
			cpu.PC = 0x80000000
			cpu.SetFPRegF32(1, f32bits(tc.a))
			cpu.SetFPRegF32(2, f32bits(tc.b))

			writeInsn(cpu, cpu.PC, encodeFMULS(3, 1, 2, 0))

			err := cpu.Step()
			if err != nil {
				t.Fatalf("Step failed: %v", err)
			}

			result := math.Float32frombits(cpu.GetFPRegF32(3))
			if result != tc.expected {
				t.Errorf("FMUL.S: got %v, want %v", result, tc.expected)
			}
		})
	}
}

// TestFDIVS tests FDIV.S instruction
func TestFDIVS(t *testing.T) {
	cpu := testCPU(t)
	cpu.FS = FSDirty

	tests := []struct {
		name     string
		a, b     float32
		expected float32
	}{
		{"6.0 / 2.0", 6.0, 2.0, 3.0},
		{"10.0 / 4.0", 10.0, 4.0, 2.5},
		{"-6.0 / 2.0", -6.0, 2.0, -3.0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.FS = FSDirty
			cpu.PC = 0x80000000
			cpu.SetFPRegF32(1, f32bits(tc.a))
			cpu.SetFPRegF32(2, f32bits(tc.b))

			writeInsn(cpu, cpu.PC, encodeFDIVS(3, 1, 2, 0))

			err := cpu.Step()
			if err != nil {
				t.Fatalf("Step failed: %v", err)
			}

			result := math.Float32frombits(cpu.GetFPRegF32(3))
			if result != tc.expected {
				t.Errorf("FDIV.S: got %v, want %v", result, tc.expected)
			}
		})
	}
}

// TestFSQRTS tests FSQRT.S instruction
func TestFSQRTS(t *testing.T) {
	cpu := testCPU(t)
	cpu.FS = FSDirty

	tests := []struct {
		name     string
		a        float32
		expected float32
	}{
		{"sqrt(4.0)", 4.0, 2.0},
		{"sqrt(9.0)", 9.0, 3.0},
		{"sqrt(16.0)", 16.0, 4.0},
		{"sqrt(1.0)", 1.0, 1.0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.FS = FSDirty
			cpu.PC = 0x80000000
			cpu.SetFPRegF32(1, f32bits(tc.a))

			writeInsn(cpu, cpu.PC, encodeFSQRTS(3, 1, 0))

			err := cpu.Step()
			if err != nil {
				t.Fatalf("Step failed: %v", err)
			}

			result := math.Float32frombits(cpu.GetFPRegF32(3))
			if result != tc.expected {
				t.Errorf("FSQRT.S: got %v, want %v", result, tc.expected)
			}
		})
	}
}

// TestFMADDS tests FMADD.S instruction: (a * b) + c
func TestFMADDS(t *testing.T) {
	cpu := testCPU(t)
	cpu.FS = FSDirty

	tests := []struct {
		name     string
		a, b, c  float32
		expected float32
	}{
		{"2*3+4", 2.0, 3.0, 4.0, 10.0},
		{"1*1+0", 1.0, 1.0, 0.0, 1.0},
		{"-2*3+10", -2.0, 3.0, 10.0, 4.0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.FS = FSDirty
			cpu.PC = 0x80000000
			cpu.SetFPRegF32(1, f32bits(tc.a))
			cpu.SetFPRegF32(2, f32bits(tc.b))
			cpu.SetFPRegF32(3, f32bits(tc.c))

			writeInsn(cpu, cpu.PC, encodeFMADDS(4, 1, 2, 3, 0))

			err := cpu.Step()
			if err != nil {
				t.Fatalf("Step failed: %v", err)
			}

			result := math.Float32frombits(cpu.GetFPRegF32(4))
			if result != tc.expected {
				t.Errorf("FMADD.S: got %v, want %v", result, tc.expected)
			}
		})
	}
}

// TestFMSUBS tests FMSUB.S instruction: (a * b) - c
func TestFMSUBS(t *testing.T) {
	cpu := testCPU(t)
	cpu.FS = FSDirty

	cpu.Reset()
	cpu.FS = FSDirty
	cpu.PC = 0x80000000
	cpu.SetFPRegF32(1, f32bits(2.0))
	cpu.SetFPRegF32(2, f32bits(3.0))
	cpu.SetFPRegF32(3, f32bits(4.0))

	writeInsn(cpu, cpu.PC, encodeFMSUBS(4, 1, 2, 3, 0))

	err := cpu.Step()
	if err != nil {
		t.Fatalf("Step failed: %v", err)
	}

	result := math.Float32frombits(cpu.GetFPRegF32(4))
	expected := float32(2.0) // 2*3-4 = 2
	if result != expected {
		t.Errorf("FMSUB.S: got %v, want %v", result, expected)
	}
}

// TestFNMSUBS tests FNMSUB.S instruction: -(a * b) + c
func TestFNMSUBS(t *testing.T) {
	cpu := testCPU(t)
	cpu.FS = FSDirty

	cpu.Reset()
	cpu.FS = FSDirty
	cpu.PC = 0x80000000
	cpu.SetFPRegF32(1, f32bits(2.0))
	cpu.SetFPRegF32(2, f32bits(3.0))
	cpu.SetFPRegF32(3, f32bits(10.0))

	writeInsn(cpu, cpu.PC, encodeFNMSUBS(4, 1, 2, 3, 0))

	err := cpu.Step()
	if err != nil {
		t.Fatalf("Step failed: %v", err)
	}

	result := math.Float32frombits(cpu.GetFPRegF32(4))
	expected := float32(4.0) // -(2*3)+10 = 4
	if result != expected {
		t.Errorf("FNMSUB.S: got %v, want %v", result, expected)
	}
}

// TestFNMADDS tests FNMADD.S instruction: -(a * b) - c
func TestFNMADDS(t *testing.T) {
	cpu := testCPU(t)
	cpu.FS = FSDirty

	cpu.Reset()
	cpu.FS = FSDirty
	cpu.PC = 0x80000000
	cpu.SetFPRegF32(1, f32bits(2.0))
	cpu.SetFPRegF32(2, f32bits(3.0))
	cpu.SetFPRegF32(3, f32bits(4.0))

	writeInsn(cpu, cpu.PC, encodeFNMADDS(4, 1, 2, 3, 0))

	err := cpu.Step()
	if err != nil {
		t.Fatalf("Step failed: %v", err)
	}

	result := math.Float32frombits(cpu.GetFPRegF32(4))
	expected := float32(-10.0) // -(2*3)-4 = -10
	if result != expected {
		t.Errorf("FNMADD.S: got %v, want %v", result, expected)
	}
}

// TestFSGNJ tests FSGNJ, FSGNJN, FSGNJX instructions
func TestFSGNJ(t *testing.T) {
	cpu := testCPU(t)
	cpu.FS = FSDirty

	// FSGNJ: copy sign from second operand
	cpu.Reset()
	cpu.FS = FSDirty
	cpu.PC = 0x80000000
	cpu.SetFPRegF32(1, f32bits(3.0))  // positive
	cpu.SetFPRegF32(2, f32bits(-5.0)) // negative

	writeInsn(cpu, cpu.PC, encodeFSGNJS(3, 1, 2))

	err := cpu.Step()
	if err != nil {
		t.Fatalf("FSGNJ Step failed: %v", err)
	}

	result := math.Float32frombits(cpu.GetFPRegF32(3))
	if result != -3.0 {
		t.Errorf("FSGNJ.S: got %v, want -3.0", result)
	}

	// FSGNJN: copy negated sign from second operand
	cpu.Reset()
	cpu.FS = FSDirty
	cpu.PC = 0x80000000
	cpu.SetFPRegF32(1, f32bits(3.0))  // positive
	cpu.SetFPRegF32(2, f32bits(-5.0)) // negative

	writeInsn(cpu, cpu.PC, encodeFSGNJNS(3, 1, 2))

	err = cpu.Step()
	if err != nil {
		t.Fatalf("FSGNJN Step failed: %v", err)
	}

	result = math.Float32frombits(cpu.GetFPRegF32(3))
	if result != 3.0 {
		t.Errorf("FSGNJN.S: got %v, want 3.0", result)
	}

	// FSGNJX: XOR signs
	cpu.Reset()
	cpu.FS = FSDirty
	cpu.PC = 0x80000000
	cpu.SetFPRegF32(1, f32bits(-3.0)) // negative
	cpu.SetFPRegF32(2, f32bits(-5.0)) // negative

	writeInsn(cpu, cpu.PC, encodeFSGNJXS(3, 1, 2))

	err = cpu.Step()
	if err != nil {
		t.Fatalf("FSGNJX Step failed: %v", err)
	}

	result = math.Float32frombits(cpu.GetFPRegF32(3))
	if result != 3.0 { // neg XOR neg = pos
		t.Errorf("FSGNJX.S: got %v, want 3.0", result)
	}
}

// TestFMINMAX tests FMIN.S and FMAX.S instructions
func TestFMINMAX(t *testing.T) {
	cpu := testCPU(t)
	cpu.FS = FSDirty

	// FMIN.S
	cpu.Reset()
	cpu.FS = FSDirty
	cpu.PC = 0x80000000
	cpu.SetFPRegF32(1, f32bits(3.0))
	cpu.SetFPRegF32(2, f32bits(5.0))

	writeInsn(cpu, cpu.PC, encodeFMINS(3, 1, 2))

	err := cpu.Step()
	if err != nil {
		t.Fatalf("FMIN Step failed: %v", err)
	}

	result := math.Float32frombits(cpu.GetFPRegF32(3))
	if result != 3.0 {
		t.Errorf("FMIN.S: got %v, want 3.0", result)
	}

	// FMAX.S
	cpu.Reset()
	cpu.FS = FSDirty
	cpu.PC = 0x80000000
	cpu.SetFPRegF32(1, f32bits(3.0))
	cpu.SetFPRegF32(2, f32bits(5.0))

	writeInsn(cpu, cpu.PC, encodeFMAXS(3, 1, 2))

	err = cpu.Step()
	if err != nil {
		t.Fatalf("FMAX Step failed: %v", err)
	}

	result = math.Float32frombits(cpu.GetFPRegF32(3))
	if result != 5.0 {
		t.Errorf("FMAX.S: got %v, want 5.0", result)
	}
}

// TestFComparisons tests FLE.S, FLT.S, FEQ.S instructions
func TestFComparisons(t *testing.T) {
	cpu := testCPU(t)
	cpu.FS = FSDirty

	tests := []struct {
		name     string
		a, b     float32
		encoder  func(rd, fs1, fs2 int) uint32
		expected uint64
	}{
		{"FLE 3<=5", 3.0, 5.0, encodeFLES, 1},
		{"FLE 5<=3", 5.0, 3.0, encodeFLES, 0},
		{"FLE 3<=3", 3.0, 3.0, encodeFLES, 1},
		{"FLT 3<5", 3.0, 5.0, encodeFLTS, 1},
		{"FLT 5<3", 5.0, 3.0, encodeFLTS, 0},
		{"FLT 3<3", 3.0, 3.0, encodeFLTS, 0},
		{"FEQ 3==3", 3.0, 3.0, encodeFEQS, 1},
		{"FEQ 3==5", 3.0, 5.0, encodeFEQS, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.FS = FSDirty
			cpu.PC = 0x80000000
			cpu.SetFPRegF32(1, f32bits(tc.a))
			cpu.SetFPRegF32(2, f32bits(tc.b))

			writeInsn(cpu, cpu.PC, tc.encoder(3, 1, 2))

			err := cpu.Step()
			if err != nil {
				t.Fatalf("Step failed: %v", err)
			}

			if cpu.GetReg(3) != tc.expected {
				t.Errorf("got %d, want %d", cpu.GetReg(3), tc.expected)
			}
		})
	}
}

// TestFCVTFloatToInt tests FCVT.W.S, FCVT.WU.S, FCVT.L.S, FCVT.LU.S
func TestFCVTFloatToInt(t *testing.T) {
	cpu := testCPU(t)
	cpu.FS = FSDirty

	tests := []struct {
		name     string
		a        float32
		encoder  func(rd, fs1, rm int) uint32
		expected uint64
	}{
		{"FCVT.W.S 3.7", 3.7, encodeFCVTWS, 4},                    // RNE rounds to 4
		{"FCVT.W.S -3.7", -3.7, encodeFCVTWS, 0xFFFFFFFFFFFFFFFC}, // -4 sign extended
		{"FCVT.WU.S 3.7", 3.7, encodeFCVTWUS, 4},
		{"FCVT.L.S 3.7", 3.7, encodeFCVTLS, 4},
		{"FCVT.LU.S 3.7", 3.7, encodeFCVTLUS, 4},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.FS = FSDirty
			cpu.PC = 0x80000000
			cpu.SetFPRegF32(1, f32bits(tc.a))

			writeInsn(cpu, cpu.PC, tc.encoder(3, 1, 0))

			err := cpu.Step()
			if err != nil {
				t.Fatalf("Step failed: %v", err)
			}

			if cpu.GetReg(3) != tc.expected {
				t.Errorf("got 0x%x, want 0x%x", cpu.GetReg(3), tc.expected)
			}
		})
	}
}

// TestFCVTIntToFloat tests FCVT.S.W, FCVT.S.WU, FCVT.S.L, FCVT.S.LU
func TestFCVTIntToFloat(t *testing.T) {
	cpu := testCPU(t)
	cpu.FS = FSDirty

	tests := []struct {
		name     string
		a        uint64
		encoder  func(fd, rs1, rm int) uint32
		expected float32
	}{
		{"FCVT.S.W 42", 42, encodeFCVTSW, 42.0},
		{"FCVT.S.W -42", 0xFFFFFFFFFFFFFFD6, encodeFCVTSW, -42.0}, // -42 as 64-bit
		{"FCVT.S.WU 42", 42, encodeFCVTSWU, 42.0},
		{"FCVT.S.L 42", 42, encodeFCVTSL, 42.0},
		{"FCVT.S.LU 42", 42, encodeFCVTSLU, 42.0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.FS = FSDirty
			cpu.PC = 0x80000000
			cpu.SetReg(1, tc.a)

			writeInsn(cpu, cpu.PC, tc.encoder(3, 1, 0))

			err := cpu.Step()
			if err != nil {
				t.Fatalf("Step failed: %v", err)
			}

			result := math.Float32frombits(cpu.GetFPRegF32(3))
			if result != tc.expected {
				t.Errorf("got %v, want %v", result, tc.expected)
			}
		})
	}
}

// TestFMVXW tests FMV.X.W instruction
func TestFMVXW(t *testing.T) {
	cpu := testCPU(t)
	cpu.FS = FSDirty

	cpu.Reset()
	cpu.FS = FSDirty
	cpu.PC = 0x80000000
	cpu.SetFPRegF32(1, f32bits(-3.14))

	writeInsn(cpu, cpu.PC, encodeFMVXW(3, 1))

	err := cpu.Step()
	if err != nil {
		t.Fatalf("Step failed: %v", err)
	}

	// Result should be sign-extended 32-bit value
	expected := uint64(int64(int32(f32bits(-3.14))))
	if cpu.GetReg(3) != expected {
		t.Errorf("FMV.X.W: got 0x%x, want 0x%x", cpu.GetReg(3), expected)
	}
}

// TestFMVWX tests FMV.W.X instruction
func TestFMVWX(t *testing.T) {
	cpu := testCPU(t)
	cpu.FS = FSDirty

	cpu.Reset()
	cpu.FS = FSDirty
	cpu.PC = 0x80000000
	floatBits := f32bits(3.14)
	cpu.SetReg(1, uint64(floatBits))

	writeInsn(cpu, cpu.PC, encodeFMVWX(3, 1))

	err := cpu.Step()
	if err != nil {
		t.Fatalf("Step failed: %v", err)
	}

	if cpu.GetFPRegF32(3) != floatBits {
		t.Errorf("FMV.W.X: got 0x%x, want 0x%x", cpu.GetFPRegF32(3), floatBits)
	}
}

// TestFCLASS tests FCLASS.S instruction
func TestFCLASS(t *testing.T) {
	cpu := testCPU(t)
	cpu.FS = FSDirty

	tests := []struct {
		name     string
		bits     uint32
		expected uint64
	}{
		{"positive zero", 0x00000000, 0x10},      // bit 4
		{"negative zero", 0x80000000, 0x08},      // bit 3
		{"positive normal", f32bits(1.0), 0x40},  // bit 6
		{"negative normal", f32bits(-1.0), 0x02}, // bit 1
		{"positive infinity", 0x7F800000, 0x80},  // bit 7
		{"negative infinity", 0xFF800000, 0x01},  // bit 0
		{"quiet NaN", 0x7FC00000, 0x200},         // bit 9
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.FS = FSDirty
			cpu.PC = 0x80000000
			cpu.SetFPRegF32(1, tc.bits)

			writeInsn(cpu, cpu.PC, encodeFCLASSS(3, 1))

			err := cpu.Step()
			if err != nil {
				t.Fatalf("Step failed: %v", err)
			}

			if cpu.GetReg(3) != tc.expected {
				t.Errorf("FCLASS.S: got 0x%x, want 0x%x", cpu.GetReg(3), tc.expected)
			}
		})
	}
}

// TestFPDisabled tests that FP instructions cause exception when FS=0
func TestFPDisabled(t *testing.T) {
	cpu := testCPU(t)
	// Don't enable FP (FS = 0)

	cpu.Reset()
	cpu.PC = 0x80000000
	cpu.Mstatus &^= MstatusFS // Ensure FP is disabled

	writeInsn(cpu, cpu.PC, encodeFADDS(3, 1, 2, 0))

	err := cpu.Step()
	// Should not error but should set up illegal instruction exception
	_ = err

	// Check that mcause indicates illegal instruction
	if cpu.Mcause != CauseIllegalInsn {
		t.Errorf("expected illegal instruction exception, got mcause=%d", cpu.Mcause)
	}
}

// TestFPLoadDisabled tests that FP load fails when FS=0
func TestFPLoadDisabled(t *testing.T) {
	cpu := testCPU(t)
	cpu.Reset()
	cpu.PC = 0x80000000
	cpu.FS = FSOff
	cpu.Mstatus &^= MstatusFS // Ensure FP is disabled
	cpu.SetReg(1, 0x80000100)

	writeInsn(cpu, cpu.PC, encodeFLW(2, 1, 0))
	cpu.Step()

	if cpu.Mcause != CauseIllegalInsn {
		t.Errorf("expected illegal instruction, got mcause=%d", cpu.Mcause)
	}
}

// TestFPStoreDisabled tests that FP store fails when FS=0
func TestFPStoreDisabled(t *testing.T) {
	cpu := testCPU(t)
	cpu.Reset()
	cpu.PC = 0x80000000
	cpu.FS = FSOff
	cpu.Mstatus &^= MstatusFS
	cpu.SetReg(1, 0x80000100)

	writeInsn(cpu, cpu.PC, encodeFSW(2, 1, 0))
	cpu.Step()

	if cpu.Mcause != CauseIllegalInsn {
		t.Errorf("expected illegal instruction, got mcause=%d", cpu.Mcause)
	}
}

// TestFPInvalidFunct3Load tests invalid funct3 for FP load
func TestFPInvalidFunct3Load(t *testing.T) {
	cpu := testCPU(t)
	cpu.Reset()
	cpu.FS = FSDirty
	cpu.PC = 0x80000000
	cpu.SetReg(1, 0x80000100)

	// Invalid funct3=0 for FP load
	insn := (uint32(0) << 20) | (1 << 15) | (0 << 12) | (2 << 7) | 0x07
	writeInsn(cpu, cpu.PC, insn)
	cpu.Step()

	if cpu.Mcause != CauseIllegalInsn {
		t.Errorf("expected illegal instruction for invalid FP load funct3, got mcause=%d", cpu.Mcause)
	}
}

// TestFPInvalidFunct3Store tests invalid funct3 for FP store
func TestFPInvalidFunct3Store(t *testing.T) {
	cpu := testCPU(t)
	cpu.Reset()
	cpu.FS = FSDirty
	cpu.PC = 0x80000000
	cpu.SetReg(1, 0x80000100)

	// Invalid funct3=0 for FP store
	insn := uint32((2 << 20) | (1 << 15) | (0 << 12) | 0x27)
	writeInsn(cpu, cpu.PC, insn)
	cpu.Step()

	if cpu.Mcause != CauseIllegalInsn {
		t.Errorf("expected illegal instruction for invalid FP store funct3, got mcause=%d", cpu.Mcause)
	}
}

// TestRoundingModes tests different rounding modes
func TestRoundingModes(t *testing.T) {
	cpu := testCPU(t)

	tests := []struct {
		name     string
		rm       int
		a        float32
		expected float32
	}{
		// Use 2.5 which rounds differently depending on mode
		{"RNE 2.5", RoundNearestEven, 2.5, 2.0}, // Round to nearest even = 2
		{"RTZ 2.7", RoundToZero, 2.7, 2.0},      // Round toward zero = 2
		{"RDN 2.7", RoundDown, 2.7, 2.0},        // Round down = 2
		{"RUP 2.1", RoundUp, 2.1, 3.0},          // Round up = 3
		{"RMM 2.5", RoundNearestMax, 2.5, 3.0},  // Round to nearest, ties to max = 3
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.FS = FSDirty
			cpu.PC = 0x80000000
			cpu.SetFPRegF32(1, f32bits(tc.a))

			// FCVT.W.S with specific rounding mode
			writeInsn(cpu, cpu.PC, encodeFCVTWS(3, 1, tc.rm))

			err := cpu.Step()
			if err != nil {
				t.Fatalf("Step failed: %v", err)
			}

			result := int32(cpu.GetReg(3))
			expected := int32(tc.expected)
			if result != expected {
				t.Errorf("got %d, want %d", result, expected)
			}
		})
	}
}

// TestDynamicRoundingMode tests using dynamic rounding mode from FRM
func TestDynamicRoundingMode(t *testing.T) {
	cpu := testCPU(t)
	cpu.Reset()
	cpu.FS = FSDirty
	cpu.PC = 0x80000000
	cpu.FRM = RoundToZero // Set FRM to round toward zero
	cpu.SetFPRegF32(1, f32bits(2.9))

	// FCVT.W.S with rm=7 (dynamic, use FRM)
	writeInsn(cpu, cpu.PC, encodeFCVTWS(3, 1, 7))

	err := cpu.Step()
	if err != nil {
		t.Fatalf("Step failed: %v", err)
	}

	// With RTZ, 2.9 should become 2
	result := int32(cpu.GetReg(3))
	if result != 2 {
		t.Errorf("got %d, want 2 (RTZ)", result)
	}
}

// TestInvalidRoundingMode tests that invalid rounding mode causes exception
func TestInvalidRoundingMode(t *testing.T) {
	cpu := testCPU(t)
	cpu.Reset()
	cpu.FS = FSDirty
	cpu.PC = 0x80000000
	cpu.FRM = 5 // Invalid rounding mode
	cpu.SetFPRegF32(1, f32bits(2.9))

	// FCVT.W.S with rm=7 (dynamic, use invalid FRM)
	writeInsn(cpu, cpu.PC, encodeFCVTWS(3, 1, 7))

	cpu.Step()

	if cpu.Mcause != CauseIllegalInsn {
		t.Errorf("expected illegal instruction for invalid rounding mode, got mcause=%d", cpu.Mcause)
	}
}

// encodeFADDD encodes FADD.D instruction: FADD.D fd, fs1, fs2
// Format: 0000001 | rs2 | rs1 | rm | rd | 1010011
func encodeFADDD(fd, fs1, fs2, rm int) uint32 {
	return (0x01 << 25) | (uint32(fs2) << 20) | (uint32(fs1) << 15) | (uint32(rm) << 12) | (uint32(fd) << 7) | 0x53
}

// encodeFSUBD encodes FSUB.D instruction
func encodeFSUBD(fd, fs1, fs2, rm int) uint32 {
	return (0x05 << 25) | (uint32(fs2) << 20) | (uint32(fs1) << 15) | (uint32(rm) << 12) | (uint32(fd) << 7) | 0x53
}

// encodeFMULD encodes FMUL.D instruction
func encodeFMULD(fd, fs1, fs2, rm int) uint32 {
	return (0x09 << 25) | (uint32(fs2) << 20) | (uint32(fs1) << 15) | (uint32(rm) << 12) | (uint32(fd) << 7) | 0x53
}

// encodeFDIVD encodes FDIV.D instruction
func encodeFDIVD(fd, fs1, fs2, rm int) uint32 {
	return (0x0D << 25) | (uint32(fs2) << 20) | (uint32(fs1) << 15) | (uint32(rm) << 12) | (uint32(fd) << 7) | 0x53
}

// encodeFSQRTD encodes FSQRT.D instruction
func encodeFSQRTD(fd, fs1, rm int) uint32 {
	return (0x2D << 25) | (uint32(fs1) << 15) | (uint32(rm) << 12) | (uint32(fd) << 7) | 0x53
}

// TestFADDD tests FADD.D instruction
func TestFADDD(t *testing.T) {
	cpu := testCPU(t)
	cpu.FS = FSDirty

	tests := []struct {
		name     string
		a, b     float64
		expected float64
	}{
		{"2.0 + 3.0", 2.0, 3.0, 5.0},
		{"1.5 + 2.5", 1.5, 2.5, 4.0},
		{"-1.0 + 1.0", -1.0, 1.0, 0.0},
		{"0.0 + 0.0", 0.0, 0.0, 0.0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.FS = FSDirty
			cpu.PC = 0x80000000
			cpu.FPReg[1] = f64bits(tc.a)
			cpu.FPReg[2] = f64bits(tc.b)

			writeInsn(cpu, cpu.PC, encodeFADDD(3, 1, 2, 0))

			err := cpu.Step()
			if err != nil {
				t.Fatalf("Step failed: %v", err)
			}

			result := math.Float64frombits(cpu.FPReg[3])
			if result != tc.expected {
				t.Errorf("FADD.D: got %v, want %v", result, tc.expected)
			}
		})
	}
}

// TestFSUBD tests FSUB.D instruction
func TestFSUBD(t *testing.T) {
	cpu := testCPU(t)
	cpu.FS = FSDirty

	tests := []struct {
		name     string
		a, b     float64
		expected float64
	}{
		{"5.0 - 3.0", 5.0, 3.0, 2.0},
		{"1.5 - 2.5", 1.5, 2.5, -1.0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.FS = FSDirty
			cpu.PC = 0x80000000
			cpu.FPReg[1] = f64bits(tc.a)
			cpu.FPReg[2] = f64bits(tc.b)

			writeInsn(cpu, cpu.PC, encodeFSUBD(3, 1, 2, 0))

			err := cpu.Step()
			if err != nil {
				t.Fatalf("Step failed: %v", err)
			}

			result := math.Float64frombits(cpu.FPReg[3])
			if result != tc.expected {
				t.Errorf("FSUB.D: got %v, want %v", result, tc.expected)
			}
		})
	}
}

// TestFMULD tests FMUL.D instruction
func TestFMULD(t *testing.T) {
	cpu := testCPU(t)
	cpu.FS = FSDirty

	tests := []struct {
		name     string
		a, b     float64
		expected float64
	}{
		{"2.0 * 3.0", 2.0, 3.0, 6.0},
		{"-2.0 * 3.0", -2.0, 3.0, -6.0},
		{"0.5 * 4.0", 0.5, 4.0, 2.0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.FS = FSDirty
			cpu.PC = 0x80000000
			cpu.FPReg[1] = f64bits(tc.a)
			cpu.FPReg[2] = f64bits(tc.b)

			writeInsn(cpu, cpu.PC, encodeFMULD(3, 1, 2, 0))

			err := cpu.Step()
			if err != nil {
				t.Fatalf("Step failed: %v", err)
			}

			result := math.Float64frombits(cpu.FPReg[3])
			if result != tc.expected {
				t.Errorf("FMUL.D: got %v, want %v", result, tc.expected)
			}
		})
	}
}

// TestFDIVD tests FDIV.D instruction
func TestFDIVD(t *testing.T) {
	cpu := testCPU(t)
	cpu.FS = FSDirty

	tests := []struct {
		name     string
		a, b     float64
		expected float64
	}{
		{"6.0 / 2.0", 6.0, 2.0, 3.0},
		{"10.0 / 4.0", 10.0, 4.0, 2.5},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.FS = FSDirty
			cpu.PC = 0x80000000
			cpu.FPReg[1] = f64bits(tc.a)
			cpu.FPReg[2] = f64bits(tc.b)

			writeInsn(cpu, cpu.PC, encodeFDIVD(3, 1, 2, 0))

			err := cpu.Step()
			if err != nil {
				t.Fatalf("Step failed: %v", err)
			}

			result := math.Float64frombits(cpu.FPReg[3])
			if result != tc.expected {
				t.Errorf("FDIV.D: got %v, want %v", result, tc.expected)
			}
		})
	}
}

// TestFSQRTD tests FSQRT.D instruction
func TestFSQRTD(t *testing.T) {
	cpu := testCPU(t)
	cpu.FS = FSDirty

	tests := []struct {
		name     string
		a        float64
		expected float64
	}{
		{"sqrt(4.0)", 4.0, 2.0},
		{"sqrt(9.0)", 9.0, 3.0},
		{"sqrt(2.0)", 2.0, math.Sqrt(2.0)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.FS = FSDirty
			cpu.PC = 0x80000000
			cpu.FPReg[1] = f64bits(tc.a)

			writeInsn(cpu, cpu.PC, encodeFSQRTD(3, 1, 0))

			err := cpu.Step()
			if err != nil {
				t.Fatalf("Step failed: %v", err)
			}

			result := math.Float64frombits(cpu.FPReg[3])
			// Use approximate comparison for sqrt
			if math.Abs(result-tc.expected) > 1e-10 {
				t.Errorf("FSQRT.D: got %v, want %v", result, tc.expected)
			}
		})
	}
}

// TestInvalidFPFormat tests that invalid FP format causes illegal instruction
func TestInvalidFPFormat(t *testing.T) {
	cpu := testCPU(t)
	cpu.Reset()
	cpu.FS = FSDirty
	cpu.PC = 0x80000000
	cpu.Mtvec = 0x80010000

	// FADD with format=2 (quad) is not supported
	// Format: funct7[31:25] | rs2[24:20] | rs1[19:15] | rm[14:12] | rd[11:7] | opcode[6:0]
	// FADD.Q: funct7 = 0000010 (format bits = 10 for quad)
	insn := uint32(0x02<<25) | (2 << 20) | (1 << 15) | (0 << 12) | (3 << 7) | 0x53
	writeInsn(cpu, cpu.PC, insn)

	cpu.Step()

	if cpu.Mcause != CauseIllegalInsn {
		t.Errorf("expected illegal instruction for quad format, got mcause=%d", cpu.Mcause)
	}
}
