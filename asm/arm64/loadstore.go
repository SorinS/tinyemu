package arm64

import (
	"fmt"
	"strings"
)

// Single-register load/store. The mnemonic fixes the access size and the opc
// (store / zero-extending load / sign-extending load), while the operand syntax
// picks the addressing mode and thus the encoding sub-form:
//
//	[Xn, #pimm]        unsigned offset   (scaled, non-negative)   bits[25:24]=01
//	[Xn, #simm]        unscaled (STUR)   (signed imm9)            bits[25:24]=00 mode=00
//	[Xn, #simm]!       pre-index         (signed imm9, writeback) mode=11
//	[Xn], #simm        post-index        (signed imm9, writeback) mode=01
//	[Xn, Xm{,ext}]     register offset                            bit21=1, bits[11:10]=10
//
// str/ldr auto-select unsigned when the offset is a non-negative multiple of
// the access size in range, else fall back to the unscaled imm9 form (matching
// llvm). stur/ldur force the unscaled form.

// lsForm returns the size (00/01/10/11) and opc fields for a load/store
// mnemonic, plus whether the mnemonic forces the unscaled (stur/ldur) form.
func lsForm(mnem string, rt reg) (size, opc uint32, unscaled, ok bool) {
	switch mnem {
	case "strb":
		return 0, 0, false, true
	case "ldrb":
		return 0, 1, false, true
	case "ldrsb":
		return 0, sopc(rt), false, true
	case "strh":
		return 1, 0, false, true
	case "ldrh":
		return 1, 1, false, true
	case "ldrsh":
		return 1, sopc(rt), false, true
	case "ldrsw":
		return 2, 2, false, true
	case "str":
		return wsize(rt), 0, false, true
	case "ldr":
		return wsize(rt), 1, false, true
	case "sturb":
		return 0, 0, true, true
	case "ldurb":
		return 0, 1, true, true
	case "ldursb":
		return 0, sopc(rt), true, true
	case "sturh":
		return 1, 0, true, true
	case "ldurh":
		return 1, 1, true, true
	case "ldursh":
		return 1, sopc(rt), true, true
	case "ldursw":
		return 2, 2, true, true
	case "stur":
		return wsize(rt), 0, true, true
	case "ldur":
		return wsize(rt), 1, true, true
	}
	return 0, 0, false, false
}

// sopc picks the opc for a sign-extending load: 10 extends to a 64-bit Xt, 11
// to a 32-bit Wt.
func sopc(rt reg) uint32 {
	if rt.is64 {
		return 2
	}
	return 3
}

// wsize is the size field for a word/doubleword str/ldr from the data register.
func wsize(rt reg) uint32 {
	if rt.is64 {
		return 3
	}
	return 2
}

func encodeLoadStore(mnem string, ops []string) (uint32, error) {
	if len(ops) < 2 || len(ops) > 3 {
		return 0, fmt.Errorf("expected 2-3 operands")
	}
	rt, ok := parseReg(ops[0])
	if !ok {
		return 0, fmt.Errorf("bad register operand %q", ops[0])
	}
	size, opc, unscaled, ok := lsForm(mnem, rt)
	if !ok {
		return 0, fmt.Errorf("unknown load/store %q", mnem)
	}
	scale := int64(1) << size

	// Post-index "[Xn], #imm" arrives as two operands: the bare base and the imm.
	if len(ops) == 3 {
		base, ok := bareBase(ops[1])
		if !ok {
			return 0, fmt.Errorf("post-index needs [Xn], #imm; got %q", ops[1])
		}
		imm, ok := parseImm(ops[2])
		if !ok {
			return 0, fmt.Errorf("bad post-index immediate %q", ops[2])
		}
		return lsImm9(size, opc, base, rt, imm, 0b01)
	}

	mem := strings.TrimSpace(ops[1])
	// Pre-index "[Xn, #imm]!"
	if strings.HasSuffix(mem, "!") {
		base, imm, ok := baseDisp(strings.TrimSuffix(mem, "!"))
		if !ok {
			return 0, fmt.Errorf("bad pre-index operand %q", mem)
		}
		return lsImm9(size, opc, base, rt, imm, 0b11)
	}
	if !strings.HasPrefix(mem, "[") || !strings.HasSuffix(mem, "]") {
		return 0, fmt.Errorf("bad memory operand %q", mem)
	}
	inner := strings.TrimSpace(mem[1 : len(mem)-1])
	parts := splitOperands(inner)
	base, ok := parseReg(strings.TrimSpace(parts[0]))
	if !ok || !base.is64 {
		return 0, fmt.Errorf("bad base register in %q", mem)
	}
	if len(parts) == 1 { // [Xn]
		if unscaled {
			return lsImm9(size, opc, base, rt, 0, 0b00)
		}
		return lsUImm(size, opc, base, rt, 0, scale)
	}
	// Register offset if the second piece is a register, else an immediate.
	if _, isReg := parseReg(firstField(parts[1])); isReg {
		return lsRegOffset(size, opc, base, rt, parts[1:])
	}
	imm, ok := parseImm(parts[1])
	if !ok {
		return 0, fmt.Errorf("bad offset in %q", mem)
	}
	if !unscaled && imm >= 0 && imm%scale == 0 && imm/scale <= 0xFFF {
		return lsUImm(size, opc, base, rt, imm, scale)
	}
	return lsImm9(size, opc, base, rt, imm, 0b00) // unscaled fallback (STUR-style)
}

// lsUImm encodes the unsigned-offset form (bits[25:24]=01).
func lsUImm(size, opc uint32, base, rt reg, imm, scale int64) (uint32, error) {
	if imm < 0 || imm%scale != 0 || imm/scale > 0xFFF {
		return 0, fmt.Errorf("offset %d is not a non-negative multiple of %d within range", imm, scale)
	}
	return 0x39000000 | size<<30 | opc<<22 | uint32(imm/scale)<<10 | base.num<<5 | rt.num, nil
}

// lsImm9 encodes the unscaled / pre-index / post-index forms (bits[25:24]=00),
// mode selecting 00 unscaled, 01 post-index, 11 pre-index.
func lsImm9(size, opc uint32, base, rt reg, imm int64, mode uint32) (uint32, error) {
	if imm < -256 || imm > 255 {
		return 0, fmt.Errorf("offset %d out of signed 9-bit range", imm)
	}
	imm9 := uint32(imm&0x1FF) << 12
	return 0x38000000 | size<<30 | opc<<22 | imm9 | mode<<10 | base.num<<5 | rt.num, nil
}

// lsRegOffset encodes the register-offset form (bit21=1, bits[11:10]=10).
// extra is the operand pieces after the base: ["Xm"], ["Xm","lsl #3"],
// ["Wm","sxtw"], ["Wm","uxtw #2"], …
func lsRegOffset(size, opc uint32, base, rt reg, extra []string) (uint32, error) {
	idx, ok := parseReg(strings.TrimSpace(extra[0]))
	if !ok {
		return 0, fmt.Errorf("bad index register %q", extra[0])
	}
	option := uint32(0b011) // X index defaults to LSL/UXTX
	if !idx.is64 {
		option = 0b010 // W index defaults to UXTW
	}
	var s uint32
	if len(extra) >= 2 {
		f := strings.Fields(strings.ToLower(strings.TrimSpace(extra[1])))
		switch f[0] {
		case "lsl", "uxtx":
			option = 0b011
		case "uxtw":
			option = 0b010
		case "sxtw":
			option = 0b110
		case "sxtx":
			option = 0b111
		default:
			return 0, fmt.Errorf("bad index extend %q", f[0])
		}
		if len(f) >= 2 {
			amt, ok := parseImm(f[1])
			if !ok {
				return 0, fmt.Errorf("bad index shift amount %q", f[1])
			}
			switch {
			case amt == 0:
				s = 0
			case amt == int64(size):
				s = 1
			default:
				return 0, fmt.Errorf("index shift must be 0 or %d", size)
			}
		}
	}
	return 0x38200000 | size<<30 | opc<<22 | idx.num<<16 | option<<13 | s<<12 | 0b10<<10 | base.num<<5 | rt.num, nil
}

// bareBase parses a "[Xn]" operand (no offset) for post-index addressing.
func bareBase(s string) (reg, bool) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, "]") {
		return reg{}, false
	}
	r, ok := parseReg(strings.TrimSpace(s[1 : len(s)-1]))
	if !ok || !r.is64 {
		return reg{}, false
	}
	return r, true
}

// baseDisp parses a "[Xn, #imm]" operand into its base and displacement.
func baseDisp(s string) (reg, int64, bool) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, "]") {
		return reg{}, 0, false
	}
	parts := splitOperands(strings.TrimSpace(s[1 : len(s)-1]))
	if len(parts) != 2 {
		return reg{}, 0, false
	}
	r, ok := parseReg(strings.TrimSpace(parts[0]))
	if !ok || !r.is64 {
		return reg{}, 0, false
	}
	imm, ok := parseImm(parts[1])
	if !ok {
		return reg{}, 0, false
	}
	return r, imm, true
}

// firstField returns the first whitespace-delimited token of s.
func firstField(s string) string {
	f := strings.Fields(strings.TrimSpace(s))
	if len(f) == 0 {
		return ""
	}
	return f[0]
}
