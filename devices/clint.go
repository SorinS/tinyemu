// Package devices provides hardware device emulation for the TinyEMU RISC-V emulator.
package devices

import (
	"sync"

	"github.com/jtolio/tinyemu-go/mem"
)

// CLINT memory-mapped register offsets per RISC-V spec
const (
	CLINTMsipOffset     = 0x0000 // Machine Software Interrupt Pending
	CLINTMtimecmpOffset = 0x4000 // Machine Timer Compare
	CLINTMtimeOffset    = 0xBFF8 // Machine Timer

	CLINTBaseAddr = 0x02000000
	CLINTSize     = 0x000C0000
)

// InterruptController interface for setting/clearing CPU interrupt bits
type InterruptController interface {
	SetMIP(mask uint32)
	ResetMIP(mask uint32)
	GetCycles() uint64 // Get CPU instruction counter for mtime calculation
}

// TimeSource provides the current RTC time.
// This allows the CLINT to use wall-clock time (for WFI support) or
// instruction-counter-based time (for deterministic testing).
// Reference: riscv_machine.c lines 90-97 (rtc_get_time)
type TimeSource interface {
	GetRTCTime() uint64
}

// MIP bits for CLINT
const (
	MipMSIP = 1 << 3 // Machine Software Interrupt Pending
	MipMTIP = 1 << 7 // Machine Timer Interrupt Pending
)

// RTCFreqDiv is the divisor to convert CPU cycles to mtime ticks.
// TinyEMU uses 16, meaning mtime = insn_counter / 16.
// This means mtime runs at CPU_freq / 16, which at 10MHz RTC_FREQ
// implies CPU runs at 160MHz equivalent.
const RTCFreqDiv = 16

// CLINT implements the Core Local Interruptor.
// It provides the machine timer (mtime), timer compare (mtimecmp),
// and software interrupt pending (msip) registers.
type CLINT struct {
	mu sync.Mutex

	// Interrupt controller (typically the CPU)
	intCtrl InterruptController

	// Time source for mtime reads (optional, falls back to intCtrl.GetCycles if nil)
	// Reference: riscv_machine.c lines 90-97 (rtc_get_time)
	timeSource TimeSource

	// Machine timer compare register
	mtimecmp uint64

	// Machine software interrupt pending (per-hart, but we only support 1 hart)
	msip uint32

	// Timer frequency in Hz (typically 10MHz for RISC-V) - used for FDT
	timerFreq uint64
}

// NewCLINT creates a new CLINT device.
func NewCLINT(intCtrl InterruptController) *CLINT {
	return &CLINT{
		intCtrl:   intCtrl,
		mtimecmp:  0xFFFFFFFFFFFFFFFF, // Max value = timer interrupt disabled
		timerFreq: 10_000_000,         // 10 MHz default (for FDT)
	}
}

// SetTimerFrequency sets the timer frequency in Hz.
func (c *CLINT) SetTimerFrequency(freq uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.timerFreq = freq
}

// SetTimeSource sets a custom time source for mtime reads.
// This allows using wall-clock time (for WFI support) instead of
// instruction-counter-based time (which doesn't advance during WFI).
// Reference: riscv_machine.c lines 90-97 (rtc_get_time)
func (c *CLINT) SetTimeSource(ts TimeSource) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.timeSource = ts
}

// GetMtime returns the current machine timer value.
func (c *CLINT) GetMtime() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.getMtimeUnlocked()
}

// getMtimeUnlocked returns the current mtime value.
// If a TimeSource is set (real-time mode), it uses that.
// Otherwise, falls back to instruction counter: mtime = insn_counter / RTCFreqDiv
// Reference: riscv_machine.c lines 90-97 (rtc_get_time)
func (c *CLINT) getMtimeUnlocked() uint64 {
	// Real-time mode: use wall-clock time (allows timer to fire during WFI)
	if c.timeSource != nil {
		return c.timeSource.GetRTCTime()
	}
	// Instruction-counter mode: deterministic but doesn't advance during WFI
	if c.intCtrl == nil {
		return 0
	}
	return c.intCtrl.GetCycles() / RTCFreqDiv
}

// SetMtimecmp sets the machine timer compare register.
func (c *CLINT) SetMtimecmp(val uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.mtimecmp = val
	// Writing mtimecmp clears the timer interrupt pending bit
	if c.intCtrl != nil {
		c.intCtrl.ResetMIP(MipMTIP)
	}
}

// GetMtimecmp returns the machine timer compare register.
func (c *CLINT) GetMtimecmp() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.mtimecmp
}

// SetMsip sets the machine software interrupt pending bit.
func (c *CLINT) SetMsip(val uint32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.msip = val & 1
	if c.intCtrl != nil {
		if c.msip != 0 {
			c.intCtrl.SetMIP(MipMSIP)
		} else {
			c.intCtrl.ResetMIP(MipMSIP)
		}
	}
}

// GetMsip returns the machine software interrupt pending bit.
func (c *CLINT) GetMsip() uint32 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.msip
}

// CheckTimer checks if the timer interrupt should be raised.
// This should be called periodically (e.g., in the CPU execution loop).
func (c *CLINT) CheckTimer() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.intCtrl != nil && c.getMtimeUnlocked() >= c.mtimecmp {
		c.intCtrl.SetMIP(MipMTIP)
	}
}

// Read handles CLINT register reads.
// This implements mem.DeviceReadFunc.
// Reference: riscv_machine.c:195-219 (clint_read)
func (c *CLINT) Read(opaque any, offset uint32, sizeLog2 int) uint32 {
	c.mu.Lock()
	defer c.mu.Unlock()

	switch offset {
	case CLINTMsipOffset:
		return c.msip

	case CLINTMtimecmpOffset:
		return uint32(c.mtimecmp)
	case CLINTMtimecmpOffset + 4:
		return uint32(c.mtimecmp >> 32)

	case CLINTMtimeOffset:
		return uint32(c.getMtimeUnlocked())
	case CLINTMtimeOffset + 4:
		return uint32(c.getMtimeUnlocked() >> 32)

	default:
		return 0
	}
}

// Write handles CLINT register writes.
// This implements mem.DeviceWriteFunc.
// Reference: riscv_machine.c:221-239 (clint_write)
func (c *CLINT) Write(opaque any, offset uint32, val uint32, sizeLog2 int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	switch offset {
	case CLINTMsipOffset:
		c.msip = val & 1
		if c.intCtrl != nil {
			if c.msip != 0 {
				c.intCtrl.SetMIP(MipMSIP)
			} else {
				c.intCtrl.ResetMIP(MipMSIP)
			}
		}

	case CLINTMtimecmpOffset:
		c.mtimecmp = (c.mtimecmp & 0xFFFFFFFF00000000) | uint64(val)
		// Writing mtimecmp clears MTIP
		if c.intCtrl != nil {
			c.intCtrl.ResetMIP(MipMTIP)
		}
	case CLINTMtimecmpOffset + 4:
		c.mtimecmp = (c.mtimecmp & 0x00000000FFFFFFFF) | (uint64(val) << 32)
		// Writing mtimecmp clears MTIP
		if c.intCtrl != nil {
			c.intCtrl.ResetMIP(MipMTIP)
		}

	// mtime is typically read-only, but some implementations allow writes
	case CLINTMtimeOffset:
		// Reset time base to produce the desired mtime value
		// For simplicity, we don't implement mtime writes
	case CLINTMtimeOffset + 4:
		// For simplicity, we don't implement mtime writes
	}
}

// Register registers the CLINT with a memory map at the default address.
func (c *CLINT) Register(memMap *mem.PhysMemoryMap) (*mem.PhysMemoryRange, error) {
	return c.RegisterAt(memMap, CLINTBaseAddr)
}

// RegisterAt registers the CLINT with a memory map at a custom address.
func (c *CLINT) RegisterAt(memMap *mem.PhysMemoryMap, addr uint64) (*mem.PhysMemoryRange, error) {
	return memMap.RegisterDevice(addr, CLINTSize, c, c.Read, c.Write, mem.DevIOSize32)
}
