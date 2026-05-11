package x86

import (
	"fmt"
	"os"
)

// pfDebug controls whether page-fault diagnostics are printed to stderr. It is
// initialized once from TINYEMU_X86_PF_DEBUG=1 and remains off by default to
// avoid flooding output during normal kernel paging activity (COW, demand
// paging, etc.).
var pfDebug = os.Getenv("TINYEMU_X86_PF_DEBUG") == "1"

// ud2SkipAndLog turns the #UD fault path into "log + skip the UD2", which
// lets us see whether the kernel's BUG sites are warnings (it would normally
// continue) or hard BUGs (it would have panicked). Enable with
// TINYEMU_X86_SKIP_UD2=1.
var ud2SkipAndLog = os.Getenv("TINYEMU_X86_SKIP_UD2") == "1"

// ud2LogAlways logs every UD2 site even when it's delivered normally as #UD.
// Enable with TINYEMU_X86_LOG_UD2=1. Useful to see which BUG/WARN sites the
// kernel hits without changing fault semantics.
var ud2LogAlways = os.Getenv("TINYEMU_X86_LOG_UD2") == "1"

// x87Trace logs every x87 instruction. Enable with TINYEMU_X86_X87_TRACE=1.
var x87Trace = os.Getenv("TINYEMU_X86_X87_TRACE") == "1"

// ud2Log prints diagnostic information about a UD2 site: the EIP, the bytes
// immediately following (which may contain inline metadata), and the current
// register snapshot.
func ud2Log(c *CPU, eip uint32) {
	// Read 24 bytes around EIP via the page tables.
	pde := c.readPhys32(c.cr[3] + (eip>>22)*4)
	if pde&1 == 0 {
		fmt.Fprintf(os.Stderr, "[UD2] EIP=0x%08X (no mapping)\n", eip)
		return
	}
	var phys uint32
	if pde&0x80 != 0 {
		phys = (pde & 0xFFC00000) | (eip & 0x3FFFFF)
	} else {
		pte := c.readPhys32((pde &^ 0xFFF) + ((eip>>12)&0x3FF)*4)
		if pte&1 == 0 {
			fmt.Fprintf(os.Stderr, "[UD2] EIP=0x%08X (pte missing)\n", eip)
			return
		}
		phys = (pte &^ 0xFFF) | (eip & 0xFFF)
	}
	fmt.Fprintf(os.Stderr, "[UD2] step=%d EIP=0x%08X (phys 0x%08X) bytes:", c.cycles, eip, phys)
	for i := uint32(0); i < 24; i++ {
		addr := phys + i
		b := uint8(c.readPhys32(addr&^uint32(3)) >> ((addr & 3) * 8))
		fmt.Fprintf(os.Stderr, " %02X", b)
	}
	fmt.Fprintf(os.Stderr, "\n[UD2]   EAX=%08X EBX=%08X ECX=%08X EDX=%08X ESI=%08X EDI=%08X EBP=%08X ESP=%08X EFLAGS=%08X\n",
		c.GetReg32(EAX), c.GetReg32(EBX), c.GetReg32(ECX), c.GetReg32(EDX),
		c.GetReg32(ESI), c.GetReg32(EDI), c.GetReg32(EBP), c.GetReg32(ESP), c.eflags)
	// Dump 6 return-address slots from the current stack to help unwinding.
	esp := c.GetReg32(ESP)
	fmt.Fprintf(os.Stderr, "[UD2]   stack(ESP):")
	for i := uint32(0); i < 8; i++ {
		defer func() { _ = recover() }()
		v := c.readPhys32(c.translateAddress(esp+i*4, false, false, false))
		fmt.Fprintf(os.Stderr, " %08X", v)
	}
	fmt.Fprintln(os.Stderr)
}

// intDebug controls whether each interrupt/exception service is logged.
// Enabled with TINYEMU_X86_INT_DEBUG=1.
var intDebug = os.Getenv("TINYEMU_X86_INT_DEBUG") == "1"

// espTrace controls whether every change to ESP (or SP via SetReg16) is
// logged with old/new values plus a Go stack trace, so we can pinpoint which
// instruction handler corrupts ESP. Enabled with TINYEMU_X86_ESP_DEBUG=1.
// Use "jumps" instead of "1" to suppress normal ±4 push/pop deltas and only
// log non-stack-shaped ESP changes (much less noise).
var espTrace = func() bool {
	v := os.Getenv("TINYEMU_X86_ESP_DEBUG")
	return v == "1" || v == "jumps" || v == "alien"
}()

// physWatchLo, physWatchHi: when both are non-zero, every writePhys* in
// [physWatchLo, physWatchHi) is logged with EIP, value, and size. Configured
// from TINYEMU_X86_PHYSWATCH=lo:hi (hex).
var physWatchLo, physWatchHi uint32 = func() (uint32, uint32) {
	s := os.Getenv("TINYEMU_X86_PHYSWATCH")
	if s == "" {
		return 0, 0
	}
	var lo, hi uint32
	if _, err := fmt.Sscanf(s, "%x:%x", &lo, &hi); err != nil {
		return 0, 0
	}
	return lo, hi
}()

// physWatchHook is invoked from each writePhys* path. If the address is in
// the configured watch range, log the write.
func (c *CPU) physWatchHook(addr, val uint32, size int) {
	if physWatchLo == 0 && physWatchHi == 0 {
		return
	}
	if addr < physWatchLo || addr >= physWatchHi {
		return
	}
	fmt.Fprintf(os.Stderr, "[physwatch] cycles=%d EIP=0x%08X write phys=0x%08X size=%d val=0x%08X\n",
		c.cycles, c.eip, addr, size, val)
}

// pagingEnabled returns true if paging is enabled (CR0.PG = 1).
func (c *CPU) pagingEnabled() bool {
	return c.cr[0]&CR0_PG != 0
}

// pageFaultError is used to signal a page fault from deep inside memory access.
type pageFaultError struct {
	addr      uint32
	errorCode uint32
}

// stackFaultError is used to signal a stack segment fault (#SS).
type stackFaultError struct {
	errorCode uint32
}

// generalProtectionFaultError is used to signal a general protection fault (#GP).
type generalProtectionFaultError struct {
	errorCode uint32
}

// invalidOpcodeError is used to signal #UD (vector 6) — raised on UD2 and on
// genuinely unimplemented opcodes that the kernel may handle via its fault
// path. No error code is pushed for #UD.
type invalidOpcodeError struct{}

// divideError is used to signal #DE (vector 0) — raised on integer divide
// by zero and on overflow during signed division.
type divideError struct{}

// pageFaultFlags carries the dimensions used to construct the #PF error code.
//
//	bit 0 (P) is set when the violation is a permission/type check (not used
//	          for non-present pages, which leave it cleared);
//	bit 1 (W/R)  set on write accesses;
//	bit 2 (U/S)  set on user-mode accesses;
//	bit 3 (RSVD) set when a reserved bit in a paging-structure entry was set;
//	bit 4 (I/D)  set when the faulting access was an instruction fetch.
type pageFaultFlags struct {
	write    bool
	user     bool
	fetch    bool
	reserved bool
	// present indicates whether the violation was on a present mapping
	// (P bit of error code = 1) versus a non-present one.
	present bool
}

// raisePageFault panics with a pageFaultError so that Step() can catch it and
// raise #PF. The panic is the cleanest way to abort an instruction mid-execution.
func (c *CPU) raisePageFault(addr uint32, f pageFaultFlags) {
	code := uint32(0)
	if f.present {
		code |= 0x01
	}
	if f.write {
		code |= 0x02
	}
	if f.user {
		code |= 0x04
	}
	if f.reserved {
		code |= 0x08
	}
	if f.fetch {
		code |= 0x10
	}
	if pfDebug {
		// Walk page tables for diagnostic context.
		pdeAddr := c.cr[3] + (addr>>22)*4
		pde := c.readPhys32(pdeAddr)
		pteVal, pteAddr := uint32(0), uint32(0)
		if pde&1 != 0 && pde&0x80 == 0 {
			pteAddr = (pde &^ uint32(0xFFF)) + ((addr>>12)&0x3FF)*4
			pteVal = c.readPhys32(pteAddr)
		}
		fmt.Fprintf(os.Stderr,
			"[PF] step=%d linear=%08X w=%v u=%v fetch=%v rsvd=%v "+
				"CR3=%08X CR0=%08X CR4=%08X EIP=%08X ESP=%08X "+
				"PDE@%08X=%08X PTE@%08X=%08X\n",
			c.cycles, addr, f.write, f.user, f.fetch, f.reserved,
			c.cr[3], c.cr[0], c.cr[4], c.eip, c.GetReg32(ESP),
			pdeAddr, pde, pteAddr, pteVal)
		// Dump 32 bytes of instruction stream around EIP and a full register
		// snapshot — exact bytes at the moment of fault. Use physical access
		// via the existing CR3/PDE/PTE walk for the EIP page.
		eipPDE := c.readPhys32(c.cr[3] + (c.eip>>22)*4)
		var eipPhys uint32 = 0xFFFFFFFF
		if eipPDE&1 != 0 {
			if eipPDE&0x80 != 0 {
				eipPhys = (eipPDE & 0xFFC00000) | (c.eip & 0x3FFFFF)
			} else {
				eipPTE := c.readPhys32((eipPDE &^ uint32(0xFFF)) + ((c.eip>>12)&0x3FF)*4)
				if eipPTE&1 != 0 {
					eipPhys = (eipPTE &^ uint32(0xFFF)) | (c.eip & 0xFFF)
				}
			}
		}
		if eipPhys != 0xFFFFFFFF {
			// c.eip is post-fetch; show bytes from EIP-8 through EIP+15 so the
			// actual faulting instruction is visible (it ends at c.eip).
			fmt.Fprintf(os.Stderr, "[PF]   code @ EIP-8..EIP+15 around 0x%08X:", c.eip)
			for i := int32(-8); i < 16; i++ {
				addr := uint32(int32(eipPhys) + i)
				b := uint8(c.readPhys32(addr&^uint32(3)) >> ((addr & 3) * 8))
				if i == 0 {
					fmt.Fprintf(os.Stderr, " |")
				}
				fmt.Fprintf(os.Stderr, " %02X", b)
			}
			fmt.Fprintln(os.Stderr)
		}
		fmt.Fprintf(os.Stderr,
			"[PF]   EAX=%08X EBX=%08X ECX=%08X EDX=%08X ESI=%08X EDI=%08X EBP=%08X ESP=%08X EFLAGS=%08X\n",
			c.GetReg32(EAX), c.GetReg32(EBX), c.GetReg32(ECX), c.GetReg32(EDX),
			c.GetReg32(ESI), c.GetReg32(EDI), c.GetReg32(EBP), c.GetReg32(ESP), c.eflags)
		// Walk 8 stack slots so we can read the caller chain. Skip if we're
		// already inside a PF dump to avoid infinite recursion when the stack
		// itself is unmapped.
		if !c.pfDumpActive {
			c.pfDumpActive = true
			fmt.Fprintf(os.Stderr, "[PF]   stack(ESP):")
			esp := c.GetReg32(ESP)
			for i := uint32(0); i < 8; i++ {
				func() {
					defer func() { _ = recover() }()
					v := c.readPhys32(c.translateAddress(esp+i*4, false, false, false))
					fmt.Fprintf(os.Stderr, " %08X", v)
				}()
			}
			fmt.Fprintln(os.Stderr)
			c.pfDumpActive = false
		}
	}
	panic(pageFaultError{addr: addr, errorCode: code})
}

// raiseStackFault panics with a stackFaultError so that Step() can catch it
// and raise #SS (vector 0x0C).
func (c *CPU) raiseStackFault(errorCode uint32) {
	panic(stackFaultError{errorCode: errorCode})
}

// raiseGeneralProtectionFault panics with a generalProtectionFaultError so that
// Step() can catch it and raise #GP (vector 0x0D).
func (c *CPU) raiseGeneralProtectionFault(errorCode uint32) {
	panic(generalProtectionFaultError{errorCode: errorCode})
}

// checkStackLimit verifies that a stack access of the given size at the given
// offset is within the stack segment bounds. If not, it raises #SS.
func (c *CPU) checkStackLimit(offset uint32, size uint32) {
	limit := c.segLimit[SS]
	access := c.segAccess[SS]
	segType := access & 0x0F
	isExpandDown := (segType&0x04 != 0) && (segType&0x08 == 0)
	is32Bit := (access>>8)&0x04 != 0 // B-bit in flags

	if isExpandDown {
		var max uint32
		if is32Bit {
			max = 0xFFFFFFFF
		} else {
			max = 0xFFFF
		}
		// For expand-down, valid range is (limit+1) to max.
		if offset <= limit || offset > max || offset+size-1 > max {
			c.raiseStackFault(0)
		}
	} else {
		// For expand-up, valid range is 0 to limit.
		if offset > limit || offset+size-1 > limit {
			c.raiseStackFault(0)
		}
	}
}

// refreshPDPTEs reloads the four PDPT entries pointed to by CR3 (aligned to
// 32 bytes). Called when CR3, or the CR0.PG / CR4.PAE state, changes while
// PAE paging is or becomes active.
func (c *CPU) refreshPDPTEs() {
	base := c.cr[3] &^ uint32(0x1F)
	for i := uint32(0); i < 4; i++ {
		c.pdpte[i] = c.readPhys64(base + i*8)
	}
}

// translatePAE walks the 3-level PAE page-table tree (PDPT -> PD -> PT) and
// returns the physical address for `lin`. Supports 4KB and 2MB pages.
func (c *CPU) translatePAE(lin uint32, write, user, fetch bool) uint32 {
	pdptIdx := (lin >> 30) & 0x3
	pdpte := c.pdpte[pdptIdx]
	if pdpte&1 == 0 {
		c.raisePageFault(lin, pageFaultFlags{write: write, user: user, fetch: fetch})
	}

	pdAddr := uint32(pdpte&0xFFFFF000) + ((lin>>21)&0x1FF)*8
	pde := c.readPhys64(pdAddr)
	if pde&1 == 0 {
		c.raisePageFault(lin, pageFaultFlags{write: write, user: user, fetch: fetch})
	}

	// NX (bit 63): instruction-fetch protection. Only honored if EFER.NXE=1.
	nxe := c.efer&(1<<11) != 0

	// 2 MB page when PDE.PS (bit 7) is set.
	if pde&0x80 != 0 {
		c.checkPAEPerms(lin, pdpte, pde, 0, write, user, fetch, nxe, true)
		// A/D bits.
		if pde&0x20 == 0 || (write && pde&0x40 == 0) {
			c.writePhys64(pdAddr, pde|0x20|writeDirtyMask(write))
		}
		phys := uint32(pde&0xFFE00000) | (lin & 0x1FFFFF)
		c.tlb.insert(lin, phys,
			pde&0x2 != 0,
			pde&0x4 != 0,
			nxe && pde>>63 != 0,
			pde&0x100 != 0 && c.cr[4]&CR4_PGE != 0,
		)
		return phys
	}

	ptAddr := uint32(pde&0xFFFFF000) + ((lin>>12)&0x1FF)*8
	pte := c.readPhys64(ptAddr)
	if pte&1 == 0 {
		c.raisePageFault(lin, pageFaultFlags{write: write, user: user, fetch: fetch})
	}

	c.checkPAEPerms(lin, pdpte, pde, pte, write, user, fetch, nxe, false)

	// A/D bits on each level.
	if pde&0x20 == 0 {
		c.writePhys64(pdAddr, pde|0x20)
	}
	newPte := pte | 0x20
	if write {
		newPte |= 0x40
	}
	if newPte != pte {
		c.writePhys64(ptAddr, newPte)
	}

	phys := uint32(pte&0xFFFFF000) | (lin & 0xFFF)
	c.tlb.insert(lin, phys,
		(pde&pte)&0x2 != 0,
		(pde&pte)&0x4 != 0,
		nxe && (pde|pte)>>63 != 0,
		pte&0x100 != 0 && c.cr[4]&CR4_PGE != 0,
	)
	return phys
}

// writeDirtyMask returns the D-bit value if `write` is true, else 0.
func writeDirtyMask(write bool) uint64 {
	if write {
		return 0x40
	}
	return 0
}

// checkPAEPerms enforces R/W and U/S checks across the page-table hierarchy
// and NX for fetches. Raises #PF with the present bit set on violation.
func (c *CPU) checkPAEPerms(lin uint32, pdpte, pde, pte uint64, write, user, fetch, nxe, largePage bool) {
	combined := pde
	if !largePage {
		combined &= pte
	}
	// PDPTE in PAE legacy mode does not have U/S or R/W bits; treat as
	// permissive.
	_ = pdpte

	if write {
		// Bit 1 = R/W. 0 means read-only.
		if combined&0x2 == 0 {
			if user || (c.cr[0]&CR0_WP != 0) {
				c.raisePageFault(lin, pageFaultFlags{write: write, user: user, fetch: fetch, present: true})
			}
		}
	}
	if user {
		// Bit 2 = U/S. 0 means supervisor only.
		if combined&0x4 == 0 {
			c.raisePageFault(lin, pageFaultFlags{write: write, user: user, fetch: fetch, present: true})
		}
	}
	if fetch && nxe {
		nx := pde >> 63 & 1
		if !largePage {
			nx |= pte >> 63 & 1
		}
		if nx != 0 {
			c.raisePageFault(lin, pageFaultFlags{write: write, user: user, fetch: fetch, present: true})
		}
	}
}

// translateAddress converts a linear address to a physical address using the
// current page tables. If the translation fails, it calls raisePageFault. The
// fetch flag indicates the access is an instruction fetch (for #PF I/D bit).
func (c *CPU) translateAddress(lin uint32, write, user, fetch bool) uint32 {
	if !c.pagingEnabled() {
		return lin
	}
	// TLB fast path. A hit means the cached translation was valid at some
	// point and we may use it even if the underlying PTE has since been
	// cleared (real CPUs do exactly this).
	if phys, hit := c.tlb.lookup(lin, write, user, fetch); hit {
		return phys
	}
	if c.cr[4]&CR4_PAE != 0 {
		return c.translatePAE(lin, write, user, fetch)
	}

	// 32-bit two-level paging (no PAE).
	pdeAddr := c.cr[3] + (lin>>22)*4
	pde := c.readPhys32(pdeAddr)

	if pde&1 == 0 {
		if pfDebug {
			fmt.Fprintf(os.Stderr, "[PF-DEBUG] lin=%08X PDE not present: pdeAddr=%08X pde=%08X\n", lin, pdeAddr, pde)
		}
		c.raisePageFault(lin, pageFaultFlags{write: write, user: user, fetch: fetch})
	}

	// 4 MB page (PSE).
	if pde&0x80 != 0 && c.cr[4]&CR4_PSE != 0 {
		phys := (pde & 0xFFC00000) | (lin & 0x3FFFFF)
		c.tlb.insert(lin, phys,
			pde&0x02 != 0,
			pde&0x04 != 0,
			false,
			pde&0x100 != 0 && c.cr[4]&CR4_PGE != 0,
		)
		return phys
	}

	pteAddr := (pde &^ uint32(0xFFF)) + ((lin>>12)&0x3FF)*4
	pte := c.readPhys32(pteAddr)

	if pte&1 == 0 {
		if pfDebug {
			fmt.Fprintf(os.Stderr, "[PF-DEBUG] lin=%08X PTE not present: pde=%08X pteAddr=%08X pte=%08X\n", lin, pde, pteAddr, pte)
		}
		c.raisePageFault(lin, pageFaultFlags{write: write, user: user, fetch: fetch})
	}

	// Permission check.
	// combined has a bit set wherever PDE or PTE denies the access.
	combined := ^pte | ^pde
	writeMask := uint32(0)
	if write {
		writeMask = 0x02
	}
	if combined&writeMask != 0 {
		// Supervisor with CR0.WP=0 can write to read-only pages.
		if user || (c.cr[0]&CR0_WP != 0) {
			c.raisePageFault(lin, pageFaultFlags{write: write, user: user, fetch: fetch, present: true})
		}
	}
	userMask := uint32(0)
	if user {
		userMask = 0x04
	}
	if combined&userMask != 0 {
		c.raisePageFault(lin, pageFaultFlags{write: write, user: user, fetch: fetch, present: true})
	}

	// Update accessed/dirty bits.
	if pde&0x20 == 0 {
		c.writePhys32(pdeAddr, pde|0x20)
	}
	newPte := pte | 0x20
	if write {
		newPte |= 0x40
	}
	if newPte != pte {
		c.writePhys32(pteAddr, newPte)
	}

	phys := (pte &^ uint32(0xFFF)) | (lin & 0xFFF)
	c.tlb.insert(lin, phys,
		(pde&pte)&0x02 != 0,
		(pde&pte)&0x04 != 0,
		false,
		pte&0x100 != 0 && c.cr[4]&CR4_PGE != 0,
	)
	return phys
}
