// Package devices provides hardware device emulation for the TinyEMU RISC-V emulator.
package devices

import (
	"math/bits"
	"sync"

	"github.com/jtolio/tinyemu-go/mem"
)

// PLIC memory-mapped register offsets and addresses per RISC-V spec
const (
	// PLIC address space layout
	PLICBaseAddr = 0x40100000
	PLICSize     = 0x00400000

	// Hart-specific registers base offset (context 0)
	PLICHartBase = 0x200000
	PLICHartSize = 0x1000

	// Register offsets within hart context
	PLICThresholdOffset = 0x0 // Priority threshold
	PLICClaimOffset     = 0x4 // Claim/Complete register

	// Maximum number of interrupt sources (0 is reserved)
	PLICMaxIRQ = 32
)

// MIP bits for external interrupts
const (
	MipMEIP = 1 << 11 // Machine External Interrupt Pending
	MipSEIP = 1 << 9  // Supervisor External Interrupt Pending
)

// DebugPLIC enables debug logging for PLIC operations.
var DebugPLIC bool

// PLIC implements the Platform-Level Interrupt Controller.
// It manages external interrupt routing from peripherals to the CPU.
//
// The PLIC supports up to 31 interrupt sources (IRQ 1-31, IRQ 0 is reserved).
// Each source can have a priority, and the PLIC routes the highest-priority
// pending interrupt to the CPU. The CPU can claim an interrupt, service it,
// and then complete it.
type PLIC struct {
	mu sync.Mutex

	// Interrupt controller (typically the CPU)
	intCtrl InterruptController

	// Pending interrupts bitmap (bits 1-31, bit 0 unused)
	// This is set when an IRQ line goes high and cleared on claim.
	pendingIRQ uint32

	// Currently served interrupts bitmap
	servedIRQ uint32

	// Current IRQ line levels (bits 1-31, bit 0 unused)
	// This tracks the actual state of each IRQ line for level-triggered behavior.
	// When an interrupt is completed, if the line is still high, pending is re-set.
	irqLevel uint32

	// Interrupt priorities (index 1-31 used, 0 reserved)
	priority [PLICMaxIRQ]uint32

	// Interrupt enable bits per context (we only support context 0)
	enable uint32
}

// NewPLIC creates a new PLIC device.
func NewPLIC(intCtrl InterruptController) *PLIC {
	return &PLIC{
		intCtrl: intCtrl,
	}
}

// CreateIRQs creates IRQ signals for all interrupt sources.
// Returns an array of 32 IRQ signals (index 0 is nil, indices 1-31 are valid).
// Reference: riscv_machine.c lines 876-878
func (p *PLIC) CreateIRQs() [PLICMaxIRQ]*mem.IRQSignal {
	var irqs [PLICMaxIRQ]*mem.IRQSignal
	for i := 1; i < PLICMaxIRQ; i++ {
		irqs[i] = mem.NewIRQSignal(p.setIRQ, p, i)
	}
	return irqs
}

// setIRQ is called when an interrupt source changes state.
// Reference: riscv_machine.c lines 303-314 (plic_set_irq)
//
// For level-triggered interrupts, we track both the IRQ line level and the
// pending bit. The pending bit is set on rising edge and cleared on claim.
// The level is used to re-assert pending when an interrupt is completed
// while the line is still high.
func (p *PLIC) setIRQ(opaque any, irqNum int, level int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if irqNum < 1 || irqNum >= PLICMaxIRQ {
		return
	}

	mask := uint32(1) << (irqNum - 1)

	// Track the actual IRQ line level
	if level != 0 {
		p.irqLevel |= mask
		// Set pending on rising edge (level-triggered with edge detection)
		p.pendingIRQ |= mask
	} else {
		p.irqLevel &^= mask
		// For level-triggered, we clear pending when line goes low
		// This matches C TinyEMU behavior
		p.pendingIRQ &^= mask
	}

	if DebugPLIC {
		println("[PLIC] setIRQ:", irqNum, "level:", level, "pending:", p.pendingIRQ, "served:", p.servedIRQ, "irqLevel:", p.irqLevel)
	}

	p.updateMIPUnlocked()
}

// SetIRQ sets or clears an interrupt line.
// irqNum should be 1-31 (0 is reserved).
// level 0 = inactive, non-zero = active.
func (p *PLIC) SetIRQ(irqNum int, level int) {
	p.setIRQ(p, irqNum, level)
}

// updateMIPUnlocked updates the CPU's MIP register based on pending/served state.
// Must be called with p.mu held.
// Reference: riscv_machine.c lines 241-251 (plic_update_mip)
func (p *PLIC) updateMIPUnlocked() {
	if p.intCtrl == nil {
		return
	}

	// Check if there are any pending interrupts that aren't being served
	mask := p.pendingIRQ &^ p.servedIRQ
	if mask != 0 {
		// There are pending unserved interrupts - signal to CPU
		p.intCtrl.SetMIP(MipMEIP | MipSEIP)
	} else {
		// No pending unserved interrupts - clear external interrupt pending
		p.intCtrl.ResetMIP(MipMEIP | MipSEIP)
	}
}

// Read handles PLIC register reads.
// This implements mem.DeviceReadFunc.
// Reference: riscv_machine.c:256-282 (plic_read)
func (p *PLIC) Read(opaque any, offset uint32, sizeLog2 int) uint32 {
	p.mu.Lock()
	defer p.mu.Unlock()

	switch offset {
	case PLICHartBase + PLICThresholdOffset:
		// C code returns 0 for threshold read at PLIC_HART_BASE
		// Reference: riscv_machine.c:263-265
		return 0

	case PLICHartBase + PLICClaimOffset:
		// Claim - return the highest priority pending interrupt
		mask := p.pendingIRQ &^ p.servedIRQ
		if mask != 0 {
			// Find the lowest numbered pending interrupt
			// Reference: tinyemu-2019-12-21/cutils.h:164-174 (ctz32)
			irqNum := bits.TrailingZeros32(mask)
			// Mark it as being served
			p.servedIRQ |= 1 << irqNum
			p.updateMIPUnlocked()
			// Return IRQ number (1-indexed)
			return uint32(irqNum + 1)
		}
		// No pending interrupts
		return 0

	default:
		// Priority registers (0x0 - 0x7C for sources 1-31)
		if offset >= 0x4 && offset < 0x80 {
			srcNum := offset / 4
			if srcNum < PLICMaxIRQ {
				return p.priority[srcNum]
			}
		}
		// Enable registers (0x2000 for context 0)
		if offset == 0x2000 {
			return p.enable
		}
		// Pending registers (0x1000)
		if offset == 0x1000 {
			return p.pendingIRQ
		}
		return 0
	}
}

// Write handles PLIC register writes.
// This implements mem.DeviceWriteFunc.
// Reference: riscv_machine.c:284-301 (plic_write)
func (p *PLIC) Write(opaque any, offset uint32, val uint32, sizeLog2 int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	switch offset {
	case PLICHartBase + PLICThresholdOffset:
		// C code ignores threshold writes at PLIC_HART_BASE
		// Reference: riscv_machine.c:284-301 (only PLIC_HART_BASE+4 handled)

	case PLICHartBase + PLICClaimOffset:
		// Complete - mark the interrupt as no longer being served
		irqNum := val - 1
		if irqNum < PLICMaxIRQ-1 {
			mask := uint32(1) << irqNum
			p.servedIRQ &^= mask
			// For level-triggered interrupts: if the IRQ line is still high,
			// re-assert the pending bit. This handles the case where new events
			// arrived while the interrupt was being serviced.
			if p.irqLevel&mask != 0 {
				p.pendingIRQ |= mask
			}
			if DebugPLIC {
				println("[PLIC] complete:", irqNum+1, "pending:", p.pendingIRQ, "served:", p.servedIRQ, "irqLevel:", p.irqLevel)
			}
			p.updateMIPUnlocked()
		}

	default:
		// Priority registers (0x4 - 0x7C for sources 1-31)
		if offset >= 0x4 && offset < 0x80 {
			srcNum := offset / 4
			if srcNum < PLICMaxIRQ {
				p.priority[srcNum] = val
			}
		}
		// Enable registers (0x2000 for context 0)
		if offset == 0x2000 {
			p.enable = val
		}
	}
}

// GetPendingIRQ returns the bitmap of pending interrupts.
func (p *PLIC) GetPendingIRQ() uint32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pendingIRQ
}

// GetServedIRQ returns the bitmap of currently served interrupts.
func (p *PLIC) GetServedIRQ() uint32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.servedIRQ
}

// Register registers the PLIC with a memory map at the default address.
func (p *PLIC) Register(memMap *mem.PhysMemoryMap) (*mem.PhysMemoryRange, error) {
	return p.RegisterAt(memMap, PLICBaseAddr)
}

// RegisterAt registers the PLIC with a memory map at a custom address.
func (p *PLIC) RegisterAt(memMap *mem.PhysMemoryMap, addr uint64) (*mem.PhysMemoryRange, error) {
	return memMap.RegisterDevice(addr, PLICSize, p, p.Read, p.Write, mem.DevIOSize32)
}
