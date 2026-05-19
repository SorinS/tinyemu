package cpu

// Core is the generic CPU emulator interface implemented by all
// architecture-specific CPU backends (RISC-V, x86, etc.).
type Core interface {
	Run(cycles int) error
	Step() error
	Reset()
	GetCycles() uint64
	SetPowerDown(bool)
	IsPowerDown() bool
	HasPendingInterrupt() bool
}

// X86Core is the surface that the PC machine board uses to drive an x86
// CPU. It is implemented by both the i386 emulator (cpu/x86) and the
// long-mode emulator (cpu/x86_64). Method signatures match the i386
// emulator since machine/pc bring-up only programs the CPU through 32-bit
// protected mode — long-mode-only state (FS/GS 64-bit bases, EFER, IDTR
// extended fields) is set by the guest at runtime via MSR writes, not by
// the board.
//
// Register and segment indices passed to SetReg32 / SetSeg* use the same
// numbering on both backends (EAX==0, CS==1, etc.); the constants live
// in cpu/x86 today and are referenced from machine/pc through that
// package.
type X86Core interface {
	Core

	AddCycles(uint64)
	SetINTR(level int)
	GetINTR() int

	SetIOHandlers(
		read8 func(port uint16) uint8,
		write8 func(port uint16, val uint8),
		read16 func(port uint16) uint16,
		write16 func(port uint16, val uint16),
		read32 func(port uint16) uint32,
		write32 func(port uint16, val uint32),
	)
	SetInterruptAckHandler(fn func() (uint8, bool))

	SetSeg(sel int, v uint16)
	SetSegBase(sel int, v uint32)
	SetSegLimit(sel int, v uint32)
	SetSegAccess(sel int, v uint32)
	SetEIP(v uint32)
	SetReg32(r int, v uint32)
	SetCR(n int, v uint32)
	GetCR(n int) uint32
}
