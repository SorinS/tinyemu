package x86_64

import (
	"fmt"
	"math"
	"math/bits"
	"math/rand/v2"
	"os"
	"time"
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

	switch op {
	// ===== Single-byte primary ops =====

	case 0x90, 0x91, 0x92, 0x93, 0x94, 0x95, 0x96, 0x97:
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

	case 0xF4:
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

	case 0x00:
		return c.opALURM(rex, 1, aluADD)
	case 0x01:
		return c.opALURM(rex, operandSize, aluADD)
	case 0x02:
		return c.opALURfromM(rex, 1, aluADD)
	case 0x03:
		return c.opALURfromM(rex, operandSize, aluADD)
	case 0x04:
		return c.opALUImmAL(aluADD)

	case 0x10:
		return c.opALURM(rex, 1, aluADC)
	case 0x11:
		return c.opALURM(rex, operandSize, aluADC)
	case 0x12:
		return c.opALURfromM(rex, 1, aluADC)
	case 0x13:
		return c.opALURfromM(rex, operandSize, aluADC)
	case 0x14:
		return c.opALUImmAL(aluADC)
	case 0x15:
		return c.opALUImmRAX(operandSize, aluADC)

	case 0x18:
		return c.opALURM(rex, 1, aluSBB)
	case 0x19:
		return c.opALURM(rex, operandSize, aluSBB)
	case 0x1A:
		return c.opALURfromM(rex, 1, aluSBB)
	case 0x1B:
		return c.opALURfromM(rex, operandSize, aluSBB)
	case 0x1C:
		return c.opALUImmAL(aluSBB)
	case 0x1D:
		return c.opALUImmRAX(operandSize, aluSBB)

	case 0x08:
		return c.opALURM(rex, 1, aluOR)
	case 0x09:
		return c.opALURM(rex, operandSize, aluOR)
	case 0x0A:
		return c.opALURfromM(rex, 1, aluOR)
	case 0x0B:
		return c.opALURfromM(rex, operandSize, aluOR)
	case 0x0C:
		return c.opALUImmAL(aluOR)

	case 0x20:
		return c.opALURM(rex, 1, aluAND)
	case 0x21:
		return c.opALURM(rex, operandSize, aluAND)
	case 0x22:
		return c.opALURfromM(rex, 1, aluAND)
	case 0x23:
		return c.opALURfromM(rex, operandSize, aluAND)
	case 0x24:
		return c.opALUImmAL(aluAND)

	case 0x28:
		return c.opALURM(rex, 1, aluSUB)
	case 0x29:
		return c.opALURM(rex, operandSize, aluSUB)
	case 0x2A:
		return c.opALURfromM(rex, 1, aluSUB)
	case 0x2B:
		return c.opALURfromM(rex, operandSize, aluSUB)
	case 0x2C:
		return c.opALUImmAL(aluSUB)

	case 0x30:
		return c.opALURM(rex, 1, aluXOR)
	case 0x31:
		return c.opALURM(rex, operandSize, aluXOR)
	case 0x32:
		return c.opALURfromM(rex, 1, aluXOR)
	case 0x33:
		return c.opALURfromM(rex, operandSize, aluXOR)
	case 0x34:
		return c.opALUImmAL(aluXOR)

	case 0x38:
		return c.opALURM(rex, 1, aluCMP)
	case 0x39:
		return c.opALURM(rex, operandSize, aluCMP)
	case 0x3A:
		return c.opALURfromM(rex, 1, aluCMP)
	case 0x3B:
		return c.opALURfromM(rex, operandSize, aluCMP)
	case 0x3C:
		return c.opALUImmAL(aluCMP)

	case 0x84: // TEST r/m8, r8
		return c.opTEST(rex, 1)
	case 0x85:
		return c.opTEST(rex, operandSize)
	case 0xA8: // TEST AL, imm8
		return c.opALUImmAL(aluTEST)
	case 0xA9:
		return c.opALUImmRAX(operandSize, aluTEST)

	// ALU rAX, imm forms (single-byte primary opcode + imm). The imm is
	// imm16 in operandSize=2 mode and imm32 sign-extended-to-64
	// otherwise. AL,imm8 byte forms (0x04/0x0C/...) are not implemented
	// — none of M2's tests need them.
	case 0x05:
		return c.opALUImmRAX(operandSize, aluADD)
	case 0x0D:
		return c.opALUImmRAX(operandSize, aluOR)
	case 0x25:
		return c.opALUImmRAX(operandSize, aluAND)
	case 0x2D:
		return c.opALUImmRAX(operandSize, aluSUB)
	case 0x35:
		return c.opALUImmRAX(operandSize, aluXOR)
	case 0x3D:
		return c.opALUImmRAX(operandSize, aluCMP)

	// ===== MOV family =====

	case 0x86:
		// XCHG r/m8, r8 — atomic swap (always atomic on real
		// hardware; in our single-CPU model it's just a swap).
		return c.opXCHGRM(rex, 1)
	case 0x87:
		return c.opXCHGRM(rex, operandSize)

	case 0x8F:
		// POP r/m. ModR/M.reg must be 0; the rest are reserved. Width
		// follows OPERAND SIZE (see pushPopOperandSize), not stack-slot
		// size: SeaBIOS's irqentry_extrastack uses `66 8F` (popl to
		// memory) in real mode, which must move 4 bytes. Using
		// stackSlotSize() popped only 2, misaligning the stack by 2 and
		// corrupting the very next `popl %ecx` (the IRQ handler pointer).
		m := c.parseModRM64(rex)
		if m.reg != 0 {
			return unimplemented("0x8F /%d (reserved)", m.reg)
		}
		size := uint8(c.pushPopOperandSize(operandSize))
		v := c.popStack(int(size))
		c.writeOperand(m, v, size)
		return nil

	case 0xC2:
		// RET imm16 — pops return at OPERAND SIZE (same rule as 0xC3),
		// then frees imm16 bytes of stack arguments. Using stackSlotSize
		// for the return pop would drop the 0x66 promotion in real mode.
		imm := uint64(c.fetch16())
		retSize := int(operandSize)
		if c.mode == ModeLong64 && operandSize == 4 {
			retSize = 8
		}
		c.rip = c.popStack(retSize)
		c.SetReg64(RSP, c.GetReg64(RSP)+imm)
		return nil

	case 0xE0, 0xE1, 0xE2:
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
	case 0xE3:
		// JCXZ / JRCXZ — branch if RCX is zero (no decrement).
		disp := int64(int8(c.fetch8()))
		if c.GetReg64(RCX) == 0 {
			c.rip = uint64(int64(c.rip) + disp)
		}
		return nil

	case 0x88:
		// MOV r/m8, r8 — byte form. Source is the 8-bit register
		// picked by ModR/M.reg with REX-aware decoding (read8FromModRM-
		// style indexing).
		return c.opMOVRM8(rex)
	case 0x8A:
		// MOV r8, r/m8.
		return c.opMOVRfromM8(rex)

	case 0x89:
		return c.opMOVRM(rex, operandSize)
	case 0x8B:
		return c.opMOVRfromM(rex, operandSize)

	case 0xb0, 0xb1, 0xb2, 0xb3, 0xb4, 0xb5, 0xb6, 0xb7:
		// MOV r8, imm8 — REX-aware: the destination encoding follows
		// the same AH/CH/DH/BH vs SPL/BPL/SIL/DIL rule as MODR/M for
		// the low 3 bits.
		return c.opMOVImm8ToReg(op-0xB0, rex)
	case 0x8C:
		// MOV r/m16, Sreg — store a segment-register selector. ModR/M
		// reg field picks the segment (0=ES, 1=CS, 2=SS, 3=DS, 4=FS,
		// 5=GS).
		return c.opMOVfromSreg(rex)

	case 0x8D:
		return c.opLEA(rex, operandSize)

	case 0x8E:
		// MOV Sreg, r/m16 — load a segment-register selector. The
		// source is always 16 bits regardless of operand-size prefix.
		return c.opMOVtoSreg(rex)

	case 0x6C, 0x6D, 0x6E, 0x6F:
		// String IO:
		//   0x6C  INSB                 IN AL,  DX ; MOV [ES:(E)DI], AL ; (E)DI+=±1
		//   0x6D  INSW/INSD            IN ?X,  DX ; MOV [ES:(E)DI], ?X ; advance
		//   0x6E  OUTSB                MOV AL, [DS:(E)SI] ; OUT DX, AL ; (E)SI+=±1
		//   0x6F  OUTSW/OUTSD          MOV ?X, [DS:(E)SI] ; OUT DX, ?X ; advance
		// Element size = operandSize for 6D/6F; always 1 for 6C/6E.
		// REP prefix (F3) iterates RCX times. DF in RFLAGS picks
		// direction. (E)SI/(E)DI mask follows addressSize.
		size := operandSize
		if op == 0x6C || op == 0x6E {
			size = 1
		}
		port := uint16(c.GetReg64(RDX))
		step := int64(size)
		if c.rflags&RFLAGS_DF != 0 {
			step = -step
		}
		// Iteration count. REP runs until RCX hits 0; non-REP runs once.
		iter := uint64(1)
		if repPrefix != 0 {
			iter = c.GetReg64(RCX) & ((uint64(1) << (uint64(addressSize) * 8)) - 1)
		}
		isIn := op == 0x6C || op == 0x6D
		for i := uint64(0); i < iter; i++ {
			if isIn {
				// Read from IO port, write to ES:DI.
				di := c.GetReg64(RDI) & ((uint64(1) << (uint64(addressSize) * 8)) - 1)
				addr := c.segBase[ES] + di
				switch size {
				case 1:
					c.writeMem8(addr, uint8(c.ioRead8(port)))
				case 2:
					c.writeMem16(addr, uint16(c.ioRead16(port)))
				case 4:
					c.writeMem32(addr, c.ioRead32(port))
				}
				c.SetReg64(RDI, uint64(int64(di)+step))
			} else {
				// Read from DS:SI (segment override-aware), write to IO port.
				seg := DS
				if c.currentSegOverride >= 0 {
					seg = c.currentSegOverride
				}
				si := c.GetReg64(RSI) & ((uint64(1) << (uint64(addressSize) * 8)) - 1)
				addr := c.segBase[seg] + si
				switch size {
				case 1:
					c.ioWrite8(port, c.readMem8(addr))
				case 2:
					c.ioWrite16(port, c.readMem16(addr))
				case 4:
					c.ioWrite32(port, c.readMem32(addr))
				}
				c.SetReg64(RSI, uint64(int64(si)+step))
			}
		}
		if repPrefix != 0 {
			c.SetReg64(RCX, 0)
		}
		return nil

	case 0xA0, 0xA1, 0xA2, 0xA3:
		// MOV AL/AX/EAX/RAX, moffs (direct memory addressing). The
		// offset is fetched at address-size width (2/4/8 bytes); the
		// data transfer is at operand size (A0/A2 = byte; A1/A3 =
		// operandSize). A0/A1 = load from moffs; A2/A3 = store to moffs.
		var off uint64
		switch addressSize {
		case 2:
			off = uint64(c.fetch16())
		case 4:
			off = uint64(c.fetch32())
		case 8:
			off = c.fetch64()
		}
		// moffs uses the DS segment by default (unless overridden by
		// segment-override prefix), which is what segBaseForModRM-style
		// access expects: just resolve the segment manually here.
		seg := DS
		if c.currentSegOverride >= 0 {
			seg = c.currentSegOverride
		}
		linear := c.segBase[seg] + off
		switch op {
		case 0xA0: // MOV AL, [moffs8]
			c.SetReg8(AL, c.readMem8(linear))
		case 0xA1: // MOV AX/EAX/RAX, [moffs]
			switch operandSize {
			case 2:
				c.SetReg16(AX, c.readMem16(linear))
			case 4:
				c.reg64[RAX] = uint64(c.readMem32(linear)) // zero-ext high
			case 8:
				c.SetReg64(RAX, c.readMem64(linear))
			}
		case 0xA2: // MOV [moffs8], AL
			c.writeMem8(linear, c.GetReg8(AL))
		case 0xA3: // MOV [moffs], AX/EAX/RAX
			switch operandSize {
			case 2:
				c.writeMem16(linear, c.GetReg16(AX))
			case 4:
				c.writeMem32(linear, uint32(c.GetReg64(RAX)))
			case 8:
				c.writeMem64(linear, c.GetReg64(RAX))
			}
		}
		return nil

	case 0xB8, 0xB9, 0xBA, 0xBB, 0xBC, 0xBD, 0xBE, 0xBF:
		return c.opMOVImmToReg(op-0xB8, rex, operandSize)

	case 0xC6:
		return c.opMOVImm(rex, 1)
	case 0xC7:
		return c.opMOVImm(rex, operandSize)

	// ===== Group 1 (immediate) =====

	case 0x80:
		// Group 1 r/m8, imm8.
		return c.opGroup1(rex, 1, true)
	case 0x81:
		// Group 1 r/m, imm16/imm32 (sign-extended to 64).
		return c.opGroup1(rex, operandSize, false)
	case 0x82:
		// Same encoding as 0x83 in legacy mode; #UD in long mode per
		// Intel SDM. Surfacing explicitly.
		return unimplemented("0x82 (illegal in long mode)")
	case 0x83:
		// Group 1 r/m, imm8 (sign-extended).
		return c.opGroup1(rex, operandSize, true)

	// ===== Stack =====

	case 0x50, 0x51, 0x52, 0x53, 0x54, 0x55, 0x56, 0x57:
		return c.opPUSHReg(op-0x50, rex, operandSize)
	case 0x58, 0x59, 0x5a, 0x5b, 0x5c, 0x5d, 0x5e, 0x5f:
		return c.opPOPReg(op-0x58, rex, operandSize)

	// ===== Segment-register PUSH/POP (16/32-bit modes only; #UD in long mode) =====
	//
	// Long mode reuses these encodings: 0x06/0x07/0x0E/0x16/0x17/0x1E/0x1F
	// are all #UD in 64-bit code. In real / pm16 / pm32 / compat32 they're
	// PUSH/POP of an implicit segment register. The pushed/popped value
	// is the 16-bit selector, occupying a stack slot of the natural width
	// (8 long never reaches here / 4 pm32 / 2 pm16). High bits of the
	// stack slot are zero on PUSH, ignored on POP. Cannot POP CS — it's
	// 0x0E only.
	case 0x62:
		if !(c.mode != ModeLong64) {
			return c.unimplementedAt("opcode %#02x rex=%#x", op, rex)
		}
		// BOUND r16/r32, m. Verifies that an index is within bounds of
		// an array; if r < [m] or r > [m+size], raises #BR (vec 5).
		// #UD in long mode (that's why the long-mode TestDecode_
		// Unimplemented test still works — it asserts 0x06 unimplemented
		// in long mode, and 0x62 follows the same gating). In legacy
		// modes the instruction is rare but real; we parse the ModRM,
		// read both bounds, perform the check, and skip the #BR
		// delivery (we don't have a real-mode IDT delivery path yet,
		// and bounds violations should be vanishingly rare in actual
		// boot code — surfacing as a stub no-op is safer than the
		// "0x62 unimplemented" wall we kept hitting).
		m := c.parseModRM64(rex)
		if m.isReg {
			return c.raiseUD() // BOUND requires a memory operand
		}
		size := operandSize
		seg := m.defaultSeg
		if c.currentSegOverride >= 0 {
			seg = c.currentSegOverride
		}
		base := c.segBase[seg] + m.ea
		var lo, hi int64
		switch size {
		case 2:
			lo = int64(int16(c.readMem16(base)))
			hi = int64(int16(c.readMem16(base + 2)))
		case 4:
			lo = int64(int32(c.readMem32(base)))
			hi = int64(int32(c.readMem32(base + 4)))
		default:
			return c.unimplementedAt("BOUND with size %d", size)
		}
		idx := int64(int32(c.readReg(uint8(m.reg), size)))
		_, _, _ = idx, lo, hi // bounds check omitted: no #BR delivery in non-long mode yet
		return nil

	case 0x27, 0x2F, 0x37, 0x3F:
		if c.mode == ModeLong64 {
			return c.unimplementedAt("opcode %#02x rex=%#x", op, rex)
		}
		// BCD adjust instructions. All set AF + CF based on a nibble
		// check on AL, and conditionally adjust AL. #UD in long mode.
		//   0x27 DAA — decimal adjust AL after addition
		//   0x2F DAS — decimal adjust AL after subtraction
		//   0x37 AAA — ASCII adjust AL after addition
		//   0x3F AAS — ASCII adjust AL after subtraction
		al := c.GetReg8(AL)
		af := c.rflags&RFLAGS_AF != 0
		cf := c.rflags&RFLAGS_CF != 0
		switch op {
		case 0x27: // DAA
			oldAL := al
			if (al&0x0F) > 9 || af {
				al += 6
				c.rflags |= RFLAGS_AF
			} else {
				c.rflags &^= RFLAGS_AF
			}
			if oldAL > 0x99 || cf {
				al += 0x60
				c.rflags |= RFLAGS_CF
			} else {
				c.rflags &^= RFLAGS_CF
			}
		case 0x2F: // DAS
			oldAL := al
			if (al&0x0F) > 9 || af {
				al -= 6
				c.rflags |= RFLAGS_AF
			} else {
				c.rflags &^= RFLAGS_AF
			}
			if oldAL > 0x99 || cf {
				al -= 0x60
				c.rflags |= RFLAGS_CF
			} else {
				c.rflags &^= RFLAGS_CF
			}
		case 0x37: // AAA
			if (al&0x0F) > 9 || af {
				al = (al + 6) & 0x0F
				c.SetReg8(AH, c.GetReg8(AH)+1)
				c.rflags |= RFLAGS_AF | RFLAGS_CF
			} else {
				al &= 0x0F
				c.rflags &^= RFLAGS_AF | RFLAGS_CF
			}
		case 0x3F: // AAS
			if (al&0x0F) > 9 || af {
				al = (al - 6) & 0x0F
				c.SetReg8(AH, c.GetReg8(AH)-1)
				c.rflags |= RFLAGS_AF | RFLAGS_CF
			} else {
				al &= 0x0F
				c.rflags &^= RFLAGS_AF | RFLAGS_CF
			}
		}
		c.SetReg8(AL, al)
		// SF/ZF/PF set from final AL per Intel SDM (for DAA/DAS at least;
		// AAA/AAS set them "undefined" but real CPUs follow this pattern).
		c.rflags &^= RFLAGS_SF | RFLAGS_ZF | RFLAGS_PF
		if al == 0 {
			c.rflags |= RFLAGS_ZF
		}
		if al&0x80 != 0 {
			c.rflags |= RFLAGS_SF
		}
		if parity8(al) {
			c.rflags |= RFLAGS_PF
		}
		return nil

	case 0xD4, 0xD5:
		if c.mode == ModeLong64 {
			return c.unimplementedAt("opcode %#02x rex=%#x", op, rex)
		}
		// AAM (D4 ib) / AAD (D5 ib) — ASCII Adjust AX after Multiply /
		// before Divide. #UD in long mode. Both take an explicit imm8
		// base; the legacy assembler `aam` / `aad` mnemonic emits the
		// default 0x0A but the instruction encoding ALWAYS includes the
		// immediate byte.
		//   AAM: tempAL = AL; AH = tempAL / imm8; AL = tempAL mod imm8
		//   AAD: tempAL = AL + (AH * imm8); AL = tempAL & 0xFF; AH = 0
		// AAM with imm8=0 is #DE (divide error). SF/ZF/PF set from AL;
		// OF/AF/CF undefined (we leave them per the SDM "undefined" note).
		base := c.fetch8()
		if op == 0xD4 {
			if base == 0 {
				return c.deliverInterrupt(0, false, 0)
			}
			al := c.GetReg8(AL)
			c.SetReg8(AH, al/base)
			c.SetReg8(AL, al%base)
		} else {
			tmp := uint16(c.GetReg8(AL)) + uint16(c.GetReg8(AH))*uint16(base)
			c.SetReg8(AL, uint8(tmp))
			c.SetReg8(AH, 0)
		}
		al := c.GetReg8(AL)
		c.rflags &^= RFLAGS_SF | RFLAGS_ZF | RFLAGS_PF
		if al == 0 {
			c.rflags |= RFLAGS_ZF
		}
		if al&0x80 != 0 {
			c.rflags |= RFLAGS_SF
		}
		if parity8(al) {
			c.rflags |= RFLAGS_PF
		}
		return nil

	case 0xD6:
		if !(c.mode != ModeLong64) {
			return c.unimplementedAt("opcode %#02x rex=%#x", op, rex)
		}
		// SALC — undocumented "Set AL to -CF". Listed in many opcode
		// references (a.k.a. SETALC). Old SeaBIOS uses it for terse
		// flag-to-byte conversions. AL = CF ? 0xFF : 0x00. Flags
		// unchanged. #UD in long mode.
		if c.rflags&RFLAGS_CF != 0 {
			c.SetReg8(AL, 0xFF)
		} else {
			c.SetReg8(AL, 0x00)
		}
		return nil

	case 0xD7:
		// XLAT / XLATB — AL = [DS:eBX + zero_extended(AL)]. Address
		// width follows currentAddressSize; segment override allowed.
		// In long mode the base is RBX (full 64-bit).
		seg := DS
		if segOverride >= 0 {
			seg = segOverride
		}
		var base uint64
		switch c.currentAddressSize {
		case 2:
			base = uint64(c.GetReg16(BX))
		case 4:
			base = uint64(uint32(c.reg64[RBX]))
		case 8:
			base = c.reg64[RBX]
		default:
			base = c.reg64[RBX]
		}
		addr := c.segBase[seg] + base + uint64(c.GetReg8(AL))
		c.SetReg8(AL, c.readMem8(addr))
		return nil

	case 0x60, 0x61:
		if c.mode == ModeLong64 {
			return c.unimplementedAt("opcode %#02x rex=%#x", op, rex)
		}
		// PUSHA/POPA (16-bit) / PUSHAD/POPAD (32-bit). #UD in long
		// mode. Push order: EAX, ECX, EDX, EBX, ESP-original, EBP,
		// ESI, EDI. POPA mirror: EDI first, then ESI..EAX, but the
		// pushed ESP slot is DROPPED (not loaded back into ESP, per
		// Intel SDM). Width follows operand size: PUSHA/POPA (2) vs
		// PUSHAD/POPAD (4), flipped by the 0x66 prefix. #UD in long mode.
		size := c.pushPopOperandSize(operandSize)
		if op == 0x60 {
			origSP := c.reg64[RSP]
			c.pushStack(c.reg64[RAX], size)
			c.pushStack(c.reg64[RCX], size)
			c.pushStack(c.reg64[RDX], size)
			c.pushStack(c.reg64[RBX], size)
			c.pushStack(origSP, size)
			c.pushStack(c.reg64[RBP], size)
			c.pushStack(c.reg64[RSI], size)
			c.pushStack(c.reg64[RDI], size)
		} else {
			setLow := func(r int, v uint64) {
				if size == 4 {
					c.reg64[r] = v & 0xFFFFFFFF
				} else {
					c.reg64[r] = (c.reg64[r] & ^uint64(0xFFFF)) | (v & 0xFFFF)
				}
			}
			setLow(RDI, c.popStack(size))
			setLow(RSI, c.popStack(size))
			setLow(RBP, c.popStack(size))
			c.popStack(size) // drop saved-SP slot
			setLow(RBX, c.popStack(size))
			setLow(RDX, c.popStack(size))
			setLow(RCX, c.popStack(size))
			setLow(RAX, c.popStack(size))
		}
		return nil

	case 0x06, 0x07, 0x0E, 0x16, 0x17, 0x1E, 0x1F:
		if c.mode == ModeLong64 {
			return c.unimplementedAt("opcode %#02x rex=%#x", op, rex)
		}
		// PUSH/POP segment register. The 6 encodings here are #UD in
		// long mode; gating on mode preserves the long-mode test that
		// pins 0x06 as unimplemented while letting BIOS / real-mode
		// code use them.
		// PUSH/POP segment use operand size; segment selector is 16 bits
		// but the slot consumed/produced is full operand-size width
		// (zero-extended on push). Using stackSlotSize() in real-mode
		// 0x66-prefixed code silently mis-sized the slot — same bug class
		// as the PUSH r/m fix in Group 5 /6.
		size := c.pushPopOperandSize(operandSize)
		switch op {
		case 0x06: // PUSH ES
			c.pushStack(uint64(c.seg[ES]), size)
		case 0x07: // POP ES
			c.seg[ES] = uint16(c.popStack(size))
			if c.cr[0]&CR0_PE == 0 {
				c.segBase[ES] = uint64(c.seg[ES]) << 4
			}
		case 0x0E: // PUSH CS (no POP CS — only this direction is valid)
			c.pushStack(uint64(c.seg[CS]), size)
		case 0x16: // PUSH SS
			c.pushStack(uint64(c.seg[SS]), size)
		case 0x17: // POP SS
			c.seg[SS] = uint16(c.popStack(size))
			if c.cr[0]&CR0_PE == 0 {
				c.segBase[SS] = uint64(c.seg[SS]) << 4
			}
		case 0x1E: // PUSH DS
			c.pushStack(uint64(c.seg[DS]), size)
		case 0x1F: // POP DS
			c.seg[DS] = uint16(c.popStack(size))
			if c.cr[0]&CR0_PE == 0 {
				c.segBase[DS] = uint64(c.seg[DS]) << 4
			}
		}
		return nil

	// ===== INC reg / DEC reg (16/32-bit modes only; REX in long mode) =====
	//
	// Reachable only when c.mode != ModeLong64 because the prefix-loop
	// in Step() captures 0x40..0x4F as a REX prefix in long mode.
	case 0x40, 0x41, 0x42, 0x43, 0x44, 0x45, 0x46, 0x47:
		// INC r16/r32 — operand size keys off mode. CF preserved per
		// Intel SDM (only OF/SF/ZF/AF/PF touched).
		idx := op - 0x40
		v := c.readReg(idx, operandSize)
		res, fl := add(v, 1, operandSize)
		c.writeReg(idx, res, operandSize)
		oldCF := c.rflags & RFLAGS_CF
		c.setArithFlags(fl)
		c.rflags = (c.rflags &^ RFLAGS_CF) | oldCF
		return nil
	case 0x48, 0x49, 0x4a, 0x4b, 0x4c, 0x4d, 0x4e, 0x4f:
		// DEC r16/r32.
		idx := op - 0x48
		v := c.readReg(idx, operandSize)
		res, fl := sub(v, 1, operandSize)
		c.writeReg(idx, res, operandSize)
		oldCF := c.rflags & RFLAGS_CF
		c.setArithFlags(fl)
		c.rflags = (c.rflags &^ RFLAGS_CF) | oldCF
		return nil

	case 0x68:
		// PUSH imm32 (or imm16 with 0x66) sign-extended to operand size
		// and pushed at the corresponding width — see pushPopOperandSize
		// for the long-mode promotion (no 32-bit PUSH form in long mode).
		var v int64
		if operandSize == 2 {
			v = int64(int16(c.fetch16()))
		} else {
			v = int64(int32(c.fetch32()))
		}
		c.pushStack(uint64(v), c.pushPopOperandSize(operandSize))
		return nil

	case 0x6A:
		// PUSH imm8 sign-extended.
		v := int64(int8(c.fetch8()))
		c.pushStack(uint64(v), c.pushPopOperandSize(operandSize))
		return nil

	// ===== Control flow =====

	case 0xC3:
		// RET near. Per Intel SDM Vol 2A: the return-address width is
		// the OPERAND SIZE, not the stack-slot size. Default operand
		// size follows CS.D / mode, but the 0x66 prefix can flip it:
		//   pm16 / real default + no 0x66 -> 2 bytes
		//   pm16 / real with 0x66        -> 4 bytes
		//   pm32 default + no 0x66       -> 4 bytes
		//   pm32 with 0x66               -> 2 bytes
		//   long mode default            -> 8 bytes
		//   long mode with 0x66          -> 2 bytes
		// Long mode is special-cased because long-mode near-return
		// always uses an 8-byte return slot unless the 0x66 prefix
		// shrinks it to 16.
		retSize := int(operandSize)
		if c.mode == ModeLong64 && operandSize == 4 {
			retSize = 8
		}
		c.rip = c.popStack(retSize)
		return nil

	case 0xCB:
		// RETF / LRETQ — far return. Pops RIP and CS at operand-size
		// width. In long mode REX.W=1 picks 8-byte slots, else 4. CS
		// is loaded by reading its descriptor from the GDT, but we
		// keep the M5c shortcut: synthesize a 64-bit code segment for
		// the new CS. The kernel's startup_64 uses this to far-return
		// into its newly-loaded GDT's kernel CS, where L=1.
		return c.opRETF(operandSize)

	case 0xCC:
		// INT3 — software breakpoint, vector 3, no error code.
		return c.deliverInterrupt(3, false, 0)

	case 0xCD:
		// INT imm8.
		vec := c.fetch8()
		return c.deliverInterrupt(vec, false, 0)

	case 0xCE:
		if !(c.mode != ModeLong64) {
			return c.unimplementedAt("opcode %#02x rex=%#x", op, rex)
		}
		// INTO — overflow trap. Delivers vector 4 if OF=1, else NOP. #UD
		// in long mode (we let it fall through to unimplementedAt).
		if c.rflags&RFLAGS_OF != 0 {
			return c.deliverInterrupt(4, false, 0)
		}
		return nil

	case 0xCF:
		// IRET. In long mode the 64-bit form requires REX.W=1; the
		// 32-bit IRETD form (REX.W=0) is decoded but pops 32-bit values
		// — not yet implemented because nothing in our test surface
		// exercises it. Treat REX.W=0 as IRETQ for simplicity.
		return c.opIRETQ(operandSize)

	case 0xE4: // IN AL, imm8
		port := uint16(c.fetch8())
		c.SetReg8(AL, c.ioRead8(port))
		return nil
	case 0xE5: // IN eAX, imm8 — width follows operandSize
		port := uint16(c.fetch8())
		return c.opINEAX(port, operandSize)
	case 0xE6: // OUT imm8, AL
		port := uint16(c.fetch8())
		c.ioWrite8(port, c.GetReg8(AL))
		return nil
	case 0xE7: // OUT imm8, eAX
		port := uint16(c.fetch8())
		return c.opOUTEAX(port, operandSize)
	case 0xEC: // IN AL, DX
		port := c.GetReg16(DX)
		c.SetReg8(AL, c.ioRead8(port))
		return nil
	case 0xED: // IN eAX, DX
		port := c.GetReg16(DX)
		return c.opINEAX(port, operandSize)
	case 0xEE: // OUT DX, AL
		port := c.GetReg16(DX)
		c.ioWrite8(port, c.GetReg8(AL))
		return nil
	case 0xEF: // OUT DX, eAX
		port := c.GetReg16(DX)
		return c.opOUTEAX(port, operandSize)

	case 0xE8:
		// CALL rel — displacement size and return-address push size
		// both follow OPERAND SIZE (Intel SDM Vol 2A). Real mode uses
		// rel16 + 16-bit push by default; 0x66 flips to rel32 + 32-bit
		// push. Long mode forces 8-byte push (no 32-bit-CALL form),
		// with 0x66 shrinking to 2 bytes.
		var disp int64
		if operandSize == 2 {
			disp = int64(int16(c.fetch16()))
		} else {
			disp = int64(int32(c.fetch32()))
		}
		pushSize := int(operandSize)
		if c.mode == ModeLong64 && operandSize == 4 {
			pushSize = 8
		}
		c.pushStack(c.rip, pushSize)
		c.rip = uint64(int64(c.rip) + disp)
		return nil

	case 0xE9:
		// JMP rel — same operand-size dispatch as CALL rel.
		var disp int64
		if operandSize == 2 {
			disp = int64(int16(c.fetch16()))
		} else {
			disp = int64(int32(c.fetch32()))
		}
		c.rip = uint64(int64(c.rip) + disp)
		return nil

	case 0xEB:
		disp := int64(int8(c.fetch8()))
		c.rip = uint64(int64(c.rip) + disp)
		return nil

	case 0x9A:
		if !(c.mode != ModeLong64) {
			return c.unimplementedAt("opcode %#02x rex=%#x", op, rex)
		}
		// CALL FAR PTR16:16/32 — direct intersegment call. Sibling of
		// 0xEA JMP FAR; pushes (CS, RIP-of-next-insn) before the jump.
		// Width of the offset follows operandSize. #UD in long mode.
		var off uint64
		if operandSize == 2 {
			off = uint64(c.fetch16())
		} else {
			off = uint64(c.fetch32())
		}
		sel := c.fetch16()
		// CALL FAR pushes CS + return offset at operand-size width so a
		// matching RETF (also operand-size now) stays balanced.
		size := c.pushPopOperandSize(operandSize)
		c.pushStack(uint64(c.seg[CS]), size)
		c.pushStack(c.rip, size)
		// Reload CS in real mode (sel<<4 base, preserve cached limit/access)
		// or via GDT descriptor in protected mode — same logic as 0xEA.
		if c.cr[0]&CR0_PE == 0 {
			c.seg[CS] = sel
			c.segBase[CS] = uint64(sel) << 4
			c.recomputeMode()
			c.rip = off
			return nil
		}
		gdtBase := c.segBase[GDTR]
		desc, err := c.memMap.Read64(gdtBase + uint64(sel&0xFFF8))
		if err != nil {
			return fmt.Errorf("0x9A: read GDT entry %#x: %w", sel, err)
		}
		base := uint64(uint32((desc>>16)&0xFFFFFF)) | ((desc >> 56) << 24)
		access := uint8((desc >> 40) & 0xFF)
		flags := uint8((desc >> 52) & 0x0F)
		segAccess := uint32(access) | (uint32(flags) << 8)
		c.seg[CS] = sel
		c.segBase[CS] = base
		c.segLimit[CS] = 0xFFFFFFFF
		c.SetSegAccess(CS, segAccess)
		c.rip = off
		return nil

	case 0xEA:
		// JMP FAR PTR16:32 / FAR PTR16:16 — direct intersegment jump.
		// Invalid only when actually executing in 64-bit mode (CS.L=1);
		// valid in real, 16-bit protected, 32-bit protected, AND
		// compatibility mode (EFER.LMA=1 but CS.L=0). The compat case is
		// load-bearing: Linux's startup_32 sets CR0.PG (→ EFER.LMA=1)
		// then `ljmp $__KERNEL_CS, $startup_64` — a 0xEA executed with
		// LMA=1 but CS.L still 0 — and THAT jump is what loads the
		// 64-bit CS and enters long mode. Gating on EFER.LMA (instead of
		// the actual mode) wrongly #UD'd that ljmp and broke every
		// x86_64 Linux boot.
		if c.mode == ModeLong64 {
			return unimplemented("0xEA JMP FAR is invalid in 64-bit mode")
		}
		var off uint64
		if operandSize == 2 {
			off = uint64(c.fetch16())
		} else {
			off = uint64(c.fetch32())
		}
		sel := c.fetch16()
		// Real mode: CS.base is just sel<<4, no descriptor table is
		// involved (and any GDTR base is meaningless). The architectural
		// rule is that real-mode segment loads only rebuild .base from
		// sel<<4; .limit and .access are preserved from the cached
		// descriptor. Preserves big-real-mode if a prior PE excursion
		// extended CSlim.
		if c.cr[0]&CR0_PE == 0 {
			c.seg[CS] = sel
			c.segBase[CS] = uint64(sel) << 4
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

	case 0x70, 0x71, 0x72, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78, 0x79, 0x7a, 0x7b, 0x7c, 0x7d, 0x7e, 0x7f:
		// Conditional jump rel8.
		disp := int64(int8(c.fetch8()))
		if c.evalCond(op & 0xF) {
			c.rip = uint64(int64(c.rip) + disp)
		}
		return nil

	// ===== Group 3 (TEST/NOT/NEG/MUL/IMUL/DIV/IDIV) =====

	case 0xF6:
		// Group 3 byte form — TEST r/m8,imm8 / NOT / NEG / MUL / IMUL
		// / DIV / IDIV at 8-bit operand width.
		return c.opGroup3(rex, 1)
	case 0xF7:
		return c.opGroup3(rex, operandSize)

	// ===== IMUL signed-integer forms =====

	case 0x69:
		// IMUL r, r/m, imm32 (sign-extended to operand size).
		return c.opIMULImm(rex, operandSize, false)
	case 0x6B:
		// IMUL r, r/m, imm8 (sign-extended).
		return c.opIMULImm(rex, operandSize, true)

	// ===== Group 2 (shifts and rotates) =====

	case 0xD1:
		// SHL/SHR/SAR/ROL/ROR/RCL/RCR r/m, 1 — count is implicit.
		return c.opGroup2Reg(rex, operandSize, 1)
	case 0xD3:
		// Group 2 r/m, CL — count comes from CL register.
		return c.opGroup2Reg(rex, operandSize, uint64(c.GetReg8(CL)))
	case 0xC1:
		// Group 2 r/m, imm8 — count is an 8-bit immediate.
		return c.opGroup2Imm(rex, operandSize)
	case 0xD0:
		// Group 2 r/m8, 1 — byte form, count is implicit.
		return c.opGroup2Reg(rex, 1, 1)
	case 0xD2:
		// Group 2 r/m8, CL — byte form, count comes from CL.
		return c.opGroup2Reg(rex, 1, uint64(c.GetReg8(CL)))
	case 0xC0:
		// Group 2 r/m8, imm8 — byte form, count is 8-bit immediate.
		return c.opGroup2Imm(rex, 1)

	// ===== Sign-extending integer move (MOVSXD r64, r/m32) =====

	case 0x63:
		// In long mode 0x63 is MOVSXD (in 32-bit mode it was ARPL).
		// Destination is the reg field, full 64 bits; source is a
		// 32-bit r/m that gets sign-extended.
		return c.opMOVSXD(rex)

	// ===== Group 4/5 (Inc/Dec [byte / full] + Call/Jmp/Push) =====

	case 0xFE:
		return c.opGroup4(rex)
	case 0xFF:
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
	case 0x9B:
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
	case 0xd8, 0xd9, 0xda, 0xdb, 0xdc, 0xdd, 0xde, 0xdf:
		return c.handleX87(op, rex)

	// ===== Flag manipulation =====

	case 0xC8:
		if !(c.mode != ModeLong64) {
			return c.unimplementedAt("opcode %#02x rex=%#x", op, rex)
		}
		// ENTER imm16, imm8 — create a stack frame for a procedure.
		//   imm16 = local variable size (bytes)
		//   imm8  = nesting level (low 5 bits). Common usage is level=0.
		// Pseudo-code (Intel SDM): push RBP; for level-1 frames copy
		// display pointers from caller's frame; RBP=RSP; RSP-=imm16.
		// SeaBIOS uses level=0 ENTER for C function prologs.
		size := c.pushPopOperandSize(operandSize)
		alloc := uint64(c.fetch16())
		level := uint8(c.fetch8()) & 0x1F
		// Push current RBP at operand-size width.
		c.pushStack(c.reg64[RBP], size)
		frameTemp := c.reg64[RSP]
		if level > 0 {
			// Display copy. Each level pushes a pointer copied from the
			// caller's frame chain. The stride is the operand size.
			stride := uint64(size)
			for i := uint8(1); i < level; i++ {
				c.reg64[RBP] -= stride
				v := uint64(0)
				switch size {
				case 2:
					v = uint64(c.readMem16(c.segBase[SS] + (c.reg64[RBP] & 0xFFFF)))
				case 4:
					v = uint64(c.readMem32(c.segBase[SS] + uint64(uint32(c.reg64[RBP]))))
				case 8:
					v = c.readMem64(c.segBase[SS] + c.reg64[RBP])
				}
				c.pushStack(v, size)
			}
			c.pushStack(frameTemp, size)
		}
		// RBP = frame_temp; RSP -= alloc. Width follows operand size.
		switch size {
		case 2:
			c.reg64[RBP] = (c.reg64[RBP] & ^uint64(0xFFFF)) | (frameTemp & 0xFFFF)
			c.reg64[RSP] = (c.reg64[RSP] & ^uint64(0xFFFF)) |
				(uint64(uint16(c.reg64[RSP])-uint16(alloc)) & 0xFFFF)
		case 4:
			c.reg64[RBP] = frameTemp & 0xFFFFFFFF
			c.reg64[RSP] = uint64(uint32(c.reg64[RSP]) - uint32(alloc))
		case 8:
			c.reg64[RBP] = frameTemp
			c.reg64[RSP] -= alloc
		}
		return nil

	case 0xC4, 0xC5:
		if c.mode == ModeLong64 {
			return c.unimplementedAt("opcode %#02x rex=%#x", op, rex)
		}
		// LES r16/32, m16:16/32 (0xC4) / LDS (0xC5). #UD in long mode
		// (the encoding is repurposed as VEX prefix). Loads a 16-bit
		// segment selector + 16/32-bit offset from memory into ES/DS
		// (resp.) and the destination GPR.
		m := c.parseModRM64(rex)
		if m.isReg {
			return c.unimplementedAt("LES/LDS with register operand")
		}
		seg := m.defaultSeg
		if c.currentSegOverride >= 0 {
			seg = c.currentSegOverride
		}
		ea := c.segBase[seg] + m.ea
		// Offset: operand-size (16 or 32). Selector: 16 bits at ea+size.
		var off uint64
		switch operandSize {
		case 2:
			off = uint64(c.readMem16(ea))
		default:
			off = uint64(c.readMem32(ea))
		}
		sel := c.readMem16(ea + uint64(operandSize))
		dstSeg := DS
		if op == 0xC4 {
			dstSeg = ES
		}
		c.seg[dstSeg] = sel
		if c.cr[0]&CR0_PE == 0 {
			c.segBase[dstSeg] = uint64(sel) << 4
		}
		c.writeReg(m.reg, off, operandSize)
		return nil

	case 0xF1:
		// INT1 / ICEBP — vectors through gate 1 (#DB). Rare in user
		// code but SeaBIOS occasionally lands here when stale data is
		// decoded as code. Delivering vector 1 is the architectural
		// behaviour; if our IDT routes vec 1 to a real handler we'll
		// trip it, but in the legitimate ICEBP case the OS would
		// install a debug handler.
		return c.deliverInterrupt(1, false, 0)

	case 0xC9: // LEAVE — restore RBP, pop saved RBP
		// LEAVE := mov rsp, rbp ; pop rbp. Per Intel SDM the width is the
		// operand-size attribute (16/32/64), so 0x66 flips it. The MOV
		// RSP, RBP and the pop both use that width.
		size := c.pushPopOperandSize(operandSize)
		switch size {
		case 8:
			c.SetReg64(RSP, c.GetReg64(RBP))
			c.SetReg64(RBP, c.popStack(8))
		case 4:
			c.reg64[RSP] = (c.reg64[RSP] & ^uint64(0xFFFFFFFF)) | (c.reg64[RBP] & 0xFFFFFFFF)
			v := c.popStack(4)
			c.reg64[RBP] = (c.reg64[RBP] & ^uint64(0xFFFFFFFF)) | (v & 0xFFFFFFFF)
		case 2:
			c.reg64[RSP] = (c.reg64[RSP] & ^uint64(0xFFFF)) | (c.reg64[RBP] & 0xFFFF)
			v := c.popStack(2)
			c.reg64[RBP] = (c.reg64[RBP] & ^uint64(0xFFFF)) | (v & 0xFFFF)
		}
		return nil

	case 0x98: // CBW / CWDE / CDQE — sign-extend AL/AX/EAX in place.
		switch operandSize {
		case 2: // CBW: AX = sign-extend(AL)
			c.SetReg16(AX, uint16(int8(c.GetReg8(AL))))
		case 4: // CWDE: EAX = sign-extend(AX)
			c.SetReg32(EAX, uint32(int16(c.GetReg16(AX))))
		case 8: // CDQE: RAX = sign-extend(EAX)
			c.SetReg64(RAX, uint64(int32(c.GetReg32(EAX))))
		}
		return nil

	case 0x99: // CWD / CDQ / CQO — sign-extend RAX into RDX.
		a := c.readReg(RAX, operandSize)
		var hi uint64
		if a&signBit(operandSize) != 0 {
			hi = mask(operandSize)
		}
		c.writeReg(RDX, hi, operandSize)
		return nil

	case 0x9C: // PUSHF / PUSHFD / PUSHFQ — width follows operand
		// size (pushPopOperandSize): real/pm16 default 2 (0x66→4), pm32
		// default 4 (0x66→2), long default 8 / PUSHFQ (0x66→2, no 4-byte
		// form). stackSlotSize() dropped the 0x66 promotion in real mode.
		size := c.pushPopOperandSize(operandSize)
		c.pushStack(c.rflags, size)
		return nil
	case 0x9D: // POPF / POPFD / POPFQ — symmetric to PUSHF.
		size := c.pushPopOperandSize(operandSize)
		v := c.popStack(size)
		// Filter reserved bits via ValidFlagMask; bit 1 always reads 1
		// per SDM. CPL=0 can change everything except VM/RF (RF gets
		// cleared on POPF semantics — we fold the same effect by
		// stripping RF from the popped value). In 16/32-bit modes only
		// the low half of the flags is touched; bits above operandSize*8
		// are preserved.
		mask := uint64(ValidFlagMask)
		if size == 4 {
			mask &= 0xFFFFFFFF
		} else if size == 2 {
			mask &= 0xFFFF
		}
		c.rflags = (c.rflags &^ mask) | (v & mask &^ RFLAGS_RF) | 2
		return nil

	case 0x9E: // SAHF — store AH into low byte of RFLAGS
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
	case 0x9F: // LAHF — load AH from low byte of RFLAGS
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

	case 0xF5: // CMC — complement CF
		c.rflags ^= RFLAGS_CF
		return nil
	case 0xF8: // CLC — clear CF
		c.rflags &^= RFLAGS_CF
		return nil
	case 0xF9: // STC — set CF
		c.rflags |= RFLAGS_CF
		return nil
	case 0xFA: // CLI — clear IF
		c.rflags &^= RFLAGS_IF
		return nil
	case 0xFB: // STI — set IF
		c.rflags |= RFLAGS_IF
		return nil
	case 0xFC: // CLD — clear DF
		c.rflags &^= RFLAGS_DF
		return nil
	case 0xFD: // STD — set DF
		c.rflags |= RFLAGS_DF
		return nil

	// ===== String operations =====

	case 0xA4:
		return c.opStringMOVS(rex, 1, repPrefix)
	case 0xA5:
		return c.opStringMOVS(rex, operandSize, repPrefix)
	case 0xA6:
		return c.opStringCMPS(rex, 1, repPrefix)
	case 0xA7:
		return c.opStringCMPS(rex, operandSize, repPrefix)
	case 0xAA:
		return c.opStringSTOS(rex, 1, repPrefix)
	case 0xAB:
		return c.opStringSTOS(rex, operandSize, repPrefix)
	case 0xAC:
		return c.opStringLODS(rex, 1, repPrefix)
	case 0xAD:
		return c.opStringLODS(rex, operandSize, repPrefix)
	case 0xAE:
		return c.opStringSCAS(rex, 1, repPrefix)
	case 0xAF:
		return c.opStringSCAS(rex, operandSize, repPrefix)

	// ===== Two-byte escape =====

	case 0x0F:
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
		// UD2 — guaranteed-invalid-opcode instruction. Delivered as #UD
		// (vector 6), a fault: saved RIP points at the UD2 itself.
		return c.raiseUD()

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
		// RDTSC — read time-stamp counter into EDX:EAX. The TSC is a
		// fixed-rate wall-clock counter, not an instruction count:
		// report nanoseconds since reset (a 1 GHz TSC), matching the
		// frequency advertised in CPUID leaf 0x15. This keeps RDTSC
		// consistent with the host-clock-backed ACPI PM timer and lets
		// guests derive real elapsed time. (tscBase is set at reset; if
		// a caller skipped Reset it is the zero time, so guard it.)
		v := uint64(0)
		if !c.tscBase.IsZero() {
			v = uint64(time.Since(c.tscBase).Nanoseconds())
		}
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
		// Jcc rel16 / rel32 — displacement size follows the operand
		// size. In real / 16-bit modes the default is 16 bits; in 32-
		// and 64-bit modes it's 32 bits. SeaBIOS's real-mode POST uses
		// rel16 extensively, so dispatching by operandSize matters.
		var disp int64
		if operandSize == 2 {
			disp = int64(int16(c.fetch16()))
		} else {
			disp = int64(int32(c.fetch32()))
		}
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
			c.writeMem8(c.segBaseForModRM(m)+m.ea, v)
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

	case op2 == 0xC7:
		// Group 9. Register form (mod=11): reg=6 RDRAND, reg=7 RDSEED.
		// Memory form (/1): CMPXCHG8B, or CMPXCHG16B under REX.W. See
		// opGroup9.
		return c.opGroup9(rex, operandSize)

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
		return c.opGroup15(rex, repPrefix)

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

	case op2 == 0xB2:
		// LSS r, m16:16/32 — load SS + GPR. Sibling of LES/LDS (0xC4/
		// 0xC5) but available in long mode too (encoding doesn't
		// collide with VEX). SeaBIOS uses LSS in some pm32 transition
		// thunks to atomically swap SS and ESP.
		m := c.parseModRM64(rex)
		if m.isReg {
			return c.unimplementedAt("LSS with register operand")
		}
		seg := m.defaultSeg
		if c.currentSegOverride >= 0 {
			seg = c.currentSegOverride
		}
		ea := c.segBase[seg] + m.ea
		var off uint64
		switch operandSize {
		case 2:
			off = uint64(c.readMem16(ea))
		case 4:
			off = uint64(c.readMem32(ea))
		default:
			off = c.readMem64(ea)
		}
		newSS := c.readMem16(ea + uint64(operandSize))
		c.seg[SS] = newSS
		if c.cr[0]&CR0_PE == 0 {
			c.segBase[SS] = uint64(newSS) << 4
		}
		c.writeReg(m.reg, off, operandSize)
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

	case op2 == 0xB8:
		// POPCNT r, r/m — requires the F3 prefix (advertised via CPUID.1
		// ECX bit 23). repPrefix is the normalised rep code (1 = F3,
		// 2 = F2). Without F3 this encoding is reserved → #UD.
		if repPrefix != 1 {
			return c.raiseUD()
		}
		m := c.parseModRM64(rex)
		src := c.readOperand(m, operandSize) & mask(operandSize)
		c.writeReg(m.reg, uint64(bits.OnesCount64(src)), operandSize)
		// ZF = (src == 0); CF/OF/SF/AF/PF cleared (Intel SDM).
		c.rflags &^= RFLAGS_CF | RFLAGS_OF | RFLAGS_SF | RFLAGS_AF | RFLAGS_PF | RFLAGS_ZF
		if src == 0 {
			c.rflags |= RFLAGS_ZF
		}
		return nil
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
//
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
func (c *CPU) opGroup15(rex, repPrefix uint8) error {
	m := c.parseModRM64(rex)
	if m.isReg {
		// F3 0F AE /0-3 (reg-form) = FSGSBASE: read/write the FS/GS base from
		// a GPR. REX.W selects 64-bit, else 32-bit (zero-extended). Used by
		// modern kernels (e.g. NuttX) for fast per-CPU/TLS base switches.
		if repPrefix == 1 { // F3
			size := uint8(4)
			if rex&0x08 != 0 { // REX.W
				size = 8
			}
			switch m.reg {
			case 0: // RDFSBASE
				c.writeReg(m.rm, c.segBase[FS], size)
				return nil
			case 1: // RDGSBASE
				c.writeReg(m.rm, c.segBase[GS], size)
				return nil
			case 2: // WRFSBASE
				v := c.readReg(m.rm, size)
				c.segBase[FS] = v
				c.msrFSBase = v
				return nil
			case 3: // WRGSBASE
				v := c.readReg(m.rm, size)
				c.segBase[GS] = v
				c.msrGSBase = v
				return nil
			}
		}
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

// opGroup9 implements 0x0F 0xC7 — Group 9. We only handle the
// register-form sub-opcodes /6 RDRAND and /7 RDSEED. Both are required
// for Linux's crng_init to finish quickly: without them, the kernel
// falls back to interrupt-arrival jitter for entropy, which on our
// emulated host takes ~300 kernel-seconds and stalls Alpine boot
// between "i8042 probe failed" and "Mounting boot media".
//
// Per Intel SDM Vol 2: on success the destination register receives a
// random value and CF=1; OF, SF, ZF, AF, PF are cleared. We always
// succeed (the host's PRNG never blocks); we do not model the
// "underflow" / "try again" failure case because Linux's loop will
// notice the rare CF=0 and just retry — making it CF=0 occasionally
// here would only slow things down.
//
// Operand size follows the usual REX.W / 0x66 rules. The 8-bit form is
// not legal for RDRAND/RDSEED, so we treat operandSize<2 as
// operandSize=2.
func (c *CPU) opGroup9(rex, operandSize uint8) error {
	m := c.parseModRM64(rex)
	if !m.isReg {
		// /1 memory form — CMPXCHG8B (operandSize 4) / CMPXCHG16B
		// (REX.W → operandSize 8). LOCK is a no-op here: the emulator is
		// single-threaded, so the read-compare-write is already atomic.
		if m.reg != 1 {
			return c.unimplementedAt("Group 9 /%d memory form", m.reg)
		}
		ea := c.segBaseForModRM(m) + m.ea
		if operandSize == 8 {
			// CMPXCHG16B: compare RDX:RAX with the 128-bit [ea].
			lo := c.readMem64(ea)
			hi := c.readMem64(ea + 8)
			if lo == c.reg64[RAX] && hi == c.reg64[RDX] {
				c.writeMem64(ea, c.reg64[RBX])
				c.writeMem64(ea+8, c.reg64[RCX])
				c.rflags |= RFLAGS_ZF
			} else {
				c.reg64[RAX] = lo
				c.reg64[RDX] = hi
				c.rflags &^= RFLAGS_ZF
			}
			return nil
		}
		// CMPXCHG8B: compare EDX:EAX with the 64-bit [ea].
		val := c.readMem64(ea)
		expected := uint64(c.GetReg32(RDX))<<32 | uint64(c.GetReg32(RAX))
		if val == expected {
			c.writeMem64(ea, uint64(c.GetReg32(RCX))<<32|uint64(c.GetReg32(RBX)))
			c.rflags |= RFLAGS_ZF
		} else {
			c.SetReg32(RAX, uint32(val))     // 32-bit write zero-extends RAX
			c.SetReg32(RDX, uint32(val>>32)) //   "          "          RDX
			c.rflags &^= RFLAGS_ZF
		}
		return nil
	}
	switch m.reg {
	case 6, 7: // RDRAND, RDSEED
		size := operandSize
		if size < 2 {
			size = 2
		}
		c.writeReg(m.rm, rand.Uint64(), size)
		// Clear OF/SF/ZF/AF/PF, set CF=1.
		c.rflags &^= RFLAGS_OF | RFLAGS_SF | RFLAGS_ZF | RFLAGS_AF | RFLAGS_PF
		c.rflags |= RFLAGS_CF
		return nil
	}
	return c.unimplementedAt("Group 9 /%d (reg form)", m.reg)
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
			lo, rerr := c.memMap.Read64(loPhys)
			if rerr != nil {
				return fmt.Errorf("LTR: read GDT entry: %w", rerr)
			}
			hi, rerr := c.memMap.Read64(hiPhys)
			if rerr != nil {
				return fmt.Errorf("LTR: read GDT entry+8: %w", rerr)
			}
			// Validate the descriptor's Present bit (lo[47]). An unmapped
			// GDT reads back as zero (PhysMemoryMap returns 0 for unmapped
			// addresses), so without this check a bogus GDTR would
			// silently install a zero TR base/limit. P=0 means the
			// selector does not reference a valid TSS.
			if lo&(uint64(1)<<47) == 0 {
				return fmt.Errorf("LTR: TSS descriptor for selector %#x not present", sel)
			}
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
		case m.reg == 2 && m.rm == 0:
			// XGETBV — read XCR[ECX] into EDX:EAX (0F 01 D0). Only XCR0
			// (ECX==0) is defined; any other index is #GP on real HW.
			if c.GetReg32(ECX) != 0 {
				return c.raiseGP(0)
			}
			c.SetReg64(RAX, c.xcr0&0xFFFFFFFF)
			c.SetReg64(RDX, c.xcr0>>32)
			return nil
		case m.reg == 2 && m.rm == 1:
			// XSETBV — write EDX:EAX to XCR[ECX] (0F 01 D1). Privileged
			// (#GP if CPL!=0). Only XCR0 is defined; bit 0 (x87) must
			// stay set, and we don't support clearing it or setting any
			// state we can't manage.
			if c.cpl != 0 {
				return c.raiseGP(0)
			}
			if c.GetReg32(ECX) != 0 {
				return c.raiseGP(0)
			}
			v := uint64(c.GetReg32(EAX)) | uint64(c.GetReg32(EDX))<<32
			if v&1 == 0 {
				// Clearing the x87 bit is always invalid.
				return c.raiseGP(0)
			}
			c.xcr0 = v
			return nil
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
	case 0: // SGDT — pseudo-descriptor size depends on mode (6 bytes
		// in 16/32-bit, 10 bytes in long mode).
		addr := c.segBaseForModRM(m) + m.ea
		c.writeMem16(addr, uint16(c.segLimit[GDTR]))
		if c.mode == ModeLong64 {
			c.writeMem64(addr+2, c.segBase[GDTR])
		} else {
			c.writeMem32(addr+2, uint32(c.segBase[GDTR]))
		}
		return nil
	case 1: // SIDT
		addr := c.segBaseForModRM(m) + m.ea
		c.writeMem16(addr, uint16(c.segLimit[IDTR]))
		if c.mode == ModeLong64 {
			c.writeMem64(addr+2, c.segBase[IDTR])
		} else {
			c.writeMem32(addr+2, uint32(c.segBase[IDTR]))
		}
		return nil
	case 2: // LGDT — load GDT base+limit. Pseudo-descriptor in 16/32-
		// bit modes is limit(2) + base(4) = 6 bytes; in long mode it's
		// limit(2) + base(8) = 10 bytes. We picked the wrong format
		// before — reading 64 bits in real mode loaded 4 bytes of
		// adjacent garbage into the high half of GDTR.base, then the
		// next far jump's descriptor read landed at a random address
		// and CS came back without its D-bit.
		addr := c.segBaseForModRM(m) + m.ea
		c.segLimit[GDTR] = uint32(c.readMem16(addr))
		if c.mode == ModeLong64 {
			c.segBase[GDTR] = c.readMem64(addr + 2)
		} else {
			c.segBase[GDTR] = uint64(c.readMem32(addr + 2))
		}
		return nil
	case 3: // LIDT — same mode-aware base width as LGDT.
		addr := c.segBaseForModRM(m) + m.ea
		c.segLimit[IDTR] = uint32(c.readMem16(addr))
		if c.mode == ModeLong64 {
			c.segBase[IDTR] = c.readMem64(addr + 2)
		} else {
			c.segBase[IDTR] = uint64(c.readMem32(addr + 2))
		}
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
		// LMSW may set PE but never clear it: there is no escape from
		// protected mode via LMSW. Preserve a PE that is already set.
		pe := c.cr[0] & uint64(CR0_PE)
		c.cr[0] = (c.cr[0] &^ 0xF) | uint64(v&0xF) | pe
		c.recomputeMode()
		return nil
	case 7: // INVLPG — invalidate the TLB entry covering the operand's
		// linear address (segment base + effective address), not the
		// bare EA: with a non-zero FS/GS base they differ.
		c.tlb.invalidatePage(c.segBaseForModRM(m) + m.ea)
		c.invalidateFetchBuffer()
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
//
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
		// CR0.ET is hardwired to 1 on P6+ CPUs — writes that clear it are
		// ignored. Firmware reads CR0 back expecting ET set.
		c.cr[0] = v | CR0_ET
		// LMA latches when paging is enabled with LME set; clears when
		// paging turns off.
		if !oldPG && newPG && c.efer&EFER_LME != 0 {
			c.efer |= EFER_LMA
		} else if oldPG && !newPG {
			c.efer &^= EFER_LMA
		}
		if oldPG != newPG || oldWP != newWP {
			c.tlb.flushAll()
			c.invalidateFetchBuffer()
		}
		c.recomputeMode()
		return
	}
	if n == 3 {
		// Non-PCID CR3 reload: drop all non-global entries. The new CR3
		// install itself happens after the flush so the next translation
		// walks under the new root.
		c.tlb.flushNonGlobal()
		c.invalidateFetchBuffer()
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
			c.invalidateFetchBuffer()
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
	msrEFER         = 0xC0000080
	msrSTAR         = 0xC0000081
	msrLSTAR        = 0xC0000082
	msrCSTAR        = 0xC0000083
	msrSFMASK       = 0xC0000084
	msrFSBaseMSR    = 0xC0000100
	msrGSBaseMSR    = 0xC0000101
	msrKernelGSBase = 0xC0000102
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
	// x2APIC register MSRs + IA32_TSC_DEADLINE route to the local APIC.
	if c.apicMSR != nil && ((num >= 0x800 && num <= 0x8FF) || num == 0x6E0) {
		return c.apicMSR.ReadMSR(num)
	}
	switch num {
	case 0x1B: // IA32_APIC_BASE
		// With a modelled local APIC: the (writable) enabled value, base
		// 0xFEE00000 | EN | BSP. Without one: just the BSP bit (0x100),
		// matching a vCPU whose APIC is disabled — the value shipped
		// before the LocalAPIC existed, keeping the no-APIC path stable.
		if c.apicEnabled {
			return c.msrApicBase
		}
		return 0x100
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
	c.noteUnknownMSR(false, num, 0)
	return 0
}

// noteUnknownMSR logs the first RDMSR/WRMSR of an MSR we don't model, in
// the debug feature profile (advisor report #4). Unknown MSRs read as 0
// and drop writes — real hardware #GPs, but the boot path passes through
// several MSRs we don't model, so the permissive default stays. Logging
// them in debug mode surfaces guest/kernel feature-probing we silently
// absorb. Deduplicated per (direction, MSR) via the debug seen-set.
func (c *CPU) noteUnknownMSR(write bool, num uint32, v uint64) {
	if c.featureProfile != profileDebug {
		return
	}
	dir := "RDMSR"
	if write {
		dir = "WRMSR"
	}
	key := fmt.Sprintf("msr-%s-%#x", dir, num)
	if _, seen := c.seenUnimpl[key]; seen {
		return
	}
	c.seenUnimpl[key] = struct{}{}
	if write {
		fmt.Fprintf(os.Stderr, "[msr] unmodeled WRMSR %#x = %#x (write dropped)\n", num, v)
	} else {
		fmt.Fprintf(os.Stderr, "[msr] unmodeled RDMSR %#x -> 0\n", num)
	}
}

func (c *CPU) writeMSR(num uint32, v uint64) error {
	// x2APIC register MSRs + IA32_TSC_DEADLINE route to the local APIC.
	if c.apicMSR != nil && ((num >= 0x800 && num <= 0x8FF) || num == 0x6E0) {
		c.apicMSR.WriteMSR(num, v)
		return nil
	}
	switch num {
	case 0x1B: // IA32_APIC_BASE — software may relocate/toggle the APIC
		if c.apicEnabled {
			c.msrApicBase = v
		}
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
	default:
		c.noteUnknownMSR(true, num, v)
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
//	register destination — the bit index is masked to (operand_size - 1).
//	  `bts rax, rbx` with rbx = 253 sets bit (253 & 63) = 61 of rax.
//
//	memory destination — the bit index is treated as a SIGNED integer
//	  that extends the memory address. `bts qword [mem], rax` with
//	  rax = 253 sets bit 253 of memory at [mem], landing at byte
//	  [mem + 253/8] (= [mem + 31]) bit 5. Equivalently, addresses the
//	  word at [mem + (idx / word_bits) * (word_bits/8)] and sets bit
//	  (idx mod word_bits) within it. Signed division rounds toward
//	  zero, so a negative idx with non-zero remainder needs a -1
//	  adjustment to the word index and a +word_bits to the bit-within-
//	  word (the same pattern the i386 backend uses).
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
			c.writeMem8(c.segBaseForModRM(m)+m.ea, uint8(src))
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
				c.writeMem8(c.segBaseForModRM(m)+m.ea, uint8(src))
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
			c.writeMem8(c.segBaseForModRM(m)+m.ea, uint8(res))
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
				c.writeMem8(c.segBaseForModRM(m)+m.ea, uint8(res))
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
// 6=DIV, 7=IDIV. Both the 0xF6 (byte) and 0xF7 (word+) forms route here.
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
	if operandSize == 1 {
		// 8-bit MUL is special: the 16-bit product fills AX (AH:AL),
		// not DL:AL like the wider forms. CF/OF set when AH is nonzero.
		prod := (a * src) & 0xFFFF
		c.writeReg(RAX, prod, 2)
		hi := prod >> 8
		var fl flagBits
		fl.cf = hi != 0
		fl.of = hi != 0
		c.setArithFlags(fl)
		return nil
	}
	var hi, lo uint64
	switch operandSize {
	case 8:
		hi, lo = bits.Mul64(a, src)
	default:
		prod := a * src
		hi = prod >> (uint(operandSize) * 8)
		lo = prod & mask(operandSize)
	}
	// Write back: low half to rAX, high half to rDX (16/32/64 forms).
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
	if operandSize == 1 {
		// 8-bit IMUL is special: the 16-bit signed product fills AX
		// (AH:AL), not DL:AL. CF/OF set when AH is not the sign
		// extension of AL (the product doesn't fit in a signed byte).
		p := int16(int8(uint8(a))) * int16(int8(uint8(src)))
		c.writeReg(RAX, uint64(uint16(p)), 2)
		var fl flagBits
		fl.cf = int16(int8(uint8(p))) != p
		fl.of = fl.cf
		c.setArithFlags(fl)
		return nil
	}
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
		c.raiseDE() // divide by zero
	}
	if operandSize == 1 {
		// Byte form (F6 /6) is special: the dividend is the 16-bit AX
		// register (NOT DL:AL), the quotient goes to AL and the remainder
		// to AH. The DX:AX / EDX:EAX / RDX:RAX shape below applies only to
		// the 16/32/64-bit forms.
		dividend := uint16(c.reg64[RAX])
		q := dividend / uint16(src)
		r := dividend % uint16(src)
		if q > 0xFF {
			c.raiseDE() // quotient doesn't fit AL
		}
		c.SetReg8(AL, uint8(q))
		c.SetReg8(AH, uint8(r))
		c.setArithFlags(flagBits{})
		return nil
	}
	switch operandSize {
	case 8:
		hi := c.GetReg64(RDX)
		lo := c.GetReg64(RAX)
		if hi >= src {
			c.raiseDE() // quotient would overflow 64 bits
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
			c.raiseDE() // quotient would overflow the destination width
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
		c.raiseDE() // signed divide by zero
	}
	if operandSize == 1 {
		// Byte form (F6 /7): dividend is signed AX, quotient→AL,
		// remainder→AH. Valid quotient range is [-128, 127].
		dividend := int64(int16(uint16(c.reg64[RAX])))
		q := dividend / srcS
		r := dividend % srcS
		if q < -128 || q > 127 {
			c.raiseDE()
		}
		c.SetReg8(AL, uint8(int8(q)))
		c.SetReg8(AH, uint8(int8(r)))
		c.setArithFlags(flagBits{})
		return nil
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
		// INT64_MIN / -1 overflows the 64-bit signed quotient (and would
		// panic Go's runtime divide); raise #DE as real hardware does.
		if dividend == math.MinInt64 && srcS == -1 {
			c.raiseDE()
		}
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
		// #DE if the signed quotient doesn't fit the destination width
		// (e.g. INT_MIN / -1). bound = 2^(n-1); valid range is
		// [-bound, bound-1].
		bound := int64(1) << (uint(operandSize)*8 - 1)
		if q < -bound || q > bound-1 {
			c.raiseDE()
		}
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
				c.writeMem8(c.segBaseForModRM(m)+m.ea, uint8(res))
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
// raiseUD delivers an Invalid-Opcode exception (#UD, vector 6) to the
// guest. Use this for encodings that are *architecturally invalid* — a
// reserved Group 4/5 /reg value, a far CALL/JMP with a register operand,
// BOUND with a register operand — so the guest's own #UD handler runs.
// This is deliberately distinct from unimplementedAt (ErrNotImplemented),
// which halts the emulator and is reserved for opcodes we simply have not
// written yet (a decoder gap). Conflating the two made temu abort on
// guest code that real hardware would fault on and recover from (e.g.
// go-boot executing 0xFF /7).
//
// Delivered as a FAULT via exceptionPanic (like #DE): Step's recover
// rewinds RIP to the faulting instruction before vectoring through the
// IDT, so the saved CS:RIP points at the bad opcode. Declared to return
// error only so callers can `return c.raiseUD()`; it never actually
// returns (it panics).
func (c *CPU) raiseUD() error {
	panic(exceptionPanic{Vec: 6})
}

// raiseGP raises #GP (vector 13) with the given error code. Like raiseUD
// it is delivered as a fault via exceptionPanic so Step's recover rewinds
// RIP to the faulting instruction. Never returns.
func (c *CPU) raiseGP(code uint32) error {
	panic(exceptionPanic{Vec: 13, HasErr: true, ErrorCode: code})
}

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
	// /2..7 are reserved encodings of 0xFE → #UD.
	return c.raiseUD()
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
		// Target and pushed return address width follow operand size
		// (pushPopOperandSize): long mode defaults to 64-bit (no 32-bit
		// near form), legacy modes 2/4 flipped by 0x66.
		size := uint8(c.pushPopOperandSize(operandSize))
		target := c.readOperand(m, size)
		c.pushStack(c.rip, int(size))
		c.rip = target
		return nil
	case 3: // CALL FAR m16:16/32/64 (intersegment indirect through memory)
		if m.isReg {
			return c.raiseUD() // CALLF requires a memory operand; register form is #UD
		}
		seg := m.defaultSeg
		if c.currentSegOverride >= 0 {
			seg = c.currentSegOverride
		}
		ea := c.segBase[seg] + m.ea
		var newRIP uint64
		switch operandSize {
		case 2:
			newRIP = uint64(c.readMem16(ea))
		case 4:
			newRIP = uint64(c.readMem32(ea))
		default:
			newRIP = c.readMem64(ea)
		}
		newCS := c.readMem16(ea + uint64(operandSize))
		// Push old CS + RIP at operand-size width to match RETF.
		size := c.pushPopOperandSize(operandSize)
		c.pushStack(uint64(c.seg[CS]), size)
		c.pushStack(c.rip, size)
		c.seg[CS] = newCS
		if c.cr[0]&CR0_PE == 0 {
			c.segBase[CS] = uint64(newCS) << 4
		}
		c.rip = newRIP
		return nil
	case 4: // JMP r/m (near, absolute indirect) — target width follows
		// operand size (64-bit default in long mode).
		size := uint8(c.pushPopOperandSize(operandSize))
		target := c.readOperand(m, size)
		c.rip = target
		return nil
	case 5: // JMP FAR m16:16/32/64 (intersegment indirect through memory)
		if m.isReg {
			return c.raiseUD() // JMPF requires a memory operand; register form is #UD
		}
		seg := m.defaultSeg
		if c.currentSegOverride >= 0 {
			seg = c.currentSegOverride
		}
		ea := c.segBase[seg] + m.ea
		var newRIP uint64
		switch operandSize {
		case 2:
			newRIP = uint64(c.readMem16(ea))
		case 4:
			newRIP = uint64(c.readMem32(ea))
		default:
			newRIP = c.readMem64(ea)
		}
		newCS := c.readMem16(ea + uint64(operandSize))
		c.seg[CS] = newCS
		if c.cr[0]&CR0_PE == 0 {
			c.segBase[CS] = uint64(newCS) << 4
		}
		c.rip = newRIP
		return nil
	case 6: // PUSH r/m — width follows operand size (see
		// pushPopOperandSize). Using stackSlotSize() here silently
		// dropped the 0x66 promotion in real mode, half-pushing the
		// value SeaBIOS later iret'd through.
		size := c.pushPopOperandSize(operandSize)
		c.pushStack(c.readOperand(m, uint8(size)), size)
		return nil
	}
	// Only /7 reaches here — a reserved encoding of 0xFF → #UD.
	return c.raiseUD()
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
	// Group 11 /0 is the canonical MOV r/m, imm. Modern Intel CPUs raise
	// #UD on /1..6 (and use /7 for TSX XBEGIN/XABORT), but pre-Pentium
	// CPUs ignored the reg field — assemblers / linkers occasionally
	// emit /1..6 forms that real BIOSes (SeaBIOS in some thunks) rely
	// on. We treat /1..6 as MOV-with-reg-ignored; /7 is the only one
	// that's genuinely a different instruction and stays unimplemented.
	if m.reg == 7 {
		// TSX (RTM): 0xC7 /7 = XBEGIN rel16/32, 0xC6 /7 = XABORT imm8. We don't
		// implement hardware transactional memory, so model the architecturally
		// valid "transaction always aborts" outcome: XBEGIN sets EAX to an abort
		// status and branches to its fallback (guest then takes the lock path);
		// XABORT outside a transaction is a no-op. This is what real RTM code is
		// written to fall back to, so it executes correctly without TSX.
		if operandSize == 1 { // XABORT imm8
			c.fetch8()
			return nil
		}
		var disp int64 // XBEGIN rel
		if operandSize == 2 {
			disp = int64(int16(c.fetch16()))
		} else {
			disp = int64(int32(c.fetch32()))
		}
		c.writeReg(0, 0, 4) // EAX = 0: aborted, no RETRY bit set → don't retry
		c.rip = uint64(int64(c.rip) + disp)
		return nil
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
		c.writeMem8(c.segBaseForModRM(m)+m.ea, src)
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

	switch {
	case c.cr[0]&CR0_PE == 0:
		// Real mode: segment base = sel << 4. The architectural rule
		// is that real-mode segment loads only update the SELECTOR and
		// rebuild .base; .limit and .access flags are PRESERVED from
		// whatever the prior protected-mode state cached. SeaBIOS uses
		// exactly this to keep "big real mode" alive (CSlim = 4 GiB
		// from a brief PE excursion, then exit to real mode with PE=0
		// and run 16-bit code that can access > 1 MB). Resetting limit
		// to 0xFFFF here breaks that — we'd see CSlim=0xFFFF in real
		// mode and the next thunk that runs past 64 KB would crash.
		c.segBase[idx] = uint64(sel) << 4
	case c.mode == ModeLong64:
		// Long mode forces flat segments: base 0, limit 4 GiB. Don't
		// disturb the access cache (the boot harness or a far jump
		// set L/D appropriately).
		c.segBase[idx] = 0
		c.segLimit[idx] = 0xFFFFFFFF
	default:
		// Protected mode (pm16, pm32, compat32): walk the GDT/LDT and
		// load the descriptor's base/limit/access into the cache. This
		// is mandatory — keeping the stale cached base "because the prior
		// far-jump set it" silently breaks any thunk that reloads a data
		// segment to a DIFFERENT base. SeaBIOS's call32 (16->32) does
		// exactly that: a 16-bit INT handler running on the zonelow
		// stack (DS base 0xda800) transitions to 32-bit and reloads DS
		// with the flat data selector. Without the descriptor walk DS
		// kept base 0xda800, so every "flat" pointer in process_op_32
		// (the op struct, op->drive_fl) resolved 0xda800 bytes high and
		// read zeros — the virtio disk read silently found a NULL drive.
		c.loadProtSegment(idx, sel)
	}
	if idx == CS {
		c.recomputeMode()
	}
	return nil
}

// loadProtSegment loads a protected-mode segment descriptor from the
// GDT (or LDT) into the cached base/limit/access for segment register
// idx. A null selector (index 0) leaves a zero base so stale state from
// a prior mode can't leak through. We don't enforce present/DPL/type
// checks — the guests we run (SeaBIOS, Linux) only load valid
// descriptors here, and faulting would just add complexity we'd have to
// model the #GP path for.
func (c *CPU) loadProtSegment(idx int, sel uint16) {
	if sel&0xFFFC == 0 {
		// Null selector: data-segment loads of null are legal; accesses
		// would #GP, but we just zero the base so nothing inherits a
		// stale non-zero base.
		c.segBase[idx] = 0
		c.segLimit[idx] = 0
		return
	}
	tableBase := c.segBase[GDTR]
	if sel&0x4 != 0 {
		tableBase = c.segBase[LDTR]
	}
	descAddr := tableBase + uint64(sel&0xFFF8)
	desc := c.readMem64(descAddr)

	base := ((desc >> 16) & 0xFFFFFF) | (((desc >> 56) & 0xFF) << 24)
	limit := (desc & 0xFFFF) | (((desc >> 48) & 0xF) << 16)
	flags := (desc >> 52) & 0xF // AVL, L, D/B, G (bits 0..3 of the nibble)
	if flags&0x8 != 0 {         // G (granularity): limit counts 4-KiB pages
		limit = (limit << 12) | 0xFFF
	}
	access := (desc >> 40) & 0xFF

	c.segBase[idx] = base
	c.segLimit[idx] = uint32(limit)
	c.segAccess[idx] = uint32(access) | (uint32(flags) << 8)
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

func (c *CPU) opPUSHReg(rd, rex, operandSize uint8) error {
	idx := uint8(rd)
	if rex&rexB != 0 {
		idx |= 0x8
	}
	c.pushStack(c.reg64[idx], c.pushPopOperandSize(operandSize))
	return nil
}

func (c *CPU) opPOPReg(rd, rex, operandSize uint8) error {
	idx := uint8(rd)
	if rex&rexB != 0 {
		idx |= 0x8
	}
	size := c.pushPopOperandSize(operandSize)
	v := c.popStack(size)
	switch size {
	case 2:
		// 16-bit POP: only the low 16 of the destination change.
		c.reg64[idx] = (c.reg64[idx] & ^uint64(0xFFFF)) | (v & 0xFFFF)
	case 4:
		// 32-bit POP: write low 32 and zero the upper half (Intel SDM
		// rule for 32-bit register writes in compat/protected mode).
		c.reg64[idx] = v & 0xFFFFFFFF
	default:
		c.reg64[idx] = v
	}
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

// stackSlotSize returns the natural stack-operation width in bytes for
// the current CPU mode: long mode = 8, protected-32 / compat-32 = 4,
// real / protected-16 = 2. The architectural rule keys off CS.D plus
// SS.B (default-stack-size); the assumption here is that SS.B matches
// the code segment's D-bit, which is the convention every kernel and
// BIOS we boot follows. If a guest ever ran 32-bit code on a 16-bit
// stack (or vice versa) this would need to read SS access directly.
func (c *CPU) stackSlotSize() int {
	switch c.mode {
	case ModeLong64:
		return 8
	case ModeProtected32, ModeCompat32:
		return 4
	case ModeReal16, ModeProtected16:
		return 2
	}
	return 8
}

// pushPopOperandSize converts the generic per-instruction operand size
// into the width that PUSH/POP/PUSHF/POPF and friends should actually
// transfer to/from the stack. Per Intel SDM Vol 2A:
//
//   - In legacy modes (real/pm16/pm32) the push/pop width follows the
//     instruction's operand size: default 2 in 16-bit modes, default 4
//     in pm32, and a 0x66 prefix swaps them.
//   - In long mode there is NO 32-bit form for PUSH/POP — the default
//     is 64-bit, and the 0x66 prefix gives 16-bit. A decoder
//     operandSize of 4 in long mode therefore means "we just defaulted
//     to 32 but PUSH/POP have no such form" and must be promoted to 8.
//     REX.W (operandSize=8) and 0x66 (operandSize=2) flow through
//     unchanged.
//
// Using stackSlotSize() instead of this helper was the cause of a
// SeaBIOS-on-x86_64 corruption: `pushl BREGS_code(%eax)` in real-mode
// .code16 (with the 0x66 prefix that promotes the push to 32-bit)
// pushed only 2 bytes, so the iretw that followed read CS from the
// wrong half of the stack and landed in zero-memory.
func (c *CPU) pushPopOperandSize(operandSize uint8) int {
	if c.mode == ModeLong64 && operandSize == 4 {
		return 8
	}
	return int(operandSize)
}

// pushStack pushes `size` bytes of v onto the stack and decrements the
// stack pointer (full RSP in long mode, low 32 in pm32, low 16 in pm16).
// The high bits of RSP are preserved across the subtraction in non-long
// modes so a later mode-switch back to long mode sees the same RSP
// upper bits we entered with.
func (c *CPU) pushStack(v uint64, size int) {
	rsp := c.reg64[RSP]
	switch size {
	case 2:
		newSP := (rsp & ^uint64(0xFFFF)) | uint64((uint16(rsp)-2)&0xFFFF)
		c.writeMem16(c.segBase[SS]+(newSP&0xFFFF), uint16(v))
		c.reg64[RSP] = newSP
	case 4:
		newSP := (rsp & ^uint64(0xFFFFFFFF)) | uint64(uint32(rsp)-4)
		c.writeMem32(c.segBase[SS]+(newSP&0xFFFFFFFF), uint32(v))
		c.reg64[RSP] = newSP
	default:
		c.push64(v)
	}
}

// popStack pops `size` bytes off the stack and returns it
// zero-extended to 64 bits. RSP advances by `size`. Like pop64, the
// memory load happens BEFORE the stack pointer mutates so a #PF on the
// load leaves RSP unchanged.
func (c *CPU) popStack(size int) uint64 {
	rsp := c.reg64[RSP]
	switch size {
	case 2:
		v := uint64(c.readMem16(c.segBase[SS] + (rsp & 0xFFFF)))
		c.reg64[RSP] = (rsp & ^uint64(0xFFFF)) | uint64((uint16(rsp)+2)&0xFFFF)
		return v
	case 4:
		v := uint64(c.readMem32(c.segBase[SS] + (rsp & 0xFFFFFFFF)))
		c.reg64[RSP] = (rsp & ^uint64(0xFFFFFFFF)) | uint64(uint32(rsp)+4)
		return v
	default:
		return c.pop64()
	}
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
