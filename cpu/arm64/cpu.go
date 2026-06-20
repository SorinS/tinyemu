// Package arm64 is an AArch64 (ARM64) execution core for the integer ISA that
// asm/arm64 assembles. It runs in a flat 64-bit address space (no MMU yet) and
// is validated against native Apple Silicon hardware via a clang-compiled
// oracle (oracle_test.go) — the run-and-check half of the assembler's
// byte-exact differential.
package arm64

import (
	"math/bits"

	"github.com/sorins/tinyemu-go/mem"
)

// CPU is the AArch64 machine state.
type CPU struct {
	X  [31]uint64 // X0..X30 (X30 = LR)
	SP uint64     // stack pointer (the "register 31" that means SP)
	PC uint64

	// NZCV condition flags.
	N, Z, C, V bool

	// SIMD&FP register file V0–V31 (128-bit each: [0]=low 64, [1]=high 64).
	// Scalar S/D writes zero the unused high bits, like a W write zeros an X.
	Vreg  [32][2]uint64
	FPCR  uint64
	FPSR  uint64

	// Exception/privilege state. EL is the current exception level (0 or 1),
	// SPSel selects SP_EL0 (0) vs SP_ELx (1), DAIF holds the interrupt masks
	// (bits 3..0 = D,A,I,F). SP is the live stack pointer for the active bank;
	// the inactive bank is parked in SP_EL0/SP_EL1.
	EL    uint8
	SPSel uint8
	DAIF  uint8
	SPEL0 uint64
	SPEL1 uint64

	// EL1 exception registers.
	SPSR uint64 // SPSR_EL1
	ELR  uint64 // ELR_EL1
	ESR  uint64 // ESR_EL1
	VBAR uint64 // VBAR_EL1

	Mem *mem.PhysMemoryMap

	// Sys holds writable system registers other than NZCV, keyed by their
	// encoded field bits (so msr/mrs round-trip). NZCV lives in N/Z/C/V.
	Sys map[uint32]uint64

	// MMU control registers (stage-1, EL1). Populated by msr; read by the
	// translation walk (mmu.go). SCTLR.M (bit 0) enables translation.
	SCTLR uint64
	TTBR0 uint64
	TTBR1 uint64
	TCR   uint64
	MAIR  uint64

	// Fault state from the last translation abort (carries type + faulting
	// address so VBAR_EL1 exception delivery can slot in later).
	FAR       uint64
	FaultKind string

	tlb     [tlbSets][tlbWays]tlbEntry // set-associative VA→PA translation cache
	tlbNext [tlbSets]uint8             // per-set round-robin replacement pointer

	Halted   bool   // an exception (svc/brk/hlt) or a real halt stopped the core
	ExcType  string // "svc"/"hvc"/"smc"/"brk"/"hlt" when Halted by an exception
	ExcImm   uint16 // the exception's immediate
	Sentinel uint64

	// Machine-mode plumbing (driven by a board: GIC, generic timer, PSCI).
	cycles    uint64 // retired-instruction count; also the generic-timer counter
	irqLine   bool   // external IRQ asserted (by the GIC)
	fiqLine   bool   // external FIQ asserted
	powerDown bool   // parked in WFI/WFE until an interrupt

	// Generic timer (EL1 physical + virtual). The counter is `cycles`; a timer
	// fires when enabled, unmasked, and counter >= CVAL. CNTFRQ is advertised to
	// the guest. TimerIRQ is invoked (by the board's CheckTimer) to drive the
	// timer PPI level into the GIC.
	cntfrq   uint64
	cntpCtl  uint64 // CNTP_CTL_EL0: bit0 ENABLE, bit1 IMASK, bit2 ISTATUS
	cntpCval uint64
	cntvCtl  uint64 // CNTV_CTL_EL0
	cntvCval uint64
	cntvOff  uint64 // CNTVOFF (0 here; virtual==physical)

	// HVCHandler, if set, services an `hvc` as a hypercall (PSCI) instead of
	// vectoring to EL1: it reads x0=function-id/args and writes the result to x0,
	// returning true when it handled the call.
	HVCHandler func(c *CPU) bool

	// TimerSync, if set, re-samples the generic-timer outputs into the interrupt
	// controller. The CPU calls it when the guest writes a timer register so a
	// just-serviced timer interrupt deasserts immediately (see writeSysreg).
	TimerSync func()

	// Local exclusive monitor (ldxr/stxr). Single-core: a stxr succeeds while
	// the monitor is set for its address; an exception clears it so a preempted
	// ldxr/stxr sequence retries.
	exclMonitor bool
	exclAddr    uint64
	irqCount    uint64
}

// New creates a CPU over the given physical memory, in the EL1h reset state.
func New(m *mem.PhysMemoryMap) *CPU {
	return &CPU{Mem: m, Sys: map[uint32]uint64{}, EL: 1, SPSel: 1, DAIF: 0xF}
}

// Reset clears architectural state.
func (c *CPU) Reset() {
	c.X = [31]uint64{}
	c.SP, c.PC = 0, 0
	c.N, c.Z, c.C, c.V = false, false, false, false
	c.Sys = map[uint32]uint64{}
	c.Halted, c.ExcType, c.ExcImm = false, "", 0
	c.SCTLR, c.TTBR0, c.TTBR1, c.TCR, c.MAIR = 0, 0, 0, 0, 0
	c.FAR, c.FaultKind = 0, ""
	c.flushTLB()
	// Reset enters EL1h (SPSel=1) with interrupts masked — the state a bare
	// program / kernel starts in.
	c.EL, c.SPSel, c.DAIF = 1, 1, 0xF
	c.SPEL0, c.SPEL1 = 0, 0
	c.SPSR, c.ELR, c.ESR, c.VBAR = 0, 0, 0, 0
	c.cycles, c.irqLine, c.fiqLine, c.powerDown = 0, false, false, false
	c.cntpCtl, c.cntpCval, c.cntvCtl, c.cntvCval, c.cntvOff = 0, 0, 0, 0, 0
}

// Encoded sysreg fields (bits 19:5) for the registers given dedicated state.
const (
	a64NZCVField  uint32 = 1<<19 | 3<<16 | 4<<12 | 2<<8        // NZCV   S3_3_C4_C2_0
	a64DAIFField  uint32 = 1<<19 | 3<<16 | 4<<12 | 2<<8 | 1<<5 // DAIF   S3_3_C4_C2_1
	a64SCTLRField uint32 = 1<<19 | 1<<12                // SCTLR_EL1 S3_0_C1_C0_0
	a64TTBR0Field uint32 = 1<<19 | 2<<12                // TTBR0_EL1 S3_0_C2_C0_0
	a64TTBR1Field uint32 = 1<<19 | 2<<12 | 1<<5         // TTBR1_EL1 S3_0_C2_C0_1
	a64TCRField   uint32 = 1<<19 | 2<<12 | 2<<5         // TCR_EL1   S3_0_C2_C0_2
	a64MAIRField  uint32 = 1<<19 | 10<<12 | 2<<8        // MAIR_EL1  S3_0_C10_C2_0

	a64SPSRField   uint32 = 1<<19 | 4<<12               // SPSR_EL1  S3_0_C4_C0_0
	a64ELRField    uint32 = 1<<19 | 4<<12 | 1<<5        // ELR_EL1   S3_0_C4_C0_1
	a64SPEL0Field  uint32 = 1<<19 | 4<<12 | 1<<8        // SP_EL0    S3_0_C4_C1_0
	a64SPSelField  uint32 = 1<<19 | 4<<12 | 2<<8        // SPSel     S3_0_C4_C2_0
	a64CurELField  uint32 = 1<<19 | 4<<12 | 2<<8 | 2<<5 // CurrentEL S3_0_C4_C2_2
	a64ESRField    uint32 = 1<<19 | 5<<12 | 2<<8        // ESR_EL1   S3_0_C5_C2_0
	a64FARField    uint32 = 1<<19 | 6<<12               // FAR_EL1   S3_0_C6_C0_0
	a64VBARField   uint32 = 1<<19 | 12<<12              // VBAR_EL1  S3_0_C12_C0_0
	a64DCZIDField  uint32 = 1<<19 | 3<<16 | 7<<5        // DCZID_EL0 S3_3_C0_C0_7
)

// readSysreg returns a system register's value. NZCV comes from the flags; the
// MMU registers from their dedicated fields; everything else from the Sys map
// (unknown reads as 0).
func (c *CPU) readSysreg(field uint32) uint64 {
	switch field {
	case a64NZCVField:
		return c.nzcv()
	case a64DAIFField:
		return uint64(c.DAIF) << 6 // PSTATE.DAIF lives in bits 9:6
	case a64SCTLRField:
		return c.SCTLR
	case a64TTBR0Field:
		return c.TTBR0
	case a64TTBR1Field:
		return c.TTBR1
	case a64TCRField:
		return c.TCR
	case a64MAIRField:
		return c.MAIR
	case a64SPSRField:
		return c.SPSR
	case a64ELRField:
		return c.ELR
	case a64ESRField:
		return c.ESR
	case a64FARField:
		return c.FAR
	case a64VBARField:
		return c.VBAR
	case a64SPEL0Field:
		return c.SPEL0
	case a64SPSelField:
		return uint64(c.SPSel)
	case a64CurELField:
		return uint64(c.EL) << 2
	case a64DCZIDField:
		return 4 // BS=4 (DC ZVA block = 64 bytes), DZP=0 (ZVA permitted)
	}
	if v, ok := c.readTimerReg(field); ok {
		return v
	}
	return c.Sys[field]
}

// writeSysreg writes a system register, routing NZCV and the MMU registers to
// their dedicated state and everything else to the Sys map.
func (c *CPU) writeSysreg(field uint32, v uint64) {
	switch field {
	case a64NZCVField:
		c.setFlags(v>>31&1 == 1, v>>30&1 == 1, v>>29&1 == 1, v>>28&1 == 1)
	case a64DAIFField:
		c.DAIF = uint8(v>>6) & 0xF // msr daif, Xt (local_irq_restore)
	case a64SCTLRField:
		// Only an MMU-enable transition (M bit) invalidates translations; cache/
		// alignment/WXN bit changes do not. edk2 toggles cache bits mid-bring-up
		// while relying on existing TLB entries (break-before-make), so a blanket
		// flush here would drop entries it still needs.
		if (c.SCTLR^v)&1 != 0 {
			c.flushTLB()
		}
		c.SCTLR = v
	case a64TTBR0Field:
		c.TTBR0 = v
		c.flushTLB() // an address-space change; we don't tag TLB entries by ASID
	case a64TTBR1Field:
		c.TTBR1 = v
		c.flushTLB()
	case a64TCRField:
		c.TCR = v
		c.flushTLB()
	case a64MAIRField:
		c.MAIR = v
	case a64SPSRField:
		c.SPSR = v
	case a64ELRField:
		c.ELR = v
	case a64ESRField:
		c.ESR = v
	case a64FARField:
		c.FAR = v
	case a64VBARField:
		c.VBAR = v
	case a64SPEL0Field:
		c.SPEL0 = v
	case a64SPSelField:
		c.switchEL(c.EL, uint8(v&1)) // banking: re-select the active SP
	default:
		if c.writeTimerReg(field, v) {
			// The guest just (re)programmed a generic-timer register — e.g. an
			// interrupt handler pushing CVAL forward to clear the current tick.
			// Re-sample the timer→GIC line now, not at the next board CheckTimer,
			// or the still-asserted PPI re-delivers the same IRQ in a loop.
			if c.TimerSync != nil {
				c.TimerSync()
			}
			return
		}
		c.Sys[field] = v
	}
}

// nzcv packs the flags into the architectural NZCV position (bits 31..28), the
// layout MRS/MSR uses and the native oracle reads back.
func (c *CPU) nzcv() uint64 {
	var v uint64
	if c.N {
		v |= 1 << 31
	}
	if c.Z {
		v |= 1 << 30
	}
	if c.C {
		v |= 1 << 29
	}
	if c.V {
		v |= 1 << 28
	}
	return v
}

// NZCV returns the condition flags packed into bits 31..28 (the MRS layout).
func (c *CPU) NZCV() uint64 { return c.nzcv() }

// readX reads register n as a width-bit value. sp selects whether field 31 is
// the stack pointer (true) or the zero register (false).
func (c *CPU) readX(n uint32, is64, sp bool) uint64 {
	var v uint64
	switch {
	case n != 31:
		v = c.X[n]
	case sp:
		v = c.SP
	default:
		v = 0 // XZR
	}
	if !is64 {
		v &= 0xFFFFFFFF
	}
	return v
}

// writeX writes register n. A 32-bit (W) write zeros the upper 32 bits of the
// X register. Field 31 is the stack pointer when sp is set, else the zero
// register (write discarded).
func (c *CPU) writeX(n uint32, is64, sp bool, v uint64) {
	if !is64 {
		v &= 0xFFFFFFFF
	}
	switch {
	case n != 31:
		c.X[n] = v
	case sp:
		c.SP = v
	}
}

// addWithCarry computes a + b + carryIn at the given width and returns the
// result together with the four NZCV flags. ARM's subtraction is add-with-carry
// of the inverted operand (a - b = a + ~b + 1), and its C flag is the carry-out
// — i.e. C=1 means "no borrow", NOT a borrow. This is the single most error-
// prone flag rule, so it lives in one place.
func addWithCarry(a, b uint64, carryIn uint64, is64 bool) (res uint64, n, z, c, v bool) {
	if is64 {
		sum, carry := bits.Add64(a, b, carryIn)
		res = sum
		c = carry == 1
		n = res>>63 != 0
		z = res == 0
		// Signed overflow: a and b same sign, result differs from both.
		v = ((a^res)&(b^res))>>63 != 0
		return
	}
	a &= 0xFFFFFFFF
	b &= 0xFFFFFFFF
	full := a + b + carryIn
	res = full & 0xFFFFFFFF
	c = full>>32 != 0
	n = res>>31&1 != 0
	z = res == 0
	v = ((a^res)&(b^res))>>31&1 != 0
	return
}

// setFlags stores N/Z/C/V.
func (c *CPU) setFlags(n, z, cf, v bool) { c.N, c.Z, c.C, c.V = n, z, cf, v }

// condHolds evaluates a 4-bit AArch64 condition against the current flags.
func (c *CPU) condHolds(cond uint32) bool {
	switch cond >> 1 {
	case 0b000: // eq/ne
		return c.Z == (cond&1 == 0)
	case 0b001: // cs/cc (hs/lo)
		return c.C == (cond&1 == 0)
	case 0b010: // mi/pl
		return c.N == (cond&1 == 0)
	case 0b011: // vs/vc
		return c.V == (cond&1 == 0)
	case 0b100: // hi/ls
		return (c.C && !c.Z) == (cond&1 == 0)
	case 0b101: // ge/lt
		return (c.N == c.V) == (cond&1 == 0)
	case 0b110: // gt/le
		return (c.N == c.V && !c.Z) == (cond&1 == 0)
	default: // 0b111: al/nv — always
		return true
	}
}
