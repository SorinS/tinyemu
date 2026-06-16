package arm64

import "fmt"

// Bitfield moves (sbfm/bfm/ubfm) and extract (extr), plus the large family of
// aliases that real code is written in (lsl/lsr/asr-immediate, ubfx/sbfx/bfi/
// bfxil/ubfiz/sbfiz, uxtb/sxtw/…, ror-immediate). The aliases are arithmetic
// rewrites onto these base instructions — see expandBitfieldAlias.

// encodeBitfield encodes sbfm/bfm/ubfm: Rd, Rn, #immr, #imms. N tracks the
// register width (bitfield requires N == sf).
func encodeBitfield(mnem string, ops []string) (uint32, error) {
	if len(ops) != 4 {
		return 0, fmt.Errorf("%s expects 4 operands", mnem)
	}
	rd, ok1 := parseReg(ops[0])
	rn, ok2 := parseReg(ops[1])
	if !ok1 || !ok2 {
		return 0, fmt.Errorf("bad register operand")
	}
	immr, ok3 := parseImm(ops[2])
	imms, ok4 := parseImm(ops[3])
	if !ok3 || !ok4 {
		return 0, fmt.Errorf("bad immediate")
	}
	hi := int64(63)
	if !rd.is64 {
		hi = 31
	}
	if immr < 0 || immr > hi || imms < 0 || imms > hi {
		return 0, fmt.Errorf("bitfield immediate out of range")
	}
	var opc uint32
	switch mnem {
	case "sbfm":
		opc = 0
	case "bfm":
		opc = 1
	case "ubfm":
		opc = 2
	default:
		return 0, fmt.Errorf("unknown bitfield %q", mnem)
	}
	var n uint32
	if rd.is64 {
		n = 1
	}
	return sfBit(rd) | opc<<29 | 0x13000000 | n<<22 | uint32(immr)<<16 | uint32(imms)<<10 | rn.num<<5 | rd.num, nil
}

// encodeExtr encodes extr: Rd, Rn, Rm, #lsb.
func encodeExtr(ops []string) (uint32, error) {
	if len(ops) != 4 {
		return 0, fmt.Errorf("extr expects 4 operands")
	}
	rd, ok1 := parseReg(ops[0])
	rn, ok2 := parseReg(ops[1])
	rm, ok3 := parseReg(ops[2])
	if !ok1 || !ok2 || !ok3 {
		return 0, fmt.Errorf("bad register operand")
	}
	lsb, ok4 := parseImm(ops[3])
	hi := int64(63)
	if !rd.is64 {
		hi = 31
	}
	if !ok4 || lsb < 0 || lsb > hi {
		return 0, fmt.Errorf("bad extract lsb")
	}
	var n uint32
	if rd.is64 {
		n = 1
	}
	return sfBit(rd) | 0x13800000 | n<<22 | rm.num<<16 | uint32(lsb)<<10 | rn.num<<5 | rd.num, nil
}

// regWidth returns 64 or 32 for a register operand string.
func regWidth(s string) (int64, bool) {
	r, ok := parseReg(s)
	if !ok {
		return 0, false
	}
	if r.is64 {
		return 64, true
	}
	return 32, true
}

// expandBitfieldAlias rewrites a bitfield alias into its sbfm/bfm/ubfm form
// with computed immr/imms. ok=false if mnem is not such an alias.
func expandBitfieldAlias(mnem string, ops []string) (string, []string, bool) {
	imm := func(v int64) string { return fmt.Sprintf("#%d", v) }

	switch mnem {
	case "lsl", "lsr", "asr": // immediate forms (register forms handled elsewhere)
		if len(ops) != 3 {
			return "", nil, false
		}
		r, ok := regWidth(ops[0])
		shift, ok2 := parseImm(ops[2])
		if !ok || !ok2 {
			return "", nil, false
		}
		switch mnem {
		case "lsl":
			return "ubfm", []string{ops[0], ops[1], imm((r - shift) & (r - 1)), imm(r - 1 - shift)}, true
		case "lsr":
			return "ubfm", []string{ops[0], ops[1], imm(shift), imm(r - 1)}, true
		case "asr":
			return "sbfm", []string{ops[0], ops[1], imm(shift), imm(r - 1)}, true
		}
	case "ubfx", "sbfx", "bfxil", "ubfiz", "sbfiz", "bfi":
		if len(ops) != 4 {
			return "", nil, false
		}
		r, ok := regWidth(ops[0])
		lsb, ok2 := parseImm(ops[2])
		width, ok3 := parseImm(ops[3])
		if !ok || !ok2 || !ok3 || width < 1 {
			return "", nil, false
		}
		switch mnem {
		case "ubfx":
			return "ubfm", []string{ops[0], ops[1], imm(lsb), imm(lsb + width - 1)}, true
		case "sbfx":
			return "sbfm", []string{ops[0], ops[1], imm(lsb), imm(lsb + width - 1)}, true
		case "bfxil":
			return "bfm", []string{ops[0], ops[1], imm(lsb), imm(lsb + width - 1)}, true
		case "ubfiz":
			return "ubfm", []string{ops[0], ops[1], imm((r - lsb) & (r - 1)), imm(width - 1)}, true
		case "sbfiz":
			return "sbfm", []string{ops[0], ops[1], imm((r - lsb) & (r - 1)), imm(width - 1)}, true
		case "bfi":
			return "bfm", []string{ops[0], ops[1], imm((r - lsb) & (r - 1)), imm(width - 1)}, true
		}
	case "uxtb", "uxth", "sxtb", "sxth", "sxtw":
		if len(ops) != 2 {
			return "", nil, false
		}
		switch mnem {
		case "uxtb":
			return "ubfm", []string{ops[0], ops[1], "#0", "#7"}, true
		case "uxth":
			return "ubfm", []string{ops[0], ops[1], "#0", "#15"}, true
		case "sxtb":
			return "sbfm", []string{ops[0], ops[1], "#0", "#7"}, true
		case "sxth":
			return "sbfm", []string{ops[0], ops[1], "#0", "#15"}, true
		case "sxtw":
			return "sbfm", []string{ops[0], ops[1], "#0", "#31"}, true
		}
	}
	return "", nil, false
}
