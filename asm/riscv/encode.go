package riscv

import (
	"fmt"
	"strconv"
	"strings"
)

// Assemble encodes a single RISC-V instruction (RV64I + M) to its 4 little-
// endian bytes. PC-relative operands (branches, jal) take a numeric byte
// offset; label resolution is a program-level concern (see AssembleProgram).
func Assemble(src string) ([]byte, error) {
	mnem, ops := parseLine(src)
	if mnem == "" {
		return nil, nil
	}
	mnem, ops = expandPseudo(mnem, ops)

	// Floating-point (F/D) instructions are in a separate table.
	if fp, ok := fpByName[mnem]; ok {
		w, err := encodeFP(fp, ops)
		if err != nil {
			return nil, fmt.Errorf("riscv %q: %w", src, err)
		}
		return []byte{byte(w), byte(w >> 8), byte(w >> 16), byte(w >> 24)}, nil
	}

	// An atomic mnemonic may carry an .aq / .rl / .aqrl ordering suffix.
	aq, rl := false, false
	if _, ok := byName[mnem]; !ok {
		if base, a, r := splitAQRL(mnem); a || r {
			if bi, ok := byName[base]; ok && (bi.format == fmtAtomic || bi.format == fmtAtomicLR) {
				mnem, aq, rl = base, a, r
			}
		}
	}

	in, ok := byName[mnem]
	if !ok {
		return nil, fmt.Errorf("riscv: unknown instruction %q", mnem)
	}
	w, err := encode(in, ops)
	if err != nil {
		return nil, fmt.Errorf("riscv %q: %w", src, err)
	}
	if aq {
		w |= 1 << 26
	}
	if rl {
		w |= 1 << 25
	}
	return []byte{byte(w), byte(w >> 8), byte(w >> 16), byte(w >> 24)}, nil
}

// parseLine splits a source line into a lower-case mnemonic and operand
// strings, stripping a '#' or ';' comment.
func parseLine(src string) (mnem string, ops []string) {
	if i := strings.IndexAny(src, "#;"); i >= 0 {
		src = src[:i]
	}
	src = strings.TrimSpace(src)
	if src == "" {
		return "", nil
	}
	sp := strings.IndexAny(src, " \t")
	if sp < 0 {
		return strings.ToLower(src), nil
	}
	mnem = strings.ToLower(src[:sp])
	for _, o := range strings.Split(src[sp+1:], ",") {
		if o = strings.TrimSpace(o); o != "" {
			ops = append(ops, o)
		}
	}
	return mnem, ops
}

func encode(in *insn, ops []string) (uint32, error) {
	base := in.opcode | in.funct3<<12
	switch in.format {
	case fmtNone:
		if len(ops) != 0 {
			return 0, fmt.Errorf("%s takes no operands", in.name)
		}
		return in.opcode | in.funct3<<12 | in.funct7<<20, nil

	case fmtR:
		rd, rs1, rs2, err := three(ops)
		if err != nil {
			return 0, err
		}
		return base | in.funct7<<25 | u(rs2)<<20 | u(rs1)<<15 | u(rd)<<7, nil

	case fmtI:
		rd, rs1, imm, err := regRegImm(ops, 12)
		if err != nil {
			return 0, err
		}
		return base | uint32(imm&0xFFF)<<20 | u(rs1)<<15 | u(rd)<<7, nil

	case fmtIShift:
		rd, rs1, sh, err := regRegImm(ops, 0)
		if err != nil {
			return 0, err
		}
		if in.opcode == 0x13 { // RV64 shift: 6-bit shamt, funct6 in [31:26]
			if sh < 0 || sh > 63 {
				return 0, fmt.Errorf("shift amount %d out of range 0..63", sh)
			}
			return base | in.funct7<<26 | uint32(sh)<<20 | u(rs1)<<15 | u(rd)<<7, nil
		}
		// word shift (opcode 0x1B): 5-bit shamt, funct7 in [31:25]
		if sh < 0 || sh > 31 {
			return 0, fmt.Errorf("shift amount %d out of range 0..31", sh)
		}
		return base | in.funct7<<25 | uint32(sh)<<20 | u(rs1)<<15 | u(rd)<<7, nil

	case fmtILoad, fmtIJalr:
		rd, imm, rs1, err := regMem(ops)
		if err != nil {
			return 0, err
		}
		if !fits(imm, 12) {
			return 0, fmt.Errorf("offset %d out of 12-bit range", imm)
		}
		return base | uint32(imm&0xFFF)<<20 | u(rs1)<<15 | u(rd)<<7, nil

	case fmtS:
		rs2, imm, rs1, err := regMem(ops)
		if err != nil {
			return 0, err
		}
		if !fits(imm, 12) {
			return 0, fmt.Errorf("offset %d out of 12-bit range", imm)
		}
		hi := uint32(imm>>5) & 0x7F
		lo := uint32(imm) & 0x1F
		return base | hi<<25 | u(rs2)<<20 | u(rs1)<<15 | lo<<7, nil

	case fmtB:
		rs1, rs2, imm, err := regRegImm(ops, 13)
		if err != nil {
			return 0, err
		}
		if imm&1 != 0 {
			return 0, fmt.Errorf("branch offset %d must be even", imm)
		}
		return base | bit(imm, 12)<<31 | bits(imm, 10, 5)<<25 | u(rs2)<<20 |
			u(rs1)<<15 | bits(imm, 4, 1)<<8 | bit(imm, 11)<<7, nil

	case fmtU:
		rd, imm, err := regImm(ops, 20)
		if err != nil {
			return 0, err
		}
		return in.opcode | (uint32(imm)&0xFFFFF)<<12 | u(rd)<<7, nil

	case fmtJ:
		rd, imm, err := regImm(ops, 21)
		if err != nil {
			return 0, err
		}
		if imm&1 != 0 {
			return 0, fmt.Errorf("jump offset %d must be even", imm)
		}
		return in.opcode | bit(imm, 20)<<31 | bits(imm, 10, 1)<<21 | bit(imm, 11)<<20 |
			bits(imm, 19, 12)<<12 | u(rd)<<7, nil

	case fmtCSR:
		if len(ops) != 3 {
			return 0, fmt.Errorf("%s: want rd, csr, rs1", in.name)
		}
		rd, err := reg(ops[0])
		if err != nil {
			return 0, err
		}
		csr, ok := parseCSR(ops[1])
		if !ok {
			return 0, fmt.Errorf("bad CSR %q", ops[1])
		}
		rs1, err := reg(ops[2])
		if err != nil {
			return 0, err
		}
		return base | csr<<20 | u(rs1)<<15 | u(rd)<<7, nil

	case fmtCSRI:
		if len(ops) != 3 {
			return 0, fmt.Errorf("%s: want rd, csr, uimm", in.name)
		}
		rd, err := reg(ops[0])
		if err != nil {
			return 0, err
		}
		csr, ok := parseCSR(ops[1])
		if !ok {
			return 0, fmt.Errorf("bad CSR %q", ops[1])
		}
		uimm, err := parseImm(ops[2])
		if err != nil {
			return 0, err
		}
		if uimm < 0 || uimm > 31 {
			return 0, fmt.Errorf("csr uimm %d out of 0..31", uimm)
		}
		return base | csr<<20 | uint32(uimm)<<15 | u(rd)<<7, nil

	case fmtFence:
		pred, succ := uint32(0xF), uint32(0xF) // default = fence iorw, iorw
		if len(ops) == 2 {
			var ok bool
			if pred, ok = parseFenceFlags(ops[0]); !ok {
				return 0, fmt.Errorf("bad fence predecessor %q", ops[0])
			}
			if succ, ok = parseFenceFlags(ops[1]); !ok {
				return 0, fmt.Errorf("bad fence successor %q", ops[1])
			}
		} else if len(ops) != 0 {
			return 0, fmt.Errorf("fence: want no operands or pred, succ")
		}
		return base | (pred<<4|succ)<<20, nil

	case fmtAtomic: // rd, rs2, (rs1)
		if len(ops) != 3 {
			return 0, fmt.Errorf("%s: want rd, rs2, (rs1)", in.name)
		}
		rd, err := reg(ops[0])
		if err != nil {
			return 0, err
		}
		rs2, err := reg(ops[1])
		if err != nil {
			return 0, err
		}
		off, rs1, err := parseMem(ops[2])
		if err != nil {
			return 0, err
		}
		if off != 0 {
			return 0, fmt.Errorf("%s: atomic address takes no offset", in.name)
		}
		return base | in.funct7<<27 | u(rs2)<<20 | u(rs1)<<15 | u(rd)<<7, nil

	case fmtAtomicLR: // rd, (rs1)
		if len(ops) != 2 {
			return 0, fmt.Errorf("%s: want rd, (rs1)", in.name)
		}
		rd, err := reg(ops[0])
		if err != nil {
			return 0, err
		}
		off, rs1, err := parseMem(ops[1])
		if err != nil {
			return 0, err
		}
		if off != 0 {
			return 0, fmt.Errorf("%s: atomic address takes no offset", in.name)
		}
		return base | in.funct7<<27 | u(rs1)<<15 | u(rd)<<7, nil
	}
	return 0, fmt.Errorf("unhandled format for %s", in.name)
}

func u(r int) uint32 { return uint32(r) & 0x1F }

// bit returns bit n of imm as a uint32 (0/1).
func bit(imm int64, n uint) uint32 { return uint32((imm >> n) & 1) }

// bits returns imm[hi:lo] as a uint32, right-aligned.
func bits(imm int64, hi, lo uint) uint32 {
	return uint32((imm >> lo) & ((1 << (hi - lo + 1)) - 1))
}

// fits reports whether v fits in a signed n-bit field.
func fits(v int64, n uint) bool {
	lo := int64(-1) << (n - 1)
	hi := int64(1)<<(n-1) - 1
	return v >= lo && v <= hi
}

// --- operand shapes ---------------------------------------------------------

func three(ops []string) (a, b, c int, err error) {
	if len(ops) != 3 {
		return 0, 0, 0, fmt.Errorf("want 3 register operands, got %d", len(ops))
	}
	if a, err = reg(ops[0]); err != nil {
		return
	}
	if b, err = reg(ops[1]); err != nil {
		return
	}
	c, err = reg(ops[2])
	return
}

// regRegImm parses "rd, rs1, imm". checkBits>0 enforces the immediate fits a
// signed field of that width.
func regRegImm(ops []string, checkBits uint) (a, b int, imm int64, err error) {
	if len(ops) != 3 {
		return 0, 0, 0, fmt.Errorf("want rd, rs1, imm; got %d operands", len(ops))
	}
	if a, err = reg(ops[0]); err != nil {
		return
	}
	if b, err = reg(ops[1]); err != nil {
		return
	}
	if imm, err = parseImm(ops[2]); err != nil {
		return
	}
	if checkBits > 0 && !fits(imm, checkBits) {
		err = fmt.Errorf("immediate %d out of %d-bit range", imm, checkBits)
	}
	return
}

// regImm parses "rd, imm".
func regImm(ops []string, checkBits uint) (a int, imm int64, err error) {
	if len(ops) != 2 {
		return 0, 0, fmt.Errorf("want rd, imm; got %d operands", len(ops))
	}
	if a, err = reg(ops[0]); err != nil {
		return
	}
	if imm, err = parseImm(ops[1]); err != nil {
		return
	}
	// U-type imm is a 20-bit unsigned upper field; J-type is signed.
	if checkBits == 20 {
		if imm < 0 || imm > 0xFFFFF {
			err = fmt.Errorf("U-immediate %d out of 0..0xFFFFF", imm)
		}
	} else if checkBits > 0 && !fits(imm, checkBits) {
		err = fmt.Errorf("immediate %d out of %d-bit range", imm, checkBits)
	}
	return
}

// regMem parses "reg, offset(base)" → (reg, offset, base).
func regMem(ops []string) (r int, off int64, base int, err error) {
	if len(ops) != 2 {
		return 0, 0, 0, fmt.Errorf("want reg, offset(base); got %d operands", len(ops))
	}
	if r, err = reg(ops[0]); err != nil {
		return
	}
	off, base, err = parseMem(ops[1])
	return
}

func reg(s string) (int, error) {
	r, ok := parseReg(s)
	if !ok {
		return 0, fmt.Errorf("bad register %q", s)
	}
	return r, nil
}

// parseMem parses "offset(base)" (offset optional → 0).
func parseMem(s string) (off int64, base int, err error) {
	s = strings.TrimSpace(s)
	open := strings.IndexByte(s, '(')
	if open < 0 || !strings.HasSuffix(s, ")") {
		return 0, 0, fmt.Errorf("bad memory operand %q (want offset(base))", s)
	}
	offStr := strings.TrimSpace(s[:open])
	baseStr := s[open+1 : len(s)-1]
	if offStr == "" {
		off = 0
	} else if off, err = parseImm(offStr); err != nil {
		return
	}
	base, err = reg(baseStr)
	return
}

// splitAQRL strips a trailing .aq / .rl / .aqrl ordering suffix from an atomic
// mnemonic, returning the base name and the aq/rl flags.
func splitAQRL(mnem string) (base string, aq, rl bool) {
	switch {
	case strings.HasSuffix(mnem, ".aqrl"):
		return mnem[:len(mnem)-5], true, true
	case strings.HasSuffix(mnem, ".aq"):
		return mnem[:len(mnem)-3], true, false
	case strings.HasSuffix(mnem, ".rl"):
		return mnem[:len(mnem)-3], false, true
	}
	return mnem, false, false
}

// parseFenceFlags parses a fence ordering set like "rw" or "iorw" into its
// 4-bit value (i=8, o=4, r=2, w=1).
func parseFenceFlags(s string) (uint32, bool) {
	s = strings.TrimSpace(strings.ToLower(s))
	var v uint32
	for _, c := range s {
		switch c {
		case 'i':
			v |= 8
		case 'o':
			v |= 4
		case 'r':
			v |= 2
		case 'w':
			v |= 1
		default:
			return 0, false
		}
	}
	return v, true
}

// parseImm parses a signed decimal or hex (0x) immediate.
func parseImm(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty immediate")
	}
	neg := false
	if strings.HasPrefix(s, "-") {
		neg, s = true, s[1:]
	} else if strings.HasPrefix(s, "+") {
		s = s[1:]
	}
	var v int64
	var err error
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		var uv uint64
		uv, err = strconv.ParseUint(s[2:], 16, 64)
		v = int64(uv)
	} else {
		v, err = strconv.ParseInt(s, 10, 64)
	}
	if err != nil {
		return 0, fmt.Errorf("bad immediate %q", s)
	}
	if neg {
		v = -v
	}
	return v, nil
}
