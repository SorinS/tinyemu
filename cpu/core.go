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
