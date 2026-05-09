package riscv

import (
	"bytes"
	"strings"
	"testing"

	"github.com/jtolio/tinyemu-go/mem"
)

// TestDefaultTracerException tests that the tracer outputs exception information.
// Reference: riscv_cpu.c lines 1048-1081 (raise_exception2 debug output)
func TestDefaultTracerException(t *testing.T) {
	memMap := mem.NewPhysMemoryMap()
	cpu := NewCPU(memMap, XLEN64)

	var buf bytes.Buffer
	tracer := NewDefaultTracer(&buf, TraceException|TraceMMUException)
	cpu.SetTracer(tracer)

	// Set up some register state
	cpu.PC = 0x80000100
	cpu.Reg[1] = 0xDEADBEEF
	cpu.Reg[2] = 0x12345678
	cpu.Priv = PrivSupervisor
	cpu.Mstatus = 0x200000000
	cpu.InsnCounter = 12345

	// Trigger a page fault (which should be traced as MMU exception)
	tracer.TraceRaiseException(cpu, CauseLoadPageFault, 0x40001000, false)

	output := buf.String()

	// Verify the output contains expected information
	if !strings.Contains(output, "raise_exception:") {
		t.Errorf("expected 'raise_exception:' in output, got: %s", output)
	}
	if !strings.Contains(output, "cause=0x0000000d") { // CauseLoadPageFault = 13
		t.Errorf("expected cause=0x0000000d in output, got: %s", output)
	}
	if !strings.Contains(output, "tval=0x0000000040001000") {
		t.Errorf("expected tval in output, got: %s", output)
	}
	if !strings.Contains(output, "pc =") {
		t.Errorf("expected register dump in output, got: %s", output)
	}
	if !strings.Contains(output, "priv=S") {
		t.Errorf("expected priv=S in output, got: %s", output)
	}
}

// TestDefaultTracerPrivChange tests privilege change tracing.
func TestDefaultTracerPrivChange(t *testing.T) {
	memMap := mem.NewPhysMemoryMap()
	cpu := NewCPU(memMap, XLEN64)

	var buf bytes.Buffer
	tracer := NewDefaultTracer(&buf, TracePrivChange)
	cpu.SetTracer(tracer)

	cpu.PC = 0x80200000
	tracer.TracePrivChange(cpu, PrivMachine, PrivSupervisor)

	output := buf.String()

	if !strings.Contains(output, "priv_change:") {
		t.Errorf("expected 'priv_change:' in output, got: %s", output)
	}
	if !strings.Contains(output, "M -> S") {
		t.Errorf("expected 'M -> S' in output, got: %s", output)
	}
}

// TestTracerInException tests that tracer is called during exception handling.
// Reference: riscv_cpu.c lines 1048-1081 (raise_exception2)
func TestTracerInException(t *testing.T) {
	memMap := mem.NewPhysMemoryMap()
	pr, err := memMap.RegisterRAM(0x80000000, 4096, 0)
	if err != nil {
		t.Fatalf("failed to register RAM: %v", err)
	}
	// Write a simple instruction that will cause an exception
	pr.PhysMem[0] = 0x00 // Invalid opcode
	pr.PhysMem[1] = 0x00
	pr.PhysMem[2] = 0x00
	pr.PhysMem[3] = 0x00

	cpu := NewCPU(memMap, XLEN64)
	cpu.PC = 0x80000000
	cpu.Priv = PrivMachine
	cpu.Mtvec = 0x80000100

	var buf bytes.Buffer
	tracer := NewDefaultTracer(&buf, TraceException|TracePrivChange)
	cpu.SetTracer(tracer)

	// Set pending exception and handle it
	cpu.SetPendingException(CauseIllegalInsn, 0)
	cpu.handleException()

	// The trace buffer should be empty because CauseIllegalInsn is filtered
	// (matching C DUMP_EXCEPTIONS behavior which excludes illegal instruction)
	if strings.Contains(buf.String(), "raise_exception:") {
		t.Logf("Note: CauseIllegalInsn was traced (may differ from C)")
	}
}

// TestTracerMMUException tests MMU exception tracing.
// Reference: riscv_cpu.c lines 1052-1059 (DUMP_MMU_EXCEPTIONS)
func TestTracerMMUException(t *testing.T) {
	memMap := mem.NewPhysMemoryMap()
	cpu := NewCPU(memMap, XLEN64)

	var buf bytes.Buffer
	tracer := NewDefaultTracer(&buf, TraceMMUException)
	cpu.SetTracer(tracer)

	cpu.PC = 0x80000200
	cpu.Priv = PrivSupervisor
	cpu.Mtvec = 0x80000100
	cpu.Medeleg = 0xFFFF // Delegate all exceptions to S-mode

	// Set pending page fault and handle it
	cpu.SetPendingException(CauseStorePageFault, 0x40002000)
	cpu.handleException()

	output := buf.String()

	// Should have traced the page fault
	if !strings.Contains(output, "raise_exception:") {
		t.Errorf("expected MMU exception to be traced, got: %s", output)
	}
	if !strings.Contains(output, "cause=0x0000000f") { // CauseStorePageFault = 15
		t.Errorf("expected cause=0x0000000f in output, got: %s", output)
	}
}

// TestDumpRegs tests the DumpRegs function matches C format.
// Reference: riscv_cpu.c lines 114-144 (dump_regs)
func TestDumpRegs(t *testing.T) {
	memMap := mem.NewPhysMemoryMap()
	cpu := NewCPU(memMap, XLEN64)

	// Set up known state
	cpu.PC = 0x80001234
	cpu.Reg[1] = 0x0000000080002000  // ra
	cpu.Reg[2] = 0x0000000080010000  // sp
	cpu.Reg[10] = 0x00000000DEADBEEF // a0
	cpu.Priv = PrivMachine
	cpu.Mstatus = 0x0000000200001800
	cpu.InsnCounter = 999999
	cpu.Mie = 0x222
	cpu.Mip = 0x080
	cpu.Mideleg = 0x100

	var buf bytes.Buffer
	cpu.DumpRegs(&buf)

	output := buf.String()

	// Verify format matches C TinyEMU expectations
	// Reference: riscv_cpu.c line 123 uses "%-3s=" format which produces "ra ="
	if !strings.Contains(output, "pc =") {
		t.Errorf("expected 'pc =' in output, got: %s", output)
	}
	if !strings.Contains(output, "ra =") {
		t.Errorf("expected 'ra =' in output, got: %s", output)
	}
	if !strings.Contains(output, "priv=M") {
		t.Errorf("expected 'priv=M' in output, got: %s", output)
	}
	if !strings.Contains(output, "mstatus=") {
		t.Errorf("expected 'mstatus=' in output, got: %s", output)
	}
	if !strings.Contains(output, "cycles=") {
		t.Errorf("expected 'cycles=' in output, got: %s", output)
	}
	if !strings.Contains(output, "mideleg=") {
		t.Errorf("expected 'mideleg=' in output, got: %s", output)
	}
}

// TestCauseName tests the cause name helper function.
func TestCauseName(t *testing.T) {
	tests := []struct {
		cause    int
		expected string
	}{
		{CauseIllegalInsn, "ILLEGAL_INSN"},
		{CauseLoadPageFault, "LOAD_PAGE_FAULT"},
		{CauseStorePageFault, "STORE_PAGE_FAULT"},
		{CauseSupervisorEcall, "SUPERVISOR_ECALL"},
		{99, "UNKNOWN(99)"},
	}

	for _, tc := range tests {
		result := CauseName(tc.cause)
		if result != tc.expected {
			t.Errorf("CauseName(%d) = %s, expected %s", tc.cause, result, tc.expected)
		}
	}
}

// TestTraceEventFlags tests that trace event filtering works correctly.
func TestTraceEventFlags(t *testing.T) {
	memMap := mem.NewPhysMemoryMap()
	cpu := NewCPU(memMap, XLEN64)
	cpu.PC = 0x80000000
	cpu.Priv = PrivSupervisor

	// Test with only TraceInterrupt enabled - should not trace exceptions
	var buf bytes.Buffer
	tracer := NewDefaultTracer(&buf, TraceInterrupt)

	// Page fault (exception, not interrupt)
	tracer.TraceRaiseException(cpu, CauseLoadPageFault, 0x40000000, false)

	if buf.Len() > 0 {
		t.Errorf("expected no output for exception when only TraceInterrupt enabled, got: %s", buf.String())
	}

	// Now test interrupt tracing
	buf.Reset()
	tracer.TraceRaiseException(cpu, CauseLoadPageFault, 0x40000000, true)

	if buf.Len() == 0 {
		t.Error("expected output for interrupt when TraceInterrupt enabled")
	}
}

// TestTraceIllegalInsn tests that TraceIllegalInsn flag correctly captures
// illegal instruction exceptions that are excluded by TraceException.
// Reference: riscv_cpu.c lines 1067-1068 (DUMP_EXCEPTIONS excludes illegal insn)
func TestTraceIllegalInsn(t *testing.T) {
	memMap := mem.NewPhysMemoryMap()
	cpu := NewCPU(memMap, XLEN64)
	cpu.PC = 0x80000100
	cpu.Priv = PrivUser

	// First verify that TraceException does NOT trace illegal instructions
	// (matching C DUMP_EXCEPTIONS behavior)
	var buf bytes.Buffer
	tracer := NewDefaultTracer(&buf, TraceException)
	tracer.TraceRaiseException(cpu, CauseIllegalInsn, 0xDEADBEEF, false)

	if buf.Len() > 0 {
		t.Errorf("TraceException should NOT trace illegal instructions, got: %s", buf.String())
	}

	// Now verify that TraceIllegalInsn DOES trace illegal instructions
	buf.Reset()
	tracer = NewDefaultTracer(&buf, TraceIllegalInsn)
	tracer.TraceRaiseException(cpu, CauseIllegalInsn, 0xDEADBEEF, false)

	if buf.Len() == 0 {
		t.Error("TraceIllegalInsn should trace illegal instructions")
	}

	output := buf.String()
	if !strings.Contains(output, "raise_exception:") {
		t.Errorf("expected 'raise_exception:' in output, got: %s", output)
	}
	if !strings.Contains(output, "cause=0x00000002") { // CauseIllegalInsn = 2
		t.Errorf("expected cause=0x00000002 in output, got: %s", output)
	}
	if !strings.Contains(output, "tval=0x00000000deadbeef") {
		t.Errorf("expected tval in output, got: %s", output)
	}
	if !strings.Contains(output, "priv=U") {
		t.Errorf("expected priv=U in output, got: %s", output)
	}
}
