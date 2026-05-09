package riscv

import (
	"testing"

	"lukechampine.com/uint128"

	"github.com/jtolio/tinyemu-go/mem"
)

// testCPU creates a CPU with RAM for testing
func testCPU(t *testing.T) *CPU {
	t.Helper()
	m := mem.NewPhysMemoryMap()
	// Allocate 1MB of RAM at 0x80000000 (standard RISC-V RAM base)
	_, err := m.RegisterRAM(0x80000000, 1024*1024, 0)
	if err != nil {
		t.Fatalf("failed to register RAM: %v", err)
	}
	cpu := NewCPU(m, XLEN64)
	cpu.PC = 0x80000000
	return cpu
}

// writeInsn writes a 32-bit instruction to memory at the given address
func writeInsn(cpu *CPU, addr uint64, insn uint32) {
	cpu.Mem.Write32(addr, insn)
}

// TestLUI tests LUI instruction
func TestLUI(t *testing.T) {
	cpu := testCPU(t)

	tests := []struct {
		name     string
		insn     uint32
		rd       int
		expected uint64
	}{
		{"LUI x1, 0x12345", 0x123450B7, 1, 0x12345000},
		{"LUI x1, 0x80000", 0x800000B7, 1, 0xFFFFFFFF80000000}, // Sign extended
		{"LUI x0, 0x12345", 0x12345037, 0, 0},                  // x0 stays 0
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.PC = 0x80000000
			writeInsn(cpu, cpu.PC, tc.insn)

			err := cpu.Step()
			if err != nil {
				t.Fatalf("Step failed: %v", err)
			}

			if cpu.GetReg(tc.rd) != tc.expected {
				t.Errorf("x%d = 0x%x, want 0x%x", tc.rd, cpu.GetReg(tc.rd), tc.expected)
			}
			if cpu.PC != 0x80000004 {
				t.Errorf("PC = 0x%x, want 0x80000004", cpu.PC)
			}
		})
	}
}

// TestAUIPC tests AUIPC instruction
func TestAUIPC(t *testing.T) {
	cpu := testCPU(t)

	tests := []struct {
		name     string
		insn     uint32
		pc       uint64
		rd       int
		expected uint64
	}{
		{"AUIPC x1, 0", 0x00000097, 0x80000000, 1, 0x80000000},
		{"AUIPC x1, 1", 0x00001097, 0x80000000, 1, 0x80001000},
		{"AUIPC x1, 0x12345", 0x12345097, 0x80000000, 1, 0x92345000},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.PC = tc.pc
			writeInsn(cpu, cpu.PC, tc.insn)

			err := cpu.Step()
			if err != nil {
				t.Fatalf("Step failed: %v", err)
			}

			if cpu.GetReg(tc.rd) != tc.expected {
				t.Errorf("x%d = 0x%x, want 0x%x", tc.rd, cpu.GetReg(tc.rd), tc.expected)
			}
		})
	}
}

// TestADDI tests ADDI instruction
func TestADDI(t *testing.T) {
	cpu := testCPU(t)

	tests := []struct {
		name     string
		insn     uint32
		rs1Val   uint64
		rd       int
		expected uint64
	}{
		{"ADDI x1, x0, 42", 0x02A00093, 0, 1, 42},
		{"ADDI x1, x0, -1", 0xFFF00093, 0, 1, 0xFFFFFFFFFFFFFFFF},
		{"ADDI x1, x2, 10", 0x00A10093, 100, 1, 110}, // x2=100, +10
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.PC = 0x80000000
			cpu.SetReg(2, tc.rs1Val)
			writeInsn(cpu, cpu.PC, tc.insn)

			err := cpu.Step()
			if err != nil {
				t.Fatalf("Step failed: %v", err)
			}

			if cpu.GetReg(tc.rd) != tc.expected {
				t.Errorf("x%d = 0x%x, want 0x%x", tc.rd, cpu.GetReg(tc.rd), tc.expected)
			}
		})
	}
}

// TestADD tests ADD instruction
func TestADD(t *testing.T) {
	cpu := testCPU(t)

	// ADD x1, x2, x3 -> 0x003100B3
	cpu.Reset()
	cpu.PC = 0x80000000
	cpu.SetReg(2, 100)
	cpu.SetReg(3, 50)
	writeInsn(cpu, cpu.PC, 0x003100B3)

	err := cpu.Step()
	if err != nil {
		t.Fatalf("Step failed: %v", err)
	}

	if cpu.GetReg(1) != 150 {
		t.Errorf("x1 = %d, want 150", cpu.GetReg(1))
	}
}

// TestSUB tests SUB instruction
func TestSUB(t *testing.T) {
	cpu := testCPU(t)

	// SUB x1, x2, x3 -> 0x403100B3
	cpu.Reset()
	cpu.PC = 0x80000000
	cpu.SetReg(2, 100)
	cpu.SetReg(3, 30)
	writeInsn(cpu, cpu.PC, 0x403100B3)

	err := cpu.Step()
	if err != nil {
		t.Fatalf("Step failed: %v", err)
	}

	if cpu.GetReg(1) != 70 {
		t.Errorf("x1 = %d, want 70", cpu.GetReg(1))
	}
}

// TestLogical tests AND, OR, XOR instructions
func TestLogical(t *testing.T) {
	cpu := testCPU(t)

	tests := []struct {
		name     string
		insn     uint32
		rs1Val   uint64
		rs2Val   uint64
		expected uint64
	}{
		// AND x1, x2, x3 -> 0x003170B3
		{"AND", 0x003170B3, 0xFF00FF00, 0x0F0F0F0F, 0x0F000F00},
		// OR x1, x2, x3 -> 0x003160B3
		{"OR", 0x003160B3, 0xFF00FF00, 0x0F0F0F0F, 0xFF0FFF0F},
		// XOR x1, x2, x3 -> 0x003140B3
		{"XOR", 0x003140B3, 0xFF00FF00, 0x0F0F0F0F, 0xF00FF00F},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.PC = 0x80000000
			cpu.SetReg(2, tc.rs1Val)
			cpu.SetReg(3, tc.rs2Val)
			writeInsn(cpu, cpu.PC, tc.insn)

			err := cpu.Step()
			if err != nil {
				t.Fatalf("Step failed: %v", err)
			}

			if cpu.GetReg(1) != tc.expected {
				t.Errorf("x1 = 0x%x, want 0x%x", cpu.GetReg(1), tc.expected)
			}
		})
	}
}

// TestShifts tests SLL, SRL, SRA instructions
func TestShifts(t *testing.T) {
	cpu := testCPU(t)

	tests := []struct {
		name     string
		insn     uint32
		rs1Val   uint64
		rs2Val   uint64
		expected uint64
	}{
		// SLL x1, x2, x3 -> 0x003110B3
		{"SLL by 4", 0x003110B3, 0x1, 4, 0x10},
		// SRL x1, x2, x3 -> 0x003150B3
		{"SRL by 4", 0x003150B3, 0x100, 4, 0x10},
		// SRA x1, x2, x3 -> 0x403150B3
		{"SRA positive", 0x403150B3, 0x100, 4, 0x10},
		{"SRA negative", 0x403150B3, 0xFFFFFFFFFFFFFF00, 4, 0xFFFFFFFFFFFFFFF0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.PC = 0x80000000
			cpu.SetReg(2, tc.rs1Val)
			cpu.SetReg(3, tc.rs2Val)
			writeInsn(cpu, cpu.PC, tc.insn)

			err := cpu.Step()
			if err != nil {
				t.Fatalf("Step failed: %v", err)
			}

			if cpu.GetReg(1) != tc.expected {
				t.Errorf("x1 = 0x%x, want 0x%x", cpu.GetReg(1), tc.expected)
			}
		})
	}
}

// TestSLT tests SLT and SLTU instructions
func TestSLT(t *testing.T) {
	cpu := testCPU(t)

	tests := []struct {
		name     string
		insn     uint32
		rs1Val   uint64
		rs2Val   uint64
		expected uint64
	}{
		// SLT x1, x2, x3 -> 0x003120B3
		{"SLT less", 0x003120B3, 5, 10, 1},
		{"SLT equal", 0x003120B3, 10, 10, 0},
		{"SLT greater", 0x003120B3, 15, 10, 0},
		{"SLT negative", 0x003120B3, 0xFFFFFFFFFFFFFFFF, 1, 1}, // -1 < 1

		// SLTU x1, x2, x3 -> 0x003130B3
		{"SLTU less", 0x003130B3, 5, 10, 1},
		{"SLTU unsigned", 0x003130B3, 0xFFFFFFFFFFFFFFFF, 1, 0}, // Max uint > 1
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.PC = 0x80000000
			cpu.SetReg(2, tc.rs1Val)
			cpu.SetReg(3, tc.rs2Val)
			writeInsn(cpu, cpu.PC, tc.insn)

			err := cpu.Step()
			if err != nil {
				t.Fatalf("Step failed: %v", err)
			}

			if cpu.GetReg(1) != tc.expected {
				t.Errorf("x1 = %d, want %d", cpu.GetReg(1), tc.expected)
			}
		})
	}
}

// TestJAL tests JAL instruction
func TestJAL(t *testing.T) {
	cpu := testCPU(t)

	// JAL x1, 8 -> jump forward 8 bytes
	// Encoding: imm[20|10:1|11|19:12] rd opcode
	// For offset 8: imm = 8, so imm[4:1] = 4
	cpu.Reset()
	cpu.PC = 0x80000000
	writeInsn(cpu, cpu.PC, 0x008000EF) // JAL x1, 8

	err := cpu.Step()
	if err != nil {
		t.Fatalf("Step failed: %v", err)
	}

	// x1 should have return address (PC + 4)
	if cpu.GetReg(1) != 0x80000004 {
		t.Errorf("x1 = 0x%x, want 0x80000004", cpu.GetReg(1))
	}
	// PC should be PC + 8
	if cpu.PC != 0x80000008 {
		t.Errorf("PC = 0x%x, want 0x80000008", cpu.PC)
	}
}

// TestJALR tests JALR instruction
func TestJALR(t *testing.T) {
	cpu := testCPU(t)

	// JALR x1, x2, 0 -> 0x000100E7
	cpu.Reset()
	cpu.PC = 0x80000000
	cpu.SetReg(2, 0x80001000)
	writeInsn(cpu, cpu.PC, 0x000100E7)

	err := cpu.Step()
	if err != nil {
		t.Fatalf("Step failed: %v", err)
	}

	if cpu.GetReg(1) != 0x80000004 {
		t.Errorf("x1 = 0x%x, want 0x80000004", cpu.GetReg(1))
	}
	if cpu.PC != 0x80001000 {
		t.Errorf("PC = 0x%x, want 0x80001000", cpu.PC)
	}
}

// TestBranches tests BEQ, BNE, BLT, BGE, BLTU, BGEU
func TestBranches(t *testing.T) {
	cpu := testCPU(t)

	tests := []struct {
		name       string
		insn       uint32
		rs1Val     uint64
		rs2Val     uint64
		shouldJump bool
	}{
		// BEQ x1, x2, 8 -> 0x00208463
		{"BEQ taken", 0x00208463, 10, 10, true},
		{"BEQ not taken", 0x00208463, 10, 20, false},

		// BNE x1, x2, 8 -> 0x00209463
		{"BNE taken", 0x00209463, 10, 20, true},
		{"BNE not taken", 0x00209463, 10, 10, false},

		// BLT x1, x2, 8 -> 0x0020C463
		{"BLT taken", 0x0020C463, 5, 10, true},
		{"BLT not taken", 0x0020C463, 10, 5, false},

		// BGE x1, x2, 8 -> 0x0020D463
		{"BGE taken", 0x0020D463, 10, 5, true},
		{"BGE equal", 0x0020D463, 10, 10, true},
		{"BGE not taken", 0x0020D463, 5, 10, false},

		// BLTU x1, x2, 8 -> 0x0020E463
		{"BLTU taken", 0x0020E463, 5, 10, true},
		{"BLTU unsigned", 0x0020E463, 0xFFFFFFFFFFFFFFFF, 1, false}, // -1 > 1 unsigned

		// BGEU x1, x2, 8 -> 0x0020F463
		{"BGEU taken", 0x0020F463, 10, 5, true},
		{"BGEU unsigned", 0x0020F463, 0xFFFFFFFFFFFFFFFF, 1, true}, // -1 > 1 unsigned
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.PC = 0x80000000
			cpu.SetReg(1, tc.rs1Val)
			cpu.SetReg(2, tc.rs2Val)
			writeInsn(cpu, cpu.PC, tc.insn)

			err := cpu.Step()
			if err != nil {
				t.Fatalf("Step failed: %v", err)
			}

			expectedPC := uint64(0x80000004)
			if tc.shouldJump {
				expectedPC = 0x80000008
			}

			if cpu.PC != expectedPC {
				t.Errorf("PC = 0x%x, want 0x%x", cpu.PC, expectedPC)
			}
		})
	}
}

// TestLoadStore tests LW, SW, LD, SD
func TestLoadStore(t *testing.T) {
	cpu := testCPU(t)

	// Test SW then LW
	t.Run("SW/LW", func(t *testing.T) {
		cpu.Reset()
		cpu.PC = 0x80000000
		cpu.SetReg(1, 0x12345678)
		cpu.SetReg(2, 0x80000100) // Base address

		// SW x1, 0(x2) -> 0x00112023
		writeInsn(cpu, cpu.PC, 0x00112023)
		cpu.Step()

		// LW x3, 0(x2) -> 0x00012183
		writeInsn(cpu, cpu.PC, 0x00012183)
		cpu.Step()

		// x3 should have the stored value (sign-extended)
		expected := uint64(0x12345678)
		if cpu.GetReg(3) != expected {
			t.Errorf("x3 = 0x%x, want 0x%x", cpu.GetReg(3), expected)
		}
	})

	// Test SD then LD
	t.Run("SD/LD", func(t *testing.T) {
		cpu.Reset()
		cpu.PC = 0x80000000
		cpu.SetReg(1, 0x123456789ABCDEF0)
		cpu.SetReg(2, 0x80000100)

		// SD x1, 0(x2) -> 0x00113023
		writeInsn(cpu, cpu.PC, 0x00113023)
		cpu.Step()

		// LD x3, 0(x2) -> 0x00013183
		writeInsn(cpu, cpu.PC, 0x00013183)
		cpu.Step()

		if cpu.GetReg(3) != 0x123456789ABCDEF0 {
			t.Errorf("x3 = 0x%x, want 0x123456789ABCDEF0", cpu.GetReg(3))
		}
	})

	// Test LB (sign extension)
	t.Run("SB/LB sign extend", func(t *testing.T) {
		cpu.Reset()
		cpu.PC = 0x80000000
		cpu.SetReg(1, 0x80) // -128 as signed byte
		cpu.SetReg(2, 0x80000100)

		// SB x1, 0(x2) -> 0x00110023
		writeInsn(cpu, cpu.PC, 0x00110023)
		cpu.Step()

		// LB x3, 0(x2) -> 0x00010183
		writeInsn(cpu, cpu.PC, 0x00010183)
		cpu.Step()

		// Should be sign-extended to -128
		expected := uint64(0xFFFFFFFFFFFFFF80)
		if cpu.GetReg(3) != expected {
			t.Errorf("x3 = 0x%x, want 0x%x", cpu.GetReg(3), expected)
		}
	})

	// Test LBU (zero extension)
	t.Run("LBU zero extend", func(t *testing.T) {
		cpu.Reset()
		cpu.PC = 0x80000000
		cpu.SetReg(1, 0x80)
		cpu.SetReg(2, 0x80000100)

		// SB x1, 0(x2)
		writeInsn(cpu, cpu.PC, 0x00110023)
		cpu.Step()

		// LBU x3, 0(x2) -> 0x00014183
		writeInsn(cpu, cpu.PC, 0x00014183)
		cpu.Step()

		// Should be zero-extended
		if cpu.GetReg(3) != 0x80 {
			t.Errorf("x3 = 0x%x, want 0x80", cpu.GetReg(3))
		}
	})
}

// TestMulDiv tests M extension instructions
func TestMulDiv(t *testing.T) {
	cpu := testCPU(t)

	tests := []struct {
		name     string
		insn     uint32
		rs1Val   uint64
		rs2Val   uint64
		expected uint64
	}{
		// MUL x1, x2, x3 -> 0x023100B3
		{"MUL", 0x023100B3, 7, 6, 42},
		{"MUL negative", 0x023100B3, 0xFFFFFFFFFFFFFFFF, 2, 0xFFFFFFFFFFFFFFFE}, // -1 * 2 = -2

		// DIV x1, x2, x3 -> 0x023140B3
		{"DIV", 0x023140B3, 42, 6, 7},
		{"DIV negative", 0x023140B3, 0xFFFFFFFFFFFFFFF6, 2, 0xFFFFFFFFFFFFFFFB}, // -10 / 2 = -5
		{"DIV by zero", 0x023140B3, 42, 0, 0xFFFFFFFFFFFFFFFF},                  // -1

		// DIVU x1, x2, x3 -> 0x023150B3
		{"DIVU", 0x023150B3, 42, 6, 7},
		{"DIVU by zero", 0x023150B3, 42, 0, 0xFFFFFFFFFFFFFFFF},

		// REM x1, x2, x3 -> 0x023160B3
		{"REM", 0x023160B3, 43, 6, 1},
		{"REM by zero", 0x023160B3, 43, 0, 43},

		// REMU x1, x2, x3 -> 0x023170B3
		{"REMU", 0x023170B3, 43, 6, 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.PC = 0x80000000
			cpu.SetReg(2, tc.rs1Val)
			cpu.SetReg(3, tc.rs2Val)
			writeInsn(cpu, cpu.PC, tc.insn)

			err := cpu.Step()
			if err != nil {
				t.Fatalf("Step failed: %v", err)
			}

			if cpu.GetReg(1) != tc.expected {
				t.Errorf("x1 = 0x%x (%d), want 0x%x (%d)",
					cpu.GetReg(1), int64(cpu.GetReg(1)),
					tc.expected, int64(tc.expected))
			}
		})
	}
}

// TestCSR tests CSR instructions
func TestCSR(t *testing.T) {
	cpu := testCPU(t)

	// CSRRW x1, mstatus, x2
	t.Run("CSRRW", func(t *testing.T) {
		cpu.Reset()
		cpu.PC = 0x80000000
		cpu.Mstatus = 0x1234
		cpu.SetReg(2, 0x8888)
		// CSRRW x1, mstatus(0x300), x2 -> 0x300110F3
		writeInsn(cpu, cpu.PC, 0x300110F3)

		cpu.Step()

		// x1 should have old mstatus value
		if cpu.GetReg(1) != 0x1234 {
			t.Errorf("x1 = 0x%x, want 0x1234", cpu.GetReg(1))
		}
	})

	// CSRRS x1, mstatus, x2 (set bits)
	t.Run("CSRRS", func(t *testing.T) {
		cpu.Reset()
		cpu.PC = 0x80000000
		cpu.Mstatus = 0x0008  // MIE set
		cpu.SetReg(2, 0x0080) // Set MPIE
		// CSRRS x1, mstatus, x2 -> 0x300120F3
		writeInsn(cpu, cpu.PC, 0x300120F3)

		cpu.Step()

		// x1 should have old value
		if cpu.GetReg(1) != 0x0008 {
			t.Errorf("x1 = 0x%x, want 0x0008", cpu.GetReg(1))
		}
	})
}

// TestECALL tests ECALL instruction
func TestECALL(t *testing.T) {
	cpu := testCPU(t)

	// ECALL -> 0x00000073
	cpu.Reset()
	cpu.PC = 0x80000000
	cpu.Priv = PrivMachine
	cpu.Mtvec = 0x80002000
	writeInsn(cpu, cpu.PC, 0x00000073)

	cpu.Step()

	// Should have taken exception
	if cpu.Mcause != uint64(CauseMachineEcall) {
		t.Errorf("mcause = 0x%x, want 0x%x", cpu.Mcause, CauseMachineEcall)
	}
	if cpu.Mepc != 0x80000000 {
		t.Errorf("mepc = 0x%x, want 0x80000000", cpu.Mepc)
	}
	if cpu.PC != 0x80002000 {
		t.Errorf("PC = 0x%x, want 0x80002000", cpu.PC)
	}
}

// TestRV64 tests RV64-specific instructions (ADDIW, ADDW, etc.)
func TestRV64(t *testing.T) {
	cpu := testCPU(t)

	tests := []struct {
		name     string
		insn     uint32
		rs1Val   uint64
		rs2Val   uint64
		expected uint64
	}{
		// ADDIW x1, x2, 10 -> 0x00A1009B
		{"ADDIW", 0x00A1009B, 0x7FFFFFFF, 0, 0xFFFFFFFF80000009}, // Overflow wraps to negative
		{"ADDIW positive", 0x00A1009B, 100, 0, 110},

		// ADDW x1, x2, x3 -> 0x003100BB
		{"ADDW", 0x003100BB, 0x7FFFFFFF, 1, 0xFFFFFFFF80000000}, // Overflow

		// SUBW x1, x2, x3 -> 0x403100BB
		{"SUBW", 0x403100BB, 100, 30, 70},

		// SLLW x1, x2, x3 -> 0x003110BB
		{"SLLW", 0x003110BB, 1, 4, 16},

		// SRLW x1, x2, x3 -> 0x003150BB
		{"SRLW", 0x003150BB, 0x80000000, 4, 0x08000000},

		// SRAW x1, x2, x3 -> 0x403150BB
		{"SRAW", 0x403150BB, 0x80000000, 4, 0xFFFFFFFFF8000000}, // Sign extended
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.PC = 0x80000000
			cpu.SetReg(2, tc.rs1Val)
			cpu.SetReg(3, tc.rs2Val)
			writeInsn(cpu, cpu.PC, tc.insn)

			err := cpu.Step()
			if err != nil {
				t.Fatalf("Step failed: %v", err)
			}

			if cpu.GetReg(1) != tc.expected {
				t.Errorf("x1 = 0x%x, want 0x%x", cpu.GetReg(1), tc.expected)
			}
		})
	}
}

// TestAtomic tests atomic instructions (LR/SC, AMO)
func TestAtomic(t *testing.T) {
	cpu := testCPU(t)

	// Test LR.D / SC.D
	t.Run("LR.D/SC.D success", func(t *testing.T) {
		cpu.Reset()
		cpu.PC = 0x80000000
		addr := uint64(0x80000100)
		cpu.SetReg(1, addr)

		// Store initial value
		cpu.Mem.Write64(addr, 0x1234)

		// LR.D x2, (x1) -> 0x1000B12F
		writeInsn(cpu, cpu.PC, 0x1000B12F)
		cpu.Step()

		if cpu.GetReg(2) != 0x1234 {
			t.Errorf("x2 = 0x%x, want 0x1234", cpu.GetReg(2))
		}
		if !cpu.LoadReservationValid {
			t.Error("reservation should be valid")
		}

		// SC.D x3, x4, (x1) -> 0x1840B1AF (x4=0x5678)
		cpu.SetReg(4, 0x5678)
		writeInsn(cpu, cpu.PC, 0x1840B1AF)
		cpu.Step()

		// x3 should be 0 (success)
		if cpu.GetReg(3) != 0 {
			t.Errorf("x3 = %d, want 0 (success)", cpu.GetReg(3))
		}

		// Memory should have new value
		val, _ := cpu.Mem.Read64(addr)
		if val != 0x5678 {
			t.Errorf("mem = 0x%x, want 0x5678", val)
		}
	})

	// Test AMOADD.W
	t.Run("AMOADD.W", func(t *testing.T) {
		cpu.Reset()
		cpu.PC = 0x80000000
		addr := uint64(0x80000100)
		cpu.SetReg(1, addr)
		cpu.SetReg(3, 10)

		// Store initial value
		cpu.Mem.Write32(addr, 100)

		// AMOADD.W x2, x3, (x1) -> 0x0030A12F
		writeInsn(cpu, cpu.PC, 0x0030A12F)
		cpu.Step()

		// x2 should have old value (sign-extended)
		if cpu.GetReg(2) != 100 {
			t.Errorf("x2 = %d, want 100", cpu.GetReg(2))
		}

		// Memory should have old + rs2
		val, _ := cpu.Mem.Read32(addr)
		if val != 110 {
			t.Errorf("mem = %d, want 110", val)
		}
	})
}

// TestWFI tests WFI instruction
func TestWFI(t *testing.T) {
	cpu := testCPU(t)

	// WFI -> 0x10500073
	cpu.Reset()
	cpu.PC = 0x80000000
	cpu.Priv = PrivMachine
	cpu.Mip = 0 // No pending interrupts
	cpu.Mie = 0
	writeInsn(cpu, cpu.PC, 0x10500073)

	cpu.Step()

	if !cpu.PowerDownFlag {
		t.Error("CPU should be in power-down mode")
	}
	if cpu.PC != 0x80000004 {
		t.Errorf("PC = 0x%x, want 0x80000004", cpu.PC)
	}
}

// Instruction encoding helpers for tests

// encodeITypeTest encodes an I-type instruction
func encodeITypeTest(opcode, rd, funct3, rs1 int, imm int32) uint32 {
	return uint32(imm)<<20 | uint32(rs1)<<15 | uint32(funct3)<<12 | uint32(rd)<<7 | uint32(opcode)
}

// encodeRTypeTest encodes an R-type instruction
func encodeRTypeTest(opcode, rd, funct3, rs1, rs2, funct7 int) uint32 {
	return uint32(funct7)<<25 | uint32(rs2)<<20 | uint32(rs1)<<15 | uint32(funct3)<<12 | uint32(rd)<<7 | uint32(opcode)
}

// TestOpImmSLTI tests SLTI instruction (set less than immediate, signed)
func TestOpImmSLTI(t *testing.T) {
	cpu := testCPU(t)

	tests := []struct {
		name     string
		rs1Val   uint64
		imm      int32
		expected uint64
	}{
		{"positive less than", 5, 10, 1},
		{"positive not less", 15, 10, 0},
		{"equal", 10, 10, 0},
		{"negative less than positive", 0xFFFFFFFFFFFFFFFF, 1, 1},  // -1 < 1
		{"positive less than negative", 1, -1, 0},                  // 1 < -1 is false
		{"negative less than negative", 0xFFFFFFFFFFFFFFFE, -1, 1}, // -2 < -1
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.PC = 0x80000000
			cpu.SetReg(2, tc.rs1Val)

			// SLTI x1, x2, imm
			insn := encodeITypeTest(OpcodeOpImm, 1, Funct3SLTI, 2, tc.imm)
			writeInsn(cpu, cpu.PC, insn)

			err := cpu.Step()
			if err != nil {
				t.Fatalf("Step failed: %v", err)
			}

			if cpu.GetReg(1) != tc.expected {
				t.Errorf("x1 = %d, want %d", cpu.GetReg(1), tc.expected)
			}
		})
	}
}

// TestOpImmSLTIU tests SLTIU instruction (set less than immediate, unsigned)
func TestOpImmSLTIU(t *testing.T) {
	cpu := testCPU(t)

	tests := []struct {
		name     string
		rs1Val   uint64
		imm      int32
		expected uint64
	}{
		{"less than", 5, 10, 1},
		{"not less", 15, 10, 0},
		{"equal", 10, 10, 0},
		{"max uint vs 1", 0xFFFFFFFFFFFFFFFF, 1, 0}, // Max uint > 1 (unsigned)
		{"1 vs -1 (as unsigned)", 1, -1, 1},         // 1 < 0xFFFF... (unsigned)
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.PC = 0x80000000
			cpu.SetReg(2, tc.rs1Val)

			// SLTIU x1, x2, imm
			insn := encodeITypeTest(OpcodeOpImm, 1, Funct3SLTIU, 2, tc.imm)
			writeInsn(cpu, cpu.PC, insn)

			err := cpu.Step()
			if err != nil {
				t.Fatalf("Step failed: %v", err)
			}

			if cpu.GetReg(1) != tc.expected {
				t.Errorf("x1 = %d, want %d", cpu.GetReg(1), tc.expected)
			}
		})
	}
}

// TestOpImmXORI tests XORI instruction
func TestOpImmXORI(t *testing.T) {
	cpu := testCPU(t)

	tests := []struct {
		name     string
		rs1Val   uint64
		imm      int32
		expected uint64
	}{
		{"basic xor", 0xFF00, 0x0F0, 0xFF00 ^ 0x0F0},
		{"xor with -1 (NOT)", 0x12345678, -1, ^uint64(0x12345678)},
		{"xor zero", 0x12345678, 0, 0x12345678},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.PC = 0x80000000
			cpu.SetReg(2, tc.rs1Val)

			// XORI x1, x2, imm
			insn := encodeITypeTest(OpcodeOpImm, 1, Funct3XORI, 2, tc.imm)
			writeInsn(cpu, cpu.PC, insn)

			err := cpu.Step()
			if err != nil {
				t.Fatalf("Step failed: %v", err)
			}

			if cpu.GetReg(1) != tc.expected {
				t.Errorf("x1 = 0x%x, want 0x%x", cpu.GetReg(1), tc.expected)
			}
		})
	}
}

// TestOpImmORI tests ORI instruction
func TestOpImmORI(t *testing.T) {
	cpu := testCPU(t)

	tests := []struct {
		name     string
		rs1Val   uint64
		imm      int32
		expected uint64
	}{
		{"basic or", 0xFF00, 0x0F0, 0xFF00 | 0x0F0},
		{"or with zero", 0x12345678, 0, 0x12345678},
		{"set bits", 0x0, 0x7FF, 0x7FF},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.PC = 0x80000000
			cpu.SetReg(2, tc.rs1Val)

			// ORI x1, x2, imm
			insn := encodeITypeTest(OpcodeOpImm, 1, Funct3ORI, 2, tc.imm)
			writeInsn(cpu, cpu.PC, insn)

			err := cpu.Step()
			if err != nil {
				t.Fatalf("Step failed: %v", err)
			}

			if cpu.GetReg(1) != tc.expected {
				t.Errorf("x1 = 0x%x, want 0x%x", cpu.GetReg(1), tc.expected)
			}
		})
	}
}

// TestOpImmANDI tests ANDI instruction
func TestOpImmANDI(t *testing.T) {
	cpu := testCPU(t)

	tests := []struct {
		name     string
		rs1Val   uint64
		imm      int32
		expected uint64
	}{
		{"basic and", 0xFFFF, 0x0F0, 0x0F0},
		{"and with -1", 0x12345678, -1, 0x12345678},
		{"mask low bits", 0x12345678, 0xFF, 0x78},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.PC = 0x80000000
			cpu.SetReg(2, tc.rs1Val)

			// ANDI x1, x2, imm
			insn := encodeITypeTest(OpcodeOpImm, 1, Funct3ANDI, 2, tc.imm)
			writeInsn(cpu, cpu.PC, insn)

			err := cpu.Step()
			if err != nil {
				t.Fatalf("Step failed: %v", err)
			}

			if cpu.GetReg(1) != tc.expected {
				t.Errorf("x1 = 0x%x, want 0x%x", cpu.GetReg(1), tc.expected)
			}
		})
	}
}

// TestOpImmSLLI tests SLLI instruction (shift left logical immediate)
func TestOpImmSLLI(t *testing.T) {
	cpu := testCPU(t)

	tests := []struct {
		name     string
		rs1Val   uint64
		shamt    uint32
		expected uint64
	}{
		{"shift by 0", 0x12345678, 0, 0x12345678},
		{"shift by 1", 1, 1, 2},
		{"shift by 4", 0x1, 4, 0x10},
		{"shift by 32", 0x1, 32, 0x100000000},
		{"shift by 63", 0x1, 63, 0x8000000000000000},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.PC = 0x80000000
			cpu.SetReg(2, tc.rs1Val)

			// SLLI x1, x2, shamt (funct3=1, funct7=0)
			insn := uint32(tc.shamt)<<20 | uint32(2)<<15 | uint32(Funct3SLLI)<<12 | uint32(1)<<7 | OpcodeOpImm
			writeInsn(cpu, cpu.PC, insn)

			err := cpu.Step()
			if err != nil {
				t.Fatalf("Step failed: %v", err)
			}

			if cpu.GetReg(1) != tc.expected {
				t.Errorf("x1 = 0x%x, want 0x%x", cpu.GetReg(1), tc.expected)
			}
		})
	}
}

// TestOpImmSRLI tests SRLI instruction (shift right logical immediate)
func TestOpImmSRLI(t *testing.T) {
	cpu := testCPU(t)

	tests := []struct {
		name     string
		rs1Val   uint64
		shamt    uint32
		expected uint64
	}{
		{"shift by 0", 0x12345678, 0, 0x12345678},
		{"shift by 4", 0x100, 4, 0x10},
		{"shift negative by 4", 0xFFFFFFFFFFFFFF00, 4, 0x0FFFFFFFFFFFFFF0},
		{"shift by 32", 0x100000000, 32, 0x1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.PC = 0x80000000
			cpu.SetReg(2, tc.rs1Val)

			// SRLI x1, x2, shamt (funct3=5, bit30=0)
			insn := uint32(tc.shamt)<<20 | uint32(2)<<15 | uint32(Funct3SRLI)<<12 | uint32(1)<<7 | OpcodeOpImm
			writeInsn(cpu, cpu.PC, insn)

			err := cpu.Step()
			if err != nil {
				t.Fatalf("Step failed: %v", err)
			}

			if cpu.GetReg(1) != tc.expected {
				t.Errorf("x1 = 0x%x, want 0x%x", cpu.GetReg(1), tc.expected)
			}
		})
	}
}

// TestOpImmSRAI tests SRAI instruction (shift right arithmetic immediate)
func TestOpImmSRAI(t *testing.T) {
	cpu := testCPU(t)

	tests := []struct {
		name     string
		rs1Val   uint64
		shamt    uint32
		expected uint64
	}{
		{"shift positive by 4", 0x100, 4, 0x10},
		{"shift negative by 4", 0xFFFFFFFFFFFFFF00, 4, 0xFFFFFFFFFFFFFFF0},
		{"shift by 0", 0x12345678, 0, 0x12345678},
		{"preserve sign", 0x8000000000000000, 1, 0xC000000000000000},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.PC = 0x80000000
			cpu.SetReg(2, tc.rs1Val)

			// SRAI x1, x2, shamt (funct3=5, bit30=1)
			insn := uint32(0x40000000) | uint32(tc.shamt)<<20 | uint32(2)<<15 | uint32(Funct3SRLI)<<12 | uint32(1)<<7 | OpcodeOpImm
			writeInsn(cpu, cpu.PC, insn)

			err := cpu.Step()
			if err != nil {
				t.Fatalf("Step failed: %v", err)
			}

			if cpu.GetReg(1) != tc.expected {
				t.Errorf("x1 = 0x%x, want 0x%x", cpu.GetReg(1), tc.expected)
			}
		})
	}
}

// TestFENCE tests FENCE instruction
func TestFENCE(t *testing.T) {
	cpu := testCPU(t)
	cpu.Reset()
	cpu.PC = 0x80000000

	// FENCE instruction (opcode=0x0F, funct3=0)
	// FENCE iorw, iorw -> 0x0FF0000F
	writeInsn(cpu, cpu.PC, 0x0FF0000F)

	err := cpu.Step()
	if err != nil {
		t.Fatalf("Step failed: %v", err)
	}

	// FENCE is a NOP in single-threaded emulator, just check PC advanced
	if cpu.PC != 0x80000004 {
		t.Errorf("PC = 0x%x, want 0x80000004", cpu.PC)
	}
}

// TestFENCEI tests FENCE.I instruction
func TestFENCEI(t *testing.T) {
	cpu := testCPU(t)
	cpu.Reset()
	cpu.PC = 0x80000000

	// Set up TLB code entry
	cpu.TLBCode[0].VAddr = 0x80000000

	// FENCE.I instruction (opcode=0x0F, funct3=1)
	writeInsn(cpu, cpu.PC, 0x0000100F)

	err := cpu.Step()
	if err != nil {
		t.Fatalf("Step failed: %v", err)
	}

	// FENCE.I should flush code TLB
	if cpu.TLBCode[0].VAddr != ^uint64(0) {
		t.Error("TLB code entry should be invalidated by FENCE.I")
	}

	if cpu.PC != 0x80000004 {
		t.Errorf("PC = 0x%x, want 0x80000004", cpu.PC)
	}
}

// TestMulDiv32 tests 32-bit multiply/divide instructions (RV64M)
func TestMulDiv32(t *testing.T) {
	cpu := testCPU(t)

	tests := []struct {
		name     string
		funct3   int
		rs1Val   uint64
		rs2Val   uint64
		expected uint64
	}{
		// MULW: multiply low 32 bits, sign-extend result
		{"MULW basic", 0, 7, 6, 42},
		{"MULW negative", 0, 0xFFFFFFFF, 2, 0xFFFFFFFFFFFFFFFE}, // -1 * 2 = -2
		{"MULW overflow", 0, 0x10000, 0x10000, 0},               // 2^16 * 2^16 = 2^32, low 32 bits = 0

		// DIVW: signed 32-bit division
		{"DIVW basic", 4, 42, 6, 7},
		{"DIVW negative", 4, 0xFFFFFFFFFFFFFFF6, 2, 0xFFFFFFFFFFFFFFFB},  // -10 / 2 = -5
		{"DIVW by zero", 4, 42, 0, 0xFFFFFFFFFFFFFFFF},                   // -1
		{"DIVW overflow", 4, 0x80000000, 0xFFFFFFFF, 0xFFFFFFFF80000000}, // INT32_MIN / -1

		// DIVUW: unsigned 32-bit division
		{"DIVUW basic", 5, 42, 6, 7},
		{"DIVUW by zero", 5, 42, 0, 0xFFFFFFFFFFFFFFFF},

		// REMW: signed 32-bit remainder
		{"REMW basic", 6, 43, 6, 1},
		{"REMW negative", 6, 0xFFFFFFFFFFFFFFF6, 3, 0xFFFFFFFFFFFFFFFF}, // -10 % 3 = -1
		{"REMW by zero", 6, 43, 0, 43},

		// REMUW: unsigned 32-bit remainder
		{"REMUW basic", 7, 43, 6, 1},
		{"REMUW by zero", 7, 43, 0, 43},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.PC = 0x80000000
			cpu.SetReg(2, tc.rs1Val)
			cpu.SetReg(3, tc.rs2Val)

			// OP-32 with funct7=1 (M extension)
			insn := encodeRTypeTest(OpcodeOp32, 1, tc.funct3, 2, 3, 1)
			writeInsn(cpu, cpu.PC, insn)

			err := cpu.Step()
			if err != nil {
				t.Fatalf("Step failed: %v", err)
			}

			if cpu.GetReg(1) != tc.expected {
				t.Errorf("x1 = 0x%x (%d), want 0x%x (%d)",
					cpu.GetReg(1), int64(cpu.GetReg(1)),
					tc.expected, int64(tc.expected))
			}
		})
	}
}

// TestMulHigh tests high-word multiplication instructions
func TestMulHigh(t *testing.T) {
	cpu := testCPU(t)

	tests := []struct {
		name     string
		funct3   int
		rs1Val   uint64
		rs2Val   uint64
		expected uint64
	}{
		// MULH: signed * signed, high bits
		// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:96-99, 150-159
		{"MULH positive", Funct3MULH, 0x100000000, 0x100000000, 0x1},
		{"MULH negative", Funct3MULH, 0xFFFFFFFFFFFFFFFF, 0x2, 0xFFFFFFFFFFFFFFFF},    // -1 * 2 high = -1
		{"MULH both negative", Funct3MULH, 0xFFFFFFFFFFFFFFFF, 0xFFFFFFFFFFFFFFFF, 0}, // -1 * -1 high = 0
		{"MULH -2 * 3", Funct3MULH, 0xFFFFFFFFFFFFFFFE, 0x3, 0xFFFFFFFFFFFFFFFF},      // -2 * 3 = -6, high = -1
		{"MULH 2 * -3", Funct3MULH, 0x2, 0xFFFFFFFFFFFFFFFD, 0xFFFFFFFFFFFFFFFF},      // 2 * -3 = -6, high = -1
		{"MULH large positive", Funct3MULH, 0x7FFFFFFFFFFFFFFF, 0x7FFFFFFFFFFFFFFF, 0x3FFFFFFFFFFFFFFF},

		// MULHU: unsigned * unsigned, high bits
		// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:106-109, 123-146
		{"MULHU basic", Funct3MULHU, 0x100000000, 0x100000000, 0x1},
		{"MULHU large", Funct3MULHU, 0xFFFFFFFFFFFFFFFF, 0x2, 0x1},
		{"MULHU max * max", Funct3MULHU, 0xFFFFFFFFFFFFFFFF, 0xFFFFFFFFFFFFFFFF, 0xFFFFFFFFFFFFFFFE},
		{"MULHU 0xFFFFFFFFFFFFFFFE * 3", Funct3MULHU, 0xFFFFFFFFFFFFFFFE, 0x3, 0x2},

		// MULHSU: signed * unsigned, high bits
		// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:101-104, 161-168
		{"MULHSU positive", Funct3MULHSU, 0x100000000, 0x100000000, 0x1},
		{"MULHSU negative rs1", Funct3MULHSU, 0xFFFFFFFFFFFFFFFF, 0x2, 0xFFFFFFFFFFFFFFFF},   // -1 * 2
		{"MULHSU -2 * 3", Funct3MULHSU, 0xFFFFFFFFFFFFFFFE, 0x3, 0xFFFFFFFFFFFFFFFF},         // -2 * 3 = -6, high = -1
		{"MULHSU large negative", Funct3MULHSU, 0x8000000000000000, 0x2, 0xFFFFFFFFFFFFFFFF}, // MIN_INT64 * 2
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.PC = 0x80000000
			cpu.SetReg(2, tc.rs1Val)
			cpu.SetReg(3, tc.rs2Val)

			// OP with funct7=1 (M extension)
			insn := encodeRTypeTest(OpcodeOp, 1, tc.funct3, 2, 3, 1)
			writeInsn(cpu, cpu.PC, insn)

			err := cpu.Step()
			if err != nil {
				t.Fatalf("Step failed: %v", err)
			}

			if cpu.GetReg(1) != tc.expected {
				t.Errorf("x1 = 0x%x, want 0x%x", cpu.GetReg(1), tc.expected)
			}
		})
	}
}

// TestAMO32Operations tests 32-bit atomic memory operations
func TestAMO32Operations(t *testing.T) {
	cpu := testCPU(t)

	tests := []struct {
		name        string
		funct5      uint32
		initMem     uint32
		rs2Val      uint64
		expectedRd  uint64 // Old value (sign-extended)
		expectedMem uint32
	}{
		// AMOSWAP.W
		{"AMOSWAP.W", Funct5AMOSWAP, 100, 200, 100, 200},

		// AMOXOR.W
		{"AMOXOR.W", Funct5AMOXOR, 0xFF00, 0x0F0F, 0xFF00, 0xF00F},

		// AMOAND.W
		{"AMOAND.W", Funct5AMOAND, 0xFF00, 0x0F0F, 0xFF00, 0x0F00},

		// AMOOR.W
		{"AMOOR.W", Funct5AMOOR, 0xFF00, 0x0F0F, 0xFF00, 0xFF0F},

		// AMOMIN.W
		{"AMOMIN.W smaller rs2", Funct5AMOMIN, 100, 50, 100, 50},
		{"AMOMIN.W smaller mem", Funct5AMOMIN, 50, 100, 50, 50},
		{"AMOMIN.W negative", Funct5AMOMIN, 0xFFFFFFFF, 1, 0xFFFFFFFFFFFFFFFF, 0xFFFFFFFF}, // -1 < 1

		// AMOMAX.W
		{"AMOMAX.W larger rs2", Funct5AMOMAX, 50, 100, 50, 100},
		{"AMOMAX.W larger mem", Funct5AMOMAX, 100, 50, 100, 100},

		// AMOMINU.W
		{"AMOMINU.W smaller rs2", Funct5AMOMINU, 100, 50, 100, 50},
		{"AMOMINU.W -1 vs 1", Funct5AMOMINU, 0xFFFFFFFF, 1, 0xFFFFFFFFFFFFFFFF, 1}, // 1 < 0xFFFFFFFF (unsigned)

		// AMOMAXU.W
		{"AMOMAXU.W -1 vs 1", Funct5AMOMAXU, 0xFFFFFFFF, 1, 0xFFFFFFFFFFFFFFFF, 0xFFFFFFFF},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.PC = 0x80000000
			addr := uint64(0x80000100)
			cpu.SetReg(1, addr)
			cpu.SetReg(3, tc.rs2Val)

			// Store initial value
			cpu.Mem.Write32(addr, tc.initMem)

			// AMO.W instruction: funct5 | aq | rl | rs2 | rs1 | funct3=010 | rd | opcode
			insn := (tc.funct5 << 27) | (3 << 20) | (1 << 15) | (2 << 12) | (2 << 7) | OpcodeAMO
			writeInsn(cpu, cpu.PC, insn)

			err := cpu.Step()
			if err != nil {
				t.Fatalf("Step failed: %v", err)
			}

			// Check rd has old value
			if cpu.GetReg(2) != tc.expectedRd {
				t.Errorf("x2 = 0x%x (%d), want 0x%x (%d)",
					cpu.GetReg(2), int64(cpu.GetReg(2)),
					tc.expectedRd, int64(tc.expectedRd))
			}

			// Check memory has new value
			val, _ := cpu.Mem.Read32(addr)
			if val != tc.expectedMem {
				t.Errorf("mem = 0x%x, want 0x%x", val, tc.expectedMem)
			}
		})
	}
}

// TestAMO64Operations tests 64-bit atomic memory operations
func TestAMO64Operations(t *testing.T) {
	cpu := testCPU(t)

	tests := []struct {
		name        string
		funct5      uint32
		initMem     uint64
		rs2Val      uint64
		expectedRd  uint64
		expectedMem uint64
	}{
		// AMOSWAP.D
		{"AMOSWAP.D", Funct5AMOSWAP, 100, 200, 100, 200},

		// AMOXOR.D
		{"AMOXOR.D", Funct5AMOXOR, 0xFF00FF00FF00FF00, 0x0F0F0F0F0F0F0F0F, 0xFF00FF00FF00FF00, 0xF00FF00FF00FF00F},

		// AMOAND.D
		{"AMOAND.D", Funct5AMOAND, 0xFF00FF00FF00FF00, 0x0F0F0F0F0F0F0F0F, 0xFF00FF00FF00FF00, 0x0F000F000F000F00},

		// AMOOR.D
		{"AMOOR.D", Funct5AMOOR, 0xFF00FF00FF00FF00, 0x0F0F0F0F0F0F0F0F, 0xFF00FF00FF00FF00, 0xFF0FFF0FFF0FFF0F},

		// AMOMIN.D
		{"AMOMIN.D negative", Funct5AMOMIN, 0xFFFFFFFFFFFFFFFF, 1, 0xFFFFFFFFFFFFFFFF, 0xFFFFFFFFFFFFFFFF},

		// AMOMAX.D
		{"AMOMAX.D negative", Funct5AMOMAX, 0xFFFFFFFFFFFFFFFF, 1, 0xFFFFFFFFFFFFFFFF, 1},

		// AMOMINU.D
		{"AMOMINU.D unsigned compare", Funct5AMOMINU, 0xFFFFFFFFFFFFFFFF, 1, 0xFFFFFFFFFFFFFFFF, 1},

		// AMOMAXU.D
		{"AMOMAXU.D unsigned compare", Funct5AMOMAXU, 0xFFFFFFFFFFFFFFFF, 1, 0xFFFFFFFFFFFFFFFF, 0xFFFFFFFFFFFFFFFF},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.PC = 0x80000000
			addr := uint64(0x80000100)
			cpu.SetReg(1, addr)
			cpu.SetReg(3, tc.rs2Val)

			// Store initial value
			cpu.Mem.Write64(addr, tc.initMem)

			// AMO.D instruction: funct5 | aq | rl | rs2 | rs1 | funct3=011 | rd | opcode
			insn := (tc.funct5 << 27) | (3 << 20) | (1 << 15) | (3 << 12) | (2 << 7) | OpcodeAMO
			writeInsn(cpu, cpu.PC, insn)

			err := cpu.Step()
			if err != nil {
				t.Fatalf("Step failed: %v", err)
			}

			// Check rd has old value
			if cpu.GetReg(2) != tc.expectedRd {
				t.Errorf("x2 = 0x%x, want 0x%x", cpu.GetReg(2), tc.expectedRd)
			}

			// Check memory has new value
			val, _ := cpu.Mem.Read64(addr)
			if val != tc.expectedMem {
				t.Errorf("mem = 0x%x, want 0x%x", val, tc.expectedMem)
			}
		})
	}
}

// TestSCFailure tests SC failure when reservation is lost
func TestSCFailure(t *testing.T) {
	cpu := testCPU(t)
	cpu.Reset()
	cpu.PC = 0x80000000
	addr := uint64(0x80000100)
	cpu.SetReg(1, addr)
	cpu.SetReg(4, 0x5678)

	// Store initial value
	cpu.Mem.Write64(addr, 0x1234)

	// SC.D without prior LR.D (no reservation)
	cpu.LoadReservationValid = false
	insn := uint32((Funct5SC << 27) | (4 << 20) | (1 << 15) | (3 << 12) | (3 << 7) | OpcodeAMO)
	writeInsn(cpu, cpu.PC, insn)

	cpu.Step()

	// x3 should be 1 (failure)
	if cpu.GetReg(3) != 1 {
		t.Errorf("x3 = %d, want 1 (failure)", cpu.GetReg(3))
	}

	// Memory should NOT have changed
	val, _ := cpu.Mem.Read64(addr)
	if val != 0x1234 {
		t.Errorf("mem = 0x%x, want 0x1234 (unchanged)", val)
	}
}

// TestLRSC32 tests 32-bit LR/SC
func TestLRSC32(t *testing.T) {
	cpu := testCPU(t)
	cpu.Reset()
	cpu.PC = 0x80000000
	addr := uint64(0x80000100)
	cpu.SetReg(1, addr)

	// Store initial value
	cpu.Mem.Write32(addr, 0x1234)

	// LR.W x2, (x1)
	insn := uint32((Funct5LR << 27) | (1 << 15) | (2 << 12) | (2 << 7) | OpcodeAMO)
	writeInsn(cpu, cpu.PC, insn)
	cpu.Step()

	// x2 should have value (sign-extended)
	if cpu.GetReg(2) != 0x1234 {
		t.Errorf("x2 = 0x%x, want 0x1234", cpu.GetReg(2))
	}
	if !cpu.LoadReservationValid {
		t.Error("reservation should be valid")
	}

	// SC.W x3, x4, (x1)
	cpu.SetReg(4, 0x5678)
	insn = uint32((Funct5SC << 27) | (4 << 20) | (1 << 15) | (2 << 12) | (3 << 7) | OpcodeAMO)
	writeInsn(cpu, cpu.PC, insn)
	cpu.Step()

	// x3 should be 0 (success)
	if cpu.GetReg(3) != 0 {
		t.Errorf("x3 = %d, want 0 (success)", cpu.GetReg(3))
	}

	// Memory should have new value
	val, _ := cpu.Mem.Read32(addr)
	if val != 0x5678 {
		t.Errorf("mem = 0x%x, want 0x5678", val)
	}
}

// TestOpImm32SLLIW tests SLLIW instruction
func TestOpImm32SLLIW(t *testing.T) {
	cpu := testCPU(t)

	tests := []struct {
		name     string
		rs1Val   uint64
		shamt    uint32
		expected uint64
	}{
		{"shift by 0", 0x12345678, 0, 0x12345678},
		{"shift by 4", 0x1, 4, 0x10},
		{"shift overflow", 0x80000000, 1, 0}, // Shifts out all bits
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.PC = 0x80000000
			cpu.SetReg(2, tc.rs1Val)

			// SLLIW x1, x2, shamt (opcode=0x1B, funct3=1)
			insn := uint32(tc.shamt)<<20 | uint32(2)<<15 | uint32(1)<<12 | uint32(1)<<7 | OpcodeOpImm32
			writeInsn(cpu, cpu.PC, insn)

			err := cpu.Step()
			if err != nil {
				t.Fatalf("Step failed: %v", err)
			}

			if cpu.GetReg(1) != tc.expected {
				t.Errorf("x1 = 0x%x, want 0x%x", cpu.GetReg(1), tc.expected)
			}
		})
	}
}

// TestOpImm32SRLIW tests SRLIW instruction
func TestOpImm32SRLIW(t *testing.T) {
	cpu := testCPU(t)

	tests := []struct {
		name     string
		rs1Val   uint64
		shamt    uint32
		expected uint64
	}{
		{"shift by 0", 0x12345678, 0, 0x12345678},
		{"shift by 4", 0x100, 4, 0x10},
		{"shift negative", 0xFFFFFFFF80000000, 4, 0x8000000}, // Operates on low 32 bits only, zero-fills, sign-extends result
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.PC = 0x80000000
			cpu.SetReg(2, tc.rs1Val)

			// SRLIW x1, x2, shamt (opcode=0x1B, funct3=5, bit30=0)
			insn := uint32(tc.shamt)<<20 | uint32(2)<<15 | uint32(5)<<12 | uint32(1)<<7 | OpcodeOpImm32
			writeInsn(cpu, cpu.PC, insn)

			err := cpu.Step()
			if err != nil {
				t.Fatalf("Step failed: %v", err)
			}

			if cpu.GetReg(1) != tc.expected {
				t.Errorf("x1 = 0x%x, want 0x%x", cpu.GetReg(1), tc.expected)
			}
		})
	}
}

// TestOpImm32SRAIW tests SRAIW instruction
func TestOpImm32SRAIW(t *testing.T) {
	cpu := testCPU(t)

	tests := []struct {
		name     string
		rs1Val   uint64
		shamt    uint32
		expected uint64
	}{
		{"shift positive by 4", 0x100, 4, 0x10},
		{"shift negative by 4", 0x80000000, 4, 0xFFFFFFFFF8000000}, // Sign-extends
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.PC = 0x80000000
			cpu.SetReg(2, tc.rs1Val)

			// SRAIW x1, x2, shamt (opcode=0x1B, funct3=5, bit30=1)
			insn := uint32(0x40000000) | uint32(tc.shamt)<<20 | uint32(2)<<15 | uint32(5)<<12 | uint32(1)<<7 | OpcodeOpImm32
			writeInsn(cpu, cpu.PC, insn)

			err := cpu.Step()
			if err != nil {
				t.Fatalf("Step failed: %v", err)
			}

			if cpu.GetReg(1) != tc.expected {
				t.Errorf("x1 = 0x%x, want 0x%x", cpu.GetReg(1), tc.expected)
			}
		})
	}
}

// TestLoadHalf tests LH and LHU instructions
func TestLoadHalf(t *testing.T) {
	cpu := testCPU(t)

	t.Run("LH sign extend", func(t *testing.T) {
		cpu.Reset()
		cpu.PC = 0x80000000
		cpu.SetReg(2, 0x80000100)

		// Store 0x8000 (negative half)
		cpu.Mem.Write16(0x80000100, 0x8000)

		// LH x1, 0(x2)
		insn := encodeITypeTest(OpcodeLoad, 1, Funct3LH, 2, 0)
		writeInsn(cpu, cpu.PC, insn)
		cpu.Step()

		expected := uint64(0xFFFFFFFFFFFF8000)
		if cpu.GetReg(1) != expected {
			t.Errorf("x1 = 0x%x, want 0x%x", cpu.GetReg(1), expected)
		}
	})

	t.Run("LHU zero extend", func(t *testing.T) {
		cpu.Reset()
		cpu.PC = 0x80000000
		cpu.SetReg(2, 0x80000100)

		// Store 0x8000
		cpu.Mem.Write16(0x80000100, 0x8000)

		// LHU x1, 0(x2)
		insn := encodeITypeTest(OpcodeLoad, 1, Funct3LHU, 2, 0)
		writeInsn(cpu, cpu.PC, insn)
		cpu.Step()

		if cpu.GetReg(1) != 0x8000 {
			t.Errorf("x1 = 0x%x, want 0x8000", cpu.GetReg(1))
		}
	})
}

// TestLoadWordUnsigned tests LWU instruction (RV64)
func TestLoadWordUnsigned(t *testing.T) {
	cpu := testCPU(t)
	cpu.Reset()
	cpu.PC = 0x80000000
	cpu.SetReg(2, 0x80000100)

	// Store 0x80000000 (negative if sign-extended)
	cpu.Mem.Write32(0x80000100, 0x80000000)

	// LWU x1, 0(x2)
	insn := encodeITypeTest(OpcodeLoad, 1, Funct3LWU, 2, 0)
	writeInsn(cpu, cpu.PC, insn)
	cpu.Step()

	// Should be zero-extended
	if cpu.GetReg(1) != 0x80000000 {
		t.Errorf("x1 = 0x%x, want 0x80000000", cpu.GetReg(1))
	}
}

// TestStoreHalf tests SH instruction
func TestStoreHalf(t *testing.T) {
	cpu := testCPU(t)
	cpu.Reset()
	cpu.PC = 0x80000000
	cpu.SetReg(1, 0x12345678)
	cpu.SetReg(2, 0x80000100)

	// SH x1, 0(x2)
	insn := uint32(0) | uint32(1)<<20 | uint32(2)<<15 | uint32(1)<<12 | uint32(0)<<7 | OpcodeStore
	writeInsn(cpu, cpu.PC, insn)
	cpu.Step()

	// Memory should have low 16 bits
	val, _ := cpu.Mem.Read16(0x80000100)
	if val != 0x5678 {
		t.Errorf("mem = 0x%x, want 0x5678", val)
	}
}

// TestDivOverflow tests division overflow edge cases
func TestDivOverflow(t *testing.T) {
	cpu := testCPU(t)

	// DIV overflow: INT64_MIN / -1 = INT64_MIN (per RISC-V spec)
	t.Run("DIV64 overflow", func(t *testing.T) {
		cpu.Reset()
		cpu.PC = 0x80000000
		cpu.SetReg(2, 0x8000000000000000) // INT64_MIN
		cpu.SetReg(3, 0xFFFFFFFFFFFFFFFF) // -1

		insn := encodeRTypeTest(OpcodeOp, 1, Funct3DIV, 2, 3, 1)
		writeInsn(cpu, cpu.PC, insn)
		cpu.Step()

		if cpu.GetReg(1) != 0x8000000000000000 {
			t.Errorf("x1 = 0x%x, want 0x8000000000000000", cpu.GetReg(1))
		}
	})

	// REM overflow: INT64_MIN % -1 = 0 (per RISC-V spec)
	t.Run("REM64 overflow", func(t *testing.T) {
		cpu.Reset()
		cpu.PC = 0x80000000
		cpu.SetReg(2, 0x8000000000000000) // INT64_MIN
		cpu.SetReg(3, 0xFFFFFFFFFFFFFFFF) // -1

		insn := encodeRTypeTest(OpcodeOp, 1, Funct3REM, 2, 3, 1)
		writeInsn(cpu, cpu.PC, insn)
		cpu.Step()

		if cpu.GetReg(1) != 0 {
			t.Errorf("x1 = 0x%x, want 0", cpu.GetReg(1))
		}
	})
}

// TestREMU64ByZero tests unsigned remainder by zero
func TestREMU64ByZero(t *testing.T) {
	cpu := testCPU(t)
	cpu.Reset()
	cpu.PC = 0x80000000
	cpu.SetReg(2, 43)
	cpu.SetReg(3, 0)

	// REMU x1, x2, x3
	insn := encodeRTypeTest(OpcodeOp, 1, Funct3REMU, 2, 3, 1)
	writeInsn(cpu, cpu.PC, insn)
	cpu.Step()

	// Per RISC-V spec: remu by zero returns dividend
	if cpu.GetReg(1) != 43 {
		t.Errorf("x1 = %d, want 43", cpu.GetReg(1))
	}
}

// TestCompressedJALRLinkAddress tests that C.JALR sets link address to PC+2 (not PC+4)
// This is a regression test for a bug where compressed JAL/JALR instructions
// incorrectly set the link register to PC+4 instead of PC+2.
// Reference: RISC-V spec - for compressed instructions, link address is PC+2
func TestCompressedJALRLinkAddress(t *testing.T) {
	cpu := testCPU(t)

	// C.JALR a4 at 0x80001000
	// Encoding: 0x9702 (funct4=1001, rs1=x14, rs2=0, op=10)
	// Little-endian: 02 97
	// This should set ra = PC + 2 = 0x80001002, then jump to [a4]
	cpu.Mem.Write8(0x80001000, 0x02)
	cpu.Mem.Write8(0x80001001, 0x97)

	cpu.PC = 0x80001000
	cpu.SetReg(14, 0x80002000) // a4 = target address

	// Execute C.JALR a4
	err := cpu.Step()
	if err != nil {
		t.Fatalf("C.JALR failed: %v", err)
	}

	// Check link address (ra = x1) should be PC+2 for compressed instruction
	ra := cpu.GetReg(1)
	if ra != 0x80001002 {
		t.Errorf("C.JALR link address wrong: expected 0x80001002, got 0x%x", ra)
	}

	// Check PC jumped to target
	if cpu.PC != 0x80002000 {
		t.Errorf("C.JALR target wrong: expected 0x80002000, got 0x%x", cpu.PC)
	}
}

// TestRegularJALRLinkAddress tests that regular (32-bit) JALR sets link address to PC+4
func TestRegularJALRLinkAddress(t *testing.T) {
	cpu := testCPU(t)

	// Regular JALR rd=1, rs1=14, imm=0 at 0x80001000
	// Encoding: JALR x1, 0(x14) = 0x000700e7
	cpu.Mem.Write32(0x80001000, 0x000700e7)

	cpu.PC = 0x80001000
	cpu.SetReg(14, 0x80002000) // a4 = target address

	// Execute JALR
	err := cpu.Step()
	if err != nil {
		t.Fatalf("JALR failed: %v", err)
	}

	// Check link address (ra = x1) should be PC+4 for 32-bit instruction
	ra := cpu.GetReg(1)
	if ra != 0x80001004 {
		t.Errorf("JALR link address wrong: expected 0x80001004, got 0x%x", ra)
	}

	// Check PC jumped to target
	if cpu.PC != 0x80002000 {
		t.Errorf("JALR target wrong: expected 0x80002000, got 0x%x", cpu.PC)
	}
}

// TestCompressedInstructionExecution tests execution of compressed instructions
func TestCompressedInstructionExecution(t *testing.T) {
	cpu := testCPU(t)

	// Test C.ADDI (adds immediate to register)
	t.Run("C.ADDI x1, 5", func(t *testing.T) {
		cpu.Reset()
		cpu.PC = 0x80000000
		cpu.SetReg(1, 10)

		// C.ADDI x1, 5: funct3=000, imm[5]=0, rd=1, imm[4:0]=5, op=01
		// = 0x0085
		cpu.Mem.Write16(cpu.PC, 0x0095) // C.ADDI x1, 5

		err := cpu.Step()
		if err != nil {
			t.Fatalf("Step failed: %v", err)
		}

		// Check PC advanced by 2 (compressed instruction)
		if cpu.PC != 0x80000002 {
			t.Errorf("PC = 0x%x, want 0x80000002", cpu.PC)
		}
	})

	// Test C.LI (load immediate)
	t.Run("C.LI x1, 31", func(t *testing.T) {
		cpu.Reset()
		cpu.PC = 0x80000000

		// C.LI x1, 31: funct3=010, imm[5]=0, rd=1, imm[4:0]=31, op=01
		// = 0x40FD
		cpu.Mem.Write16(cpu.PC, 0x40FD)

		err := cpu.Step()
		if err != nil {
			t.Fatalf("Step failed: %v", err)
		}

		if cpu.GetReg(1) != 31 {
			t.Errorf("x1 = %d, want 31", cpu.GetReg(1))
		}
		if cpu.PC != 0x80000002 {
			t.Errorf("PC = 0x%x, want 0x80000002", cpu.PC)
		}
	})

	// Test C.MV (move register)
	t.Run("C.MV x1, x2", func(t *testing.T) {
		cpu.Reset()
		cpu.PC = 0x80000000
		cpu.SetReg(2, 0xDEADBEEF)

		// C.MV x1, x2: funct3=100, bit12=0, rd=1, rs2=2, op=10
		// = 0x808A
		cpu.Mem.Write16(cpu.PC, 0x808A)

		err := cpu.Step()
		if err != nil {
			t.Fatalf("Step failed: %v", err)
		}

		if cpu.GetReg(1) != 0xDEADBEEF {
			t.Errorf("x1 = 0x%x, want 0xDEADBEEF", cpu.GetReg(1))
		}
		if cpu.PC != 0x80000002 {
			t.Errorf("PC = 0x%x, want 0x80000002", cpu.PC)
		}
	})

	// Test C.NOP
	t.Run("C.NOP", func(t *testing.T) {
		cpu.Reset()
		cpu.PC = 0x80000000

		// C.NOP: 0x0001
		cpu.Mem.Write16(cpu.PC, 0x0001)

		err := cpu.Step()
		if err != nil {
			t.Fatalf("Step failed: %v", err)
		}

		// PC should advance by 2 (compressed instruction)
		if cpu.PC != 0x80000002 {
			t.Errorf("PC = 0x%x, want 0x80000002", cpu.PC)
		}
	})
}

// TestIllegalCompressedInstruction tests that illegal compressed instructions raise exception
func TestIllegalCompressedInstruction(t *testing.T) {
	cpu := testCPU(t)
	cpu.Reset()
	cpu.PC = 0x80000000
	cpu.Mtvec = 0x80010000

	// C.ADDI4SPN with nzuimm=0 is illegal
	cpu.Mem.Write16(cpu.PC, 0x0000)

	cpu.Step()

	if cpu.Mcause != CauseIllegalInsn {
		t.Errorf("mcause = 0x%x, want 0x%x (illegal insn)", cpu.Mcause, CauseIllegalInsn)
	}
}

// TestRunWithMultipleInstructions tests Run() with multiple instructions
func TestRunWithMultipleInstructions(t *testing.T) {
	cpu := testCPU(t)
	cpu.Reset()
	cpu.PC = 0x80000000

	// Write a sequence of instructions
	// ADDI x1, x0, 1
	writeInsn(cpu, 0x80000000, 0x00100093)
	// ADDI x2, x1, 1
	writeInsn(cpu, 0x80000004, 0x00108113)
	// ADDI x3, x2, 1
	writeInsn(cpu, 0x80000008, 0x00110193)

	cpu.NCycles = 3
	cpu.Run(3)

	if cpu.GetReg(1) != 1 {
		t.Errorf("x1 = %d, want 1", cpu.GetReg(1))
	}
	if cpu.GetReg(2) != 2 {
		t.Errorf("x2 = %d, want 2", cpu.GetReg(2))
	}
	if cpu.GetReg(3) != 3 {
		t.Errorf("x3 = %d, want 3", cpu.GetReg(3))
	}
	if cpu.PC != 0x8000000C {
		t.Errorf("PC = 0x%x, want 0x8000000C", cpu.PC)
	}
}

// TestRunWithPowerDown tests Run() exits on power down
func TestRunWithPowerDown(t *testing.T) {
	cpu := testCPU(t)
	cpu.Reset()
	cpu.PC = 0x80000000

	// Set power down flag
	cpu.PowerDownFlag = true

	// Run should exit immediately
	cpu.Run(100)

	// PC should not have changed
	if cpu.PC != 0x80000000 {
		t.Errorf("PC = 0x%x, want 0x80000000 (should not execute with power down)", cpu.PC)
	}
}

// TestDiv128 tests 128-bit division operations for RV128 support.
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:37-75 (using XLEN=128)
func TestDiv128(t *testing.T) {
	// -10 in two's complement 128-bit
	neg10 := uint128.New(0xFFFFFFFFFFFFFFF6, 0xFFFFFFFFFFFFFFFF)
	// -3 in two's complement 128-bit
	neg3 := uint128.New(0xFFFFFFFFFFFFFFFD, 0xFFFFFFFFFFFFFFFF)

	tests := []struct {
		name     string
		a        uint128.Uint128
		b        uint128.Uint128
		expected uint128.Uint128
	}{
		// div128 (signed)
		{"div128 positive", uint128.From64(10), uint128.From64(3), uint128.From64(3)},
		{"div128 by zero", uint128.From64(10), uint128.Zero, uint128.Max},
		{"div128 negative by positive", neg10, uint128.From64(3), neg3}, // -10 / 3 = -3
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := div128(tc.a, tc.b)
			if !result.Equals(tc.expected) {
				t.Errorf("div128(%v, %v) = %v, want %v", tc.a, tc.b, result, tc.expected)
			}
		})
	}
}

// TestDivu128 tests 128-bit unsigned division operations.
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:48-55 (using XLEN=128)
func TestDivu128(t *testing.T) {
	tests := []struct {
		name     string
		a        uint128.Uint128
		b        uint128.Uint128
		expected uint128.Uint128
	}{
		{"divu128 basic", uint128.From64(10), uint128.From64(3), uint128.From64(3)},
		{"divu128 by zero", uint128.From64(10), uint128.Zero, uint128.Max},
		{"divu128 large", uint128.Max, uint128.From64(2), uint128.New(0xFFFFFFFFFFFFFFFF, 0x7FFFFFFFFFFFFFFF)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := divu128(tc.a, tc.b)
			if !result.Equals(tc.expected) {
				t.Errorf("divu128(%v, %v) = %v, want %v", tc.a, tc.b, result, tc.expected)
			}
		})
	}
}

// TestRem128 tests 128-bit remainder operations.
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:57-75 (using XLEN=128)
func TestRem128(t *testing.T) {
	tests := []struct {
		name     string
		a        uint128.Uint128
		b        uint128.Uint128
		expected uint128.Uint128
	}{
		{"rem128 positive", uint128.From64(10), uint128.From64(3), uint128.From64(1)},
		{"rem128 by zero", uint128.From64(10), uint128.Zero, uint128.From64(10)},
		{"remu128 basic", uint128.From64(10), uint128.From64(3), uint128.From64(1)},
		{"remu128 by zero", uint128.From64(10), uint128.Zero, uint128.From64(10)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var result uint128.Uint128
			if tc.name[:4] == "remu" {
				result = remu128(tc.a, tc.b)
			} else {
				result = rem128(tc.a, tc.b)
			}
			if !result.Equals(tc.expected) {
				t.Errorf("got %v, want %v", result, tc.expected)
			}
		})
	}
}

// TestMulhu128 tests 128-bit unsigned multiply high.
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:123-146 (using XLEN=128)
func TestMulhu128(t *testing.T) {
	tests := []struct {
		name     string
		a        uint128.Uint128
		b        uint128.Uint128
		expected uint128.Uint128
	}{
		{"mulhu128 small", uint128.From64(1), uint128.From64(1), uint128.Zero},
		{"mulhu128 2^64 * 2", uint128.New(0, 1), uint128.From64(2), uint128.Zero}, // Result fits in 128 bits
		{"mulhu128 2^64 * 2^64", uint128.New(0, 1), uint128.New(0, 1), uint128.From64(1)},
		{"mulhu128 max * 2", uint128.Max, uint128.From64(2), uint128.From64(1)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := mulhu128(tc.a, tc.b)
			if !result.Equals(tc.expected) {
				t.Errorf("mulhu128(%v, %v) = %v, want %v", tc.a, tc.b, result, tc.expected)
			}
		})
	}
}

// TestMulh128 tests 128-bit signed multiply high.
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:150-159 (using XLEN=128)
func TestMulh128(t *testing.T) {
	negOne := uint128.Max // -1 in two's complement
	// -2 in two's complement: 0xFFFFFFFFFFFFFFFF_FFFFFFFFFFFFFFFE
	negTwo := uint128.New(0xFFFFFFFFFFFFFFFE, 0xFFFFFFFFFFFFFFFF)

	tests := []struct {
		name     string
		a        uint128.Uint128
		b        uint128.Uint128
		expected uint128.Uint128
	}{
		{"mulh128 1 * 1", uint128.From64(1), uint128.From64(1), uint128.Zero},
		{"mulh128 -1 * 2", negOne, uint128.From64(2), negOne}, // -1 * 2 = -2, high = -1
		{"mulh128 -1 * -1", negOne, negOne, uint128.Zero},     // -1 * -1 = 1, high = 0
		{"mulh128 -2 * 3", negTwo, uint128.From64(3), negOne}, // -2 * 3 = -6, high = -1
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := mulh128(tc.a, tc.b)
			if !result.Equals(tc.expected) {
				t.Errorf("mulh128(%v, %v) = %v, want %v", tc.a, tc.b, result, tc.expected)
			}
		})
	}
}

// TestMulhsu128 tests 128-bit signed*unsigned multiply high.
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:161-168 (using XLEN=128)
func TestMulhsu128(t *testing.T) {
	negOne := uint128.Max // -1 in two's complement
	// -2 in two's complement: 0xFFFFFFFFFFFFFFFF_FFFFFFFFFFFFFFFE
	negTwo := uint128.New(0xFFFFFFFFFFFFFFFE, 0xFFFFFFFFFFFFFFFF)

	tests := []struct {
		name     string
		a        uint128.Uint128
		b        uint128.Uint128
		expected uint128.Uint128
	}{
		{"mulhsu128 1 * 1", uint128.From64(1), uint128.From64(1), uint128.Zero},
		{"mulhsu128 -1 * 2", negOne, uint128.From64(2), negOne}, // -1 * 2 = -2, high = -1
		{"mulhsu128 -2 * 3", negTwo, uint128.From64(3), negOne}, // -2 * 3 = -6, high = -1
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := mulhsu128(tc.a, tc.b)
			if !result.Equals(tc.expected) {
				t.Errorf("mulhsu128(%v, %v) = %v, want %v", tc.a, tc.b, result, tc.expected)
			}
		})
	}
}

// =============================================================================
// Tests for illegal instruction detection
// These tests verify that the Go implementation correctly rejects illegal
// instruction encodings that match the C TinyEMU behavior.
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h
// =============================================================================

// TestIllegalSLLI tests that SLLI rejects illegal encodings.
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:950-953
func TestIllegalSLLI(t *testing.T) {
	cpu := testCPU(t)

	tests := []struct {
		name    string
		insn    uint32
		illegal bool
	}{
		// Valid SLLI x1, x2, 0: 0x00011093
		{"SLLI x1, x2, 0", 0x00011093, false},
		// Valid SLLI x1, x2, 63: 0x03f11093
		{"SLLI x1, x2, 63", 0x03f11093, false},
		// Invalid: bit 26 set (would be shamt=64 which exceeds 63)
		// Encoding: imm[11:0]=0x040 (bit 6 set), rs1=x2, funct3=001, rd=x1, opcode=0010011
		{"SLLI with bit 6 set", 0x04011093, true},
		// Invalid: bit 30 set (this would be SRAI encoding, but with funct3=SLLI)
		{"SLLI with bit 30 set", 0x40011093, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.PC = 0x80000000
			cpu.SetReg(2, 0x1234)
			writeInsn(cpu, cpu.PC, tc.insn)

			cpu.Step()

			if tc.illegal {
				// Exception is handled immediately, so check Mcause
				if cpu.Mcause != uint64(CauseIllegalInsn) {
					t.Errorf("Expected illegal instruction exception for %s, got Mcause=%d", tc.name, cpu.Mcause)
				}
			} else {
				if cpu.Mcause == uint64(CauseIllegalInsn) {
					t.Errorf("Unexpected exception for %s", tc.name)
				}
			}
		})
	}
}

// TestIllegalSRLI tests that SRLI rejects illegal encodings.
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:964-971
func TestIllegalSRLI(t *testing.T) {
	cpu := testCPU(t)

	tests := []struct {
		name    string
		insn    uint32
		illegal bool
	}{
		// Valid SRLI x1, x2, 0: 0x00015093
		{"SRLI x1, x2, 0", 0x00015093, false},
		// Valid SRLI x1, x2, 63: 0x03f15093
		{"SRLI x1, x2, 63", 0x03f15093, false},
		// Valid SRAI x1, x2, 5 (bit 10 set): 0x40515093
		{"SRAI x1, x2, 5", 0x40515093, false},
		// Invalid: bit 6 set (shamt>63)
		{"SRLI with bit 6 set", 0x04015093, true},
		// Invalid: bit 29 set (only bit 30 allowed for SRAI)
		{"SRLI with bit 29 set", 0x20015093, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.PC = 0x80000000
			cpu.SetReg(2, 0x1234)
			writeInsn(cpu, cpu.PC, tc.insn)

			cpu.Step()

			if tc.illegal {
				// Exception is handled immediately, so check Mcause
				if cpu.Mcause != uint64(CauseIllegalInsn) {
					t.Errorf("Expected illegal instruction exception for %s, got Mcause=%d", tc.name, cpu.Mcause)
				}
			} else {
				if cpu.Mcause == uint64(CauseIllegalInsn) {
					t.Errorf("Unexpected exception for %s", tc.name)
				}
			}
		})
	}
}

// TestIllegalSLLIW tests that SLLIW rejects illegal encodings.
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:992-995
func TestIllegalSLLIW(t *testing.T) {
	cpu := testCPU(t)

	tests := []struct {
		name    string
		insn    uint32
		illegal bool
	}{
		// Valid SLLIW x1, x2, 0: 0x0001109b
		{"SLLIW x1, x2, 0", 0x0001109b, false},
		// Valid SLLIW x1, x2, 31: 0x01f1109b
		{"SLLIW x1, x2, 31", 0x01f1109b, false},
		// Invalid: bit 5 set (shamt>31)
		{"SLLIW with bit 5 set", 0x0201109b, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.PC = 0x80000000
			cpu.SetReg(2, 0x1234)
			writeInsn(cpu, cpu.PC, tc.insn)

			cpu.Step()

			if tc.illegal {
				if cpu.PendingException != CauseIllegalInsn {
					t.Errorf("Expected illegal instruction exception for %s", tc.name)
				}
			} else {
				if cpu.PendingException >= 0 {
					t.Errorf("Unexpected exception %d for %s", cpu.PendingException, tc.name)
				}
			}
		})
	}
}

// TestIllegalOPFunct7 tests that OP instructions reject illegal funct7 values.
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:1076-1077
func TestIllegalOPFunct7(t *testing.T) {
	cpu := testCPU(t)

	tests := []struct {
		name    string
		insn    uint32
		illegal bool
	}{
		// Valid ADD x1, x2, x3: funct7=0x00
		{"ADD x1, x2, x3", 0x003100b3, false},
		// Valid SUB x1, x2, x3: funct7=0x20
		{"SUB x1, x2, x3", 0x403100b3, false},
		// Valid MUL x1, x2, x3: funct7=0x01
		{"MUL x1, x2, x3", 0x023100b3, false},
		// Invalid: funct7=0x10 (not 0x00 or 0x20 or 0x01)
		{"OP with funct7=0x10", 0x203100b3, true},
		// Invalid: funct7=0x21 (has bit 0 and bit 5 set)
		{"OP with funct7=0x21", 0x423100b3, true},
		// Invalid SLL with funct7=0x20 (SLL only allows funct7=0x00)
		{"SLL with funct7=0x20", 0x403110b3, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.PC = 0x80000000
			cpu.SetReg(2, 100)
			cpu.SetReg(3, 50)
			writeInsn(cpu, cpu.PC, tc.insn)

			cpu.Step()

			if tc.illegal {
				if cpu.PendingException != CauseIllegalInsn {
					t.Errorf("Expected illegal instruction exception for %s, got exception=%d", tc.name, cpu.PendingException)
				}
			} else {
				if cpu.PendingException >= 0 {
					t.Errorf("Unexpected exception %d for %s", cpu.PendingException, tc.name)
				}
			}
		})
	}
}

// TestIllegalFENCE tests that FENCE rejects illegal encodings.
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:1354-1357
func TestIllegalFENCE(t *testing.T) {
	cpu := testCPU(t)

	tests := []struct {
		name    string
		insn    uint32
		illegal bool
	}{
		// Valid FENCE (full fence): 0x0ff0000f
		{"FENCE full", 0x0ff0000f, false},
		// Valid FENCE with different pred/succ: 0x0310000f
		{"FENCE r,w", 0x0310000f, false},
		// Invalid: rd != 0 (bit 7 set)
		{"FENCE rd!=0", 0x0ff0008f, true},
		// Invalid: rs1 != 0 (bit 15 set)
		{"FENCE rs1!=0", 0x0ff8000f, true},
		// Invalid: fm field set (bit 28 set)
		{"FENCE fm!=0", 0x1ff0000f, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.PC = 0x80000000
			writeInsn(cpu, cpu.PC, tc.insn)

			cpu.Step()

			if tc.illegal {
				if cpu.PendingException != CauseIllegalInsn {
					t.Errorf("Expected illegal instruction exception for %s", tc.name)
				}
			} else {
				if cpu.PendingException >= 0 {
					t.Errorf("Unexpected exception %d for %s", cpu.PendingException, tc.name)
				}
			}
		})
	}
}

// TestIllegalFENCEI tests that FENCE.I rejects non-standard encodings.
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:1358-1361
func TestIllegalFENCEI(t *testing.T) {
	cpu := testCPU(t)

	tests := []struct {
		name    string
		insn    uint32
		illegal bool
	}{
		// Valid FENCE.I: exact encoding 0x0000100f
		{"FENCE.I", 0x0000100f, false},
		// Invalid: any other bit set
		{"FENCE.I rd!=0", 0x0000108f, true},
		{"FENCE.I rs1!=0", 0x0008100f, true},
		{"FENCE.I imm!=0", 0x0010100f, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.PC = 0x80000000
			writeInsn(cpu, cpu.PC, tc.insn)

			cpu.Step()

			if tc.illegal {
				if cpu.PendingException != CauseIllegalInsn {
					t.Errorf("Expected illegal instruction exception for %s", tc.name)
				}
			} else {
				if cpu.PendingException >= 0 {
					t.Errorf("Unexpected exception %d for %s", cpu.PendingException, tc.name)
				}
			}
		})
	}
}

// TestIllegalECALL tests that ECALL rejects illegal encodings.
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:1281-1285
func TestIllegalECALL(t *testing.T) {
	cpu := testCPU(t)

	tests := []struct {
		name          string
		insn          uint32
		expectedCause uint64 // Expected value in Mcause after Step
	}{
		// Valid ECALL: 0x00000073 - should trigger machine ecall exception
		{"ECALL", 0x00000073, uint64(CauseMachineEcall)},
		// Invalid: rd != 0 - should trigger illegal instruction
		{"ECALL rd!=0", 0x000000f3, uint64(CauseIllegalInsn)},
		// Invalid: rs1 != 0 - should trigger illegal instruction
		{"ECALL rs1!=0", 0x00008073, uint64(CauseIllegalInsn)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.PC = 0x80000000
			writeInsn(cpu, cpu.PC, tc.insn)

			cpu.Step()

			// Check Mcause for the exception that was handled
			if cpu.Mcause != tc.expectedCause {
				t.Errorf("Expected Mcause=%d for %s, got %d", tc.expectedCause, tc.name, cpu.Mcause)
			}
		})
	}
}

// TestIllegalWFI tests that WFI rejects illegal encodings.
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:1313-1325
func TestIllegalWFI(t *testing.T) {
	cpu := testCPU(t)

	tests := []struct {
		name    string
		insn    uint32
		illegal bool
	}{
		// Valid WFI: 0x10500073
		{"WFI", 0x10500073, false},
		// Invalid: rd != 0 (bit 7 set, rd = 1)
		{"WFI rd!=0", 0x105000f3, true},
		// Note: funct3 != 0 case is not tested here because it goes to
		// CSR handling before reaching executePriv.
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.PC = 0x80000000
			writeInsn(cpu, cpu.PC, tc.insn)

			cpu.Step()

			if tc.illegal {
				// For illegal encoding, Mcause should be CauseIllegalInsn
				if cpu.Mcause != uint64(CauseIllegalInsn) {
					t.Errorf("Expected Mcause=%d for %s, got %d", CauseIllegalInsn, tc.name, cpu.Mcause)
				}
			} else {
				// For valid WFI, PC should advance and PowerDownFlag should be set
				// (since no interrupts are pending)
				if cpu.PC != 0x80000004 {
					t.Errorf("PC should advance for valid WFI, got 0x%x", cpu.PC)
				}
				if !cpu.PowerDownFlag {
					t.Errorf("PowerDownFlag should be set for valid WFI")
				}
			}
		})
	}
}

// ========== RV32 Tests ==========

// testCPU32 creates an RV32 CPU with RAM for testing
func testCPU32(t *testing.T) *CPU {
	t.Helper()
	m := mem.NewPhysMemoryMap()
	_, err := m.RegisterRAM(0x80000000, 1024*1024, 0)
	if err != nil {
		t.Fatalf("failed to register RAM: %v", err)
	}
	cpu := NewCPU(m, XLEN32)
	cpu.PC = 0x80000000
	return cpu
}

// TestRV32ShiftValidation verifies that shift amounts are limited to 5 bits in RV32.
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:950-953
func TestRV32ShiftValidation(t *testing.T) {
	cpu := testCPU32(t)

	tests := []struct {
		name    string
		insn    uint32
		rs1Val  uint64
		illegal bool
	}{
		// SLLI x1, x2, 31 - valid in RV32 (max shift)
		// SLLI format: imm[11:0] | rs1 | 001 | rd | 0010011
		// shamt=31 is 0x1F, rd=1, rs1=2, funct3=001, opcode=0010011
		// 0x01F11093 = 000000_11111_00010_001_00001_0010011
		{"SLLI 31 valid", 0x01F11093, 1, false},
		// SLLI x1, x2, 32 - illegal in RV32 (bit 5 set)
		// shamt=32 is 0x20
		// 0x02011093 = 000001_00000_00010_001_00001_0010011
		{"SLLI 32 illegal", 0x02011093, 1, true},
		// SRLI x1, x2, 31 - valid in RV32
		// 0x01F15093 = 000000_11111_00010_101_00001_0010011
		{"SRLI 31 valid", 0x01F15093, 0x80000000, false},
		// SRLI x1, x2, 32 - illegal in RV32
		// 0x02015093 = 000001_00000_00010_101_00001_0010011
		{"SRLI 32 illegal", 0x02015093, 0x80000000, true},
		// SRAI x1, x2, 31 - valid in RV32
		// 0x41F15093 = 010000_11111_00010_101_00001_0010011
		{"SRAI 31 valid", 0x41F15093, 0x80000000, false},
		// SRAI x1, x2, 32 - illegal in RV32
		// 0x42015093 = 010000_100000_00010_101_00001_0010011 - but this has bit 5 set in shamt
		{"SRAI 32 illegal", 0x42015093, 0x80000000, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.PC = 0x80000000
			cpu.SetReg(2, tc.rs1Val)
			writeInsn(cpu, cpu.PC, tc.insn)

			cpu.Step()

			if tc.illegal {
				// Check pending exception since we're in machine mode and exceptions go to M-mode
				if cpu.Mcause != uint64(CauseIllegalInsn) {
					t.Errorf("expected illegal instruction exception (Mcause=%d), got Mcause=%d, PendingException=%d",
						CauseIllegalInsn, cpu.Mcause, cpu.PendingException)
				}
			} else {
				if cpu.Mcause == uint64(CauseIllegalInsn) {
					t.Errorf("unexpected illegal instruction exception")
				}
				if cpu.PC != 0x80000004 {
					t.Errorf("PC = 0x%x, want 0x80000004", cpu.PC)
				}
			}
		})
	}
}

// TestRV32WInstructionsIllegal verifies that W-instructions raise illegal instruction
// exceptions in RV32 mode.
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h (OpcodeOpImm32, OpcodeOp32 are RV64 only)
func TestRV32WInstructionsIllegal(t *testing.T) {
	cpu := testCPU32(t)

	tests := []struct {
		name string
		insn uint32
	}{
		// ADDIW x1, x2, 0 -> OpcodeOpImm32
		{"ADDIW", 0x0001009B},
		// SLLIW x1, x2, 0 -> OpcodeOpImm32
		{"SLLIW", 0x0001109B},
		// SRLIW x1, x2, 0 -> OpcodeOpImm32
		{"SRLIW", 0x0001509B},
		// SRAIW x1, x2, 0 -> OpcodeOpImm32
		{"SRAIW", 0x4001509B},
		// ADDW x1, x2, x3 -> OpcodeOp32
		{"ADDW", 0x003100BB},
		// SUBW x1, x2, x3 -> OpcodeOp32
		{"SUBW", 0x403100BB},
		// SLLW x1, x2, x3 -> OpcodeOp32
		{"SLLW", 0x003110BB},
		// SRLW x1, x2, x3 -> OpcodeOp32
		{"SRLW", 0x003150BB},
		// SRAW x1, x2, x3 -> OpcodeOp32
		{"SRAW", 0x403150BB},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.PC = 0x80000000
			cpu.SetReg(2, 100)
			cpu.SetReg(3, 50)
			writeInsn(cpu, cpu.PC, tc.insn)

			cpu.Step()

			if cpu.Mcause != uint64(CauseIllegalInsn) {
				t.Errorf("expected illegal instruction exception for %s, got Mcause=%d", tc.name, cpu.Mcause)
			}
		})
	}
}

// TestRV32LoadStoreWidth verifies that LD, LWU, and SD are illegal in RV32.
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h (XLEN >= 64)
func TestRV32LoadStoreWidth(t *testing.T) {
	cpu := testCPU32(t)

	tests := []struct {
		name string
		insn uint32
	}{
		// LD x1, 0(x2)
		{"LD", 0x00013083},
		// LWU x1, 0(x2)
		{"LWU", 0x00016083},
		// SD x3, 0(x2)
		{"SD", 0x00313023},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.PC = 0x80000000
			cpu.SetReg(2, 0x80000100) // Valid address
			cpu.SetReg(3, 0x12345678)
			writeInsn(cpu, cpu.PC, tc.insn)

			cpu.Step()

			if cpu.Mcause != uint64(CauseIllegalInsn) {
				t.Errorf("expected illegal instruction exception for %s, got Mcause=%d", tc.name, cpu.Mcause)
			}
		})
	}
}

// TestRV32CompressedJAL verifies that C.JAL works correctly in RV32 mode.
// In RV32, funct3=001 in quadrant 1 is C.JAL (vs C.ADDIW in RV64).
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:517-525
func TestRV32CompressedJAL(t *testing.T) {
	// C.JAL encodes: imm[11|4|9:8|10|6|7|3:1|5] = insn[12|11|10:9|8|7|6|5:3|2]
	// Format: funct3=001, rd/imm bits, op=01

	// For a simple test with offset=0:
	// All imm bits are 0, so insn = 0b001_0_00000_00000_01 = 0x2001
	cjal := uint16(0x2001)

	// Verify RV32 expansion produces JAL x1, offset
	expanded32, err := ExpandCompressed(cjal, XLEN32)
	if err != nil {
		t.Fatalf("ExpandCompressed failed for RV32: %v", err)
	}

	if ExtractOpcode(expanded32) != OpcodeJAL {
		t.Errorf("RV32: expected OpcodeJAL, got 0x%02x", ExtractOpcode(expanded32))
	}
	if ExtractRd(expanded32) != 1 {
		t.Errorf("RV32: expected rd=1, got %d", ExtractRd(expanded32))
	}

	// In RV64, 0x2001 is C.ADDIW with rd=0, which is reserved (illegal)
	// Use a different encoding with rd != 0 to test C.ADDIW
	caddiw := uint16(0x2085) // C.ADDIW x1, 1 (rd=1, imm=1)
	expanded64, err := ExpandCompressed(caddiw, XLEN64)
	if err != nil {
		t.Fatalf("ExpandCompressed failed for RV64: %v", err)
	}
	if ExtractOpcode(expanded64) != OpcodeOpImm32 {
		t.Errorf("RV64: expected OpcodeOpImm32 for C.ADDIW, got 0x%02x", ExtractOpcode(expanded64))
	}

	// In RV32, the same encoding (0x2085) is C.JAL with a non-zero offset
	expanded32b, err := ExpandCompressed(caddiw, XLEN32)
	if err != nil {
		t.Fatalf("ExpandCompressed failed for RV32 (caddiw): %v", err)
	}
	if ExtractOpcode(expanded32b) != OpcodeJAL {
		t.Errorf("RV32: expected OpcodeJAL for 0x2085, got 0x%02x", ExtractOpcode(expanded32b))
	}
}

// TestRV32CompressedShiftIllegal verifies that C.SRLI/C.SRAI with shamt >= 32
// are illegal in RV32.
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:499-506
func TestRV32CompressedShiftIllegal(t *testing.T) {
	tests := []struct {
		name    string
		insn    uint16
		illegal bool
	}{
		// C.SRLI x8, 31 - valid
		{"C.SRLI 31", 0x807D, false},
		// C.SRLI x8, 32 - bit 5 set, illegal in RV32
		{"C.SRLI 32", 0x9001, true},
		// C.SRAI x8, 31 - valid
		{"C.SRAI 31", 0x847D, false},
		// C.SRAI x8, 32 - bit 5 set, illegal in RV32
		{"C.SRAI 32", 0x9401, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ExpandCompressed(tc.insn, XLEN32)

			if tc.illegal {
				if err != ErrIllegalCompressedInsn {
					t.Errorf("expected illegal instruction for %s, got err=%v", tc.name, err)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error for %s: %v", tc.name, err)
				}
			}
		})
	}
}

// TestRV32CompressedSubwAddwIllegal verifies that C.SUBW/C.ADDW are illegal in RV32.
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:513-516
func TestRV32CompressedSubwAddwIllegal(t *testing.T) {
	tests := []struct {
		name string
		insn uint16
	}{
		// C.SUBW x8, x9
		{"C.SUBW", 0x9C05},
		// C.ADDW x8, x9
		{"C.ADDW", 0x9C25},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ExpandCompressed(tc.insn, XLEN32)

			if err != ErrIllegalCompressedInsn {
				t.Errorf("expected illegal instruction for %s in RV32, got err=%v", tc.name, err)
			}

			// Verify it's valid in RV64
			_, err = ExpandCompressed(tc.insn, XLEN64)
			if err != nil {
				t.Errorf("unexpected error for %s in RV64: %v", tc.name, err)
			}
		})
	}
}

// TestRV32Basic verifies basic RV32 operation (32-bit register width).
func TestRV32Basic(t *testing.T) {
	cpu := testCPU32(t)

	// Test that registers are 32-bit sign-extended
	cpu.Reset()
	cpu.PC = 0x80000000

	// LUI x1, 0x80000 - should sign-extend to 0xFFFFFFFF80000000
	writeInsn(cpu, cpu.PC, 0x800000B7)
	cpu.Step()

	// In RV32, register value should be sign-extended to 64 bits internally
	got := cpu.GetReg(1)
	expected := uint64(0xFFFFFFFF80000000) // 0x80000000 sign-extended to 64 bits
	if got != expected {
		t.Errorf("LUI x1, 0x80000: got 0x%x, want 0x%x", got, expected)
	}

	// Add should wrap at 32 bits
	cpu.Reset()
	cpu.PC = 0x80000000
	cpu.SetReg(2, 0xFFFFFFFF)
	cpu.SetReg(3, 2)
	// ADD x1, x2, x3
	writeInsn(cpu, cpu.PC, 0x003100B3)
	cpu.Step()

	got = cpu.GetReg(1)
	// 0xFFFFFFFF + 2 = 0x100000001, which wraps to 0x00000001 in 32 bits
	// Sign-extended to 64 bits: 0x0000000000000001
	expected = 1
	if got != expected {
		t.Errorf("ADD 0xFFFFFFFF + 2: got 0x%x, want 0x%x", got, expected)
	}
}

// TestRV32MulHighOperations verifies that MULH/MULHSU/MULHU compute the high
// 32 bits of a 64-bit product in RV32 mode (not high 64 bits of 128-bit product).
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:79-92
func TestRV32MulHighOperations(t *testing.T) {
	testCases := []struct {
		name   string
		funct3 uint32
		rs1    uint32
		rs2    uint32
		want   uint32
	}{
		// MULH: signed * signed, high 32 bits
		// 0x7FFFFFFF * 0x7FFFFFFF = 0x3FFFFFFF00000001 (64-bit)
		// High 32 bits = 0x3FFFFFFF
		{"MULH large positive", Funct3MULH, 0x7FFFFFFF, 0x7FFFFFFF, 0x3FFFFFFF},
		// -1 * 2 = -2, high 32 bits of 64-bit result = -1
		{"MULH -1 * 2", Funct3MULH, 0xFFFFFFFF, 0x00000002, 0xFFFFFFFF},
		// -2147483648 * 2 = -4294967296 (0xFFFFFFFF00000000)
		// High 32 bits = 0xFFFFFFFF
		{"MULH min * 2", Funct3MULH, 0x80000000, 0x00000002, 0xFFFFFFFF},
		// 0x10000 * 0x10000 = 0x100000000, high 32 bits = 0x1
		{"MULH 0x10000 * 0x10000", Funct3MULH, 0x00010000, 0x00010000, 0x00000001},

		// MULHU: unsigned * unsigned, high 32 bits
		// 0xFFFFFFFF * 0xFFFFFFFF = 0xFFFFFFFE00000001
		// High 32 bits = 0xFFFFFFFE
		{"MULHU max * max", Funct3MULHU, 0xFFFFFFFF, 0xFFFFFFFF, 0xFFFFFFFE},
		// 0x80000000 * 2 = 0x100000000, high 32 bits = 1
		{"MULHU 0x80000000 * 2", Funct3MULHU, 0x80000000, 0x00000002, 0x00000001},

		// MULHSU: signed * unsigned, high 32 bits
		// -1 (signed) * 2 (unsigned) = -2, high 32 bits = -1
		{"MULHSU -1 * 2", Funct3MULHSU, 0xFFFFFFFF, 0x00000002, 0xFFFFFFFF},
		// 0x7FFFFFFF * 2 = 0xFFFFFFFE, high 32 bits = 0
		{"MULHSU large positive * 2", Funct3MULHSU, 0x7FFFFFFF, 0x00000002, 0x00000000},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cpu := testCPU32(t)
			cpu.Reset()
			cpu.PC = 0x80000000

			// Set up source registers
			cpu.SetReg(2, uint64(tc.rs1))
			cpu.SetReg(3, uint64(tc.rs2))

			// Build OP instruction: funct7=1 (M extension), rd=1, rs1=2, rs2=3
			// Format: funct7[31:25] | rs2[24:20] | rs1[19:15] | funct3[14:12] | rd[11:7] | opcode[6:0]
			insn := uint32(0x01) << 25 // funct7 = 1 (M extension)
			insn |= uint32(3) << 20    // rs2 = 3
			insn |= uint32(2) << 15    // rs1 = 2
			insn |= tc.funct3 << 12    // funct3
			insn |= uint32(1) << 7     // rd = 1
			insn |= OpcodeOp           // opcode for OP

			writeInsn(cpu, cpu.PC, insn)
			cpu.Step()

			// Get result (low 32 bits, sign extended)
			got := uint32(cpu.GetReg(1))
			if got != tc.want {
				t.Errorf("%s: got 0x%08X, want 0x%08X", tc.name, got, tc.want)
			}
		})
	}
}

// TestRV32MulHighVs64 verifies that RV32 MULH gives different results than
// RV64 MULH for cases where the 64-bit product exceeds 32-bit high word capacity.
func TestRV32MulHighVs64(t *testing.T) {
	// For 0x7FFFFFFF * 0x7FFFFFFF:
	// - 64-bit product: 0x3FFFFFFF00000001
	// - RV32 MULH wants high 32 bits of 64-bit product: 0x3FFFFFFF
	// - RV64 MULH (incorrectly applied) would give high 64 bits of 128-bit product: 0

	cpu32 := testCPU32(t)
	cpu32.Reset()
	cpu32.PC = 0x80000000
	cpu32.SetReg(2, 0x7FFFFFFF)
	cpu32.SetReg(3, 0x7FFFFFFF)

	// MULH x1, x2, x3
	insn := uint32(0x01)<<25 | uint32(3)<<20 | uint32(2)<<15 | (Funct3MULH << 12) | uint32(1)<<7 | OpcodeOp
	writeInsn(cpu32, cpu32.PC, insn)
	cpu32.Step()

	got32 := uint32(cpu32.GetReg(1))
	want32 := uint32(0x3FFFFFFF)
	if got32 != want32 {
		t.Errorf("RV32 MULH 0x7FFFFFFF * 0x7FFFFFFF: got 0x%08X, want 0x%08X", got32, want32)
	}

	// Now verify RV64 gives the expected (different) result for the same inputs
	cpu64 := testCPU(t)
	cpu64.Reset()
	cpu64.PC = 0x80000000
	cpu64.SetReg(2, 0x7FFFFFFF)
	cpu64.SetReg(3, 0x7FFFFFFF)

	writeInsn(cpu64, cpu64.PC, insn)
	cpu64.Step()

	got64 := cpu64.GetReg(1)
	want64 := uint64(0) // High 64 bits of 128-bit product is 0 for this case
	if got64 != want64 {
		t.Errorf("RV64 MULH 0x7FFFFFFF * 0x7FFFFFFF: got 0x%016X, want 0x%016X", got64, want64)
	}
}
