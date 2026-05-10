package x86

import "fmt"

// SegmentDescriptor represents an x86 segment descriptor.
type SegmentDescriptor struct {
	Base    uint32
	Limit   uint32
	Access  uint8
	Flags   uint8
	Present bool
}

// ParseDescriptor reads an 8-byte segment descriptor from memory.
func ParseDescriptor(b [8]byte) SegmentDescriptor {
	base := uint32(b[2]) | uint32(b[3])<<8 | uint32(b[4])<<16 | uint32(b[7])<<24
	limit := uint32(b[0]) | uint32(b[1])<<8 | uint32(b[6]&0x0F)<<16
	flags := b[6] >> 4
	access := b[5]
	if flags&0x08 != 0 { // G (granularity) bit
		limit = (limit << 12) | 0xFFF
	}
	return SegmentDescriptor{
		Base:    base,
		Limit:   limit,
		Access:  access,
		Flags:   flags,
		Present: access&0x80 != 0,
	}
}

// IsCode returns true if this is a code segment.
func (d SegmentDescriptor) IsCode() bool {
	return d.Access&0x18 == 0x18 // S=1, Type bit 3=1
}

// IsData returns true if this is a data segment.
func (d SegmentDescriptor) IsData() bool {
	return d.Access&0x18 == 0x10 // S=1, Type bit 3=0
}

// DPL returns the descriptor privilege level.
func (d SegmentDescriptor) DPL() uint8 {
	return (d.Access >> 5) & 3
}

// Is32Bit returns true if the D/B flag is set (32-bit segment).
func (d SegmentDescriptor) Is32Bit() bool {
	return d.Flags&0x04 != 0
}

// IsConforming returns true if this is a conforming code segment.
func (d SegmentDescriptor) IsConforming() bool {
	return d.IsCode() && d.Access&0x04 != 0
}

// LoadSegmentProtected loads a segment register from a selector in protected mode.
func (c *CPU) LoadSegmentProtected(segReg int, selector uint16) error {
	if selector == 0 && (segReg == CS || segReg == SS) {
		return fmt.Errorf("load null selector into %d", segReg)
	}
	if selector == 0 {
		c.seg[segReg] = 0
		c.segBase[segReg] = 0
		c.segLimit[segReg] = 0
		c.segAccess[segReg] = 0
		return nil
	}

	index := (selector >> 3) & 0x1FFF
	ti := (selector >> 2) & 1
	rpl := selector & 3

	var tableBase uint32
	var tableLimit uint32
	if ti == 0 {
		tableBase = c.segBase[GDTR]
		tableLimit = c.segLimit[GDTR]
	} else {
		tableBase = c.segBase[LDTR]
		tableLimit = c.segLimit[LDTR]
	}

	if uint32(index)*8+7 > tableLimit {
		return fmt.Errorf("segment selector out of bounds: index=%d", index)
	}

	addr := tableBase + uint32(index)*8
	var descBytes [8]byte
	for i := 0; i < 8; i++ {
		descBytes[i] = c.readMem8(addr + uint32(i))
	}
	desc := ParseDescriptor(descBytes)

	if !desc.Present {
		return fmt.Errorf("segment not present: index=%d", index)
	}

	// TODO: privilege checks, type checks

	c.seg[segReg] = selector
	c.segBase[segReg] = desc.Base
	c.segLimit[segReg] = desc.Limit
	c.segAccess[segReg] = uint32(desc.Access) | (uint32(desc.Flags) << 8)

	if segReg == CS && !desc.IsConforming() {
		c.cpl = int(rpl)
	}

	return nil
}

// handleGroupO_01 handles 0F 01 /n (LGDT, LIDT, LMSW, etc.).
// handleGroupO_00 handles 0F 00 /n (SLDT, STR, LLDT, LTR, VERR, VERW).
func (c *CPU) handleGroupO_00() error {
	mr := c.parseModRM()
	switch mr.reg {
	case 0: // SLDT r/m16
		val := uint16(0)
		if mr.isReg {
			c.SetReg16(reg16FromModRM(int(mr.rm)), val)
		} else {
			c.writeMem16(c.segBase[DS]+mr.ea, val)
		}
	case 1: // STR r/m16
		val := uint16(0)
		if mr.isReg {
			c.SetReg16(reg16FromModRM(int(mr.rm)), val)
		} else {
			c.writeMem16(c.segBase[DS]+mr.ea, val)
		}
	case 2: // LLDT r/m16
		// No-op for now; we don't track LDT state.
	case 3: // LTR r/m16
		// No-op for now; we don't track task register state.
	case 4: // VERR r/m16
		c.setZF(false)
	case 5: // VERW r/m16
		c.setZF(false)
	default:
		return fmt.Errorf("0F 00 /%d not implemented", mr.reg)
	}
	return nil
}

func (c *CPU) handleGroupO_01() error {
	mr := c.parseModRM()
	switch mr.reg {
	case 0: // SGDT
		if !mr.isReg {
			addr := c.segBase[DS] + mr.ea
			c.writeMem16(addr, uint16(c.segLimit[GDTR]))
			c.writeMem32(addr+2, c.segBase[GDTR])
		}
	case 1: // SIDT
		if !mr.isReg {
			addr := c.segBase[DS] + mr.ea
			c.writeMem16(addr, uint16(c.segLimit[IDTR]))
			c.writeMem32(addr+2, c.segBase[IDTR])
		}
	case 2: // LGDT
		addr := c.segBase[DS] + mr.ea
		limit := uint32(c.readMem16(addr))
		base := uint32(c.readMem32(addr + 2))
		c.segLimit[GDTR] = limit
		c.segBase[GDTR] = base
	case 3: // LIDT
		addr := c.segBase[DS] + mr.ea
		limit := uint32(c.readMem16(addr))
		base := uint32(c.readMem32(addr + 2))
		c.segLimit[IDTR] = limit
		c.segBase[IDTR] = base
	case 4: // SMSW
		val := uint16(c.cr[0])
		if mr.isReg {
			c.SetReg16(reg16FromModRM(int(mr.rm)), val)
		} else {
			c.writeMem16(c.segBase[DS]+mr.ea, val)
		}
	case 6: // LMSW
		var val uint16
		if mr.isReg {
			val = c.GetReg16(reg16FromModRM(int(mr.rm)))
		} else {
			val = c.readMem16(c.segBase[DS] + mr.ea)
		}
		c.cr[0] = (c.cr[0] & ^uint32(0xFFFF)) | uint32(val)
	case 7: // INVLPG
		// No TLB in this emulator, so this is a NOP.
	default:
		return fmt.Errorf("0F 01 /%d not implemented", mr.reg)
	}
	return nil
}

// handleMovCR handles MOV r32, CRn (read) and MOV CRn, r32 (write).
func (c *CPU) handleMovCR(read bool) error {
	mr := c.parseModRM()
	cr := mr.reg
	r32 := mr.rm
	if read {
		c.SetReg32(int(r32), c.cr[cr])
	} else {
		c.cr[cr] = c.GetReg32(int(r32))
		if cr == 0 {
			c.updateCPLFromCR0()
		}
	}
	return nil
}

// updateCPLFromCR0 updates the CPU state when CR0 changes.
func (c *CPU) updateCPLFromCR0() {
	if c.cr[0]&CR0_PE != 0 {
		// Entering protected mode
		// TODO: more protected mode setup
	} else {
		// Back to real mode
		c.cpl = 0
	}
}

// handleCPUID handles the CPUID instruction.
func (c *CPU) handleCPUID() {
	eax := c.GetReg32(EAX)
	switch eax {
	case 0:
		c.SetReg32(EAX, 0x00000002) // Maximum standard function
		c.SetReg32(EBX, 0x756E6547) // "Genu"
		c.SetReg32(ECX, 0x6C65746E) // "ntel"
		c.SetReg32(EDX, 0x49656E69) // "ineI"
	case 1:
		c.SetReg32(EAX, 0x00000633) // Family 6, Model 3
		c.SetReg32(EBX, 0x00000000)
		c.SetReg32(ECX, 0x00000000)
		c.SetReg32(EDX, 0x00000001) // FPU present
	default:
		c.SetReg32(EAX, 0)
		c.SetReg32(EBX, 0)
		c.SetReg32(ECX, 0)
		c.SetReg32(EDX, 0)
	}
}

// handleRDTSC handles the RDTSC instruction.
func (c *CPU) handleRDTSC() {
	c.SetReg32(EAX, uint32(c.cycles))
	c.SetReg32(EDX, uint32(c.cycles>>32))
}
