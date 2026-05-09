package riscv

import "lukechampine.com/uint128"

// Step executes a single instruction and returns any error
func (c *CPU) Step() error {
	// Fetch instruction
	insn, err := c.FetchInstruction()
	if err != nil {
		return c.handleException()
	}

	// Determine instruction size
	insnSize := GetInsnSize(insn)

	// Decode and execute
	if insnSize == 2 {
		// Compressed instruction - expand to 32-bit equivalent
		expanded, err := ExpandCompressed(uint16(insn), c.CurXLEN)
		if err != nil {
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			return c.handleException()
		}
		insn = expanded
	}

	// Save PC before execution to detect if an exception changed it
	pcBeforeExec := c.PC

	// Execute 32-bit instruction
	// Pass insnSize so JAL/JALR can set correct link address (PC + insnSize)
	if err := c.execute32(insn, insnSize); err != nil {
		return err
	}

	// Adjust PC for compressed instructions
	// execute32 always adds 4 to PC for non-jump instructions, but compressed instructions are only 2 bytes
	// We check if this is a non-control-flow instruction by checking the opcode.
	// Control flow instructions (JAL, JALR, Branch) set PC directly to the target,
	// so we should not adjust them. Other instructions added 4, but should have added 2.
	//
	// IMPORTANT: If an exception occurred during execution, PC was set to the trap vector.
	// We must NOT adjust PC in this case. We detect this by checking if PC changed by
	// something other than +4 (the normal increment for non-control-flow instructions).
	//
	// Reference: riscv_cpu_template.h lines 191-193
	//   C TinyEMU uses separate macros for PC advancement:
	//   - C_NEXT_INSN: code_ptr += 2 (compressed)
	//   - NEXT_INSN: code_ptr += 4 (standard)
	//   - JUMP_INSN: sets PC to jump target
	//   When raise_exception2 is called (line 1723), no PC increment occurs because
	//   the exception handler sets PC to the trap vector (xtvec).
	if insnSize == 2 {
		opcode := ExtractOpcode(insn)
		if opcode != OpcodeJAL && opcode != OpcodeJALR && opcode != OpcodeBranch {
			// Only adjust if PC was incremented normally (by +4)
			// If an exception occurred, PC was set to trap vector (not pcBeforeExec+4)
			if c.PC == pcBeforeExec+4 {
				// Non-control-flow instruction: adjust from +4 to +2
				c.PC -= 2
			}
		}
	}

	// Increment instruction counter
	c.InsnCounter++

	return nil
}

// Run executes instructions for the given number of cycles.
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:200-305 (glue function setup and main loop)
// C implementation:
//   - Lines 216-217: Early return if n_cycles1 == 0
//   - Lines 218-219: Setup insn_counter_addend and s->n_cycles
//   - Lines 221-227: Initial interrupt check before loop
//   - Lines 237-288: Main execution loop with TLB handling
//   - Lines 248-257: Cycle check and interrupt check at block boundaries
//   - Lines 289-305: Cycle decrement and opcode extraction
//
// Go implementation differs by checking interrupts every instruction rather than
// at block boundaries, which provides equivalent correctness with different latency.
func (c *CPU) Run(cycles int) error {
	c.NCycles = int32(cycles)

	for c.NCycles > 0 && !c.PowerDownFlag {
		// Check pending interrupts
		if c.checkInterrupts() {
			c.NCycles--
			continue
		}

		if err := c.Step(); err != nil {
			// Exception was handled, continue
		}
		c.NCycles--
	}

	return nil
}

// CheckInterrupts checks for and handles pending interrupts.
// This is useful when using Step() in a loop instead of Run(),
// as Step() does not check for interrupts.
// Returns true if an interrupt was taken.
//
// Reference: riscv_cpu.c target_interrupt_handler
func (c *CPU) CheckInterrupts() bool {
	return c.checkInterrupts()
}

// checkInterrupts checks for and handles pending interrupts.
// Returns true if an interrupt was taken.
// Reference: tinyemu-2019-12-21/riscv_cpu.c:1159-1197 (get_pending_irq_mask, raise_interrupt)
func (c *CPU) checkInterrupts() bool {
	pending := c.Mip & c.Mie
	if pending == 0 {
		return false
	}

	// Determine which interrupts are enabled at current privilege level
	// Reference: riscv_cpu.c:1167-1182 (get_pending_irq_mask)
	var enabled uint32
	switch c.Priv {
	case PrivMachine:
		if c.Mstatus&MstatusMIE != 0 {
			enabled = ^c.Mideleg
		}
	case PrivSupervisor:
		enabled = ^c.Mideleg
		if c.Mstatus&MstatusSIE != 0 {
			enabled |= c.Mideleg
		}
	default: // PrivUser
		enabled = ^uint32(0)
	}
	enabled &= pending

	if enabled == 0 {
		return false
	}

	// Use ctz32 to find lowest set bit - this is the interrupt number
	// Lower bit numbers have higher priority per TinyEMU
	// Reference: riscv_cpu.c:1194 - irq_num = ctz32(mask)
	cause := ctz32(enabled)

	// Raise interrupt (cause has interrupt bit set in mcause)
	c.raiseInterrupt(cause)
	c.PowerDownFlag = false
	return true
}

// ctz32 counts trailing zeros in a 32-bit value.
// Returns the bit position of the lowest set bit (0-31), or 32 if val is 0.
// Reference: used by riscv_cpu.c:1194 for interrupt priority
func ctz32(val uint32) int {
	if val == 0 {
		return 32
	}
	n := 0
	if val&0x0000FFFF == 0 {
		n += 16
		val >>= 16
	}
	if val&0x000000FF == 0 {
		n += 8
		val >>= 8
	}
	if val&0x0000000F == 0 {
		n += 4
		val >>= 4
	}
	if val&0x00000003 == 0 {
		n += 2
		val >>= 2
	}
	if val&0x00000001 == 0 {
		n += 1
	}
	return n
}

// raiseInterrupt raises an interrupt with the given cause
// Per TinyEMU: xPIE = IE bit for current privilege mode (not xIE)
// Reference: tinyemu-2019-12-21/riscv_cpu.c:1042-1120 (raise_exception2)
func (c *CPU) raiseInterrupt(cause int) {
	// Interrupt cause has MSB set
	fullCause := uint64(cause) | CauseInterrupt

	// Determine if delegated to S-mode
	delegated := false
	if c.Priv < PrivMachine {
		delegated = (c.Mideleg>>cause)&1 != 0
	}

	if delegated {
		c.Scause = fullCause
		c.Sepc = c.PC
		c.Stval = 0

		// Update sstatus: SPIE = IE bit for current priv, SPP = current priv, SIE = 0
		// Extract the IE bit at position c.Priv (UIE=0, SIE=1)
		spie := (c.Mstatus >> c.Priv) & 1
		c.Mstatus = (c.Mstatus &^ MstatusSPIE) | (spie << MstatusSPIEShift)
		c.Mstatus = (c.Mstatus &^ MstatusSPP) | (uint64(c.Priv) << MstatusSPPShift)
		c.Mstatus &^= MstatusSIE

		// Use SetPriv to handle TLB flush and CurXLEN update
		c.SetPriv(PrivSupervisor)
		c.PC = c.Stvec & ^uint64(3) // Vectored or direct
	} else {
		c.Mcause = fullCause
		c.Mepc = c.PC
		c.Mtval = 0

		// Update mstatus: MPIE = IE bit for current priv, MPP = current priv, MIE = 0
		// Extract the IE bit at position c.Priv (UIE=0, SIE=1, MIE=3)
		mpie := (c.Mstatus >> c.Priv) & 1
		c.Mstatus = (c.Mstatus &^ MstatusMPIE) | (mpie << MstatusMPIEShift)
		c.Mstatus = (c.Mstatus &^ MstatusMPP) | (uint64(c.Priv) << MstatusMPPShift)
		c.Mstatus &^= MstatusMIE

		// Use SetPriv to handle TLB flush and CurXLEN update
		c.SetPriv(PrivMachine)
		c.PC = c.Mtvec & ^uint64(3)
	}
}

// handleException handles a pending exception
// Per TinyEMU: xPIE = IE bit for current privilege mode (not xIE)
// Reference: riscv_cpu.c lines 1042-1120 (raise_exception2)
func (c *CPU) handleException() error {
	if c.PendingException < 0 {
		return nil
	}

	cause := c.PendingException
	tval := c.PendingTval
	c.PendingException = -1

	// Trace the exception before handling (matches C DUMP_* behavior)
	// Reference: riscv_cpu.c lines 1048-1081
	if c.Tracer != nil {
		isInterrupt := false // Exceptions (not interrupts) use this path
		c.Tracer.TraceRaiseException(c, cause, tval, isInterrupt)
	}

	// Determine if delegated to S-mode
	delegated := false
	if c.Priv < PrivMachine {
		delegated = (c.Medeleg>>cause)&1 != 0
	}

	oldPriv := c.Priv
	if delegated {
		c.Scause = uint64(cause)
		c.Sepc = c.PC
		c.Stval = tval

		// Update sstatus: SPIE = IE bit for current priv, SPP = current priv, SIE = 0
		spie := (c.Mstatus >> c.Priv) & 1
		c.Mstatus = (c.Mstatus &^ MstatusSPIE) | (spie << MstatusSPIEShift)
		c.Mstatus = (c.Mstatus &^ MstatusSPP) | (uint64(c.Priv) << MstatusSPPShift)
		c.Mstatus &^= MstatusSIE

		// Use SetPriv to handle TLB flush and CurXLEN update
		// Reference: riscv_cpu.c:1106 - set_priv(s, PRV_S)
		c.SetPriv(PrivSupervisor)
		c.PC = c.Stvec & ^uint64(3)
	} else {
		c.Mcause = uint64(cause)
		c.Mepc = c.PC
		c.Mtval = tval

		// Update mstatus: MPIE = IE bit for current priv, MPP = current priv, MIE = 0
		mpie := (c.Mstatus >> c.Priv) & 1
		c.Mstatus = (c.Mstatus &^ MstatusMPIE) | (mpie << MstatusMPIEShift)
		c.Mstatus = (c.Mstatus &^ MstatusMPP) | (uint64(c.Priv) << MstatusMPPShift)
		c.Mstatus &^= MstatusMIE

		// Use SetPriv to handle TLB flush and CurXLEN update
		// Reference: riscv_cpu.c:1117 - set_priv(s, PRV_M)
		c.SetPriv(PrivMachine)
		c.PC = c.Mtvec & ^uint64(3)
	}

	// Trace privilege change if it occurred
	if c.Tracer != nil && oldPriv != c.Priv {
		c.Tracer.TracePrivChange(c, oldPriv, c.Priv)
	}

	return nil
}

// execute32 decodes and executes a 32-bit instruction
// execute32 executes a 32-bit instruction (or expanded compressed instruction).
// insnSize is the original instruction size (2 for compressed, 4 for 32-bit),
// used for calculating the correct link address in JAL/JALR.
func (c *CPU) execute32(insn uint32, insnSize int) error {
	opcode := ExtractOpcode(insn)
	rd := int(ExtractRd(insn))
	rs1 := int(ExtractRs1(insn))
	rs2 := int(ExtractRs2(insn))
	funct3 := ExtractFunct3(insn)
	funct7 := ExtractFunct7(insn)

	// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:778-942 (base integer instructions)
	switch opcode {
	case OpcodeLUI:
		// LUI rd, imm - Load Upper Immediate
		// Reference: riscv_cpu_template.h:778-781
		imm := ExtractUImm(insn)
		c.SetReg(rd, uint64(imm))
		c.PC += 4

	case OpcodeAUIPC:
		// AUIPC rd, imm - Add Upper Immediate to PC
		// Reference: riscv_cpu_template.h:782-785
		imm := ExtractUImm(insn)
		c.SetReg(rd, uint64(int64(c.PC)+imm))
		c.PC += 4

	case OpcodeJAL:
		// JAL rd, offset - Jump and Link
		// Link address is PC + insnSize (2 for compressed, 4 for 32-bit)
		// Reference: riscv_cpu_template.h:786-795
		imm := ExtractJImm(insn)
		c.SetReg(rd, c.PC+uint64(insnSize))
		c.PC = uint64(int64(c.PC) + imm)

	case OpcodeJALR:
		// JALR rd, rs1, offset - Jump and Link Register
		// Link address is PC + insnSize (2 for compressed, 4 for 32-bit)
		// Reference: riscv_cpu_template.h:796-802
		imm := ExtractIImm(insn)
		target := uint64((int64(c.GetReg(rs1)) + imm) & ^int64(1))
		c.SetReg(rd, c.PC+uint64(insnSize))
		c.PC = target

	case OpcodeBranch:
		if err := c.executeBranch(insn, funct3, rs1, rs2, insnSize); err != nil {
			return err
		}

	case OpcodeLoad:
		if err := c.executeLoad(insn, funct3, rd, rs1); err != nil {
			return err
		}

	case OpcodeStore:
		if err := c.executeStore(insn, funct3, rs1, rs2); err != nil {
			return err
		}

	case OpcodeOpImm:
		c.executeOpImm(insn, funct3, rd, rs1)

	case OpcodeOpImm32:
		// RV64 only: ADDIW, SLLIW, SRLIW, SRAIW
		// Reference: tinyemu-2019-12-21/riscv_cpu_template.h (these are illegal in RV32)
		if c.CurXLEN == XLEN32 {
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			return c.handleException()
		}
		c.executeOpImm32(insn, funct3, rd, rs1)

	case OpcodeOp:
		c.executeOp(insn, funct3, funct7, rd, rs1, rs2)

	case OpcodeOp32:
		// RV64 only: ADDW, SUBW, SLLW, SRLW, SRAW, MULW, DIVW, etc.
		// Reference: tinyemu-2019-12-21/riscv_cpu_template.h (these are illegal in RV32)
		if c.CurXLEN == XLEN32 {
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			return c.handleException()
		}
		c.executeOp32(insn, funct3, funct7, rd, rs1, rs2)

	case OpcodeMiscMem:
		c.executeMiscMem(insn, funct3)

	case OpcodeSystem:
		return c.executeSystem(insn, funct3, rd, rs1)

	case OpcodeAMO:
		return c.executeAMO(insn, funct3, rd, rs1, rs2)

	case OpcodeFPLoad:
		return c.executeFPLoad(insn, funct3, rd, rs1)

	case OpcodeFPStore:
		return c.executeFPStore(insn, funct3, rs1, rs2)

	case OpcodeFMADD:
		return c.executeFMADD(insn)

	case OpcodeFMSUB:
		return c.executeFMSUB(insn)

	case OpcodeFNMSUB:
		return c.executeFNMSUB(insn)

	case OpcodeFNMADD:
		return c.executeFNMADD(insn)

	case OpcodeFPOp:
		return c.executeFPOp(insn)

	default:
		c.SetPendingException(CauseIllegalInsn, uint64(insn))
		return c.handleException()
	}

	return nil
}

// executeBranch executes branch instructions
// insnSize is the original instruction size (2 for compressed, 4 for 32-bit)
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:803-828
func (c *CPU) executeBranch(insn uint32, funct3 uint32, rs1, rs2, insnSize int) error {
	val1 := c.GetReg(rs1)
	val2 := c.GetReg(rs2)
	imm := ExtractBImm(insn)

	var taken bool
	switch funct3 {
	case Funct3BEQ:
		taken = val1 == val2
	case Funct3BNE:
		taken = val1 != val2
	case Funct3BLT:
		taken = int64(val1) < int64(val2)
	case Funct3BGE:
		taken = int64(val1) >= int64(val2)
	case Funct3BLTU:
		taken = val1 < val2
	case Funct3BGEU:
		taken = val1 >= val2
	default:
		c.SetPendingException(CauseIllegalInsn, uint64(insn))
		return c.handleException()
	}

	if taken {
		c.PC = uint64(int64(c.PC) + imm)
	} else {
		c.PC += uint64(insnSize)
	}
	return nil
}

// executeLoad executes load instructions
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:829-907
func (c *CPU) executeLoad(insn uint32, funct3 uint32, rd, rs1 int) error {
	imm := ExtractIImm(insn)
	addr := uint64(int64(c.GetReg(rs1)) + imm)

	var val uint64
	var err error

	switch funct3 {
	case Funct3LB:
		var v uint8
		v, err = c.LoadU8(addr)
		val = uint64(int64(int8(v))) // Sign extend
	case Funct3LH:
		var v uint16
		v, err = c.LoadU16(addr)
		val = uint64(int64(int16(v))) // Sign extend
	case Funct3LW:
		var v uint32
		v, err = c.LoadU32(addr)
		val = uint64(int64(int32(v))) // Sign extend
	case Funct3LD:
		// LD is RV64/RV128 only
		// Reference: tinyemu-2019-12-21/riscv_cpu_template.h (XLEN >= 64)
		if c.CurXLEN == XLEN32 {
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			return c.handleException()
		}
		val, err = c.LoadU64(addr)
	case Funct3LBU:
		var v uint8
		v, err = c.LoadU8(addr)
		val = uint64(v)
	case Funct3LHU:
		var v uint16
		v, err = c.LoadU16(addr)
		val = uint64(v)
	case Funct3LWU:
		// LWU is RV64/RV128 only
		// Reference: tinyemu-2019-12-21/riscv_cpu_template.h (XLEN >= 64)
		if c.CurXLEN == XLEN32 {
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			return c.handleException()
		}
		var v uint32
		v, err = c.LoadU32(addr)
		val = uint64(v)
	default:
		c.SetPendingException(CauseIllegalInsn, uint64(insn))
		return c.handleException()
	}

	if err != nil {
		return c.handleException()
	}

	c.SetReg(rd, val)
	c.PC += 4
	return nil
}

// executeStore executes store instructions
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:908-942
func (c *CPU) executeStore(insn uint32, funct3 uint32, rs1, rs2 int) error {
	imm := ExtractSImm(insn)
	addr := uint64(int64(c.GetReg(rs1)) + imm)
	val := c.GetReg(rs2)

	var err error

	switch funct3 {
	case 0: // SB
		err = c.StoreU8(addr, uint8(val))
	case 1: // SH
		err = c.StoreU16(addr, uint16(val))
	case 2: // SW
		err = c.StoreU32(addr, uint32(val))
	case 3: // SD
		// SD is RV64/RV128 only
		// Reference: tinyemu-2019-12-21/riscv_cpu_template.h (XLEN >= 64)
		if c.CurXLEN == XLEN32 {
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			return c.handleException()
		}
		err = c.StoreU64(addr, val)
	default:
		c.SetPendingException(CauseIllegalInsn, uint64(insn))
		return c.handleException()
	}

	if err != nil {
		return c.handleException()
	}

	c.PC += 4
	return nil
}

// executeOpImm executes OP-IMM instructions (ADDI, SLTI, etc.)
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:943-982
func (c *CPU) executeOpImm(insn uint32, funct3 uint32, rd, rs1 int) {
	imm := ExtractIImm(insn)
	val := c.GetReg(rs1)

	var result uint64

	switch funct3 {
	case Funct3ADDI:
		result = uint64(int64(val) + imm)
	case Funct3SLTI:
		if int64(val) < imm {
			result = 1
		} else {
			result = 0
		}
	case Funct3SLTIU:
		if val < uint64(imm) {
			result = 1
		} else {
			result = 0
		}
	case Funct3XORI:
		result = val ^ uint64(imm)
	case Funct3ORI:
		result = val | uint64(imm)
	case Funct3ANDI:
		result = val & uint64(imm)
	case Funct3SLLI:
		// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:950-953
		// imm & ~(XLEN - 1) != 0 is illegal
		// For RV32: shamt must be 0-31, for RV64: shamt must be 0-63
		if (imm & ^int64(c.CurXLEN-1)) != 0 {
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			c.handleException()
			return
		}
		shamt := uint32(imm) & uint32(c.CurXLEN-1)
		result = val << shamt
	case Funct3SRLI: // Also SRAI
		// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:964-971
		// imm & ~((XLEN - 1) | 0x400) != 0 is illegal
		// For RV32: shamt must be 0-31, for RV64: shamt must be 0-63
		// Bit 10 (0x400) indicates arithmetic shift (SRAI)
		if (imm & ^int64(int(c.CurXLEN)-1|0x400)) != 0 {
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			c.handleException()
			return
		}
		shamt := uint32(imm) & uint32(c.CurXLEN-1)
		if (insn>>30)&1 != 0 {
			// SRAI - arithmetic shift
			result = uint64(int64(val) >> shamt)
		} else {
			// SRLI - logical shift
			result = val >> shamt
		}
	default:
		c.SetPendingException(CauseIllegalInsn, uint64(insn))
		c.PC += 4
		return
	}

	c.SetReg(rd, result)
	c.PC += 4
}

// executeOpImm32 executes OP-IMM-32 instructions (RV64: ADDIW, etc.)
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:983-1011
func (c *CPU) executeOpImm32(insn uint32, funct3 uint32, rd, rs1 int) {
	imm := ExtractIImm(insn)
	val := uint32(c.GetReg(rs1))

	var result int32

	switch funct3 {
	case 0: // ADDIW
		result = int32(val) + int32(imm)
	case 1: // SLLIW
		// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:992-995
		// For W instructions: check that only bits [4:0] are set
		if (imm & ^int64(31)) != 0 {
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			c.PC += 4
			return
		}
		shamt := ExtractShamt32(insn)
		result = int32(val << shamt)
	case 5: // SRLIW / SRAIW
		// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:997-1003
		// For W instructions: check that only bits [4:0] and bit 10 (0x400) are allowed
		if (imm & ^int64(31|0x400)) != 0 {
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			c.PC += 4
			return
		}
		shamt := ExtractShamt32(insn)
		if (insn>>30)&1 != 0 {
			// SRAIW
			result = int32(val) >> shamt
		} else {
			// SRLIW
			result = int32(val >> shamt)
		}
	default:
		c.SetPendingException(CauseIllegalInsn, uint64(insn))
		c.PC += 4
		return
	}

	c.SetReg(rd, uint64(int64(result)))
	c.PC += 4
}

// executeOp executes OP instructions (ADD, SUB, etc.)
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:1041-1116
func (c *CPU) executeOp(insn uint32, funct3, funct7 uint32, rd, rs1, rs2 int) {
	val1 := c.GetReg(rs1)
	val2 := c.GetReg(rs2)

	var result uint64

	// Check for M extension (multiply/divide)
	if funct7 == 1 {
		// For RV32, use 32-bit operations for MULH/MULHSU/MULHU
		// Reference: tinyemu-2019-12-21/riscv_cpu_template.h (XLEN-specific operations)
		if c.CurXLEN == XLEN32 {
			result = c.executeMulDivRV32(funct3, uint32(val1), uint32(val2))
		} else {
			result = c.executeMulDiv(funct3, val1, val2)
		}
	} else {
		// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:1076-1077
		// For base integer operations, only funct7 values 0x00 and 0x20 are allowed
		if funct7 & ^uint32(0x20) != 0 {
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			c.PC += 4
			return
		}
		// Base integer operations
		switch funct3 {
		case Funct3ADD:
			if funct7 == 0x20 {
				// SUB
				result = val1 - val2
			} else {
				// ADD
				result = val1 + val2
			}
		case Funct3SLL:
			if funct7 != 0 {
				c.SetPendingException(CauseIllegalInsn, uint64(insn))
				c.PC += 4
				return
			}
			// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:1078
			// Shift amount is masked by XLEN-1
			result = val1 << (val2 & uint64(c.CurXLEN-1))
		case Funct3SLT:
			if funct7 != 0 {
				c.SetPendingException(CauseIllegalInsn, uint64(insn))
				c.PC += 4
				return
			}
			if int64(val1) < int64(val2) {
				result = 1
			}
		case Funct3SLTU:
			if funct7 != 0 {
				c.SetPendingException(CauseIllegalInsn, uint64(insn))
				c.PC += 4
				return
			}
			if val1 < val2 {
				result = 1
			}
		case Funct3XOR:
			if funct7 != 0 {
				c.SetPendingException(CauseIllegalInsn, uint64(insn))
				c.PC += 4
				return
			}
			result = val1 ^ val2
		case Funct3SRL:
			// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:1098-1107
			// Shift amount is masked by XLEN-1
			if funct7 == 0x20 {
				// SRA
				result = uint64(int64(val1) >> (val2 & uint64(c.CurXLEN-1)))
			} else if funct7 == 0 {
				// SRL
				result = val1 >> (val2 & uint64(c.CurXLEN-1))
			} else {
				c.SetPendingException(CauseIllegalInsn, uint64(insn))
				c.PC += 4
				return
			}
		case Funct3OR:
			if funct7 != 0 {
				c.SetPendingException(CauseIllegalInsn, uint64(insn))
				c.PC += 4
				return
			}
			result = val1 | val2
		case Funct3AND:
			if funct7 != 0 {
				c.SetPendingException(CauseIllegalInsn, uint64(insn))
				c.PC += 4
				return
			}
			result = val1 & val2
		default:
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			c.PC += 4
			return
		}
	}

	c.SetReg(rd, result)
	c.PC += 4
}

// executeOp32 executes OP-32 instructions (RV64: ADDW, SUBW, etc.)
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:1117-1170
func (c *CPU) executeOp32(insn uint32, funct3, funct7 uint32, rd, rs1, rs2 int) {
	val1 := uint32(c.GetReg(rs1))
	val2 := uint32(c.GetReg(rs2))

	var result int32

	// Check for M extension (multiply/divide on 32-bit)
	if funct7 == 1 {
		result = c.executeMulDiv32(funct3, val1, val2)
	} else {
		// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:1143-1145
		// For base integer operations, only funct7 values 0x00 and 0x20 are allowed
		if funct7 & ^uint32(0x20) != 0 {
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			c.PC += 4
			return
		}
		switch funct3 {
		case 0: // ADDW / SUBW
			if funct7 == 0x20 {
				result = int32(val1) - int32(val2)
			} else {
				result = int32(val1) + int32(val2)
			}
		case 1: // SLLW
			if funct7 != 0 {
				c.SetPendingException(CauseIllegalInsn, uint64(insn))
				c.PC += 4
				return
			}
			result = int32(val1 << (val2 & 31))
		case 5: // SRLW / SRAW
			if funct7 == 0x20 {
				result = int32(val1) >> (val2 & 31)
			} else if funct7 == 0 {
				result = int32(val1 >> (val2 & 31))
			} else {
				c.SetPendingException(CauseIllegalInsn, uint64(insn))
				c.PC += 4
				return
			}
		default:
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			c.PC += 4
			return
		}
	}

	c.SetReg(rd, uint64(int64(result)))
	c.PC += 4
}

// executeMulDiv executes M extension operations (64-bit)
func (c *CPU) executeMulDiv(funct3 uint32, val1, val2 uint64) uint64 {
	switch funct3 {
	case Funct3MUL:
		return val1 * val2
	case Funct3MULH:
		return mulh64(int64(val1), int64(val2))
	case Funct3MULHSU:
		return mulhsu64(int64(val1), val2)
	case Funct3MULHU:
		return mulhu64(val1, val2)
	case Funct3DIV:
		return div64(int64(val1), int64(val2))
	case Funct3DIVU:
		return divu64(val1, val2)
	case Funct3REM:
		return rem64(int64(val1), int64(val2))
	case Funct3REMU:
		return remu64(val1, val2)
	}
	return 0
}

// executeMulDiv32 executes M extension operations (32-bit for RV64)
func (c *CPU) executeMulDiv32(funct3 uint32, val1, val2 uint32) int32 {
	switch funct3 {
	case 0: // MULW
		return int32(val1) * int32(val2)
	case 4: // DIVW
		return div32(int32(val1), int32(val2))
	case 5: // DIVUW
		return int32(divu32(val1, val2))
	case 6: // REMW
		return rem32(int32(val1), int32(val2))
	case 7: // REMUW
		return int32(remu32(val1, val2))
	}
	return 0
}

// executeMulDivRV32 executes M extension operations for RV32 mode.
// This differs from executeMulDiv in that MULH/MULHSU/MULHU compute
// the high 32 bits of a 64-bit product, not the high 64 bits of a 128-bit product.
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h (XLEN=32)
func (c *CPU) executeMulDivRV32(funct3 uint32, val1, val2 uint32) uint64 {
	switch funct3 {
	case Funct3MUL:
		return uint64(int64(int32(val1) * int32(val2)))
	case Funct3MULH:
		return uint64(int64(int32(mulh32(int32(val1), int32(val2)))))
	case Funct3MULHSU:
		return uint64(int64(int32(mulhsu32(int32(val1), val2))))
	case Funct3MULHU:
		return uint64(int64(int32(mulhu32(val1, val2))))
	case Funct3DIV:
		return uint64(int64(div32(int32(val1), int32(val2))))
	case Funct3DIVU:
		return uint64(int64(int32(divu32(val1, val2))))
	case Funct3REM:
		return uint64(int64(rem32(int32(val1), int32(val2))))
	case Funct3REMU:
		return uint64(int64(int32(remu32(val1, val2))))
	}
	return 0
}

// Division helper functions with edge case handling
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:37-75

// div64 performs signed division with RISC-V semantics.
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:37-46
func div64(a, b int64) uint64 {
	if b == 0 {
		return ^uint64(0) // -1
	}
	if a == -1<<63 && b == -1 {
		return uint64(a) // Overflow
	}
	return uint64(a / b)
}

// divu64 performs unsigned division with RISC-V semantics.
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:48-55
func divu64(a, b uint64) uint64 {
	if b == 0 {
		return ^uint64(0)
	}
	return a / b
}

// rem64 performs signed remainder with RISC-V semantics.
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:57-66
func rem64(a, b int64) uint64 {
	if b == 0 {
		return uint64(a)
	}
	if a == -1<<63 && b == -1 {
		return 0
	}
	return uint64(a % b)
}

// remu64 performs unsigned remainder with RISC-V semantics.
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:68-75
func remu64(a, b uint64) uint64 {
	if b == 0 {
		return a
	}
	return a % b
}

// div32 performs 32-bit signed division with RISC-V semantics (for DIVW).
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:37-46 (using XLEN=32)
func div32(a, b int32) int32 {
	if b == 0 {
		return -1
	}
	if a == -1<<31 && b == -1 {
		return a
	}
	return a / b
}

// divu32 performs 32-bit unsigned division with RISC-V semantics (for DIVUW).
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:48-55 (using XLEN=32)
func divu32(a, b uint32) uint32 {
	if b == 0 {
		return ^uint32(0)
	}
	return a / b
}

// rem32 performs 32-bit signed remainder with RISC-V semantics (for REMW).
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:57-66 (using XLEN=32)
func rem32(a, b int32) int32 {
	if b == 0 {
		return a
	}
	if a == -1<<31 && b == -1 {
		return 0
	}
	return a % b
}

// remu32 performs 32-bit unsigned remainder with RISC-V semantics (for REMUW).
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:68-75 (using XLEN=32)
func remu32(a, b uint32) uint32 {
	if b == 0 {
		return a
	}
	return a % b
}

// High multiplication helpers
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:96-168

// mulhu64 computes the upper 64 bits of unsigned 64x64 multiplication.
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:106-109 (int128 version)
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:123-146 (manual version)
func mulhu64(a, b uint64) uint64 {
	// Split into 32-bit halves
	a0 := a & 0xFFFFFFFF
	a1 := a >> 32
	b0 := b & 0xFFFFFFFF
	b1 := b >> 32

	// Partial products
	p00 := a0 * b0
	p01 := a0 * b1
	p10 := a1 * b0
	p11 := a1 * b1

	// Sum with carry tracking
	carry := (p00 >> 32) + (p01 & 0xFFFFFFFF) + (p10 & 0xFFFFFFFF)
	return p11 + (p01 >> 32) + (p10 >> 32) + (carry >> 32)
}

// mulh64 computes the upper 64 bits of signed 64x64 multiplication.
// Uses formula: mulh = mulhu - b*(a<0) - a*(b<0)
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:96-99 (int128 version)
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:150-159 (manual version)
func mulh64(a, b int64) uint64 {
	result := mulhu64(uint64(a), uint64(b))
	if a < 0 {
		result -= uint64(b)
	}
	if b < 0 {
		result -= uint64(a)
	}
	return result
}

// mulhsu64 computes the upper 64 bits of signed*unsigned 64x64 multiplication.
// Uses formula: mulhsu = mulhu - b*(a<0)
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:101-104 (int128 version)
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:161-168 (manual version)
func mulhsu64(a int64, b uint64) uint64 {
	result := mulhu64(uint64(a), b)
	if a < 0 {
		result -= b
	}
	return result
}

// 32-bit high multiplication helpers for RV32
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:79-92

// mulh32 computes the upper 32 bits of signed 32x32 multiplication.
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:79-82
func mulh32(a, b int32) uint32 {
	return uint32((int64(a) * int64(b)) >> 32)
}

// mulhsu32 computes the upper 32 bits of signed*unsigned 32x32 multiplication.
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:84-87
func mulhsu32(a int32, b uint32) uint32 {
	return uint32((int64(a) * int64(b)) >> 32)
}

// mulhu32 computes the upper 32 bits of unsigned 32x32 multiplication.
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:89-92
func mulhu32(a, b uint32) uint32 {
	return uint32((uint64(a) * uint64(b)) >> 32)
}

// 128-bit division and multiplication for RV128 support
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:37-168 (using XLEN=128)

// div128 performs 128-bit signed division with RISC-V semantics.
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:37-46 (using XLEN=128)
func div128(a, b uint128.Uint128) uint128.Uint128 {
	if b.IsZero() {
		return uint128.Max // -1 in two's complement
	}
	// Check for overflow: MIN_INT128 / -1
	minInt128 := uint128.New(0, 0x8000000000000000)
	negOne := uint128.Max
	if a.Equals(minInt128) && b.Equals(negOne) {
		return a
	}
	// Signed division
	return signedDiv128(a, b)
}

// divu128 performs 128-bit unsigned division with RISC-V semantics.
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:48-55 (using XLEN=128)
func divu128(a, b uint128.Uint128) uint128.Uint128 {
	if b.IsZero() {
		return uint128.Max // -1 as unsigned
	}
	return a.Div(b)
}

// rem128 performs 128-bit signed remainder with RISC-V semantics.
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:57-66 (using XLEN=128)
func rem128(a, b uint128.Uint128) uint128.Uint128 {
	if b.IsZero() {
		return a
	}
	// Check for overflow: MIN_INT128 % -1
	minInt128 := uint128.New(0, 0x8000000000000000)
	negOne := uint128.Max
	if a.Equals(minInt128) && b.Equals(negOne) {
		return uint128.Zero
	}
	return signedRem128(a, b)
}

// remu128 performs 128-bit unsigned remainder with RISC-V semantics.
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:68-75 (using XLEN=128)
func remu128(a, b uint128.Uint128) uint128.Uint128 {
	if b.IsZero() {
		return a
	}
	return a.Mod(b)
}

// mulhu128 computes the upper 128 bits of unsigned 128x128 multiplication.
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:123-146 (using XLEN=128, UHALF_LEN=64)
func mulhu128(a, b uint128.Uint128) uint128.Uint128 {
	// Split into 64-bit halves
	a0 := a.Lo
	a1 := a.Hi
	b0 := b.Lo
	b1 := b.Hi

	// Partial products (each is 128 bits)
	// r00Lo is discarded as it contributes only to the low 128 bits of the 256-bit result
	r00Hi, _ := mul64to128(a0, b0)
	r01Hi, r01Lo := mul64to128(a0, b1)
	r10Hi, r10Lo := mul64to128(a1, b0)
	r11Hi, r11Lo := mul64to128(a1, b1)

	// Sum with carry tracking (following C algorithm from riscv_cpu_template.h:137-142)
	// c = (r00 >> 64) + (uint64)r01 + (uint64)r10
	c := uint128.New(r00Hi, 0)
	c = c.Add(uint128.From64(r01Lo))
	c = c.Add(uint128.From64(r10Lo))

	// c = (c >> 64) + (r01 >> 64) + (r10 >> 64) + (uint64)r11
	c = uint128.From64(c.Hi)
	c = c.Add(uint128.From64(r01Hi))
	c = c.Add(uint128.From64(r10Hi))
	c = c.Add(uint128.From64(r11Lo))
	r2 := c.Lo

	// r3 = (c >> 64) + (r11 >> 64)
	r3 := c.Hi + r11Hi

	return uint128.New(r2, r3)
}

// mulh128 computes the upper 128 bits of signed 128x128 multiplication.
// Uses formula: mulh = mulhu - b*(a<0) - a*(b<0)
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:150-159 (using XLEN=128)
func mulh128(a, b uint128.Uint128) uint128.Uint128 {
	result := mulhu128(a, b)
	// Check if a is negative (high bit set)
	if a.Hi >= 0x8000000000000000 {
		result = result.SubWrap(b)
	}
	// Check if b is negative (high bit set)
	if b.Hi >= 0x8000000000000000 {
		result = result.SubWrap(a)
	}
	return result
}

// mulhsu128 computes the upper 128 bits of signed*unsigned 128x128 multiplication.
// Uses formula: mulhsu = mulhu - b*(a<0)
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:161-168 (using XLEN=128)
func mulhsu128(a, b uint128.Uint128) uint128.Uint128 {
	result := mulhu128(a, b)
	// Check if a is negative (high bit set)
	if a.Hi >= 0x8000000000000000 {
		result = result.SubWrap(b)
	}
	return result
}

// mul64to128 multiplies two 64-bit numbers and returns a 128-bit result as (hi, lo).
func mul64to128(a, b uint64) (uint64, uint64) {
	// Split into 32-bit halves
	a0 := a & 0xFFFFFFFF
	a1 := a >> 32
	b0 := b & 0xFFFFFFFF
	b1 := b >> 32

	// Partial products
	p00 := a0 * b0
	p01 := a0 * b1
	p10 := a1 * b0
	p11 := a1 * b1

	// Combine
	lo := p00 + (p01 << 32) + (p10 << 32)
	hi := p11 + (p01 >> 32) + (p10 >> 32)

	// Handle carry from lo to hi
	carry := uint64(0)
	if (p00>>32)+(p01&0xFFFFFFFF)+(p10&0xFFFFFFFF) >= 0x100000000 {
		carry = 1
	}
	hi += carry

	return hi, lo
}

// signedDiv128 performs signed 128-bit division.
func signedDiv128(a, b uint128.Uint128) uint128.Uint128 {
	aSign := a.Hi >= 0x8000000000000000
	bSign := b.Hi >= 0x8000000000000000

	// Get absolute values using SubWrap (two's complement negation)
	if aSign {
		a = uint128.Zero.SubWrap(a)
	}
	if bSign {
		b = uint128.Zero.SubWrap(b)
	}

	result := a.Div(b)

	// Apply sign using SubWrap
	if aSign != bSign {
		result = uint128.Zero.SubWrap(result)
	}
	return result
}

// signedRem128 performs signed 128-bit remainder.
func signedRem128(a, b uint128.Uint128) uint128.Uint128 {
	aSign := a.Hi >= 0x8000000000000000

	// Get absolute values using SubWrap (two's complement negation)
	aAbs := a
	bAbs := b
	if aSign {
		aAbs = uint128.Zero.SubWrap(a)
	}
	if b.Hi >= 0x8000000000000000 {
		bAbs = uint128.Zero.SubWrap(b)
	}

	result := aAbs.Mod(bAbs)

	// Remainder has same sign as dividend
	if aSign {
		result = uint128.Zero.SubWrap(result)
	}
	return result
}

// executeMiscMem executes FENCE and FENCE.I
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:1351-1375
func (c *CPU) executeMiscMem(insn uint32, funct3 uint32) {
	switch funct3 {
	case 0: // FENCE
		// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:1354-1357
		// Check reserved bits: rd, rs1, and fm must be as expected
		if insn&0xf00fff80 != 0 {
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			c.PC += 4
			return
		}
		// Memory ordering - no-op for single-threaded emulator
	case 1: // FENCE.I
		// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:1358-1361
		// Must be exact encoding: 0x0000100f
		if insn != 0x0000100f {
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			c.PC += 4
			return
		}
		// Instruction cache flush - flush TLB code entries
		for i := range c.TLBCode {
			c.TLBCode[i].VAddr = ^uint64(0)
		}
	default:
		c.SetPendingException(CauseIllegalInsn, uint64(insn))
	}
	c.PC += 4
}

// executeSystem executes SYSTEM instructions (ECALL, EBREAK, CSR*, etc.)
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:1225-1350
func (c *CPU) executeSystem(insn uint32, funct3 uint32, rd, rs1 int) error {
	if funct3 == Funct3PRIV {
		return c.executePriv(insn)
	}

	// CSR operations
	csr := ExtractCSR(insn)
	var val uint64
	if funct3&0x4 != 0 {
		// Immediate form (CSRRWI, CSRRSI, CSRRCI)
		val = uint64(rs1)
	} else {
		val = c.GetReg(rs1)
	}

	// Read CSR
	csrVal, err := c.ReadCSR(csr)
	if err != nil {
		c.SetPendingException(CauseIllegalInsn, uint64(insn))
		return c.handleException()
	}

	// Compute new value
	var newVal uint64
	writeCSR := true
	switch funct3 & 3 {
	case 1: // CSRRW / CSRRWI
		newVal = val
	case 2: // CSRRS / CSRRSI
		newVal = csrVal | val
		if rs1 == 0 {
			writeCSR = false
		}
	case 3: // CSRRC / CSRRCI
		newVal = csrVal &^ val
		if rs1 == 0 {
			writeCSR = false
		}
	}

	// Write CSR
	if writeCSR {
		if err := c.WriteCSR(csr, newVal); err != nil {
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			return c.handleException()
		}
	}

	c.SetReg(rd, csrVal)
	c.PC += 4
	return nil
}

// executePriv executes privileged instructions
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:1279-1345
func (c *CPU) executePriv(insn uint32) error {
	imm := insn >> 20

	switch imm {
	case PrivECALL:
		// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:1281-1285
		// Check that rd, rs1, funct3 are all zero
		if insn&0x000fff80 != 0 {
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			return c.handleException()
		}
		cause := CauseUserEcall + int(c.Priv)
		c.SetPendingException(cause, 0)
		return c.handleException()

	case PrivEBREAK:
		// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:1286-1290
		// Check that rd, rs1, funct3 are all zero
		if insn&0x000fff80 != 0 {
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			return c.handleException()
		}
		c.SetPendingException(CauseBreakpoint, c.PC)
		return c.handleException()

	case PrivSRET:
		// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:1291-1301
		// Check that rd, rs1, funct3 are all zero
		if insn&0x000fff80 != 0 {
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			return c.handleException()
		}
		if c.Priv < PrivSupervisor {
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			return c.handleException()
		}
		return c.executeSRET()

	case PrivMRET:
		// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:1302-1312
		// Check that rd, rs1, funct3 are all zero
		if insn&0x000fff80 != 0 {
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			return c.handleException()
		}
		if c.Priv < PrivMachine {
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			return c.handleException()
		}
		return c.executeMRET()

	case PrivWFI:
		// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:1313-1325
		// Check that rd and funct3 are zero (rs1 can be non-zero)
		if insn&0x00007f80 != 0 {
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			return c.handleException()
		}
		if c.Priv == PrivUser {
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			return c.handleException()
		}
		// Wait for interrupt
		if c.Mip&c.Mie == 0 {
			c.PowerDownFlag = true
		}
		c.PC += 4
		return nil

	default:
		// Check for SFENCE.VMA
		if (imm >> 5) == 0x09 {
			// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:1327-1343
			// Check that rd and funct3 are zero
			if insn&0x00007f80 != 0 {
				c.SetPendingException(CauseIllegalInsn, uint64(insn))
				return c.handleException()
			}
			if c.Priv == PrivUser {
				c.SetPendingException(CauseIllegalInsn, uint64(insn))
				return c.handleException()
			}
			rs1 := int(ExtractRs1(insn))
			if rs1 == 0 {
				c.FlushTLB()
			} else {
				c.FlushTLBEntry(c.GetReg(rs1))
			}
			c.PC += 4
			return nil
		}

		c.SetPendingException(CauseIllegalInsn, uint64(insn))
		return c.handleException()
	}
}

// executeSRET executes SRET instruction
// Per TinyEMU/RISC-V: sets the IE bit at position spp (the target privilege) to spie
// Reference: tinyemu-2019-12-21/riscv_cpu.c:1127-1141 (handle_sret)
func (c *CPU) executeSRET() error {
	oldPriv := c.Priv
	spp := (c.Mstatus >> MstatusSPPShift) & 1
	spie := (c.Mstatus >> MstatusSPIEShift) & 1

	// Set the IE bit at position spp to spie
	// Clear the bit first, then set it if spie is 1
	c.Mstatus = (c.Mstatus &^ (1 << spp)) | (spie << spp)

	// Set SPIE to 1
	c.Mstatus |= MstatusSPIE

	// Set SPP to U (0)
	c.Mstatus &^= MstatusSPP

	// Use SetPriv to handle TLB flush and CurXLEN update
	// Reference: riscv_cpu.c:1139 - set_priv(s, spp)
	c.SetPriv(uint8(spp))
	c.PC = c.Sepc

	// Trace privilege change
	if c.Tracer != nil && oldPriv != c.Priv {
		c.Tracer.TracePrivChange(c, oldPriv, c.Priv)
	}

	return nil
}

// executeMRET executes MRET instruction
// Per TinyEMU/RISC-V: sets the IE bit at position mpp (the target privilege) to mpie
// Reference: tinyemu-2019-12-21/riscv_cpu.c:1143-1157 (handle_mret)
func (c *CPU) executeMRET() error {
	oldPriv := c.Priv
	mpp := (c.Mstatus >> MstatusMPPShift) & 3
	mpie := (c.Mstatus >> MstatusMPIEShift) & 1

	// Set the IE bit at position mpp to mpie
	// Clear the bit first, then set it if mpie is 1
	c.Mstatus = (c.Mstatus &^ (1 << mpp)) | (mpie << mpp)

	// Set MPIE to 1
	c.Mstatus |= MstatusMPIE

	// Set MPP to U (0)
	c.Mstatus &^= MstatusMPP

	// Use SetPriv to handle TLB flush and CurXLEN update
	// Reference: riscv_cpu.c:1155 - set_priv(s, mpp)
	c.SetPriv(uint8(mpp))
	c.PC = c.Mepc

	// Trace privilege change
	if c.Tracer != nil && oldPriv != c.Priv {
		c.Tracer.TracePrivChange(c, oldPriv, c.Priv)
	}

	return nil
}

// executeAMO executes atomic memory operations
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:1376-1476
func (c *CPU) executeAMO(insn uint32, funct3 uint32, rd, rs1, rs2 int) error {
	funct5 := ExtractFunct5(insn)
	addr := c.GetReg(rs1)

	switch funct3 {
	case 2: // 32-bit AMO
		return c.executeAMO32(insn, funct5, rd, addr, rs2)
	case 3: // 64-bit AMO
		// 64-bit AMO is RV64/RV128 only
		// Reference: tinyemu-2019-12-21/riscv_cpu_template.h (XLEN >= 64)
		if c.CurXLEN == XLEN32 {
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			return c.handleException()
		}
		return c.executeAMO64(insn, funct5, rd, addr, rs2)
	default:
		c.SetPendingException(CauseIllegalInsn, uint64(insn))
		return c.handleException()
	}
}

// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:1376-1476 (OP_A macro)
func (c *CPU) executeAMO32(insn uint32, funct5 uint32, rd int, addr uint64, rs2 int) error {
	switch funct5 {
	case Funct5LR:
		// LR.W - Reference: riscv_cpu_template.h:1385-1392
		// C code checks: if (rs2 != 0) goto illegal_insn;
		if rs2 != 0 {
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			return c.handleException()
		}
		val, err := c.LoadU32(addr)
		if err != nil {
			return c.handleException()
		}
		c.SetReg(rd, uint64(int64(int32(val))))
		c.SetLoadReservation(addr)

	case Funct5SC:
		// SC.W - Reference: riscv_cpu_template.h:1393-1401
		// Note: C TinyEMU doesn't explicitly clear load_res after SC, but RISC-V
		// spec says SC clears the reservation. We clear it for spec compliance.
		if c.CheckLoadReservation(addr) {
			if err := c.StoreU32(addr, uint32(c.GetReg(rs2))); err != nil {
				return c.handleException()
			}
			c.SetReg(rd, 0) // Success
		} else {
			c.SetReg(rd, 1) // Failure
		}
		c.ClearLoadReservation()

	default:
		// Other AMO operations
		val, err := c.LoadU32(addr)
		if err != nil {
			return c.handleException()
		}
		oldVal := int32(val)
		rs2Val := int32(c.GetReg(rs2))

		var newVal int32
		switch funct5 {
		case Funct5AMOSWAP:
			newVal = rs2Val
		case Funct5AMOADD:
			newVal = oldVal + rs2Val
		case Funct5AMOXOR:
			newVal = oldVal ^ rs2Val
		case Funct5AMOAND:
			newVal = oldVal & rs2Val
		case Funct5AMOOR:
			newVal = oldVal | rs2Val
		case Funct5AMOMIN:
			if oldVal < rs2Val {
				newVal = oldVal
			} else {
				newVal = rs2Val
			}
		case Funct5AMOMAX:
			if oldVal > rs2Val {
				newVal = oldVal
			} else {
				newVal = rs2Val
			}
		case Funct5AMOMINU:
			if uint32(oldVal) < uint32(rs2Val) {
				newVal = oldVal
			} else {
				newVal = rs2Val
			}
		case Funct5AMOMAXU:
			if uint32(oldVal) > uint32(rs2Val) {
				newVal = oldVal
			} else {
				newVal = rs2Val
			}
		default:
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			return c.handleException()
		}

		if err := c.StoreU32(addr, uint32(newVal)); err != nil {
			return c.handleException()
		}
		c.SetReg(rd, uint64(int64(oldVal)))
	}

	c.PC += 4
	return nil
}

func (c *CPU) executeAMO64(insn uint32, funct5 uint32, rd int, addr uint64, rs2 int) error {
	switch funct5 {
	case Funct5LR:
		// LR.D - Reference: riscv_cpu_template.h:1385-1392
		// C code checks: if (rs2 != 0) goto illegal_insn;
		if rs2 != 0 {
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			return c.handleException()
		}
		val, err := c.LoadU64(addr)
		if err != nil {
			return c.handleException()
		}
		c.SetReg(rd, val)
		c.SetLoadReservation(addr)

	case Funct5SC:
		// SC.D - Reference: riscv_cpu_template.h:1393-1401
		// Note: C TinyEMU doesn't explicitly clear load_res after SC, but RISC-V
		// spec says SC clears the reservation. We clear it for spec compliance.
		if c.CheckLoadReservation(addr) {
			if err := c.StoreU64(addr, c.GetReg(rs2)); err != nil {
				return c.handleException()
			}
			c.SetReg(rd, 0) // Success
		} else {
			c.SetReg(rd, 1) // Failure
		}
		c.ClearLoadReservation()

	default:
		// Other AMO operations
		val, err := c.LoadU64(addr)
		if err != nil {
			return c.handleException()
		}
		oldVal := int64(val)
		rs2Val := int64(c.GetReg(rs2))

		var newVal int64
		switch funct5 {
		case Funct5AMOSWAP:
			newVal = rs2Val
		case Funct5AMOADD:
			newVal = oldVal + rs2Val
		case Funct5AMOXOR:
			newVal = oldVal ^ rs2Val
		case Funct5AMOAND:
			newVal = oldVal & rs2Val
		case Funct5AMOOR:
			newVal = oldVal | rs2Val
		case Funct5AMOMIN:
			if oldVal < rs2Val {
				newVal = oldVal
			} else {
				newVal = rs2Val
			}
		case Funct5AMOMAX:
			if oldVal > rs2Val {
				newVal = oldVal
			} else {
				newVal = rs2Val
			}
		case Funct5AMOMINU:
			if uint64(oldVal) < uint64(rs2Val) {
				newVal = oldVal
			} else {
				newVal = rs2Val
			}
		case Funct5AMOMAXU:
			if uint64(oldVal) > uint64(rs2Val) {
				newVal = oldVal
			} else {
				newVal = rs2Val
			}
		default:
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			return c.handleException()
		}

		if err := c.StoreU64(addr, uint64(newVal)); err != nil {
			return c.handleException()
		}
		c.SetReg(rd, uint64(oldVal))
	}

	c.PC += 4
	return nil
}
