package x86_64

import "fmt"

// Mode is the architectural CPU mode. It is recomputed any time the
// architectural bits that determine it change (CR0.PE, EFLAGS.VM,
// EFER.LMA, CS.L, CS.D). Holding it as an explicit field lets the
// instruction decoder switch on a single value instead of re-evaluating
// the half-dozen bits on every Step().
type Mode uint8

const (
	// ModeReal16 — real mode (CR0.PE=0). 16-bit code, segmented memory.
	ModeReal16 Mode = iota
	// ModeProtected16 — protected mode with CS.D=0. 16-bit default
	// operand size, full segmentation.
	ModeProtected16
	// ModeProtected32 — protected mode with CS.D=1. 32-bit default
	// operand size; what an i386 Linux kernel runs in.
	ModeProtected32
	// ModeCompat32 — long mode active (EFER.LMA=1) with CS.L=0. Code
	// runs as 32-bit, addresses still go through the 4-level page
	// walker. Used by 32-bit userspace under a 64-bit kernel.
	ModeCompat32
	// ModeLong64 — long mode active with CS.L=1. The native AMD64
	// runtime: 64-bit operand size by default (REX.W=1 for 64-bit ops),
	// 64-bit address size, segment bases ignored for CS/DS/ES/SS.
	ModeLong64
)

// String returns a human-readable mode name (used by error messages and
// future trace output).
func (m Mode) String() string {
	switch m {
	case ModeReal16:
		return "real16"
	case ModeProtected16:
		return "pm16"
	case ModeProtected32:
		return "pm32"
	case ModeCompat32:
		return "compat32"
	case ModeLong64:
		return "long64"
	default:
		return fmt.Sprintf("mode(%d)", uint8(m))
	}
}

// recomputeMode derives the current Mode from the architectural state
// and stores it on the CPU. Callers must invoke this after any mutation
// that affects CR0.PE, EFLAGS.VM, EFER.LMA, or CS access bits.
func (c *CPU) recomputeMode() {
	if c.efer&EFER_LMA != 0 {
		// Long mode. CS.L is bit 13 of the access flags (the "L" bit on
		// 64-bit code-segment descriptors). The decoder for CS sets it
		// alongside the descriptor's other access bits when CS is loaded
		// via a far jump.
		if c.segAccess[CS]&csLBit != 0 {
			c.mode = ModeLong64
		} else {
			c.mode = ModeCompat32
		}
		return
	}
	if c.cr[0]&CR0_PE == 0 {
		c.mode = ModeReal16
		return
	}
	// CS descriptor's D-bit (bit 22 of segAccess, since segAccess holds
	// the high 32 bits of the descriptor — same layout as cpu/x86).
	if c.segAccess[CS]&csDBit != 0 {
		c.mode = ModeProtected32
	} else {
		c.mode = ModeProtected16
	}
}

// Bit positions inside the segAccess word. Matches cpu/x86's encoding:
// the low byte is the access byte (P, DPL, S, Type) and bits 8..11 are
// the flags nibble (AVL, L, D/B, G) — i.e. the descriptor's high dword
// shifted to a friendly layout.
//
//	bit 9  (0x0200) — L   (long-mode code segment). 1 = 64-bit code.
//	bit 10 (0x0400) — D/B (default operand size).   1 = 32-bit.
const (
	csLBit = 1 << 9
	csDBit = 1 << 10
)
