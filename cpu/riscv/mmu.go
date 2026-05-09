package riscv

import (
	"encoding/binary"
)

// AccessType represents the type of memory access
type AccessType int

const (
	AccessRead AccessType = iota
	AccessWrite
	AccessCode
)

// Page table entry flags (for Sv39/Sv48)
const (
	PTEValid    = 1 << 0
	PTERead     = 1 << 1
	PTEWrite    = 1 << 2
	PTEExecute  = 1 << 3
	PTEUser     = 1 << 4
	PTEGlobal   = 1 << 5
	PTEAccessed = 1 << 6
	PTEDirty    = 1 << 7
)

// GetSatpMode extracts the address translation mode from SATP
func (c *CPU) GetSatpMode() int {
	if c.CurXLEN == XLEN32 {
		return int(c.Satp >> 31)
	}
	return int(c.Satp >> 60)
}

// GetSatpPPN extracts the physical page number from SATP
func (c *CPU) GetSatpPPN() uint64 {
	if c.CurXLEN == XLEN32 {
		return c.Satp & 0x3FFFFF
	}
	return c.Satp & 0x0FFFFFFFFFFF
}

// IsMmuEnabled returns true if address translation is enabled for the current mode
func (c *CPU) IsMmuEnabled(accessType AccessType) bool {
	if c.Priv == PrivMachine {
		// Check MPRV for loads/stores in M-mode
		if accessType != AccessCode && (c.Mstatus&MstatusMPRV) != 0 {
			mpp := (c.Mstatus >> MstatusMPPShift) & 3
			if mpp != PrivMachine {
				return c.GetSatpMode() != SatpModeBare
			}
		}
		return false
	}
	return c.GetSatpMode() != SatpModeBare
}

// GetEffectivePrivForAccess returns the effective privilege level for a memory access
func (c *CPU) GetEffectivePrivForAccess(accessType AccessType) uint8 {
	if c.Priv == PrivMachine && accessType != AccessCode && (c.Mstatus&MstatusMPRV) != 0 {
		return uint8((c.Mstatus >> MstatusMPPShift) & 3)
	}
	return c.Priv
}

// TLBLookup performs a TLB lookup for the given address
func (c *CPU) TLBLookup(vaddr uint64, accessType AccessType) ([]byte, bool) {
	idx := (vaddr >> PageShift) & (TLBSize - 1)
	pageAddr := vaddr &^ uint64(PageMask)

	var tlb *[TLBSize]TLBEntry
	switch accessType {
	case AccessRead:
		tlb = &c.TLBRead
	case AccessWrite:
		tlb = &c.TLBWrite
	case AccessCode:
		tlb = &c.TLBCode
	}

	entry := &tlb[idx]
	// Check for TLB hit - VAddr == ^uint64(0) indicates invalid entry (set by FlushTLB)
	// Reference: riscv_cpu_priv.h lines 257, 275 - C only checks vaddr match
	if entry.VAddr == pageAddr {
		// TLB hit - calculate physical address
		paddr := uint64(int64(vaddr) + entry.MemAddend)
		offset := vaddr & PageMask
		if c.Mem != nil {
			pr := c.Mem.GetRange(paddr)
			if pr != nil && pr.IsRAM {
				localOffset := paddr - pr.Addr
				if localOffset+PageSize <= uint64(len(pr.PhysMem)) {
					return pr.PhysMem[localOffset+offset:], true
				}
			}
		}
	}

	return nil, false
}

// FetchInstruction fetches an instruction at the current PC
// Returns the instruction word and any error
// Reference: tinyemu-2019-12-21/riscv_cpu.c:539-585 (target_read_insn_slow, target_read_insn_u16)
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:272-281 (page-crossing instruction handling)
func (c *CPU) FetchInstruction() (uint32, error) {
	pc := c.PC

	// Check if instruction might cross a page boundary
	// Reference: riscv_cpu_template.h:272-281
	// If PC is within 2 bytes of page end, the 32-bit instruction could span pages
	offsetInPage := pc & PageMask
	if offsetInPage >= PageSize-2 {
		return c.fetchInstructionPageCrossing(pc)
	}

	// Fast path - instruction is fully within a single page
	if !c.IsMmuEnabled(AccessCode) {
		// No address translation - direct physical access
		return c.fetchInstructionPhys(pc)
	}

	// Address translation enabled - use TLB or walk page table
	idx := (pc >> PageShift) & (TLBSize - 1)
	pageAddr := pc &^ uint64(PageMask)

	entry := &c.TLBCode[idx]
	if entry.VAddr == pageAddr {
		// TLB hit
		return c.fetchFromTLBEntry(pc, entry)
	}

	// TLB miss - walk page table
	paddr, err := c.TranslateAddress(pc, AccessCode)
	if err != nil {
		return 0, err
	}

	return c.fetchInstructionPhys(paddr)
}

// fetchInstructionPageCrossing handles instruction fetch when PC is near page boundary
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:272-281
func (c *CPU) fetchInstructionPageCrossing(pc uint64) (uint32, error) {
	// Fetch first 16 bits
	insn, err := c.fetchInsn16(pc)
	if err != nil {
		return 0, err
	}

	// Check if this is a compressed instruction (bits 0-1 != 11)
	if (insn & 0x3) != 0x3 {
		// Compressed 16-bit instruction
		return uint32(insn), nil
	}

	// 32-bit instruction spanning page boundary - fetch second half
	// Reference: riscv_cpu_template.h:278-280
	insnHigh, err := c.fetchInsn16(pc + 2)
	if err != nil {
		return 0, err
	}

	return uint32(insn) | (uint32(insnHigh) << 16), nil
}

// fetchInsn16 fetches a 16-bit instruction half at a virtual address
// Reference: tinyemu-2019-12-21/riscv_cpu.c:570-586 (target_read_insn_u16)
func (c *CPU) fetchInsn16(vaddr uint64) (uint16, error) {
	if !c.IsMmuEnabled(AccessCode) {
		// No address translation - direct physical access
		return c.fetchInsn16Phys(vaddr)
	}

	// Try TLB first
	idx := (vaddr >> PageShift) & (TLBSize - 1)
	pageAddr := vaddr &^ uint64(PageMask)

	entry := &c.TLBCode[idx]
	if entry.VAddr == pageAddr {
		// TLB hit
		paddr := uint64(int64(vaddr) + entry.MemAddend)
		return c.fetchInsn16Phys(paddr)
	}

	// TLB miss - translate
	paddr, err := c.TranslateAddress(vaddr, AccessCode)
	if err != nil {
		return 0, err
	}

	return c.fetchInsn16Phys(paddr)
}

// fetchInsn16Phys fetches a 16-bit instruction half from a physical address
func (c *CPU) fetchInsn16Phys(paddr uint64) (uint16, error) {
	pr := c.Mem.GetRange(paddr)
	if pr == nil {
		c.SetPendingException(CauseFaultFetch, paddr)
		return 0, errFetchFault
	}

	offset := paddr - pr.Addr
	if pr.IsRAM && offset+2 <= uint64(len(pr.PhysMem)) {
		return binary.LittleEndian.Uint16(pr.PhysMem[offset:]), nil
	}

	c.SetPendingException(CauseFaultFetch, paddr)
	return 0, errFetchFault
}

// fetchFromTLBEntry reads an instruction using a TLB entry
func (c *CPU) fetchFromTLBEntry(vaddr uint64, entry *TLBEntry) (uint32, error) {
	// Calculate physical address from TLB entry
	paddr := uint64(int64(vaddr) + entry.MemAddend)
	return c.fetchInstructionPhys(paddr)
}

// fetchInstructionPhys fetches an instruction from a physical address
// Reference: tinyemu-2019-12-21/riscv_cpu.c:529-536 (get_insn32)
func (c *CPU) fetchInstructionPhys(paddr uint64) (uint32, error) {
	pr := c.Mem.GetRange(paddr)
	if pr == nil {
		c.SetPendingException(CauseFaultFetch, paddr)
		return 0, errFetchFault
	}

	offset := paddr - pr.Addr

	if pr.IsRAM {
		// Fast path for RAM
		if offset+4 <= uint64(len(pr.PhysMem)) {
			return binary.LittleEndian.Uint32(pr.PhysMem[offset:]), nil
		} else if offset+2 <= uint64(len(pr.PhysMem)) {
			// Might be compressed instruction at page boundary
			insn := uint32(binary.LittleEndian.Uint16(pr.PhysMem[offset:]))
			if (insn & 0x3) != 0x3 {
				// Compressed instruction
				return insn, nil
			}
			// Need second half from next page
			c.SetPendingException(CauseFaultFetch, paddr)
			return 0, errFetchFault
		}
	}

	c.SetPendingException(CauseFaultFetch, paddr)
	return 0, errFetchFault
}

// TranslateAddress translates a virtual address to physical
// This is a simplified version - full implementation needs page table walk
func (c *CPU) TranslateAddress(vaddr uint64, accessType AccessType) (uint64, error) {
	if !c.IsMmuEnabled(accessType) {
		return vaddr, nil // No translation
	}

	mode := c.GetSatpMode()

	switch mode {
	case SatpModeBare:
		return vaddr, nil
	case SatpModeSv32:
		// Sv32 is only valid for RV32
		if c.CurXLEN != XLEN32 {
			c.SetPendingException(c.getPageFaultCause(accessType), vaddr)
			return 0, errPageFault
		}
		return c.translateSv32(vaddr, accessType)
	case SatpModeSv39:
		return c.translateSv39(vaddr, accessType)
	case SatpModeSv48:
		return c.translateSv48(vaddr, accessType)
	default:
		// Unsupported mode
		c.SetPendingException(c.getPageFaultCause(accessType), vaddr)
		return 0, errPageFault
	}
}

// translateSv32 performs Sv32 address translation for RV32
// Reference: tinyemu-2019-12-21/riscv_cpu.c:212-224, 241-292 (get_phys_addr function)
// Sv32 uses:
// - 2-level page table (vs 3-4 for Sv39/Sv48)
// - 4-byte PTEs (vs 8-byte)
// - 22-bit PPN, 10-bit VPN per level
// - 34-bit physical address
func (c *CPU) translateSv32(vaddr uint64, accessType AccessType) (uint64, error) {
	ppn := c.GetSatpPPN() // Returns 22-bit PPN for XLEN32

	// VPN[0]: bits 12-21, VPN[1]: bits 22-31
	vpn := [2]uint64{
		(vaddr >> 12) & 0x3FF, // VPN[0]: 10 bits
		(vaddr >> 22) & 0x3FF, // VPN[1]: 10 bits
	}

	// Start page table walk
	pageTableAddr := ppn << PageShift
	levels := 2
	var pte uint64
	var a int

	// Reference: riscv_cpu.c:244-292 (page table walk loop)
	for a = levels - 1; a >= 0; a-- {
		// PTEs are 4 bytes in Sv32
		pteAddr := pageTableAddr + vpn[a]*4

		// Read PTE (32-bit for Sv32)
		pteVal, err := c.Mem.Read32(pteAddr)
		if err != nil {
			c.SetPendingException(c.getPageFaultCause(accessType), vaddr)
			return 0, errPageFault
		}
		pte = uint64(pteVal)

		// Check valid bit
		if pte&PTEValid == 0 {
			c.SetPendingException(c.getPageFaultCause(accessType), vaddr)
			return 0, errPageFault
		}

		// Check if leaf PTE (xwr != 0)
		xwr := (pte >> 1) & 7
		if xwr != 0 {
			// Reject invalid xwr combinations (write-only or write+execute)
			if xwr == 2 || xwr == 6 {
				c.SetPendingException(c.getPageFaultCause(accessType), vaddr)
				return 0, errPageFault
			}
			break // Leaf page found
		}

		// Non-leaf, continue to next level
		// In Sv32, PPN is bits 31:10 of PTE (22 bits)
		pageTableAddr = ((pte >> 10) & 0x3FFFFF) << PageShift
	}

	if a < 0 {
		// No leaf found
		c.SetPendingException(c.getPageFaultCause(accessType), vaddr)
		return 0, errPageFault
	}

	// Check permissions
	if !c.checkPTEPermissions(pte, accessType) {
		c.SetPendingException(c.getPageFaultCause(accessType), vaddr)
		return 0, errPageFault
	}

	// Update A/D bits if needed
	neededBits := uint64(PTEAccessed)
	if accessType == AccessWrite {
		neededBits |= PTEDirty
	}
	if pte&neededBits != neededBits {
		pte |= neededBits
		pteAddr := pageTableAddr + vpn[a]*4
		if err := c.Mem.Write32(pteAddr, uint32(pte)); err != nil {
			c.SetPendingException(c.getPageFaultCause(accessType), vaddr)
			return 0, errPageFault
		}
	}

	// Construct physical address
	// For Sv32, PPN in PTE is 22 bits (bits 31:10)
	// Physical address is 34 bits
	ppnMask := uint64(0)
	for i := 0; i < a; i++ {
		ppnMask |= 0x3FF << (i * 10) // 10 bits per VPN level
	}

	ptePPN := (pte >> 10) & 0x3FFFFF // 22-bit PPN
	vaddrPPN := (vaddr >> 12) & ppnMask
	paddr := ((ptePPN &^ ppnMask) | vaddrPPN) << 12
	paddr |= vaddr & PageMask

	// Update TLB
	c.updateTLB(vaddr, paddr, accessType)

	return paddr, nil
}

// translateSv39 performs Sv39 address translation
// Reference: riscv_cpu.c:188-294 (get_phys_addr function)
func (c *CPU) translateSv39(vaddr uint64, accessType AccessType) (uint64, error) {
	ppn := c.GetSatpPPN()

	// Check canonical address (bits 63:39 must match bit 38)
	// Reference: riscv_cpu.c:235-237
	signBit := (vaddr >> 38) & 1
	high := vaddr >> 39
	if (signBit == 1 && high != 0x1FFFFFF) || (signBit == 0 && high != 0) {
		c.SetPendingException(c.getPageFaultCause(accessType), vaddr)
		return 0, errPageFault
	}

	vpn := [3]uint64{
		(vaddr >> 12) & 0x1FF,
		(vaddr >> 21) & 0x1FF,
		(vaddr >> 30) & 0x1FF,
	}

	// Reference: riscv_cpu.c:241-243
	pageTableAddr := ppn << PageShift
	levels := 3
	var pte uint64
	var a int

	// Reference: riscv_cpu.c:244-292 (page table walk loop)
	for a = levels - 1; a >= 0; a-- {
		pteAddr := pageTableAddr + vpn[a]*8

		// Read PTE - Reference: riscv_cpu.c:248-251
		pteVal, err := c.Mem.Read64(pteAddr)
		if err != nil {
			c.SetPendingException(c.getPageFaultCause(accessType), vaddr)
			return 0, errPageFault
		}
		pte = pteVal

		// Reference: riscv_cpu.c:253-254
		if pte&PTEValid == 0 {
			c.SetPendingException(c.getPageFaultCause(accessType), vaddr)
			return 0, errPageFault
		}

		// Check if leaf PTE (xwr != 0)
		// Reference: riscv_cpu.c:256-257
		xwr := (pte >> 1) & 7
		if xwr != 0 {
			// Reference: riscv_cpu.c:258-259 - reject invalid xwr combinations
			// xwr=2 (write-only) and xwr=6 (write+execute) are invalid
			if xwr == 2 || xwr == 6 {
				c.SetPendingException(c.getPageFaultCause(accessType), vaddr)
				return 0, errPageFault
			}
			break // Leaf page found
		}

		// Non-leaf, continue to next level
		// Reference: riscv_cpu.c:290
		pageTableAddr = ((pte >> 10) & 0xFFFFFFFFFFF) << PageShift
	}

	if a < 0 {
		// No leaf found
		c.SetPendingException(c.getPageFaultCause(accessType), vaddr)
		return 0, errPageFault
	}

	// Check permissions
	// Reference: riscv_cpu.c:260-274
	if !c.checkPTEPermissions(pte, accessType) {
		c.SetPendingException(c.getPageFaultCause(accessType), vaddr)
		return 0, errPageFault
	}

	// Update A/D bits if needed
	// Reference: riscv_cpu.c:275-285
	neededBits := uint64(PTEAccessed)
	if accessType == AccessWrite {
		neededBits |= PTEDirty
	}
	if pte&neededBits != neededBits {
		pte |= neededBits
		pteAddr := pageTableAddr + vpn[a]*8
		if err := c.Mem.Write64(pteAddr, pte); err != nil {
			c.SetPendingException(c.getPageFaultCause(accessType), vaddr)
			return 0, errPageFault
		}
	}

	// Construct physical address
	// Reference: riscv_cpu.c:286-288
	ppnMask := uint64(0)
	for i := 0; i < a; i++ {
		ppnMask |= 0x1FF << (i * 9)
	}

	ptePPN := (pte >> 10) & 0xFFFFFFFFFFF
	vaddrPPN := (vaddr >> 12) & ppnMask
	paddr := ((ptePPN &^ ppnMask) | vaddrPPN) << 12
	paddr |= vaddr & PageMask

	// Update TLB
	c.updateTLB(vaddr, paddr, accessType)

	return paddr, nil
}

// translateSv48 performs Sv48 address translation
// Reference: riscv_cpu.c:188-294 (get_phys_addr function, Sv48 uses same logic with 4 levels)
func (c *CPU) translateSv48(vaddr uint64, accessType AccessType) (uint64, error) {
	ppn := c.GetSatpPPN()

	// Check canonical address (bits 63:48 must match bit 47)
	// Reference: riscv_cpu.c:235-237 (same pattern as Sv39)
	signBit := (vaddr >> 47) & 1
	high := vaddr >> 48
	if (signBit == 1 && high != 0xFFFF) || (signBit == 0 && high != 0) {
		c.SetPendingException(c.getPageFaultCause(accessType), vaddr)
		return 0, errPageFault
	}

	vpn := [4]uint64{
		(vaddr >> 12) & 0x1FF,
		(vaddr >> 21) & 0x1FF,
		(vaddr >> 30) & 0x1FF,
		(vaddr >> 39) & 0x1FF,
	}

	// Reference: riscv_cpu.c:241-243
	pageTableAddr := ppn << PageShift
	levels := 4
	var pte uint64
	var a int

	// Reference: riscv_cpu.c:244-292 (page table walk loop)
	for a = levels - 1; a >= 0; a-- {
		pteAddr := pageTableAddr + vpn[a]*8

		// Read PTE - Reference: riscv_cpu.c:248-251
		pteVal, err := c.Mem.Read64(pteAddr)
		if err != nil {
			c.SetPendingException(c.getPageFaultCause(accessType), vaddr)
			return 0, errPageFault
		}
		pte = pteVal

		// Reference: riscv_cpu.c:253-254
		if pte&PTEValid == 0 {
			c.SetPendingException(c.getPageFaultCause(accessType), vaddr)
			return 0, errPageFault
		}

		// Check if leaf PTE (xwr != 0)
		// Reference: riscv_cpu.c:256-257
		xwr := (pte >> 1) & 7
		if xwr != 0 {
			// Reference: riscv_cpu.c:258-259 - reject invalid xwr combinations
			// xwr=2 (write-only) and xwr=6 (write+execute) are invalid
			if xwr == 2 || xwr == 6 {
				c.SetPendingException(c.getPageFaultCause(accessType), vaddr)
				return 0, errPageFault
			}
			break // Leaf page found
		}

		// Non-leaf, continue to next level
		// Reference: riscv_cpu.c:290
		pageTableAddr = ((pte >> 10) & 0xFFFFFFFFFFF) << PageShift
	}

	if a < 0 {
		c.SetPendingException(c.getPageFaultCause(accessType), vaddr)
		return 0, errPageFault
	}

	// Check permissions
	// Reference: riscv_cpu.c:260-274
	if !c.checkPTEPermissions(pte, accessType) {
		c.SetPendingException(c.getPageFaultCause(accessType), vaddr)
		return 0, errPageFault
	}

	// Update A/D bits
	// Reference: riscv_cpu.c:275-285
	neededBits := uint64(PTEAccessed)
	if accessType == AccessWrite {
		neededBits |= PTEDirty
	}
	if pte&neededBits != neededBits {
		pte |= neededBits
		pteAddr := pageTableAddr + vpn[a]*8
		if err := c.Mem.Write64(pteAddr, pte); err != nil {
			c.SetPendingException(c.getPageFaultCause(accessType), vaddr)
			return 0, errPageFault
		}
	}

	// Construct physical address
	// Reference: riscv_cpu.c:286-288
	ppnMask := uint64(0)
	for i := 0; i < a; i++ {
		ppnMask |= 0x1FF << (i * 9)
	}

	ptePPN := (pte >> 10) & 0xFFFFFFFFFFF
	vaddrPPN := (vaddr >> 12) & ppnMask
	paddr := ((ptePPN &^ ppnMask) | vaddrPPN) << 12
	paddr |= vaddr & PageMask

	c.updateTLB(vaddr, paddr, accessType)

	return paddr, nil
}

// checkPTEPermissions verifies PTE permissions for the given access type
// Reference: riscv_cpu.c:260-274 (privilege and protection checks in get_phys_addr)
func (c *CPU) checkPTEPermissions(pte uint64, accessType AccessType) bool {
	priv := c.GetEffectivePrivForAccess(accessType)

	// Privilege check - Reference: riscv_cpu.c:261-267
	if priv == PrivUser {
		if pte&PTEUser == 0 {
			return false
		}
	} else {
		// Supervisor mode - SUM allows S-mode to access U pages
		if pte&PTEUser != 0 && (c.Mstatus&MstatusSUM) == 0 {
			return false
		}
	}

	// Protection check - Reference: riscv_cpu.c:268-274
	switch accessType {
	case AccessRead:
		// MXR allows reads from executable pages
		// Reference: riscv_cpu.c:269-271
		if pte&PTERead != 0 {
			return true
		}
		if (c.Mstatus&MstatusMXR) != 0 && pte&PTEExecute != 0 {
			return true
		}
		return false
	case AccessWrite:
		return pte&PTEWrite != 0
	case AccessCode:
		return pte&PTEExecute != 0
	}
	return false
}

// updateTLB updates the TLB with a new translation
func (c *CPU) updateTLB(vaddr, paddr uint64, accessType AccessType) {
	idx := (vaddr >> PageShift) & (TLBSize - 1)
	pageVaddr := vaddr &^ uint64(PageMask)
	pagePaddr := paddr &^ uint64(PageMask)

	// Calculate addend for fast translation
	addend := int64(pagePaddr) - int64(pageVaddr)

	var tlb *[TLBSize]TLBEntry
	switch accessType {
	case AccessRead:
		tlb = &c.TLBRead
	case AccessWrite:
		tlb = &c.TLBWrite
	case AccessCode:
		tlb = &c.TLBCode
	}

	tlb[idx] = TLBEntry{
		VAddr:     pageVaddr,
		MemAddend: addend,
	}
}

// getPageFaultCause returns the appropriate page fault exception cause
func (c *CPU) getPageFaultCause(accessType AccessType) int {
	switch accessType {
	case AccessRead:
		return CauseLoadPageFault
	case AccessWrite:
		return CauseStorePageFault
	case AccessCode:
		return CauseFetchPageFault
	}
	return CauseLoadPageFault
}

// Error sentinels for internal use
var (
	errFetchFault = &cpuError{cause: CauseFaultFetch}
	errPageFault  = &cpuError{cause: CauseFetchPageFault}
	errLoadFault  = &cpuError{cause: CauseFaultLoad}
	errStoreFault = &cpuError{cause: CauseFaultStore}
)

type cpuError struct {
	cause int
}

func (e *cpuError) Error() string {
	switch e.cause {
	case CauseFaultFetch:
		return "instruction access fault"
	case CauseFetchPageFault:
		return "instruction page fault"
	case CauseFaultLoad:
		return "load access fault"
	case CauseLoadPageFault:
		return "load page fault"
	case CauseFaultStore:
		return "store access fault"
	case CauseStorePageFault:
		return "store page fault"
	default:
		return "memory error"
	}
}

// Memory access functions for load/store instructions
// Reference: tinyemu-2019-12-21/riscv_cpu.c:297-436 (target_read_slow)
// Reference: tinyemu-2019-12-21/riscv_cpu.c:439-522 (target_write_slow)

// LoadU8 loads an unsigned byte from memory
func (c *CPU) LoadU8(addr uint64) (uint8, error) {
	paddr, err := c.TranslateAddress(addr, AccessRead)
	if err != nil {
		return 0, err
	}

	val, err := c.Mem.Read8(paddr)
	if err != nil {
		c.SetPendingException(CauseFaultLoad, addr)
		return 0, errLoadFault
	}
	return val, nil
}

// LoadU16 loads an unsigned 16-bit value from memory
func (c *CPU) LoadU16(addr uint64) (uint16, error) {
	paddr, err := c.TranslateAddress(addr, AccessRead)
	if err != nil {
		return 0, err
	}

	val, err := c.Mem.Read16(paddr)
	if err != nil {
		c.SetPendingException(CauseFaultLoad, addr)
		return 0, errLoadFault
	}
	return val, nil
}

// LoadU32 loads an unsigned 32-bit value from memory
func (c *CPU) LoadU32(addr uint64) (uint32, error) {
	paddr, err := c.TranslateAddress(addr, AccessRead)
	if err != nil {
		return 0, err
	}

	val, err := c.Mem.Read32(paddr)
	if err != nil {
		c.SetPendingException(CauseFaultLoad, addr)
		return 0, errLoadFault
	}
	return val, nil
}

// LoadU64 loads an unsigned 64-bit value from memory
func (c *CPU) LoadU64(addr uint64) (uint64, error) {
	paddr, err := c.TranslateAddress(addr, AccessRead)
	if err != nil {
		return 0, err
	}

	val, err := c.Mem.Read64(paddr)
	if err != nil {
		c.SetPendingException(CauseFaultLoad, addr)
		return 0, errLoadFault
	}
	return val, nil
}

// StoreU8 stores a byte to memory
func (c *CPU) StoreU8(addr uint64, val uint8) error {
	paddr, err := c.TranslateAddress(addr, AccessWrite)
	if err != nil {
		return err
	}

	if err := c.Mem.Write8(paddr, val); err != nil {
		c.SetPendingException(CauseFaultStore, addr)
		return errStoreFault
	}
	return nil
}

// StoreU16 stores a 16-bit value to memory
func (c *CPU) StoreU16(addr uint64, val uint16) error {
	paddr, err := c.TranslateAddress(addr, AccessWrite)
	if err != nil {
		return err
	}

	if err := c.Mem.Write16(paddr, val); err != nil {
		c.SetPendingException(CauseFaultStore, addr)
		return errStoreFault
	}
	return nil
}

// StoreU32 stores a 32-bit value to memory
func (c *CPU) StoreU32(addr uint64, val uint32) error {
	paddr, err := c.TranslateAddress(addr, AccessWrite)
	if err != nil {
		return err
	}

	if err := c.Mem.Write32(paddr, val); err != nil {
		c.SetPendingException(CauseFaultStore, addr)
		return errStoreFault
	}
	return nil
}

// StoreU64 stores a 64-bit value to memory
func (c *CPU) StoreU64(addr uint64, val uint64) error {
	paddr, err := c.TranslateAddress(addr, AccessWrite)
	if err != nil {
		return err
	}

	if err := c.Mem.Write64(paddr, val); err != nil {
		c.SetPendingException(CauseFaultStore, addr)
		return errStoreFault
	}
	return nil
}
