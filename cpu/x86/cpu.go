// Package x86 implements an x86-32 CPU emulator core, based on the halfix
// emulator architecture.
package x86

import (
	"github.com/jtolio/tinyemu-go/mem"
)

// Register indices for 32-bit access.
const (
	EAX  = 0
	ECX  = 1
	EDX  = 2
	EBX  = 3
	ESP  = 4
	EBP  = 5
	ESI  = 6
	EDI  = 7
	EZR  = 8 // zero register
	ETMP = 9 // temp register
)

// Register indices for 16-bit access.
const (
	AX  = 0
	CX  = 2
	DX  = 4
	BX  = 6
	SP  = 8
	BP  = 10
	SI  = 12
	DI  = 14
	ZR  = 16 // 16-bit zero
	TMP = 18
)

// Register indices for 8-bit access.
const (
	AL  = 0
	CL  = 4
	DL  = 8
	BL  = 12
	AH  = 1
	CH  = 5
	DH  = 9
	BH  = 13
	ZR8 = 32
)

// Segment register indices.
const (
	ES   = 0
	CS   = 1
	SS   = 2
	DS   = 3
	FS   = 4
	GS   = 5
	TR   = 6
	GDTR = 7
	LDTR = 8
	IDTR = 9
)

// CR0 bits.
const (
	CR0_PE = 1 << 0
	CR0_MP = 1 << 1
	CR0_EM = 1 << 2
	CR0_TS = 1 << 3
	CR0_ET = 1 << 4
	CR0_NE = 1 << 5
	CR0_WP = 1 << 16
	CR0_NW = 1 << 29
	CR0_CD = 1 << 30
	CR0_PG = 1 << 31
)

// CR4 bits.
const (
	CR4_VME        = 1 << 0
	CR4_PVI        = 1 << 1
	CR4_TSD        = 1 << 2
	CR4_DE         = 1 << 3
	CR4_PSE        = 1 << 4
	CR4_PAE        = 1 << 5
	CR4_MCE        = 1 << 6
	CR4_PGE        = 1 << 7
	CR4_PCE        = 1 << 8
	CR4_OSFXSR     = 1 << 9
	CR4_OSXMMEXCPT = 1 << 10
	CR4_UMIP       = 1 << 11
	CR4_LA57       = 1 << 12
	CR4_VMXE       = 1 << 13
	CR4_SMXE       = 1 << 14
	CR4_FSGSBASE   = 1 << 16
	CR4_PCIDE      = 1 << 17
	CR4_OSXSAVE    = 1 << 18
	CR4_SMEP       = 1 << 20
	CR4_SMAP       = 1 << 21
	CR4_PKE        = 1 << 22
)

// EFLAGS bits.
const (
	EFLAGS_CF     = 1 << 0
	EFLAGS_PF     = 1 << 2
	EFLAGS_AF     = 1 << 4
	EFLAGS_ZF     = 1 << 6
	EFLAGS_SF     = 1 << 7
	EFLAGS_TF     = 1 << 8
	EFLAGS_IF     = 1 << 9
	EFLAGS_DF     = 1 << 10
	EFLAGS_OF     = 1 << 11
	EFLAGS_IOPL   = 3 << 12
	EFLAGS_NT     = 1 << 14
	EFLAGS_RF     = 1 << 16
	EFLAGS_VM     = 1 << 17
	EFLAGS_AC     = 1 << 18
	EFLAGS_VIF    = 1 << 19
	EFLAGS_VIP    = 1 << 20
	EFLAGS_ID     = 1 << 21
	ValidFlagMask = EFLAGS_ID | EFLAGS_VIP | EFLAGS_VIF | EFLAGS_AC | EFLAGS_VM |
		EFLAGS_RF | EFLAGS_NT | EFLAGS_IOPL | EFLAGS_OF | EFLAGS_DF |
		EFLAGS_IF | EFLAGS_TF | EFLAGS_SF | EFLAGS_ZF | EFLAGS_AF |
		EFLAGS_PF | EFLAGS_CF
	ArithFlagMask = EFLAGS_OF | EFLAGS_SF | EFLAGS_ZF | EFLAGS_AF | EFLAGS_PF | EFLAGS_CF
)

// CPU state and execution core.
type CPU struct {
	// Registers
	reg32 [16]uint32

	// Segment registers
	seg       [16]uint16
	segBase   [16]uint32
	segLimit  [16]uint32
	segAccess [16]uint32

	// Control and debug registers
	cr [8]uint32
	dr [8]uint32

	// EFLAGS and lazy flag computation
	eflags uint32
	laux   uint32
	lop1   uint32
	lop2   uint32
	lr     uint32

	// Instruction pointer
	eip uint32

	// Current privilege level
	cpl int

	// A20 gate mask
	a20Mask uint32

	// Memory reference
	memMap *mem.PhysMemoryMap

	// I/O port handlers (set by board)
	ioRead8Func   func(port uint16) uint8
	ioWrite8Func  func(port uint16, val uint8)
	ioRead16Func  func(port uint16) uint16
	ioWrite16Func func(port uint16, val uint16)
	ioRead32Func  func(port uint16) uint32
	ioWrite32Func func(port uint16, val uint32)

	// Cycle counting
	cycles uint64

	// Power-down (HLT) state
	powerDown bool

	// Interrupt state
	intrLineState     int
	interruptsBlocked bool
	exitReason        int
	pendingVector     uint8
	ackInterruptFunc  func() (uint8, bool)

	// Current instruction sizes (set during Step/executeOpcode)
	currentAddrSize uint8
	currentOpSize   uint8

	// Model-specific registers (allow-list).
	msrSysenterCS  uint32
	msrSysenterESP uint32
	msrSysenterEIP uint32
	msrFSBase      uint32
	msrGSBase      uint32
	msrKernelGSBase uint32
	msrMiscEnable  uint64
	efer           uint64
	mtrrPhysBase   [16]uint64
	mtrrPhysMask   [16]uint64
	mtrrFixed      [11]uint64
	mtrrDefType    uint64

	// PAE state (set when CR4.PAE && CR0.PG).
	pdpte      [4]uint64
	paeActive  bool
}

// NewCPU creates a new x86 CPU instance.
func NewCPU(memMap *mem.PhysMemoryMap) *CPU {
	c := &CPU{
		memMap:  memMap,
		a20Mask: 0xFFFFFFFF,
	}
	c.Reset()
	return c
}

// Reset initializes the CPU to its power-on state.
func (c *CPU) Reset() {
	for i := range c.reg32 {
		c.reg32[i] = 0
	}
	for i := range c.seg {
		c.seg[i] = 0
		c.segBase[i] = 0
		c.segLimit[i] = 0xFFFF
		c.segAccess[i] = 0
	}
	// Real mode: CS base = 0xFFFF0, IP = 0xFFF0 (reset vector)
	c.seg[CS] = 0xF000
	c.segBase[CS] = 0xF0000
	c.eip = 0xFFF0
	c.eflags = 2 // Bit 1 is always set
	c.cr[0] = 0
	c.cpl = 0
	c.powerDown = false
	c.intrLineState = 0
	c.interruptsBlocked = false
	c.exitReason = 0
	c.pendingVector = 0
	c.ackInterruptFunc = nil
}

// ===== Register accessors =====

func (c *CPU) GetReg32(r int) uint32 {
	if r == EZR {
		return 0
	}
	return c.reg32[r]
}

func (c *CPU) SetReg32(r int, v uint32) {
	if r == EZR {
		return
	}
	c.reg32[r] = v
}

func (c *CPU) GetReg16(r int) uint16 {
	if r == ZR {
		return 0
	}
	return uint16(c.reg32[r>>1])
}

func (c *CPU) SetReg16(r int, v uint16) {
	if r == ZR {
		return
	}
	idx := r >> 1
	c.reg32[idx] = (c.reg32[idx] & ^uint32(0xFFFF)) | uint32(v)
}

func (c *CPU) GetReg8(r int) uint8 {
	if r == ZR8 {
		return 0
	}
	idx := r >> 2
	shift := (r & 3) * 8
	return uint8(c.reg32[idx] >> shift)
}

func (c *CPU) SetReg8(r int, v uint8) {
	if r == ZR8 {
		return
	}
	idx := r >> 2
	shift := (r & 3) * 8
	c.reg32[idx] = (c.reg32[idx] & ^(uint32(0xFF) << shift)) | (uint32(v) << shift)
}

// ===== Segment accessors =====

func (c *CPU) GetSeg(sel int) uint16 {
	return c.seg[sel]
}

func (c *CPU) SetSeg(sel int, v uint16) {
	c.seg[sel] = v
}

func (c *CPU) GetSegBase(sel int) uint32 {
	return c.segBase[sel]
}

func (c *CPU) SetSegBase(sel int, v uint32) {
	c.segBase[sel] = v
}

func (c *CPU) GetSegAccess(sel int) uint32 {
	return c.segAccess[sel]
}

func (c *CPU) SetSegAccess(sel int, v uint32) {
	c.segAccess[sel] = v
}

func (c *CPU) SetSegLimit(sel int, v uint32) {
	c.segLimit[sel] = v
}

func (c *CPU) GetSegLimit(sel int) uint32 {
	return c.segLimit[sel]
}

// ===== EIP accessors =====

func (c *CPU) GetEIP() uint32 {
	return c.eip
}

func (c *CPU) SetEIP(v uint32) {
	c.eip = v
}

// Linear EIP (virtual address for code fetch).
func (c *CPU) GetLIP() uint32 {
	return c.segBase[CS] + c.eip
}

// ===== EFLAGS accessors =====

func (c *CPU) GetEFLAGS() uint32 {
	return c.eflags
}

func (c *CPU) SetEFLAGS(v uint32) {
	c.eflags = v&ValidFlagMask | 2
}

// ===== CR accessors =====

func (c *CPU) GetCR(n int) uint32 {
	return c.cr[n]
}

func (c *CPU) SetCR(n int, v uint32) {
	c.cr[n] = v
}

// SetIOHandlers registers I/O port read/write callbacks.
func (c *CPU) SetIOHandlers(
	read8 func(port uint16) uint8,
	write8 func(port uint16, val uint8),
	read16 func(port uint16) uint16,
	write16 func(port uint16, val uint16),
	read32 func(port uint16) uint32,
	write32 func(port uint16, val uint32),
) {
	c.ioRead8Func = read8
	c.ioWrite8Func = write8
	c.ioRead16Func = read16
	c.ioWrite16Func = write16
	c.ioRead32Func = read32
	c.ioWrite32Func = write32
}

// SetINTR sets or clears the CPU's external interrupt line.
func (c *CPU) SetINTR(level int) {
	c.intrLineState = level
}

// GetINTR returns the current external interrupt line state (independent of
// EFLAGS.IF). Useful for tests that wire PIC -> CPU.
func (c *CPU) GetINTR() int {
	return c.intrLineState
}

// SetInterruptAckHandler registers a callback that the CPU calls when it is
// about to service a hardware interrupt. The callback should acknowledge the
// interrupt with the PIC/APIC and return the vector number.
func (c *CPU) SetInterruptAckHandler(fn func() (uint8, bool)) {
	c.ackInterruptFunc = fn
}

// IsProtectedMode returns true if CR0.PE is set.
func (c *CPU) IsProtectedMode() bool {
	return c.cr[0]&CR0_PE != 0
}

// IsVirtual8086Mode returns true if CR0.PE is set and EFLAGS.VM is set.
func (c *CPU) IsVirtual8086Mode() bool {
	return c.IsProtectedMode() && (c.eflags&EFLAGS_VM) != 0
}

// DefaultOperandSize returns the default operand size (2 or 4) based on mode.
// In real mode: 16-bit. In protected mode: uses CS descriptor D-bit.
func (c *CPU) DefaultOperandSize() uint8 {
	if !c.IsProtectedMode() {
		return 2
	}
	// In protected mode, check CS descriptor D-bit (bit 6 of flags = bit 10 of segAccess)
	if (c.segAccess[CS]>>10)&1 != 0 {
		return 4
	}
	return 2
}

// DefaultAddressSize returns the default address size (2 or 4) based on mode.
// In real mode: 16-bit. In protected mode: uses CS descriptor D-bit.
func (c *CPU) DefaultAddressSize() uint8 {
	if !c.IsProtectedMode() {
		return 2
	}
	if (c.segAccess[CS]>>10)&1 != 0 {
		return 4
	}
	return 2
}

// ===== Core interface implementation =====

func (c *CPU) GetCycles() uint64 {
	return c.cycles
}

func (c *CPU) SetPowerDown(flag bool) {
	c.powerDown = flag
}

func (c *CPU) IsPowerDown() bool {
	return c.powerDown
}

func (c *CPU) HasPendingInterrupt() bool {
	// On x86, this checks the PIC/APIC state. For now, simplified.
	return c.intrLineState != 0 && (c.eflags&EFLAGS_IF) != 0
}

// Run executes up to maxCycles cycles.
func (c *CPU) Run(maxCycles int) error {
	for i := 0; i < maxCycles; i++ {
		if c.powerDown {
			// HLT idle: wake only if an enabled hardware interrupt is
			// pending. Otherwise yield back to the host loop so it can
			// advance the PIT and raise IRQs.
			if !c.HasPendingInterrupt() {
				return nil
			}
			c.powerDown = false
		}
		if err := c.Step(); err != nil {
			return err
		}
		c.cycles++
	}
	return nil
}
