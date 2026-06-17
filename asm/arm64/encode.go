package arm64

import (
	"fmt"
	"strings"
)

// class is an AArch64 instruction encoding class. Unlike RISC-V's handful of
// uniform formats, each class here has its own field layout and encoder.
type class int

const (
	clsAddSub     class = iota // add/sub/adds/subs — register OR immediate (picked by operand)
	clsLogicalReg              // and/orr/eor/ands/bic/orn/eon/bics — shifted register
	clsMoveWide                // movz/movn/movk
	clsLoadStore               // ldr/str unsigned-offset (x/w)
	clsBranch                  // b/bl (imm26)
	clsBranchCond              // b.<cond> (imm19)
	clsCompareBranch           // cbz/cbnz (imm19)
	clsBranchReg               // ret/br/blr
	clsPair                    // ldp/stp/ldpsw
	clsMul                     // madd/msub/smaddl/.../smulh/umulh (3-source)
	clsDataProc2               // udiv/sdiv/lslv/lsrv/asrv/rorv (2-source)
	clsDataProc1               // rbit/rev/rev16/rev32/clz/cls (1-source)
	clsBitfield                // ubfm/sbfm/bfm
	clsExtr                    // extr
	clsCondSel                 // csel/csinc/csinv/csneg
	clsAddr                    // adr/adrp
	clsAddSubCarry             // adc/adcs/sbc/sbcs
	clsCondCmp                 // ccmp/ccmn
	clsSystem                  // nop/hint/yield/wfe/wfi/sev/sevl/dmb/dsb/isb/mrs/msr
	clsException               // svc/hvc/smc/brk/hlt
)

// insn is one mnemonic's encoding facts. Which fields apply depends on class.
type insn struct {
	name  string
	class class
	op    uint32 // add/sub: 0=add 1=sub; load/store: 0=str 1=ldr; branch: 0=b 1=bl
	s     uint32 // add/sub: the S (flag-setting) bit
	opc   uint32 // logical: opc[1:0]; move-wide: opc[1:0]
	n     uint32 // logical: the N bit (bic/orn/eon/bics)
	base  uint32 // branch-reg: the full template word (Rn ORed in)
}

var table = []insn{
	// --- Add/subtract (shifted register and immediate share a mnemonic) ---
	{name: "add", class: clsAddSub, op: 0, s: 0},
	{name: "adds", class: clsAddSub, op: 0, s: 1},
	{name: "sub", class: clsAddSub, op: 1, s: 0},
	{name: "subs", class: clsAddSub, op: 1, s: 1},
	// --- Logical (shifted register) ---
	{name: "and", class: clsLogicalReg, opc: 0, n: 0},
	{name: "bic", class: clsLogicalReg, opc: 0, n: 1},
	{name: "orr", class: clsLogicalReg, opc: 1, n: 0},
	{name: "orn", class: clsLogicalReg, opc: 1, n: 1},
	{name: "eor", class: clsLogicalReg, opc: 2, n: 0},
	{name: "eon", class: clsLogicalReg, opc: 2, n: 1},
	{name: "ands", class: clsLogicalReg, opc: 3, n: 0},
	{name: "bics", class: clsLogicalReg, opc: 3, n: 1},
	// --- Move wide immediate ---
	{name: "movn", class: clsMoveWide, opc: 0},
	{name: "movz", class: clsMoveWide, opc: 2},
	{name: "movk", class: clsMoveWide, opc: 3},
	// --- Load/store register (size/sign variants; addressing in loadstore.go) ---
	{name: "str", class: clsLoadStore}, {name: "ldr", class: clsLoadStore},
	{name: "strb", class: clsLoadStore}, {name: "ldrb", class: clsLoadStore},
	{name: "strh", class: clsLoadStore}, {name: "ldrh", class: clsLoadStore},
	{name: "ldrsb", class: clsLoadStore}, {name: "ldrsh", class: clsLoadStore},
	{name: "ldrsw", class: clsLoadStore},
	{name: "stur", class: clsLoadStore}, {name: "ldur", class: clsLoadStore},
	{name: "sturb", class: clsLoadStore}, {name: "ldurb", class: clsLoadStore},
	{name: "sturh", class: clsLoadStore}, {name: "ldurh", class: clsLoadStore},
	{name: "ldursb", class: clsLoadStore}, {name: "ldursh", class: clsLoadStore},
	{name: "ldursw", class: clsLoadStore},
	// --- Load/store pair ---
	{name: "stp", class: clsPair}, {name: "ldp", class: clsPair},
	{name: "ldpsw", class: clsPair},
	// --- Data processing: 3-source (multiply family) ---
	{name: "madd", class: clsMul}, {name: "msub", class: clsMul},
	{name: "smaddl", class: clsMul}, {name: "smsubl", class: clsMul},
	{name: "umaddl", class: clsMul}, {name: "umsubl", class: clsMul},
	{name: "smulh", class: clsMul}, {name: "umulh", class: clsMul},
	// --- Data processing: 2-source (divide, variable shift) ---
	{name: "udiv", class: clsDataProc2}, {name: "sdiv", class: clsDataProc2},
	{name: "lslv", class: clsDataProc2}, {name: "lsrv", class: clsDataProc2},
	{name: "asrv", class: clsDataProc2}, {name: "rorv", class: clsDataProc2},
	// --- Data processing: 1-source ---
	{name: "rbit", class: clsDataProc1}, {name: "rev16", class: clsDataProc1},
	{name: "rev32", class: clsDataProc1}, {name: "rev", class: clsDataProc1},
	{name: "clz", class: clsDataProc1}, {name: "cls", class: clsDataProc1},
	// --- Bitfield + extract ---
	{name: "sbfm", class: clsBitfield}, {name: "bfm", class: clsBitfield},
	{name: "ubfm", class: clsBitfield}, {name: "extr", class: clsExtr},
	// --- Conditional select ---
	{name: "csel", class: clsCondSel}, {name: "csinc", class: clsCondSel},
	{name: "csinv", class: clsCondSel}, {name: "csneg", class: clsCondSel},
	// --- PC-relative address ---
	{name: "adr", class: clsAddr}, {name: "adrp", class: clsAddr},
	// --- Add/subtract with carry ---
	{name: "adc", class: clsAddSubCarry}, {name: "adcs", class: clsAddSubCarry},
	{name: "sbc", class: clsAddSubCarry}, {name: "sbcs", class: clsAddSubCarry},
	// --- Conditional compare ---
	{name: "ccmp", class: clsCondCmp}, {name: "ccmn", class: clsCondCmp},
	// --- System: hints, barriers, system-register move ---
	{name: "nop", class: clsSystem}, {name: "yield", class: clsSystem},
	{name: "wfe", class: clsSystem}, {name: "wfi", class: clsSystem},
	{name: "sev", class: clsSystem}, {name: "sevl", class: clsSystem},
	{name: "hint", class: clsSystem},
	{name: "dmb", class: clsSystem}, {name: "dsb", class: clsSystem},
	{name: "isb", class: clsSystem},
	{name: "mrs", class: clsSystem}, {name: "msr", class: clsSystem},
	{name: "tlbi", class: clsSystem},
	// --- Exception generation ---
	{name: "svc", class: clsException}, {name: "hvc", class: clsException},
	{name: "smc", class: clsException}, {name: "brk", class: clsException},
	{name: "hlt", class: clsException},
	// --- Unconditional immediate branch ---
	{name: "b", class: clsBranch, op: 0},
	{name: "bl", class: clsBranch, op: 1},
	// --- Compare and branch ---
	{name: "cbz", class: clsCompareBranch, op: 0},
	{name: "cbnz", class: clsCompareBranch, op: 1},
	// --- Unconditional register branch ---
	{name: "br", class: clsBranchReg, base: 0xD61F0000},
	{name: "blr", class: clsBranchReg, base: 0xD63F0000},
	{name: "ret", class: clsBranchReg, base: 0xD65F0000},
	{name: "eret", class: clsBranchReg, base: 0xD69F03E0},
}

var byName = func() map[string]*insn {
	m := map[string]*insn{}
	for i := range table {
		m[table[i].name] = &table[i]
	}
	return m
}()

// Assemble encodes a single AArch64 instruction to its 4 little-endian bytes.
// PC-relative branch operands take a numeric byte offset; label resolution is
// a program-level concern (see AssembleProgram).
func Assemble(src string) ([]byte, error) {
	w, err := assembleWord(src)
	if err != nil {
		return nil, err
	}
	if w == noWord {
		return nil, nil
	}
	return []byte{byte(w), byte(w >> 8), byte(w >> 16), byte(w >> 24)}, nil
}

// noWord marks a blank/comment line (no instruction). 0 is a real encoding
// (it's udf #0), so a sentinel outside the 32-bit range is used.
const noWord = ^uint64(0)

func assembleWord(src string) (uint64, error) {
	mnem, ops := parseLine(src)
	if mnem == "" {
		return noWord, nil
	}
	// b.<cond> is a conditional branch whose condition rides in the mnemonic.
	if strings.HasPrefix(mnem, "b.") {
		w, err := encodeBranchCond(mnem[2:], ops)
		if err != nil {
			return 0, fmt.Errorf("arm64 %q: %w", src, err)
		}
		return uint64(w), nil
	}
	if nm, no, ok := expandAlias(mnem, ops); ok {
		mnem, ops = nm, no
	}
	in, ok := byName[mnem]
	if !ok {
		return 0, fmt.Errorf("arm64: unknown instruction %q", mnem)
	}
	w, err := encode(in, ops)
	if err != nil {
		return 0, fmt.Errorf("arm64 %q: %w", src, err)
	}
	return uint64(w), nil
}

// parseLine splits a source line into a lower-case mnemonic and operand
// strings, stripping a '//', '#'-at-start, or ';' comment. Note '#' also
// introduces immediates, so only a ';' or '//' starts a comment.
func parseLine(src string) (mnem string, ops []string) {
	if i := strings.Index(src, "//"); i >= 0 {
		src = src[:i]
	}
	if i := strings.IndexByte(src, ';'); i >= 0 {
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
	for _, o := range splitOperands(src[sp+1:]) {
		if o = strings.TrimSpace(o); o != "" {
			ops = append(ops, o)
		}
	}
	return mnem, ops
}

// splitOperands splits on commas that are not inside [ ] brackets, so a memory
// operand like "[x1, #8]" stays one piece.
func splitOperands(s string) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '[':
			depth++
		case ']':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

func encode(in *insn, ops []string) (uint32, error) {
	switch in.class {
	case clsAddSub:
		return encodeAddSub(in, ops)
	case clsLogicalReg:
		return encodeLogicalReg(in, ops)
	case clsMoveWide:
		return encodeMoveWide(in, ops)
	case clsLoadStore:
		return encodeLoadStore(in.name, ops)
	case clsPair:
		return encodePair(in.name, ops)
	case clsMul:
		return encodeMul(in.name, ops)
	case clsDataProc2:
		return encodeDataProc2(in.name, ops)
	case clsDataProc1:
		return encodeDataProc1(in.name, ops)
	case clsBitfield:
		return encodeBitfield(in.name, ops)
	case clsExtr:
		return encodeExtr(ops)
	case clsCondSel:
		return encodeCondSel(in.name, ops)
	case clsAddr:
		return encodeAddr(in.name, ops)
	case clsAddSubCarry:
		return encodeAddSubCarry(in.name, ops)
	case clsCondCmp:
		return encodeCondCmp(in.name, ops)
	case clsSystem:
		return encodeSystem(in.name, ops)
	case clsException:
		return encodeException(in.name, ops)
	case clsBranch:
		return encodeBranch(in, ops)
	case clsCompareBranch:
		return encodeCompareBranch(in, ops)
	case clsBranchReg:
		return encodeBranchReg(in, ops)
	}
	return 0, fmt.Errorf("unhandled class")
}

// sfBit returns the 64-bit size flag (bit 31) for a register.
func sfBit(r reg) uint32 {
	if r.is64 {
		return 1 << 31
	}
	return 0
}

// shiftTypes maps a shift keyword to its 2-bit type for shifted-register forms.
var shiftTypes = map[string]uint32{"lsl": 0, "lsr": 1, "asr": 2, "ror": 3}

// parseShift parses an optional trailing "lsl/lsr/asr/ror #amount" operand,
// returning the shift type and amount. allowRor gates ROR (logical only).
func parseShift(s string, allowRor bool) (typ, amount uint32, ok bool) {
	f := strings.Fields(strings.ToLower(s))
	if len(f) != 2 {
		return 0, 0, false
	}
	t, ok := shiftTypes[f[0]]
	if !ok || (t == 3 && !allowRor) {
		return 0, 0, false
	}
	amt, ok2 := parseImm(f[1])
	if !ok2 || amt < 0 || amt > 63 {
		return 0, 0, false
	}
	return t, uint32(amt), true
}

func encodeAddSub(in *insn, ops []string) (uint32, error) {
	if len(ops) < 3 || len(ops) > 4 {
		return 0, fmt.Errorf("expected 3-4 operands")
	}
	rd, ok1 := parseReg(ops[0])
	rn, ok2 := parseReg(ops[1])
	if !ok1 || !ok2 {
		return 0, fmt.Errorf("bad register operand")
	}
	common := sfBit(rd) | in.op<<30 | in.s<<29 | rn.num<<5 | rd.num
	// Immediate form: third operand is an immediate.
	if strings.HasPrefix(strings.TrimSpace(ops[2]), "#") || isImmOperand(ops[2]) {
		imm, ok := parseImm(ops[2])
		if !ok {
			return 0, fmt.Errorf("bad immediate")
		}
		var sh uint32
		if len(ops) == 4 {
			t, amt, ok := parseShift(ops[3], false)
			if !ok || t != 0 || (amt != 0 && amt != 12) {
				return 0, fmt.Errorf("add/sub immediate shift must be lsl #0 or lsl #12")
			}
			if amt == 12 {
				sh = 1
			}
		}
		if imm < 0 || imm > 0xFFF {
			return 0, fmt.Errorf("immediate out of 12-bit range")
		}
		return 0x11000000 | common | sh<<22 | uint32(imm)<<10, nil
	}
	// Shifted-register form.
	rm, ok := parseReg(ops[2])
	if !ok {
		return 0, fmt.Errorf("bad register operand")
	}
	// SP can't appear in the shifted-register form (field 31 means XZR there),
	// so when Rd or Rn is the stack pointer the assembler must use the
	// extended-register encoding: option = LSL (011 for x, 010 for w), and an
	// optional "lsl #amount" becomes imm3 (0–4).
	if rd.isSP || rn.isSP {
		var imm3 uint32
		if len(ops) == 4 {
			t, amt, ok := parseShift(ops[3], false)
			if !ok || t != 0 || amt > 4 {
				return 0, fmt.Errorf("add/sub with sp allows only lsl #0..#4")
			}
			imm3 = amt
		}
		option := uint32(0b011) // UXTX (= LSL) for 64-bit
		if !rd.is64 {
			option = 0b010 // UXTW for 32-bit
		}
		return 0x0B200000 | common | rm.num<<16 | option<<13 | imm3<<10, nil
	}
	var shift, amount uint32
	if len(ops) == 4 {
		var ok bool
		shift, amount, ok = parseShift(ops[3], false)
		if !ok {
			return 0, fmt.Errorf("bad shift")
		}
	}
	return 0x0B000000 | common | shift<<22 | rm.num<<16 | amount<<10, nil
}

func encodeLogicalReg(in *insn, ops []string) (uint32, error) {
	if len(ops) < 3 || len(ops) > 4 {
		return 0, fmt.Errorf("expected 3-4 operands")
	}
	rd, ok1 := parseReg(ops[0])
	rn, ok2 := parseReg(ops[1])
	if !ok1 || !ok2 {
		return 0, fmt.Errorf("bad register operand")
	}
	// Immediate (bitmask) form — and/orr/eor/ands only; bic/orn/eon/bics have
	// no immediate encoding (use the inverted immediate with the base op).
	if len(ops) == 3 && (strings.HasPrefix(strings.TrimSpace(ops[2]), "#") || isImmOperand(ops[2])) {
		if in.n == 1 {
			return 0, fmt.Errorf("%s has no immediate form", in.name)
		}
		imm, ok := parseImm(ops[2])
		if !ok {
			return 0, fmt.Errorf("bad immediate")
		}
		regSize := 32
		if rd.is64 {
			regSize = 64
		}
		n, immr, imms, ok := encodeBitmask(uint64(imm), regSize)
		if !ok {
			return 0, fmt.Errorf("%#x is not a valid logical immediate", uint64(imm))
		}
		return 0x12000000 | sfBit(rd) | in.opc<<29 | n<<22 | immr<<16 | imms<<10 | rn.num<<5 | rd.num, nil
	}
	rm, ok3 := parseReg(ops[2])
	if !ok3 {
		return 0, fmt.Errorf("bad register operand")
	}
	var shift, amount uint32
	if len(ops) == 4 {
		var ok bool
		shift, amount, ok = parseShift(ops[3], true)
		if !ok {
			return 0, fmt.Errorf("bad shift")
		}
	}
	return 0x0A000000 | sfBit(rd) | in.opc<<29 | shift<<22 | in.n<<21 |
		rm.num<<16 | amount<<10 | rn.num<<5 | rd.num, nil
}

func encodeMoveWide(in *insn, ops []string) (uint32, error) {
	if len(ops) < 2 || len(ops) > 3 {
		return 0, fmt.Errorf("expected 2-3 operands")
	}
	rd, ok := parseReg(ops[0])
	if !ok {
		return 0, fmt.Errorf("bad register operand")
	}
	imm, ok := parseImm(ops[1])
	if !ok || imm < 0 || imm > 0xFFFF {
		return 0, fmt.Errorf("move-wide immediate must be 0..0xFFFF (use the hw shift for high halves)")
	}
	var hw uint32
	if len(ops) == 3 {
		t, amt, ok := parseShift(ops[2], false)
		if !ok || t != 0 || amt%16 != 0 || amt > 48 {
			return 0, fmt.Errorf("move-wide shift must be lsl #0/#16/#32/#48")
		}
		hw = amt / 16
	}
	if !rd.is64 && hw > 1 {
		return 0, fmt.Errorf("32-bit move-wide shift must be lsl #0 or #16")
	}
	return 0x12800000 | sfBit(rd) | in.opc<<29 | hw<<21 | uint32(imm)<<5 | rd.num, nil
}


func encodeBranch(in *insn, ops []string) (uint32, error) {
	if len(ops) != 1 {
		return 0, fmt.Errorf("expected 1 operand")
	}
	off, ok := parseImm(ops[0])
	if !ok {
		return 0, fmt.Errorf("branch target must be a numeric offset here (labels resolve at program level)")
	}
	if off%4 != 0 {
		return 0, fmt.Errorf("branch offset must be 4-byte aligned")
	}
	imm26 := uint32((off >> 2) & 0x3FFFFFF)
	return 0x14000000 | in.op<<31 | imm26, nil
}

func encodeBranchCond(cond string, ops []string) (uint32, error) {
	c, ok := condCodes[cond]
	if !ok {
		return 0, fmt.Errorf("unknown condition %q", cond)
	}
	if len(ops) != 1 {
		return 0, fmt.Errorf("expected 1 operand")
	}
	off, ok := parseImm(ops[0])
	if !ok {
		return 0, fmt.Errorf("branch target must be a numeric offset here")
	}
	if off%4 != 0 {
		return 0, fmt.Errorf("branch offset must be 4-byte aligned")
	}
	imm19 := uint32((off >> 2) & 0x7FFFF)
	return 0x54000000 | imm19<<5 | c, nil
}

func encodeCompareBranch(in *insn, ops []string) (uint32, error) {
	if len(ops) != 2 {
		return 0, fmt.Errorf("expected 2 operands")
	}
	rt, ok := parseReg(ops[0])
	if !ok {
		return 0, fmt.Errorf("bad register operand")
	}
	off, ok := parseImm(ops[1])
	if !ok {
		return 0, fmt.Errorf("branch target must be a numeric offset here")
	}
	if off%4 != 0 {
		return 0, fmt.Errorf("branch offset must be 4-byte aligned")
	}
	imm19 := uint32((off >> 2) & 0x7FFFF)
	return 0x34000000 | sfBit(rt) | in.op<<24 | imm19<<5 | rt.num, nil
}

func encodeBranchReg(in *insn, ops []string) (uint32, error) {
	if in.name == "eret" { // fixed encoding, no register operand
		if len(ops) != 0 {
			return 0, fmt.Errorf("eret takes no operands")
		}
		return in.base, nil
	}
	rn := reg{num: 30, is64: true} // ret defaults to x30 (lr)
	if len(ops) == 1 {
		r, ok := parseReg(ops[0])
		if !ok {
			return 0, fmt.Errorf("bad register operand")
		}
		rn = r
	} else if len(ops) != 0 {
		return 0, fmt.Errorf("expected 0-1 operands")
	}
	return in.base | rn.num<<5, nil
}

// isImmOperand reports whether an operand looks like a bare numeric immediate
// (no '#'), so "add x0, x1, 8" is accepted alongside "add x0, x1, #8".
func isImmOperand(s string) bool {
	_, ok := parseImm(s)
	if !ok {
		return false
	}
	if _, isReg := parseReg(s); isReg {
		return false
	}
	return true
}
