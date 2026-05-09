package riscv

import (
	"fmt"
	"io"
)

// TraceEvent represents a traceable CPU event.
// Reference: riscv_cpu.c lines 43-48 (DUMP_* macros)
type TraceEvent int

const (
	// TraceException traces all exceptions (except ECALL and illegal instruction)
	// Reference: riscv_cpu.c lines 1064-1068 (DUMP_EXCEPTIONS)
	TraceException TraceEvent = 1 << iota

	// TraceInterrupt traces interrupts
	// Reference: riscv_cpu.c lines 1061-1062 (DUMP_INTERRUPTS)
	TraceInterrupt

	// TraceMMUException traces page faults and access faults
	// Reference: riscv_cpu.c lines 1052-1059 (DUMP_MMU_EXCEPTIONS)
	TraceMMUException

	// TraceInvalidCSR traces invalid CSR access
	// Reference: riscv_cpu.c line 846, 1013 (DUMP_INVALID_CSR)
	TraceInvalidCSR

	// TraceCSR traces all CSR read/write operations
	// Reference: riscv_cpu.c line 885 (DUMP_CSR)
	TraceCSR

	// TraceInvalidMem traces invalid memory access
	// Reference: riscv_cpu.c line 377 (DUMP_INVALID_MEM_ACCESS)
	TraceInvalidMem

	// TraceInstruction traces every instruction (very verbose)
	TraceInstruction

	// TracePrivChange traces privilege mode changes
	TracePrivChange

	// TraceIllegalInsn traces illegal instruction exceptions specifically.
	// Note: TraceException excludes illegal instructions to match C TinyEMU's
	// DUMP_EXCEPTIONS behavior. Use this flag when debugging boot failures
	// that involve illegal instructions in userspace.
	// Reference: riscv_cpu.c lines 1067-1068 (exclusion) - this flag reverses that
	TraceIllegalInsn

	// TraceAll enables all tracing
	TraceAll = TraceException | TraceInterrupt | TraceMMUException |
		TraceInvalidCSR | TraceCSR | TraceInvalidMem |
		TraceInstruction | TracePrivChange | TraceIllegalInsn
)

// Tracer is the interface for CPU trace output.
type Tracer interface {
	// TraceRaiseException is called when an exception or interrupt is raised.
	// Reference: riscv_cpu.c lines 1048-1081 (raise_exception2 debug output)
	TraceRaiseException(cpu *CPU, cause int, tval uint64, isInterrupt bool)

	// TraceCSRAccess is called when a CSR is read or written.
	// Reference: riscv_cpu.c line 885 (DUMP_CSR)
	TraceCSRAccess(cpu *CPU, csr uint32, value uint64, isWrite bool)

	// TraceInvalidCSR is called when an invalid CSR is accessed.
	// Reference: riscv_cpu.c line 846, 1013 (DUMP_INVALID_CSR)
	TraceInvalidCSR(cpu *CPU, csr uint32, isWrite bool)

	// TraceInstruction is called before each instruction executes.
	TraceInstruction(cpu *CPU, pc uint64, insn uint32)

	// TracePrivChange is called when privilege mode changes.
	TracePrivChange(cpu *CPU, oldPriv, newPriv uint8)

	// TraceMemAccess is called for memory access (optional, very verbose)
	TraceMemAccess(cpu *CPU, addr uint64, size int, isWrite bool, value uint64)
}

// DefaultTracer implements the Tracer interface with output matching C TinyEMU.
// Reference: riscv_cpu.c lines 114-144 (dump_regs function)
type DefaultTracer struct {
	Writer io.Writer
	Events TraceEvent
}

// NewDefaultTracer creates a tracer that writes to the given writer.
func NewDefaultTracer(w io.Writer, events TraceEvent) *DefaultTracer {
	return &DefaultTracer{
		Writer: w,
		Events: events,
	}
}

// privString returns the privilege character matching C TinyEMU.
// Reference: riscv_cpu.c line 117 (const char priv_str[4] = "USHM")
var privString = [4]byte{'U', 'S', 'H', 'M'}

// TraceRaiseException implements Tracer.
// Reference: riscv_cpu.c lines 1048-1081 (raise_exception2 debug code)
func (t *DefaultTracer) TraceRaiseException(cpu *CPU, cause int, tval uint64, isInterrupt bool) {
	if t.Writer == nil {
		return
	}

	// Check which events to trace, matching C DUMP_* behavior
	shouldTrace := false

	if isInterrupt && (t.Events&TraceInterrupt) != 0 {
		shouldTrace = true
	}

	if !isInterrupt {
		// Check for illegal instruction exceptions specifically
		// Reference: riscv_cpu.c lines 1067-1068 excludes illegal insn from DUMP_EXCEPTIONS
		// TraceIllegalInsn reverses this for diagnostic purposes
		if (t.Events&TraceIllegalInsn) != 0 && cause == CauseIllegalInsn {
			shouldTrace = true
		}

		// Check for MMU exceptions
		// Reference: riscv_cpu.c lines 1053-1059
		if (t.Events & TraceMMUException) != 0 {
			switch cause {
			case CauseFaultFetch, CauseFaultLoad, CauseFaultStore,
				CauseFetchPageFault, CauseLoadPageFault, CauseStorePageFault:
				shouldTrace = true
			}
		}

		// Check for general exceptions (excluding ECALL and illegal insn)
		// Reference: riscv_cpu.c lines 1064-1068
		if (t.Events & TraceException) != 0 {
			if cause != CauseSupervisorEcall && cause != CauseIllegalInsn &&
				cause != CauseMachineEcall && cause != CauseUserEcall {
				shouldTrace = true
			}
		}
	}

	if !shouldTrace {
		return
	}

	// Output format matches C TinyEMU
	// Reference: riscv_cpu.c lines 1071-1078
	fmt.Fprintf(t.Writer, "raise_exception: cause=0x%08x tval=0x%016x\n", cause, tval)
	t.dumpRegs(cpu)
}

// TraceCSRAccess implements Tracer.
func (t *DefaultTracer) TraceCSRAccess(cpu *CPU, csr uint32, value uint64, isWrite bool) {
	if t.Writer == nil || (t.Events&TraceCSR) == 0 {
		return
	}

	op := "read"
	if isWrite {
		op = "write"
	}
	fmt.Fprintf(t.Writer, "csr_%s: csr=0x%03x value=0x%016x\n", op, csr, value)
}

// TraceInvalidCSR implements Tracer.
func (t *DefaultTracer) TraceInvalidCSR(cpu *CPU, csr uint32, isWrite bool) {
	if t.Writer == nil || (t.Events&TraceInvalidCSR) == 0 {
		return
	}

	op := "read"
	if isWrite {
		op = "write"
	}
	fmt.Fprintf(t.Writer, "invalid_csr_%s: csr=0x%03x pc=0x%016x priv=%c\n",
		op, csr, cpu.PC, privString[cpu.Priv])
}

// TraceInstruction implements Tracer.
func (t *DefaultTracer) TraceInstruction(cpu *CPU, pc uint64, insn uint32) {
	if t.Writer == nil || (t.Events&TraceInstruction) == 0 {
		return
	}
	fmt.Fprintf(t.Writer, "insn: pc=0x%016x insn=0x%08x priv=%c\n",
		pc, insn, privString[cpu.Priv])
}

// TracePrivChange implements Tracer.
func (t *DefaultTracer) TracePrivChange(cpu *CPU, oldPriv, newPriv uint8) {
	if t.Writer == nil || (t.Events&TracePrivChange) == 0 {
		return
	}
	fmt.Fprintf(t.Writer, "priv_change: %c -> %c pc=0x%016x\n",
		privString[oldPriv], privString[newPriv], cpu.PC)
}

// TraceMemAccess implements Tracer.
func (t *DefaultTracer) TraceMemAccess(cpu *CPU, addr uint64, size int, isWrite bool, value uint64) {
	if t.Writer == nil || (t.Events&TraceInvalidMem) == 0 {
		return
	}

	op := "load"
	if isWrite {
		op = "store"
	}
	fmt.Fprintf(t.Writer, "mem_%s: addr=0x%016x size=%d value=0x%016x\n",
		op, addr, size, value)
}

// dumpRegs outputs register state matching C TinyEMU format.
// Reference: riscv_cpu.c lines 114-144 (dump_regs function)
func (t *DefaultTracer) dumpRegs(cpu *CPU) {
	// Reference: riscv_cpu.c lines 107-112 (reg_name array)
	regName := [32]string{
		"zero", "ra", "sp", "gp", "tp", "t0", "t1", "t2",
		"s0", "s1", "a0", "a1", "a2", "a3", "a4", "a5",
		"a6", "a7", "s2", "s3", "s4", "s5", "s6", "s7",
		"s8", "s9", "s10", "s11", "t3", "t4", "t5", "t6",
	}

	// Match C format: pc first, then registers in 4-column layout for 64-bit
	// Reference: riscv_cpu.c lines 118-129
	fmt.Fprintf(t.Writer, "pc =%016x ", cpu.PC)
	cols := 4 // For 64-bit XLEN

	for i := 1; i < 32; i++ {
		fmt.Fprintf(t.Writer, "%-3s=%016x", regName[i], cpu.Reg[i])
		if (i & (cols - 1)) == (cols - 1) {
			fmt.Fprintln(t.Writer)
		} else {
			fmt.Fprint(t.Writer, " ")
		}
	}

	// Match C format for privilege and CSRs
	// Reference: riscv_cpu.c lines 130-143
	fmt.Fprintf(t.Writer, "priv=%c", privString[cpu.Priv])
	fmt.Fprintf(t.Writer, " mstatus=%016x", cpu.Mstatus)
	fmt.Fprintf(t.Writer, " cycles=%d\n", cpu.InsnCounter)
	fmt.Fprintf(t.Writer, " mideleg=%016x", uint64(cpu.Mideleg))
	fmt.Fprintf(t.Writer, " mie=%016x", uint64(cpu.Mie))
	fmt.Fprintf(t.Writer, " mip=%016x\n", uint64(cpu.Mip))
}

// DumpRegs outputs the current CPU register state.
// This can be called directly for debugging without a Tracer.
// Reference: riscv_cpu.c lines 114-144 (dump_regs function)
func (c *CPU) DumpRegs(w io.Writer) {
	tracer := &DefaultTracer{Writer: w}
	tracer.dumpRegs(c)
}

// causeName returns a human-readable name for an exception cause.
func causeName(cause int) string {
	names := map[int]string{
		CauseMisalignedFetch: "MISALIGNED_FETCH",
		CauseFaultFetch:      "FAULT_FETCH",
		CauseIllegalInsn:     "ILLEGAL_INSN",
		CauseBreakpoint:      "BREAKPOINT",
		CauseMisalignedLoad:  "MISALIGNED_LOAD",
		CauseFaultLoad:       "FAULT_LOAD",
		CauseMisalignedStore: "MISALIGNED_STORE",
		CauseFaultStore:      "FAULT_STORE",
		CauseUserEcall:       "USER_ECALL",
		CauseSupervisorEcall: "SUPERVISOR_ECALL",
		CauseMachineEcall:    "MACHINE_ECALL",
		CauseFetchPageFault:  "FETCH_PAGE_FAULT",
		CauseLoadPageFault:   "LOAD_PAGE_FAULT",
		CauseStorePageFault:  "STORE_PAGE_FAULT",
	}
	if name, ok := names[cause]; ok {
		return name
	}
	return fmt.Sprintf("UNKNOWN(%d)", cause)
}

// CauseName returns a human-readable name for an exception cause.
func CauseName(cause int) string {
	return causeName(cause)
}
