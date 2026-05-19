// Package x86_64 implements a long-mode (AMD64/Intel 64) x86 CPU emulator
// core. It is a superset of the i386 emulator in cpu/x86: the same CPU
// boots from the real-mode reset vector through protected mode and on
// into long mode. The 16/32-bit paths mirror cpu/x86 closely; the
// long-mode-specific work (REX decode, 4-level paging, 64-bit IDT/IRETQ,
// SYSCALL/SYSRET, FSGSBASE, SWAPGS) is new.
//
// Reusable shared semantics (segment descriptor parsing, EFLAGS/CR bit
// constants, SSE/MMX state) are duplicated here for now. Once the
// long-mode side stabilises, the natural shared code will be extracted
// into a cpu/x86common package and both backends will import it.
package x86_64

import (
	"github.com/jtolio/tinyemu-go/mem"
)

// Register indices for 64-bit access. Indices 0..7 match the i386
// EAX/ECX/.../EDI numbering so the cpu.X86Core interface (used by
// machine/pc) can address the low halves uniformly across both
// backends. Indices 8..15 are the long-mode extension registers R8..R15
// reachable only via REX.B / REX.R / REX.X.
const (
	RAX = 0
	RCX = 1
	RDX = 2
	RBX = 3
	RSP = 4
	RBP = 5
	RSI = 6
	RDI = 7
	R8  = 8
	R9  = 9
	R10 = 10
	R11 = 11
	R12 = 12
	R13 = 13
	R14 = 14
	R15 = 15
)

// 32-bit aliases. SetReg32(EAX, v) writes the low 32 bits of RAX. In
// long mode, 32-bit writes performed by guest code via the decoder zero-
// extend into the upper 32 bits; the SetReg32 API used by machine/pc
// during bring-up also zero-extends so the state is well-defined
// regardless of which mode the boot code last left the register in.
const (
	EAX = RAX
	ECX = RCX
	EDX = RDX
	EBX = RBX
	ESP = RSP
	EBP = RBP
	ESI = RSI
	EDI = RDI
)

// Segment register indices. Same numbering as cpu/x86 so machine/pc can
// program both backends with x86.CS / x86.DS / ... literals.
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

// CR0 bits. Values match cpu/x86.
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

// RFLAGS bits. Layout matches EFLAGS in the low 32 bits; the upper 32
// are reserved on x86_64.
const (
	RFLAGS_CF   = 1 << 0
	RFLAGS_PF   = 1 << 2
	RFLAGS_AF   = 1 << 4
	RFLAGS_ZF   = 1 << 6
	RFLAGS_SF   = 1 << 7
	RFLAGS_TF   = 1 << 8
	RFLAGS_IF   = 1 << 9
	RFLAGS_DF   = 1 << 10
	RFLAGS_OF   = 1 << 11
	RFLAGS_IOPL = 3 << 12
	RFLAGS_NT   = 1 << 14
	RFLAGS_RF   = 1 << 16
	RFLAGS_VM   = 1 << 17
	RFLAGS_AC   = 1 << 18
	RFLAGS_VIF  = 1 << 19
	RFLAGS_VIP  = 1 << 20
	RFLAGS_ID   = 1 << 21

	ValidFlagMask = RFLAGS_ID | RFLAGS_VIP | RFLAGS_VIF | RFLAGS_AC |
		RFLAGS_VM | RFLAGS_RF | RFLAGS_NT | RFLAGS_IOPL | RFLAGS_OF |
		RFLAGS_DF | RFLAGS_IF | RFLAGS_TF | RFLAGS_SF | RFLAGS_ZF |
		RFLAGS_AF | RFLAGS_PF | RFLAGS_CF
)

// EFER bits.
const (
	EFER_SCE = 1 << 0  // SYSCALL/SYSRET enable
	EFER_LME = 1 << 8  // Long Mode Enable (request)
	EFER_LMA = 1 << 10 // Long Mode Active (latched when paging enabled with LME)
	EFER_NXE = 1 << 11 // No-Execute Enable
)

// CPU is the long-mode x86 CPU state and execution core.
type CPU struct {
	reg64 [16]uint64

	seg       [16]uint16
	segBase   [16]uint64
	segLimit  [16]uint32
	segAccess [16]uint32

	cr [8]uint64
	dr [8]uint64

	rflags uint64
	rip    uint64

	cpl int

	a20Mask uint64

	memMap *mem.PhysMemoryMap

	// I/O port handlers, registered by the board. Port space is still
	// 16-bit on x86_64 just like i386 — no widening needed.
	ioRead8Func   func(port uint16) uint8
	ioWrite8Func  func(port uint16, val uint8)
	ioRead16Func  func(port uint16) uint16
	ioWrite16Func func(port uint16, val uint16)
	ioRead32Func  func(port uint16) uint32
	ioWrite32Func func(port uint16, val uint32)

	cycles uint64

	powerDown bool

	intrLineState     int
	interruptsBlocked bool
	exitReason        int
	pendingVector     uint8
	ackInterruptFunc  func() (uint8, bool)

	// EFER and the long-mode-relevant MSRs. Set by guest via WRMSR.
	efer            uint64
	msrFSBase       uint64
	msrGSBase       uint64
	msrKernelGSBase uint64
	msrStar         uint64 // SYSCALL target CS/SS + 32-bit entry
	msrLstar        uint64 // SYSCALL 64-bit RIP target
	msrCstar        uint64 // SYSCALL compatibility-mode RIP target
	msrSFMask       uint64 // SYSCALL RFLAGS clear mask

	// mode tracks the current operating mode. It is derived from the
	// architectural state (CR0.PE, EFLAGS.VM, EFER.LMA, CS.L, CS.D) and
	// recomputed any time those bits change. Held explicitly so hot
	// decode paths can switch on a single field instead of re-evaluating
	// half a dozen bits per instruction.
	mode Mode
}

// NewCPU constructs a fresh long-mode-capable CPU at the reset state
// (real mode, RIP=0xFFF0, CS:base=0xF0000) — identical to what cpu/x86
// produces. The CPU walks through the standard real → protected →
// long-mode bring-up sequence in response to guest writes to CR0/CR4/
// EFER, the same way real hardware does.
func NewCPU(memMap *mem.PhysMemoryMap) *CPU {
	c := &CPU{
		memMap:  memMap,
		a20Mask: 0xFFFFFFFFFFFFFFFF,
	}
	c.Reset()
	return c
}

// Reset returns the CPU to its power-on state.
func (c *CPU) Reset() {
	for i := range c.reg64 {
		c.reg64[i] = 0
	}
	for i := range c.seg {
		c.seg[i] = 0
		c.segBase[i] = 0
		c.segLimit[i] = 0xFFFF
		c.segAccess[i] = 0
	}
	c.seg[CS] = 0xF000
	c.segBase[CS] = 0xF0000
	c.rip = 0xFFF0
	c.rflags = 2

	for i := range c.cr {
		c.cr[i] = 0
	}
	for i := range c.dr {
		c.dr[i] = 0
	}
	c.cr[0] = CR0_ET

	c.cpl = 0
	c.powerDown = false
	c.intrLineState = 0
	c.interruptsBlocked = false
	c.exitReason = 0
	c.pendingVector = 0
	c.ackInterruptFunc = nil

	c.efer = 0
	c.msrFSBase = 0
	c.msrGSBase = 0
	c.msrKernelGSBase = 0
	c.msrStar = 0
	c.msrLstar = 0
	c.msrCstar = 0
	c.msrSFMask = 0

	c.mode = ModeReal16
}

// ===== 64-bit register accessors =====

func (c *CPU) GetReg64(r int) uint64 { return c.reg64[r&0xF] }
func (c *CPU) SetReg64(r int, v uint64) { c.reg64[r&0xF] = v }

// ===== 32-bit register accessors =====

// GetReg32 returns the low 32 bits of the register. The high 32 bits
// are visible only through GetReg64.
func (c *CPU) GetReg32(r int) uint32 {
	return uint32(c.reg64[r&0xF])
}

// SetReg32 zero-extends a 32-bit value into the full 64-bit register
// (matching the long-mode semantics for 32-bit writes from guest
// instructions). On the i386 backend the upper 32 bits don't exist;
// using zero-extend here keeps the interface contract uniform — the
// register's high bits after a SetReg32 call are deterministically zero
// in both backends.
func (c *CPU) SetReg32(r int, v uint32) {
	c.reg64[r&0xF] = uint64(v)
}

// ===== 16-bit register accessors =====

// 16-bit register indices follow the i386 convention: AX=0, CX=2, DX=4,
// BX=6, SP=8, BP=10, SI=12, DI=14. The low bit of the slot picks
// AX vs CX style ordering; the upper bits index reg64.
const (
	AX = 0
	CX = 2
	DX = 4
	BX = 6
	SP = 8
	BP = 10
	SI = 12
	DI = 14
)

func (c *CPU) GetReg16(r int) uint16 {
	return uint16(c.reg64[(r>>1)&0xF])
}

// SetReg16 writes only the low 16 bits, preserving the upper 48 (same as
// real hardware: 16-bit writes do not zero the upper bits).
func (c *CPU) SetReg16(r int, v uint16) {
	idx := (r >> 1) & 0xF
	c.reg64[idx] = (c.reg64[idx] & ^uint64(0xFFFF)) | uint64(v)
}

// ===== 8-bit register accessors (i386 set: AL/CL/DL/BL + AH/CH/DH/BH) =====

const (
	AL = 0
	CL = 4
	DL = 8
	BL = 12
	AH = 1
	CH = 5
	DH = 9
	BH = 13
)

func (c *CPU) GetReg8(r int) uint8 {
	idx := (r >> 2) & 0xF
	shift := uint(r&3) * 8
	return uint8(c.reg64[idx] >> shift)
}

func (c *CPU) SetReg8(r int, v uint8) {
	idx := (r >> 2) & 0xF
	shift := uint(r&3) * 8
	mask := uint64(0xFF) << shift
	c.reg64[idx] = (c.reg64[idx] & ^mask) | (uint64(v) << shift)
}

// ===== Segment accessors =====

func (c *CPU) GetSeg(sel int) uint16     { return c.seg[sel] }
func (c *CPU) SetSeg(sel int, v uint16)  { c.seg[sel] = v }
func (c *CPU) GetSegBase64(sel int) uint64 { return c.segBase[sel] }
func (c *CPU) SetSegBase64(sel int, v uint64) { c.segBase[sel] = v }

// SetSegBase / GetSegBase use a 32-bit value to match cpu.X86Core. The
// 32-bit setter is what machine/pc uses during bring-up; the 64-bit FS/
// GS bases that long mode introduces are written by the guest via WRMSR
// (FS_BASE / GS_BASE MSRs) and stored in segBase[] through that path.
func (c *CPU) SetSegBase(sel int, v uint32) { c.segBase[sel] = uint64(v) }
func (c *CPU) GetSegBase(sel int) uint32    { return uint32(c.segBase[sel]) }

func (c *CPU) GetSegLimit(sel int) uint32     { return c.segLimit[sel] }
func (c *CPU) SetSegLimit(sel int, v uint32)  { c.segLimit[sel] = v }
func (c *CPU) GetSegAccess(sel int) uint32    { return c.segAccess[sel] }
func (c *CPU) SetSegAccess(sel int, v uint32) { c.segAccess[sel] = v }

// ===== Instruction pointer =====

func (c *CPU) GetRIP() uint64     { return c.rip }
func (c *CPU) SetRIP(v uint64)    { c.rip = v }
func (c *CPU) GetEIP() uint32     { return uint32(c.rip) }
func (c *CPU) SetEIP(v uint32)    { c.rip = uint64(v) }

// ===== RFLAGS / CR / EFER =====

func (c *CPU) GetRFLAGS() uint64    { return c.rflags }
func (c *CPU) SetRFLAGS(v uint64)   { c.rflags = v&ValidFlagMask | 2 }
func (c *CPU) GetEFLAGS() uint32    { return uint32(c.rflags) }
func (c *CPU) SetEFLAGS(v uint32)   { c.rflags = uint64(v)&ValidFlagMask | 2 }

func (c *CPU) GetCR64(n int) uint64  { return c.cr[n] }
func (c *CPU) SetCR64(n int, v uint64) { c.cr[n] = v }
func (c *CPU) GetCR(n int) uint32    { return uint32(c.cr[n]) }
func (c *CPU) SetCR(n int, v uint32) { c.cr[n] = uint64(v) }

func (c *CPU) GetEFER() uint64     { return c.efer }
func (c *CPU) SetEFER(v uint64)    { c.efer = v }

// ===== I/O wiring (cpu.X86Core) =====

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

func (c *CPU) SetINTR(level int) { c.intrLineState = level }
func (c *CPU) GetINTR() int      { return c.intrLineState }

func (c *CPU) SetInterruptAckHandler(fn func() (uint8, bool)) {
	c.ackInterruptFunc = fn
}

// ===== cpu.Core interface =====

func (c *CPU) GetCycles() uint64    { return c.cycles }
func (c *CPU) AddCycles(n uint64)   { c.cycles += n }
func (c *CPU) SetPowerDown(b bool)  { c.powerDown = b }
func (c *CPU) IsPowerDown() bool    { return c.powerDown }

func (c *CPU) HasPendingInterrupt() bool {
	return c.intrLineState != 0 && (c.rflags&RFLAGS_IF) != 0
}

// IsProtectedMode reports whether CR0.PE is set.
func (c *CPU) IsProtectedMode() bool { return c.cr[0]&CR0_PE != 0 }

// IsLongMode reports whether EFER.LMA is set (paging-enabled long mode).
func (c *CPU) IsLongMode() bool { return c.efer&EFER_LMA != 0 }
