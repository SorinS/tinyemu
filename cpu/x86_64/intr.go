package x86_64

import (
	"fmt"
	"os"
)

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
	if intrTrace {
		fmt.Fprintf(os.Stderr, "[intr] deliver vec=%d hasErr=%v ec=%#x RIP=%#x CR2=%#x IDTR.base=%#x IDTR.limit=%#x\n",
			vec, hasErr, errorCode, c.rip, c.cr[2], idtBase, idtLimit)
		// Dump all 16 GPRs on every delivery so we can correlate "what
		// was the kernel computing right before the fault?" without
		// needing a pre-fault step-trace breadcrumb.
		fmt.Fprintf(os.Stderr, "[intr]   RAX=%#x RBX=%#x RCX=%#x RDX=%#x RSI=%#x RDI=%#x RBP=%#x RSP=%#x\n",
			c.reg64[RAX], c.reg64[RBX], c.reg64[RCX], c.reg64[RDX],
			c.reg64[RSI], c.reg64[RDI], c.reg64[RBP], c.reg64[RSP])
		fmt.Fprintf(os.Stderr, "[intr]   R8=%#x R9=%#x R10=%#x R11=%#x R12=%#x R13=%#x R14=%#x R15=%#x\n",
			c.reg64[R8], c.reg64[R9], c.reg64[R10], c.reg64[R11],
			c.reg64[R12], c.reg64[R13], c.reg64[R14], c.reg64[R15])
		// For #PF only: also walk what the kernel sees at [RDI+0x20]
		// and [R12+0x20] — the common "struct field" offsets in the
		// boot-time helpers that surface garbage pointers. Read via
		// translateForData so we see what the guest sees.
		if vec == 14 {
			for _, label := range []struct {
				name string
				base uint64
			}{
				{"[RDI+0x20]", c.reg64[RDI] + 0x20},
				{"[R12+0x20]", c.reg64[R12] + 0x20},
			} {
				if phys, perr := c.translateForData(label.base, false); perr == nil {
					if v, err := c.memMap.Read64(phys); err == nil {
						fmt.Fprintf(os.Stderr, "[intr]   %s @ %#x (phys %#x) = %#x\n",
							label.name, label.base, phys, v)
					}
				}
			}
		}
	}
	if uint64(vec)*16+16 > uint64(idtLimit)+1 {
		if intrTrace {
			fmt.Fprintf(os.Stderr, "[intr]   FAIL: vector %d beyond IDT limit %#x\n", vec, idtLimit)
		}
		return fmt.Errorf("vector %d beyond IDT limit (%#x)", vec, idtLimit)
	}

	// IDTR.base is a linear address; in long mode it must be translated
	// through the current page tables to reach the gate bytes.
	gateLoPhys, pferr := c.translateForData(gateAddr, false)
	if pferr != nil {
		if intrTrace {
			fmt.Fprintf(os.Stderr, "[intr]   FAIL: translate gate@%#x: %v\n", gateAddr, pferr)
		}
		return fmt.Errorf("translate IDT gate: %w", pferr)
	}
	gateHiPhys, pferr := c.translateForData(gateAddr+8, false)
	if pferr != nil {
		if intrTrace {
			fmt.Fprintf(os.Stderr, "[intr]   FAIL: translate gate+8@%#x: %v\n", gateAddr+8, pferr)
		}
		return fmt.Errorf("translate IDT gate+8: %w", pferr)
	}
	lo, err := c.memMap.Read64(gateLoPhys)
	if err != nil {
		if intrTrace {
			fmt.Fprintf(os.Stderr, "[intr]   FAIL: read gate@%#x (phys %#x) low: %v\n", gateAddr, gateLoPhys, err)
		}
		return fmt.Errorf("read IDT gate low: %w", err)
	}
	hi, err := c.memMap.Read64(gateHiPhys)
	if err != nil {
		if intrTrace {
			fmt.Fprintf(os.Stderr, "[intr]   FAIL: read gate+8@%#x (phys %#x) high: %v\n", gateAddr+8, gateHiPhys, err)
		}
		return fmt.Errorf("read IDT gate high: %w", err)
	}
	if intrTrace {
		fmt.Fprintf(os.Stderr, "[intr]   gate@%#x (phys %#x) lo=%#x hi=%#x\n", gateAddr, gateLoPhys, lo, hi)
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
		if intrTrace {
			fmt.Fprintf(os.Stderr, "[intr]   FAIL: gate not present (typeAttr=%#x)\n", typeAttr)
		}
		// Not-present gate ⇒ #NP (#GP for some vectors). M5c returns
		// the host-level error rather than cascading.
		return fmt.Errorf("vector %d: gate not present", vec)
	}
	if intrTrace {
		fmt.Fprintf(os.Stderr, "[intr]   handler CS=%#x RIP=%#x type=%#x\n", selector, offset, gateType)
	}

	oldRIP := c.rip
	oldCS := c.seg[CS]
	oldRFLAGS := c.rflags
	oldRSP := c.reg64[RSP]
	oldSS := c.seg[SS]

	if intrTrace {
		fmt.Fprintf(os.Stderr, "[intr]   pushing frame: SS=%#x RSP=%#x RFLAGS=%#x CS=%#x RIP=%#x\n",
			oldSS, oldRSP, oldRFLAGS, oldCS, oldRIP)
	}

	// Push the saved context. Real hardware atomically switches stacks
	// first; we serialize the writes on the current RSP. Wrap in a
	// recover so a fault during the push is reported as a returned
	// error rather than propagating out of the deferred recover in
	// Step (where it could not be re-handled cleanly).
	if pushErr := c.pushFrameRecover(uint64(oldSS), oldRSP, oldRFLAGS, uint64(oldCS), oldRIP, hasErr, errorCode); pushErr != nil {
		if intrTrace {
			fmt.Fprintf(os.Stderr, "[intr]   FAIL: push frame: %v\n", pushErr)
		}
		c.reg64[RSP] = oldRSP
		return pushErr
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

// dumpWalk traces the 4-level page-table walk for `linAddr` to stderr,
// printing each entry value so a "present + reserved bit" fault can
// be triaged to a specific level. Used from Step's deferred recover
// when ex.Err.ErrorCode != 0 and intrTrace is on.
func (c *CPU) dumpWalk(linAddr uint64) {
	cr3 := c.cr[3] & 0xFFFFFFFFF000
	pml4Idx := (linAddr >> 39) & 0x1FF
	pdptIdx := (linAddr >> 30) & 0x1FF
	pdIdx := (linAddr >> 21) & 0x1FF
	ptIdx := (linAddr >> 12) & 0x1FF
	pml4e, _ := c.memMap.Read64(cr3 + pml4Idx*8)
	fmt.Fprintf(os.Stderr, "[walk] linAddr=%#x CR3=%#x\n", linAddr, cr3)
	fmt.Fprintf(os.Stderr, "[walk]   PML4[%d] @ %#x = %#x\n", pml4Idx, cr3+pml4Idx*8, pml4e)
	if pml4e&1 == 0 {
		return
	}
	pdpt := pml4e & 0xFFFFFFFFF000
	pdpte, _ := c.memMap.Read64(pdpt + pdptIdx*8)
	fmt.Fprintf(os.Stderr, "[walk]   PDPT[%d] @ %#x = %#x\n", pdptIdx, pdpt+pdptIdx*8, pdpte)
	if pdpte&1 == 0 || pdpte&0x80 != 0 {
		return
	}
	pd := pdpte & 0xFFFFFFFFF000
	pde, _ := c.memMap.Read64(pd + pdIdx*8)
	fmt.Fprintf(os.Stderr, "[walk]   PD[%d]   @ %#x = %#x\n", pdIdx, pd+pdIdx*8, pde)
	if pde&1 == 0 || pde&0x80 != 0 {
		return
	}
	pt := pde & 0xFFFFFFFFF000
	pte, _ := c.memMap.Read64(pt + ptIdx*8)
	fmt.Fprintf(os.Stderr, "[walk]   PT[%d]   @ %#x = %#x\n", ptIdx, pt+ptIdx*8, pte)
}

// pushFrameRecover wraps the five (or six with errorCode) pushes that
// build the interrupt frame and converts any guest-#PF panic during a
// push into a returned error. Without this, a fault during the IDT
// stack-push would propagate out of Step's deferred recover and
// terminate the goroutine with an unrecovered Go panic.
func (c *CPU) pushFrameRecover(ss, rsp, rflags, cs, rip uint64, hasErr bool, errorCode uint32) (err error) {
	defer func() {
		if r := recover(); r != nil {
			if pfp, ok := r.(pageFaultPanic); ok {
				err = pfp.Err
				return
			}
			panic(r)
		}
	}()
	c.push64(ss)
	c.push64(rsp)
	c.push64(rflags)
	c.push64(cs)
	c.push64(rip)
	if hasErr {
		c.push64(uint64(errorCode))
	}
	return nil
}

// CPUID. The Linux x86_64 boot path probes several leaves before
// printk works — most importantly leaf 0 (vendor string, max basic
// leaf), leaf 1 (family/model + base feature flags including SSE2),
// leaf 0x80000000 (max extended leaf), and 0x80000001 (LM + SYSCALL
// + NX bits). Brand string leaves 0x80000002-0x80000004 fill the
// CPU model name shown in /proc/cpuinfo.
//
// We deliberately advertise SSE/SSE2 here even though the decoder
// doesn't implement the corresponding XMM opcodes yet — without
// them the kernel won't even leave early-boot. Each missing opcode
// surfaces as the next ErrNotImplemented and the implementation
// catches up incrementally.
func (c *CPU) opCPUID() error {
	leaf := c.GetReg32(EAX)
	var a, b, cx, d uint32
	switch leaf {
	case 0:
		// Max basic leaf = 1; vendor = "GenuineIntel".
		a = 1
		b = 0x756E6547 // "Genu"
		d = 0x49656E69 // "ineI"
		cx = 0x6C65746E // "ntel"
	case 1:
		// Signature: family 6, model 0, stepping 0.
		a = 0x00000600
		b = 0
		// ECX features: keep minimal — most kernels probe a few specific
		// bits (SSE3=0, SSSE3=9, SSE4_1=19, SSE4_2=20). Advertise SSE3
		// (bit 0) for the rare cases that expect it; nothing else.
		cx = 1 << 0
		// EDX features (bits): FPU(0), TSC(4), MSR(5), PAE(6), CX8(8),
		// APIC(9), SEP(11), PGE(13), CMOV(15), PAT(16), PSE36(17),
		// MMX(23), FXSR(24), SSE(25), SSE2(26).
		d = 1<<0 | 1<<4 | 1<<5 | 1<<6 | 1<<8 | 1<<9 | 1<<11 |
			1<<13 | 1<<15 | 1<<16 | 1<<17 | 1<<23 | 1<<24 | 1<<25 | 1<<26
	case 0x80000000:
		a = 0x80000004
	case 0x80000001:
		// EDX: SYSCALL(11), NX(20), Page1GB(26), LM(29). The LM bit
		// is the "long mode supported" advertisement Linux uses to
		// decide whether to even attempt 64-bit boot. Page1GB lets
		// the decompressor's identity-map setup use 1 GiB huge
		// pages — without it, mapping >1 GiB needs many more PD
		// entries, exhausting the small static boot_pgt_buf.
		d = 1<<11 | 1<<20 | 1<<26 | 1<<29
	case 0x80000002, 0x80000003, 0x80000004:
		// Brand string: 48 bytes split across 3 leaves × 16 bytes
		// (4 dwords each). "tinyemu-go x86_64 long-mode emulator     ".
		brand := []byte("tinyemu-go x86_64 long-mode emulator             ")
		off := int(leaf-0x80000002) * 16
		a = leUint32(brand, off)
		b = leUint32(brand, off+4)
		cx = leUint32(brand, off+8)
		d = leUint32(brand, off+12)
	default:
		// Unrecognised leaves return zero, matching what real CPUs do
		// for invalid leaves above the advertised maximums.
	}
	c.SetReg32(EAX, a)
	c.SetReg32(EBX, b)
	c.SetReg32(ECX, cx)
	c.SetReg32(EDX, d)
	return nil
}

func leUint32(buf []byte, off int) uint32 {
	if off+4 > len(buf) {
		return 0
	}
	return uint32(buf[off]) | uint32(buf[off+1])<<8 |
		uint32(buf[off+2])<<16 | uint32(buf[off+3])<<24
}

// opSYSCALL — 0x0F 0x05 — fast kernel entry. Saves RIP to RCX and
// RFLAGS to R11 (no stack push), loads RIP from LSTAR, builds CS and
// SS selectors out of STAR[47:32], drops to CPL 0, and masks RFLAGS
// with ~SFMASK. The kernel does its own stack switch (SWAPGS + load
// RSP from the per-CPU area) since SYSCALL is a "let it go fast"
// instruction.
func (c *CPU) opSYSCALL() error {
	// EFER.SCE governs whether SYSCALL is enabled. Real hardware
	// raises #UD if SCE=0; M6 ignores that bit for simplicity.
	c.SetReg64(RCX, c.rip)
	c.SetReg64(R11, c.rflags)

	c.rip = c.msrLstar

	starCSMask := uint16((c.msrStar >> 32) & 0xFFFF)
	c.seg[CS] = starCSMask & 0xFFFC
	c.segBase[CS] = 0
	c.segAccess[CS] = csLBit | 0x9A // P=1, S=1, code, executable, readable
	c.seg[SS] = (starCSMask + 8) & 0xFFFC
	c.segBase[SS] = 0
	c.segAccess[SS] = 0x92 // P=1, S=1, data, writable

	// RFLAGS &= ~SFMASK, then re-OR reserved bit 1.
	c.rflags = (c.rflags &^ c.msrSFMask) | 2
	c.cpl = 0
	c.recomputeMode()
	return nil
}

// opSYSRET — 0x0F 0x07 — fast kernel exit. Loads RIP from RCX, RFLAGS
// from R11, and reconstructs user-mode CS/SS from STAR[63:48]. The
// SDM specifies a +16 offset for CS (the user CS) and +8 for SS (the
// user SS), with the result OR'd with RPL=3.
func (c *CPU) opSYSRET(rex uint8) error {
	starUserBase := uint16((c.msrStar >> 48) & 0xFFFF)
	// Per SDM: SYSRET sets CS to STAR[63:48]+16, SS to STAR[63:48]+8,
	// both with RPL=3. The +16 is because the SYSRET CS slot in the
	// GDT is two descriptors above the user-data SS slot.
	c.seg[CS] = (starUserBase + 16) | 3
	c.segBase[CS] = 0
	if rex&rexW != 0 {
		c.segAccess[CS] = csLBit | 0xFA // user code, 64-bit
	} else {
		c.segAccess[CS] = csDBit | 0xFA // user code, 32-bit compat
	}
	c.seg[SS] = (starUserBase + 8) | 3
	c.segBase[SS] = 0
	c.segAccess[SS] = 0xF2 // user data
	c.rip = c.GetReg64(RCX)
	c.rflags = (c.GetReg64(R11) & ValidFlagMask) | 2
	c.cpl = 3
	c.recomputeMode()
	return nil
}

// opRETF implements RETF (0xCB) — far return. Pops a (RIP, CS) pair
// at operand-size width. M5c-style: synthesises a 64-bit code-segment
// descriptor cache for the new CS rather than walking the GDT. That
// holds as long as the kernel uses RETF to land in 64-bit code; if a
// compat-mode target ever appears we'll need the descriptor walk.
func (c *CPU) opRETF(operandSize uint8) error {
	if operandSize != 8 {
		// 32- and 16-bit far returns aren't typical in long-mode
		// kernel code; surface explicitly until needed.
		return unimplemented("RETF with operand size %d (expected 8)", operandSize)
	}
	newRIP := c.pop64()
	newCS := uint16(c.pop64())
	c.rip = newRIP
	c.seg[CS] = newCS
	c.segBase[CS] = 0
	c.segAccess[CS] = csLBit | 0x9A
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

	if intrTrace {
		fmt.Fprintf(os.Stderr, "[iretq] -> RIP=%#x CS=%#x RFLAGS=%#x RSP=%#x SS=%#x\n",
			newRIP, newCS, newFlags, newRSP, newSS)
	}
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
