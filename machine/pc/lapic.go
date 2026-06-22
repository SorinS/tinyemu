package pc

import (
	"fmt"
	"os"

	"github.com/sorins/tinyemu-go/cpu"
)

var lapicDebug = os.Getenv("TINYEMU_LAPIC_DEBUG") == "1"

// LocalAPIC is a minimal xAPIC (MMIO) local-APIC model, enabled only when
// Config.EnableAPIC is set. It exists for firmware (OVMF) and OSes that
// require a real local APIC — the default PC build keeps the 8259 PIC path
// untouched so existing Linux boots (which run with `noapic nolapic`) are
// not affected.
//
// What it models:
//   - The register block at LAPICBase (0xFEE00000): ID/VERSION, SVR (with
//     the software-enable bit), TPR/PPR, EOI, the 256-bit IRR/ISR/TMR
//     bitmaps, the LVT entries, ICR, and the APIC timer.
//   - Interrupt prioritisation + delivery into the CPU via the same
//     SetINTR / ack-handler hook the PIC uses.
//   - The APIC timer (one-shot and periodic), driven off the CPU cycle
//     counter exactly like the 8254 PIT.
//   - ExtINT input from the 8259 PIC (virtual-wire mode): when the PIC has
//     a pending IRQ it asserts the LAPIC's external-interrupt line, and on
//     acknowledge the LAPIC delegates the vector fetch to the PIC. This
//     gives a working interrupt path for legacy device IRQs without an
//     I/O APIC (that comes later).
//
// Not yet modelled: I/O APIC redirection, x2APIC (MSR) mode, multi-CPU
// IPIs (single BSP only — INIT/SIPI are accepted and dropped).

const (
	// LAPICBase is the default xAPIC MMIO window (IA32_APIC_BASE bits 12+).
	LAPICBase = 0xFEE00000

	// Register offsets within the 4 KiB window.
	lapicID       = 0x020
	lapicVersion  = 0x030
	lapicTPR      = 0x080
	lapicPPR      = 0x0A0
	lapicEOI      = 0x0B0
	lapicLDR      = 0x0D0
	lapicDFR      = 0x0E0
	lapicSVR      = 0x0F0 // bit 8 = APIC software enable
	lapicISRBase  = 0x100 // 8 × 16-byte-spaced 32-bit words
	lapicTMRBase  = 0x180
	lapicIRRBase  = 0x200
	lapicESR      = 0x280
	lapicICRLow   = 0x300
	lapicICRHigh  = 0x310
	lapicLVTTimer = 0x320
	lapicLVTTherm = 0x330
	lapicLVTPerf  = 0x340
	lapicLVTLINT0 = 0x350
	lapicLVTLINT1 = 0x360
	lapicLVTError = 0x370
	lapicTimerICR = 0x380 // initial count
	lapicTimerCCR = 0x390 // current count (read-only)
	lapicTimerDCR = 0x3E0 // divide config

	// LVT bits.
	lvtMasked        = 1 << 16
	lvtTimerPeriodic = 1 << 17
	lvtVectorMask    = 0xFF

	// SVR.
	svrEnable = 1 << 8

	// ICR delivery modes (bits 8-10).
	icrDeliveryMask    = 0x700
	icrDeliveryFixed   = 0x000
	icrDeliveryInit    = 0x500
	icrDeliveryStartup = 0x600
)

// LocalAPIC holds the modelled register state.
type LocalAPIC struct {
	cpu        cpu.X86Core
	cyclesFunc func() uint64

	id  uint32
	svr uint32
	tpr uint32
	ldr uint32
	dfr uint32
	esr uint32

	irr [8]uint32 // requested but not yet in service
	isr [8]uint32 // in service (awaiting EOI)
	tmr [8]uint32 // trigger mode (level=1)

	lvtTimer, lvtTherm, lvtPerf, lvtLINT0, lvtLINT1, lvtError uint32
	icrLow, icrHigh                                           uint32

	timerInitCount uint32
	timerDivideCfg uint32
	timerStart     uint64 // cycle count when the current count was (re)loaded
	timerRunning   bool

	extINT bool // 8259 PIC asserting its INTR (virtual-wire ExtINT)
}

// NewLocalAPIC creates a software-disabled local APIC. Firmware enables it
// by writing SVR.bit8 (and programming IA32_APIC_BASE).
func NewLocalAPIC(core cpu.X86Core, cyclesFunc func() uint64) *LocalAPIC {
	return &LocalAPIC{
		cpu:        core,
		cyclesFunc: cyclesFunc,
		svr:        0xFF, // vector 0xFF, disabled (bit 8 clear)
		lvtTimer:   lvtMasked,
		lvtTherm:   lvtMasked,
		lvtPerf:    lvtMasked,
		lvtLINT0:   lvtMasked,
		lvtLINT1:   lvtMasked,
		lvtError:   lvtMasked,
	}
}

func (l *LocalAPIC) enabled() bool { return l.svr&svrEnable != 0 }

// timerDivisor decodes the divide-config register (bits 0,1,3 form the
// 1,2,4,...,128 divisor; the encoding is deliberately scrambled).
func (l *LocalAPIC) timerDivisor() uint64 {
	v := (l.timerDivideCfg & 0x3) | ((l.timerDivideCfg & 0x8) >> 1)
	switch v {
	case 0:
		return 2
	case 1:
		return 4
	case 2:
		return 8
	case 3:
		return 16
	case 4:
		return 32
	case 5:
		return 64
	case 6:
		return 128
	default: // 7
		return 1
	}
}

// currentCount returns the timer's current count, advancing it from the
// cycle counter. Also fires the timer interrupt when it crosses zero.
func (l *LocalAPIC) currentCount() uint32 {
	if !l.timerRunning || l.timerInitCount == 0 {
		return 0
	}
	elapsed := (l.cyclesFunc() - l.timerStart) / l.timerDivisor()
	if elapsed >= uint64(l.timerInitCount) {
		return 0
	}
	return l.timerInitCount - uint32(elapsed)
}

// CyclesToNextTimer returns the CPU cycles until the LVT timer next fires, or 0
// if no timer is armed/deliverable. Used by the board's idle fast-forward to
// jump straight to the next interrupt instead of crawling tick by tick.
func (l *LocalAPIC) CyclesToNextTimer() uint64 {
	if !l.timerRunning || l.timerInitCount == 0 || l.lvtTimer&lvtMasked != 0 {
		return 0
	}
	target := l.timerStart + uint64(l.timerInitCount)*l.timerDivisor()
	now := l.cyclesFunc()
	if target <= now {
		return 1
	}
	return target - now
}

// Tick advances the timer and fires the LVT-timer interrupt on expiry.
// Called from the machine's CheckTimer loop with the cycle delta.
func (l *LocalAPIC) Tick(uint64) {
	if !l.timerRunning || l.timerInitCount == 0 {
		return
	}
	elapsed := (l.cyclesFunc() - l.timerStart) / l.timerDivisor()
	if elapsed < uint64(l.timerInitCount) {
		return
	}
	// Expired.
	if l.lvtTimer&lvtMasked == 0 {
		l.requestVector(uint8(l.lvtTimer & lvtVectorMask))
	}
	if l.lvtTimer&lvtTimerPeriodic != 0 {
		l.timerStart = l.cyclesFunc()
	} else {
		l.timerRunning = false
	}
	l.updateINTR()
}

// requestVector sets a bit in the IRR.
func (l *LocalAPIC) requestVector(vec uint8) {
	l.irr[vec>>5] |= 1 << (vec & 31)
}

// highestSet returns the highest set bit index in a 256-bit bitmap, or -1.
func highestSet(b *[8]uint32) int {
	for i := 7; i >= 0; i-- {
		if b[i] != 0 {
			for bit := 31; bit >= 0; bit-- {
				if b[i]&(1<<uint(bit)) != 0 {
					return i*32 + bit
				}
			}
		}
	}
	return -1
}

// updateINTR re-evaluates whether an interrupt should be presented to the
// CPU and drives the INTR line accordingly.
func (l *LocalAPIC) updateINTR() {
	if l.pendingVector() >= 0 || (l.extINT && l.enabled()) {
		l.cpu.SetINTR(1)
	} else {
		l.cpu.SetINTR(0)
	}
}

// pendingVector returns the highest IRR vector that outranks both the
// current TPR class and anything already in service, or -1.
func (l *LocalAPIC) pendingVector() int {
	if !l.enabled() {
		return -1
	}
	top := highestSet(&l.irr)
	if top < 0 {
		return -1
	}
	// Priority = vector >> 4. Must exceed TPR's class and the top ISR class.
	if (top >> 4) <= int((l.tpr>>4)&0xF) {
		return -1
	}
	if isrTop := highestSet(&l.isr); isrTop >= 0 && (top>>4) <= (isrTop>>4) {
		return -1
	}
	return top
}

// SetExtINT records the 8259 PIC's INTR line (virtual-wire ExtINT) and
// re-evaluates delivery.
func (l *LocalAPIC) SetExtINT(level int) {
	l.extINT = level != 0
	l.updateINTR()
}

// Ack is the CPU interrupt-acknowledge hook. It returns the vector to
// deliver, moving an APIC interrupt from IRR to ISR, or delegating to the
// PIC for an ExtINT. extInta is called to fetch the PIC's vector.
func (l *LocalAPIC) Ack(extInta func() (uint8, bool)) (uint8, bool) {
	if vec := l.pendingVector(); vec >= 0 {
		l.irr[vec>>5] &^= 1 << (uint(vec) & 31)
		l.isr[vec>>5] |= 1 << (uint(vec) & 31)
		l.updateINTR()
		return uint8(vec), true
	}
	if l.extINT && l.enabled() {
		v, ok := extInta()
		l.updateINTR()
		return v, ok
	}
	return 0, false
}

func (l *LocalAPIC) eoi() {
	if top := highestSet(&l.isr); top >= 0 {
		l.isr[top>>5] &^= 1 << (uint(top) & 31)
	}
	l.updateINTR()
}

// writeICR processes an inter-processor-interrupt command. Single-CPU:
// INIT/SIPI (AP startup) are accepted and dropped; a Fixed IPI to self is
// injected.
func (l *LocalAPIC) writeICR() {
	switch l.icrLow & icrDeliveryMask {
	case icrDeliveryInit, icrDeliveryStartup:
		// No application processors to start.
	case icrDeliveryFixed:
		// Destination shorthand bits 18-19: 1=self, 2=all-incl-self.
		sh := (l.icrLow >> 18) & 0x3
		if sh == 1 || sh == 2 {
			l.requestVector(uint8(l.icrLow & lvtVectorMask))
			l.updateINTR()
		}
	}
}

// MMIORead/MMIOWrite implement the RegisterDevice accessors.
func (l *LocalAPIC) MMIORead(_ any, offset uint32, _ int) uint32 {
	if lapicDebug && offset == lapicSVR {
		fmt.Fprintf(os.Stderr, "[lapic] R SVR -> %#x\n", l.svr)
	}
	switch offset {
	case lapicID:
		return l.id << 24
	case lapicVersion:
		// Version 0x14, max LVT entry = 5 (so bits 16-23 = 5).
		return 0x00050014
	case lapicTPR:
		return l.tpr
	case lapicPPR:
		isrTop := highestSet(&l.isr)
		ppr := l.tpr & 0xF0
		if isrTop >= 0 && (isrTop&0xF0) > int(ppr) {
			ppr = uint32(isrTop & 0xF0)
		}
		return ppr
	case lapicLDR:
		return l.ldr
	case lapicDFR:
		return l.dfr
	case lapicSVR:
		return l.svr
	case lapicESR:
		return l.esr
	case lapicICRLow:
		if lapicDebug {
			fmt.Fprintf(os.Stderr, "[lapic] R ICRLow -> %#x\n", l.icrLow)
		}
		return l.icrLow
	case lapicICRHigh:
		return l.icrHigh
	case lapicLVTTimer:
		return l.lvtTimer
	case lapicLVTTherm:
		return l.lvtTherm
	case lapicLVTPerf:
		return l.lvtPerf
	case lapicLVTLINT0:
		return l.lvtLINT0
	case lapicLVTLINT1:
		return l.lvtLINT1
	case lapicLVTError:
		return l.lvtError
	case lapicTimerICR:
		return l.timerInitCount
	case lapicTimerCCR:
		return l.currentCount()
	case lapicTimerDCR:
		return l.timerDivideCfg
	}
	if offset >= lapicISRBase && offset < lapicISRBase+0x80 {
		return l.isr[(offset-lapicISRBase)/16]
	}
	if offset >= lapicTMRBase && offset < lapicTMRBase+0x80 {
		return l.tmr[(offset-lapicTMRBase)/16]
	}
	if offset >= lapicIRRBase && offset < lapicIRRBase+0x80 {
		return l.irr[(offset-lapicIRRBase)/16]
	}
	return 0
}

func (l *LocalAPIC) MMIOWrite(_ any, offset uint32, val uint32, _ int) {
	if lapicDebug {
		fmt.Fprintf(os.Stderr, "[lapic] W off=%#03x val=%#x\n", offset, val)
	}
	switch offset {
	case lapicID:
		l.id = val >> 24
	case lapicTPR:
		l.tpr = val & 0xFF
		l.updateINTR()
	case lapicEOI:
		l.eoi()
	case lapicLDR:
		l.ldr = val & 0xFF000000
	case lapicDFR:
		l.dfr = val | 0x0FFFFFFF
	case lapicSVR:
		l.svr = val
		l.updateINTR()
	case lapicESR:
		l.esr = 0
	case lapicICRLow:
		l.icrLow = val
		l.writeICR()
	case lapicICRHigh:
		l.icrHigh = val
	case lapicLVTTimer:
		l.lvtTimer = val
	case lapicLVTTherm:
		l.lvtTherm = val
	case lapicLVTPerf:
		l.lvtPerf = val
	case lapicLVTLINT0:
		l.lvtLINT0 = val
	case lapicLVTLINT1:
		l.lvtLINT1 = val
	case lapicLVTError:
		l.lvtError = val
	case lapicTimerICR:
		l.timerInitCount = val
		l.timerStart = l.cyclesFunc()
		l.timerRunning = val != 0
	case lapicTimerDCR:
		l.timerDivideCfg = val
	}
}
