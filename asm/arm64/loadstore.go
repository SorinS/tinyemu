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
	var rt reg
	var size, opc, vbit uint32
	var unscaled bool
	var scale int64
	if fp, isFP := parseFPReg(ops[0]); isFP {
		// FP/SIMD load/store: V=1, size/opc/scale from the register width.
		var ok bool
		if size, opc, scale, ok = fpLoadStoreForm(mnem, fp.size); !ok {
			return 0, fmt.Errorf("unsupported FP load/store %q %s", mnem, ops[0])
		}
		rt, vbit = reg{num: fp.num}, 1<<26
	} else {
		var ok bool
		if rt, ok = parseReg(ops[0]); !ok {
			return 0, fmt.Errorf("bad register operand %q", ops[0])
		}
		if size, opc, unscaled, ok = lsForm(mnem, rt); !ok {
			return 0, fmt.Errorf("unknown load/store %q", mnem)
		}
		scale = int64(1) << size
	}

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
		return lsImm9(vbit, size, opc, base, rt, imm, 0b01)
	}

	mem := strings.TrimSpace(ops[1])
	// Pre-index "[Xn, #imm]!"
	if strings.HasSuffix(mem, "!") {
		base, imm, ok := baseDisp(strings.TrimSuffix(mem, "!"))
		if !ok {
			return 0, fmt.Errorf("bad pre-index operand %q", mem)
		}
		return lsImm9(vbit, size, opc, base, rt, imm, 0b11)
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
			return lsImm9(vbit, size, opc, base, rt, 0, 0b00)
		}
		return lsUImm(vbit, size, opc, base, rt, 0, scale)
	}
	// Register offset if the second piece is a register, else an immediate.
	if _, isReg := parseReg(firstField(parts[1])); isReg {
		return lsRegOffset(vbit, size, opc, base, rt, parts[1:])
	}
	imm, ok := parseImm(parts[1])
	if !ok {
		return 0, fmt.Errorf("bad offset in %q", mem)
	}
	if !unscaled && imm >= 0 && imm%scale == 0 && imm/scale <= 0xFFF {
		return lsUImm(vbit, size, opc, base, rt, imm, scale)
	}
	return lsImm9(vbit, size, opc, base, rt, imm, 0b00) // unscaled fallback (STUR-style)
}

// fpLoadStoreForm gives the size/opc fields and the access scale for a ldr/str
// of a B/H/S/D/Q register. Q (128-bit) uses size=00 with opc bit set.
func fpLoadStoreForm(mnem string, fpSize int) (size, opc uint32, scale int64, ok bool) {
	load := mnem == "ldr"
	if mnem != "ldr" && mnem != "str" {
		return 0, 0, 0, false
	}
	switch fpSize {
	case 8:
		size, scale = 0, 1
	case 16:
		size, scale = 1, 2
	case 32:
		size, scale = 2, 4
	case 64:
		size, scale = 3, 8
	case 128: // Q: size=00, opc = 11 (ldr) / 10 (str)
		if load {
			return 0, 0b11, 16, true
		}
		return 0, 0b10, 16, true
	default:
		return 0, 0, 0, false
	}
	if load {
		opc = 0b01
	}
	return size, opc, scale, true
}

// lsUImm encodes the unsigned-offset form (bits[25:24]=01); v is the SIMD&FP bit.
func lsUImm(v, size, opc uint32, base, rt reg, imm, scale int64) (uint32, error) {
	if imm < 0 || imm%scale != 0 || imm/scale > 0xFFF {
		return 0, fmt.Errorf("offset %d is not a non-negative multiple of %d within range", imm, scale)
	}
	return 0x39000000 | v | size<<30 | opc<<22 | uint32(imm/scale)<<10 | base.num<<5 | rt.num, nil
}

// lsImm9 encodes the unscaled / pre-index / post-index forms (bits[25:24]=00),
// mode selecting 00 unscaled, 01 post-index, 11 pre-index; v is the SIMD&FP bit.
func lsImm9(v, size, opc uint32, base, rt reg, imm int64, mode uint32) (uint32, error) {
	if imm < -256 || imm > 255 {
		return 0, fmt.Errorf("offset %d out of signed 9-bit range", imm)
	}
	imm9 := uint32(imm&0x1FF) << 12
	return 0x38000000 | v | size<<30 | opc<<22 | imm9 | mode<<10 | base.num<<5 | rt.num, nil
}

// lsRegOffset encodes the register-offset form (bit21=1, bits[11:10]=10);
// v is the SIMD&FP bit. extra is the operand pieces after the base.
func lsRegOffset(v, size, opc uint32, base, rt reg, extra []string) (uint32, error) {
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
	return 0x38200000 | v | size<<30 | opc<<22 | idx.num<<16 | option<<13 | s<<12 | 0b10<<10 | base.num<<5 | rt.num, nil
}

// encodePair encodes the load/store pair instructions (ldp/stp/ldpsw). imm is
// a signed 7-bit field scaled by the access size; the three addressing modes
// (offset/pre-index/post-index) are picked by syntax exactly as for the
// single-register forms.
func encodePair(mnem string, ops []string) (uint32, error) {
	if len(ops) < 3 || len(ops) > 4 {
		return 0, fmt.Errorf("expected 3-4 operands")
	}
	rt, ok1 := parseReg(ops[0])
	rt2, ok2 := parseReg(ops[1])
	if !ok1 || !ok2 {
		return 0, fmt.Errorf("bad register operand")
	}
	var opc, l uint32
	scale := int64(8)
	switch mnem {
	case "stp":
		if !rt.is64 {
			opc, scale = 0, 4
		} else {
			opc = 2
		}
	case "ldp":
		l = 1
		if !rt.is64 {
			opc, scale = 0, 4
		} else {
			opc = 2
		}
	case "ldpsw": // sign-extending word pair: 32-bit access, 64-bit destinations
		opc, l, scale = 1, 1, 4
	default:
		return 0, fmt.Errorf("unknown pair op %q", mnem)
	}

	var mode uint32
	var base reg
	var imm int64
	if len(ops) == 4 { // post-index: Rt, Rt2, [Xn], #imm
		b, ok := bareBase(ops[2])
		if !ok {
			return 0, fmt.Errorf("post-index needs [Xn], #imm; got %q", ops[2])
		}
		v, ok := parseImm(ops[3])
		if !ok {
			return 0, fmt.Errorf("bad post-index immediate %q", ops[3])
		}
		mode, base, imm = 0b001, b, v
	} else {
		mem := strings.TrimSpace(ops[2])
		if strings.HasSuffix(mem, "!") { // pre-index
			b, v, ok := baseDisp(strings.TrimSuffix(mem, "!"))
			if !ok {
				return 0, fmt.Errorf("bad pre-index operand %q", mem)
			}
			mode, base, imm = 0b011, b, v
		} else { // signed offset ([Xn] or [Xn, #imm])
			b, v, ok := baseMaybeDisp(mem)
			if !ok {
				return 0, fmt.Errorf("bad memory operand %q", mem)
			}
			mode, base, imm = 0b010, b, v
		}
	}
	if imm%scale != 0 {
		return 0, fmt.Errorf("pair offset %d not a multiple of %d", imm, scale)
	}
	off := imm / scale
	if off < -64 || off > 63 {
		return 0, fmt.Errorf("pair offset %d out of range", imm)
	}
	imm7 := uint32(off & 0x7F)
	return opc<<30 | 0b101<<27 | mode<<23 | l<<22 | imm7<<15 | rt2.num<<10 | base.num<<5 | rt.num, nil
}

// baseMaybeDisp parses "[Xn]" or "[Xn, #imm]" (offset defaults to 0).
func baseMaybeDisp(s string) (reg, int64, bool) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, "]") {
		return reg{}, 0, false
	}
	parts := splitOperands(strings.TrimSpace(s[1 : len(s)-1]))
	b, ok := parseReg(strings.TrimSpace(parts[0]))
	if !ok || !b.is64 {
		return reg{}, 0, false
	}
	if len(parts) == 1 {
		return b, 0, true
	}
	if len(parts) != 2 {
		return reg{}, 0, false
	}
	imm, ok := parseImm(parts[1])
	if !ok {
		return reg{}, 0, false
	}
	return b, imm, true
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
