package riscv

import (
	"testing"

	"github.com/jtolio/tinyemu-go/mem"
)

// benchCPU creates a CPU with RAM for benchmarking.
// Uses a larger RAM region to allow for more complex benchmark programs.
func benchCPU(b *testing.B) *CPU {
	b.Helper()
	m := mem.NewPhysMemoryMap()
	// Allocate 16MB of RAM at 0x80000000 (standard RISC-V RAM base)
	_, err := m.RegisterRAM(0x80000000, 16*1024*1024, 0)
	if err != nil {
		b.Fatalf("failed to register RAM: %v", err)
	}
	cpu := NewCPU(m, XLEN64)
	cpu.PC = 0x80000000
	return cpu
}

// BenchmarkStep measures the performance of stepping through individual instructions.
// This is the most fine-grained benchmark, measuring single instruction execution.
//
// Program:
//
//	loop:
//	    addi x1, x1, 1          # x1 = x1 + 1
//	    jal x0, loop            # jump back (infinite loop)
//
// Reference: This benchmarks the core Step() method in cpu/exec.go
func BenchmarkStep(b *testing.B) {
	cpu := benchCPU(b)

	// addi x1, x1, 1 -> 0x00108093
	// jal x0, -4 -> 0xffdff06f (jump back, discard link)
	writeInsn(cpu, 0x80000000, 0x00108093) // addi x1, x1, 1
	writeInsn(cpu, 0x80000004, 0xffdff06f) // jal x0, -4 (loop)

	cpu.InsnCounter = 0

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := cpu.Step(); err != nil {
			b.Fatalf("Step failed: %v", err)
		}
	}

	b.StopTimer()

	// Report MIPS (million instructions per second)
	insns := cpu.InsnCounter
	b.ReportMetric(float64(insns)/b.Elapsed().Seconds()/1e6, "MIPS")
}

// BenchmarkRun measures the performance of the Run() method.
// This benchmarks the main execution loop with interrupt checking.
//
// Program:
//
//	loop:
//	    addi x1, x1, 1          # x1 = x1 + 1
//	    jal x0, loop            # jump back (infinite loop)
//
// Reference: Tests cpu/exec.go Run() method performance
func BenchmarkRun(b *testing.B) {
	cpu := benchCPU(b)

	// addi x1, x1, 1 -> 0x00108093
	// jal x0, -4 -> 0xffdff06f (jump back, discard link)
	writeInsn(cpu, 0x80000000, 0x00108093) // addi x1, x1, 1
	writeInsn(cpu, 0x80000004, 0xffdff06f) // jal x0, -4 (loop)

	cpu.InsnCounter = 0

	b.ResetTimer()

	// Run b.N cycles
	if err := cpu.Run(b.N); err != nil {
		b.Fatalf("Run failed: %v", err)
	}

	b.StopTimer()

	// Report MIPS
	insns := cpu.InsnCounter
	b.ReportMetric(float64(insns)/b.Elapsed().Seconds()/1e6, "MIPS")
}

// BenchmarkALU measures ALU instruction throughput.
// Tests a mix of arithmetic and logical operations.
//
// Program:
//
//	loop:
//	    add x1, x1, x2          # addition
//	    sub x3, x1, x2          # subtraction
//	    and x4, x1, x3          # logical AND
//	    or x5, x1, x3           # logical OR
//	    jal x0, loop            # jump back
//
// Reference: Tests ALU operations in cpu/exec.go executeOp
func BenchmarkALU(b *testing.B) {
	cpu := benchCPU(b)

	// add x1, x1, x2 -> 0x002080b3
	// sub x3, x1, x2 -> 0x402081b3
	// and x4, x1, x3 -> 0x0030f233
	// or x5, x1, x3 -> 0x0030e2b3
	// jal x0, -16 -> 0xff1ff06f
	writeInsn(cpu, 0x80000000, 0x002080b3) // add x1, x1, x2
	writeInsn(cpu, 0x80000004, 0x402081b3) // sub x3, x1, x2
	writeInsn(cpu, 0x80000008, 0x0030f233) // and x4, x1, x3
	writeInsn(cpu, 0x8000000c, 0x0030e2b3) // or x5, x1, x3
	writeInsn(cpu, 0x80000010, 0xff1ff06f) // jal x0, -16

	cpu.SetReg(2, 7)
	cpu.InsnCounter = 0

	b.ResetTimer()

	if err := cpu.Run(b.N); err != nil {
		b.Fatalf("Run failed: %v", err)
	}

	b.StopTimer()

	insns := cpu.InsnCounter
	b.ReportMetric(float64(insns)/b.Elapsed().Seconds()/1e6, "MIPS")
}

// BenchmarkLoad measures memory load performance.
// Tests the TLB and memory access path for loads.
//
// Program:
//
//	    lui x2, 0x80001         # x2 = 0x80001000 (data address)
//	loop:
//	    lw x3, 0(x2)            # load word from memory
//	    jal x0, loop            # jump back
//
// Reference: Tests memory access path in cpu/mmu.go LoadU32
func BenchmarkLoad(b *testing.B) {
	cpu := benchCPU(b)

	// lui x2, 0x80001 -> 0x80001137 (x2 = 0x80001000)
	// lw x3, 0(x2) -> 0x00012183
	// jal x0, -4 -> 0xffdff06f
	writeInsn(cpu, 0x80000000, 0x80001137) // lui x2, 0x80001
	writeInsn(cpu, 0x80000004, 0x00012183) // lw x3, 0(x2)
	writeInsn(cpu, 0x80000008, 0xffdff06f) // jal x0, -4

	// Store test data at 0x80001000
	cpu.Mem.Write32(0x80001000, 0xDEADBEEF)

	// Execute LUI once to set up x2
	if err := cpu.Step(); err != nil {
		b.Fatalf("Step failed: %v", err)
	}
	cpu.InsnCounter = 0

	b.ResetTimer()

	if err := cpu.Run(b.N); err != nil {
		b.Fatalf("Run failed: %v", err)
	}

	b.StopTimer()

	insns := cpu.InsnCounter
	b.ReportMetric(float64(insns)/b.Elapsed().Seconds()/1e6, "MIPS")
}

// BenchmarkStore measures memory store performance.
// Tests the TLB and memory access path for stores.
//
// Program:
//
//	    lui x2, 0x80001         # x2 = 0x80001000 (data address)
//	loop:
//	    sw x3, 0(x2)            # store word to memory
//	    jal x0, loop            # jump back
//
// Reference: Tests memory access path in cpu/mmu.go StoreU32
func BenchmarkStore(b *testing.B) {
	cpu := benchCPU(b)

	// lui x2, 0x80001 -> 0x80001137 (x2 = 0x80001000)
	// sw x3, 0(x2) -> 0x00312023
	// jal x0, -4 -> 0xffdff06f
	writeInsn(cpu, 0x80000000, 0x80001137) // lui x2, 0x80001
	writeInsn(cpu, 0x80000004, 0x00312023) // sw x3, 0(x2)
	writeInsn(cpu, 0x80000008, 0xffdff06f) // jal x0, -4

	cpu.SetReg(3, 0xCAFEBABE)

	// Execute LUI once to set up x2
	if err := cpu.Step(); err != nil {
		b.Fatalf("Step failed: %v", err)
	}
	cpu.InsnCounter = 0

	b.ResetTimer()

	if err := cpu.Run(b.N); err != nil {
		b.Fatalf("Run failed: %v", err)
	}

	b.StopTimer()

	insns := cpu.InsnCounter
	b.ReportMetric(float64(insns)/b.Elapsed().Seconds()/1e6, "MIPS")
}

// BenchmarkBranch measures branch instruction performance.
// Tests the branch prediction / execution path.
//
// Program:
//
//	loop:
//	    addi x1, x1, 1          # counter++
//	    beq x0, x0, loop        # always branch (unconditional via beq)
//
// Reference: Tests branch handling in cpu/exec.go executeBranch
func BenchmarkBranch(b *testing.B) {
	cpu := benchCPU(b)

	// addi x1, x1, 1 -> 0x00108093
	// beq x0, x0, -4 -> 0xfe000ee3
	writeInsn(cpu, 0x80000000, 0x00108093) // addi x1, x1, 1
	writeInsn(cpu, 0x80000004, 0xfe000ee3) // beq x0, x0, -4

	cpu.InsnCounter = 0

	b.ResetTimer()

	if err := cpu.Run(b.N); err != nil {
		b.Fatalf("Run failed: %v", err)
	}

	b.StopTimer()

	insns := cpu.InsnCounter
	b.ReportMetric(float64(insns)/b.Elapsed().Seconds()/1e6, "MIPS")
}

// BenchmarkCompressed measures compressed (RVC) instruction performance.
// Tests the compressed instruction decoding and expansion.
//
// Program:
//
//	loop:
//	    c.addi x1, 1            # x1 = x1 + 1 (compressed)
//	    c.j loop                # jump back (compressed)
//
// Reference: Tests compressed instruction decoding in cpu/compressed.go
func BenchmarkCompressed(b *testing.B) {
	cpu := benchCPU(b)

	// c.addi x1, 1 -> 0x0085 (2 bytes)
	// c.j -2 -> 0xbffd (2 bytes) - offset is -2 to jump back to c.addi
	cpu.Mem.Write16(0x80000000, 0x0085) // c.addi x1, 1
	cpu.Mem.Write16(0x80000002, 0xbffd) // c.j -2

	cpu.InsnCounter = 0

	b.ResetTimer()

	if err := cpu.Run(b.N); err != nil {
		b.Fatalf("Run failed: %v", err)
	}

	b.StopTimer()

	insns := cpu.InsnCounter
	b.ReportMetric(float64(insns)/b.Elapsed().Seconds()/1e6, "MIPS")
}

// BenchmarkMul measures multiply instruction performance.
// Tests M extension multiply operations.
//
// Program:
//
//	loop:
//	    mul x3, x1, x2          # x3 = x1 * x2
//	    jal x0, loop            # jump back
//
// Reference: Tests M extension in cpu/exec.go executeMulDiv
func BenchmarkMul(b *testing.B) {
	cpu := benchCPU(b)

	// mul x3, x1, x2 -> 0x022081b3
	// jal x0, -4 -> 0xffdff06f
	writeInsn(cpu, 0x80000000, 0x022081b3) // mul x3, x1, x2
	writeInsn(cpu, 0x80000004, 0xffdff06f) // jal x0, -4

	cpu.SetReg(1, 12345)
	cpu.SetReg(2, 67890)
	cpu.InsnCounter = 0

	b.ResetTimer()

	if err := cpu.Run(b.N); err != nil {
		b.Fatalf("Run failed: %v", err)
	}

	b.StopTimer()

	insns := cpu.InsnCounter
	b.ReportMetric(float64(insns)/b.Elapsed().Seconds()/1e6, "MIPS")
}

// BenchmarkDiv measures divide instruction performance.
// Tests M extension divide operations (typically slower than multiply).
//
// Program:
//
//	loop:
//	    div x3, x1, x2          # x3 = x1 / x2
//	    jal x0, loop            # jump back
//
// Reference: Tests M extension in cpu/exec.go executeMulDiv
func BenchmarkDiv(b *testing.B) {
	cpu := benchCPU(b)

	// div x3, x1, x2 -> 0x022041b3
	// jal x0, -4 -> 0xffdff06f
	writeInsn(cpu, 0x80000000, 0x022041b3) // div x3, x1, x2
	writeInsn(cpu, 0x80000004, 0xffdff06f) // jal x0, -4

	cpu.SetReg(1, 1000000)
	cpu.SetReg(2, 7)
	cpu.InsnCounter = 0

	b.ResetTimer()

	if err := cpu.Run(b.N); err != nil {
		b.Fatalf("Run failed: %v", err)
	}

	b.StopTimer()

	insns := cpu.InsnCounter
	b.ReportMetric(float64(insns)/b.Elapsed().Seconds()/1e6, "MIPS")
}

// BenchmarkCSR measures CSR instruction performance.
// Tests the CSR read/write path.
//
// Program:
//
//	loop:
//	    csrr x1, cycle          # read cycle CSR
//	    jal x0, loop            # jump back
//
// Reference: Tests CSR handling in cpu/csr.go
func BenchmarkCSR(b *testing.B) {
	cpu := benchCPU(b)

	// csrrs x1, cycle, x0 -> 0xc0002073 (csrr x1, cycle)
	// jal x0, -4 -> 0xffdff06f
	writeInsn(cpu, 0x80000000, 0xc00020f3) // csrr x1, cycle
	writeInsn(cpu, 0x80000004, 0xffdff06f) // jal x0, -4

	cpu.InsnCounter = 0

	b.ResetTimer()

	if err := cpu.Run(b.N); err != nil {
		b.Fatalf("Run failed: %v", err)
	}

	b.StopTimer()

	insns := cpu.InsnCounter
	b.ReportMetric(float64(insns)/b.Elapsed().Seconds()/1e6, "MIPS")
}
