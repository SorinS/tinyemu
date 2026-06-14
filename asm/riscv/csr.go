package riscv

import "strings"

// csrByName maps common control/status register names to their 12-bit address.
// A numeric operand (decimal or 0x…) is also accepted by parseCSR.
var csrByName = map[string]uint32{
	// Machine-level
	"mstatus": 0x300, "misa": 0x301, "medeleg": 0x302, "mideleg": 0x303,
	"mie": 0x304, "mtvec": 0x305, "mcounteren": 0x306,
	"mscratch": 0x340, "mepc": 0x341, "mcause": 0x342, "mtval": 0x343, "mip": 0x344,
	"mvendorid": 0xF11, "marchid": 0xF12, "mimpid": 0xF13, "mhartid": 0xF14,
	// Supervisor-level
	"sstatus": 0x100, "sie": 0x104, "stvec": 0x105, "scounteren": 0x106,
	"sscratch": 0x140, "sepc": 0x141, "scause": 0x142, "stval": 0x143, "sip": 0x144,
	"satp": 0x180,
	// Unprivileged counters
	"cycle": 0xC00, "time": 0xC01, "instret": 0xC02,
	"fflags": 0x001, "frm": 0x002, "fcsr": 0x003,
}

// parseCSR resolves a CSR operand (name or numeric literal) to its 12-bit
// address.
func parseCSR(s string) (uint32, bool) {
	s = strings.TrimSpace(strings.ToLower(s))
	if v, ok := csrByName[s]; ok {
		return v, true
	}
	if v, err := parseImm(s); err == nil && v >= 0 && v <= 0xFFF {
		return uint32(v), true
	}
	return 0, false
}

// csrName returns a CSR's name for disassembly, or its hex address if unknown.
func csrName(addr uint32) string {
	for name, v := range csrByName {
		if v == addr {
			return name
		}
	}
	return "0x" + strings.TrimPrefix(itoaHex(addr), "0x")
}

func itoaHex(v uint32) string {
	const digits = "0123456789abcdef"
	if v == 0 {
		return "0"
	}
	var b [8]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = digits[v&0xF]
		v >>= 4
	}
	return string(b[i:])
}
