package x86

import (
	"fmt"
	"os"
)

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

// LoadSegmentProtected loads a segment register from a selector in protected
// mode. It performs full Intel-style privilege and type checks; on a failure
// it returns an error. Callers in the instruction-execution path translate
// the error into #GP via raiseGeneralProtectionFault; the interrupt-delivery
// path propagates the error so it can be reported without re-entering the
// fault machinery.
func (c *CPU) LoadSegmentProtected(segReg int, selector uint16) error {
	if selector == 0 {
		if segReg == CS || segReg == SS {
			return fmt.Errorf("null selector for %d in protected mode", segReg)
		}
		// DS/ES/FS/GS may be null-loaded; the segment becomes unusable.
		c.seg[segReg] = 0
		c.segBase[segReg] = 0
		c.segLimit[segReg] = 0
		c.segAccess[segReg] = 0
		return nil
	}

	index := (selector >> 3) & 0x1FFF
	ti := (selector >> 2) & 1
	rpl := uint8(selector & 3)

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
		return fmt.Errorf("selector 0x%04X exceeds table limit", selector)
	}

	addr := tableBase + uint32(index)*8
	var descBytes [8]byte
	for i := 0; i < 8; i++ {
		descBytes[i] = c.readMemSV8(addr + uint32(i))
	}
	desc := ParseDescriptor(descBytes)

	// Type field is bits 0-3 of the access byte. System segments (S=0) have
	// type 0x0..0x7 plus subtypes; we only allow user segments (S=1) for
	// CS/SS/DS/ES/FS/GS via this function. (LDTR/TR are loaded via
	// LLDT/LTR which have their own checks.)
	isUserSeg := desc.Access&0x10 != 0
	if !isUserSeg {
		return fmt.Errorf("selector 0x%04X is not a user segment", selector)
	}
	isCode := desc.Access&0x08 != 0
	isWritable := desc.Access&0x02 != 0 // data: writable
	isReadable := desc.Access&0x02 != 0 // code: readable (same bit position)
	dpl := desc.DPL()
	cpl := uint8(c.cpl)

	// Type checks only. CPL/DPL/RPL enforcement varies by the instruction
	// performing the load (MOV vs JMP-far vs interrupt gate vs CALL gate)
	// and is best handled at the call site, not in this shared loader.
	switch segReg {
	case CS:
		if !isCode {
			return fmt.Errorf("CS selector 0x%04X is not a code segment", selector)
		}
	case SS:
		if isCode || !isWritable {
			return fmt.Errorf("SS selector 0x%04X is not a writable data segment", selector)
		}
		// User-mode SS load: require RPL == CPL and DPL == CPL. This guard
		// only catches the obvious case; the full check still belongs at
		// each MOV-to-SS site.
		if cpl > 0 && (rpl != cpl || dpl != cpl) {
			return fmt.Errorf("SS privilege: rpl=%d cpl=%d dpl=%d", rpl, cpl, dpl)
		}
	default:
		// DS/ES/FS/GS: data or readable code.
		if isCode && !desc.IsConforming() && !isReadable {
			return fmt.Errorf("non-conforming code segment 0x%04X is not readable", selector)
		}
		// Data segments and conforming code: max(CPL, RPL) <= DPL. Skip
		// for ring 0 supervisor (the kernel uses every segment).
		if cpl > 0 && (!isCode || !desc.IsConforming()) {
			eff := cpl
			if rpl > eff {
				eff = rpl
			}
			if eff > dpl {
				return fmt.Errorf("data segment privilege: max(cpl=%d, rpl=%d) > dpl=%d", cpl, rpl, dpl)
			}
		}
	}

	if !desc.Present {
		return fmt.Errorf("segment 0x%04X not present", selector)
	}

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
		val := c.seg[LDTR]
		if mr.isReg {
			c.SetReg16(reg16FromModRM(int(mr.rm)), val)
		} else {
			c.writeMem16(c.segBaseForModRM(mr) + mr.ea, val)
		}
	case 1: // STR r/m16
		val := c.seg[TR]
		if mr.isReg {
			c.SetReg16(reg16FromModRM(int(mr.rm)), val)
		} else {
			c.writeMem16(c.segBaseForModRM(mr) + mr.ea, val)
		}
	case 2: // LLDT r/m16
		var selector uint16
		if mr.isReg {
			selector = c.GetReg16(reg16FromModRM(int(mr.rm)))
		} else {
			selector = c.readMem16(c.segBaseForModRM(mr) + mr.ea)
		}
		if selector == 0 {
			c.seg[LDTR] = 0
			c.segBase[LDTR] = 0
			c.segLimit[LDTR] = 0
			c.segAccess[LDTR] = 0
		} else {
			index := (selector >> 3) & 0x1FFF
			gdtBase := c.segBase[GDTR]
			gdtLimit := c.segLimit[GDTR]
			if uint32(index)*8+7 > gdtLimit {
				return fmt.Errorf("LLDT: selector out of bounds")
			}
			addr := gdtBase + uint32(index)*8
			var descBytes [8]byte
			for i := 0; i < 8; i++ {
				descBytes[i] = c.readMem8(addr + uint32(i))
			}
			desc := ParseDescriptor(descBytes)
			if !desc.Present {
				return fmt.Errorf("LLDT: segment not present")
			}
			segType := desc.Access & 0x0F
			if segType != 0x02 {
				return fmt.Errorf("LLDT: not an LDT descriptor")
			}
			c.seg[LDTR] = selector
			c.segBase[LDTR] = desc.Base
			c.segLimit[LDTR] = desc.Limit
			c.segAccess[LDTR] = uint32(desc.Access) | (uint32(desc.Flags) << 8)
		}
	case 3: // LTR r/m16
		var selector uint16
		if mr.isReg {
			selector = c.GetReg16(reg16FromModRM(int(mr.rm)))
		} else {
			selector = c.readMem16(c.segBaseForModRM(mr) + mr.ea)
		}
		if selector == 0 {
			return fmt.Errorf("LTR: cannot load null selector")
		}
		index := (selector >> 3) & 0x1FFF
		gdtBase := c.segBase[GDTR]
		gdtLimit := c.segLimit[GDTR]
		if uint32(index)*8+7 > gdtLimit {
			return fmt.Errorf("LTR: selector out of bounds")
		}
		addr := gdtBase + uint32(index)*8
		var descBytes [8]byte
		for i := 0; i < 8; i++ {
			descBytes[i] = c.readMem8(addr + uint32(i))
		}
		desc := ParseDescriptor(descBytes)
		if !desc.Present {
			return fmt.Errorf("LTR: segment not present")
		}
		segType := desc.Access & 0x0F
		if segType != 0x09 && segType != 0x0B {
			return fmt.Errorf("LTR: not a TSS descriptor")
		}
		c.seg[TR] = selector
		c.segBase[TR] = desc.Base
		c.segLimit[TR] = desc.Limit
		c.segAccess[TR] = uint32(desc.Access) | (uint32(desc.Flags) << 8)
		// Mark TSS as busy (type 0x0B).
		c.writeMem8(addr+5, desc.Access|0x02)
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
			addr := c.segBaseForModRM(mr) + mr.ea
			c.writeMem16(addr, uint16(c.segLimit[GDTR]))
			c.writeMem32(addr+2, c.segBase[GDTR])
		}
	case 1: // SIDT
		if !mr.isReg {
			addr := c.segBaseForModRM(mr) + mr.ea
			c.writeMem16(addr, uint16(c.segLimit[IDTR]))
			c.writeMem32(addr+2, c.segBase[IDTR])
		}
	case 2: // LGDT
		addr := c.segBaseForModRM(mr) + mr.ea
		limit := uint32(c.readMem16(addr))
		base := uint32(c.readMem32(addr + 2))
		c.segLimit[GDTR] = limit
		c.segBase[GDTR] = base
	case 3: // LIDT
		addr := c.segBaseForModRM(mr) + mr.ea
		limit := uint32(c.readMem16(addr))
		base := uint32(c.readMem32(addr + 2))
		c.segLimit[IDTR] = limit
		c.segBase[IDTR] = base
	case 4: // SMSW
		val := uint16(c.cr[0])
		if mr.isReg {
			c.SetReg16(reg16FromModRM(int(mr.rm)), val)
		} else {
			c.writeMem16(c.segBaseForModRM(mr) + mr.ea, val)
		}
	case 6: // LMSW
		var val uint16
		if mr.isReg {
			val = c.GetReg16(reg16FromModRM(int(mr.rm)))
		} else {
			val = c.readMem16(c.segBaseForModRM(mr) + mr.ea)
		}
		c.cr[0] = (c.cr[0] & ^uint32(0xFFFF)) | uint32(val)
	case 7:
		if mr.isReg {
			// 0F 01 F9 = RDTSCP (ModRM byte 11 111 001). Returns TSC in
			// EDX:EAX and the IA32_TSC_AUX MSR in ECX (we return 0).
			if mr.rm == 1 {
				c.handleRDTSC()
				c.SetReg32(ECX, 0)
				return nil
			}
			return fmt.Errorf("0F 01 modrm=%02X not implemented", (3<<6)|(7<<3)|mr.rm)
		}
		// INVLPG m: invalidate the TLB entry for the supplied linear page.
		if !mr.isReg {
			lin := c.segBaseForModRM(mr) + mr.ea
			if invlpgDebug {
				fmt.Fprintf(os.Stderr, "[invlpg] EIP=0x%08X lin=0x%08X cycles=%d\n",
					c.eip, lin, c.cycles)
			}
			c.tlb.invalidatePage(lin)
			c.invalidateFetchBuffer()
		}
	default:
		return fmt.Errorf("0F 01 /%d not implemented", mr.reg)
	}
	return nil
}

// handleLAR implements LAR r16/32, r/m16 (0F 02): load access-rights byte of
// the segment descriptor named by r/m. We return the access byte shifted to
// bits [15:8] of the result, with ZF set on success.
func (c *CPU) handleLAR(operandSize uint8) error {
	mr := c.parseModRM()
	var sel uint16
	if mr.isReg {
		sel = c.GetReg16(reg16FromModRM(int(mr.rm)))
	} else {
		sel = c.readMem16(c.segBaseForModRM(mr) + mr.ea)
	}
	access, flags, ok := c.descriptorAccessByte(sel)
	if !ok {
		c.setZF(false)
		return nil
	}
	c.setZF(true)
	if operandSize == 2 {
		c.SetReg16(reg16FromModRM(int(mr.reg)), uint16(access)<<8)
	} else {
		// 32-bit: bits 15:8 = access byte, bits 23:20 = the G/D-B/L/AVL
		// flags nibble (descriptor byte 6, high nibble). Per SDM the LAR
		// result is the descriptor's high dword masked with 0x00F0FF00.
		c.SetReg32(int(mr.reg), uint32(access)<<8|uint32(flags)<<20)
	}
	return nil
}

// handleLSL implements LSL r16/32, r/m16 (0F 03): load the byte-granular
// limit of the segment named by r/m.
func (c *CPU) handleLSL(operandSize uint8) error {
	mr := c.parseModRM()
	var sel uint16
	if mr.isReg {
		sel = c.GetReg16(reg16FromModRM(int(mr.rm)))
	} else {
		sel = c.readMem16(c.segBaseForModRM(mr) + mr.ea)
	}
	limit, ok := c.descriptorLimit(sel)
	if !ok {
		c.setZF(false)
		return nil
	}
	c.setZF(true)
	if operandSize == 2 {
		c.SetReg16(reg16FromModRM(int(mr.reg)), uint16(limit))
	} else {
		c.SetReg32(int(mr.reg), limit)
	}
	return nil
}

// descriptorAccessByte fetches the access byte of the descriptor named by
// selector. Returns ok=false for null selectors or out-of-range indexes.
func (c *CPU) descriptorAccessByte(selector uint16) (access uint8, flags uint8, ok bool) {
	if selector == 0 {
		return 0, 0, false
	}
	index := (selector >> 3) & 0x1FFF
	ti := (selector >> 2) & 1
	var base, limit uint32
	if ti == 0 {
		base = c.segBase[GDTR]
		limit = c.segLimit[GDTR]
	} else {
		base = c.segBase[LDTR]
		limit = c.segLimit[LDTR]
	}
	if uint32(index)*8+7 > limit {
		return 0, 0, false
	}
	descBase := base + uint32(index)*8
	// Byte 5 = access-rights byte; byte 6 high nibble = G/D-B/L/AVL flags.
	return c.readMem8(descBase + 5), c.readMem8(descBase+6) >> 4, true
}

// descriptorLimit returns the byte-granular limit field of the descriptor.
func (c *CPU) descriptorLimit(selector uint16) (uint32, bool) {
	if selector == 0 {
		return 0, false
	}
	index := (selector >> 3) & 0x1FFF
	ti := (selector >> 2) & 1
	var base, tlimit uint32
	if ti == 0 {
		base = c.segBase[GDTR]
		tlimit = c.segLimit[GDTR]
	} else {
		base = c.segBase[LDTR]
		tlimit = c.segLimit[LDTR]
	}
	if uint32(index)*8+7 > tlimit {
		return 0, false
	}
	addr := base + uint32(index)*8
	var d [8]byte
	for i := 0; i < 8; i++ {
		d[i] = c.readMem8(addr + uint32(i))
	}
	desc := ParseDescriptor(d)
	return desc.Limit, true
}

// handleMovCR handles MOV r32, CRn (read) and MOV CRn, r32 (write).
func (c *CPU) handleMovCR(read bool) error {
	mr := c.parseModRM()
	cr := mr.reg
	r32 := mr.rm
	if read {
		c.SetReg32(int(r32), c.cr[cr])
	} else {
		oldCR0 := c.cr[0]
		oldCR3 := c.cr[3]
		oldCR4 := c.cr[4]
		c.cr[cr] = c.GetReg32(int(r32))
		if crDebug && cr != 2 {
			fmt.Fprintf(os.Stderr, "[cr] EIP=0x%08X CR%d old=0x%08X new=0x%08X\n",
				c.eip, cr, []uint32{oldCR0, 0, 0, oldCR3, oldCR4}[cr], c.cr[cr])
		}
		switch cr {
		case 0:
			c.updateCPLFromCR0()
			// Toggling CR0.PG flushes the entire TLB.
			if (oldCR0^c.cr[0])&CR0_PG != 0 {
				c.tlb.flushAll()
				c.invalidateFetchBuffer()
			}
		case 3:
			// CR3 reload flushes all non-global TLB entries (matches the
			// behaviour software relies on for context switches). If the
			// value is unchanged we still flush — software writes CR3 to
			// the same value precisely to invoke this flush.
			c.tlb.flushNonGlobal()
			c.invalidateFetchBuffer()
		case 4:
			// Toggling CR4.PGE/PSE/PAE flushes the entire TLB. (Linux uses
			// CR4.PGE-toggle as its __flush_tlb_global primitive.)
			if (oldCR4^c.cr[4])&(CR4_PGE|CR4_PSE|CR4_PAE) != 0 {
				c.tlb.flushAll()
				c.invalidateFetchBuffer()
			}
		}
		c.updatePAEActive()
	}
	return nil
}

// crDebug enables tracing of CR0/CR3/CR4 writes (env TINYEMU_X86_CR_DEBUG=1).
var crDebug = os.Getenv("TINYEMU_X86_CR_DEBUG") == "1"

// invlpgDebug enables tracing of INVLPG (env TINYEMU_X86_INVLPG_DEBUG=1).
var invlpgDebug = os.Getenv("TINYEMU_X86_INVLPG_DEBUG") == "1"

// updatePAEActive reconciles c.paeActive with the current CR0.PG and CR4.PAE
// state and reloads the PDPTE cache from CR3 whenever PAE is active.
func (c *CPU) updatePAEActive() {
	active := c.pagingEnabled() && c.cr[4]&CR4_PAE != 0
	if active {
		c.paeActive = true
		c.refreshPDPTEs()
	} else {
		c.paeActive = false
	}
}

// handleMovDR handles MOV r32, DRn (read) and MOV DRn, r32 (write). Debug
// registers are simply stored; no debug-trap functionality is emulated.
func (c *CPU) handleMovDR(read bool) error {
	mr := c.parseModRM()
	dr := mr.reg
	r32 := mr.rm
	if read {
		c.SetReg32(int(r32), c.dr[dr])
	} else {
		c.dr[dr] = c.GetReg32(int(r32))
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

// CPUID feature bits we advertise. These choices push Linux toward simple code
// paths: TSC (so it uses fast TSC calibration instead of the PIT delay loop),
// MSR + CX8 + CMOV + PAT + PGE + PSE for standard kernel functionality. We
// advertise FPU=1 so the kernel doesn't bail at fpu__init_disable_system_
// fpu_support (Yocto's qemux86 kernel is built without CONFIG_MATH_EMULATION,
// so "no FPU" → panic). x87 handlers are stubs (NOPs that consume ModRM);
// Linux's early-boot FPU usage is limited to FNINIT + a feature probe.
// We do NOT advertise SEP (forces INT 0x80 instead of SYSENTER), APIC,
// or MTRR. MMX has the full integer-arithmetic set (moves, packed
// add/sub/cmp/logical/pack/unpack/shift/mul). SSE/SSE2 currently
// provide ONLY 128-bit move instructions (MOVAPS/MOVUPS/MOVDQA on the
// XMM register file) — enough to satisfy musl libc's memcpy / TLS
// setup paths but not actual packed FP/integer arithmetic. Userspace
// binaries that compute on XMM (e.g. zlib's CRC32) will still hit
// unimplemented opcodes; SSE arith is the next layer to add.
const (
	cpuidFeat1EDX = (1 << 0) | // FPU (x87)
		(1 << 3) | // PSE
		(1 << 4) | // TSC
		(1 << 5) | // MSR
		(1 << 6) | // PAE
		(1 << 8) | // CX8 (CMPXCHG8B)
		(1 << 13) | // PGE
		(1 << 15) | // CMOV
		(1 << 16) | // PAT
		(1 << 23) | // MMX (full integer set)
		(1 << 24) | // FXSR (FXSAVE/FXRSTOR)
		(1 << 25) | // SSE  (moves only — arith not implemented)
		(1 << 26) //   SSE2 (moves only — arith not implemented)
)

// cpuidTrace logs each CPUID invocation (leaf + EIP) to stderr. Enable with
// TINYEMU_X86_CPUID_TRACE=1.
var cpuidTrace = os.Getenv("TINYEMU_X86_CPUID_TRACE") == "1"

// handleCPUID handles the CPUID instruction.
func (c *CPU) handleCPUID() {
	eax := c.GetReg32(EAX)
	if cpuidTrace {
		fmt.Fprintf(os.Stderr, "[CPUID] leaf=0x%08X EIP=0x%08X\n", eax, c.eip)
	}
	switch eax {
	case 0:
		c.SetReg32(EAX, 0x00000004) // Maximum standard function
		c.SetReg32(EBX, 0x756E6547) // "Genu"
		c.SetReg32(ECX, 0x6C65746E) // "ntel"
		c.SetReg32(EDX, 0x49656E69) // "ineI"
	case 1:
		c.SetReg32(EAX, 0x00000633) // Family 6, Model 3, stepping 3
		c.SetReg32(EBX, 0x00000000)
		c.SetReg32(ECX, 0x00000000)
		c.SetReg32(EDX, cpuidFeat1EDX)
	case 2, 3, 4:
		c.SetReg32(EAX, 0)
		c.SetReg32(EBX, 0)
		c.SetReg32(ECX, 0)
		c.SetReg32(EDX, 0)
	case 0x80000000:
		c.SetReg32(EAX, 0x80000004)
		c.SetReg32(EBX, 0)
		c.SetReg32(ECX, 0)
		c.SetReg32(EDX, 0)
	case 0x80000001:
		c.SetReg32(EAX, 0)
		c.SetReg32(EBX, 0)
		c.SetReg32(ECX, 0)
		c.SetReg32(EDX, 0)
	case 0x80000002:
		// "tinyemu-go x86 "
		c.SetReg32(EAX, 0x796e6974)
		c.SetReg32(EBX, 0x2d756d65)
		c.SetReg32(ECX, 0x78206f67)
		c.SetReg32(EDX, 0x20203638)
	case 0x80000003:
		// "CPU @ 1.0 GHz   "
		c.SetReg32(EAX, 0x20555043)
		c.SetReg32(EBX, 0x2e312040)
		c.SetReg32(ECX, 0x48472030)
		c.SetReg32(EDX, 0x2020207a)
	case 0x80000004:
		c.SetReg32(EAX, 0x20202020)
		c.SetReg32(EBX, 0x20202020)
		c.SetReg32(ECX, 0x20202020)
		c.SetReg32(EDX, 0x20202020)
	default:
		c.SetReg32(EAX, 0)
		c.SetReg32(EBX, 0)
		c.SetReg32(ECX, 0)
		c.SetReg32(EDX, 0)
	}
}

var tscFastpathHits uint64

// handleRDTSC handles the RDTSC instruction. Linux's delay_tsc() spins
// reading TSC waiting for `cycles >= bclock + target`. With our slow
// emulation, large delays would take minutes of wall time, so when we detect
// we're inside delay_tsc we satisfy the comparison by reading the target and
// bclock directly off the kernel's stack and setting cycles to satisfy.
func (c *CPU) handleRDTSC() {
	if c.detectTSCDelayLoop() {
		tscFastpathHits++
		// delay_tsc layout (verified from disassembly of Alpine 3.19 x86):
		//   bclock = qword [ebp-0x20]
		//   target = qword [ebp-0x18]
		ebp := c.GetReg32(EBP)
		bclock := uint64(c.readMem32(ebp-0x20)) | uint64(c.readMem32(ebp-0x1C))<<32
		target := uint64(c.readMem32(ebp-0x18)) | uint64(c.readMem32(ebp-0x14))<<32
		want := bclock + target + 1
		if want > c.cycles {
			c.cycles = want
		}
	}
	c.SetReg32(EAX, uint32(c.cycles))
	c.SetReg32(EDX, uint32(c.cycles>>32))
}

// TSCFastpathHits returns the number of times the delay_tsc fastpath has
// fired since process start. Exported for tests/diagnostics.
func TSCFastpathHits() uint64 { return tscFastpathHits }

// detectTSCDelayLoop matches Linux's delay_tsc pattern: RDTSC followed
// (within 32 bytes) by SUB EAX, [ebp-disp]; SBB EDX, [ebp-disp] computing the
// TSC delta, ending in `JB rel8` looping back through the RDTSC. We require
// the SUB/SBB signature to avoid firing on PIT calibration's plain
// count++/JB-back loop.
func (c *CPU) detectTSCDelayLoop() bool {
	rdtscEIP := c.eip - 2 // RDTSC is 2 bytes (0F 31); EIP is now after it
	lip := c.GetLIP()
	sawSub := false
	sawSbb := false
	for i := uint32(0); i < 32; i++ {
		op := c.readMem8(lip + i)
		if op == 0x2B { // SUB r32, r/m32
			sawSub = true
		}
		if op == 0x1B { // SBB r32, r/m32
			sawSbb = true
		}
		if op == 0x72 { // JB rel8
			off := int8(c.readMem8(lip + i + 1))
			if off < 0 {
				target := c.eip + i + 2 + uint32(int32(off))
				if target <= rdtscEIP && sawSub && sawSbb {
					return true
				}
			}
		}
	}
	return false
}

// MSR numbers we recognize.
const (
	msrIA32_TSC           = 0x00000010
	msrIA32_APIC_BASE     = 0x0000001B
	msrIA32_MISC_ENABLE   = 0x000001A0
	msrIA32_SYSENTER_CS   = 0x00000174
	msrIA32_SYSENTER_ESP  = 0x00000175
	msrIA32_SYSENTER_EIP  = 0x00000176
	msrIA32_MTRRCAP       = 0x000000FE
	msrIA32_MTRR_DEF_TYPE = 0x000002FF
	msrIA32_EFER          = 0xC0000080
	msrIA32_FS_BASE       = 0xC0000100
	msrIA32_GS_BASE       = 0xC0000101
	msrIA32_KGS_BASE      = 0xC0000102
)

// handleWRMSR writes the value in EDX:EAX to the MSR identified by ECX.
// Unknown MSRs raise #GP(0); the kernel uses fixup tables to recover.
func (c *CPU) handleWRMSR() {
	idx := c.GetReg32(ECX)
	val := uint64(c.GetReg32(EAX)) | (uint64(c.GetReg32(EDX)) << 32)
	switch idx {
	case msrIA32_TSC:
		c.cycles = val
	case msrIA32_APIC_BASE:
		// Absorb; we have no APIC and report it disabled in reads.
	case msrIA32_MISC_ENABLE:
		c.msrMiscEnable = val
	case msrIA32_SYSENTER_CS:
		c.msrSysenterCS = uint32(val)
	case msrIA32_SYSENTER_ESP:
		c.msrSysenterESP = uint32(val)
	case msrIA32_SYSENTER_EIP:
		c.msrSysenterEIP = uint32(val)
	case msrIA32_EFER:
		// Reject LME (bit 8): no long-mode support.
		if val&(1<<8) != 0 {
			c.raiseGeneralProtectionFault(0)
		}
		c.efer = val
	case msrIA32_FS_BASE:
		c.msrFSBase = uint32(val)
		c.segBase[FS] = uint32(val)
	case msrIA32_GS_BASE:
		c.msrGSBase = uint32(val)
		c.segBase[GS] = uint32(val)
	case msrIA32_KGS_BASE:
		c.msrKernelGSBase = uint32(val)
	case msrIA32_MTRR_DEF_TYPE:
		c.mtrrDefType = val
	default:
		// MTRR phys base/mask pairs at 0x200-0x20F (16 pairs of 2 MSRs).
		if idx >= 0x200 && idx < 0x210 {
			pair := (idx - 0x200) >> 1
			if idx&1 == 0 {
				c.mtrrPhysBase[pair] = val
			} else {
				c.mtrrPhysMask[pair] = val
			}
			return
		}
		// MTRR fixed-range MSRs.
		if idx == 0x250 || (idx >= 0x258 && idx <= 0x259) || (idx >= 0x268 && idx <= 0x26F) {
			// Just absorb; our memory model has no MTRR effect.
			return
		}
		c.raiseGeneralProtectionFault(0)
	}
}

// handleRDMSR reads the MSR identified by ECX into EDX:EAX. Unknown MSRs raise
// #GP(0).
func (c *CPU) handleRDMSR() {
	idx := c.GetReg32(ECX)
	var val uint64
	switch idx {
	case msrIA32_TSC:
		val = c.cycles
	case msrIA32_APIC_BASE:
		// Bits 31:12 = APIC base address. Bit 11 = enable (0 = disabled).
		val = 0xFEE00000
	case msrIA32_MISC_ENABLE:
		val = c.msrMiscEnable
	case msrIA32_SYSENTER_CS:
		val = uint64(c.msrSysenterCS)
	case msrIA32_SYSENTER_ESP:
		val = uint64(c.msrSysenterESP)
	case msrIA32_SYSENTER_EIP:
		val = uint64(c.msrSysenterEIP)
	case msrIA32_MTRRCAP:
		// VCNT (count of variable ranges) = 8, no fixed-range support, no WC.
		val = 8
	case msrIA32_MTRR_DEF_TYPE:
		val = c.mtrrDefType
	case msrIA32_EFER:
		val = c.efer
	case msrIA32_FS_BASE:
		val = uint64(c.msrFSBase)
	case msrIA32_GS_BASE:
		val = uint64(c.msrGSBase)
	case msrIA32_KGS_BASE:
		val = uint64(c.msrKernelGSBase)
	default:
		if idx >= 0x200 && idx < 0x210 {
			pair := (idx - 0x200) >> 1
			if idx&1 == 0 {
				val = c.mtrrPhysBase[pair]
			} else {
				val = c.mtrrPhysMask[pair]
			}
		} else if idx == 0x250 || (idx >= 0x258 && idx <= 0x259) || (idx >= 0x268 && idx <= 0x26F) {
			val = 0
		} else {
			c.raiseGeneralProtectionFault(0)
			return
		}
	}
	c.SetReg32(EAX, uint32(val))
	c.SetReg32(EDX, uint32(val>>32))
}
