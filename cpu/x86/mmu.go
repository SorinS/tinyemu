package x86

// pagingEnabled returns true if paging is enabled (CR0.PG = 1).
func (c *CPU) pagingEnabled() bool {
	return c.cr[0]&CR0_PG != 0
}

// pageFaultError is used to signal a page fault from deep inside memory access.
type pageFaultError struct {
	addr      uint32
	errorCode uint32
}

// raisePageFault panics with a pageFaultError so that Step() can catch it and
// raise #PF. The panic is the cleanest way to abort an instruction mid-execution.
func (c *CPU) raisePageFault(addr uint32, write, user bool) {
	code := uint32(0)
	if write {
		code |= 0x02
	}
	if user {
		code |= 0x04
	}
	panic(pageFaultError{addr: addr, errorCode: code})
}

// translateAddress converts a linear address to a physical address using the
// current page tables. If the translation fails, it calls raisePageFault.
func (c *CPU) translateAddress(lin uint32, write, user bool) uint32 {
	if !c.pagingEnabled() {
		return lin
	}

	// 32-bit two-level paging (no PAE).
	pdeAddr := c.cr[3] + (lin>>22)*4
	pde := c.readPhys32(pdeAddr)

	if pde&1 == 0 {
		c.raisePageFault(lin, write, user)
	}

	// 4 MB page (PSE).
	if pde&0x80 != 0 && c.cr[4]&CR4_PSE != 0 {
		phys := (pde & 0xFFC00000) | (lin & 0x3FFFFF)
		return phys
	}

	pteAddr := (pde &^ uint32(0xFFF)) + ((lin>>12)&0x3FF)*4
	pte := c.readPhys32(pteAddr)

	if pte&1 == 0 {
		c.raisePageFault(lin, write, user)
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
			c.raisePageFault(lin, write, user)
		}
	}
	userMask := uint32(0)
	if user {
		userMask = 0x04
	}
	if combined&userMask != 0 {
		c.raisePageFault(lin, write, user)
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
	return phys
}
