package arm64

import "sort"

// aliasMnemonics are the assembler aliases (not present in the encoding table)
// plus the condition-suffixed forms, for editor completion/diagnostics.
var aliasMnemonics = []string{
	"mov", "cmp", "cmn", "tst", "neg", "negs", "mvn",
	"mul", "mneg", "smull", "umull", "smnegl", "umnegl",
	"lsl", "lsr", "asr", "ror",
	"ubfx", "sbfx", "bfxil", "ubfiz", "sbfiz", "bfi",
	"uxtb", "uxth", "sxtb", "sxth", "sxtw",
	"cset", "csetm", "cinc", "cinv", "cneg",
}

// Mnemonics returns every mnemonic the assembler accepts — encoding-table
// entries, aliases, and the b.<cond> conditional branches — sorted and unique.
func Mnemonics() []string {
	set := map[string]bool{}
	for i := range table {
		set[table[i].name] = true
	}
	for _, m := range aliasMnemonics {
		set[m] = true
	}
	for cond := range condCodes {
		set["b."+cond] = true
	}
	out := make([]string, 0, len(set))
	for m := range set {
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

// IsMnemonic reports whether s is a mnemonic the assembler accepts (including
// b.<cond>).
func IsMnemonic(s string) bool {
	for _, m := range Mnemonics() {
		if m == s {
			return true
		}
	}
	return false
}
