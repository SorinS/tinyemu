package riscv

import (
	"errors"
)

// CSR access errors
var (
	ErrCSRNotFound  = errors.New("CSR not found")
	ErrCSRReadOnly  = errors.New("CSR is read-only")
	ErrCSRPrivilege = errors.New("insufficient privilege for CSR access")
)

// getMstatus returns the complete mstatus with the SD bit computed dynamically.
// Reference: tinyemu-2019-12-21/riscv_cpu.c:651-662
func (c *CPU) getMstatus(mask uint64) uint64 {
	// Merge FS field from cache into mstatus
	val := c.Mstatus | (uint64(c.FS) << MstatusFSShift)
	val &= mask

	// Compute SD bit: set if FS==Dirty or XS==Dirty
	fs := (val & MstatusFS) >> MstatusFSShift
	xs := (val & MstatusXS) >> MstatusXSShift
	sd := (fs == FSDirty) || (xs == 3)
	if sd {
		// SD bit is at the MSB of the current XLEN
		val |= uint64(1) << (c.CurXLEN - 1)
	}
	return val
}

// ReadCSR reads a CSR value
// Reference: tinyemu-2019-12-21/riscv_cpu.c:703-857
func (c *CPU) ReadCSR(csr uint32) (uint64, error) {
	// Check privilege level
	requiredPriv := uint8((csr >> 8) & 3)
	if c.Priv < requiredPriv {
		return 0, ErrCSRPrivilege
	}

	switch csr {
	// User-level CSRs
	// Reference: riscv_cpu.c:714-729 - FP CSRs require FS != 0
	case CSRFflags:
		if c.FS == FSOff {
			return 0, ErrCSRNotFound
		}
		return uint64(c.FFlags), nil
	case CSRFrm:
		if c.FS == FSOff {
			return 0, ErrCSRNotFound
		}
		return uint64(c.FRM), nil
	case CSRFcsr:
		if c.FS == FSOff {
			return 0, ErrCSRNotFound
		}
		return uint64(c.FRM)<<5 | uint64(c.FFlags), nil
	case CSRCycle, CSRInstret:
		// Reference: riscv_cpu.c lines 731-744 (counteren permission check)
		// U-mode uses scounteren, S-mode uses mcounteren, M-mode always allowed
		if c.Priv < PrivMachine {
			var counteren uint32
			if c.Priv < PrivSupervisor {
				counteren = c.Scounteren
			} else {
				counteren = c.Mcounteren
			}
			// Check bit corresponding to CSR (cycle=0, time=1, instret=2)
			bit := csr & 0x1f
			if (counteren>>bit)&1 == 0 {
				return 0, ErrCSRPrivilege
			}
		}
		// Reference: riscv_cpu.c:744 - val = (int64_t)s->insn_counter
		return c.InsnCounter, nil
	case CSRCycleh, CSRInstreth:
		// Reference: riscv_cpu.c:746-761 - high 32 bits, only valid for cur_xlen=32
		if c.CurXLEN != XLEN32 {
			return 0, ErrCSRNotFound
		}
		if c.Priv < PrivMachine {
			var counteren uint32
			if c.Priv < PrivSupervisor {
				counteren = c.Scounteren
			} else {
				counteren = c.Mcounteren
			}
			bit := csr & 0x1f
			if (counteren>>bit)&1 == 0 {
				return 0, ErrCSRPrivilege
			}
		}
		return c.InsnCounter >> 32, nil
	case CSRTime:
		// Time CSR returns mtime value = insn_counter / RTCFreqDiv
		// This matches CLINT mtime calculation for consistency
		// Note: TinyEMU doesn't implement TIME CSR (guest reads CLINT mtime directly)
		// but for compatibility we provide it here
		// counteren gates time just like cycle/instret (TM = bit 1).
		if c.Priv < PrivMachine {
			var counteren uint32
			if c.Priv < PrivSupervisor {
				counteren = c.Scounteren
			} else {
				counteren = c.Mcounteren
			}
			bit := csr & 0x1f
			if (counteren>>bit)&1 == 0 {
				return 0, ErrCSRPrivilege
			}
		}
		var timeVal uint64
		if c.TimeSource != nil {
			timeVal = c.TimeSource.GetRTCTime()
		} else {
			const RTCFreqDiv = 16
			timeVal = c.InsnCounter / RTCFreqDiv
		}
		return timeVal, nil

	// Supervisor-level CSRs
	case CSRSstatus:
		return c.readSstatus(), nil
	case CSRSie:
		return uint64(c.Mie & c.Mideleg), nil
	case CSRStvec:
		return c.Stvec, nil
	case CSRScounteren:
		return uint64(c.Scounteren), nil
	case CSRSscratch:
		return c.Sscratch, nil
	case CSRSepc:
		return c.Sepc, nil
	case CSRScause:
		return c.Scause, nil
	case CSRStval:
		return c.Stval, nil
	case CSRSip:
		return uint64(c.Mip & c.Mideleg), nil
	case CSRSatp:
		// mstatus.TVM traps S-mode satp access (read and write), same rule
		// as SFENCE.VMA. M-mode is exempt.
		if c.Priv == PrivSupervisor && c.Mstatus&MstatusTVM != 0 {
			return 0, ErrCSRPrivilege
		}
		return c.Satp, nil

	// Machine-level CSRs
	case CSRMstatus:
		return c.getMstatus(^uint64(0)), nil
	case CSRMisa:
		// Reference: riscv_cpu.c:797-800 - OR in mxl based on cur_xlen
		val := c.Misa
		val |= uint64(c.MXL) << (c.CurXLEN - 2)
		return val, nil
	case CSRMedeleg:
		return uint64(c.Medeleg), nil
	case CSRMideleg:
		return uint64(c.Mideleg), nil
	case CSRMie:
		return uint64(c.Mie), nil
	case CSRMtvec:
		return c.Mtvec, nil
	case CSRMcounteren:
		return uint64(c.Mcounteren), nil
	case CSRMscratch:
		return c.Mscratch, nil
	case CSRMepc:
		return c.Mepc, nil
	case CSRMcause:
		return c.Mcause, nil
	case CSRMtval:
		return c.Mtval, nil
	case CSRMip:
		return uint64(c.Mip), nil
	case CSRMcycle, CSRMinstret:
		// Reference: riscv_cpu.c:831-834 - val = (int64_t)s->insn_counter
		return c.InsnCounter, nil
	case CSRMcycleh, CSRMinstreth:
		// Reference: riscv_cpu.c:835-840 - high 32 bits, only valid for cur_xlen=32
		if c.CurXLEN != XLEN32 {
			return 0, ErrCSRNotFound
		}
		return c.InsnCounter >> 32, nil
	case CSRMhartid:
		return c.Mhartid, nil
	case CSRMvendorid:
		// Return 0 = not implemented
		return 0, nil
	case CSRMarchid:
		// Return 0 = not implemented
		return 0, nil
	case CSRMimpid:
		// Return 0 = not implemented
		return 0, nil

	default:
		return 0, ErrCSRNotFound
	}
}

// WriteCSR writes a value to a CSR
func (c *CPU) WriteCSR(csr uint32, val uint64) error {
	// Check privilege level
	requiredPriv := uint8((csr >> 8) & 3)
	if c.Priv < requiredPriv {
		return ErrCSRPrivilege
	}

	// Check read-only bit (bits 11:10 = 11 means read-only)
	if (csr>>10)&3 == 3 {
		return ErrCSRReadOnly
	}

	switch csr {
	// User-level CSRs
	// Reference: riscv_cpu.c:860-865 - set_frm clamps invalid values to 0
	case CSRFflags:
		if c.FS == FSOff {
			return ErrCSRNotFound
		}
		c.FFlags = uint32(val) & 0x1F
		c.FS = FSDirty
	case CSRFrm:
		if c.FS == FSOff {
			return ErrCSRNotFound
		}
		// Reference: riscv_cpu.c:860-865 - values >= 5 are clamped to 0
		frm := uint8(val) & 0x7
		if frm >= 5 {
			frm = 0
		}
		c.FRM = frm
		c.FS = FSDirty
	case CSRFcsr:
		if c.FS == FSOff {
			return ErrCSRNotFound
		}
		c.FFlags = uint32(val) & 0x1F
		// Reference: riscv_cpu.c:860-865 - values >= 5 are clamped to 0
		frm := uint8((val >> 5) & 0x7)
		if frm >= 5 {
			frm = 0
		}
		c.FRM = frm
		c.FS = FSDirty

	// Supervisor-level CSRs
	case CSRSstatus:
		c.writeSstatus(val)
	case CSRSie:
		// Only bits delegated to S-mode can be written
		mask := c.Mideleg
		c.Mie = (c.Mie &^ mask) | (uint32(val) & mask)
	case CSRStvec:
		// Reference: riscv_cpu.c line 914 (s->stvec = val & ~3)
		// TinyEMU always uses direct mode - clears low 2 bits on write
		c.Stvec = val & ^uint64(3)
	case CSRScounteren:
		c.Scounteren = uint32(val) & 0x7
	case CSRSscratch:
		c.Sscratch = val
	case CSRSepc:
		c.Sepc = val & ^uint64(1) // Must be aligned
	case CSRScause:
		c.Scause = val
	case CSRStval:
		c.Stval = val
	case CSRSip:
		// Reference: riscv_cpu.c:931-933 - mask = s->mideleg
		// C TinyEMU allows writing any bits that are in mideleg (SSIP, STIP, SEIP)
		// Note: RISC-V spec says only SSIP is software-writable, but we match C.
		mask := c.Mideleg
		c.Mip = (c.Mip &^ mask) | (uint32(val) & mask)
	case CSRSatp:
		// mstatus.TVM traps S-mode satp access (read and write).
		if c.Priv == PrivSupervisor && c.Mstatus&MstatusTVM != 0 {
			return ErrCSRPrivilege
		}
		c.writeSatp(val)

	// Machine-level CSRs
	case CSRMstatus:
		c.writeMstatus(val)
	case CSRMisa:
		// MISA is typically read-only in simple implementations
		// Some bits can be cleared to disable extensions
	case CSRMedeleg:
		// Delegate exceptions to S-mode
		// Reference: riscv_cpu.c:978-980 - mask = (1 << (CAUSE_STORE_PAGE_FAULT + 1)) - 1 = 0xFFFF
		// C TinyEMU allows all 16 low bits to be written.
		c.Medeleg = uint32(val) & 0xFFFF
	case CSRMideleg:
		// Can only delegate certain interrupts
		c.Mideleg = uint32(val) & 0x222
	case CSRMie:
		c.Mie = uint32(val) & 0xAAA
	case CSRMtvec:
		// Reference: riscv_cpu.c line 991 (s->mtvec = val & ~3)
		// TinyEMU always uses direct mode - clears low 2 bits on write
		c.Mtvec = val & ^uint64(3)
	case CSRMcounteren:
		c.Mcounteren = uint32(val) & 0x7
	case CSRMscratch:
		c.Mscratch = val
	case CSRMepc:
		c.Mepc = val & ^uint64(1)
	case CSRMcause:
		c.Mcause = val
	case CSRMtval:
		c.Mtval = val
	case CSRMip:
		// Per RISC-V spec, M-mode software can write SSIP and STIP.
		// MSIP is controlled by CLINT memory-mapped register, not CSR write.
		// MTIP is read-only, controlled by CLINT hardware.
		// Reference: riscv_cpu.c line 1009
		mask := uint32(MipSSIP | MipSTIP)
		c.Mip = (c.Mip &^ mask) | (uint32(val) & mask)

	default:
		return ErrCSRNotFound
	}

	return nil
}

// readSstatus reads sstatus (supervisor view of mstatus)
// Reference: tinyemu-2019-12-21/riscv_cpu.c:764-765
func (c *CPU) readSstatus() uint64 {
	// SSTATUS_MASK includes: SIE, SPIE, SPP, FS, XS, SUM, MXR, UXL
	// SD bit is computed dynamically by getMstatus
	const sstatusMask = MstatusUIE | MstatusSIE | MstatusUPIE | MstatusSPIE |
		MstatusSPP | MstatusFS | MstatusXS | MstatusSUM | MstatusMXR |
		MstatusUXLMask | (1 << 63) // Include SD position in mask

	return c.getMstatus(sstatusMask)
}

// writeSstatus writes sstatus (supervisor view of mstatus)
// Reference: tinyemu-2019-12-21/riscv_cpu.c:910-914 - goes through set_mstatus
func (c *CPU) writeSstatus(val uint64) {
	// Reference: riscv_cpu.c:629-633 - SSTATUS_MASK0 without FS for mask
	// C goes through set_mstatus which extracts FS separately
	const sstatusMask = MstatusUIE | MstatusSIE | MstatusUPIE | MstatusSPIE |
		MstatusSPP | MstatusSUM | MstatusMXR

	// Reference: riscv_cpu.c:678-683 - flush TLB when SUM or MXR change
	mod := c.Mstatus ^ val
	if (mod & (MstatusSUM | MstatusMXR)) != 0 {
		c.FlushTLB()
	}

	// Extract FS from val (stored separately like in C)
	c.FS = uint8((val >> MstatusFSShift) & 3)

	c.Mstatus = (c.Mstatus &^ sstatusMask) | (val & sstatusMask)
}

// writeMstatus writes mstatus with field validation
// Reference: tinyemu-2019-12-21/riscv_cpu.c:674-699
func (c *CPU) writeMstatus(val uint64) {
	// Reference: riscv_cpu.c:678-683 - flush TLB if MMU config changes
	// Flush TLB when MPRV, SUM, MXR change, or when MPP changes while MPRV is set
	mod := c.Mstatus ^ val
	if (mod&(MstatusMPRV|MstatusSUM|MstatusMXR)) != 0 ||
		((c.Mstatus&MstatusMPRV) != 0 && (mod&MstatusMPP) != 0) {
		c.FlushTLB()
	}

	// Reference: riscv_cpu.c:684 - extract FS before applying mask
	// FS is stored separately, not in mstatus (to match C behavior)
	c.FS = uint8((val >> MstatusFSShift) & 3)

	// Reference: riscv_cpu.c:641-645, 686 - MSTATUS_MASK without FS
	// C mask: UIE|SIE|MIE|UPIE|SPIE|MPIE|SPP|MPP|FS|MPRV|SUM|MXR
	// Then mask = MSTATUS_MASK & ~MSTATUS_FS, so FS is excluded
	const mstatusMask = MstatusUIE | MstatusSIE | MstatusMIE |
		MstatusUPIE | MstatusSPIE | MstatusMPIE |
		MstatusSPP | MstatusMPP |
		MstatusMPRV | MstatusSUM | MstatusMXR

	c.Mstatus = (c.Mstatus &^ mstatusMask) | (val & mstatusMask)

	// Reference: riscv_cpu.c:687-696 - UXL/SXL handling for RV64
	// C uses get_base_from_xlen() to validate values: 1=RV32, 2=RV64, 3=RV128
	// C allows writing UXL/SXL if value is valid (1 <= val <= max_base)
	// For simplicity, we always force UXL/SXL to MXL (TinyEMU typically doesn't change these)
	if c.MaxXLEN == XLEN64 {
		c.Mstatus &^= MstatusUXLMask | MstatusSXLMask
		c.Mstatus |= uint64(c.MXL) << MstatusUXLShift
		c.Mstatus |= uint64(c.MXL) << MstatusSXLShift
	}
}

// writeSatp writes the SATP register with TLB flush
// Rejects unsupported address translation modes (Sv57, Sv64)
func (c *CPU) writeSatp(val uint64) {
	// On RV32 satp is 32-bit; mask off any sign-extension carried in from
	// the 32-bit source register so the mode bit (31), ASID and PPN are
	// interpreted correctly (and GetSatpMode's >>31 isn't polluted).
	if c.MaxXLEN == XLEN32 {
		val &= 0xFFFFFFFF
	}

	// Check if the requested mode is supported. The mode field position
	// depends on XLEN: bits 63:60 on RV64, a single bit at 31 on RV32.
	var requestedMode int
	if c.MaxXLEN == XLEN64 {
		requestedMode = int(val >> 60)
	} else {
		requestedMode = int((val >> 31) & 1)
	}
	if c.MaxXLEN == XLEN64 {
		// RV64: only support Bare, Sv39, Sv48
		switch requestedMode {
		case SatpModeBare, SatpModeSv39, SatpModeSv48:
			// Supported mode
		default:
			// Unsupported mode (Sv57, Sv64, etc) - ignore write
			return
		}
	} else {
		// RV32: only support Bare, Sv32
		switch requestedMode {
		case SatpModeBare, SatpModeSv32:
			// Supported mode
		default:
			// Unsupported mode - ignore write
			return
		}
	}

	oldSatp := c.Satp
	c.Satp = val

	// Flush the TLB whenever the translation changes — not just on a mode
	// change but also when the root page-table PPN changes (e.g. a context
	// switch between two processes both in Sv39). Otherwise stale entries
	// from the previous address space survive. Matches the C TinyEMU
	// reference, which flushes on every satp write.
	if oldSatp != val {
		c.FlushTLB()
	}
}
