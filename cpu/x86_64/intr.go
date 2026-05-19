package x86_64

import "fmt"

// deliverInterrupt vectors `vec` through the long-mode IDT. The frame
// pushed onto the kernel stack is the standard 64-bit-mode five-word
// layout: SS, RSP, RFLAGS, CS, RIP, [error code]. The error code is
// pushed when hasErr is true (vectors 8, 10, 11, 12, 13, 14, 17, 21).
//
// M5c keeps the implementation minimal:
//   - Stack switch via IST is not modeled (uses the current RSP).
//   - Cross-privilege-level stack switching (TSS.RSP0/1/2) is not
//     modeled (CPL is assumed 0 in tests).
//   - The new CS's descriptor is not walked through the GDT; we trust
//     the gate and synthesize a 64-bit code-segment access word.
// The full descriptor walk + IST/TSS lookup arrives in a later phase
// once those features are needed for real kernel boot.
func (c *CPU) deliverInterrupt(vec uint8, hasErr bool, errorCode uint32) error {
	idtBase := c.segBase[IDTR]
	idtLimit := c.segLimit[IDTR]
	gateAddr := idtBase + uint64(vec)*16
	if uint64(vec)*16+16 > uint64(idtLimit)+1 {
		return fmt.Errorf("vector %d beyond IDT limit (%#x)", vec, idtLimit)
	}

	lo, err := c.memMap.Read64(gateAddr)
	if err != nil {
		return fmt.Errorf("read IDT gate low: %w", err)
	}
	hi, err := c.memMap.Read64(gateAddr + 8)
	if err != nil {
		return fmt.Errorf("read IDT gate high: %w", err)
	}

	// Long-mode IDT gate layout (16 bytes):
	//   [0..1]   low 16 of handler RIP
	//   [2..3]   target CS selector
	//   [4]      IST (low 3 bits); rest reserved
	//   [5]      type (low 4) | DPL (bits 5-6) | P (bit 7)
	//   [6..7]   bits 16..31 of handler RIP
	//   [8..11]  bits 32..63 of handler RIP
	//   [12..15] reserved
	offset := uint64(lo&0xFFFF) |
		(uint64(lo>>32) & 0xFFFF0000) |
		(uint64(hi&0xFFFFFFFF) << 32)
	selector := uint16((lo >> 16) & 0xFFFF)
	typeAttr := uint8((lo >> 40) & 0xFF)
	gateType := typeAttr & 0xF
	if typeAttr&0x80 == 0 {
		// Not-present gate ⇒ #NP (#GP for some vectors). M5c returns
		// the host-level error rather than cascading.
		return fmt.Errorf("vector %d: gate not present", vec)
	}

	oldRIP := c.rip
	oldCS := c.seg[CS]
	oldRFLAGS := c.rflags
	oldRSP := c.reg64[RSP]
	oldSS := c.seg[SS]

	// Push the saved context. Real hardware atomically switches stacks
	// first; we serialize the writes on the current RSP.
	c.push64(uint64(oldSS))
	c.push64(oldRSP)
	c.push64(oldRFLAGS)
	c.push64(uint64(oldCS))
	c.push64(oldRIP)
	if hasErr {
		c.push64(uint64(errorCode))
	}

	// Install the handler's CS:RIP. The descriptor cache is faked as a
	// 64-bit code segment so subsequent fetches land in long mode.
	c.seg[CS] = selector
	c.segBase[CS] = 0
	c.segAccess[CS] = csLBit | 0x9A // P=1, S=1, code, executable, readable
	c.rip = offset

	// Type 0xE = interrupt gate (clears IF); 0xF = trap gate (keeps IF).
	if gateType == 0xE {
		c.rflags &^= RFLAGS_IF
	}
	// Always clear TF/NT/RF/VM per Intel SDM.
	c.rflags &^= RFLAGS_TF | RFLAGS_NT | RFLAGS_RF | RFLAGS_VM
	c.recomputeMode()
	return nil
}

// opIRETQ implements the 64-bit interrupt return. Pops five 8-byte
// values from the current stack in the order RIP, CS, RFLAGS, RSP, SS.
// In 64-bit mode IRET always pops all five regardless of whether a
// privilege change actually occurred — different from the 32-bit form.
func (c *CPU) opIRETQ() error {
	newRIP := c.pop64()
	newCS := uint16(c.pop64())
	newFlags := c.pop64()
	newRSP := c.pop64()
	newSS := uint16(c.pop64())

	c.rip = newRIP
	c.seg[CS] = newCS
	c.segBase[CS] = 0
	c.segAccess[CS] = csLBit | 0x9A
	c.rflags = (newFlags & ValidFlagMask) | 2
	c.reg64[RSP] = newRSP
	c.seg[SS] = newSS
	c.recomputeMode()
	return nil
}
