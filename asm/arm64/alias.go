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
	}
	return mnem, ops, false
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
