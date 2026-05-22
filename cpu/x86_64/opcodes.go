package x86_64

import (
	"fmt"
	"math/bits"
	"os"
)

// cr3Trace is enabled by TINYEMU_X64_CR3=1 and logs every write to CR3
// with the resulting PML4[0]/[273]/[511] (identity/direct-map/kernel-text).
// Diagnostic for "missing PML4[273] at PF" investigations.
var cr3Trace = os.Getenv("TINYEMU_X64_CR3") == "1"

// intrTrace is enabled by TINYEMU_X64_INTR=1 and logs LIDT loads and
// every deliverInterrupt() call (vector, IDTR state, gate bytes, new
// CS:RIP, error). Diagnostic for "early page-fault handler never ran"
// boot bugs.
var intrTrace = os.Getenv("TINYEMU_X64_INTR") == "1"

// msrTrace logs every RDMSR / WRMSR with RIP. Diagnostic for "kernel
// silently skipped a feature because our MSR returned 0" issues.
var msrTrace = os.Getenv("TINYEMU_X64_MSR") == "1"

// ioTrace logs every IO port write (OUT). Diagnostic for "where is
// the kernel's first printk going?" and similar IO investigations.
var ioTrace = os.Getenv("TINYEMU_X64_IO") == "1"

// userSyscallTrace logs every user-mode SYSCALL with its number and
// the SysV ABI argument registers. Diagnostic for "what is this user
// process blocked on?" investigations — particularly nlplug-findfs
// hangs.
var userSyscallTrace = os.Getenv("TINYEMU_X64_USYS") == "1"

// linuxSyscallName returns the symbolic name for a small set of
// commonly-blocked syscall numbers (just enough for diagnostics).
// Numbers from arch/x86/entry/syscalls/syscall_64.tbl.
func linuxSyscallName(n uint32) string {
	switch n {
	case 0:
		return "read"
	case 1:
		return "write"
	case 2:
		return "open"
	case 3:
		return "close"
	case 7:
		return "poll"
	case 8:
		return "lseek"
	case 9:
		return "mmap"
	case 10:
		return "mprotect"
	case 13:
		return "rt_sigaction"
	case 16:
		return "ioctl"
	case 21:
		return "access"
	case 22:
		return "pipe"
	case 23:
		return "select"
	case 35:
		return "nanosleep"
	case 41:
		return "socket"
	case 42:
		return "connect"
	case 43:
		return "accept"
	case 44:
		return "sendto"
	case 45:
		return "recvfrom"
	case 46:
		return "sendmsg"
	case 47:
		return "recvmsg"
	case 49:
		return "bind"
	case 50:
		return "listen"
	case 56:
		return "clone"
	case 57:
		return "fork"
	case 59:
		return "execve"
	case 60:
		return "exit"
	case 61:
		return "wait4"
	case 62:
		return "kill"
	case 80:
		return "chdir"
	case 89:
		return "readlink"
	case 96:
		return "gettimeofday"
	case 158:
		return "arch_prctl"
	case 165:
		return "mount"
	case 166:
		return "umount2"
	case 202:
		return "futex"
	case 217:
		return "getdents64"
	case 230:
		return "clock_nanosleep"
	case 231:
		return "exit_group"
	case 232:
		return "epoll_wait"
	case 233:
		return "epoll_ctl"
	case 257:
		return "openat"
	case 281:
		return "epoll_pwait"
	case 291:
		return "epoll_create1"
	}
	return "?"
}

// executeOpcode dispatches a decoded primary opcode after the prefix
// loop in Step has consumed the leading prefix bytes. operandSize is
// 2, 4, or 8 (bytes); addressSize is 4 or 8 in long mode.
//
// The surface covers the M1 vertical slice (MOV, LEA, ADD, SUB, PUSH/
// POP, CALL/RET, JMP, NOP, HLT) plus the M2 expansion: conditional
// jumps Jcc (rel8 and rel32 via 0x0F escape), the full Group 1 family
// (ADD/OR/ADC/SBB/AND/SUB/XOR/CMP), bitwise ops in their primary "rm
// vs r" forms (0x09 OR, 0x21 AND, 0x29 SUB, 0x31 XOR, 0x39 CMP), TEST
// r/m,r (0x85), and Group 5 INC/DEC (0xFF /0, /1).
func (c *CPU) executeOpcode(op, rex, operandSize, addressSize uint8, segOverride int, repPrefix uint8, has66 bool) error {
	_ = segOverride // segment-override handling lands with explicit memory operands beyond the initial slice
	_ = addressSize // 32-bit addressing not yet wired

	switch {
	// ===== Single-byte primary ops =====

	case op >= 0x90 && op <= 0x97:
		// 0x90+r = XCHG rAX, rN with REX.B extending the source to
		// R8..R15. The classic 0x90 (REX absent, or REX with B=0) is
		// XCHG RAX,RAX which is a NOP — also covers the F3 90 PAUSE
		// hint (F3 prefix is dropped by our dispatcher for non-string
		// ops). With REX.B=1 we swap RAX with R8..R15.
		dstReg := uint8(op-0x90) | (rex&rexB)<<3
		if dstReg == RAX {
			return nil // NOP (XCHG RAX, RAX)
		}
		tmp := c.GetReg64(int(RAX))
		c.writeReg(RAX, c.GetReg64(int(dstReg)), operandSize)
		c.writeReg(dstReg, tmp, operandSize)
		return nil

	case op == 0xF4:
		if intrTrace {
			fmt.Fprintf(os.Stderr, "[hlt] RIP=%#x RFLAGS=%#x IF=%v intrLine=%d\n",
				c.rip, c.rflags, c.rflags&RFLAGS_IF != 0, c.intrLineState)
		}
		c.powerDown = true
		return nil

	// ===== ALU (each op has 6 encodings) =====
	// Layout per op (using ADD/0x_0..5 as reference): _0=Eb,Gb;
	// _1=Ev,Gv; _2=Gb,Eb; _3=Gv,Ev; _4=AL,imm8; _5=rAX,imm.
	// Same pattern for OR (0x08-0x0D), AND (0x20-0x25), SUB (0x28-
	// 0x2D), XOR (0x30-0x35), CMP (0x38-0x3D). TEST is its own
	// shorter sub-family (0x84/0x85, 0xA8/0xA9).

	case op == 0x00:
		return c.opALURM(rex, 1, aluADD)
	case op == 0x01:
		return c.opALURM(rex, operandSize, aluADD)
	case op == 0x02:
		return c.opALURfromM(rex, 1, aluADD)
	case op == 0x03:
		return c.opALURfromM(rex, operandSize, aluADD)
	case op == 0x04:
		return c.opALUImmAL(aluADD)

	case op == 0x10:
		return c.opALURM(rex, 1, aluADC)
	case op == 0x11:
		return c.opALURM(rex, operandSize, aluADC)
	case op == 0x12:
		return c.opALURfromM(rex, 1, aluADC)
	case op == 0x13:
		return c.opALURfromM(rex, operandSize, aluADC)
	case op == 0x14:
		return c.opALUImmAL(aluADC)
	case op == 0x15:
		return c.opALUImmRAX(operandSize, aluADC)

	case op == 0x18:
		return c.opALURM(rex, 1, aluSBB)
	case op == 0x19:
		return c.opALURM(rex, operandSize, aluSBB)
	case op == 0x1A:
		return c.opALURfromM(rex, 1, aluSBB)
	case op == 0x1B:
		return c.opALURfromM(rex, operandSize, aluSBB)
	case op == 0x1C:
		return c.opALUImmAL(aluSBB)
	case op == 0x1D:
		return c.opALUImmRAX(operandSize, aluSBB)

	case op == 0x08:
		return c.opALURM(rex, 1, aluOR)
	case op == 0x09:
		return c.opALURM(rex, operandSize, aluOR)
	case op == 0x0A:
		return c.opALURfromM(rex, 1, aluOR)
	case op == 0x0B:
		return c.opALURfromM(rex, operandSize, aluOR)
	case op == 0x0C:
		return c.opALUImmAL(aluOR)

	case op == 0x20:
		return c.opALURM(rex, 1, aluAND)
	case op == 0x21:
		return c.opALURM(rex, operandSize, aluAND)
	case op == 0x22:
		return c.opALURfromM(rex, 1, aluAND)
	case op == 0x23:
		return c.opALURfromM(rex, operandSize, aluAND)
	case op == 0x24:
		return c.opALUImmAL(aluAND)

	case op == 0x28:
		return c.opALURM(rex, 1, aluSUB)
	case op == 0x29:
		return c.opALURM(rex, operandSize, aluSUB)
	case op == 0x2A:
		return c.opALURfromM(rex, 1, aluSUB)
	case op == 0x2B:
		return c.opALURfromM(rex, operandSize, aluSUB)
	case op == 0x2C:
		return c.opALUImmAL(aluSUB)

	case op == 0x30:
		return c.opALURM(rex, 1, aluXOR)
	case op == 0x31:
		return c.opALURM(rex, operandSize, aluXOR)
	case op == 0x32:
		return c.opALURfromM(rex, 1, aluXOR)
	case op == 0x33:
		return c.opALURfromM(rex, operandSize, aluXOR)
	case op == 0x34:
		return c.opALUImmAL(aluXOR)

	case op == 0x38:
		return c.opALURM(rex, 1, aluCMP)
	case op == 0x39:
		return c.opALURM(rex, operandSize, aluCMP)
	case op == 0x3A:
		return c.opALURfromM(rex, 1, aluCMP)
	case op == 0x3B:
		return c.opALURfromM(rex, operandSize, aluCMP)
	case op == 0x3C:
		return c.opALUImmAL(aluCMP)

	case op == 0x84: // TEST r/m8, r8
		return c.opTEST(rex, 1)
	case op == 0x85:
		return c.opTEST(rex, operandSize)
	case op == 0xA8: // TEST AL, imm8
		return c.opALUImmAL(aluTEST)
	case op == 0xA9:
		return c.opALUImmRAX(operandSize, aluTEST)

	// ALU rAX, imm forms (single-byte primary opcode + imm). The imm is
	// imm16 in operandSize=2 mode and imm32 sign-extended-to-64
	// otherwise. AL,imm8 byte forms (0x04/0x0C/...) are not implemented
	// — none of M2's tests need them.
	case op == 0x05:
		return c.opALUImmRAX(operandSize, aluADD)
	case op == 0x0D:
		return c.opALUImmRAX(operandSize, aluOR)
	case op == 0x25:
		return c.opALUImmRAX(operandSize, aluAND)
	case op == 0x2D:
		return c.opALUImmRAX(operandSize, aluSUB)
	case op == 0x35:
		return c.opALUImmRAX(operandSize, aluXOR)
	case op == 0x3D:
		return c.opALUImmRAX(operandSize, aluCMP)

	// ===== MOV family =====

	case op == 0x86:
		// XCHG r/m8, r8 — atomic swap (always atomic on real
		// hardware; in our single-CPU model it's just a swap).
		return c.opXCHGRM(rex, 1)
	case op == 0x87:
		return c.opXCHGRM(rex, operandSize)

	case op == 0x8F:
		// POP r/m. ModR/M.reg must be 0; the rest are reserved.
		m := c.parseModRM64(rex)
		if m.reg != 0 {
			return unimplemented("0x8F /%d (reserved)", m.reg)
		}
		v := c.pop64()
		c.writeOperand(m, v, 8) // long mode: always 64-bit stack ops
		return nil

	case op == 0xC2:
		// RET imm16 — pops return then pops imm16 bytes off stack.
		imm := uint64(c.fetch16())
		c.rip = c.pop64()
		c.SetReg64(RSP, c.GetReg64(RSP)+imm)
		return nil

	case op == 0xE0, op == 0xE1, op == 0xE2:
		// LOOPNE / LOOPE / LOOP — decrement RCX, branch if !=0
		// (with ZF condition for LOOPNE/LOOPE).
		disp := int64(int8(c.fetch8()))
		rcx := c.GetReg64(RCX) - 1
		c.SetReg64(RCX, rcx)
		take := rcx != 0
		switch op {
		case 0xE0:
			take = take && c.rflags&RFLAGS_ZF == 0
		case 0xE1:
			take = take && c.rflags&RFLAGS_ZF != 0
		}
		if take {
			c.rip = uint64(int64(c.rip) + disp)
		}
		return nil
	case op == 0xE3:
		// JCXZ / JRCXZ — branch if RCX is zero (no decrement).
		disp := int64(int8(c.fetch8()))
		if c.GetReg64(RCX) == 0 {
			c.rip = uint64(int64(c.rip) + disp)
		}
		return nil

	case op == 0x88:
		// MOV r/m8, r8 — byte form. Source is the 8-bit register
		// picked by ModR/M.reg with REX-aware decoding (read8FromModRM-
		// style indexing).
		return c.opMOVRM8(rex)
	case op == 0x8A:
		// MOV r8, r/m8.
		return c.opMOVRfromM8(rex)

	case op == 0x89:
		return c.opMOVRM(rex, operandSize)
	case op == 0x8B:
		return c.opMOVRfromM(rex, operandSize)

	case op >= 0xB0 && op <= 0xB7:
		// MOV r8, imm8 — REX-aware: the destination encoding follows
		// the same AH/CH/DH/BH vs SPL/BPL/SIL/DIL rule as MODR/M for
		// the low 3 bits.
		return c.opMOVImm8ToReg(op-0xB0, rex)
	case op == 0x8C:
		// MOV r/m16, Sreg — store a segment-register selector. ModR/M
		// reg field picks the segment (0=ES, 1=CS, 2=SS, 3=DS, 4=FS,
		// 5=GS).
		return c.opMOVfromSreg(rex)

	case op == 0x8D:
		return c.opLEA(rex, operandSize)

	case op == 0x8E:
		// MOV Sreg, r/m16 — load a segment-register selector. The
		// source is always 16 bits regardless of operand-size prefix.
		return c.opMOVtoSreg(rex)

	case op == 0xB8, op == 0xB9, op == 0xBA, op == 0xBB,
		op == 0xBC, op == 0xBD, op == 0xBE, op == 0xBF:
		return c.opMOVImmToReg(op-0xB8, rex, operandSize)

	case op == 0xC6:
		return c.opMOVImm(rex, 1)
	case op == 0xC7:
		return c.opMOVImm(rex, operandSize)

	// ===== Group 1 (immediate) =====

	case op == 0x80:
		// Group 1 r/m8, imm8.
		return c.opGroup1(rex, 1, true)
	case op == 0x81:
		// Group 1 r/m, imm16/imm32 (sign-extended to 64).
		return c.opGroup1(rex, operandSize, false)
	case op == 0x82:
		// Same encoding as 0x83 in legacy mode; #UD in long mode per
		// Intel SDM. Surfacing explicitly.
		return unimplemented("0x82 (illegal in long mode)")
	case op == 0x83:
		// Group 1 r/m, imm8 (sign-extended).
		return c.opGroup1(rex, operandSize, true)

	// ===== Stack =====

	case op >= 0x50 && op <= 0x57:
		return c.opPUSHReg(op-0x50, rex)
	case op >= 0x58 && op <= 0x5F:
		return c.opPOPReg(op-0x58, rex)

	case op == 0x68:
		// PUSH imm32 (or imm16 with 0x66) sign-extended to operand
		// size. In long mode the pushed value occupies 8 bytes on
		// the stack regardless.
		var v int64
		if operandSize == 2 {
			v = int64(int16(c.fetch16()))
		} else {
			v = int64(int32(c.fetch32()))
		}
		c.push64(uint64(v))
		return nil

	case op == 0x6A:
		// PUSH imm8 sign-extended.
		v := int64(int8(c.fetch8()))
		c.push64(uint64(v))
		return nil

	// ===== Control flow =====

	case op == 0xC3:
		c.rip = c.pop64()
		return nil

	case op == 0xCB:
		// RETF / LRETQ — far return. Pops RIP and CS at operand-size
		// width. In long mode REX.W=1 picks 8-byte slots, else 4. CS
		// is loaded by reading its descriptor from the GDT, but we
		// keep the M5c shortcut: synthesize a 64-bit code segment for
		// the new CS. The kernel's startup_64 uses this to far-return
		// into its newly-loaded GDT's kernel CS, where L=1.
		return c.opRETF(operandSize)

	case op == 0xCC:
		// INT3 — software breakpoint, vector 3, no error code.
		return c.deliverInterrupt(3, false, 0)

	case op == 0xCD:
		// INT imm8.
		vec := c.fetch8()
		return c.deliverInterrupt(vec, false, 0)

	case op == 0xCF:
		// IRET. In long mode the 64-bit form requires REX.W=1; the
		// 32-bit IRETD form (REX.W=0) is decoded but pops 32-bit values
		// — not yet implemented because nothing in our test surface
		// exercises it. Treat REX.W=0 as IRETQ for simplicity.
		return c.opIRETQ()

	case op == 0xE4: // IN AL, imm8
		port := uint16(c.fetch8())
		c.SetReg8(AL, c.ioRead8(port))
		return nil
	case op == 0xE5: // IN eAX, imm8 — width follows operandSize
		port := uint16(c.fetch8())
		return c.opINEAX(port, operandSize)
	case op == 0xE6: // OUT imm8, AL
		port := uint16(c.fetch8())
		c.ioWrite8(port, c.GetReg8(AL))
		return nil
	case op == 0xE7: // OUT imm8, eAX
		port := uint16(c.fetch8())
		return c.opOUTEAX(port, operandSize)
	case op == 0xEC: // IN AL, DX
		port := c.GetReg16(DX)
		c.SetReg8(AL, c.ioRead8(port))
		return nil
	case op == 0xED: // IN eAX, DX
		port := c.GetReg16(DX)
		return c.opINEAX(port, operandSize)
	case op == 0xEE: // OUT DX, AL
		port := c.GetReg16(DX)
		c.ioWrite8(port, c.GetReg8(AL))
		return nil
	case op == 0xEF: // OUT DX, eAX
		port := c.GetReg16(DX)
		return c.opOUTEAX(port, operandSize)

	case op == 0xE8:
		disp := int64(int32(c.fetch32()))
		c.push64(c.rip)
		c.rip = uint64(int64(c.rip) + disp)
		return nil

	case op == 0xE9:
		disp := int64(int32(c.fetch32()))
		c.rip = uint64(int64(c.rip) + disp)
		return nil

	case op == 0xEB:
		disp := int64(int8(c.fetch8()))
		c.rip = uint64(int64(c.rip) + disp)
		return nil

	case op == 0xEA:
		// JMP FAR PTR16:32 / FAR PTR16:16 — direct intersegment jump.
		// Invalid in long mode (the opcode was reassigned); valid in
		// real, 16-bit protected, and 32-bit protected mode. SeaBIOS
		// uses this for its first jump out of the reset vector (real
		// mode); PVH-style i386→x86_64 bootstraps use it to reload CS
		// after their own GDT goes live.
		if c.efer&EFER_LMA != 0 {
			return unimplemented("0xEA JMP FAR is invalid in long mode")
		}
		var off uint64
		if operandSize == 2 {
			off = uint64(c.fetch16())
		} else {
			off = uint64(c.fetch32())
		}
		sel := c.fetch16()
		// Real mode: CS.base is just sel<<4, no descriptor table is
		// involved (and any GDTR base is meaningless). All the other
		// segment-cache fields (limit, access, flags) stay the same —
		// real mode permanently runs as 16-bit code with a 64 KB limit.
		if c.cr[0]&CR0_PE == 0 {
			c.seg[CS] = sel
			c.segBase[CS] = uint64(sel) << 4
			c.segLimit[CS] = 0xFFFF
			// access stays at its real-mode default; recomputeMode will
			// pick ModeReal16 because PE=0.
			c.recomputeMode()
			c.rip = off
			return nil
		}
		// Protected mode: read the target descriptor. Selector bits 2..15
		// are the index; bits 0..1 are RPL/TI (we ignore TI — assume GDT).
		// With paging off (PVH / SeaBIOS pre-mode-switch) GDTR.base is a
		// physical address.
		gdtBase := c.segBase[GDTR]
		descAddr := gdtBase + uint64(sel&0xFFF8)
		desc, err := c.memMap.Read64(descAddr)
		if err != nil {
			return fmt.Errorf("0xEA: read GDT entry %#x: %w", sel, err)
		}
		// Descriptor layout (Intel SDM Vol 3 §3.4.5):
		//   bits  0..15  limit[15:0]
		//   bits 16..39  base[23:0]
		//   bits 40..47  access byte (P, DPL, S, Type)
		//   bits 48..51  limit[19:16]
		//   bits 52..55  flags nibble (AVL, L, D/B, G)
		//   bits 56..63  base[31:24]
		base := uint64(uint32((desc>>16)&0xFFFFFF)) | ((desc >> 56) << 24)
		access := uint8((desc >> 40) & 0xFF)
		flags := uint8((desc >> 52) & 0x0F)
		// segAccess layout: low byte = access; bits 8..11 = flags nibble.
		// That matches the encoding mode.go's csLBit / csDBit read.
		segAccess := uint32(access) | (uint32(flags) << 8)
		c.seg[CS] = sel
		c.segBase[CS] = base
		c.segLimit[CS] = 0xFFFFFFFF // limit isn't checked on most code paths
		c.SetSegAccess(CS, segAccess)
		c.rip = off
		return nil

	case op >= 0x70 && op <= 0x7F:
		// Conditional jump rel8.
		disp := int64(int8(c.fetch8()))
		if c.evalCond(op & 0xF) {
			c.rip = uint64(int64(c.rip) + disp)
		}
		return nil

	// ===== Group 3 (TEST/NOT/NEG/MUL/IMUL/DIV/IDIV) =====

	case op == 0xF6:
		// Group 3 byte form — TEST r/m8,imm8 / NOT / NEG / MUL / IMUL
		// / DIV / IDIV at 8-bit operand width.
		return c.opGroup3(rex, 1)
	case op == 0xF7:
		return c.opGroup3(rex, operandSize)

	// ===== IMUL signed-integer forms =====

	case op == 0x69:
		// IMUL r, r/m, imm32 (sign-extended to operand size).
		return c.opIMULImm(rex, operandSize, false)
	case op == 0x6B:
		// IMUL r, r/m, imm8 (sign-extended).
		return c.opIMULImm(rex, operandSize, true)

	// ===== Group 2 (shifts and rotates) =====

	case op == 0xD1:
		// SHL/SHR/SAR/ROL/ROR/RCL/RCR r/m, 1 — count is implicit.
		return c.opGroup2Reg(rex, operandSize, 1)
	case op == 0xD3:
		// Group 2 r/m, CL — count comes from CL register.
		return c.opGroup2Reg(rex, operandSize, uint64(c.GetReg8(CL)))
	case op == 0xC1:
		// Group 2 r/m, imm8 — count is an 8-bit immediate.
		return c.opGroup2Imm(rex, operandSize)
	case op == 0xD0:
		// Group 2 r/m8, 1 — byte form, count is implicit.
		return c.opGroup2Reg(rex, 1, 1)
	case op == 0xD2:
		// Group 2 r/m8, CL — byte form, count comes from CL.
		return c.opGroup2Reg(rex, 1, uint64(c.GetReg8(CL)))
	case op == 0xC0:
		// Group 2 r/m8, imm8 — byte form, count is 8-bit immediate.
		return c.opGroup2Imm(rex, 1)

	// ===== Sign-extending integer move (MOVSXD r64, r/m32) =====

	case op == 0x63:
		// In long mode 0x63 is MOVSXD (in 32-bit mode it was ARPL).
		// Destination is the reg field, full 64 bits; source is a
		// 32-bit r/m that gets sign-extended.
		return c.opMOVSXD(rex)

	// ===== Group 4/5 (Inc/Dec [byte / full] + Call/Jmp/Push) =====

	case op == 0xFE:
		return c.opGroup4(rex)
	case op == 0xFF:
		return c.opGroup5(rex, operandSize)

	// ===== Group 15 (0x0F 0xAE) — FXSAVE/FXRSTOR/LDMXCSR/STMXCSR + LFENCE/MFENCE/SFENCE/CLFLUSH =====
	//
	// Stub: implemented as memory shuffles. Linux only uses these to
	// save/restore the FPU on context switch — it never inspects the
	// contents beyond what it itself wrote/read. So the kernel's view
	// of "the FPU state" is a 512-byte opaque blob the emulator
	// faithfully round-trips, which is all the boot needs. The
	// in-CPU FPU register file stays uninitialised because no kernel
	// path reads it.
	//
	// (Encoded as the two-byte 0x0F 0xAE form, dispatched in opTwoByte
	// below.)

	// FWAIT / WAIT — synchronise with the FPU. We don't pipeline FPU
	// ops so there's nothing to wait for; effectively a NOP.
	case op == 0x9B:
		return nil

	// ===== x87 FPU escape opcodes (0xD8..0xDF) =====
	//
	// The Linux 6.6 boot reaches fpu__init_system which issues FNINIT
	// (db e3) to clear FPU state. We don't actually emulate the x87
	// stack here — the kernel only uses it for context-switch
	// FXSAVE/FXRSTOR shuffles, which we treat as opaque RAM stores
	// (the kernel writes its own pattern, we read it back unchanged).
	// Minimal stub: identify the no-operand control insns and silently
	// succeed; for memory-operand forms consume the ModR/M so the
	// instruction stream stays aligned.
	case op >= 0xD8 && op <= 0xDF:
		return c.handleX87(op, rex)

	// ===== Flag manipulation =====

	case op == 0xC9: // LEAVE — restore RBP, pop saved RBP
		// LEAVE := mov rsp, rbp ; pop rbp
		c.SetReg64(RSP, c.GetReg64(RBP))
		c.SetReg64(RBP, c.pop64())
		return nil

	case op == 0x98: // CBW / CWDE / CDQE — sign-extend AL/AX/EAX in place.
		switch operandSize {
		case 2: // CBW: AX = sign-extend(AL)
			c.SetReg16(AX, uint16(int8(c.GetReg8(AL))))
		case 4: // CWDE: EAX = sign-extend(AX)
			c.SetReg32(EAX, uint32(int16(c.GetReg16(AX))))
		case 8: // CDQE: RAX = sign-extend(EAX)
			c.SetReg64(RAX, uint64(int32(c.GetReg32(EAX))))
		}
		return nil

	case op == 0x99: // CWD / CDQ / CQO — sign-extend RAX into RDX.
		a := c.readReg(RAX, operandSize)
		var hi uint64
		if a&signBit(operandSize) != 0 {
			hi = mask(operandSize)
		}
		c.writeReg(RDX, hi, operandSize)
		return nil

	case op == 0x9C: // PUSHFQ — push RFLAGS as 8 bytes (long-mode default)
		c.push64(c.rflags)
		return nil
	case op == 0x9D: // POPFQ — pop 8 bytes into RFLAGS
		v := c.pop64()
		// Filter reserved bits via ValidFlagMask; bit 1 always reads 1
		// per SDM. CPL=0 can change everything except VM/RF (RF gets
		// cleared on POPF semantics — we fold the same effect by
		// stripping RF from the popped value).
		c.rflags = (v & ValidFlagMask &^ RFLAGS_RF) | 2
		return nil

	case op == 0x9E: // SAHF — store AH into low byte of RFLAGS
		// AH bits 7,6,4,2,0 ↔ SF, ZF, AF, PF, CF. Bits 1,3,5 ignored.
		ah := c.GetReg8(AH)
		c.rflags &^= RFLAGS_SF | RFLAGS_ZF | RFLAGS_AF | RFLAGS_PF | RFLAGS_CF
		if ah&0x80 != 0 {
			c.rflags |= RFLAGS_SF
		}
		if ah&0x40 != 0 {
			c.rflags |= RFLAGS_ZF
		}
		if ah&0x10 != 0 {
			c.rflags |= RFLAGS_AF
		}
		if ah&0x04 != 0 {
			c.rflags |= RFLAGS_PF
		}
		if ah&0x01 != 0 {
			c.rflags |= RFLAGS_CF
		}
		return nil
	case op == 0x9F: // LAHF — load AH from low byte of RFLAGS
		var ah uint8 = 0x02 // bit 1 always reads 1
		if c.rflags&RFLAGS_SF != 0 {
			ah |= 0x80
		}
		if c.rflags&RFLAGS_ZF != 0 {
			ah |= 0x40
		}
		if c.rflags&RFLAGS_AF != 0 {
			ah |= 0x10
		}
		if c.rflags&RFLAGS_PF != 0 {
			ah |= 0x04
		}
		if c.rflags&RFLAGS_CF != 0 {
			ah |= 0x01
		}
		c.SetReg8(AH, ah)
		return nil

	case op == 0xF5: // CMC — complement CF
		c.rflags ^= RFLAGS_CF
		return nil
	case op == 0xF8: // CLC — clear CF
		c.rflags &^= RFLAGS_CF
		return nil
	case op == 0xF9: // STC — set CF
		c.rflags |= RFLAGS_CF
		return nil
	case op == 0xFA: // CLI — clear IF
		c.rflags &^= RFLAGS_IF
		return nil
	case op == 0xFB: // STI — set IF
		c.rflags |= RFLAGS_IF
		return nil
	case op == 0xFC: // CLD — clear DF
		c.rflags &^= RFLAGS_DF
		return nil
	case op == 0xFD: // STD — set DF
		c.rflags |= RFLAGS_DF
		return nil

	// ===== String operations =====

	case op == 0xA4:
		return c.opStringMOVS(rex, 1, repPrefix)
	case op == 0xA5:
		return c.opStringMOVS(rex, operandSize, repPrefix)
	case op == 0xAA:
		return c.opStringSTOS(rex, 1, repPrefix)
	case op == 0xAB:
		return c.opStringSTOS(rex, operandSize, repPrefix)
	case op == 0xAC:
		return c.opStringLODS(rex, 1, repPrefix)
	case op == 0xAD:
		return c.opStringLODS(rex, operandSize, repPrefix)
	case op == 0xAE:
		return c.opStringSCAS(rex, 1, repPrefix)
	case op == 0xAF:
		return c.opStringSCAS(rex, operandSize, repPrefix)

	// ===== Two-byte escape =====

	case op == 0x0F:
		return c.opTwoByte(rex, operandSize, segOverride, repPrefix, has66)
	}

	return c.unimplementedAt("opcode %#02x rex=%#x", op, rex)
}

// opTwoByte dispatches the 0x0F escape opcode family.
//
// repPrefix carries the F2 (=2) / F3 (=1) repeat prefix the legacy
// decoder collected; SSE2 reinterprets these as opcode-extension bits
// (e.g. F3 0F 6F = MOVDQU, F2 0F 10 = MOVSD). Passed through to
// opSSE2 below.
func (c *CPU) opTwoByte(rex, operandSize uint8, segOverride int, repPrefix uint8, has66 bool) error {
	_ = segOverride
	op2 := c.fetch8()
	switch {
	case op2 == 0x00:
		// Group 6 — SLDT/STR/LLDT/LTR/VERR/VERW. Only LTR and LLDT
		// are wired for now (and just stash the selector).
		return c.opGroup6(rex)

	case op2 == 0x01:
		// Group 7 — SGDT/SIDT/LGDT/LIDT/SMSW/LMSW/INVLPG.
		return c.opGroup7(rex)

	case op2 == 0x0B:
		// UD2 — guaranteed-invalid-opcode instruction. Routes through
		// vector 6 (#UD).
		return c.deliverInterrupt(6, false, 0)

	case op2 == 0x1F:
		// Multi-byte NOP — the compiler emits this for alignment
		// padding around branch targets. ModR/M (+ SIB + disp) is
		// consumed for side-effect-free operand decoding; the result
		// is discarded.
		c.parseModRM64(rex)
		return nil

	case op2 >= 0x18 && op2 <= 0x1E:
		// 0F 18..1E are "prefetch hint" NOPs — PREFETCHNTA/T0/T1/T2
		// and reserved-NOP encodings the compiler emits for code
		// padding. All take a ModR/M operand and do nothing
		// observable in our model.
		c.parseModRM64(rex)
		return nil

	case op2 == 0x09:
		// WBINVD — invalidate cache. We don't model caches, so the
		// instruction is observably a no-op (kernel uses it for
		// boot-time cache flushing).
		return nil

	case op2 == 0x31:
		// RDTSC — read time-stamp counter into EDX:EAX. Our cycle
		// counter is monotonic; bind RDTSC to it.
		v := c.cycles
		c.SetReg32(EAX, uint32(v))
		c.SetReg32(EDX, uint32(v>>32))
		return nil


	case op2 == 0x05:
		// SYSCALL — fast kernel entry. EFER.SCE must be set; we
		// honor that lazily by always allowing the entry.
		return c.opSYSCALL()

	case op2 == 0xA2:
		// CPUID. Inputs in EAX (+ ECX for some leaves); outputs in
		// EAX/EBX/ECX/EDX. Advertises the minimum feature set Linux
		// x86_64 requires (LM, SSE/SSE2, FPU, MSR, PAE, CMOV, etc.).
		return c.opCPUID()
	case op2 == 0x07:
		// SYSRET — fast kernel exit. REX.W picks 64-bit-return (SYSRETQ);
		// REX.W=0 is the 32-bit compat-mode form (SYSRETL).
		return c.opSYSRET(rex)

	case op2 == 0x20:
		// MOV r64, CRn — reads control register into a GPR. The ModR/M
		// is the unusual "register-register" form where mod is treated
		// as 11 regardless of its actual value (the operand size is
		// always 64 bits in long mode).
		return c.opMovFromCR(rex)
	case op2 == 0x22:
		return c.opMovToCR(rex)
	case op2 == 0x21:
		return c.opMovFromDR(rex)
	case op2 == 0x23:
		return c.opMovToDR(rex)

	case op2 == 0x30:
		return c.opWRMSR()
	case op2 == 0x32:
		return c.opRDMSR()
	case op2 >= 0x40 && op2 <= 0x4F:
		// CMOVcc r, r/m — conditional move. Operand size follows the
		// usual rules (REX.W → 64, 0x66 → 16, else 32). Destination
		// reg always gets updated *with the source* if the condition
		// holds; otherwise no change to the destination — and crucially
		// no zero-extension of the upper bits in the fall-through case.
		m := c.parseModRM64(rex)
		src := c.readOperand(m, operandSize)
		if c.evalCond(op2 & 0xF) {
			c.writeReg(m.reg, src, operandSize)
		}
		return nil

	case op2 >= 0x80 && op2 <= 0x8F:
		// Jcc rel32 — even in 64-bit mode the displacement is 32 bits
		// sign-extended (operand-size override could shrink to 16 but
		// modern code never uses that).
		disp := int64(int32(c.fetch32()))
		if c.evalCond(op2 & 0xF) {
			c.rip = uint64(int64(c.rip) + disp)
		}
		return nil

	case op2 >= 0x90 && op2 <= 0x9F:
		// SETcc r/m8 — store 1 if condition holds, 0 otherwise. The
		// destination is always 8 bits regardless of operand-size prefix.
		m := c.parseModRM64(rex)
		var v uint8
		if c.evalCond(op2 & 0xF) {
			v = 1
		}
		if m.isReg {
			c.write8FromModRM(m, v)
		} else {
			c.writeMem8(c.segBaseForModRM(m) + m.ea, v)
		}
		return nil

	case op2 == 0xAF:
		// IMUL r, r/m — two-operand signed multiply, destination is reg.
		return c.opIMUL2Op(rex, operandSize)

	case op2 == 0xB0:
		// CMPXCHG r/m8, r8 — atomic compare-and-swap. Compares AL
		// against r/m: if equal, write r into r/m and set ZF; else
		// load r/m into AL and clear ZF.
		return c.opCMPXCHG(rex, 1)
	case op2 == 0xB1:
		return c.opCMPXCHG(rex, operandSize)

	case op2 == 0xC0:
		// XADD r/m8, r8 — exchange + add. Atomic on real hw.
		return c.opXADD(rex, 1)
	case op2 == 0xC1:
		return c.opXADD(rex, operandSize)

	case op2 == 0xAE:
		// Group 15 — FXSAVE/FXRSTOR/LDMXCSR/STMXCSR/XSAVE/LFENCE/
		// MFENCE/SFENCE etc.
		//
		// Peek at ModR/M.mod first so we can distinguish the
		// reg-form fences from the memory-form save/restore ops
		// without doubly-consuming bytes.
		// (Note: in long mode the "fence" mod-3 encodings are
		// /5 LFENCE, /6 MFENCE, /7 SFENCE. /0..3 are FXSAVE / FXRSTOR
		// / LDMXCSR / STMXCSR — memory only. /4..7 may be the XSAVE
		// family with mod != 3.)
		return c.opGroup15(rex)

	case op2 == 0xBA:
		// Group 8 — BT/BTS/BTR/BTC r/m, imm8. Sub-op (reg field):
		// 4=BT, 5=BTS, 6=BTR, 7=BTC.
		return c.opGroup8(rex, operandSize)

	case op2 == 0xA3:
		// BT r/m, r — read bit-index from reg into CF.
		return c.opBT(rex, operandSize, false, false)
	case op2 == 0xAB:
		// BTS r/m, r — set the bit, copy old to CF.
		return c.opBT(rex, operandSize, true, false)
	case op2 == 0xB3:
		// BTR r/m, r — reset the bit, copy old to CF.
		return c.opBT(rex, operandSize, false, true)
	case op2 == 0xBB:
		// BTC r/m, r — complement the bit, copy old to CF.
		return c.opBTC(rex, operandSize)

	case op2 >= 0xC8 && op2 <= 0xCF:
		// BSWAP r{32,64}: byte-swap the destination register. Only
		// defined for 32- and 64-bit operand sizes; the 16-bit form
		// is "undefined" per Intel SDM (we still permit it for
		// completeness).
		idx := uint8(op2 - 0xC8)
		if rex&rexB != 0 {
			idx |= 0x8
		}
		c.reg64[idx] = bswap(c.reg64[idx], operandSize)
		return nil

	case op2 == 0xB6:
		// MOVZX r, r/m8.
		return c.opMOVZX(rex, operandSize, 1)
	case op2 == 0xB7:
		// MOVZX r, r/m16.
		return c.opMOVZX(rex, operandSize, 2)
	case op2 == 0xBE:
		// MOVSX r, r/m8.
		return c.opMOVSX(rex, operandSize, 1)
	case op2 == 0xBF:
		// MOVSX r, r/m16.
		return c.opMOVSX(rex, operandSize, 2)

	case op2 == 0xA4:
		// SHLD r/m, r, imm8 — double-precision shift left.
		return c.opSHxD(rex, operandSize, true, false)
	case op2 == 0xA5:
		// SHLD r/m, r, CL.
		return c.opSHxD(rex, operandSize, true, true)
	case op2 == 0xAC:
		// SHRD r/m, r, imm8.
		return c.opSHxD(rex, operandSize, false, false)
	case op2 == 0xAD:
		// SHRD r/m, r, CL.
		return c.opSHxD(rex, operandSize, false, true)

	case op2 == 0xBC:
		// BSF r, r/m — bit scan forward (find lowest set bit). With an
		// F3 prefix on CPUs that report BMI1, the encoding becomes
		// TZCNT, which differs only in the source==0 case (TZCNT sets
		// CF=1 and writes operand-size; BSF sets ZF=1 and leaves the
		// destination undefined). We don't advertise BMI1, so the F3
		// prefix is silently ignored (matches CPUs without BMI1) and
		// the kernel's fallback path through BSF works as written.
		return c.opBSF(rex, operandSize)
	case op2 == 0xBD:
		// BSR r, r/m — bit scan reverse (find highest set bit). Same
		// LZCNT-vs-BSR consideration as TZCNT/BSF above.
		return c.opBSR(rex, operandSize)
	}

	// SSE2 / MMX dispatch (see sse2.go). Returns handled=true if the
	// opcode was recognised; otherwise we fall through to the
	// "unimplemented" path below. The SSE2 module needs the prefix
	// bytes the legacy decoder loop captured (operand-size and rep)
	// so it can pick the right per-prefix variant.
	if handled, err := c.opSSE2(op2, rex, repPrefix, has66); handled {
		return err
	}
	return c.unimplementedAt("0F %#02x rex=%#x", op2, rex)
}

// opBSF / opBSR — bit scan forward / reverse. Per Intel SDM:
//   - if src == 0: ZF = 1, destination is *undefined* (we leave it
//     unchanged, matching observable real-hardware behaviour on
//     several microarchitectures);
//   - else: ZF = 0, destination = bit position of the lowest (BSF)
//     or highest (BSR) set bit.
// CF/OF/SF/AF/PF are undefined on Intel; AMD documents them as
// preserved. We follow AMD's "leave them alone" rule for portability.
// opSHxD implements SHLD / SHRD — double-precision shift. Combines
// `dst` (r/m) with `src` (reg field) into a 2*operand-size virtual
// value, shifts left (SHLD) or right (SHRD) by `count` bits, and
// stores the operand-size slice that intersects dst.
//
// Per Intel SDM Vol 2:
//   - count is masked to 5 bits for 16/32-bit operand, 6 bits for 64.
//   - count == 0: no-op (flags unchanged).
//   - else: CF = the bit shifted OUT of dst; SF/ZF/PF reflect result.
//     AF undefined; OF defined only for count == 1 (sign changed).
//
// fromCL=true picks the byte CL as the count source (0xA5 / 0xAD);
// otherwise the count is an imm8 byte that follows the ModR/M (the
// 0xA4 / 0xAC variants — and that imm8 has to be passed to
// parseModRM64WithImm so a RIP-relative EA stays correct).
func (c *CPU) opSHxD(rex, operandSize uint8, left, fromCL bool) error {
	var immBytes uint8
	if !fromCL {
		immBytes = 1
	}
	m := c.parseModRM64WithImm(rex, immBytes)
	src := c.readReg(m.reg, operandSize)
	var count uint64
	if fromCL {
		count = uint64(c.GetReg8(CL))
	} else {
		count = uint64(c.fetch8())
	}
	if operandSize == 8 {
		count &= 0x3F
	} else {
		count &= 0x1F
	}
	if count == 0 {
		return nil
	}
	bits := uint64(operandSize) * 8
	dst := c.readOperand(m, operandSize) & mask(operandSize)
	srcMasked := src & mask(operandSize)
	var res uint64
	var cf bool
	if left {
		// SHLD: shift dst left, low bits from high bits of src.
		// CF = bit (bits - count) of original dst (the last bit shifted out).
		cf = (dst>>(bits-count))&1 != 0
		res = ((dst << count) | (srcMasked >> (bits - count))) & mask(operandSize)
	} else {
		// SHRD: shift dst right, high bits from low bits of src.
		// CF = bit (count - 1) of original dst.
		cf = (dst>>(count-1))&1 != 0
		res = (dst >> count) | ((srcMasked << (bits - count)) & mask(operandSize))
	}
	c.setCF(cf)
	// OF for count==1: set if the sign bit of the result differs from
	// the sign bit of the original dst. Undefined for count > 1; we
	// leave OF cleared to match what real silicon mostly does.
	if count == 1 {
		c.setOF((dst&signBit(operandSize) != 0) != (res&signBit(operandSize) != 0))
	} else {
		c.setOF(false)
	}
	// SF/ZF/PF from result; AF undefined (left untouched).
	if res == 0 {
		c.rflags |= RFLAGS_ZF
	} else {
		c.rflags &^= RFLAGS_ZF
	}
	if res&signBit(operandSize) != 0 {
		c.rflags |= RFLAGS_SF
	} else {
		c.rflags &^= RFLAGS_SF
	}
	if parity8(uint8(res)) {
		c.rflags |= RFLAGS_PF
	} else {
		c.rflags &^= RFLAGS_PF
	}
	c.writeOperand(m, res, operandSize)
	return nil
}

// opGroup15 dispatches the 0x0F 0xAE encoding family —
// FXSAVE/FXRSTOR/LDMXCSR/STMXCSR/XSAVE/XRSTOR/LFENCE/MFENCE/SFENCE/
// CLFLUSH. Most have a memory operand; fences and a couple of CLFLUSH-
// adjacent forms are reg-form (mod=11).
//
// Stub-level correctness: FXSAVE/FXRSTOR move 512 bytes of opaque FPU
// state to/from memory. The kernel only inspects the same bytes it
// wrote, so faithful round-trip is enough — initial FPU state is
// zero (matching FNINIT semantics). LDMXCSR/STMXCSR read/write the
// 32-bit MXCSR. We don't model MXCSR, so STMXCSR returns the default
// 0x1f80 (all exceptions masked), and LDMXCSR is a no-op.
//
// LFENCE/MFENCE/SFENCE are no-ops in our single-threaded model.
// CLFLUSH is a no-op (no CPU cache modelled).
func (c *CPU) opGroup15(rex uint8) error {
	m := c.parseModRM64(rex)
	if m.isReg {
		switch m.reg {
		case 5: // LFENCE
			return nil
		case 6: // MFENCE
			return nil
		case 7: // SFENCE
			return nil
		}
		return c.unimplementedAt("Group 15 /%d mod=11", m.reg)
	}
	// Memory form. Resolve the linear address through segment base.
	addr := c.segBaseForModRM(m) + m.ea
	switch m.reg {
	case 0: // FXSAVE — 512-byte save area, written from CPU FPU/XMM
		// state (see fxsave in x87.go).
		c.fxsave(addr)
		return nil
	case 1: // FXRSTOR — 512-byte save area, read back into CPU
		// FPU/XMM state.
		c.fxrstor(addr)
		return nil
	case 2: // LDMXCSR — load 4 bytes into MXCSR
		c.mxcsr = c.readMem32(addr)
		return nil
	case 3: // STMXCSR — store MXCSR
		c.writeMem32(addr, c.mxcsr)
		return nil
	case 7: // CLFLUSH — flush cache line (no cache modelled)
		return nil
	}
	return c.unimplementedAt("Group 15 /%d (memory form)", m.reg)
}

// opX87Stub was deleted in favour of handleX87 in x87.go, which
// provides a real x87 FPU implementation (port of cpu/x86's handleX87).

func (c *CPU) opBSF(rex, operandSize uint8) error {
	m := c.parseModRM64(rex)
	src := c.readOperand(m, operandSize) & mask(operandSize)
	if src == 0 {
		c.rflags |= RFLAGS_ZF
		return nil
	}
	c.rflags &^= RFLAGS_ZF
	// bits.TrailingZeros64 returns the index of the lowest set bit.
	pos := uint64(bits.TrailingZeros64(src))
	c.writeReg(m.reg, pos, operandSize)
	return nil
}

func (c *CPU) opBSR(rex, operandSize uint8) error {
	m := c.parseModRM64(rex)
	src := c.readOperand(m, operandSize) & mask(operandSize)
	if src == 0 {
		c.rflags |= RFLAGS_ZF
		return nil
	}
	c.rflags &^= RFLAGS_ZF
	// bits.Len64(x) - 1 = index of the highest set bit (for x != 0).
	pos := uint64(bits.Len64(src) - 1)
	c.writeReg(m.reg, pos, operandSize)
	return nil
}

// opGroup6 dispatches 0x0F 0x00 — SLDT/STR/LLDT/LTR/VERR/VERW. M5b
// only needs LLDT and LTR; the others are deferred. LLDT/LTR just
// stash the selector in seg[LDTR]/seg[TR]; the full descriptor walk
// happens lazily when the segment is consulted.
func (c *CPU) opGroup6(rex uint8) error {
	m := c.parseModRM64(rex)
	switch m.reg {
	case 0: // SLDT — store LDTR selector
		c.writeOperand(m, uint64(c.seg[LDTR]), 2)
		return nil
	case 1: // STR — store TR selector
		c.writeOperand(m, uint64(c.seg[TR]), 2)
		return nil
	case 2: // LLDT
		sel := uint16(c.readOperand(m, 2))
		c.seg[LDTR] = sel
		return nil
	case 3: // LTR — load task register. Beyond stashing the selector
		// we walk the GDT entry to extract the TSS base address. The
		// 64-bit TSS descriptor is 16 bytes (two GDT slots); offset 4
		// of the TSS itself holds RSP0, which is the stack the CPU
		// switches to on a user→kernel interrupt. Without this lookup,
		// deliverInterrupt pushes the IRETQ frame on whatever RSP was
		// live at fault time — typically the user RSP, which faults
		// during the push and breaks #PF delivery for user-mode faults.
		sel := uint16(c.readOperand(m, 2))
		c.seg[TR] = sel
		if sel&0xFFFC != 0 {
			gdtBase := c.segBase[GDTR]
			loVA := gdtBase + uint64(sel&0xFFF8)
			hiVA := loVA + 8
			loPhys, perr := c.translateForData(loVA, false)
			if perr != nil {
				return fmt.Errorf("LTR: translate GDT entry: %w", perr)
			}
			hiPhys, perr := c.translateForData(hiVA, false)
			if perr != nil {
				return fmt.Errorf("LTR: translate GDT entry+8: %w", perr)
			}
			lo, _ := c.memMap.Read64(loPhys)
			hi, _ := c.memMap.Read64(hiPhys)
			// SDM Vol.3 §3.5.2 — 64-bit TSS descriptor layout:
			//   lo[15:0]   = limit[15:0]
			//   lo[31:16]  = base[15:0]
			//   lo[39:32]  = base[23:16]
			//   lo[47:40]  = access (P/DPL/S/type)
			//   lo[55:48]  = limit[19:16] | flags (G/AVL)
			//   lo[63:56]  = base[31:24]
			//   hi[31:0]   = base[63:32]
			//   hi[63:32]  = reserved
			base := ((lo >> 16) & 0xFFFFFF) |
				(((lo >> 56) & 0xFF) << 24) |
				((hi & 0xFFFFFFFF) << 32)
			limit := uint32(lo&0xFFFF) | (uint32((lo>>48)&0xF) << 16)
			c.segBase[TR] = base
			c.segLimit[TR] = limit
		}
		return nil
	case 4: // VERR — verify segment can be read at current CPL
		// Stub: set ZF=1 (verification passes). Real impl walks
		// the descriptor and checks DPL/RPL; Linux only uses VERR/
		// VERW for the MDS-mitigation side-effect (microarchitectural
		// buffer clear), not for actual selector validation.
		_ = c.readOperand(m, 2) // consume the selector argument
		c.rflags |= RFLAGS_ZF
		return nil
	case 5: // VERW — verify segment can be written at current CPL
		// Same stub treatment as VERR. The MDS mitigation in Linux
		// 6.6 calls VERW with a known data-segment selector before
		// returning to user mode to flush the CPU's internal store
		// buffers — we have no such buffers, so the side-effect is
		// trivially satisfied.
		_ = c.readOperand(m, 2)
		c.rflags |= RFLAGS_ZF
		return nil
	}
	return unimplemented("Group 6 /%d", m.reg)
}

// opGroup7 dispatches 0x0F 0x01. Sub-ops 0..3 use a memory operand
// holding a pseudo-descriptor; 4/6 use a 16-bit operand; 7 is per-
// page TLB invalidation. mod=11 forms (XGETBV, MONITOR, etc.) are
// not yet wired.
func (c *CPU) opGroup7(rex uint8) error {
	m := c.parseModRM64(rex)
	if m.isReg {
		// Many mod=11 forms exist (XGETBV at reg=2,rm=0; SWAPGS at
		// reg=7,rm=0 — that one is M6); not yet routed.
		switch {
		case m.reg == 7 && m.rm == 0:
			// SWAPGS — atomic swap of GS.base and KernelGSBase. Lives
			// at 0x0F 0x01 0xF8.
			c.msrGSBase, c.msrKernelGSBase = c.msrKernelGSBase, c.msrGSBase
			c.segBase[GS] = c.msrGSBase
			return nil
		}
		return unimplemented("Group 7 mod=11 reg=%d rm=%d", m.reg, m.rm)
	}
	switch m.reg {
	case 0: // SGDT
		c.writeMem16(c.segBaseForModRM(m) + m.ea, uint16(c.segLimit[GDTR]))
		c.writeMem64(c.segBaseForModRM(m) + m.ea+2, c.segBase[GDTR])
		return nil
	case 1: // SIDT
		c.writeMem16(c.segBaseForModRM(m) + m.ea, uint16(c.segLimit[IDTR]))
		c.writeMem64(c.segBaseForModRM(m) + m.ea+2, c.segBase[IDTR])
		return nil
	case 2: // LGDT — load GDT base+limit from memory
		c.segLimit[GDTR] = uint32(c.readMem16(c.segBaseForModRM(m) + m.ea))
		c.segBase[GDTR] = c.readMem64(c.segBaseForModRM(m) + m.ea + 2)
		return nil
	case 3: // LIDT
		c.segLimit[IDTR] = uint32(c.readMem16(c.segBaseForModRM(m) + m.ea))
		c.segBase[IDTR] = c.readMem64(c.segBaseForModRM(m) + m.ea + 2)
		if intrTrace {
			fmt.Fprintf(os.Stderr, "[lidt] RIP=%#x base=%#x limit=%#x\n",
				c.rip, c.segBase[IDTR], c.segLimit[IDTR])
		}
		return nil
	case 4: // SMSW — store low 16 of CR0
		c.writeOperand(m, c.cr[0]&0xFFFF, 2)
		return nil
	case 6: // LMSW — set low 4 bits of CR0 (PE/MP/EM/TS). Cannot clear PE.
		v := uint16(c.readOperand(m, 2))
		c.cr[0] = (c.cr[0] &^ 0xF) | uint64(v&0xF)
		c.recomputeMode()
		return nil
	case 7: // INVLPG — invalidate the TLB entry covering m.ea.
		c.tlb.invalidatePage(m.ea)
		return nil
	}
	return unimplemented("Group 7 /%d", m.reg)
}

// opMovFromCR / opMovToCR — 0x0F 0x20 / 0x22. The ModR/M byte: reg is
// the CRn index (0..7, REX.R doesn't normally extend in long mode but
// some implementations honor it for CR8 access on AMD). rm is the
// GPR. mod is ignored (treated as 11).
func (c *CPU) opMovFromCR(rex uint8) error {
	mb := c.fetch8()
	cr := (mb >> 3) & 7
	rm := mb & 7
	if rex&rexR != 0 {
		cr |= 0x8
	}
	if rex&rexB != 0 {
		rm |= 0x8
	}
	c.reg64[rm&0xF] = c.cr[cr&0x7]
	return nil
}

func (c *CPU) opMovToCR(rex uint8) error {
	mb := c.fetch8()
	cr := (mb >> 3) & 7
	rm := mb & 7
	if rex&rexR != 0 {
		cr |= 0x8
	}
	if rex&rexB != 0 {
		rm |= 0x8
	}
	v := c.reg64[rm&0xF]
	c.writeCR(int(cr&0x7), v)
	return nil
}

// writeCR centralises the CR update + side-effects. Writes to CR0
// (PE/PG bits) and CR4 (PAE) can flip the long-mode-active latch
// (LMA), which gates the mode field. The recomputeMode call here
// keeps c.mode coherent without the rest of the decoder needing to
// know which CR was written.
//
// TLB invalidations follow Intel SDM Vol 3 §4.10.4:
//   - CR3 reload (PCID-less): drop all non-global entries.
//   - CR0.PG / CR0.WP transitions: full flush.
//   - CR4.PGE / CR4.PAE / CR4.PSE / CR4.SMEP / CR4.SMAP / CR4.PCIDE
//     transitions: full flush.
// We match the x86 backend's behaviour where the semantics are equivalent.
func (c *CPU) writeCR(n int, v uint64) {
	if cr3Trace && n == 3 {
		pml4 := v & 0xFFFFFFFFF000
		e0, _ := c.memMap.Read64(pml4)
		e273, _ := c.memMap.Read64(pml4 + 273*8)
		e511, _ := c.memMap.Read64(pml4 + 511*8)
		fmt.Fprintf(os.Stderr, "[cr3] RIP=%#x CR3 %#x -> %#x  [0]=%#x [273]=%#x [511]=%#x\n",
			c.rip, c.cr[3], v, e0, e273, e511)
	}
	if n == 0 {
		oldPG := c.cr[0]&CR0_PG != 0
		newPG := v&CR0_PG != 0
		oldWP := c.cr[0]&CR0_WP != 0
		newWP := v&CR0_WP != 0
		c.cr[0] = v
		// LMA latches when paging is enabled with LME set; clears when
		// paging turns off.
		if !oldPG && newPG && c.efer&EFER_LME != 0 {
			c.efer |= EFER_LMA
		} else if oldPG && !newPG {
			c.efer &^= EFER_LMA
		}
		if oldPG != newPG || oldWP != newWP {
			c.tlb.flushAll()
		}
		c.recomputeMode()
		return
	}
	if n == 3 {
		// Non-PCID CR3 reload: drop all non-global entries. The new CR3
		// install itself happens after the flush so the next translation
		// walks under the new root.
		c.tlb.flushNonGlobal()
		c.cr[3] = v
		return
	}
	if n == 4 {
		// Architecturally-relevant bits (PGE/PAE/PSE/SMEP/SMAP/PCIDE)
		// require a full flush; lesser bits do not. Flushing on any
		// change is conservative and matches the x86 backend.
		const flushMask = CR4_PGE | CR4_PAE | CR4_PSE | CR4_SMEP | CR4_SMAP | CR4_PCIDE
		if (c.cr[4]^v)&flushMask != 0 {
			c.tlb.flushAll()
		}
	}
	c.cr[n] = v
	if n == 4 {
		c.recomputeMode()
	}
}

func (c *CPU) opMovFromDR(rex uint8) error {
	mb := c.fetch8()
	dr := (mb >> 3) & 7
	rm := mb & 7
	if rex&rexB != 0 {
		rm |= 0x8
	}
	c.reg64[rm&0xF] = c.dr[dr&0x7]
	return nil
}

func (c *CPU) opMovToDR(rex uint8) error {
	mb := c.fetch8()
	dr := (mb >> 3) & 7
	rm := mb & 7
	if rex&rexB != 0 {
		rm |= 0x8
	}
	c.dr[dr&0x7] = c.reg64[rm&0xF]
	return nil
}

// MSR numbers we route. Unrecognized MSRs return zero on RDMSR and
// silently drop writes on WRMSR — real hardware raises #GP, but the
// boot path passes through several MSRs we don't model.
const (
	msrEFER          = 0xC0000080
	msrSTAR          = 0xC0000081
	msrLSTAR         = 0xC0000082
	msrCSTAR         = 0xC0000083
	msrSFMASK        = 0xC0000084
	msrFSBaseMSR     = 0xC0000100
	msrGSBaseMSR     = 0xC0000101
	msrKernelGSBase  = 0xC0000102
)

func (c *CPU) opRDMSR() error {
	num := c.GetReg32(ECX)
	v := c.readMSR(num)
	if msrTrace {
		fmt.Fprintf(os.Stderr, "[msr] rdmsr ecx=%#x → %#x RIP=%#x\n", num, v, c.rip)
	}
	c.SetReg32(EAX, uint32(v))
	c.SetReg32(EDX, uint32(v>>32))
	return nil
}

func (c *CPU) opWRMSR() error {
	num := c.GetReg32(ECX)
	v := uint64(c.GetReg32(EAX)) | (uint64(c.GetReg32(EDX)) << 32)
	if msrTrace {
		fmt.Fprintf(os.Stderr, "[msr] wrmsr ecx=%#x v=%#x RIP=%#x\n", num, v, c.rip)
	}
	return c.writeMSR(num, v)
}

func (c *CPU) readMSR(num uint32) uint64 {
	switch num {
	case msrEFER:
		return c.efer
	case msrSTAR:
		return c.msrStar
	case msrLSTAR:
		return c.msrLstar
	case msrCSTAR:
		return c.msrCstar
	case msrSFMASK:
		return c.msrSFMask
	case msrFSBaseMSR:
		return c.msrFSBase
	case msrGSBaseMSR:
		return c.msrGSBase
	case msrKernelGSBase:
		return c.msrKernelGSBase
	}
	return 0
}

func (c *CPU) writeMSR(num uint32, v uint64) error {
	switch num {
	case msrEFER:
		// Route through SetEFER so the LMA-latch logic AND the TLB
		// flush on EFER.NXE / LMA / LME transitions happen in one
		// place. Direct c.efer assignment used to live here and was
		// a real source of subtle drift across paths.
		c.SetEFER(v)
	case msrSTAR:
		c.msrStar = v
	case msrLSTAR:
		c.msrLstar = v
	case msrCSTAR:
		c.msrCstar = v
	case msrSFMASK:
		c.msrSFMask = v
	case msrFSBaseMSR:
		c.msrFSBase = v
		c.segBase[FS] = v
	case msrGSBaseMSR:
		c.msrGSBase = v
		c.segBase[GS] = v
	case msrKernelGSBase:
		c.msrKernelGSBase = v
	}
	return nil
}

// opGroup8 — 0x0F 0xBA — BT/BTS/BTR/BTC r/m, imm8. The reg field
// of ModR/M selects the operation (4..7); the imm8 is the bit
// index masked to operandSize*8 - 1.
func (c *CPU) opGroup8(rex, operandSize uint8) error {
	m := c.parseModRM64WithImm(rex, 1) // BT/BTS/BTR/BTC r/m, imm8
	imm := uint64(c.fetch8())
	bitWidth := uint64(operandSize) * 8
	bitNum := imm & (bitWidth - 1)
	dst := c.readOperand(m, operandSize)
	bitVal := (dst >> bitNum) & 1
	if bitVal != 0 {
		c.rflags |= RFLAGS_CF
	} else {
		c.rflags &^= RFLAGS_CF
	}
	switch m.reg {
	case 4: // BT — no writeback
		return nil
	case 5: // BTS
		dst |= 1 << bitNum
	case 6: // BTR
		dst &^= 1 << bitNum
	case 7: // BTC
		dst ^= 1 << bitNum
	default:
		return unimplemented("Group 8 /%d", m.reg)
	}
	c.writeOperand(m, dst, operandSize)
	return nil
}

// opBT implements the register-bit-index forms — 0x0F A3 BT,
// 0x0F AB BTS, 0x0F B3 BTR. The bit index in the source register
// is taken modulo operandSize*8 (no masking for memory operands in
// real hardware — the offset can range over 4 GiB / 8 — but our
// implementation always works against the register-form value).
func (c *CPU) opBT(rex, operandSize uint8, set, reset bool) error {
	m := c.parseModRM64(rex)
	idx := c.readReg(m.reg, operandSize)
	return c.opBitTest(m, idx, operandSize, set, reset, false)
}

// opBTC implements BTC r/m, r — toggle bit, copy old to CF.
func (c *CPU) opBTC(rex, operandSize uint8) error {
	m := c.parseModRM64(rex)
	idx := c.readReg(m.reg, operandSize)
	return c.opBitTest(m, idx, operandSize, false, false, true)
}

// opBitTest is the shared implementation of the BT/BTS/BTR/BTC family
// when the bit index comes from a register operand. Behaviour depends
// on whether the destination is a register or memory; the SDM has
// two distinct rules:
//
//   register destination — the bit index is masked to (operand_size - 1).
//     `bts rax, rbx` with rbx = 253 sets bit (253 & 63) = 61 of rax.
//
//   memory destination — the bit index is treated as a SIGNED integer
//     that extends the memory address. `bts qword [mem], rax` with
//     rax = 253 sets bit 253 of memory at [mem], landing at byte
//     [mem + 253/8] (= [mem + 31]) bit 5. Equivalently, addresses the
//     word at [mem + (idx / word_bits) * (word_bits/8)] and sets bit
//     (idx mod word_bits) within it. Signed division rounds toward
//     zero, so a negative idx with non-zero remainder needs a -1
//     adjustment to the word index and a +word_bits to the bit-within-
//     word (the same pattern the i386 backend uses).
//
// We previously masked the bit index unconditionally — that meant
// `lock bts qword [system_vectors], rax` with rax = 253 (Linux's
// idt_setup_from_table when sys=true and vec=253) ended up setting
// bit 61 of system_vectors[0] instead of bit 253. The
// for_each_clear_bit_from loop in idt_setup_apic_and_irq_gates then
// saw bit 61 "set" and silently skipped installing the IRQ stub at
// vec 48-63 — leaving idt_table[48] empty and the first PIT IRQ
// faulting "gate not present" on the Linux 6.6 boot.
func (c *CPU) opBitTest(m modRMResult, idx uint64, operandSize uint8, set, reset, complement bool) error {
	bitWidth := uint64(operandSize) * 8
	if m.isReg {
		bitNum := idx & (bitWidth - 1)
		dst := c.readOperand(m, operandSize)
		bitVal := (dst >> bitNum) & 1
		if bitVal != 0 {
			c.rflags |= RFLAGS_CF
		} else {
			c.rflags &^= RFLAGS_CF
		}
		switch {
		case set:
			dst |= 1 << bitNum
			c.writeOperand(m, dst, operandSize)
		case reset:
			dst &^= 1 << bitNum
			c.writeOperand(m, dst, operandSize)
		case complement:
			dst ^= 1 << bitNum
			c.writeOperand(m, dst, operandSize)
		}
		return nil
	}
	// Memory destination — idx is a signed bit offset relative to the
	// base address. Address shifts by floor(idx / bitWidth) words.
	signedIdx := int64(idx)
	wordIdx := signedIdx / int64(bitWidth)
	bitInWord := signedIdx % int64(bitWidth)
	if bitInWord < 0 {
		bitInWord += int64(bitWidth)
		wordIdx--
	}
	baseEA := c.segBaseForModRM(m) + m.ea
	addr := baseEA + uint64(wordIdx*int64(operandSize))
	var dst uint64
	switch operandSize {
	case 8:
		dst = c.readMem64(addr)
	case 4:
		dst = uint64(c.readMem32(addr))
	case 2:
		dst = uint64(c.readMem16(addr))
	}
	bitVal := (dst >> uint64(bitInWord)) & 1
	if bitVal != 0 {
		c.rflags |= RFLAGS_CF
	} else {
		c.rflags &^= RFLAGS_CF
	}
	switch {
	case set:
		dst |= 1 << uint64(bitInWord)
	case reset:
		dst &^= 1 << uint64(bitInWord)
	case complement:
		dst ^= 1 << uint64(bitInWord)
	default:
		return nil // BT: read-only, no write-back
	}
	switch operandSize {
	case 8:
		c.writeMem64(addr, dst)
	case 4:
		c.writeMem32(addr, uint32(dst))
	case 2:
		c.writeMem16(addr, uint16(dst))
	}
	return nil
}

// opXCHGRM implements 0x86 / 0x87 — XCHG r/m, r. The LOCK prefix
// is implicit on the memory form per Intel SDM; we treat the swap
// atomically by virtue of being single-threaded.
func (c *CPU) opXCHGRM(rex, operandSize uint8) error {
	m := c.parseModRM64(rex)
	var src, dst uint64
	if operandSize == 1 {
		src = uint64(c.read8RegField(m))
		if m.isReg {
			dst = uint64(c.read8FromModRM(m))
		} else {
			dst = uint64(c.readMem8(c.segBaseForModRM(m) + m.ea))
		}
	} else {
		src = c.readReg(m.reg, operandSize)
		dst = c.readOperand(m, operandSize)
	}
	if operandSize == 1 {
		c.write8RegField(m, uint8(dst))
		if m.isReg {
			c.write8FromModRM(m, uint8(src))
		} else {
			c.writeMem8(c.segBaseForModRM(m) + m.ea, uint8(src))
		}
	} else {
		c.writeReg(m.reg, dst, operandSize)
		c.writeOperand(m, src, operandSize)
	}
	return nil
}

// opCMPXCHG implements 0x0F 0xB0 / 0xB1 — CMPXCHG. The accumulator
// (AL/AX/EAX/RAX) is compared with the destination. On match the
// source register is stored; on miss the destination is loaded into
// the accumulator. ZF tracks the comparison outcome.
func (c *CPU) opCMPXCHG(rex, operandSize uint8) error {
	m := c.parseModRM64(rex)
	var dst, src, acc uint64
	if operandSize == 1 {
		src = uint64(c.read8RegField(m))
		if m.isReg {
			dst = uint64(c.read8FromModRM(m))
		} else {
			dst = uint64(c.readMem8(c.segBaseForModRM(m) + m.ea))
		}
		acc = uint64(c.GetReg8(AL))
	} else {
		src = c.readReg(m.reg, operandSize)
		dst = c.readOperand(m, operandSize)
		acc = c.readReg(RAX, operandSize)
	}
	// SUB acc, dst — sets flags as for CMP; CMPXCHG uses these
	// same flag semantics so ZF reflects equality.
	_, fl := sub(acc, dst, operandSize)
	c.setArithFlags(fl)
	if fl.zf {
		if operandSize == 1 {
			if m.isReg {
				c.write8FromModRM(m, uint8(src))
			} else {
				c.writeMem8(c.segBaseForModRM(m) + m.ea, uint8(src))
			}
		} else {
			c.writeOperand(m, src, operandSize)
		}
	} else {
		if operandSize == 1 {
			c.SetReg8(AL, uint8(dst))
		} else {
			c.writeReg(RAX, dst, operandSize)
		}
	}
	return nil
}

// opXADD implements 0x0F 0xC0 / 0xC1 — XADD. Exchange the
// destination with the source, then add: dst_new = old_dst + src,
// src_new = old_dst.
func (c *CPU) opXADD(rex, operandSize uint8) error {
	m := c.parseModRM64(rex)
	var dst, src uint64
	if operandSize == 1 {
		src = uint64(c.read8RegField(m))
		if m.isReg {
			dst = uint64(c.read8FromModRM(m))
		} else {
			dst = uint64(c.readMem8(c.segBaseForModRM(m) + m.ea))
		}
	} else {
		src = c.readReg(m.reg, operandSize)
		dst = c.readOperand(m, operandSize)
	}
	res, fl := add(dst, src, operandSize)
	if operandSize == 1 {
		c.write8RegField(m, uint8(dst))
		if m.isReg {
			c.write8FromModRM(m, uint8(res))
		} else {
			c.writeMem8(c.segBaseForModRM(m) + m.ea, uint8(res))
		}
	} else {
		c.writeReg(m.reg, dst, operandSize)
		c.writeOperand(m, res, operandSize)
	}
	c.setArithFlags(fl)
	return nil
}

// opMOVZX implements MOVZX r, r/m{8,16}. srcSize is 1 or 2; the
// destination width follows operandSize and writes through writeReg,
// which already handles the 32-bit-write-zero-extends-to-64 rule.
func (c *CPU) opMOVZX(rex, operandSize, srcSize uint8) error {
	m := c.parseModRM64(rex)
	var src uint64
	switch {
	case m.isReg && srcSize == 1:
		src = uint64(c.read8FromModRM(m))
	case m.isReg:
		src = c.readReg(m.rm, srcSize)
	case srcSize == 1:
		src = uint64(c.readMem8(c.segBaseForModRM(m) + m.ea))
	default:
		src = uint64(c.readMem16(c.segBaseForModRM(m) + m.ea))
	}
	c.writeReg(m.reg, src, operandSize)
	return nil
}

// opMOVSX implements MOVSX r, r/m{8,16}.
func (c *CPU) opMOVSX(rex, operandSize, srcSize uint8) error {
	m := c.parseModRM64(rex)
	var src uint64
	switch {
	case m.isReg && srcSize == 1:
		src = uint64(c.read8FromModRM(m))
	case m.isReg:
		src = c.readReg(m.rm, srcSize)
	case srcSize == 1:
		src = uint64(c.readMem8(c.segBaseForModRM(m) + m.ea))
	default:
		src = uint64(c.readMem16(c.segBaseForModRM(m) + m.ea))
	}
	if srcSize == 1 {
		src = uint64(int64(int8(src)))
	} else {
		src = uint64(int64(int16(src)))
	}
	c.writeReg(m.reg, src, operandSize)
	return nil
}

// opMOVSXD implements 0x63 — MOVSXD r64, r/m32. The 32-bit source is
// sign-extended into the full 64-bit destination. (In 32-bit mode this
// opcode was ARPL; long mode reuses the encoding.)
func (c *CPU) opMOVSXD(rex uint8) error {
	m := c.parseModRM64(rex)
	var src uint32
	if m.isReg {
		src = uint32(c.readReg(m.rm, 4))
	} else {
		src = c.readMem32(c.segBaseForModRM(m) + m.ea)
	}
	c.writeReg(m.reg, uint64(int64(int32(src))), 8)
	return nil
}

// evalCond evaluates the four-bit condition code (the low nibble of
// the conditional opcode). Order matches Intel SDM Vol 1 Appendix B:
//
//	0 O    1 NO   2 B/C   3 NB/NC   4 Z/E   5 NZ/NE   6 BE/NA   7 NBE/A
//	8 S    9 NS   A P/PE  B NP/PO   C L     D NL/GE   E LE/NG   F NLE/G
func (c *CPU) evalCond(cc uint8) bool {
	fl := c.rflags
	cf := fl&RFLAGS_CF != 0
	zf := fl&RFLAGS_ZF != 0
	sf := fl&RFLAGS_SF != 0
	of := fl&RFLAGS_OF != 0
	pf := fl&RFLAGS_PF != 0
	switch cc {
	case 0x0:
		return of
	case 0x1:
		return !of
	case 0x2:
		return cf
	case 0x3:
		return !cf
	case 0x4:
		return zf
	case 0x5:
		return !zf
	case 0x6:
		return cf || zf
	case 0x7:
		return !cf && !zf
	case 0x8:
		return sf
	case 0x9:
		return !sf
	case 0xA:
		return pf
	case 0xB:
		return !pf
	case 0xC:
		return sf != of
	case 0xD:
		return sf == of
	case 0xE:
		return zf || sf != of
	case 0xF:
		return !zf && sf == of
	}
	return false
}

// ===== ALU dispatch =====

type aluOp int

const (
	aluADD aluOp = iota
	aluOR
	aluADC
	aluSBB
	aluAND
	aluSUB
	aluXOR
	aluCMP
	aluTEST
)

// aluApply runs op over (dst, src) at the given operand size, returning
// (result, flags). ADC/SBB are caller-CF-aware via the closure variant
// aluApplyWithCF below — direct callers of aluApply should not select
// aluADC/aluSBB. For the bitwise ops (AND, OR, XOR, TEST) CF and OF
// are cleared. aluCMP and aluTEST are no-writeback variants of aluSUB
// and aluAND respectively — the dispatcher decides whether to commit
// the result.
func aluApply(op aluOp, dst, src uint64, size uint8) (uint64, flagBits) {
	switch op {
	case aluADD:
		return add(dst, src, size)
	case aluSUB, aluCMP:
		return sub(dst, src, size)
	case aluAND, aluTEST:
		r := (dst & src) & mask(size)
		return r, logicalFlags(r, size)
	case aluOR:
		r := (dst | src) & mask(size)
		return r, logicalFlags(r, size)
	case aluXOR:
		r := (dst ^ src) & mask(size)
		return r, logicalFlags(r, size)
	}
	return 0, flagBits{}
}

// aluApplyWithCF is like aluApply but threads the CPU's current CF
// into the computation. Required for ADC/SBB; for other ops it falls
// through to aluApply.
func (c *CPU) aluApplyWithCF(op aluOp, dst, src uint64, size uint8) (uint64, flagBits) {
	var cf uint64
	if c.rflags&RFLAGS_CF != 0 {
		cf = 1
	}
	switch op {
	case aluADC:
		return addWithCarry(dst, src, cf, size)
	case aluSBB:
		return subWithBorrow(dst, src, cf, size)
	}
	return aluApply(op, dst, src, size)
}

// aluWritesBack reports whether op stores its result. CMP and TEST
// only update flags.
func aluWritesBack(op aluOp) bool {
	return op != aluCMP && op != aluTEST
}

// opALURM handles the "r/m, r" form. Width-1 operands use the
// REX-aware 8-bit register encoding (AH/CH/DH/BH vs SPL/BPL/SIL/DIL).
func (c *CPU) opALURM(rex, operandSize uint8, op aluOp) error {
	m := c.parseModRM64(rex)
	var src, dst uint64
	if operandSize == 1 {
		src = uint64(c.read8RegField(m))
		if m.isReg {
			dst = uint64(c.read8FromModRM(m))
		} else {
			dst = uint64(c.readMem8(c.segBaseForModRM(m) + m.ea))
		}
	} else {
		src = c.readReg(m.reg, operandSize)
		dst = c.readOperand(m, operandSize)
	}
	res, fl := c.aluApplyWithCF(op, dst, src, operandSize)
	if aluWritesBack(op) {
		if operandSize == 1 {
			if m.isReg {
				c.write8FromModRM(m, uint8(res))
			} else {
				c.writeMem8(c.segBaseForModRM(m) + m.ea, uint8(res))
			}
		} else {
			c.writeOperand(m, res, operandSize)
		}
	}
	c.setArithFlags(fl)
	return nil
}

// opALURfromM handles the "r, r/m" form.
func (c *CPU) opALURfromM(rex, operandSize uint8, op aluOp) error {
	m := c.parseModRM64(rex)
	var src, dst uint64
	if operandSize == 1 {
		if m.isReg {
			src = uint64(c.read8FromModRM(m))
		} else {
			src = uint64(c.readMem8(c.segBaseForModRM(m) + m.ea))
		}
		dst = uint64(c.read8RegField(m))
	} else {
		src = c.readOperand(m, operandSize)
		dst = c.readReg(m.reg, operandSize)
	}
	res, fl := c.aluApplyWithCF(op, dst, src, operandSize)
	if aluWritesBack(op) {
		if operandSize == 1 {
			c.write8RegField(m, uint8(res))
		} else {
			c.writeReg(m.reg, res, operandSize)
		}
	}
	c.setArithFlags(fl)
	return nil
}

// opALUImmRAX handles the 0x05/0x0D/0x25/0x2D/0x35/0x3D and 0xA9
// (TEST) family — "op rAX, imm". imm is operandSize bytes (max 32,
// sign-extended to 64 in the 8-byte case).
func (c *CPU) opALUImmRAX(operandSize uint8, op aluOp) error {
	var imm uint64
	switch operandSize {
	case 2:
		imm = uint64(c.fetch16())
	default:
		imm = uint64(int64(int32(c.fetch32())))
	}
	dst := c.readReg(RAX, operandSize)
	res, fl := c.aluApplyWithCF(op, dst, imm, operandSize)
	if aluWritesBack(op) {
		c.writeReg(RAX, res, operandSize)
	}
	c.setArithFlags(fl)
	return nil
}

// opALUImmAL handles the byte-form rAX-imm immediates: 0x04 ADD AL,
// imm8 / 0x0C OR / 0x24 AND / 0x2C SUB / 0x34 XOR / 0x3C CMP /
// 0xA8 TEST AL, imm8.
func (c *CPU) opALUImmAL(op aluOp) error {
	imm := uint64(c.fetch8())
	dst := uint64(c.GetReg8(AL))
	res, fl := c.aluApplyWithCF(op, dst, imm, 1)
	if aluWritesBack(op) {
		c.SetReg8(AL, uint8(res))
	}
	c.setArithFlags(fl)
	return nil
}

// opGroup3 dispatches 0xF7 — Group 3 with non-byte operand size.
// Sub-ops: 0=TEST r/m,imm, 1=reserved, 2=NOT, 3=NEG, 4=MUL, 5=IMUL,
// 6=DIV, 7=IDIV. The 0xF6 byte-operand variant is not yet wired.
func (c *CPU) opGroup3(rex, operandSize uint8) error {
	// Sub-op /0 and /1 (TEST) have a trailing immediate whose size
	// matches the operand width (capped at 32 for 64-bit operands).
	// Other sub-ops (NOT/NEG/MUL/IMUL/DIV/IDIV) have no immediate.
	// We can only know which after parsing ModR/M, so call the plain
	// parser and apply a RIP-relative fixup in the TEST branch.
	m := c.parseModRM64(rex)
	switch m.reg {
	case 0, 1: // TEST r/m, imm. Immediate width = operand size.
		var imm uint64
		switch operandSize {
		case 1:
			m.shiftEAForImm(1)
			imm = uint64(c.fetch8())
		case 2:
			m.shiftEAForImm(2)
			imm = uint64(c.fetch16())
		default:
			m.shiftEAForImm(4)
			imm = uint64(int64(int32(c.fetch32())))
		}
		dst := c.readOperand(m, operandSize)
		r := (dst & imm) & mask(operandSize)
		c.setArithFlags(logicalFlags(r, operandSize))
		return nil
	case 2: // NOT r/m — does not affect flags
		dst := c.readOperand(m, operandSize)
		c.writeOperand(m, ^dst&mask(operandSize), operandSize)
		return nil
	case 3: // NEG r/m
		dst := c.readOperand(m, operandSize)
		res, fl := sub(0, dst, operandSize)
		fl.cf = dst != 0 // CF = "source was non-zero" per SDM (overrides sub's CF rule)
		c.writeOperand(m, res, operandSize)
		c.setArithFlags(fl)
		return nil
	case 4: // MUL r/m — unsigned: rDX:rAX = rAX × r/m
		dst := c.readOperand(m, operandSize)
		return c.opMUL(dst, operandSize)
	case 5: // IMUL r/m (one-operand) — signed: rDX:rAX = rAX × r/m
		dst := c.readOperand(m, operandSize)
		return c.opIMUL1Op(dst, operandSize)
	case 6: // DIV r/m — unsigned
		dst := c.readOperand(m, operandSize)
		return c.opDIV(dst, operandSize)
	case 7: // IDIV r/m — signed
		dst := c.readOperand(m, operandSize)
		return c.opIDIV(dst, operandSize)
	}
	return unimplemented("Group 3 /%d", m.reg)
}

// opMUL: unsigned multiply of rAX (operandSize wide) by src; the
// product fills rDX:rAX. CF and OF are set if the upper half is
// nonzero — i.e. the product didn't fit in the low operand-width.
func (c *CPU) opMUL(src uint64, operandSize uint8) error {
	a := c.readReg(RAX, operandSize) & mask(operandSize)
	src &= mask(operandSize)
	var hi, lo uint64
	switch operandSize {
	case 8:
		hi, lo = bits.Mul64(a, src)
	default:
		prod := a * src
		hi = prod >> (uint(operandSize) * 8)
		lo = prod & mask(operandSize)
	}
	// Write back. For 8-bit MUL the entire product lands in AX (no
	// RDX); we don't model the 8-bit form yet so 16/32/64 only.
	c.writeReg(RAX, lo, operandSize)
	c.writeReg(RDX, hi, operandSize)
	var fl flagBits
	fl.cf = hi != 0
	fl.of = hi != 0
	// SF/ZF/PF/AF are "undefined" per SDM; setArithFlags clears them
	// (which is a common but not architecturally guaranteed result).
	c.setArithFlags(fl)
	return nil
}

// opIMUL1Op: one-operand signed multiply rDX:rAX = rAX × src. CF/OF
// set if the result doesn't fit in operandSize bits (the high half
// is not just the sign extension of the low half).
func (c *CPU) opIMUL1Op(src uint64, operandSize uint8) error {
	a := c.readReg(RAX, operandSize) & mask(operandSize)
	src &= mask(operandSize)
	// Sign-extend both to 64 bits, multiply, then split.
	as := signExtend(a, operandSize)
	ss := signExtend(src, operandSize)
	prod128hi, prod128lo := mul128(uint64(as), uint64(ss))
	// Low half goes to rAX, high half to rDX, at operandSize width.
	c.writeReg(RAX, prod128lo, operandSize)
	c.writeReg(RDX, prod128hi, operandSize)
	// CF/OF: set if the sign-extended low half does NOT equal the full
	// 128-bit product. Equivalent: hi part not just sign-fill of lo.
	expectedHi := uint64(0)
	if prod128lo&signBit(operandSize) != 0 {
		expectedHi = mask(operandSize) // all-ones in the operand-width
	}
	// For operandSize < 8 we only compare within the operand width.
	var fl flagBits
	fl.cf = prod128hi&mask(operandSize) != expectedHi
	fl.of = fl.cf
	c.setArithFlags(fl)
	return nil
}

// opIMUL2Op: 0x0F 0xAF — IMUL r, r/m. Destination = reg = reg × r/m.
// Only the low operandSize bits of the product are kept; flags
// indicate whether the truncation lost meaningful information.
func (c *CPU) opIMUL2Op(rex, operandSize uint8) error {
	m := c.parseModRM64(rex)
	a := signExtend(c.readReg(m.reg, operandSize), operandSize)
	b := signExtend(c.readOperand(m, operandSize), operandSize)
	prod128hi, prod128lo := mul128(uint64(a), uint64(b))
	c.writeReg(m.reg, prod128lo, operandSize)
	expectedHi := uint64(0)
	if prod128lo&signBit(operandSize) != 0 {
		expectedHi = mask(operandSize)
	}
	var fl flagBits
	fl.cf = prod128hi&mask(operandSize) != expectedHi
	fl.of = fl.cf
	c.setArithFlags(fl)
	return nil
}

// opIMULImm: 0x69 (imm32 sign-extended) or 0x6B (imm8 sign-extended)
// — three-operand IMUL: destination = r/m × imm. ModR/M.reg is the
// destination; ModR/M.rm is the source.
func (c *CPU) opIMULImm(rex, operandSize uint8, imm8 bool) error {
	// 0x6B = imm8 (sign-extended); 0x69 = imm matching operand size (16
	// or 32 — never 64, since the imm32 sign-extends to 64).
	var immBytes uint8
	if imm8 {
		immBytes = 1
	} else if operandSize == 2 {
		immBytes = 2
	} else {
		immBytes = 4
	}
	m := c.parseModRM64WithImm(rex, immBytes)
	var imm int64
	if imm8 {
		imm = int64(int8(c.fetch8()))
	} else {
		switch operandSize {
		case 2:
			imm = int64(int16(c.fetch16()))
		default:
			imm = int64(int32(c.fetch32()))
		}
	}
	src := signExtend(c.readOperand(m, operandSize), operandSize)
	prod128hi, prod128lo := mul128(uint64(src), uint64(imm))
	c.writeReg(m.reg, prod128lo, operandSize)
	expectedHi := uint64(0)
	if prod128lo&signBit(operandSize) != 0 {
		expectedHi = mask(operandSize)
	}
	var fl flagBits
	fl.cf = prod128hi&mask(operandSize) != expectedHi
	fl.of = fl.cf
	c.setArithFlags(fl)
	return nil
}

// opDIV: unsigned divide of rDX:rAX by src, quotient to rAX,
// remainder to rDX. Divide-by-zero or quotient overflow raises #DE on
// real hardware; we surface as an error for now (Phase 7 wires up
// #DE delivery through the IDT).
func (c *CPU) opDIV(src uint64, operandSize uint8) error {
	src &= mask(operandSize)
	if src == 0 {
		return unimplemented("#DE on division by zero — IDT delivery pending")
	}
	switch operandSize {
	case 8:
		hi := c.GetReg64(RDX)
		lo := c.GetReg64(RAX)
		if hi >= src {
			return unimplemented("#DE on quotient overflow — IDT delivery pending")
		}
		q, r := bits.Div64(hi, lo, src)
		c.SetReg64(RAX, q)
		c.SetReg64(RDX, r)
	default:
		// 32-bit: dividend = EDX:EAX (assembled into a 64-bit value).
		hi := uint64(c.readReg(RDX, operandSize))
		lo := uint64(c.readReg(RAX, operandSize))
		dividend := (hi << (uint(operandSize) * 8)) | lo
		q := dividend / src
		r := dividend % src
		if q > mask(operandSize) {
			return unimplemented("#DE on quotient overflow")
		}
		c.writeReg(RAX, q, operandSize)
		c.writeReg(RDX, r, operandSize)
	}
	// DIV leaves SF/ZF/PF/AF/CF/OF undefined; we clear them.
	c.setArithFlags(flagBits{})
	return nil
}

// opIDIV: signed counterpart.
func (c *CPU) opIDIV(src uint64, operandSize uint8) error {
	srcS := signExtend(src&mask(operandSize), operandSize)
	if srcS == 0 {
		return unimplemented("#DE on signed division by zero")
	}
	switch operandSize {
	case 8:
		hi := int64(c.GetReg64(RDX))
		lo := c.GetReg64(RAX)
		// Compose a signed 128-bit dividend and divide. Most cases the
		// dividend fits in 64 signed bits (RDX is just sign-extension of
		// RAX). M4 handles that common case; the full 128-bit IDIV
		// arrives if/when we see real code emit it.
		if hi != int64(-1) && hi != 0 {
			return unimplemented("IDIV with non-trivial RDX hi half")
		}
		dividend := int64(lo)
		q := dividend / srcS
		r := dividend % srcS
		c.SetReg64(RAX, uint64(q))
		c.SetReg64(RDX, uint64(r))
	default:
		hi := signExtend(c.readReg(RDX, operandSize), operandSize)
		lo := uint64(c.readReg(RAX, operandSize))
		// Compose signed dividend at 2× width.
		dividend := (hi << (uint(operandSize) * 8)) | int64(lo)
		q := dividend / srcS
		r := dividend % srcS
		c.writeReg(RAX, uint64(q), operandSize)
		c.writeReg(RDX, uint64(r), operandSize)
	}
	c.setArithFlags(flagBits{})
	return nil
}

// signExtend interprets v (operandSize bits) as a signed value and
// sign-extends it into a Go int64.
func signExtend(v uint64, operandSize uint8) int64 {
	switch operandSize {
	case 1:
		return int64(int8(v))
	case 2:
		return int64(int16(v))
	case 4:
		return int64(int32(v))
	default:
		return int64(v)
	}
}

// mul128 returns the high and low 64 bits of a signed 64x64
// multiplication. Goes through unsigned bits.Mul64 and corrects the
// high half using the "subtract-on-negative" identity.
func mul128(a, b uint64) (uint64, uint64) {
	hi, lo := bits.Mul64(a, b)
	if int64(a) < 0 {
		hi -= b
	}
	if int64(b) < 0 {
		hi -= a
	}
	return hi, lo
}

// bswap byte-swaps the low operandSize bytes of v (BSWAP). The 16-bit
// form is "undefined" per Intel SDM (older silicon zeroed the result);
// we mirror what most modern CPUs do: leave upper bits unchanged and
// swap the low pair.
func bswap(v uint64, operandSize uint8) uint64 {
	switch operandSize {
	case 8:
		return bits.ReverseBytes64(v)
	case 4:
		return uint64(bits.ReverseBytes32(uint32(v)))
	case 2:
		return (v & ^uint64(0xFFFF)) | uint64(bits.ReverseBytes16(uint16(v)))
	}
	return v
}

// opGroup2Reg handles the shift/rotate family forms with no
// immediate: 0xD0/D1 (count = 1) and 0xD2/D3 (count = CL). The caller
// supplies the count, which means a CL value of zero is correctly
// interpreted as "shift by 0" rather than "fetch an imm8 from the
// instruction stream" — a real boot-time bug we hit when the kernel
// did `shl eax, cl` with CL=0 and our previous single-entry dispatch
// over-consumed one byte of the next instruction.
//
// Per Intel SDM Vol 2: count is masked to 5 bits for 8/16/32-bit
// operands and 6 bits for 64-bit. ROL/ROR further reduce count
// modulo operand width; RCL/RCR modulo (width+1). Zero-count
// operations leave all flags unchanged.
func (c *CPU) opGroup2Reg(rex, operandSize uint8, count uint64) error {
	m := c.parseModRM64(rex) // no trailing immediate
	return c.opGroup2Body(m, operandSize, count)
}

// opGroup2Imm handles the imm8-bearing forms of Group 2 (0xC0/0xC1).
// The trailing imm8 is communicated to parseModRM64WithImm so RIP-
// relative effective addresses point at the right qword.
func (c *CPU) opGroup2Imm(rex, operandSize uint8) error {
	m := c.parseModRM64WithImm(rex, 1)
	count := uint64(c.fetch8())
	return c.opGroup2Body(m, operandSize, count)
}

func (c *CPU) opGroup2Body(m modRMResult, operandSize uint8, count uint64) error {
	// Mask count per Intel SDM: 5 bits for 8/16/32 ops, 6 bits for 64.
	if operandSize == 8 {
		count &= 0x3F
	} else {
		count &= 0x1F
	}
	if count == 0 {
		return nil // flags unchanged
	}
	dst := c.readOperand(m, operandSize) & mask(operandSize)
	var res uint64
	var fl flagBits
	width := uint64(operandSize) * 8
	// Rotates (ROL/ROR/RCL/RCR) preserve AF/PF/ZF/SF; only CF (and OF
	// for count==1) get touched. We mutate c.rflags directly for those
	// rather than going through setArithFlags (which clears all six).
	switch m.reg {
	case 0: // ROL — rotate left, no CF involvement in the rotation
		eff := count % width
		if eff == 0 {
			res = dst
		} else {
			res = ((dst << eff) | (dst >> (width - eff))) & mask(operandSize)
		}
		newCF := res&1 != 0
		c.setCF(newCF)
		if count == 1 {
			hi := res & signBit(operandSize)
			c.setOF((hi != 0) != newCF)
		}
		c.writeOperand(m, res, operandSize)
		return nil
	case 1: // ROR — rotate right
		eff := count % width
		if eff == 0 {
			res = dst
		} else {
			res = ((dst >> eff) | (dst << (width - eff))) & mask(operandSize)
		}
		newCF := res&signBit(operandSize) != 0
		c.setCF(newCF)
		if count == 1 {
			hi1 := res & signBit(operandSize)
			hi2 := res & (signBit(operandSize) >> 1)
			c.setOF((hi1 != 0) != (hi2 != 0))
		}
		c.writeOperand(m, res, operandSize)
		return nil
	case 2: // RCL — rotate-through-carry left (width+1 bits)
		eff := count % (width + 1)
		cfIn := uint64(0)
		if c.rflags&RFLAGS_CF != 0 {
			cfIn = 1
		}
		newCF := cfIn != 0
		if eff == 0 {
			res = dst
		} else {
			combined := dst | (cfIn << width)
			combined = (combined << eff) | (combined >> ((width + 1) - eff))
			res = combined & mask(operandSize)
			newCF = (combined>>width)&1 != 0
		}
		c.setCF(newCF)
		if count == 1 {
			hi := res & signBit(operandSize)
			c.setOF((hi != 0) != newCF)
		}
		c.writeOperand(m, res, operandSize)
		return nil
	case 3: // RCR — rotate-through-carry right (width+1 bits)
		eff := count % (width + 1)
		cfIn := uint64(0)
		if c.rflags&RFLAGS_CF != 0 {
			cfIn = 1
		}
		if count == 1 {
			// OF for count==1 = MSB(dst) XOR CF (PRE-rotation).
			hi := dst & signBit(operandSize)
			c.setOF((hi != 0) != (cfIn != 0))
		}
		newCF := cfIn != 0
		if eff != 0 {
			combined := dst | (cfIn << width)
			combined = (combined >> eff) | (combined << ((width + 1) - eff))
			res = combined & mask(operandSize)
			newCF = (combined>>width)&1 != 0
		} else {
			res = dst
		}
		c.setCF(newCF)
		c.writeOperand(m, res, operandSize)
		return nil
	case 4, 6: // SHL / SAL (alias)
		// CF = bit shifted out of the high end on the last shift.
		// Pre-pad dst to 64 bits, shift, then mask. The shifted-out
		// bit is bit (size*8 - 1) of (dst << (count-1)).
		preShift := dst << (count - 1)
		fl.cf = preShift&signBit(operandSize) != 0
		res = (dst << count) & mask(operandSize)
		// OF (count==1): set if sign bit changed.
		if count == 1 {
			origSign := dst & signBit(operandSize)
			newSign := res & signBit(operandSize)
			fl.of = origSign != newSign
		}
	case 5: // SHR
		res = dst >> count
		fl.cf = (dst>>(count-1))&1 != 0
		if count == 1 {
			// OF for SHR-1 = high bit of original.
			fl.of = dst&signBit(operandSize) != 0
		}
	case 7: // SAR
		// Arithmetic right shift: sign-extend.
		signed := int64(dst)
		// Re-sign-extend from operandSize to 64 first.
		switch operandSize {
		case 4:
			signed = int64(int32(uint32(dst)))
		case 2:
			signed = int64(int16(uint16(dst)))
		case 1:
			signed = int64(int8(uint8(dst)))
		}
		fl.cf = uint64(signed>>(count-1))&1 != 0
		res = uint64(signed>>count) & mask(operandSize)
		// OF for SAR-1 = 0 (sign bit can't change).
	default:
		return unimplemented("Group 2 /%d", m.reg)
	}
	fl.zf = res == 0
	fl.sf = res&signBit(operandSize) != 0
	fl.pf = parity8(uint8(res))
	c.writeOperand(m, res, operandSize)
	c.setArithFlags(fl)
	return nil
}

// opTEST implements 0x84 (byte) and 0x85 (word/dword/qword) — TEST
// r/m, r. Routes through aluTEST so the byte form gets the same
// REX-aware 8-bit register handling as the other ALU byte forms.
func (c *CPU) opTEST(rex, operandSize uint8) error {
	return c.opALURM(rex, operandSize, aluTEST)
}

// opGroup1 dispatches 0x80/0x81/0x83. imm8 (true) reads a signed-8-bit
// immediate and sign-extends to operandSize; otherwise reads imm16 (for
// operandSize=2) or imm32 (for 4/8, sign-extending to 8). The sub-op
// (0..7) lives in ModR/M.reg. operandSize=1 (the 0x80 byte form) uses
// the REX-aware 8-bit register encoding for r/m.
func (c *CPU) opGroup1(rex, operandSize uint8, imm8 bool) error {
	// Compute the trailing-immediate size so parseModRM64WithImm can
	// adjust RIP-relative effective addresses by the right amount.
	var immBytes uint8
	if imm8 {
		immBytes = 1
	} else if operandSize == 1 {
		immBytes = 1
	} else if operandSize == 2 {
		immBytes = 2
	} else {
		immBytes = 4
	}
	m := c.parseModRM64WithImm(rex, immBytes)
	var imm uint64
	if imm8 {
		if operandSize == 1 {
			imm = uint64(c.fetch8()) // byte form: no sign-extend needed
		} else {
			imm = uint64(int64(int8(c.fetch8())))
		}
	} else {
		switch operandSize {
		case 2:
			imm = uint64(int64(int16(c.fetch16())))
		default: // 4 or 8 — imm32, sign-extend to 64 in the 8 case
			imm = uint64(int64(int32(c.fetch32())))
		}
	}
	var dst uint64
	if operandSize == 1 {
		if m.isReg {
			dst = uint64(c.read8FromModRM(m))
		} else {
			dst = uint64(c.readMem8(c.segBaseForModRM(m) + m.ea))
		}
	} else {
		dst = c.readOperand(m, operandSize)
	}
	var op aluOp
	switch m.reg {
	case 0:
		op = aluADD
	case 1:
		op = aluOR
	case 2:
		op = aluADC
	case 3:
		op = aluSBB
	case 4:
		op = aluAND
	case 5:
		op = aluSUB
	case 6:
		op = aluXOR
	case 7:
		op = aluCMP
	}
	res, fl := c.aluApplyWithCF(op, dst, imm, operandSize)
	if aluWritesBack(op) {
		if operandSize == 1 {
			if m.isReg {
				c.write8FromModRM(m, uint8(res))
			} else {
				c.writeMem8(c.segBaseForModRM(m) + m.ea, uint8(res))
			}
		} else {
			c.writeOperand(m, res, operandSize)
		}
	}
	c.setArithFlags(fl)
	return nil
}

// opGroup5 dispatches 0xFF. Sub-ops: 0=INC, 1=DEC, 2=CALL, 3=CALLF,
// 4=JMP, 5=JMPF, 6=PUSH, 7=reserved. M2 wires the data ops; CALL/JMP
// indirect arrive when control flow gets more interesting.
// opGroup4 implements 0xFE — the byte-form INC/DEC family (/0 and /1
// only; /2..7 are illegal). Mirrors Group 5's INC/DEC handling but
// with operandSize fixed at 1, which matters for read8FromModRM /
// write8FromModRM's AH/CH/DH/BH-vs-SPL/BPL/SIL/DIL split.
func (c *CPU) opGroup4(rex uint8) error {
	m := c.parseModRM64(rex)
	dst := uint64(c.readOperand(m, 1))
	switch m.reg {
	case 0: // INC r/m8
		res, fl := add(dst, 1, 1)
		c.writeOperand(m, res, 1)
		oldCF := c.rflags & RFLAGS_CF
		c.setArithFlags(fl)
		c.rflags = (c.rflags &^ RFLAGS_CF) | oldCF
		return nil
	case 1: // DEC r/m8
		res, fl := sub(dst, 1, 1)
		c.writeOperand(m, res, 1)
		oldCF := c.rflags & RFLAGS_CF
		c.setArithFlags(fl)
		c.rflags = (c.rflags &^ RFLAGS_CF) | oldCF
		return nil
	}
	return c.unimplementedAt("Group 4 /%d", m.reg)
}

func (c *CPU) opGroup5(rex, operandSize uint8) error {
	m := c.parseModRM64(rex)
	dst := c.readOperand(m, operandSize)
	switch m.reg {
	case 0: // INC r/m
		res, fl := add(dst, 1, operandSize)
		c.writeOperand(m, res, operandSize)
		// INC/DEC preserve CF (Intel SDM); only OF/SF/ZF/AF/PF are
		// touched. Read the current CF and re-OR it after setArithFlags.
		oldCF := c.rflags & RFLAGS_CF
		c.setArithFlags(fl)
		c.rflags = (c.rflags &^ RFLAGS_CF) | oldCF
		return nil
	case 1: // DEC r/m
		res, fl := sub(dst, 1, operandSize)
		c.writeOperand(m, res, operandSize)
		oldCF := c.rflags & RFLAGS_CF
		c.setArithFlags(fl)
		c.rflags = (c.rflags &^ RFLAGS_CF) | oldCF
		return nil
	case 2: // CALL r/m (near, absolute indirect)
		// Long-mode default operand size for indirect CALL is 64 bits
		// regardless of operand-size prefix. Push the return address
		// (current RIP, already advanced past the instruction by
		// parseModRM64) and jump.
		target := c.readOperand(m, 8)
		c.push64(c.rip)
		c.rip = target
		return nil
	case 4: // JMP r/m (near, absolute indirect)
		target := c.readOperand(m, 8)
		c.rip = target
		return nil
	case 6: // PUSH r/m
		c.push64(c.readOperand(m, 8))
		return nil
	}
	return c.unimplementedAt("Group 5 /%d", m.reg)
}

// opMOVImm implements 0xC7 /0 — MOV r/m, imm. In 64-bit operand mode
// the immediate is 32 bits, sign-extended to 64.
func (c *CPU) opMOVImm(rex, operandSize uint8) error {
	// 0xC7 in 64-bit mode takes a 32-bit immediate (sign-extended to
	// 64); 32/16/8-bit forms take matching immediate widths. The size
	// must be passed to parseModRM64WithImm so RIP-relative effective
	// addresses account for the trailing immediate (Intel SDM Vol 2
	// §2.2.1.6).
	immBytes := operandSize
	if operandSize == 8 {
		immBytes = 4
	}
	m := c.parseModRM64WithImm(rex, immBytes)
	if m.reg != 0 {
		return unimplemented("Group 11 /%d (only /0 = MOV)", m.reg)
	}
	var v uint64
	switch operandSize {
	case 8:
		v = uint64(int64(int32(c.fetch32())))
	case 4:
		v = uint64(c.fetch32())
	case 2:
		v = uint64(c.fetch16())
	case 1:
		v = uint64(c.fetch8())
	}
	c.writeOperand(m, v, operandSize)
	return nil
}

// opMOVImmToReg implements 0xB8+rd. REX.W → 64-bit imm; else 32-bit
// imm zero-extends to 64 (the long-mode rule); 16-bit form preserves
// the upper 48 bits.
func (c *CPU) opMOVImmToReg(rd, rex, operandSize uint8) error {
	idx := uint8(rd)
	if rex&rexB != 0 {
		idx |= 0x8
	}
	switch operandSize {
	case 8:
		c.reg64[idx] = c.fetch64()
	case 4:
		c.reg64[idx] = uint64(c.fetch32())
	case 2:
		c.reg64[idx] = (c.reg64[idx] & ^uint64(0xFFFF)) | uint64(c.fetch16())
	}
	return nil
}

func (c *CPU) opMOVRM(rex, operandSize uint8) error {
	m := c.parseModRM64(rex)
	src := c.readReg(m.reg, operandSize)
	c.writeOperand(m, src, operandSize)
	return nil
}

func (c *CPU) opMOVRfromM(rex, operandSize uint8) error {
	m := c.parseModRM64(rex)
	val := c.readOperand(m, operandSize)
	c.writeReg(m.reg, val, operandSize)
	return nil
}

// opMOVRM8 implements 0x88 — MOV r/m8, r8. ModR/M.reg picks the
// source 8-bit register; rm picks the destination. read8FromModRM /
// write8FromModRM honour the no-REX vs REX-present split for
// AH/CH/DH/BH vs SPL/BPL/SIL/DIL.
func (c *CPU) opMOVRM8(rex uint8) error {
	m := c.parseModRM64(rex)
	// Build a "reg-as-rm" marker so read8FromModRM can apply the
	// same rule to the reg field. The reg field has REX.R applied
	// in parseModRM64.
	src := c.read8RegField(m)
	if m.isReg {
		c.write8FromModRM(m, src)
	} else {
		c.writeMem8(c.segBaseForModRM(m) + m.ea, src)
	}
	return nil
}

// opMOVRfromM8 implements 0x8A — MOV r8, r/m8.
func (c *CPU) opMOVRfromM8(rex uint8) error {
	m := c.parseModRM64(rex)
	var src uint8
	if m.isReg {
		src = c.read8FromModRM(m)
	} else {
		src = c.readMem8(c.segBaseForModRM(m) + m.ea)
	}
	c.write8RegField(m, src)
	return nil
}

// read8RegField / write8RegField apply the REX-aware 8-bit register
// encoding to ModR/M.reg (rather than rm). reg already had REX.R
// applied in parseModRM64.
func (c *CPU) read8RegField(m modRMResult) uint8 {
	if m.hasREX || m.reg < 4 {
		return uint8(c.reg64[m.reg&0xF])
	}
	return uint8(c.reg64[m.reg-4] >> 8) // AH/CH/DH/BH
}

func (c *CPU) write8RegField(m modRMResult, v uint8) {
	if m.hasREX || m.reg < 4 {
		i := m.reg & 0xF
		c.reg64[i] = (c.reg64[i] & ^uint64(0xFF)) | uint64(v)
		return
	}
	i := m.reg - 4
	c.reg64[i] = (c.reg64[i] & ^uint64(0xFF00)) | (uint64(v) << 8)
}

// opMOVImm8ToReg implements 0xB0+rb — MOV r8, imm8. The low three
// bits of opcode pick the destination; REX.B extends to R8B..R15B.
// Without REX, rb 4..7 means AH/CH/DH/BH (the high-byte aliases).
func (c *CPU) opMOVImm8ToReg(rb, rex uint8) error {
	imm := c.fetch8()
	if rex != 0 || rb < 4 {
		idx := rb
		if rex&rexB != 0 {
			idx |= 0x8
		}
		c.reg64[idx] = (c.reg64[idx] & ^uint64(0xFF)) | uint64(imm)
		return nil
	}
	// No-REX, high-byte alias: AH=4, CH=5, DH=6, BH=7 → high byte of
	// reg64[rb-4].
	i := rb - 4
	c.reg64[i] = (c.reg64[i] & ^uint64(0xFF00)) | (uint64(imm) << 8)
	return nil
}

// opMOVtoSreg implements 0x8E — load a segment register from r/m16.
// In long mode CS/DS/ES/SS bases are architecturally forced to 0;
// FS/GS bases come from the FS_BASE/GS_BASE MSRs (set via WRMSR), so
// this stores the selector and leaves the cached base untouched
// except for forcing zero on the four flat segments. A real CPU
// walks the GDT to fault on bad selectors; M-series skip that check.
func (c *CPU) opMOVtoSreg(rex uint8) error {
	m := c.parseModRM64(rex)
	if m.reg > 5 {
		return unimplemented("MOV to invalid Sreg index %d", m.reg)
	}
	sel := uint16(c.readOperand(m, 2))
	idx := int(m.reg)
	c.seg[idx] = sel
	switch idx {
	case CS, DS, ES, SS:
		// Long-mode forces flat segments: base 0, limit 4 GiB. Don't
		// disturb the access cache (the boot harness or a far jump
		// set L/D appropriately).
		c.segBase[idx] = 0
		c.segLimit[idx] = 0xFFFFFFFF
	}
	if idx == CS {
		c.recomputeMode()
	}
	return nil
}

// opMOVfromSreg implements 0x8C — store a segment-register selector
// to r/m16. Destination width is always 16 bits.
func (c *CPU) opMOVfromSreg(rex uint8) error {
	m := c.parseModRM64(rex)
	if m.reg > 5 {
		return unimplemented("MOV from invalid Sreg index %d", m.reg)
	}
	sel := uint64(c.seg[m.reg])
	c.writeOperand(m, sel, 2)
	return nil
}

func (c *CPU) opLEA(rex, operandSize uint8) error {
	m := c.parseModRM64(rex)
	if m.isReg {
		return unimplemented("LEA with register source")
	}
	c.writeReg(m.reg, m.ea, operandSize)
	return nil
}

// opINEAX / opOUTEAX handle the 16- and 32-bit forms of IN/OUT.
// In long mode the operand size for these instructions never grows
// to 64 bits (no REX.W effect). 0x66 prefix selects 16-bit; default
// is 32-bit. The CPU's I/O handlers, set by machine/pc, dispatch to
// the appropriate device.
func (c *CPU) opINEAX(port uint16, operandSize uint8) error {
	switch operandSize {
	case 2:
		c.SetReg16(AX, c.ioRead16(port))
	default:
		c.SetReg32(EAX, c.ioRead32(port))
	}
	return nil
}

func (c *CPU) opOUTEAX(port uint16, operandSize uint8) error {
	switch operandSize {
	case 2:
		c.ioWrite16(port, c.GetReg16(AX))
	default:
		c.ioWrite32(port, c.GetReg32(EAX))
	}
	return nil
}

func (c *CPU) ioRead8(port uint16) uint8 {
	if ioTrace {
		fmt.Fprintf(os.Stderr, "[io] in8  port=%#x RIP=%#x\n", port, c.rip)
	}
	if c.ioRead8Func == nil {
		return 0xFF
	}
	return c.ioRead8Func(port)
}
func (c *CPU) ioRead16(port uint16) uint16 {
	if ioTrace {
		fmt.Fprintf(os.Stderr, "[io] in16 port=%#x RIP=%#x\n", port, c.rip)
	}
	if c.ioRead16Func == nil {
		return 0xFFFF
	}
	return c.ioRead16Func(port)
}
func (c *CPU) ioRead32(port uint16) uint32 {
	if ioTrace {
		fmt.Fprintf(os.Stderr, "[io] in32 port=%#x RIP=%#x\n", port, c.rip)
	}
	if c.ioRead32Func == nil {
		return 0xFFFFFFFF
	}
	return c.ioRead32Func(port)
}
func (c *CPU) ioWrite8(port uint16, v uint8) {
	if ioTrace {
		fmt.Fprintf(os.Stderr, "[io] out8 port=%#x val=%#x RIP=%#x\n", port, v, c.rip)
	}
	if c.ioWrite8Func != nil {
		c.ioWrite8Func(port, v)
	}
}
func (c *CPU) ioWrite16(port uint16, v uint16) {
	if ioTrace {
		fmt.Fprintf(os.Stderr, "[io] out16 port=%#x val=%#x RIP=%#x\n", port, v, c.rip)
	}
	if c.ioWrite16Func != nil {
		c.ioWrite16Func(port, v)
	}
}
func (c *CPU) ioWrite32(port uint16, v uint32) {
	if ioTrace {
		fmt.Fprintf(os.Stderr, "[io] out32 port=%#x val=%#x RIP=%#x\n", port, v, c.rip)
	}
	if c.ioWrite32Func != nil {
		c.ioWrite32Func(port, v)
	}
}

func (c *CPU) opPUSHReg(rd, rex uint8) error {
	idx := uint8(rd)
	if rex&rexB != 0 {
		idx |= 0x8
	}
	c.push64(c.reg64[idx])
	return nil
}

func (c *CPU) opPOPReg(rd, rex uint8) error {
	idx := uint8(rd)
	if rex&rexB != 0 {
		idx |= 0x8
	}
	c.reg64[idx] = c.pop64()
	return nil
}

// ===== Operand helpers =====

func (c *CPU) readReg(idx, size uint8) uint64 {
	v := c.reg64[idx&0xF]
	switch size {
	case 8:
		return v
	case 4:
		return v & 0xFFFFFFFF
	case 2:
		return v & 0xFFFF
	case 1:
		// Width-1 access via this path is only legal when the caller
		// has already resolved AH/CH/DH/BH vs SPL/.../R15B for itself —
		// i.e. when REX was present on the instruction. Otherwise the
		// caller must use read8FromModRM with the rex byte. We do the
		// "REX-only low-byte" case here.
		return v & 0xFF
	}
	return v
}

func (c *CPU) writeReg(idx uint8, v uint64, size uint8) {
	i := idx & 0xF
	switch size {
	case 8:
		c.reg64[i] = v
	case 4:
		c.reg64[i] = v & 0xFFFFFFFF
	case 2:
		c.reg64[i] = (c.reg64[i] & ^uint64(0xFFFF)) | (v & 0xFFFF)
	case 1:
		c.reg64[i] = (c.reg64[i] & ^uint64(0xFF)) | (v & 0xFF)
	}
}

// read8FromModRM reads an 8-bit register value where the encoding may
// be the "no-REX" form (rm 4..7 are AH/CH/DH/BH) or the "REX present"
// form (rm 4..7 are SPL/BPL/SIL/DIL). modRMResult.hasREX picks.
//
// rmRaw is the ModRM.rm field with REX.B applied — i.e. m.rm.
func (c *CPU) read8FromModRM(m modRMResult) uint8 {
	if m.hasREX || m.rm < 4 {
		return uint8(c.reg64[m.rm&0xF])
	}
	// No-REX, rm in 4..7: AH/CH/DH/BH = high byte of reg64[rm-4].
	return uint8(c.reg64[m.rm-4] >> 8)
}

func (c *CPU) write8FromModRM(m modRMResult, v uint8) {
	if m.hasREX || m.rm < 4 {
		i := m.rm & 0xF
		c.reg64[i] = (c.reg64[i] & ^uint64(0xFF)) | uint64(v)
		return
	}
	i := m.rm - 4
	c.reg64[i] = (c.reg64[i] & ^uint64(0xFF00)) | (uint64(v) << 8)
}

func (c *CPU) readOperand(m modRMResult, size uint8) uint64 {
	if m.isReg {
		if size == 1 {
			return uint64(c.read8FromModRM(m))
		}
		return c.readReg(m.rm, size)
	}
	ea := c.segBaseForModRM(m) + m.ea
	switch size {
	case 8:
		return c.readMem64(ea)
	case 4:
		return uint64(c.readMem32(ea))
	case 2:
		return uint64(c.readMem16(ea))
	}
	return uint64(c.readMem8(ea))
}

func (c *CPU) writeOperand(m modRMResult, v uint64, size uint8) {
	if m.isReg {
		if size == 1 {
			c.write8FromModRM(m, uint8(v))
			return
		}
		c.writeReg(m.rm, v, size)
		return
	}
	ea := c.segBaseForModRM(m) + m.ea
	switch size {
	case 8:
		c.writeMem64(ea, v)
	case 4:
		c.writeMem32(ea, uint32(v))
	case 2:
		c.writeMem16(ea, uint16(v))
	case 1:
		c.writeMem8(ea, uint8(v))
	}
}

// segBaseForModRM returns the segment base that should be added to a
// memory operand's effective address. If a segment-override prefix is
// active (CS/SS/DS/ES/FS/GS) it wins; otherwise the encoding's
// default (DS for most, SS for RSP/RBP-based addressing) is used.
//
// In long mode CS/DS/ES/SS are forced to base 0 by the architecture,
// so the only segments whose base actually matters here are FS and
// GS — which the kernel uses extensively for per-CPU storage via
// WRMSR(MSR_FS_BASE) / WRMSR(MSR_GS_BASE). Dropping the segment
// override (the previous behaviour) silently misdirected every
// `mov gs:[rip+disp32], reg` to (rip+disp32) instead of
// (gs_base + rip + disp32) — every per-CPU access landed in random
// kernel-image memory. That was the root cause of the Linux 6.6
// boot fault at x86_64_start_kernel+0xC.
func (c *CPU) segBaseForModRM(m modRMResult) uint64 {
	seg := m.defaultSeg
	if c.currentSegOverride >= 0 {
		seg = c.currentSegOverride
	}
	// In long mode, only FS and GS have non-zero bases on real
	// hardware. Skip the lookup for the others — both saves a few
	// cycles per access and stays correct even if the guest leaves
	// garbage in segBase[CS/DS/ES/SS] from an earlier mode.
	if c.mode == ModeLong64 && seg != FS && seg != GS {
		return 0
	}
	return c.segBase[seg]
}

// push64 implements the architectural PUSH: the write at [new RSP]
// happens BEFORE RSP commits, so a #PF on the store (lazily-allocated
// stack page) leaves RSP unchanged. Without this, Step's deferred
// recover rewinds c.rip for kernel #PF delivery, the kernel handles
// the PF, IRETQ returns to the same CALL/PUSH, and the re-executed
// push subtracts ANOTHER 8 from RSP — silently corrupting the stack
// frame by 8 bytes for every PF-during-push. (Symptom: fork-wrapper's
// epilogue pops from the wrong slot, RET reads the saved RBP slot,
// jumps to RBP's value, NX fault, init dies.)
func (c *CPU) push64(v uint64) {
	newRSP := c.reg64[RSP] - 8
	c.writeMem64(newRSP, v)
	c.reg64[RSP] = newRSP
}

// pop64 already reads before incrementing RSP, so a #PF on the load
// leaves RSP unchanged — the architecturally-correct behaviour.
func (c *CPU) pop64() uint64 {
	v := c.readMem64(c.reg64[RSP])
	c.reg64[RSP] += 8
	return v
}

// ===== ALU helpers =====

type flagBits struct {
	cf, pf, af, zf, sf, of bool
}

func mask(size uint8) uint64 {
	switch size {
	case 8:
		return 0xFFFFFFFF_FFFFFFFF
	case 4:
		return 0xFFFFFFFF
	case 2:
		return 0xFFFF
	}
	return 0xFF
}

func signBit(size uint8) uint64 {
	switch size {
	case 8:
		return 1 << 63
	case 4:
		return 1 << 31
	case 2:
		return 1 << 15
	}
	return 1 << 7
}

func add(a, b uint64, size uint8) (uint64, flagBits) {
	m := mask(size)
	a &= m
	b &= m
	r := (a + b) & m
	var fl flagBits
	fl.cf = (a + b) < a
	if size != 8 {
		fl.cf = (a+b)&(m+1) != 0
	}
	fl.af = ((a ^ b ^ r) & 0x10) != 0
	fl.zf = r == 0
	fl.sf = r&signBit(size) != 0
	fl.of = ((^(a ^ b)) & (a ^ r) & signBit(size)) != 0
	fl.pf = parity8(uint8(r))
	return r, fl
}

// addWithCarry: a + b + cf (cf ∈ {0,1}). CF and OF computed for the
// full triple-input add.
func addWithCarry(a, b, cf uint64, size uint8) (uint64, flagBits) {
	m := mask(size)
	a &= m
	b &= m
	r := (a + b + cf) & m
	var fl flagBits
	// Unsigned overflow: result of full add doesn't fit in size bits.
	if size == 8 {
		// 64-bit: check by comparing to one of the operands. The
		// triple add overflows when (a+b) overflows OR (a+b+cf)
		// produces a carry beyond the result we already saw.
		s1 := a + b
		fl.cf = s1 < a || (s1+cf) < s1
	} else {
		fl.cf = (a+b+cf)&(m+1) != 0
	}
	fl.af = ((a ^ b ^ r) & 0x10) != 0
	fl.zf = r == 0
	fl.sf = r&signBit(size) != 0
	// OF for ADC: same as ADD — set if both operands have the same
	// sign and result differs.
	fl.of = ((^(a ^ b)) & (a ^ r) & signBit(size)) != 0
	fl.pf = parity8(uint8(r))
	return r, fl
}

// subWithBorrow: a - b - cf.
func subWithBorrow(a, b, cf uint64, size uint8) (uint64, flagBits) {
	m := mask(size)
	a &= m
	b &= m
	r := (a - b - cf) & m
	var fl flagBits
	// CF is the borrow-out: true when a < b + cf.
	bPlusCF := b + cf
	fl.cf = a < bPlusCF || (b == m && cf == 1) // overflow guard for b+cf
	if size == 8 {
		// At width 8, the (b+cf) sum can overflow 64 bits; the
		// secondary clause covers that.
	} else {
		// Easier formulation at narrower widths: extend, subtract,
		// inspect the borrow bit.
		fl.cf = (a-b-cf)&(m+1) != 0
	}
	fl.af = ((a ^ b ^ r) & 0x10) != 0
	fl.zf = r == 0
	fl.sf = r&signBit(size) != 0
	fl.of = ((a ^ b) & (a ^ r) & signBit(size)) != 0
	fl.pf = parity8(uint8(r))
	return r, fl
}

func sub(a, b uint64, size uint8) (uint64, flagBits) {
	m := mask(size)
	a &= m
	b &= m
	r := (a - b) & m
	var fl flagBits
	fl.cf = a < b
	fl.af = ((a ^ b ^ r) & 0x10) != 0
	fl.zf = r == 0
	fl.sf = r&signBit(size) != 0
	fl.of = ((a ^ b) & (a ^ r) & signBit(size)) != 0
	fl.pf = parity8(uint8(r))
	return r, fl
}

// logicalFlags computes the flag profile for bitwise ops (AND, OR, XOR,
// TEST). Per Intel SDM: CF and OF are cleared; SF, ZF, PF follow the
// result; AF is undefined (we leave it zero).
func logicalFlags(r uint64, size uint8) flagBits {
	return flagBits{
		zf: r == 0,
		sf: r&signBit(size) != 0,
		pf: parity8(uint8(r)),
	}
}

func parity8(v uint8) bool {
	return bits.OnesCount8(v)%2 == 0
}

// setCF / setOF write a single flag bit in c.rflags without disturbing
// the others. Used by the rotate ops (ROL/ROR/RCL/RCR) which per Intel
// SDM only touch CF (and OF when count==1) — AF/PF/ZF/SF must survive.
func (c *CPU) setCF(v bool) {
	if v {
		c.rflags |= RFLAGS_CF
	} else {
		c.rflags &^= RFLAGS_CF
	}
}

func (c *CPU) setOF(v bool) {
	if v {
		c.rflags |= RFLAGS_OF
	} else {
		c.rflags &^= RFLAGS_OF
	}
}

func (c *CPU) setArithFlags(fl flagBits) {
	f := c.rflags
	f &^= RFLAGS_CF | RFLAGS_PF | RFLAGS_AF | RFLAGS_ZF | RFLAGS_SF | RFLAGS_OF
	if fl.cf {
		f |= RFLAGS_CF
	}
	if fl.pf {
		f |= RFLAGS_PF
	}
	if fl.af {
		f |= RFLAGS_AF
	}
	if fl.zf {
		f |= RFLAGS_ZF
	}
	if fl.sf {
		f |= RFLAGS_SF
	}
	if fl.of {
		f |= RFLAGS_OF
	}
	c.rflags = f | 2
}
