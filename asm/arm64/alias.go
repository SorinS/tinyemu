package arm64

// AArch64 has a layer of assembler aliases that rewrite to a base instruction
// with the zero register wired in. expandAlias performs that rewrite before
// encoding; ok=false means the mnemonic is not an alias (encode it directly).
//
// Covered here: cmp/cmn/tst (flag-setting with the result discarded to ZR),
// neg/negs/mvn (a source from ZR), and the register form of mov. The
// immediate form of mov (which must pick movz/movn/orr) is a separate concern
// and is not handled here.

// zeroReg returns the zero-register name of the given width.
func zeroReg(is64 bool) string {
	if is64 {
		return "xzr"
	}
	return "wzr"
}

func expandAlias(mnem string, ops []string) (string, []string, bool) {
	switch mnem {
	case "mov":
		// Register move only. mov Xd,Xm = orr Xd,xzr,Xm, except when SP is
		// involved (ZR is unrepresentable there) where it is add Xd,Xm,#0.
		if len(ops) == 2 {
			rd, okd := parseReg(ops[0])
			rm, okm := parseReg(ops[1])
			if okd && okm {
				if rd.isSP || rm.isSP {
					return "add", []string{ops[0], ops[1], "#0"}, true
				}
				return "orr", []string{ops[0], zeroReg(rd.is64), ops[1]}, true
			}
		}
	case "cmp": // cmp Xn,op = subs ZR,Xn,op
		if d, ok := discardDest(ops); ok {
			return "subs", d, true
		}
	case "cmn": // cmn Xn,op = adds ZR,Xn,op
		if d, ok := discardDest(ops); ok {
			return "adds", d, true
		}
	case "tst": // tst Xn,op = ands ZR,Xn,op
		if d, ok := discardDest(ops); ok {
			return "ands", d, true
		}
	case "neg": // neg Xd,op = sub Xd,ZR,op
		if d, ok := zeroSource(ops); ok {
			return "sub", d, true
		}
	case "negs": // negs Xd,op = subs Xd,ZR,op
		if d, ok := zeroSource(ops); ok {
			return "subs", d, true
		}
	case "mvn": // mvn Xd,op = orn Xd,ZR,op
		if d, ok := zeroSource(ops); ok {
			return "orn", d, true
		}
	case "mul": // mul Xd,Xn,Xm = madd Xd,Xn,Xm,ZR
		if d, ok := zeroAccum(ops); ok {
			return "madd", d, true
		}
	case "mneg": // mneg Xd,Xn,Xm = msub Xd,Xn,Xm,ZR
		if d, ok := zeroAccum(ops); ok {
			return "msub", d, true
		}
	case "smull": // smull Xd,Wn,Wm = smaddl Xd,Wn,Wm,XZR
		if len(ops) == 3 {
			return "smaddl", []string{ops[0], ops[1], ops[2], "xzr"}, true
		}
	case "umull":
		if len(ops) == 3 {
			return "umaddl", []string{ops[0], ops[1], ops[2], "xzr"}, true
		}
	case "smnegl":
		if len(ops) == 3 {
			return "smsubl", []string{ops[0], ops[1], ops[2], "xzr"}, true
		}
	case "umnegl":
		if len(ops) == 3 {
			return "umsubl", []string{ops[0], ops[1], ops[2], "xzr"}, true
		}
	case "lsl", "lsr", "asr": // register form → 2-source; immediate form → bitfield
		if len(ops) == 3 {
			if _, isReg := parseReg(ops[2]); isReg {
				return map[string]string{"lsl": "lslv", "lsr": "lsrv", "asr": "asrv"}[mnem], ops, true
			}
			return expandBitfieldAlias(mnem, ops)
		}
	case "ror": // register form → rorv; immediate form → extr Xd,Xn,Xn,#imm
		if len(ops) == 3 {
			if _, isReg := parseReg(ops[2]); isReg {
				return "rorv", ops, true
			}
			return "extr", []string{ops[0], ops[1], ops[1], ops[2]}, true
		}
	case "ubfx", "sbfx", "bfxil", "ubfiz", "sbfiz", "bfi",
		"uxtb", "uxth", "sxtb", "sxth", "sxtw":
		return expandBitfieldAlias(mnem, ops)
	case "cset": // cset Xd, cond = csinc Xd, ZR, ZR, invert(cond)
		if d, ok := csetForm("csinc", ops); ok {
			return "csinc", d, true
		}
	case "csetm": // csetm Xd, cond = csinv Xd, ZR, ZR, invert(cond)
		if d, ok := csetForm("csinv", ops); ok {
			return "csinv", d, true
		}
	case "cinc": // cinc Xd, Xn, cond = csinc Xd, Xn, Xn, invert(cond)
		if d, ok := cFromN(ops); ok {
			return "csinc", d, true
		}
	case "cinv":
		if d, ok := cFromN(ops); ok {
			return "csinv", d, true
		}
	case "cneg":
		if d, ok := cFromN(ops); ok {
			return "csneg", d, true
		}
	}
	return mnem, ops, false
}

// invertCond returns the condition whose code is the given condition's code
// with its low bit flipped (eq<->ne, lt<->ge, …).
func invertCond(s string) (string, bool) {
	c, ok := condCodes[normCond(s)]
	if !ok {
		return "", false
	}
	return condNames[c^1], true
}

// csetForm builds "Xd, ZR, ZR, invert(cond)" for cset/csetm.
func csetForm(_ string, ops []string) ([]string, bool) {
	if len(ops) != 2 {
		return nil, false
	}
	rd, ok := parseReg(ops[0])
	inv, ok2 := invertCond(ops[1])
	if !ok || !ok2 {
		return nil, false
	}
	z := zeroReg(rd.is64)
	return []string{ops[0], z, z, inv}, true
}

// cFromN builds "Xd, Xn, Xn, invert(cond)" for cinc/cinv/cneg.
func cFromN(ops []string) ([]string, bool) {
	if len(ops) != 3 {
		return nil, false
	}
	inv, ok := invertCond(ops[2])
	if !ok {
		return nil, false
	}
	return []string{ops[0], ops[1], ops[1], inv}, true
}

// zeroAccum appends the zero register (of the destination's width) as the
// accumulator: "Xd,Xn,Xm" → "Xd,Xn,Xm,ZR". For mul/mneg.
func zeroAccum(ops []string) ([]string, bool) {
	if len(ops) != 3 {
		return nil, false
	}
	rd, ok := parseReg(ops[0])
	if !ok {
		return nil, false
	}
	return []string{ops[0], ops[1], ops[2], zeroReg(rd.is64)}, true
}

// discardDest prepends the zero register (of the first operand's width) as the
// destination: "Xn, ..." → "ZR, Xn, ...". For cmp/cmn/tst.
func discardDest(ops []string) ([]string, bool) {
	if len(ops) < 2 {
		return nil, false
	}
	rn, ok := parseReg(ops[0])
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(ops)+1)
	out = append(out, zeroReg(rn.is64))
	out = append(out, ops...)
	return out, true
}

// zeroSource inserts the zero register (of the destination's width) as the
// first source: "Xd, op..." → "Xd, ZR, op...". For neg/negs/mvn.
func zeroSource(ops []string) ([]string, bool) {
	if len(ops) < 2 {
		return nil, false
	}
	rd, ok := parseReg(ops[0])
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(ops)+1)
	out = append(out, ops[0], zeroReg(rd.is64))
	out = append(out, ops[1:]...)
	return out, true
}
