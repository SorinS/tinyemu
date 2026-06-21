package arm64

import (
	"testing"

	"github.com/sorins/tinyemu-go/mem"
)

// Build SIMD instruction words from fields (validated against real FreeBSD
// kernel opcodes 0x2e303800 (uaddlv) and 0x4e209823 (cmeq #0)).
func across(q, u, size, opcode, rn, rd uint32) uint32 {
	return q<<30 | u<<29 | 0x0E<<24 | size<<22 | 0x18<<17 | opcode<<12 | 0x2<<10 | rn<<5 | rd
}
func tworeg(q, u, size, opcode, rn, rd uint32) uint32 {
	return q<<30 | u<<29 | 0x0E<<24 | size<<22 | 0x10<<17 | opcode<<12 | 0x2<<10 | rn<<5 | rd
}

func runV(t *testing.T, word uint32, v1 [2]uint64) [2]uint64 {
	t.Helper()
	c := New(mem.NewPhysMemoryMap())
	c.Vreg[1] = v1
	var next uint64
	if err := c.exec(word, &next); err != nil {
		t.Fatalf("exec %08x: %v", word, err)
	}
	return c.Vreg[0]
}

func TestSIMD_UADDLV(t *testing.T) {
	// uaddlv h0, v1.8b : sum of the low 8 bytes -> 16-bit result.
	got := runV(t, across(0, 1, 0, 0x03, 1, 0), [2]uint64{0x0102030405060708, 0xdead})
	if got != [2]uint64{0x24, 0} { // 1+2+...+8 = 36
		t.Errorf("uaddlv .8b = %#x, want 0x24", got)
	}
	// uaddlv h0, v1.16b : all 16 bytes.
	got = runV(t, across(1, 1, 0, 0x03, 1, 0), [2]uint64{0x0102030405060708, 0x0101010101010101})
	if got != [2]uint64{0x2C, 0} { // 36 + 8
		t.Errorf("uaddlv .16b = %#x, want 0x2C", got)
	}
}

func TestSIMD_CmpZero(t *testing.T) {
	// cmeq v0.16b, v1.16b, #0 : zero bytes -> 0xFF, else 0.
	if got := runV(t, tworeg(1, 0, 0, 0x09, 1, 0), [2]uint64{0, 0}); got != [2]uint64{^uint64(0), ^uint64(0)} {
		t.Errorf("cmeq#0 (all zero) = %#x, want all ones", got)
	}
	if got := runV(t, tworeg(1, 0, 0, 0x09, 1, 0), [2]uint64{^uint64(0), ^uint64(0)}); got != [2]uint64{0, 0} {
		t.Errorf("cmeq#0 (no zero) = %#x, want all zero", got)
	}
	// cmgt v0.4s, v1.4s, #0 : signed >0 lanes -> all ones.
	v := [2]uint64{0xFFFFFFFF_00000001, 0x80000000_7FFFFFFF} // lanes: 1,-1,MAX,MIN
	got := runV(t, tworeg(1, 0, 2, 0x08, 1, 0), v)
	if got != [2]uint64{0x00000000_FFFFFFFF, 0x00000000_FFFFFFFF} {
		t.Errorf("cmgt#0 = %#x", got)
	}
	// cmlt v0.4s, v1.4s, #0 : signed <0 lanes -> all ones (complement of >=0).
	got = runV(t, tworeg(1, 0, 2, 0x0A, 1, 0), v)
	if got != [2]uint64{0xFFFFFFFF_00000000, 0xFFFFFFFF_00000000} {
		t.Errorf("cmlt#0 = %#x", got)
	}
}

func TestSIMD_ClzCls(t *testing.T) {
	// clz v0.4s, v1.4s
	v := [2]uint64{0x00000001_00000000, 0x80000000_00008000} // lanes: 0,1,0x8000,0x80000000
	got := runV(t, tworeg(1, 1, 2, 0x04, 1, 0), v)
	want := [2]uint64{32 | 31<<32, 16 | 0<<32}
	if got != want {
		t.Errorf("clz = %#x, want %#x", got, want)
	}
	// cls v0.4s, v1.4s : leading sign bits (excludes the sign bit).
	v = [2]uint64{0xFFFFFFFF_00000000, 0xC0000000_40000000} // lanes: 0,-1,0x40000000,0xC0000000
	got = runV(t, tworeg(1, 0, 2, 0x04, 1, 0), v)
	want = [2]uint64{31 | 31<<32, 0 | 1<<32}
	if got != want {
		t.Errorf("cls = %#x, want %#x", got, want)
	}
}
