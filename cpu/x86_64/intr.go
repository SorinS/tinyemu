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
//
// The full descriptor walk + IST/TSS lookup arrives in a later phase
// once those features are needed for real kernel boot.
func (c *CPU) deliverInterrupt(vec uint8, hasErr bool, errorCode uint32) error {
	// Long-mode-only path. The 16-byte gate format, 64-bit RIP/RSP
	// pushes, TSS.RSP0 stack switch — all are long-mode specifics. If
	// we ever fire an interrupt while in real / pm16 / pm32 / compat32
	// we'd silently corrupt the stack and the IDT lookup would
	// misframe (16/32-bit IDT entries are 8 bytes, not 16). Panic loud
	// rather than crash silent — a non-long-mode interrupt path needs
	// its own implementation.
	if c.mode != ModeLong64 {
		return c.deliverInterruptLegacy(vec, hasErr, errorCode)
	}
	idtBase := c.segBase[IDTR]
	idtLimit := c.segLimit[IDTR]
	gateAddr := idtBase + uint64(vec)*16

	// Capture CPL before we flip it. The transition to a kernel handler
	// is architecturally supervisor-mode by the time the CPU touches
	// the IDT and (for user→kernel) the TSS. Real silicon performs the
	// gate read and TSS.RSP0 fetch as microcode operations that are
	// NOT gated on the current CPL's U/S check — they're treated as
	// supervisor accesses regardless of where the fault originated.
	// Flip c.cpl to 0 NOW so the subsequent translateForData calls in
	// this function go through the walker with isUser=false. Without
	// this, a user-mode #PF whose IDT gate sits on a kernel-only page
	// (U/S=0) gets a recursive "user can't read kernel page" PF when
	// we try to deliver, and the original fault escapes as an
	// emulator error. We still use oldCPL for the "do we need to load
	// TSS.RSP0?" decision below.
	oldCPL := c.cpl
	c.cpl = 0
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
			// Walk the stack a few frames to reconstruct an approximate
			// call chain. We only know RSP at fault; the SDM doesn't
			// require a frame pointer. Print the next 24 quadwords
			// above RSP, marking those that look like kernel-text
			// return addresses with [RET] so the caller can be picked
			// out of the noise.
			rsp := c.reg64[RSP]
			fmt.Fprintf(os.Stderr, "[intr]   stack-near-RSP (24 qwords above):\n")
			for i := uint64(0); i < 24; i++ {
				addr := rsp + i*8
				phys, perr := c.translateForData(addr, false)
				if perr != nil {
					continue
				}
				v, err := c.memMap.Read64(phys)
				if err != nil {
					continue
				}
				marker := ""
				// Kernel-text range from vmlinux PT_LOAD (this image).
				if v >= 0xffffffff81000000 && v < 0xffffffff820a7e10 {
					marker = " [RET-text]"
				} else if v >= 0xffffffff82200000 && v < 0xffffffff83000000 {
					marker = " [data]"
				}
				if v != 0 {
					fmt.Fprintf(os.Stderr, "[intr]     [RSP+%#x] = %#x%s\n", i*8, v, marker)
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
			fmt.Fprintf(os.Stderr, "[intr]   FAIL: gate not present (typeAttr=%#x), translated gate phys: lo=%#x hi=%#x\n",
				typeAttr, gateLoPhys, gateHiPhys)
			// Dump the whole IDT (256 vectors × 16 bytes = 4 KB) and
			// list any populated entries. With this we can tell at a
			// glance whether *no* IDT writes have happened (kernel
			// init never wrote real gates) vs *some* (just vec 48
			// missed) vs *all-but-this-one* (alias confusion).
			idtBasePhys := gateLoPhys - uint64(vec)*16
			populated := 0
			var missing []int
			for i := uint64(0); i < 256; i++ {
				lo, _ := c.memMap.Read64(idtBasePhys + i*16)
				hi, _ := c.memMap.Read64(idtBasePhys + i*16 + 8)
				if lo != 0 || hi != 0 {
					populated++
				} else {
					missing = append(missing, int(i))
				}
			}
			fmt.Fprintf(os.Stderr, "[intr]   IDT populated: %d / 256.  Missing vectors: %v\n",
				populated, missing)
			// Dump system_vectors[] — Linux's bitmap of vectors reserved
			// for system use (skipped by idt_setup_apic_and_irq_gates'
			// for_each_clear_bit_from loop). Any vec with a set bit
			// here is EXPECTED to have its gate installed by a
			// different code path (legacy_pic init, apic init, etc.).
			// Symbol: ffffffff82bcb660 B system_vectors (Alpine 6.6).
			const sysVecVA = 0xffffffff82bcb660
			if phys, perr := c.translateForData(sysVecVA, false); perr == nil {
				var bits [4]uint64
				for i := 0; i < 4; i++ {
					bits[i], _ = c.memMap.Read64(phys + uint64(i)*8)
				}
				fmt.Fprintf(os.Stderr, "[intr]   system_vectors[0..3] = %#x %#x %#x %#x\n",
					bits[0], bits[1], bits[2], bits[3])
				var sysSet []int
				for v := 0; v < 256; v++ {
					if bits[v/64]&(1<<(v%64)) != 0 {
						sysSet = append(sysSet, v)
					}
				}
				fmt.Fprintf(os.Stderr, "[intr]   system_vectors set: %v\n", sysSet)
			}
			// Decode the IRQ-stub entries (vectors 32..63) so we can see
			// which stub each gate points at and detect off-by-8 errors
			// where alternate stubs are installed.
			for v := 32; v < 64; v++ {
				lo, _ := c.memMap.Read64(idtBasePhys + uint64(v)*16)
				hi, _ := c.memMap.Read64(idtBasePhys + uint64(v)*16 + 8)
				if lo == 0 && hi == 0 {
					continue
				}
				offset := uint64(lo&0xFFFF) |
					(uint64(lo>>32) & 0xFFFF0000) |
					(uint64(hi&0xFFFFFFFF) << 32)
				fmt.Fprintf(os.Stderr, "[intr]   idt[%d] handler=%#x\n", v, offset)
			}
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

	// On a user→kernel privilege escalation (oldCPL=3 entering a gate
	// that drops to CPL=0), real silicon loads RSP from TSS.RSP0
	// BEFORE pushing the frame. Without this swap, the pushes land on
	// the USER stack — which (a) might be unmapped and fault the
	// push, or (b) leaks kernel state into user-readable memory. LTR
	// previously stashed the TSS base in c.segBase[TR]; TSS.RSP0
	// lives 4 bytes into the TSS structure. We already flipped c.cpl
	// to 0 at the top of this function for the IDT gate read, so we
	// test oldCPL here, not c.cpl.
	if oldCPL == 3 {
		tssBase := c.segBase[TR]
		if tssBase == 0 {
			return fmt.Errorf("user-mode interrupt vec=%d but TR not loaded (TSS base unknown)", vec)
		}
		rsp0VA := tssBase + 4
		rsp0Phys, perr := c.translateForData(rsp0VA, false)
		if perr != nil {
			if intrTrace {
				fmt.Fprintf(os.Stderr, "[intr]   FAIL: translate TSS.RSP0@%#x: %v\n", rsp0VA, perr)
			}
			return fmt.Errorf("translate TSS.RSP0: %w", perr)
		}
		rsp0, err := c.memMap.Read64(rsp0Phys)
		if err != nil {
			return fmt.Errorf("read TSS.RSP0: %w", err)
		}
		if intrTrace {
			fmt.Fprintf(os.Stderr, "[intr]   user→kernel: switching to TSS.RSP0=%#x (was user RSP=%#x)\n", rsp0, oldRSP)
		}
		c.reg64[RSP] = rsp0
	}

	if intrTrace {
		fmt.Fprintf(os.Stderr, "[intr]   pushing frame: SS=%#x RSP=%#x RFLAGS=%#x CS=%#x RIP=%#x (current RSP=%#x)\n",
			oldSS, oldRSP, oldRFLAGS, oldCS, oldRIP, c.reg64[RSP])
	}

	// Push the saved context. Real hardware atomically switches stacks
	// first (which we just did via TSS.RSP0 above for CPL changes);
	// we serialize the writes on the (now kernel) RSP. Wrap in a
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

	// CPL was already flipped to 0 at the top of this function so the
	// gate-read and TSS.RSP0-read translations would go as supervisor.
	// Nothing more to do here.

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
// deliverInterruptLegacy handles real-mode / pm16 / pm32 interrupt
// delivery. SeaBIOS depends on this for IRQ0 (timer), INT 13h (disk),
// INT 10h (video), and the rest of the legacy BIOS callback world. Long
// mode has its own path in deliverInterrupt.
//
// Real mode (CR0.PE=0):
//   - IDT is an array of 256 × 4-byte gates at IDTR.base: [IP_lo16,
//     CS_lo16].
//   - Push FLAGS, CS, IP (3×2 bytes) onto the current stack.
//   - Clear IF and TF; load CS:IP from the gate; CS.base = sel<<4.
//   - Error-code pushes don't apply in real mode (the CPU doesn't
//     generate them); hasErr is ignored.
//
// Protected mode (CR0.PE=1, not long):
//   - IDT is an array of 8-byte gates.
//     Gate format (Intel SDM Vol 3 §6.11): [offset_lo16, selector,
//     reserved, type_attr, offset_hi16].
//   - Same-CPL: push EFLAGS, CS, EIP (3×4 in pm32, 3×2 in pm16). If
//     hasErr, push errorCode.
//   - Cross-CPL stack switch via TSS is NOT modelled here — BIOS code
//     runs CPL=0. CS descriptor walk is not modelled either; we keep
//     the cached base/limit and just update the selector.
func (c *CPU) deliverInterruptLegacy(vec uint8, hasErr bool, errorCode uint32) error {
	if c.cr[0]&CR0_PE == 0 {
		return c.deliverInterruptRealMode(vec)
	}
	return c.deliverInterruptProt(vec, hasErr, errorCode)
}

func (c *CPU) deliverInterruptRealMode(vec uint8) error {
	idtBase := c.segBase[IDTR]
	idtLimit := c.segLimit[IDTR]
	gateAddr := idtBase + uint64(vec)*4
	if uint64(vec)*4+4 > uint64(idtLimit)+1 {
		return fmt.Errorf("real-mode interrupt: vector %d beyond IDT limit %#x", vec, idtLimit)
	}
	// Real mode: IDTR.base is a linear address, paging is off, no
	// translation needed.
	newIP := uint64(c.readMem16(gateAddr))
	newCS := c.readMem16(gateAddr + 2)
	if intrTrace {
		fmt.Fprintf(os.Stderr, "[intr-rm] cycle=%d vec=%d IDT.base=%#x gate@%#x -> %#x:%#x\n",
			c.cycles, vec, idtBase, gateAddr, newCS, newIP)
	}
	// Build a fresh FLAGS image to push: current flags with the high
	// 16 stripped (real mode operates on the low 16-bit FLAGS only;
	// IRET will pop 16 bits back).
	flagsToPush := uint16(c.rflags)
	c.pushStack(uint64(flagsToPush), 2)
	c.pushStack(uint64(c.seg[CS]), 2)
	c.pushStack(c.rip&0xFFFF, 2)
	// Clear IF and TF after pushing the saved copy — the handler runs
	// with interrupts off and single-step disabled. Per Intel SDM Vol 1
	// §6.8.2 AC is also cleared.
	c.rflags &^= RFLAGS_IF | RFLAGS_TF | RFLAGS_AC
	c.seg[CS] = newCS
	c.segBase[CS] = uint64(newCS) << 4
	// CS.limit/access preserved (big-real-mode if previously set).
	c.rip = newIP
	return nil
}

func (c *CPU) deliverInterruptProt(vec uint8, hasErr bool, errorCode uint32) error {
	idtBase := c.segBase[IDTR]
	idtLimit := c.segLimit[IDTR]
	gateAddr := idtBase + uint64(vec)*8
	if uint64(vec)*8+8 > uint64(idtLimit)+1 {
		return fmt.Errorf("prot-mode interrupt: vector %d beyond IDT limit %#x (RIP=%#x CS=%#x)",
			vec, idtLimit, c.rip, c.seg[CS])
	}
	gate := c.readMem64(gateAddr)
	offsetLo := uint16(gate)
	selector := uint16(gate >> 16)
	typeAttr := uint8(gate >> 40)
	offsetHi := uint16(gate >> 48)
	if typeAttr&0x80 == 0 {
		return fmt.Errorf("prot-mode interrupt: gate %d not present (typeAttr=%#x RIP=%#x CS=%#x)",
			vec, typeAttr, c.rip, c.seg[CS])
	}
	gateType := typeAttr & 0xF
	newRIP := uint64(offsetLo) | (uint64(offsetHi) << 16)
	if intrTrace {
		fmt.Fprintf(os.Stderr, "[intr-prot] vec=%d gate@%#x type=%#x -> %#x:%#x\n",
			vec, gateAddr, gateType, selector, newRIP)
	}
	// Stack-slot width: pm32 pushes 4, pm16 pushes 2. Same-CPL only —
	// no SS:ESP load from TSS modelled.
	slotSize := 4
	if c.mode == ModeProtected16 {
		slotSize = 2
	}
	flagsToPush := c.rflags
	if slotSize == 2 {
		flagsToPush &= 0xFFFF
	} else {
		flagsToPush &= 0xFFFFFFFF
	}
	c.pushStack(flagsToPush, slotSize)
	c.pushStack(uint64(c.seg[CS]), slotSize)
	c.pushStack(c.rip, slotSize)
	if hasErr {
		c.pushStack(uint64(errorCode), slotSize)
	}
	// Per Intel SDM: clear IF only for interrupt-gates (type 0xE/0x6),
	// not trap-gates (0xF/0x7). TF cleared either way.
	c.rflags &^= RFLAGS_TF
	if gateType == 0xE || gateType == 0x6 {
		c.rflags &^= RFLAGS_IF
	}
	c.seg[CS] = selector
	// Don't walk the GDT: trust the existing cached base/limit. For
	// SeaBIOS in pm32 the gates point back into the same flat segment
	// the BIOS uses, so the cache is fine. Refine when we hit a
	// kernel-style cross-segment gate.
	c.rip = newRIP
	return nil
}

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
		// Max basic leaf = 0x16 (so leaves 0x15/0x16 below are reachable
		// — guests check cpuid_level before reading them). Leaves between
		// 1 and 0x16 that we don't model fall to the default case and
		// return zero, which is the correct "nothing here" answer (no
		// AVX in leaf 7, no XSAVE in leaf 0xD — gated by CPUID.1 ECX bits
		// we leave clear, no deterministic-cache data in leaf 4, etc.).
		a = 0x16
		b = 0x756E6547  // "Genu"
		d = 0x49656E69  // "ineI"
		cx = 0x6C65746E // "ntel"
	case 1:
		// Signature: family 6, model 0, stepping 0.
		a = 0x00000600
		// EBX: bits 15:8 = CLFLUSH line size in 8-byte units (8 → 64 bytes),
		// matching the QEMU vCPU. Brand index / APIC ID / max-logical-procs
		// (the other bytes) stay 0 for our single, unbranded CPU. Firmware
		// reads this early; a zero CLFLUSH size diverges from real hardware.
		b = 0x00000800
		// ECX features:
		//   bit  0  SSE3 — advertised for the rare cases that expect it.
		//   bit 13  CX16 — CMPXCHG16B is implemented (opGroup9 /1).
		//   bit 23  POPCNT — implemented (F3 0F B8).
		//   bit 30  RDRAND — load-bearing for Linux's crng_init. Without
		//           it Linux's entropy collection falls back to
		//           interrupt-arrival timing, which on our emulator (no
		//           HPET, slow TSC, no real device noise) drags crng
		//           init out to ~300 *kernel-seconds*. That's the
		//           Alpine-boot delay between "i8042 probe failed" and
		//           "Mounting boot media". With RDRAND advertised + a
		//           working `rdrand` opcode, crng init drops to under
		//           a second of boot-time and the wait disappears.
		cx = 1<<0 | 1<<13 | 1<<23 | 1<<30
		if c.featureProfile == profileStrict {
			// Drop SSE3 + RDRAND: keep only the SSE2 baseline (in EDX)
			// that Linux requires, so guests are less likely to emit
			// instructions past what we implement. CX16/POPCNT are real
			// implemented opcodes, so they stay advertised even here.
			cx = 1<<13 | 1<<23
		}
		// EDX features (bits): FPU(0), TSC(4), MSR(5), PAE(6), CX8(8),
		// SEP(11), PGE(13), CMOV(15), PAT(16), PSE36(17),
		// MMX(23), FXSR(24), SSE(25), SSE2(26).
		//
		// APIC (bit 9) is deliberately OFF. We have no APIC modelled
		// in the chassis and Linux on x86_64 with this bit set marks
		// vectors 0x30..0x3F (the legacy PIC range) in system_vectors
		// expecting lapic_assign_system_vectors() to install them.
		// With `nolapic noapic` on the kernel command line that path
		// is a no-op, and idt_setup_apic_and_irq_gates' for_each_
		// clear_bit_from loop SKIPS those vectors — so the IDT slots
		// for the timer (vec 48 = IRQ 0) and friends stay zero and
		// the first IRQ faults "gate not present". Clearing this bit
		// is the correct advertisement for the configuration we
		// actually emulate.
		d = 1<<0 | 1<<4 | 1<<5 | 1<<6 | 1<<8 | 1<<11 |
			1<<13 | 1<<15 | 1<<16 | 1<<17 | 1<<23 | 1<<24 | 1<<25 | 1<<26
		// APIC (bit 9): only when a local APIC is actually modelled. See
		// the note above — advertising it without an APIC breaks the
		// nolapic IRQ path; the machine sets apicEnabled only when it
		// wires a LocalAPIC.
		if c.apicEnabled {
			d |= 1 << 9
		}
	case 0x15:
		// TSC / core-crystal-clock ratio. TSC_freq = ECX * EBX / EAX.
		// We report a 1 GHz TSC (RDTSC returns ns since reset): crystal
		// (ECX) = 25 MHz, ratio (EBX/EAX) = 40/1 → 25e6 * 40 = 1e9 Hz.
		// EAX (denominator) must be non-zero or consumers ignore the leaf.
		a = 1          // denominator
		b = 40         // numerator
		cx = 25_000_000 // nominal core crystal clock, Hz
		d = 0
	case 0x16:
		// Processor frequency: EAX = base MHz, EBX = max MHz, ECX = bus
		// MHz. 1000 MHz matches the 1 GHz TSC. Used as a fallback by
		// guests when leaf 0x15's crystal (ECX) is zero, and for the
		// "CPU speed" line tools print.
		a = 1000 // base frequency, MHz
		b = 1000 // max frequency, MHz
		cx = 100 // bus (reference) frequency, MHz
		d = 0
	case 0x80000000:
		// Max extended leaf = 0x80000008 so the address-size leaf below
		// is reachable.
		a = 0x80000008
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
	case 0x80000008:
		// Address sizes. EAX[7:0] = physical-address bits,
		// EAX[15:8] = linear (virtual) address bits, EAX[23:16] =
		// guest-physical-address bits. Firmware (OVMF) reads this to size
		// its GCD memory space and the coverage of its identity page
		// tables; a zero here (the old default for an unadvertised leaf)
		// makes those two disagree. 40/48 matches the QEMU default vCPU.
		//
		// GuestPhysBits (bits 23:16) is what OVMF's
		// PlatformAddressWidthFromCpuid uses as its "trust this width"
		// signal: with it zero it marks the address width "Valid: No" and
		// falls back to a layout that strands PEI permanent memory (and
		// thus the EmuVariable NV-store reservation) near 4 GiB, above our
		// mapped RAM — a #PF the moment it seeds the store from flash.
		// Reporting it equal to the physical width matches a QEMU vCPU
		// configured with host-phys-bits and makes the width Valid.
		a = (40 << 16) | (48 << 8) | 40
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
	if userSyscallTrace {
		// Log user-mode syscalls — the syscall number is in RAX (SysV
		// ABI), args in RDI/RSI/RDX/R10/R8/R9. We only log from CPL=3
		// (user mode); kernel-internal syscalls (CPL=0) are noise.
		if c.cpl == 3 {
			fmt.Fprintf(os.Stderr, "[usys] nr=%d (%s) rdi=%#x rsi=%#x rdx=%#x r10=%#x rip=%#x\n",
				c.reg64[RAX], linuxSyscallName(uint32(c.reg64[RAX])),
				c.reg64[RDI], c.reg64[RSI], c.reg64[RDX], c.reg64[R10], c.rip)
		}
	}
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
	// Per Intel SDM Vol 2A: RETF pops (IP/EIP, CS) at OPERAND SIZE, not
	// stack-slot size. Real-mode default is 16-bit; 0x66 promotes to
	// 32-bit. CS is always at the same width as the pushed offset (so
	// CALL FAR + RETF stay balanced) — even though "CS" is only 16
	// significant bits, the stack slot is full operand-size width.
	if c.mode != ModeLong64 {
		slotSize := int(operandSize)
		newRIP := c.popStack(slotSize)
		newCS := uint16(c.popStack(slotSize))
		c.rip = newRIP
		c.seg[CS] = newCS
		if c.cr[0]&CR0_PE == 0 {
			// Real mode: only base rebuilds; limit + access preserved
			// to support big-real-mode (see opMOVtoSreg comment).
			c.segBase[CS] = uint64(newCS) << 4
		} else {
			// Protected mode (pm16, pm32, compat32): walk the GDT/LDT
			// and refresh the cached base/limit/access from the new
			// descriptor. Without this the descriptor's L bit (and
			// D, base, limit) stay frozen at the previous selector's
			// values — and a RETF that intentionally crosses from
			// compat32 to long64 by pushing a CS selector whose new
			// GDT slot has L=1 silently stays in compat32.
			//
			// That was the TinyCorePure64 wall: Linux's PVH entry
			// builds a one-shot transition GDT, pushes (long64-CS, IP)
			// and `retf`s into 64-bit code. Without this descriptor
			// reload the next instruction (assembled for long mode
			// with RIP-relative addressing) decodes as compat32
			// absolute-disp32 and the read of [disp32] page-faults
			// before the kernel installs its IDT.
			c.loadProtSegment(CS, newCS)
		}
		c.cpl = int(newCS & 3)
		c.recomputeMode()
		return nil
	}
	newRIP := c.pop64()
	newCS := uint16(c.pop64())
	c.rip = newRIP
	c.seg[CS] = newCS
	c.segBase[CS] = 0
	// Mirror IRETQ: derive CPL/DPL from CS RPL so a RETF to user mode
	// (e.g. from a far-jump trampoline) does not leave c.cpl stale.
	newCPL := int(newCS & 3)
	c.cpl = newCPL
	if newCPL == 3 {
		c.segAccess[CS] = csLBit | 0xFA
	} else {
		c.segAccess[CS] = csLBit | 0x9A
	}
	c.recomputeMode()
	return nil
}

// opIRETQ implements the 64-bit interrupt return. Pops five 8-byte
// values from the current stack in the order RIP, CS, RFLAGS, RSP, SS.
// In 64-bit mode IRET always pops all five regardless of whether a
// privilege change actually occurred — different from the 32-bit form.
func (c *CPU) opIRETQ(operandSize uint8) error {
	// Mode dispatch:
	//   Long mode:    pop 5×8 bytes (RIP, CS, RFLAGS, RSP, SS).
	//   Real / pm16:  pop 3×2 bytes (IP, CS, FLAGS).
	//   pm32 same-CPL: pop 3×4 bytes (EIP, CS, EFLAGS).
	//   pm32 + CPL change or VM=1: pop 5×4 bytes (adds ESP, SS).
	// We split the long-mode path; the legacy 16/32-bit paths share a
	// helper because the shape is the same modulo slot width.
	if c.mode != ModeLong64 {
		return c.opIRETlegacy(operandSize)
	}
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
	// Derive new CPL from CS RPL (Intel SDM Vol.3 §6.14.3 — IRETQ
	// reads CPL from CS[1:0]). The synthesised descriptor cache must
	// match: a return to RPL=3 sets DPL=3 (0xFA access byte), a return
	// to RPL=0 keeps DPL=0 (0x9A). Without the c.cpl update the
	// emulator stays "kernel" while CS says "user", and the next
	// page-fault generates an error_code with X86_PF_USER cleared,
	// driving Linux's do_user_addr_fault straight into no_context →
	// "BUG: unable to handle page fault" → init killed.
	newCPL := int(newCS & 3)
	c.cpl = newCPL
	if newCPL == 3 {
		c.segAccess[CS] = csLBit | 0xFA
	} else {
		c.segAccess[CS] = csLBit | 0x9A
	}
	c.rflags = (newFlags & ValidFlagMask) | 2
	c.reg64[RSP] = newRSP
	c.seg[SS] = newSS
	c.segBase[SS] = 0
	if newCPL == 3 {
		c.segAccess[SS] = 0xF2 // user data, P=1, S=1, W=1, DPL=3
	} else {
		c.segAccess[SS] = 0x92 // kernel data, P=1, S=1, W=1, DPL=0
	}
	c.recomputeMode()
	return nil
}

// opIRETlegacy implements IRET for real / 16-bit / 32-bit modes. It's
// called by opIRETQ when c.mode != ModeLong64.
//
// Stack frame layout (popped low-to-high):
//
//	real / pm16: IP (2), CS (2), FLAGS (2)
//	pm32 same-CPL / VM=0: EIP (4), CS (4), EFLAGS (4)
//	pm32 + CPL change or VM=1: EIP, CS, EFLAGS, ESP, SS (5×4)
//
// SeaBIOS uses real-mode IRET extensively (for INT 10h / INT 13h
// callbacks); we focus on the same-CPL paths here. CPL-changing IRET
// in pm32 is a separate beast and would need its own implementation
// when a guest exercises it.
func (c *CPU) opIRETlegacy(operandSize uint8) error {
	// Per Intel SDM Vol 2A IRET pseudocode: the pop width is OPERAND
	// SIZE — the 0x66 prefix promotes real-mode IRET to IRETD (3×4-byte
	// pops) and demotes pm32 IRET to 16-bit (3×2-byte pops). Using the
	// stack-segment D-bit here was wrong: SeaBIOS issues `66 cf` in real
	// mode for its 32-bit pm32 thunks, and we'd only pop 6 bytes instead
	// of 12, leaving 6 stale bytes that subsequent CALL/RET pairs read
	// as garbage return addresses.
	slotSize := int(operandSize)

	newIP := c.popStack(slotSize)
	newCS := c.popStack(slotSize) & 0xFFFF // only low 16 are the selector
	newFlags := c.popStack(slotSize)

	c.seg[CS] = uint16(newCS)
	if c.cr[0]&CR0_PE == 0 {
		// Real mode: CS.base = sel<<4 only. CS.limit and access stay
		// at whatever the descriptor cache holds — preserving big-real-
		// mode if a prior PE excursion set CSlim > 0xFFFF.
		c.segBase[CS] = uint64(newCS) << 4
	} else {
		// Protected mode: walk the GDT to load the new descriptor.
		// (Real BIOSes occasionally IRET from pm32 to pm32 same-CPL —
		// SeaBIOS doesn't, but the door is open.) For now we trust the
		// existing CS access cache and just update the selector — the
		// descriptor walk can come when needed.
	}
	c.rip = newIP

	// Flag-mask semantics: VIF/VIP/IOPL only updatable at CPL=0;
	// otherwise preserved. For real mode (CPL=0 always), all flags
	// update freely. Sticky reserved bit 1 stays 1.
	mask := uint64(ValidFlagMask)
	if slotSize == 2 {
		mask &= 0xFFFF
	} else if slotSize == 4 {
		mask &= 0xFFFFFFFF
	}
	c.rflags = (c.rflags &^ mask) | (newFlags & mask &^ RFLAGS_RF) | 2
	c.recomputeMode()
	return nil
}
