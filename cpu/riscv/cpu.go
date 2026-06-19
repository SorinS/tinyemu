// Package riscv is the RISC-V CPU core — the original heart of tinyemu-go.
//
// Heritage: this package (and the surrounding RISC-V machine/device stack) began
// as a Claude-generated transliteration of Fabrice Bellard's TinyEMU from C to
// Go, directed by JT Olio (github.com/jtolio). The module was later forked to
// github.com/sorins/tinyemu-go, which added the x86, x86_64 and ARM64 cores; the
// RISC-V core here is inherited from that original jtolio work. See the README
// for the project's genesis.
package riscv

import (
	"github.com/sorins/tinyemu-go/mem"
)

// XLEN represents the register width
type XLEN int

const (
	XLEN32  XLEN = 32
	XLEN64  XLEN = 64
	XLEN128 XLEN = 128
)

// CPU represents the RISC-V CPU state.
// This structure contains all state needed for CPU emulation including
// registers, CSRs, TLB, and references to memory.
type CPU struct {
	// Program counter
	PC uint64

	// Integer registers (x0 is hardwired to 0, but we store it for simplicity)
	Reg [32]uint64

	// Floating-point registers (64-bit, NaN-boxed for F32)
	FPReg [32]uint64

	// Floating-point control
	FFlags uint32 // Accrued exception flags (NV, DZ, OF, UF, NX)
	FRM    uint8  // Floating-point rounding mode

	// Current XLEN (32, 64, or 128)
	CurXLEN XLEN
	// Maximum XLEN supported
	MaxXLEN XLEN

	// Privilege level (PRV_U, PRV_S, PRV_M)
	Priv uint8

	// Floating-point status (FS field from mstatus, cached for fast access)
	FS uint8

	// MXL field from MISA (indicates max XLEN)
	MXL uint8

	// Execution state
	NCycles       int32  // Cycles remaining in current timeslice
	InsnCounter   uint64 // Total instructions executed
	PowerDownFlag bool   // CPU is in WFI (wait for interrupt)

	// Exception handling state
	PendingException int    // -1 if no pending exception, otherwise exception cause
	PendingTval      uint64 // Trap value for pending exception

	// Machine-mode CSRs
	Mstatus    uint64 // Machine status
	Mtvec      uint64 // Machine trap vector base
	Mscratch   uint64 // Machine scratch register
	Mepc       uint64 // Machine exception program counter
	Mcause     uint64 // Machine trap cause
	Mtval      uint64 // Machine trap value
	Mhartid    uint64 // Machine hardware thread ID (read-only)
	Misa       uint64 // Machine ISA register
	Mie        uint32 // Machine interrupt enable
	Mip        uint32 // Machine interrupt pending
	Medeleg    uint32 // Machine exception delegation
	Mideleg    uint32 // Machine interrupt delegation
	Mcounteren uint32 // Machine counter enable

	// Supervisor-mode CSRs
	Stvec      uint64 // Supervisor trap vector base
	Sscratch   uint64 // Supervisor scratch register
	Sepc       uint64 // Supervisor exception program counter
	Scause     uint64 // Supervisor trap cause
	Stval      uint64 // Supervisor trap value
	Satp       uint64 // Supervisor address translation and protection
	Scounteren uint32 // Supervisor counter enable

	// Atomic LR/SC reservation
	LoadReservation      uint64 // Address of load reservation
	LoadReservationValid bool   // Whether reservation is valid

	// Memory map
	Mem *mem.PhysMemoryMap

	// TLB entries (indexed by (vaddr >> PageShift) & (TLBSize-1))
	TLBRead  [TLBSize]TLBEntry
	TLBWrite [TLBSize]TLBEntry
	TLBCode  [TLBSize]TLBEntry

	// Tracer for debugging (optional, nil if disabled)
	// Reference: riscv_cpu.c lines 43-48 (DUMP_* macros)
	Tracer Tracer

	// TimeSource provides the current time for the TIME CSR.
	// If nil, instruction counter is used (deterministic mode).
	// If set, real-time clock is used (matches CLINT mtime in real-time mode).
	TimeSource TimeSource
}

// TimeSource provides the current time value for the TIME CSR.
// This should return the same value as CLINT's mtime to ensure consistency.
type TimeSource interface {
	GetRTCTime() uint64
}

// TLBEntry represents a single TLB entry for fast virtual-to-physical translation.
type TLBEntry struct {
	VAddr     uint64 // Virtual address (page-aligned)
	MemAddend int64  // Addend to convert virtual address to pointer offset
	// When valid: physical_ptr = memory_base + (vaddr + MemAddend)
}

// NewCPU creates a new RISC-V CPU with the specified memory map and XLEN.
// Reference: tinyemu-2019-12-21/riscv_cpu.c:1289-1321 (riscv_cpu_init)
func NewCPU(memMap *mem.PhysMemoryMap, maxXLEN XLEN) *CPU {
	cpu := &CPU{
		Mem:              memMap,
		MaxXLEN:          maxXLEN,
		CurXLEN:          maxXLEN,
		PC:               0x1000,      // Reference: riscv_cpu.c:1300 - s->pc = 0x1000
		Priv:             PrivMachine, // Start in machine mode
		PendingException: -1,          // No pending exception
	}

	// Initialize MISA based on XLEN
	// Extensions supported: IMAFDC + SU (supervisor/user mode)
	extensions := uint64(MisaI | MisaM | MisaA | MisaF | MisaD | MisaC | MisaS | MisaU)
	switch maxXLEN {
	case XLEN32:
		cpu.MXL = MxlRV32
		cpu.Misa = (uint64(MxlRV32) << 30) | extensions
	case XLEN64:
		cpu.MXL = MxlRV64
		// For RV64, MXL is in bits [63:62]
		cpu.Misa = (uint64(MxlRV64) << 62) | extensions
	case XLEN128:
		cpu.MXL = MxlRV128
		// For RV128, MXL is in bits [127:126] but we only support 64-bit storage
		cpu.Misa = (uint64(MxlRV128) << 62) | extensions
	}

	// Initialize mstatus with reasonable defaults
	// UXL and SXL inherit from MXL
	if maxXLEN == XLEN64 {
		cpu.Mstatus = (uint64(cpu.MXL) << MstatusUXLShift) | (uint64(cpu.MXL) << MstatusSXLShift)
	}

	// Set up TLB flush callback on memory map
	if memMap != nil {
		memMap.SetTLBFlushFunc(cpu, func(ramAddr []byte, ramSize uint64) {
			cpu.FlushTLBWriteRange(ramAddr, ramSize)
		})
	}

	return cpu
}

// Reset resets the CPU to initial state.
func (c *CPU) Reset() {
	// Clear registers
	for i := range c.Reg {
		c.Reg[i] = 0
	}
	for i := range c.FPReg {
		c.FPReg[i] = 0
	}

	// Reset to machine mode
	c.PC = 0
	c.Priv = PrivMachine
	c.PendingException = -1
	c.PowerDownFlag = false
	c.InsnCounter = 0

	// Clear TLBs
	c.FlushTLB()

	// Reset CSRs to default values
	c.Mstatus = 0
	if c.MaxXLEN == XLEN64 {
		c.Mstatus = (uint64(c.MXL) << MstatusUXLShift) | (uint64(c.MXL) << MstatusSXLShift)
	}
	c.Mtvec = 0
	c.Mscratch = 0
	c.Mepc = 0
	c.Mcause = 0
	c.Mtval = 0
	c.Mie = 0
	c.Mip = 0
	c.Medeleg = 0
	c.Mideleg = 0
	c.Mcounteren = 0

	c.Stvec = 0
	c.Sscratch = 0
	c.Sepc = 0
	c.Scause = 0
	c.Stval = 0
	c.Satp = 0
	c.Scounteren = 0

	c.FFlags = 0
	c.FRM = 0
	c.FS = FSOff

	c.LoadReservationValid = false
}

// GetCycles returns the total number of instructions executed.
// Reference: tinyemu-2019-12-21/riscv_cpu.c:1261-1264 (riscv_cpu_get_cycles)
func (c *CPU) GetCycles() uint64 {
	return c.InsnCounter
}

// SetMIP sets bits in the machine interrupt pending register.
// If the CPU is in power-down mode (WFI) and an interrupt is now pending and
// enabled, it wakes the CPU.
// Reference: riscv_cpu.c lines 1266-1272
func (c *CPU) SetMIP(mask uint32) {
	c.Mip |= mask
	// Exit from power down if an interrupt is pending
	if c.PowerDownFlag && (c.Mip&c.Mie) != 0 {
		c.PowerDownFlag = false
	}
}

// ResetMIP clears bits in the machine interrupt pending register.
// Reference: tinyemu-2019-12-21/riscv_cpu.c:1274-1277 (riscv_cpu_reset_mip)
func (c *CPU) ResetMIP(mask uint32) {
	c.Mip &^= mask
}

// GetMIP returns the machine interrupt pending register.
// Reference: tinyemu-2019-12-21/riscv_cpu.c:1279-1282 (riscv_cpu_get_mip)
func (c *CPU) GetMIP() uint32 {
	return c.Mip
}

// GetMISA returns the machine ISA register.
// Reference: tinyemu-2019-12-21/riscv_cpu.c:1330-1333 (riscv_cpu_get_misa)
func (c *CPU) GetMISA() uint64 {
	return c.Misa
}

// IsPowerDown returns true if the CPU is in power-down mode (WFI).
// Reference: tinyemu-2019-12-21/riscv_cpu.c:1284-1287 (riscv_cpu_get_power_down)
func (c *CPU) IsPowerDown() bool {
	return c.PowerDownFlag
}

// SetPowerDown sets the power-down flag.
func (c *CPU) SetPowerDown(flag bool) {
	c.PowerDownFlag = flag
}

// HasPendingInterrupt reports whether any enabled interrupt is pending.
// This is used by the generic emulation loop to decide whether to wake
// the CPU from a power-down (WFI) state.
func (c *CPU) HasPendingInterrupt() bool {
	return (c.Mip & c.Mie) != 0
}

// GetPriv returns the current privilege level.
func (c *CPU) GetPriv() uint8 {
	return c.Priv
}

// SetPriv sets the current privilege level and updates CurXLEN accordingly.
// Reference: tinyemu-2019-12-21/riscv_cpu.c:1021-1040 (set_priv)
func (c *CPU) SetPriv(priv uint8) {
	if priv != c.Priv {
		c.FlushTLB() // Privilege change requires TLB flush

		// Update CurXLEN based on target privilege level
		// Reference: riscv_cpu.c:1025-1036
		if c.MaxXLEN >= XLEN64 {
			var mxl uint8
			switch priv {
			case PrivSupervisor:
				mxl = uint8((c.Mstatus >> MstatusSXLShift) & 3)
			case PrivUser:
				mxl = uint8((c.Mstatus >> MstatusUXLShift) & 3)
			default: // PrivMachine
				mxl = c.MXL
			}
			// CurXLEN = 2^(mxl + 4) = 1 << (mxl + 4)
			if mxl >= 1 && mxl <= 3 {
				c.CurXLEN = 1 << (mxl + 4)
			}
		}

		c.Priv = priv
	}
}

// GetReg returns the value of integer register r.
// Register x0 always returns 0.
func (c *CPU) GetReg(r int) uint64 {
	if r == 0 {
		return 0
	}
	return c.signExtendReg(c.Reg[r])
}

// SetReg sets the value of integer register r.
// Writes to x0 are ignored.
func (c *CPU) SetReg(r int, val uint64) {
	if r != 0 {
		c.Reg[r] = c.signExtendReg(val)
	}
}

// signExtendReg sign-extends a value to the current XLEN.
func (c *CPU) signExtendReg(val uint64) uint64 {
	switch c.CurXLEN {
	case XLEN32:
		return uint64(int64(int32(val)))
	default:
		return val
	}
}

// GetFPReg returns the value of floating-point register r.
func (c *CPU) GetFPReg(r int) uint64 {
	return c.FPReg[r]
}

// SetFPReg sets the value of floating-point register r.
func (c *CPU) SetFPReg(r int, val uint64) {
	c.FPReg[r] = val
	c.FS = FSDirty // Mark FP state as dirty
}

// GetFPRegF32 returns the F32 value from a floating-point register.
// If the register is not properly NaN-boxed, returns the canonical NaN.
func (c *CPU) GetFPRegF32(r int) uint32 {
	val := c.FPReg[r]
	// Check NaN-boxing: upper 32 bits must be all 1s
	if val>>32 != 0xFFFFFFFF {
		return 0x7FC00000 // Canonical quiet NaN
	}
	return uint32(val)
}

// SetFPRegF32 sets an F32 value in a floating-point register with NaN-boxing.
func (c *CPU) SetFPRegF32(r int, val uint32) {
	c.FPReg[r] = 0xFFFFFFFF00000000 | uint64(val)
	c.FS = FSDirty
}

// FlushTLB flushes all TLB entries.
// Reference: tinyemu-2019-12-21/riscv_cpu.c:587-601 (tlb_init, tlb_flush_all)
func (c *CPU) FlushTLB() {
	for i := range c.TLBRead {
		c.TLBRead[i].VAddr = ^uint64(0) // Invalid entry
	}
	for i := range c.TLBWrite {
		c.TLBWrite[i].VAddr = ^uint64(0)
	}
	for i := range c.TLBCode {
		c.TLBCode[i].VAddr = ^uint64(0)
	}
}

// FlushTLBEntry flushes TLB entries for a specific virtual address.
// Reference: tinyemu-2019-12-21/riscv_cpu.c:603-606 (tlb_flush_vaddr)
// Note: C implementation flushes all entries; Go optimizes by only flushing one index.
func (c *CPU) FlushTLBEntry(vaddr uint64) {
	idx := (vaddr >> PageShift) & (TLBSize - 1)
	c.TLBRead[idx].VAddr = ^uint64(0)
	c.TLBWrite[idx].VAddr = ^uint64(0)
	c.TLBCode[idx].VAddr = ^uint64(0)
}

// FlushTLBWriteRange flushes TLB write entries for a range of RAM addresses.
// This is called when dirty bits are cleared to ensure TLB consistency.
// Reference: tinyemu-2019-12-21/riscv_cpu.c:609-626 (glue(riscv_cpu_flush_tlb_write_range_ram))
// Note: C checks each TLB entry against RAM range; Go simplifies by flushing all write entries.
func (c *CPU) FlushTLBWriteRange(ramAddr []byte, ramSize uint64) {
	// For simplicity, we flush all write TLB entries
	// A more sophisticated implementation could track RAM addresses
	for i := range c.TLBWrite {
		c.TLBWrite[i].VAddr = ^uint64(0)
	}
}

// HasPendingException returns true if there is a pending exception.
func (c *CPU) HasPendingException() bool {
	return c.PendingException >= 0
}

// ClearPendingException clears any pending exception.
func (c *CPU) ClearPendingException() {
	c.PendingException = -1
}

// SetPendingException sets a pending exception with the given cause and trap value.
func (c *CPU) SetPendingException(cause int, tval uint64) {
	c.PendingException = cause
	c.PendingTval = tval
}

// ClearLoadReservation clears the load reservation (used for SC failure).
func (c *CPU) ClearLoadReservation() {
	c.LoadReservationValid = false
}

// SetLoadReservation sets a load reservation for the given address.
func (c *CPU) SetLoadReservation(addr uint64) {
	c.LoadReservation = addr
	c.LoadReservationValid = true
}

// CheckLoadReservation checks if there is a valid reservation for the address.
func (c *CPU) CheckLoadReservation(addr uint64) bool {
	return c.LoadReservationValid && c.LoadReservation == addr
}

// UpdateMstatus updates specific fields in mstatus.
func (c *CPU) UpdateMstatus(mask, value uint64) {
	c.Mstatus = (c.Mstatus &^ mask) | (value & mask)
	// Update cached FS
	c.FS = uint8((c.Mstatus >> MstatusFSShift) & 3)
}

// GetEffectivePriv returns the effective privilege level for memory access.
// This handles MPRV (Modify Privilege) in mstatus.
func (c *CPU) GetEffectivePriv(isLoad bool) uint8 {
	if isLoad && (c.Mstatus&MstatusMPRV) != 0 {
		return uint8((c.Mstatus >> MstatusMPPShift) & 3)
	}
	return c.Priv
}

// HasExtension checks if the CPU has a specific extension enabled.
func (c *CPU) HasExtension(ext uint64) bool {
	return (c.Misa & ext) != 0
}

// SupportsFloat returns true if floating-point is supported.
func (c *CPU) SupportsFloat() bool {
	return c.HasExtension(MisaF)
}

// SupportsDouble returns true if double-precision FP is supported.
func (c *CPU) SupportsDouble() bool {
	return c.HasExtension(MisaD)
}

// SupportsCompressed returns true if compressed instructions are supported.
func (c *CPU) SupportsCompressed() bool {
	return c.HasExtension(MisaC)
}

// SupportsAtomic returns true if atomic instructions are supported.
func (c *CPU) SupportsAtomic() bool {
	return c.HasExtension(MisaA)
}

// SupportsMulDiv returns true if multiply/divide is supported.
func (c *CPU) SupportsMulDiv() bool {
	return c.HasExtension(MisaM)
}

// SetTracer sets the tracer for debug output.
// Reference: riscv_cpu.c lines 43-48 (DUMP_* macros)
func (c *CPU) SetTracer(t Tracer) {
	c.Tracer = t
}
